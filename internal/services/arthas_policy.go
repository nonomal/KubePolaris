package services

import "strings"

// ArthasRisk 表示 Arthas 命令的执行风险等级。
type ArthasRisk string

const (
	ArthasRiskLow    ArthasRisk = "low"
	ArthasRiskMedium ArthasRisk = "medium"
	ArthasRiskHigh   ArthasRisk = "high"
)

// ArthasCommandDecision 是命令执行前的策略判断结果。
type ArthasCommandDecision struct {
	Risk                 ArthasRisk `json:"risk"`
	AutoExecutable       bool       `json:"autoExecutable"`
	RequiresConfirmation bool       `json:"requiresConfirmation"`
	Reason               string     `json:"reason"`
	MaxOutputBytes       int64      `json:"maxOutputBytes"`
}

// ArthasCommandPolicy 对 Arthas 命令做风险分类和自动执行决策。
type ArthasCommandPolicy struct {
	autoExecLowRisk bool
	maxOutputBytes  int64
}

// NewArthasCommandPolicy 创建 Arthas 命令策略。
func NewArthasCommandPolicy(autoExecLowRisk bool, maxOutputBytes int64) *ArthasCommandPolicy {
	if maxOutputBytes <= 0 {
		maxOutputBytes = 1024 * 1024
	}
	return &ArthasCommandPolicy{
		autoExecLowRisk: autoExecLowRisk,
		maxOutputBytes:  maxOutputBytes,
	}
}

// Classify 返回命令风险等级和执行策略。
func (p *ArthasCommandPolicy) Classify(command string) ArthasCommandDecision {
	normalized := normalizeArthasCommand(command)
	if normalized == "" {
		return p.highRisk("空命令需要确认")
	}

	name := firstArthasToken(normalized)
	if isHighRiskArthasCommand(name) {
		return p.highRisk("命令可能读取敏感数据、改变 JVM 状态或产生大量输出")
	}

	if name == "watch" || name == "trace" {
		if hasArthasLimit(normalized) && hasArthasAnchor(normalized) {
			return ArthasCommandDecision{
				Risk:                 ArthasRiskMedium,
				AutoExecutable:       false,
				RequiresConfirmation: true,
				Reason:               "已限定观察目标和次数，但仍可能增加运行时开销",
				MaxOutputBytes:       p.maxOutputBytes,
			}
		}
		return p.highRisk("watch/trace 需要明确类名、方法名和 -n 限量")
	}

	if isLowRiskArthasCommand(name) {
		return ArthasCommandDecision{
			Risk:                 ArthasRiskLow,
			AutoExecutable:       p.autoExecLowRisk,
			RequiresConfirmation: false,
			Reason:               "只读诊断命令，默认允许低风险自动执行",
			MaxOutputBytes:       p.maxOutputBytes,
		}
	}

	return ArthasCommandDecision{
		Risk:                 ArthasRiskMedium,
		AutoExecutable:       false,
		RequiresConfirmation: true,
		Reason:               "未在低风险白名单中，需确认后执行",
		MaxOutputBytes:       p.maxOutputBytes,
	}
}

func (p *ArthasCommandPolicy) highRisk(reason string) ArthasCommandDecision {
	return ArthasCommandDecision{
		Risk:                 ArthasRiskHigh,
		AutoExecutable:       false,
		RequiresConfirmation: true,
		Reason:               reason,
		MaxOutputBytes:       p.maxOutputBytes,
	}
}

func normalizeArthasCommand(command string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(command)), " ")
}

func firstArthasToken(command string) string {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(parts[0])
}

func isLowRiskArthasCommand(name string) bool {
	lowRisk := map[string]bool{
		"version":     true,
		"help":        true,
		"sysprop":     true,
		"sysenv":      true,
		"jvm":         true,
		"dashboard":   true,
		"thread":      true,
		"memory":      true,
		"classloader": true,
	}
	return lowRisk[name]
}

func isHighRiskArthasCommand(name string) bool {
	highRisk := map[string]bool{
		"ognl":        true,
		"vmtool":      true,
		"redefine":    true,
		"retransform": true,
		"dump":        true,
		"heapdump":    true,
	}
	return highRisk[name]
}

func hasArthasLimit(command string) bool {
	parts := strings.Fields(command)
	for i, part := range parts {
		if part == "-n" && i+1 < len(parts) {
			return true
		}
		if strings.HasPrefix(part, "-n") && len(part) > 2 {
			return true
		}
	}
	return false
}

func hasArthasAnchor(command string) bool {
	parts := strings.Fields(command)
	return len(parts) >= 3 && !strings.Contains(parts[1], "*") && !strings.Contains(parts[2], "*")
}
