package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

// Promoted from the independent handoff review counterexample. Check the
// assembled entry, since an isolated dispatch projection cannot expose this bug.
func TestHandoffReviewMainProgressMustNotRepeatSamePreparation(t *testing.T) {
	root, _, _, _, _ := s9HandoffMissingCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	before := repairDispatchRuntimeBytes(t, root)
	request := policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root, Context: context.Background()}
	for _, got := range []string{
		currentQuestionnaireLifecycleContextFromSnapshot(root, request, snap),
		currentQuestionnaireLifecycleContext(root, request),
	} {
		for _, want := range []string{
			"S9 execution preparation uses the same Builder division answer:",
			"S9 local evidence source gap",
		} {
			if count := strings.Count(got, want); count != 1 {
				t.Errorf("Main entry must display original preparation/concrete failure once: %q count=%d; context=%s", want, count, got)
			}
		}
		if !strings.Contains(got, "missing-handoff.txt") {
			t.Errorf("Main lost the exact failed source: %s", got)
		}
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestS9EntryAssemblyPreservesParallelResponsibilitiesAndWorker(t *testing.T) {
	t.Run("Builder and independent verifier", func(t *testing.T) {
		root, snap, plan, _ := repairDispatchFixture(t)
		snap.State["schema_version"] = "1.2.0"
		mapFieldCLI(snap.State, "configuration")["optimization"] = map[string]any{"packet": true}
		before := repairDispatchRuntimeBytes(t, root)
		got := currentQuestionnaireLifecycleContextFromSnapshot(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root}, snap)
		for _, want := range []string{"S9 Main Builder division:", "S9 Main independent verifier division:"} {
			if count := strings.Count(got, want); count != 1 {
				t.Errorf("parallel responsibility %q count=%d: %s", want, count, got)
			}
		}
		for _, assignment := range plan.Assignments {
			// Both forms intentionally display their own row for the same anchor.
			// Deduplicating equal strings would erase a real responsibility here.
			want := repairDispatchAssignmentSummary(plan, assignment, "")
			if count := strings.Count(got, want); count != 2 {
				t.Errorf("Builder/verifier anchor %s count=%d, want two distinct responsibilities: %s", assignment.AssignmentID, count, got)
			}
		}
		repairDispatchAssertUnchanged(t, root, before)
	})
	t.Run("original Worker", func(t *testing.T) {
		root, worker, plan, _, result := s9HandoffMissingCandidate(t)
		before := repairDispatchRuntimeBytes(t, root)
		got := currentQuestionnaireLifecycleContext(root, policy.Input{Event: "PreToolUse", AgentID: plan.Binding.OwnerAgentID, CWD: root})
		for _, want := range []string{plan.Binding.OwnerAgentID, plan.Binding.RepairAssignmentID, filepath.Join(worker, result.AnswerPath)} {
			if !strings.Contains(got, want) {
				t.Errorf("Worker lost original responsibility %q: %s", want, got)
			}
		}
		if strings.Contains(got, "S9 execution preparation uses the same Builder division answer:") || strings.Contains(got, "S9 Main independent verifier division:") {
			t.Errorf("Worker received Main dispatch context: %s", got)
		}
		repairDispatchAssertUnchanged(t, root, before)
	})
}

func TestS9EntryAssemblyPreservesNewPlanFailureAndOriginalStopFailure(t *testing.T) {
	root, _, _, _, _ := s9HandoffMissingCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	path := resolveRootPath(root, stringValue(repairPointerCLI(snap.State)["plan_ref"]))
	if err := os.WriteFile(path, []byte("x-new-plan-read-failure"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	got := currentQuestionnaireLifecycleContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root})
	for _, want := range []string{
		"cannot read the original Plan:",
		"invalid character 'x' looking for beginning of value",
		"S9 local evidence source gap",
	} {
		if count := strings.Count(got, want); count != 1 {
			t.Errorf("new/current and original failures must each survive once: %q count=%d: %s", want, count, got)
		}
	}
	if !strings.Contains(got, "missing-handoff.txt") {
		t.Errorf("new Plan read failure erased the exact original failed source: %s", got)
	}
	repairDispatchAssertUnchanged(t, root, before)
}
