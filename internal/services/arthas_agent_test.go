package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clay-wangzhi/KubePolaris/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestBuildFallbackArthasPlanForCPUHigh(t *testing.T) {
	service := NewArthasAgentService(nil)

	plan := service.BuildFallbackPlan("现在 CPU 使用率很高，帮我查查")

	if plan.Intent == "" {
		t.Fatal("expected intent")
	}
	if len(plan.Commands) == 0 {
		t.Fatal("expected commands")
	}
	if plan.Commands[0].Command != "dashboard -n 1" {
		t.Fatalf("expected dashboard first, got %s", plan.Commands[0].Command)
	}
	if plan.Commands[0].Risk != ArthasRiskLow {
		t.Fatalf("expected low risk, got %s", plan.Commands[0].Risk)
	}
}

func TestBuildFallbackArthasPlanForHighLoad(t *testing.T) {
	service := NewArthasAgentService(nil)

	plan := service.BuildFallbackPlan("当前负载高吗")

	if plan.Skill != "cpu-high" {
		t.Fatalf("expected cpu-high skill for high load prompt, got %s", plan.Skill)
	}
}

func TestParseArthasPlanContentAcceptsMarkdownJSONFence(t *testing.T) {
	content := "```json\n{\"intent\":\"负载巡检\",\"skill\":\"cpu-high\",\"reasoning\":\"检查负载\",\"commands\":[{\"id\":\"cmd-1\",\"command\":\"thread -n 3\",\"purpose\":\"查看热点线程\"}],\"expectedSignals\":[\"线程 CPU\"]}\n```"

	plan, err := parseArthasPlanContent(content)

	if err != nil {
		t.Fatalf("expected fenced JSON to parse: %v", err)
	}
	if plan.Skill != "cpu-high" {
		t.Fatalf("expected parsed skill cpu-high, got %s", plan.Skill)
	}
}

func TestParseArthasPlanContentAcceptsStringExpectedSignals(t *testing.T) {
	content := "{\"intent\":\"负载巡检\",\"skill\":\"cpu-high\",\"reasoning\":\"检查负载\",\"commands\":[{\"id\":\"cmd-1\",\"command\":\"thread -n 3\",\"purpose\":\"查看热点线程\"}],\"expectedSignals\":\"线程 CPU 是否异常\"}"

	plan, err := parseArthasPlanContent(content)

	if err != nil {
		t.Fatalf("expected string expectedSignals to parse: %v", err)
	}
	if len(plan.ExpectedSignals) != 1 || plan.ExpectedSignals[0] != "线程 CPU 是否异常" {
		t.Fatalf("expected single expected signal, got %#v", plan.ExpectedSignals)
	}
}

func TestApplyPolicyAssignsMissingCommandIDs(t *testing.T) {
	service := NewArthasAgentService(nil)
	plan := &ArthasPlan{
		Commands: []ArthasPlannedCommand{
			{Command: "gc", Purpose: "查看 GC"},
			{Command: "vmoption", Purpose: "查看 JVM 参数"},
		},
	}

	service.applyPolicy(plan)

	if plan.Commands[0].ID != "cmd-1" || plan.Commands[1].ID != "cmd-2" {
		t.Fatalf("expected generated command ids, got %#v", plan.Commands)
	}
}

func TestBuildFallbackArthasPlanForSpringPort(t *testing.T) {
	service := NewArthasAgentService(nil)

	plan := service.BuildFallbackPlan("获取 spring 配置 server.port 值")

	if plan.Skill != "spring-env" {
		t.Fatalf("expected spring-env skill, got %s", plan.Skill)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected one command, got %d", len(plan.Commands))
	}
	if plan.Commands[0].Risk != ArthasRiskHigh {
		t.Fatalf("spring OGNL should require confirmation, got %s", plan.Commands[0].Risk)
	}
}

func TestBuildFallbackReportIncludesEvidenceAndAdvice(t *testing.T) {
	service := NewArthasAgentService(nil)

	report := service.BuildFallbackReport("CPU 使用率高", []string{"thread -n 5 显示 busy-thread 栈顶为 RegexService.match"})

	if report.Conclusion == "" {
		t.Fatal("expected conclusion")
	}
	if len(report.Evidence) != 1 {
		t.Fatalf("expected evidence, got %d", len(report.Evidence))
	}
	if len(report.Recommendations) == 0 {
		t.Fatal("expected recommendations")
	}
}

func TestBuildFallbackReportForJVMConfigDoesNotUseCPUConclusion(t *testing.T) {
	service := NewArthasAgentService(nil)

	report := service.BuildFallbackReport("JVM 参数如何配置的 query_jvm_config", []string{"jvm 输出包含 INPUT-ARGUMENTS 与 CPU COUNT"})

	if containsReportText(report.Conclusion, "CPU 热点") {
		t.Fatalf("expected JVM config conclusion, got %s", report.Conclusion)
	}
	if !containsReportText(report.Conclusion, "JVM 参数") {
		t.Fatalf("expected JVM config conclusion, got %s", report.Conclusion)
	}
}

func TestBuildFallbackReportForRiskInspectionHasCompletionConclusion(t *testing.T) {
	service := NewArthasAgentService(nil)

	report := service.BuildFallbackReport("进行风险巡检，查看当前状态是否正常", []string{"thread -n 3 显示线程 CPU 使用率较低"})

	if !containsReportText(report.Conclusion, "已完成") {
		t.Fatalf("expected completion conclusion, got %s", report.Conclusion)
	}
}

func TestParseDiagnosisReportContentAcceptsMarkdownJSONFence(t *testing.T) {
	content := "```json\n{\"conclusion\":\"JVM 最大堆为 1024m\",\"evidence\":[\"-Xmx1024m\"],\"possibleCauses\":\"启动脚本配置\",\"recommendations\":\"继续核对容器启动参数\"}\n```"

	report, err := parseDiagnosisReportContent(content)

	if err != nil {
		t.Fatalf("expected report JSON to parse: %v", err)
	}
	if report.Conclusion != "JVM 最大堆为 1024m" {
		t.Fatalf("unexpected conclusion: %s", report.Conclusion)
	}
	if len(report.PossibleCauses) != 1 || report.PossibleCauses[0] != "启动脚本配置" {
		t.Fatalf("expected string possibleCauses to become slice, got %#v", report.PossibleCauses)
	}
}

func TestBuildReportUsesLLMResponse(t *testing.T) {
	var requestMessages []ChatMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requestMessages = body.Messages
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"conclusion\":\"模型基于输出判断 Xmx 为 1024m\",\"evidence\":[\"INPUT-ARGUMENTS: -Xmx1024m\"],\"possibleCauses\":[\"启动参数显式指定\"],\"recommendations\":[\"按容量评估是否调整堆大小\"]}"}}]}`))
	}))
	defer server.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.AIConfig{}); err != nil {
		t.Fatalf("migrate ai config: %v", err)
	}
	if err := db.Create(&models.AIConfig{
		Provider: "openai",
		Endpoint: server.URL,
		APIKey:   "test-key",
		Model:    "test-model",
		Enabled:  true,
	}).Error; err != nil {
		t.Fatalf("create ai config: %v", err)
	}

	service := NewArthasAgentService(NewAIConfigService(db))
	report := service.BuildReport(context.Background(), "jvm 参数配置", &ArthasPlan{Intent: "查看JVM参数配置"}, []string{"jvm\nINPUT-ARGUMENTS: -Xmx1024m"})

	if report.Conclusion != "模型基于输出判断 Xmx 为 1024m" {
		t.Fatalf("expected LLM report, got %s", report.Conclusion)
	}
	if len(requestMessages) != 2 {
		t.Fatalf("expected system and user messages, got %d", len(requestMessages))
	}
	if !containsReportText(requestMessages[1].Content, "INPUT-ARGUMENTS: -Xmx1024m") {
		t.Fatalf("expected evidence in LLM prompt, got %s", requestMessages[1].Content)
	}
}

func TestArthasReportPromptIncludesKubernetesMetricSemantics(t *testing.T) {
	prompt := buildArthasReportUserPrompt("jvm 参数配置", &ArthasPlan{Intent: "查看JVM参数配置"}, []string{"OPERATING-SYSTEM\nLOAD-AVERAGE 6.14\nPROCESSORS-COUNT 1"})
	systemPrompt := arthasReportSystemPrompt()

	for _, expected := range []string{"Kubernetes Pod", "LOAD-AVERAGE", "Pod CPU"} {
		if !containsReportText(prompt, expected) && !containsReportText(systemPrompt, expected) {
			t.Fatalf("expected report prompt to include %q semantics, user prompt=%s, system prompt=%s", expected, prompt, systemPrompt)
		}
	}
}

func containsReportText(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
