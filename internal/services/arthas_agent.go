package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/clay-wangzhi/KubePolaris/pkg/logger"
)

// ArthasPlan 是 Agent 生成的下一步诊断计划。
type ArthasPlan struct {
	Intent          string                 `json:"intent"`
	Skill           string                 `json:"skill"`
	Reasoning       string                 `json:"reasoning"`
	Commands        []ArthasPlannedCommand `json:"commands"`
	ExpectedSignals []string               `json:"expectedSignals"`
	Report          string                 `json:"report,omitempty"`
}

// ArthasPlannedCommand 描述一条待执行 Arthas 命令。
type ArthasPlannedCommand struct {
	ID                   string     `json:"id"`
	Command              string     `json:"command"`
	Purpose              string     `json:"purpose"`
	Risk                 ArthasRisk `json:"risk"`
	RequiresConfirmation bool       `json:"requiresConfirmation"`
}

// ArthasDiagnosisReport 是面向用户的结构化诊断报告。
type ArthasDiagnosisReport struct {
	Conclusion      string   `json:"conclusion"`
	Evidence        []string `json:"evidence"`
	PossibleCauses  []string `json:"possibleCauses"`
	Recommendations []string `json:"recommendations"`
}

// ArthasAgentService 负责将自然语言诊断请求转换为 Arthas 诊断计划。
type ArthasAgentService struct {
	aiConfigService *AIConfigService
	policy          *ArthasCommandPolicy
}

func NewArthasAgentService(aiConfigService *AIConfigService) *ArthasAgentService {
	return &ArthasAgentService{
		aiConfigService: aiConfigService,
		policy:          NewArthasCommandPolicy(true, 1024*1024),
	}
}

func NewArthasAgentServiceWithPolicy(aiConfigService *AIConfigService, policy *ArthasCommandPolicy) *ArthasAgentService {
	return &ArthasAgentService{
		aiConfigService: aiConfigService,
		policy:          policy,
	}
}

// BuildPlan 优先使用已配置的 AI Provider，未配置或调用失败时使用内置 Skills 兜底。
func (s *ArthasAgentService) BuildPlan(ctx context.Context, prompt string, evidence []string) (*ArthasPlan, error) {
	if s.aiConfigService == nil || !s.aiConfigService.IsEnabled() {
		logger.Warn("Arthas Agent 使用内置 fallback: AI 配置未启用")
		plan := s.BuildFallbackPlan(prompt)
		return &plan, nil
	}

	config, err := s.aiConfigService.GetConfigWithAPIKey()
	if err != nil || config == nil {
		logger.Warn("Arthas Agent 使用内置 fallback: 获取 AI 配置失败", "error", err)
		plan := s.BuildFallbackPlan(prompt)
		return &plan, nil
	}

	provider := NewAIProvider(config)
	resp, err := provider.Chat(ctx, ChatRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: arthasAgentSystemPrompt()},
			{Role: "user", Content: buildArthasAgentUserPrompt(prompt, evidence)},
		},
	})
	if err != nil || len(resp.Choices) == 0 {
		logger.Warn("Arthas Agent 使用内置 fallback: LLM 调用失败或无返回", "error", err)
		plan := s.BuildFallbackPlan(prompt)
		return &plan, nil
	}

	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	plan, err := parseArthasPlanContent(content)
	if err != nil {
		logger.Warn("Arthas Agent 使用内置 fallback: LLM 返回不是可解析计划 JSON", "error", err)
		plan := s.BuildFallbackPlan(prompt)
		plan.Report = content
		return &plan, nil
	}
	s.applyPolicy(plan)
	return plan, nil
}

// BuildReport 优先使用 AI 基于真实 Arthas 输出生成诊断报告，失败时回退到保守模板。
func (s *ArthasAgentService) BuildReport(ctx context.Context, prompt string, plan *ArthasPlan, evidence []string) ArthasDiagnosisReport {
	fallbackIntent := prompt
	if plan != nil {
		fallbackIntent = strings.TrimSpace(prompt + " " + plan.Intent + " " + plan.Skill)
	}
	if s.aiConfigService == nil || !s.aiConfigService.IsEnabled() {
		logger.Warn("Arthas Agent 使用内置报告 fallback: AI 配置未启用")
		return s.BuildFallbackReport(fallbackIntent, evidence)
	}

	config, err := s.aiConfigService.GetConfigWithAPIKey()
	if err != nil || config == nil {
		logger.Warn("Arthas Agent 使用内置报告 fallback: 获取 AI 配置失败", "error", err)
		return s.BuildFallbackReport(fallbackIntent, evidence)
	}

	provider := NewAIProvider(config)
	resp, err := provider.Chat(ctx, ChatRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: arthasReportSystemPrompt()},
			{Role: "user", Content: buildArthasReportUserPrompt(prompt, plan, evidence)},
		},
	})
	if err != nil || len(resp.Choices) == 0 {
		logger.Warn("Arthas Agent 使用内置报告 fallback: LLM 调用失败或无返回", "error", err)
		return s.BuildFallbackReport(fallbackIntent, evidence)
	}

	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	report, err := parseDiagnosisReportContent(content)
	if err != nil {
		logger.Warn("Arthas Agent 使用内置报告 fallback: LLM 返回不是可解析报告 JSON", "error", err)
		fallback := s.BuildFallbackReport(fallbackIntent, evidence)
		if content != "" {
			fallback.Conclusion = content
		}
		return fallback
	}
	if len(report.Evidence) == 0 {
		report.Evidence = evidence
	}
	return *report
}

// BuildFallbackPlan 使用内置 Skill 生成保守诊断计划。
func (s *ArthasAgentService) BuildFallbackPlan(prompt string) ArthasPlan {
	lower := strings.ToLower(prompt)
	switch {
	case strings.Contains(lower, "cpu") || strings.Contains(lower, "负载") || strings.Contains(lower, "load"):
		return s.planFromCommands("CPU 使用率高", "cpu-high", "先获取 JVM 运行面和热点线程，再决定是否 trace 具体方法。", []ArthasPlannedCommand{
			{ID: "cmd-1", Command: "dashboard -n 1", Purpose: "获取 CPU、线程、GC 的整体概况"},
			{ID: "cmd-2", Command: "thread -n 5", Purpose: "定位当前最消耗 CPU 的线程和堆栈"},
		}, []string{"是否存在非 GC 热点线程", "热点线程栈顶是否指向业务方法"})
	case strings.Contains(lower, "启动") || strings.Contains(lower, "卡住") || strings.Contains(lower, "hang"):
		return s.planFromCommands("应用启动或运行卡住", "startup-stuck", "先用线程栈判断是否死锁、锁等待、远程依赖或 Spring 初始化阻塞。", []ArthasPlannedCommand{
			{ID: "cmd-1", Command: "thread --state BLOCKED -n 10", Purpose: "检查阻塞线程"},
			{ID: "cmd-2", Command: "thread -n 10", Purpose: "查看主要活动线程堆栈"},
		}, []string{"main 线程或业务线程是否阻塞", "是否存在死锁或远程依赖等待"})
	case strings.Contains(lower, "server.port") || strings.Contains(lower, "spring"):
		return s.planFromCommands("读取 Spring 运行时配置", "spring-env", "需要通过 Spring Environment 获取最终生效值，OGNL 默认需要确认。", []ArthasPlannedCommand{
			{ID: "cmd-1", Command: "ognl '#ctx=@org.springframework.web.context.ContextLoader@getCurrentWebApplicationContext(),#ctx.getEnvironment().getProperty(\"server.port\")'", Purpose: "读取运行时 Spring Environment 中的 server.port"},
		}, []string{"运行时最终生效配置值", "是否需要继续追溯配置来源"})
	default:
		return s.planFromCommands("JVM 健康检查", "jvm-health", "先执行低风险 JVM 体检命令，建立诊断上下文。", []ArthasPlannedCommand{
			{ID: "cmd-1", Command: "jvm", Purpose: "获取 JVM、内存、GC 和启动参数摘要"},
			{ID: "cmd-2", Command: "thread -n 3", Purpose: "获取当前最活跃线程"},
		}, []string{"JVM 参数和 GC 是否异常", "线程是否存在明显热点"})
	}
}

// BuildFallbackReport 基于已收集证据生成保守诊断报告。
func (s *ArthasAgentService) BuildFallbackReport(intent string, evidence []string) ArthasDiagnosisReport {
	conclusion := "已完成初步诊断，建议结合证据继续收敛根因。"
	intentLower := strings.ToLower(intent)
	possibleCauses := []string{"需要结合更多 Arthas 输出确认最终根因"}
	recommendations := []string{"继续按 Agent 计划执行下一步低风险命令，并保留输出作为诊断证据"}

	if strings.Contains(intentLower, "jvm") || strings.Contains(intentLower, "参数") || strings.Contains(intentLower, "启动参数") || strings.Contains(intentLower, "input-arguments") || strings.Contains(intentLower, "config") {
		conclusion = "本轮 JVM 参数查询已完成，启动参数和运行时配置可从 Arthas jvm 输出中的 INPUT-ARGUMENTS 等字段查看。"
		possibleCauses = []string{"参数可能来自启动脚本、环境变量、容器启动命令或 JVM 默认值", "部分容器运行时参数可能不会直接体现在应用配置文件中"}
		recommendations = []string{"重点核对 INPUT-ARGUMENTS、JVM FLAGS、系统属性和内存/GC 参数", "如需确认某个业务配置的最终生效值，继续查询对应 Spring Environment 或系统属性"}
	}
	if strings.Contains(intentLower, "cpu") || strings.Contains(intentLower, "负载") || strings.Contains(intentLower, "load") {
		conclusion = "当前问题更像 CPU 热点或忙循环，需要从热点线程堆栈收敛到具体方法。"
		possibleCauses = []string{"计算型热点", "正则或序列化开销", "日志格式化或循环调用"}
		recommendations = []string{"优先优化热点线程栈顶对应方法", "必要时使用限量 trace/watch 验证入参和耗时分布"}
	}
	if strings.Contains(intentLower, "风险") || strings.Contains(intentLower, "巡检") || strings.Contains(intentLower, "健康") || strings.Contains(intentLower, "状态") {
		conclusion = "本轮风险巡检已完成，当前 Arthas 输出未显示明确的高风险异常。"
		possibleCauses = []string{"当前采样窗口较短，低频问题可能未命中", "仍需结合 JVM、GC、线程和业务错误日志综合判断"}
		recommendations = []string{"继续查看 jvm 和 dashboard -n 1 的整体指标", "如存在业务慢请求，再对具体类方法使用限量 trace/watch 收敛"}
	}
	if strings.Contains(intentLower, "blocked") || strings.Contains(intentLower, "卡住") {
		conclusion = "当前问题可能与线程阻塞或启动阶段依赖等待有关。"
		possibleCauses = []string{"锁等待", "远程依赖初始化超时", "Spring Bean 初始化阻塞"}
		recommendations = []string{"检查阻塞线程持锁方", "将启动期远程依赖改为懒加载或异步初始化"}
	}

	return ArthasDiagnosisReport{
		Conclusion:      conclusion,
		Evidence:        evidence,
		PossibleCauses:  possibleCauses,
		Recommendations: recommendations,
	}
}

func parseArthasPlanContent(content string) (*ArthasPlan, error) {
	candidate := strings.TrimSpace(content)
	candidate = strings.TrimPrefix(candidate, "```json")
	candidate = strings.TrimPrefix(candidate, "```JSON")
	candidate = strings.TrimPrefix(candidate, "```")
	candidate = strings.TrimSuffix(candidate, "```")
	candidate = strings.TrimSpace(candidate)

	start := strings.Index(candidate, "{")
	end := strings.LastIndex(candidate, "}")
	if start >= 0 && end > start {
		candidate = candidate[start : end+1]
	}

	var payload struct {
		Intent          string                 `json:"intent"`
		Skill           string                 `json:"skill"`
		Reasoning       string                 `json:"reasoning"`
		Commands        []ArthasPlannedCommand `json:"commands"`
		ExpectedSignals json.RawMessage        `json:"expectedSignals"`
		Report          string                 `json:"report,omitempty"`
	}
	if err := json.Unmarshal([]byte(candidate), &payload); err != nil {
		return nil, err
	}
	expectedSignals, err := parseStringSlice(payload.ExpectedSignals)
	if err != nil {
		return nil, fmt.Errorf("解析 expectedSignals 失败: %w", err)
	}
	plan := ArthasPlan{
		Intent:          payload.Intent,
		Skill:           payload.Skill,
		Reasoning:       payload.Reasoning,
		Commands:        payload.Commands,
		ExpectedSignals: expectedSignals,
		Report:          payload.Report,
	}
	return &plan, nil
}

func parseDiagnosisReportContent(content string) (*ArthasDiagnosisReport, error) {
	candidate := extractJSONContent(content)
	var payload struct {
		Conclusion      string          `json:"conclusion"`
		Evidence        json.RawMessage `json:"evidence"`
		PossibleCauses  json.RawMessage `json:"possibleCauses"`
		Recommendations json.RawMessage `json:"recommendations"`
	}
	if err := json.Unmarshal([]byte(candidate), &payload); err != nil {
		return nil, err
	}
	evidence, err := parseStringSlice(payload.Evidence)
	if err != nil {
		return nil, fmt.Errorf("解析 evidence 失败: %w", err)
	}
	possibleCauses, err := parseStringSlice(payload.PossibleCauses)
	if err != nil {
		return nil, fmt.Errorf("解析 possibleCauses 失败: %w", err)
	}
	recommendations, err := parseStringSlice(payload.Recommendations)
	if err != nil {
		return nil, fmt.Errorf("解析 recommendations 失败: %w", err)
	}
	return &ArthasDiagnosisReport{
		Conclusion:      payload.Conclusion,
		Evidence:        evidence,
		PossibleCauses:  possibleCauses,
		Recommendations: recommendations,
	}, nil
}

func extractJSONContent(content string) string {
	candidate := strings.TrimSpace(content)
	candidate = strings.TrimPrefix(candidate, "```json")
	candidate = strings.TrimPrefix(candidate, "```JSON")
	candidate = strings.TrimPrefix(candidate, "```")
	candidate = strings.TrimSuffix(candidate, "```")
	candidate = strings.TrimSpace(candidate)

	start := strings.Index(candidate, "{")
	end := strings.LastIndex(candidate, "}")
	if start >= 0 && end > start {
		candidate = candidate[start : end+1]
	}
	return candidate
}

func parseStringSlice(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err == nil {
		return values, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	return []string{value}, nil
}

func (s *ArthasAgentService) planFromCommands(intent, skill, reasoning string, commands []ArthasPlannedCommand, expectedSignals []string) ArthasPlan {
	plan := ArthasPlan{
		Intent:          intent,
		Skill:           skill,
		Reasoning:       reasoning,
		Commands:        commands,
		ExpectedSignals: expectedSignals,
	}
	s.applyPolicy(&plan)
	return plan
}

func (s *ArthasAgentService) applyPolicy(plan *ArthasPlan) {
	for i := range plan.Commands {
		if strings.TrimSpace(plan.Commands[i].ID) == "" {
			plan.Commands[i].ID = fmt.Sprintf("cmd-%d", i+1)
		}
		decision := s.policy.Classify(plan.Commands[i].Command)
		plan.Commands[i].Risk = decision.Risk
		plan.Commands[i].RequiresConfirmation = decision.RequiresConfirmation
	}
}

func arthasAgentSystemPrompt() string {
	return `你是 KubePolaris 的 Arthas Agent。只输出 JSON，不要输出 Markdown。JSON 字段必须是 intent、skill、reasoning、commands、expectedSignals。commands 中每项包含 id、command、purpose。优先低风险只读命令；watch/trace 必须明确类名、方法名和 -n 限量；ognl/vmtool/redefine/retransform/dump/heapdump 必须等待用户确认。`
}

func arthasReportSystemPrompt() string {
	return `你是 KubePolaris 的 Arthas 诊断报告助手，诊断对象运行在 Kubernetes Pod 容器内。只输出 JSON，不要输出 Markdown。JSON 字段必须是 conclusion、evidence、possibleCauses、recommendations，后 3 个字段必须尽量使用数组。必须基于用户问题、诊断计划和真实 Arthas 输出总结，不允许编造未在输出中出现的事实；如果证据不足，要明确说明未观察到或需要继续采样。注意指标语义：Arthas jvm 的 OPERATING-SYSTEM 段来自 JVM 视角，LOAD-AVERAGE 通常是宿主机/节点级系统负载，不是 Pod CPU 使用率；PROCESSORS-COUNT 可能受容器 CPU 配额影响。除非有 dashboard、thread、top、cgroup 或 Kubernetes metrics 证据，否则不能把 LOAD-AVERAGE 直接判定为 Pod CPU 瓶颈。`
}

func buildArthasAgentUserPrompt(prompt string, evidence []string) string {
	payload := map[string]interface{}{
		"user_prompt": prompt,
		"evidence":    evidence,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("user_prompt=%s", prompt)
	}
	return string(data)
}

func buildArthasReportUserPrompt(prompt string, plan *ArthasPlan, evidence []string) string {
	payload := map[string]interface{}{
		"user_prompt":        prompt,
		"plan":               plan,
		"evidence":           evidence,
		"kubernetes_context": "诊断目标是 Kubernetes Pod 内的 Java 进程，Arthas 命令在容器中 attach JVM，但部分 JVM/OS 指标可能反映节点或 JVM 视角，不等同于 Pod 资源指标。",
		"metric_semantics": []string{
			"Arthas jvm OPERATING-SYSTEM.LOAD-AVERAGE 通常表示宿主机/节点级 load average，不是 Pod CPU 使用率。",
			"PROCESSORS-COUNT 可能是 JVM 感知到的可用处理器数量，可能受容器 CPU quota/cgroup 限制影响。",
			"判断 Pod CPU 瓶颈需要 dashboard/thread/top、容器 cgroup 或 Kubernetes metrics 证据；只有 jvm LOAD-AVERAGE 时只能提示节点负载观察，不应下结论为 Pod CPU 异常。",
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("user_prompt=%s\nevidence=%s", prompt, strings.Join(evidence, "\n\n"))
	}
	return string(data)
}
