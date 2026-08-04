// ct_bug_correction_test.go — CT-039-14 L4 intent, CT-039-22, CT-039-23.

package req039_test

import (
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestCT03914_FindingToBugRepairFullReview exercises finding→BUG→repair→full review
// via Hook-only steps (SYNC-039 §12 CT-039-14). Conflict/unknown are failures
// unless a documented product blocker prevents commit.
func TestCT03914_FindingToBugRepairFullReview(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "design", 30)
	req039fixtures.SeedVerificationDelivery(t, root, state)
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	req039fixtures.EnsureStateRoot(state, root)
	writeSystemState(t, root, state)

	toolInput := map[string]any{"file_path": "internal/controller/cycle.go"}
	const bugID = "CT-039-14/TR-008"

	// TR-008: blocking finding → bug_resolution.investigation
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteBlockingFindingEvidence(t, root, state, "delivery-verifier-1", "Delivery Verifier")
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct14-tr008", "Edit", toolInput,
		"TR-008", "bug_resolution", "investigation", bugID)

	// PTR-BUG-01 → bug_report_review
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteRootCauseComplete(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct14-ptr-bug-01", "Edit", toolInput,
		"PTR-BUG-01", "bug_resolution", "bug_report_review", bugID)

	// PTR-BUG-02 → repair_readback
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteBugBatchAccepted(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct14-ptr-bug-02", "Edit", toolInput,
		"PTR-BUG-02", "bug_resolution", "repair_readback", bugID)

	// PTR-BUG-04 → fixing
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteRepairActivationApproved(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct14-ptr-bug-04", "Edit", toolInput,
		"PTR-BUG-04", "bug_resolution", "fixing", bugID)

	// PTR-BUG-05 → targeted_reverification
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteRepairBatchReported(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct14-ptr-bug-05", "Edit", toolInput,
		"PTR-BUG-05", "bug_resolution", "targeted_reverification", bugID)

	// PTR-BUG-06 → ready_for_full_review
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteTargetedReverificationPass(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct14-ptr-bug-06", "Edit", toolInput,
		"PTR-BUG-06", "bug_resolution", "ready_for_full_review", bugID)

	// TR-012 → verification.delivery (full review return)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct14-tr012", "Edit", toolInput,
		"TR-012", "verification", "delivery", bugID)

	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-14 manual transition count=%d, want 0", runner.ManualTransitionCalls)
	}
}

// TestCT03922_BugReportRejectedSystem drives PTR-BUG-03 via Hook.
func TestCT03922_BugReportRejectedSystem(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "design", 35)
	req039fixtures.SeedBugReportsRejected(t, root, state)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-ct-039-22-sys", "Edit", map[string]any{
		"file_path": "docs/reports/bugs/BUG-039-15.md",
	})
	code, _, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	after := req039fixtures.ReadState(t, root)
	_, ph := req039fixtures.Lifecycle(after)
	if ph != "investigation" {
		t.Fatalf("PTR-BUG-03 must return to investigation, got %q", ph)
	}
}

// TestCT03923_TargetedRecheckFailedSystem drives PTR-BUG-07 via Hook.
func TestCT03923_TargetedRecheckFailedSystem(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "design", 36)
	req039fixtures.SeedTargetedReverificationFail(t, root, state)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-ct-039-23-sys", "Edit", map[string]any{
		"file_path": "internal/controller/cycle.go",
	})
	code, _, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	after := req039fixtures.ReadState(t, root)
	_, ph := req039fixtures.Lifecycle(after)
	if ph != "investigation" {
		t.Fatalf("PTR-BUG-07 must return to investigation, got %q", ph)
	}
}
