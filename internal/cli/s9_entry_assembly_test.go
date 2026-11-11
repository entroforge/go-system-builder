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
	root, worker, plan, _, _ := s9HandoffMissingCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	before := repairDispatchRuntimeBytes(t, root)
	request := policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root, Context: context.Background()}
	for _, got := range []string{
		currentQuestionnaireLifecycleContextFromSnapshot(root, request, snap),
		currentQuestionnaireLifecycleContext(root, request),
	} {
		for _, want := range []string{
			"S9 execution preparation uses the same Builder division answer:",
			// The original Result is still not submitted, so the original
			// consumer reports that exact pending boundary (L3-S9 §6.2). The
			// removed local-evidence source gap is not a reception condition.
			"Result is not submitted or is unfinished",
		} {
			if count := strings.Count(got, want); count != 1 {
				t.Errorf("Main entry must display original preparation/concrete failure once: %q count=%d; context=%s", want, count, got)
			}
		}
		if !strings.Contains(got, filepath.Join(worker, nativeS9TestResultPath(t, root, plan))) {
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
	root, worker, plan, _, _ := s9HandoffMissingCandidate(t)
	snap := repairDispatchSnapshot(t, root)
	path := resolveRootPath(root, stringValue(repairPointerCLI(snap.State)["plan_ref"]))
	if err := os.WriteFile(path, []byte("x-new-plan-read-failure"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	got := currentQuestionnaireLifecycleContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root})
	for _, want := range []string{
		// The S9 repairing phase owns the Builder division only. The
		// independent verifier division is not current there
		// (repairVerifierDispatchCurrent admits planning, reproducing and
		// targeted_reverification), so the read failure Main must report is
		// the execution preparation one. The verifier division's own "could
		// not read the original Plan:" wording is structurally unreachable in
		// this state and must not be asserted.
		"cannot read the original Plan:",
		"invalid character 'x' looking for beginning of value",
		"Result is not submitted or is unfinished",
	} {
		// Every original division that read the same corrupted Plan carries
		// the concrete read error, so each failure identity must survive at
		// least once instead of exactly once.
		if count := strings.Count(got, want); count < 1 {
			t.Errorf("new/current and original failures must each survive: %q count=%d: %s", want, count, got)
		}
	}
	if !strings.Contains(got, filepath.Join(worker, nativeS9TestResultPath(t, root, plan))) {
		t.Errorf("new Plan read failure erased the exact original failed source: %s", got)
	}
	// The unreadable Plan must not be masked by the preparation success
	// path: Main must not present the dispatch answer as prepared.
	if strings.Contains(got, "S9 execution preparation uses the same Builder division answer:") {
		t.Errorf("unreadable original Plan was masked by the preparation success path: %s", got)
	}
	// The independent verifier division is not current in the repairing
	// phase, so Main must not present a division dispatch context it does
	// not own. Both the prepared-division wording and the failed-division
	// wording name that division, and neither may appear here.
	if strings.Contains(got, "independent verifier division") {
		t.Errorf("Main presented a division that is not current in the repairing phase: %s", got)
	}
	// The exact questionnaire Main is supposed to prepare stays named next
	// to the read failure, so the reader knows which file is blocked.
	if paths := repairDispatchPaths(root, snap.State); len(paths) == 1 && !strings.Contains(got, paths[0]) {
		t.Errorf("read failure lost the exact dispatch answer path: %s", got)
	}
	// Nothing rewrote the unreadable Plan behind Main's back.
	if data, err := os.ReadFile(path); err != nil || string(data) != "x-new-plan-read-failure" {
		t.Errorf("the unreadable original Plan was rewritten: %v %q", err, string(data))
	}
	repairDispatchAssertUnchanged(t, root, before)
}
