package services

import "testing"

func TestClassifyArthasCommandAllowsLowRiskReadOnlyCommands(t *testing.T) {
	policy := NewArthasCommandPolicy(true, 4096)

	result := policy.Classify("thread -n 3")

	if result.Risk != ArthasRiskLow {
		t.Fatalf("expected low risk, got %s", result.Risk)
	}
	if !result.AutoExecutable {
		t.Fatal("expected low risk command to be auto executable")
	}
	if result.RequiresConfirmation {
		t.Fatal("low risk command should not require confirmation")
	}
}

func TestClassifyArthasCommandBlocksHighRiskCommandsFromAutoExecution(t *testing.T) {
	policy := NewArthasCommandPolicy(true, 4096)

	result := policy.Classify("heapdump /tmp/app.hprof")

	if result.Risk != ArthasRiskHigh {
		t.Fatalf("expected high risk, got %s", result.Risk)
	}
	if result.AutoExecutable {
		t.Fatal("high risk command must not be auto executable")
	}
	if !result.RequiresConfirmation {
		t.Fatal("high risk command should require confirmation")
	}
}

func TestClassifyArthasCommandRequiresLimitsForTraceAndWatch(t *testing.T) {
	policy := NewArthasCommandPolicy(true, 4096)

	result := policy.Classify("watch com.example.UserService getUser '{params, returnObj}'")

	if result.Risk != ArthasRiskHigh {
		t.Fatalf("unlimited watch should be high risk, got %s", result.Risk)
	}
	if !result.RequiresConfirmation {
		t.Fatal("unlimited watch should require confirmation")
	}

	limited := policy.Classify("watch com.example.UserService getUser '{params, returnObj}' -n 3")
	if limited.Risk != ArthasRiskMedium {
		t.Fatalf("limited watch should be medium risk, got %s", limited.Risk)
	}
	if limited.AutoExecutable {
		t.Fatal("medium risk command should not be auto executable by default")
	}
}

func TestClassifyArthasCommandRespectsDisabledAutoExecution(t *testing.T) {
	policy := NewArthasCommandPolicy(false, 4096)

	result := policy.Classify("jvm")

	if result.Risk != ArthasRiskLow {
		t.Fatalf("expected low risk, got %s", result.Risk)
	}
	if result.AutoExecutable {
		t.Fatal("auto execution disabled should prevent auto executable commands")
	}
}
