package handlers

import (
	"testing"

	"github.com/clay-wangzhi/KubePolaris/internal/services"
)

func TestShouldSendPromptDoneForCommandlessPlan(t *testing.T) {
	plan := &services.ArthasPlan{Intent: "自我介绍"}

	if !shouldSendPromptDone(plan, 0, 0, 0) {
		t.Fatal("expected commandless plan to send done event")
	}
}

func TestShouldSendPromptDoneAfterEvidenceCollected(t *testing.T) {
	plan := &services.ArthasPlan{
		Commands: []services.ArthasPlannedCommand{{Command: "jvm"}},
	}

	if !shouldSendPromptDone(plan, 0, 1, 0) {
		t.Fatal("expected plan with collected evidence to send done event")
	}
}

func TestShouldSendPromptDoneWaitsForCommandResult(t *testing.T) {
	plan := &services.ArthasPlan{
		Commands: []services.ArthasPlannedCommand{{Command: "jvm"}},
	}

	if shouldSendPromptDone(plan, 0, 0, 0) {
		t.Fatal("expected plan with commands and no evidence to wait")
	}
}

func TestShouldSendPromptDoneWaitsForPendingConfirmations(t *testing.T) {
	plan := &services.ArthasPlan{
		Commands: []services.ArthasPlannedCommand{
			{Command: "jvm"},
			{Command: "vmoption"},
		},
	}

	if shouldSendPromptDone(plan, 0, 1, 1) {
		t.Fatal("expected plan with pending confirmations to wait before done")
	}
}

func TestDiagnosisContextWaitsUntilPendingCommandsComplete(t *testing.T) {
	plan := &services.ArthasPlan{
		Commands: []services.ArthasPlannedCommand{
			{ID: "cmd-1", Command: "jvm"},
			{ID: "cmd-2", Command: "vmoption"},
		},
	}
	state := newArthasDiagnosisState()
	state.start("诊断 JVM", plan, nil)
	state.addEvidence("jvm output")
	state.addPending(plan.Commands[1])

	if state.shouldBuildReport() {
		t.Fatal("expected report to wait while command is pending")
	}

	state.resolvePending("cmd-2", "vmoption")
	state.addEvidence("vmoption output")

	if !state.shouldBuildReport() {
		t.Fatal("expected report after pending command resolves")
	}
}
