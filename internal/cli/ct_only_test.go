package cli_test

import (
	"context"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestCT03912ControllerOnly(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "building", "", 19)
	req039fixtures.SeedBuilderBatchReady(t, root, state)
	req039fixtures.WriteState(t, root, state)
	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root: root, Event: "PreToolUse", ToolName: "Bash",
		ToolInput: map[string]any{"command": "go test ./..."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.QualityGate.TransitionCommitted {
		t.Fatalf("status=%s err=%q missing=%v", result.QualityGate.Status, result.Error, result.QualityGate.Missing)
	}
}

// L3-S7: with the final required Claim unconsumed, neither S7 exit gate is
// satisfied and the controller must not commit anything.
func TestCT03921ControllerOnly(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "verification", "running", 40)
	req039fixtures.SeedReviewPlanRound(t, root, state)
	req039fixtures.SeedReviewResultPass(t, root, state, "assignment-dv-1")
	req039fixtures.SeedReviewResultPass(t, root, state, "assignment-e2e-1")
	req039fixtures.WriteState(t, root, state)
	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root: root, Event: "PreToolUse", ToolName: "Bash",
		ToolInput: map[string]any{"command": "go test ./..."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityGate.TransitionCommitted {
		t.Fatalf("an open round must not commit: candidate=%s status=%s missing=%v",
			result.QualityGate.CandidateTransition, result.QualityGate.Status, result.QualityGate.Missing)
	}
}

func TestCT03922ControllerOnly(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "bug_report_review", 35)
	req039fixtures.SeedBugReportsRejected(t, root, state)
	req039fixtures.WriteState(t, root, state)
	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root: root, Event: "PreToolUse", ToolName: "Edit",
		ToolInput: map[string]any{"file_path": "docs/reports/bugs/BUG-039-15.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityGate.CandidateTransition != "PTR-BUG-03" || !result.QualityGate.TransitionCommitted {
		t.Fatalf("candidate=%s committed=%v status=%s", result.QualityGate.CandidateTransition, result.QualityGate.TransitionCommitted, result.QualityGate.Status)
	}
}
