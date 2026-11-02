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
	// L3-S6 §12.1/§13.1 and L4-workflow-convergence §"S6／S9 当前工作区":
	// the TR-006 denominator is the canonical Assignment Record path. The
	// legacy completion-report envelope plus a durable worktree checkpoint
	// is not a current legal delivery, so the frozen/legacy projection is
	// gone and the gate stays not ready, naming the canonical
	// responsibility each batch TASK still lacks.
	if result.QualityGate.TransitionCommitted {
		t.Fatalf("legacy completion facts must not open the TR-006 exit: status=%s err=%q missing=%v",
			result.QualityGate.Status, result.Error, result.QualityGate.Missing)
	}
	for _, taskID := range []string{"TASK-039-01", "TASK-039-02"} {
		want := "builder_responsibility:" + taskID
		found := false
		for _, missing := range result.QualityGate.Missing {
			if missing == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("gate must name the missing canonical responsibility %q: status=%s missing=%v",
				want, result.QualityGate.Status, result.QualityGate.Missing)
		}
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
