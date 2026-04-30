package services

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ArthasJavaProcess 表示容器内可诊断的 Java 进程。
type ArthasJavaProcess struct {
	PID       string `json:"pid"`
	MainClass string `json:"mainClass"`
}

// ArthasStatus 描述目标容器内 Arthas 诊断可用性。
type ArthasStatus struct {
	Enabled          bool                `json:"enabled"`
	Attached         bool                `json:"attached"`
	Container        string              `json:"container"`
	Processes        []ArthasJavaProcess `json:"processes"`
	ArthasAvailable  bool                `json:"arthasAvailable"`
	Downloadable     bool                `json:"downloadable"`
	Launcher         string              `json:"launcher,omitempty"`
	Message          string              `json:"message,omitempty"`
	AutoExecLowRisk  bool                `json:"autoExecLowRisk"`
	MaxOutputBytes   int64               `json:"maxOutputBytes"`
	SessionTimeoutMS int64               `json:"sessionTimeoutMs"`
}

// ArthasSession 是一次 Arthas 命令执行的目标上下文。
type ArthasSession struct {
	ClusterID string
	Namespace string
	PodName   string
	Container string
	PID       string
	Command   string
	// PackageSource/PackageURL 允许诊断时按需下载 arthas-boot.jar。
	PackageSource string
	PackageURL    string
}

// ArthasExecResult 是容器命令执行结果。
type ArthasExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exitCode"`
}

// PodCommandExecutor 在目标 Pod 容器内执行命令。
type PodCommandExecutor interface {
	Exec(ctx context.Context, namespace, podName, container string, command []string) (*ArthasExecResult, error)
}

// K8sPodCommandExecutor 使用 Kubernetes exec 子资源执行命令。
type K8sPodCommandExecutor struct {
	client *kubernetes.Clientset
	config *rest.Config
}

func NewK8sPodCommandExecutor(client *kubernetes.Clientset, config *rest.Config) *K8sPodCommandExecutor {
	return &K8sPodCommandExecutor{client: client, config: config}
}

func (e *K8sPodCommandExecutor) Exec(ctx context.Context, namespace, podName, container string, command []string) (*ArthasExecResult, error) {
	req := e.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   command,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(e.config, "POST", req.URL())
	if err != nil {
		return nil, fmt.Errorf("创建 exec 连接失败: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})
	result := &ArthasExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		result.ExitCode = 1
		return result, fmt.Errorf("执行容器命令失败: %w", err)
	}
	return result, nil
}

// ArthasSessionService 负责探测与执行目标容器内 Arthas 命令。
type ArthasSessionService struct {
	executor       PodCommandExecutor
	policy         *ArthasCommandPolicy
	sessionTimeout time.Duration
	packageSource  string
	packageURL     string
}

func NewArthasSessionService(executor PodCommandExecutor, policy *ArthasCommandPolicy, sessionTimeout time.Duration) *ArthasSessionService {
	if sessionTimeout <= 0 {
		sessionTimeout = 30 * time.Second
	}
	return &ArthasSessionService{
		executor:       executor,
		policy:         policy,
		sessionTimeout: sessionTimeout,
		packageSource:  "container",
	}
}

func NewArthasSessionServiceWithPackage(executor PodCommandExecutor, policy *ArthasCommandPolicy, sessionTimeout time.Duration, packageSource, packageURL string) *ArthasSessionService {
	service := NewArthasSessionService(executor, policy, sessionTimeout)
	service.packageSource = strings.TrimSpace(packageSource)
	service.packageURL = strings.TrimSpace(packageURL)
	return service
}

func (s *ArthasSessionService) GetStatus(ctx context.Context, namespace, podName, container string) (*ArthasStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, s.sessionTimeout)
	defer cancel()

	result, err := s.executor.Exec(ctx, namespace, podName, container, []string{"sh", "-c", buildArthasStatusScript()})
	if err != nil && result == nil {
		return nil, err
	}

	output := ""
	if result != nil {
		output = result.Stdout + "\n" + result.Stderr
	}
	processes := ParseArthasJavaProcesses(output)
	launcher := detectArthasLauncher(output)

	status := &ArthasStatus{
		Enabled:          true,
		Attached:         false,
		Container:        container,
		Processes:        processes,
		ArthasAvailable:  launcher != "",
		Downloadable:     s.canDownloadArthas(),
		Launcher:         launcher,
		AutoExecLowRisk:  s.policy.Classify("jvm").AutoExecutable,
		MaxOutputBytes:   s.policy.Classify("jvm").MaxOutputBytes,
		SessionTimeoutMS: s.sessionTimeout.Milliseconds(),
	}
	if len(processes) == 0 {
		status.Message = "未发现 Java 进程"
	} else if launcher == "" && status.Downloadable {
		status.Message = "已发现 Java 进程，将在首次诊断时下载 arthas-boot.jar"
	} else if launcher == "" {
		status.Message = "已发现 Java 进程，但未检测到 Arthas 启动脚本或 arthas-boot.jar，也未配置下载地址"
	} else {
		status.Message = "可创建 Arthas 诊断会话"
	}
	return status, nil
}

func (s *ArthasSessionService) Execute(ctx context.Context, session ArthasSession) (*ArthasExecResult, ArthasCommandDecision, error) {
	decision := s.policy.Classify(session.Command)
	if !decision.AutoExecutable {
		return nil, decision, nil
	}
	return s.ExecuteConfirmed(ctx, session, decision)
}

func (s *ArthasSessionService) ExecuteConfirmed(ctx context.Context, session ArthasSession, decision ArthasCommandDecision) (*ArthasExecResult, ArthasCommandDecision, error) {
	ctx, cancel := context.WithTimeout(ctx, s.sessionTimeout)
	defer cancel()

	s.applyPackageConfig(&session)
	result, err := s.executor.Exec(ctx, session.Namespace, session.PodName, session.Container, []string{"sh", "-c", BuildArthasCommandScript(session)})
	if result != nil && int64(len(result.Stdout)+len(result.Stderr)) > decision.MaxOutputBytes {
		combined := strings.TrimRight(result.Stdout, "\n")
		if result.Stderr != "" {
			combined += "\n[stderr]\n" + result.Stderr
		}
		result.Stdout = trimBytes(combined, decision.MaxOutputBytes)
		result.Stderr = ""
	}
	return result, decision, err
}

func (s *ArthasSessionService) applyPackageConfig(session *ArthasSession) {
	if session.PackageSource == "" {
		session.PackageSource = s.packageSource
	}
	if session.PackageURL == "" {
		session.PackageURL = s.packageURL
	}
}

func (s *ArthasSessionService) canDownloadArthas() bool {
	return strings.EqualFold(s.packageSource, "url") && strings.TrimSpace(s.packageURL) != ""
}

// ParseArthasJavaProcesses 从 jps/ps 输出中提取 Java 进程。
func ParseArthasJavaProcesses(output string) []ArthasJavaProcess {
	processes := make([]ArthasJavaProcess, 0)
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if proc, ok := parseJpsLine(line); ok && !seen[proc.PID] {
			processes = append(processes, proc)
			seen[proc.PID] = true
			continue
		}
		if proc, ok := parsePsJavaLine(line); ok && !seen[proc.PID] {
			processes = append(processes, proc)
			seen[proc.PID] = true
		}
	}
	return processes
}

// BuildArthasCommandScript 构建容器内执行 Arthas 命令的脚本。
func BuildArthasCommandScript(session ArthasSession) string {
	pid := shellQuote(session.PID)
	command := shellQuote(session.Command)
	finalBranch := buildArthasFinalBranch(session.PackageSource, session.PackageURL, pid, command)
	return fmt.Sprintf(`set -e
if command -v as.sh >/dev/null 2>&1; then
  as.sh --select %s -c %s
elif [ -f /opt/arthas/as.sh ]; then
  /opt/arthas/as.sh --select %s -c %s
elif [ -f /tmp/arthas/arthas-boot.jar ]; then
  java -jar /tmp/arthas/arthas-boot.jar --target-ip 127.0.0.1 %s -c %s
elif [ -f /opt/arthas/arthas-boot.jar ]; then
  java -jar /opt/arthas/arthas-boot.jar --target-ip 127.0.0.1 %s -c %s
%s
fi`, pid, command, pid, command, pid, command, pid, command, finalBranch)
}

func buildArthasFinalBranch(packageSource, packageURL, pid, command string) string {
	if !strings.EqualFold(strings.TrimSpace(packageSource), "url") || strings.TrimSpace(packageURL) == "" {
		return `else
  echo "Arthas launcher not found. Please provide as.sh or arthas-boot.jar in the container." >&2
  exit 127`
	}
	url := shellQuote(packageURL)
	return fmt.Sprintf(`else
  mkdir -p /tmp/arthas
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL %s -o /tmp/arthas/arthas-boot.jar
  elif command -v wget >/dev/null 2>&1; then
    wget -q %s -O /tmp/arthas/arthas-boot.jar
  else
    echo "curl or wget is required to download arthas-boot.jar." >&2
    exit 127
  fi
  java -jar /tmp/arthas/arthas-boot.jar --target-ip 127.0.0.1 %s -c %s`, url, url, pid, command)
}

func buildArthasStatusScript() string {
	return `set +e
if command -v as.sh >/dev/null 2>&1; then echo "ARTHAS_LAUNCHER=as.sh"; fi
if [ -f /opt/arthas/as.sh ]; then echo "ARTHAS_LAUNCHER=/opt/arthas/as.sh"; fi
if [ -f /tmp/arthas/arthas-boot.jar ]; then echo "ARTHAS_LAUNCHER=/tmp/arthas/arthas-boot.jar"; fi
if [ -f /opt/arthas/arthas-boot.jar ]; then echo "ARTHAS_LAUNCHER=/opt/arthas/arthas-boot.jar"; fi
if command -v jps >/dev/null 2>&1; then jps -l; fi
ps -eo pid,args 2>/dev/null | grep '[j]ava'`
}

func parseJpsLine(line string) (ArthasJavaProcess, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ArthasJavaProcess{}, false
	}
	if _, err := strconv.Atoi(fields[0]); err != nil {
		return ArthasJavaProcess{}, false
	}
	if strings.HasPrefix(fields[1], "ARTHAS_LAUNCHER=") {
		return ArthasJavaProcess{}, false
	}
	return ArthasJavaProcess{PID: fields[0], MainClass: strings.Join(fields[1:], " ")}, true
}

func parsePsJavaLine(line string) (ArthasJavaProcess, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ArthasJavaProcess{}, false
	}
	pidIndex := 0
	if _, err := strconv.Atoi(fields[0]); err != nil {
		if len(fields) < 3 {
			return ArthasJavaProcess{}, false
		}
		pidIndex = 1
	}
	pid := fields[pidIndex]
	if _, err := strconv.Atoi(pid); err != nil {
		return ArthasJavaProcess{}, false
	}
	args := strings.Join(fields[pidIndex+1:], " ")
	javaIndex := strings.Index(args, "java")
	if javaIndex < 0 {
		return ArthasJavaProcess{}, false
	}
	return ArthasJavaProcess{PID: pid, MainClass: strings.TrimSpace(args[javaIndex:])}, true
}

func detectArthasLauncher(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ARTHAS_LAUNCHER=") {
			return strings.TrimPrefix(line, "ARTHAS_LAUNCHER=")
		}
	}
	return ""
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func trimBytes(value string, maxBytes int64) string {
	if maxBytes <= 0 || int64(len(value)) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "\n... output truncated ..."
}
