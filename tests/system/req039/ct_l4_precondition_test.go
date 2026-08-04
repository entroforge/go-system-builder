// ct_l4_precondition_test.go — honest L4 upgrades for single-step CT/AC
// contracts where evidence precondition + Hook CLI produce the measured
// transition (audit §3.2 rule 3: compressed seed OK for cursor only).

package req039_test

import (
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestCT03901_DesignGateCommitsPTRPLAN01System covers SYNC-039 §12 CT-039-01
// and REQ-039 AC-001: satisfied design gate → PTR-PLAN-01 → planning.contracts.
func TestCT03901_DesignGateCommitsPTRPLAN01System(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "design", 1)
	req039fixtures.SeedPlanningDesignComplete(t, root, state)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-ct-039-01-sys", "Edit", map[string]any{
		"file_path": "docs/design/architecture/ARCHITECTURE-039-loop-control-plane.md",
	})
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-01 must not use manual transition CLI")
	}
	_, qg := parseEnv(t, stdout)
	if !req039fixtures.TransitionCommitted(qg) {
		t.Fatalf("CT-039-01 must commit PTR-PLAN-01, qg=%v stdout=%s", qg, stdout)
	}
	if cand := req039fixtures.CandidateTransition(qg); cand != "PTR-PLAN-01" {
		t.Fatalf("CT-039-01 candidate want PTR-PLAN-01, got %q", cand)
	}
	after := req039fixtures.ReadState(t, root)
	req039fixtures.AssertLifecycle(t, after, "planning", "contracts")
	req039fixtures.AssertLastTransition(t, after, "PTR-PLAN-01")
}

// TestAC001_DesignGateAdvancesToContractsSystem is the AC-001 twin of CT-039-01.
func TestAC001_DesignGateAdvancesToContractsSystem(t *testing.T) {
	TestCT03901_DesignGateCommitsPTRPLAN01System(t)
}

// TestCT03911_DualPassDocumentVerificationSystem covers CT-039-11:
// dual PASS DV + PreToolUse → TR-003 → building.
func TestCT03911_DualPassDocumentVerificationSystem(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "design", 18)
	req039fixtures.SeedDocumentPassS5(t, root, state, "dv-spec-1", "dv-task-2")
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-ct-039-11-sys", "Edit", map[string]any{
		"file_path": "docs/contracts/BE-039-loop-controller.md",
	})
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-11 must not use manual transition CLI")
	}
	if !strings.Contains(stdout, "TR-003") && !strings.Contains(stdout, "GATE-DOCUMENT-PASS") {
		t.Fatalf("CT-039-11 must surface TR-003 / GATE-DOCUMENT-PASS, got %s", stdout)
	}
	after := req039fixtures.ReadState(t, root)
	lc, _ := req039fixtures.Lifecycle(after)
	if lc != "building" {
		t.Fatalf("CT-039-11 TR-003 must advance to building, got %q stdout=%s", lc, stdout)
	}
	req039fixtures.AssertLastTransition(t, after, "TR-003")
}

// TestCT03912_BuilderCompleteCommitsTR006System covers CT-039-12:
// builder complete + PreToolUse → TR-006 → verification.delivery.
func TestCT03912_BuilderCompleteCommitsTR006System(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	// Use BaseState (same as CLI CT-039-12) so locked TASK/docs + revision
	// match the TR-006 angle guards; systemPlanningState alone trips
	// LOOP_TRANSITION_GUARD on incomplete planning docs.
	state := req039fixtures.BaseState(t, root, "building", "", 19)
	req039fixtures.SeedBuilderBatchReady(t, root, state)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-ct-039-12-sys", "Bash", map[string]any{
		"command": "go test ./...",
	})
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-12 forbids manual transition CLI")
	}
	after := req039fixtures.ReadState(t, root)
	lc, ph := req039fixtures.Lifecycle(after)
	if lc != "verification" || ph != "delivery" {
		t.Fatalf("CT-039-12 TR-006 must land verification.delivery, got %q/%q stdout=%s", lc, ph, stdout)
	}
	req039fixtures.AssertLastTransition(t, after, "TR-006")
}

// TestCT03924_SameAgentDualDVBlocksTR003System covers CT-039-24:
// same agent dual DV labels → GATE-DOCUMENT-PASS not_ready/unknown, no TR-003.
func TestCT03924_SameAgentDualDVBlocksTR003System(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "design", 18)
	req039fixtures.SeedDualDVSameAgent(t, root, state)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-ct-039-24-sys", "Edit", map[string]any{
		"file_path": "docs/contracts/BE-039-loop-controller.md",
	})
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-24 must not use manual transition CLI")
	}
	_, qg := parseEnv(t, stdout)
	status, _ := qg["status"].(string)
	if status != "not_ready" && status != "unknown" {
		t.Fatalf("CT-039-24 want not_ready/unknown, got %q qg=%v", status, qg)
	}
	if req039fixtures.TransitionCommitted(qg) {
		t.Fatal("CT-039-24 must not commit TR-003 when independent reviewers missing")
	}
	after := req039fixtures.ReadState(t, root)
	lc, _ := req039fixtures.Lifecycle(after)
	if lc != "document_verification" {
		t.Fatalf("CT-039-24 must remain at document_verification, got %q", lc)
	}
}

// TestCT03908_SessionStartAfterCompactSameMilestoneSystem covers CT-039-08:
// SessionStart after Compact surfaces the same persisted milestone.
func TestCT03908_SessionStartAfterCompactSameMilestoneSystem(t *testing.T) {
	TestCompact_Recovery_SameMilestone(t)
}

// TestAC008_CompactRecoversMilestoneSystem is the AC-008 twin of CT-039-08.
func TestAC008_CompactRecoversMilestoneSystem(t *testing.T) {
	TestCompact_Recovery_SameMilestone(t)
}
