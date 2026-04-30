package services

import (
	"context"
	"strings"
	"testing"
)

func TestParseJavaProcessesFromJpsAndPsOutput(t *testing.T) {
	output := `1234 org.springframework.boot.loader.JarLauncher
2345 com.example.Worker
root      3456     1  0 10:10 ?        00:00:10 java -jar /app/order.jar`

	processes := ParseArthasJavaProcesses(output)

	if len(processes) != 3 {
		t.Fatalf("expected 3 java processes, got %d", len(processes))
	}
	if processes[0].PID != "1234" || processes[0].MainClass != "org.springframework.boot.loader.JarLauncher" {
		t.Fatalf("unexpected first process: %#v", processes[0])
	}
	if processes[2].PID != "3456" || processes[2].MainClass != "java -jar /app/order.jar" {
		t.Fatalf("unexpected ps process: %#v", processes[2])
	}
}

func TestBuildArthasCommandScriptUsesDetectedLauncher(t *testing.T) {
	session := ArthasSession{
		PID:     "1234",
		Command: "jvm",
	}

	script := BuildArthasCommandScript(session)

	if script == "" {
		t.Fatal("expected script")
	}
	if !containsAll(script, "as.sh", "arthas-boot.jar", "-c", "jvm") {
		t.Fatalf("script does not include expected launcher fallback: %s", script)
	}
}

func TestBuildArthasCommandScriptDownloadsBootJarWhenConfigured(t *testing.T) {
	session := ArthasSession{
		PID:           "1234",
		Command:       "jvm",
		PackageSource: "url",
		PackageURL:    "https://arthas.aliyun.com/arthas-boot.jar",
	}

	script := BuildArthasCommandScript(session)

	if !containsAll(script, "mkdir -p /tmp/arthas", "curl -fsSL", "wget -q", "arthas.aliyun.com/arthas-boot.jar") {
		t.Fatalf("script does not include download fallback: %s", script)
	}
}

func TestExecuteDoesNotRunWhenLowRiskAutoExecutionDisabled(t *testing.T) {
	executor := &fakeArthasExecutor{}
	service := NewArthasSessionService(executor, NewArthasCommandPolicy(false, 4096), 0)

	result, decision, err := service.Execute(context.Background(), ArthasSession{
		Namespace: "default",
		PodName:   "app",
		Container: "app",
		PID:       "1234",
		Command:   "jvm",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected no execution result")
	}
	if decision.AutoExecutable {
		t.Fatal("expected auto execution to be disabled")
	}
	if executor.called {
		t.Fatal("executor should not be called")
	}
}

func TestExecuteConfirmedTruncatesCombinedOutput(t *testing.T) {
	executor := &fakeArthasExecutor{stdout: "1234567890", stderr: "abcdefghij"}
	service := NewArthasSessionService(executor, NewArthasCommandPolicy(true, 8), 0)

	result, _, err := service.ExecuteConfirmed(context.Background(), ArthasSession{
		Namespace: "default",
		PodName:   "app",
		Container: "app",
		PID:       "1234",
		Command:   "jvm",
	}, ArthasCommandDecision{Risk: ArthasRiskLow, AutoExecutable: true, MaxOutputBytes: 8})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Stdout, "output truncated") {
		t.Fatalf("expected truncated output, got stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
	if result.Stderr != "" {
		t.Fatalf("expected stderr to be folded into truncated stdout, got %q", result.Stderr)
	}
}

type fakeArthasExecutor struct {
	called bool
	stdout string
	stderr string
}

func (f *fakeArthasExecutor) Exec(_ context.Context, _, _, _ string, _ []string) (*ArthasExecResult, error) {
	f.called = true
	stdout := f.stdout
	if stdout == "" {
		stdout = "ok"
	}
	return &ArthasExecResult{Stdout: stdout, Stderr: f.stderr}, nil
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
