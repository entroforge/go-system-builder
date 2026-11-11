// ct_l4_precondition_test.go — honest L4 upgrades for single-step CT/AC
// contracts where evidence precondition + Hook CLI produce the measured
// transition (audit §3.2 rule 3: compressed seed OK for cursor only).

package req039_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	body := req039fixtures.MainPreToolUseBody(root, "session-ct-039-01-sys", "Edit", map[string]any{
		"file_path": "docs/architecture/ARCHITECTURE-039-loop-control-plane.md",
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

	body := req039fixtures.MainPreToolUseBody(root, "session-ct-039-11-sys", "Edit", map[string]any{
		"file_path": "docs/dev/contracts/BE-039-loop-controller.md",
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

// TestCT03912_BuilderCompleteCommitsTR006System covers CT-039-12.
//
// The S6 batch gate has two halves, and this case asserts both.
//
// Segment 1 (negative): the retired legacy wiring — a completion report plus a
// worktree checkpoint — is evaluated by the gate, which names the missing
// canonical responsibility per TASK and commits nothing. The PreToolUse Hook
// is a gate evaluator, not the TR-006 outlet.
// blueprint/l3-stages/L3-S6-build.md:293 —
// 「删除 frozen／legacy 的启动、观察、接收及恢复接线，而非增加兼容层。」
// blueprint/l4-mechanisms/L4-workflow-convergence.md:293 —
// 「删除 frozen／legacy 的启动、观察、接收及恢复接线，而非增加兼容层。」
//
// Segment 2 (positive): the canonical denominator is the Assignment Record
// path, and TR-006 is committed by Main's integration exit.
// blueprint/l3-stages/L3-S6-build.md:417 —
// 「原 `GATE-BUILDER-BATCH-READY` 及 TR-006 使用 TR-003 的精确非空任务集…」
// blueprint/l3-stages/L3-S6-build.md:436 —
// 「最后一次有效集成的完成接续应检查原整批 Gate，通过原 TR-006 进入 S7。」
func TestCT03912_BuilderCompleteCommitsTR006System(t *testing.T) {
	runner := &req039fixtures.CLIRunner{}

	// --- Segment 1: the retired legacy facts are evaluated, never accepted.
	root := freshRoot(t)
	// Use BaseState (same as CLI CT-039-12) so locked TASK/docs + revision
	// match the TR-006 batch gate; systemPlanningState alone trips
	// LOOP_TRANSITION_GUARD on incomplete planning docs.
	state := req039fixtures.BaseState(t, root, "building", "", 19)
	req039fixtures.SeedBuilderBatchReady(t, root, state)
	writeSystemState(t, root, state)
	beforeLast := req039fixtures.LastTransitionID(state)
	beforeEvents := transitionJournalEvents(t, root)

	body := req039fixtures.MainPreToolUseBody(root, "session-ct-039-12-sys", "Bash", map[string]any{
		"command": "go test ./...",
	})
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-12 forbids manual transition CLI")
	}
	_, qg := parseEnv(t, stdout)
	if gate, _ := qg["gate_id"].(string); gate != "GATE-BUILDER-BATCH-READY" {
		t.Fatalf("CT-039-12 must evaluate the S6 batch gate, got %q qg=%v", gate, qg)
	}
	if cand := req039fixtures.CandidateTransition(qg); cand != "TR-006" {
		t.Fatalf("CT-039-12 candidate want TR-006, got %q", cand)
	}
	// Negative: the retired legacy completion facts must not open TR-006.
	if req039fixtures.TransitionCommitted(qg) {
		t.Fatalf("CT-039-12: the retired legacy facts must not commit TR-006, qg=%v", qg)
	}
	// The gate names the missing canonical responsibility for every TASK.
	missing := fmt.Sprint(qg["missing"])
	for _, taskID := range []string{"TASK-039-01", "TASK-039-02"} {
		if !strings.Contains(missing, "builder_responsibility:"+taskID) {
			t.Fatalf("CT-039-12 must name the missing canonical responsibility for %s, got missing=%v", taskID, qg["missing"])
		}
	}
	after := req039fixtures.ReadState(t, root)
	lc, ph := req039fixtures.Lifecycle(after)
	if lc != "building" || ph != "" {
		t.Fatalf("CT-039-12 must leave the legacy-fact runtime at building, got %q/%q stdout=%s", lc, ph, stdout)
	}
	// Negative: no ghost TR-006 — neither a recorded transition nor a journal
	// transition event.
	if last := req039fixtures.LastTransitionID(after); last != beforeLast {
		t.Fatalf("CT-039-12 must not record a transition from the legacy facts, got %q want %q", last, beforeLast)
	}
	if events := transitionJournalEvents(t, root); events != beforeEvents {
		t.Fatalf("CT-039-12 must not write a transition event for the legacy facts, got %q want %q", events, beforeEvents)
	}

	// --- Segment 2: the canonical path commits TR-006 through Main's
	// integration exit, not through a Hook.
	canonicalRoot, worker, record := systemNativeBuilder(t)
	receiveSystemNativeBuilder(t, canonicalRoot, worker, record)
	code, stdout, stderr = runTaskIntegrate(t, canonicalRoot, record.AssignmentID)
	if code != 0 {
		t.Fatalf("Main integration exit failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "TR-006") {
		t.Fatalf("Main integration exit must record the original TR-006, stdout=%s", stdout)
	}
	canonical := req039fixtures.ReadState(t, canonicalRoot)
	req039fixtures.AssertLifecycle(t, canonical, "verification", "planned")
	req039fixtures.AssertLastTransition(t, canonical, "TR-006")
	// Positive: the delivery was really merged by Main (a two-parent merge
	// commit), not simulated by a state edit.
	parents := strings.Fields(runGitIn(t, canonicalRoot, "rev-list", "--parents", "-n", "1", "HEAD"))
	if len(parents) != 3 {
		t.Fatalf("CT-039-12: Main must merge the reviewed delivery, rev-list --parents=%v", parents)
	}
}

// transitionJournalEvents returns the transition events recorded in the
// Runtime journal. A gate evaluation that commits nothing must leave it
// unchanged; a runtime that never recorded a transition has no journal yet.
func transitionJournalEvents(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude", "loop-events.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	var found []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["event"] == "transition_committed" {
			found = append(found, fmt.Sprint(event["transition_id"]))
		}
	}
	return strings.Join(found, ",")
}

// TestCT03924_SameAgentDualDVBlocksTR003System covers CT-039-24:
// same agent dual DV labels → GATE-DOCUMENT-PASS not_ready/unknown, no TR-003.
func TestCT03924_SameAgentDualDVBlocksTR003System(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "design", 18)
	req039fixtures.SeedDualDVSameAgent(t, root, state)
	writeSystemState(t, root, state)

	body := req039fixtures.MainPreToolUseBody(root, "session-ct-039-24-sys", "Edit", map[string]any{
		"file_path": "docs/dev/contracts/BE-039-loop-controller.md",
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
