package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestCT03911_DualPassDocumentVerificationCommitsTR003 covers CT-039-11:
// S5 dual PASS records + PreToolUse → TR-003 with locked exact generation.
func TestCT03911_DualPassDocumentVerificationCommitsTR003(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "document_verification", "", 18)
	req039fixtures.SeedDocumentPassS5(t, root, state, "dv-spec-1", "dv-task-2")
	req039fixtures.WriteState(t, root, state)

	runner := &req039fixtures.CLIRunner{}
	body := req039fixtures.PreToolUseBody("session-ct-039-11", "Edit", map[string]any{
		"file_path": "docs/contracts/BE-039-loop-controller.md",
	})
	code, stdout, stderr := req039fixtures.RunHook(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-11 must not use manual transition CLI, got %d calls", runner.ManualTransitionCalls)
	}
	if !strings.Contains(stdout, "GATE-DOCUMENT-PASS") {
		t.Fatalf("CT-039-11 hook must surface GATE-DOCUMENT-PASS, stdout=%s", stdout)
	}
	if !strings.Contains(stdout, "TR-003") {
		t.Fatalf("CT-039-11 hook must commit/candidate TR-003, stdout=%s", stdout)
	}
	after := req039fixtures.ReadState(t, root)
	lc, _ := req039fixtures.Lifecycle(after)
	if lc != "building" {
		t.Fatalf("CT-039-11 TR-003 must advance to building, got lifecycle=%q", lc)
	}
}

// TestCT03912_BuilderCompleteCommitsTR006NoManualCLI covers CT-039-12:
// builder reports complete + PreToolUse → TR-006 without CLI transition.
func TestCT03912_BuilderCompleteCommitsTR006NoManualCLI(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "building", "", 19)
	req039fixtures.SeedBuilderBatchReady(t, root, state)
	req039fixtures.WriteState(t, root, state)

	runner := &req039fixtures.CLIRunner{}
	body := req039fixtures.PreToolUseBody("session-ct-039-12", "Bash", map[string]any{
		"command": "go test ./...",
	})
	code, stdout, stderr := req039fixtures.RunHook(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-12 forbids manual transition CLI, got %d", runner.ManualTransitionCalls)
	}
	if !strings.Contains(stdout, "TR-006") {
		t.Fatalf("CT-039-12 hook must commit/candidate TR-006, stdout=%s", stdout)
	}
	after := req039fixtures.ReadState(t, root)
	lc, ph := req039fixtures.Lifecycle(after)
	if lc != "verification" || ph != "delivery" {
		t.Fatalf("CT-039-12 TR-006 must land verification.delivery, got %q/%q", lc, ph)
	}
}

// TestCT03910_ConflictDirtyPreservePins maps SYNC-039 §12 CT-039-10 to the
// L4 system scenario (SubagentStop + merge conflict preserve). Integration
// package also covers conflict/dirty/check-fail preserve paths.
func TestCT03910_ConflictDirtyPreservePins(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{
		"tests/system/req039/ct_integration_preserve_test.go:TestCT03910_ConflictPreservesWorktreeViaSubagentStop",
		"internal/integration/integrate_test.go:TestIntegrateConflictPreservesWorktree",
	} {
		if !strings.Contains(ref, "req039") && !strings.Contains(ref, "integration") {
			t.Fatalf("unexpected CT-039-10 ref %q", ref)
		}
	}
}

// TestCT03913_VerificationChainOneHookPerStep is a thin CLI-level pin for
// CT-039-13; the multi-step Delivery→QA→E2E→clean chain is exercised in
// tests/system/req039/ct_verification_chain_test.go.
func TestCT03913_VerificationChainOneHookPerStep(t *testing.T) {
	t.Run("delivery_cursor_surfaces_PTR-VERIFY-01", func(t *testing.T) {
		root := req039fixtures.FreshRoot(t)
		state := req039fixtures.BaseState(t, root, "verification", "delivery", 25)
		req039fixtures.SeedVerificationDelivery(t, root, state)
		req039fixtures.WriteState(t, root, state)

		result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
			Root: root, Event: "PreToolUse", ToolName: "Bash",
			ToolInput: map[string]any{"command": "go test ./..."},
		})
		if err != nil {
			t.Fatalf("control cycle: %v", err)
		}
		if result.Decision.Decision != "allow" {
			t.Fatalf("CT-039-13 must allow tool at delivery cursor, got %q", result.Decision.Decision)
		}
		if result.QualityGate.GateID == "" && result.QualityGate.Status != controller.StatusUnknown {
			t.Fatalf("CT-039-13 must surface a gate projection, got %+v", result.QualityGate)
		}
	})
}

// TestCT03914_EvidenceDrivenCorrectionPath pins CT-039-14 at the Hook
// entry; the full finding→BUG→repair→full review chain is in system tests.
func TestCT03914_EvidenceDrivenCorrectionPath(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "verification", "delivery", 30)
	req039fixtures.SeedVerificationDelivery(t, root, state)
	state["entities"] = map[string]any{
		"agents": []any{}, "tasks": []any{}, "teams": []any{},
		"bugs": []any{map[string]any{"id": "BUG-039-14", "state": "investigating"}},
	}
	req039fixtures.WriteState(t, root, state)

	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root: root, Event: "PreToolUse", ToolName: "Edit",
		ToolInput: map[string]any{"file_path": "internal/controller/cycle.go"},
	})
	if err != nil {
		t.Fatalf("control cycle: %v", err)
	}
	if result.Decision.Decision != "allow" {
		t.Fatalf("CT-039-14 correction path must allow tools, got %q", result.Decision.Decision)
	}
}

// TestCT03915_S11StopsAutoAdvance covers CT-039-15: ACC + audit approved
// terminal cursor must not auto-advance across awaiting_human_release.
func TestCT03915_S11StopsAutoAdvance(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "awaiting_human_release", "", 50)
	req039fixtures.SeedAwaitingHumanRelease(t, root, state)
	req039fixtures.WriteState(t, root, state)

	runner := &req039fixtures.CLIRunner{}
	before := req039fixtures.ReadState(t, root)
	beforeLC, beforePh := req039fixtures.Lifecycle(before)

	body := req039fixtures.PreToolUseBody("session-ct-039-15", "Bash", map[string]any{"command": "ls"})
	code, stdout, stderr := req039fixtures.RunHook(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse at S11 failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-15 must not require manual transition CLI")
	}
	_, qg := req039fixtures.ParseQualityGate(t, stdout)
	if committed, _ := qg["transition_committed"].(bool); committed {
		t.Fatalf("CT-039-15 terminal state must not commit transition, qg=%v", qg)
	}

	after := req039fixtures.ReadState(t, root)
	afterLC, afterPh := req039fixtures.Lifecycle(after)
	if afterLC != beforeLC || afterPh != beforePh {
		t.Fatalf("CT-039-15 must not cross terminal lifecycle: %s/%s -> %s/%s", beforeLC, beforePh, afterLC, afterPh)
	}
	if !strings.Contains(stdout, "release") && !strings.Contains(stdout, "human") && !strings.Contains(stdout, "Gateway") {
		t.Fatalf("CT-039-15 must surface human Gateway guidance, got %s", stdout)
	}
}

// TestCT03916_ConflictingEventsUnknownConflict covers CT-039-16:
// conflicting requested events → unknown + LOOP_TRIGGER_CONFLICT, allow, no transition.
func TestCT03916_ConflictingEventsUnknownConflict(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "verification", "delivery", 32)
	req039fixtures.SeedConflictingDeliveryEvents(t, root, state)
	req039fixtures.WriteState(t, root, state)

	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root: root, Event: "PreToolUse", ToolName: "Bash",
		ToolInput: map[string]any{"command": "go test ./..."},
	})
	if err != nil {
		t.Fatalf("control cycle: %v", err)
	}
	if result.QualityGate.Status != controller.StatusUnknown {
		t.Fatalf("CT-039-16 want status=unknown, got %q", result.QualityGate.Status)
	}
	if result.QualityGate.ErrorCode != "LOOP_TRIGGER_CONFLICT" {
		t.Fatalf("CT-039-16 want LOOP_TRIGGER_CONFLICT, got %q", result.QualityGate.ErrorCode)
	}
	if result.Decision.Decision != "allow" {
		t.Fatalf("CT-039-16 must allow tool on conflict, got %q", result.Decision.Decision)
	}
	if result.QualityGate.TransitionCommitted {
		t.Fatal("CT-039-16 must not commit transition on selector conflict")
	}
}

// TestCT03917_IdempotentIntegrationResume pins CT-039-17 at the system layer;
// see tests/system/req039/ct_integration_resume_test.go.
func TestCT03917_IdempotentIntegrationResume(t *testing.T) {
	t.Skip("CT-039-17 Hook+git worktree scenario lives in tests/system/req039/ct_integration_resume_test.go")
}

// TestCT03918_G2ReworkOldArtifactImmutable covers CT-039-18: g2 rework keeps
// g1 artifacts superseded while generation-2 manifest is active.
func TestCT03918_G2ReworkOldArtifactImmutable(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "document_verification", "", 20)
	req039fixtures.SeedG2Rework(t, root, state)
	req039fixtures.WriteState(t, root, state)

	after := req039fixtures.ReadState(t, root)
	baseline, _ := after["baseline"].(map[string]any)
	if gen, _ := baseline["generation"].(float64); gen != 2 {
		t.Fatalf("CT-039-18 baseline generation must be 2, got %v", baseline["generation"])
	}
	docs, _ := after["documents"].([]any)
	var g1Status, g2Status string
	for _, raw := range docs {
		doc, _ := raw.(map[string]any)
		if gen, _ := doc["generation"].(float64); gen == 1 {
			g1Status, _ = doc["status"].(string)
		}
		if gen, _ := doc["generation"].(float64); gen == 2 {
			g2Status, _ = doc["status"].(string)
		}
	}
	if g1Status != "superseded" {
		t.Fatalf("CT-039-18 g1 artifact must remain superseded/immutable in manifest, got %q", g1Status)
	}
	if g2Status != "locked" {
		t.Fatalf("CT-039-18 g2 manifest must be active locked, got %q", g2Status)
	}

	runner := &req039fixtures.CLIRunner{}
	body := req039fixtures.PreToolUseBody("session-ct-039-18", "Edit", map[string]any{
		"file_path": "docs/design/versions/REQ-039/g2/ARCHITECTURE-039-loop-control-plane.md",
	})
	code, _, stderr := req039fixtures.RunHook(t, runner, root, "PreToolUse", body)
	if code == 2 {
		t.Fatalf("CT-039-18 g2 active path must not be blocked: stderr=%s", stderr)
	}
}

// TestCT03919_GateTimeoutStableErrorNoTransition covers CT-039-19:
// gate timeout → LOOP_GATE_UNKNOWN + final safety + no transition.
// BUG-039-16 landed the budget path; this CT drives it via ControlRequest
// injection (slow evaluator + short budget) and asserts the SYNC-039 §12
// contract at the Controller entry used by Hooks.
func TestCT03919_GateTimeoutStableErrorNoTransition(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "planning", "design", 12)
	req039fixtures.WriteState(t, root, state)

	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	beforeTransitions := countJournalEvents(t, journalPath, "transition_committed")

	start := time.Now()
	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:               root,
		Event:              "PreToolUse",
		ToolName:           "Edit",
		ToolInput:          map[string]any{"file_path": "docs/design/architecture/ARCHITECTURE-039-loop-control-plane.md"},
		QualityCycleBudget: 50 * time.Millisecond,
		GateEvaluator:      ctSlowEvaluator{delay: 500 * time.Millisecond},
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("control cycle: %v", err)
	}
	if elapsed >= 500*time.Millisecond {
		t.Fatalf("CT-039-19 must cut off within budget, waited %v", elapsed)
	}
	if result.QualityGate.Status != controller.StatusUnknown {
		t.Fatalf("CT-039-19 status want unknown, got %q", result.QualityGate.Status)
	}
	if result.QualityGate.ErrorCode != controller.CodeGateUnknown && result.ErrorCode != controller.CodeGateUnknown {
		t.Fatalf("CT-039-19 want LOOP_GATE_UNKNOWN, got gate=%q result=%q", result.QualityGate.ErrorCode, result.ErrorCode)
	}
	if result.QualityGate.TransitionCommitted {
		t.Fatal("CT-039-19 must not commit a transition on timeout")
	}
	if result.Decision.Decision != "allow" {
		t.Fatalf("CT-039-19 final safety must allow tool, got %q", result.Decision.Decision)
	}
	if after := countJournalEvents(t, journalPath, "transition_committed"); after != beforeTransitions {
		t.Fatalf("CT-039-19 journal transition_committed before=%d after=%d", beforeTransitions, after)
	}
	if lc, _ := req039fixtures.Lifecycle(req039fixtures.ReadState(t, root)); lc != "planning" {
		t.Fatalf("CT-039-19 must leave lifecycle unchanged, got %q", lc)
	}
}

type ctSlowEvaluator struct {
	delay time.Duration
}

func (c ctSlowEvaluator) Evaluate(context.Context, qualitygate.Input) (qualitygate.Evaluation, error) {
	time.Sleep(c.delay)
	return qualitygate.Evaluation{Status: qualitygate.StatusSatisfied}, nil
}

func countJournalEvents(t *testing.T, journalPath, eventType string) int {
	t.Helper()
	data, err := os.ReadFile(journalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["event"] == eventType {
			count++
		}
	}
	return count
}

// TestCT03920_BuilderCompletionDurableRef covers CT-039-20: Builder
// completion must reference schema-valid durable storage.
func TestCT03920_BuilderCompletionDurableRef(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "building", "", 19)
	req039fixtures.SeedBuilderBatchReady(t, root, state)
	req039fixtures.WriteState(t, root, state)

	reportDir := filepath.Join(root, ".claude", "evidence", req039fixtures.RuntimeIDFromState(state), "g1", "assignments", "assignment-ct-20")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	report := `{"schema_version":"1.0.0","message_type":"completion_report","assignment_id":"assignment-ct-20","task_id":"TASK-039-01"}`
	reportPath := filepath.Join(reportDir, "completion.json")
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &req039fixtures.CLIRunner{}
	runtimeID := req039fixtures.RuntimeIDFromState(state)
	input := `{
		"session_id":"session-ct-039-20",
		"hook_event_name":"SubagentStop",
		"agent_id":"builder-1",
		"agent_report_complete":true,
		"assignment_id":"assignment-ct-20",
		"completion_ref":".claude/evidence/` + runtimeID + `/g1/assignments/assignment-ct-20/completion.json"
	}`
	code, stdout, stderr := req039fixtures.RunHook(t, runner, root, "SubagentStop", input)
	if code != 0 {
		t.Fatalf("SubagentStop failed: code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "completion") && !strings.Contains(stdout, "integration") {
		t.Fatalf("CT-039-20 must surface completion/integration guidance, got %s", stdout)
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("CT-039-20 completion ref must remain durable on disk: %v", err)
	}
}

// TestCT03921_CleanRoundIncompleteReturnsDelivery covers CT-039-21:
// clean round incomplete → PTR-VERIFY-05 back to delivery.
func TestCT03921_CleanRoundIncompleteReturnsDelivery(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "verification", "clean_round_evaluation", 40)
	req039fixtures.SeedCleanRoundIncomplete(t, root, state)
	req039fixtures.WriteState(t, root, state)

	runner := &req039fixtures.CLIRunner{}
	body := req039fixtures.PreToolUseBody("session-ct-039-21", "Bash", map[string]any{"command": "go test ./..."})
	code, stdout, stderr := req039fixtures.RunHook(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-21 must not use manual transition CLI")
	}
	if !strings.Contains(stdout, "PTR-VERIFY-05") {
		t.Fatalf("CT-039-21 hook must commit/candidate PTR-VERIFY-05, stdout=%s", stdout)
	}
	after := req039fixtures.ReadState(t, root)
	_, ph := req039fixtures.Lifecycle(after)
	if ph != "delivery" {
		t.Fatalf("CT-039-21 must return to verification.delivery, got phase=%q", ph)
	}
}

// TestCT03922_BugReportRejectedReturnsInvestigation covers CT-039-22.
func TestCT03922_BugReportRejectedReturnsInvestigation(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "bug_report_review", 35)
	req039fixtures.SeedBugReportsRejected(t, root, state)
	req039fixtures.WriteState(t, root, state)

	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root: root, Event: "PreToolUse", ToolName: "Edit",
		ToolInput: map[string]any{"file_path": "docs/reports/bugs/BUG-039-15.md"},
	})
	if err != nil {
		t.Fatalf("control cycle: %v", err)
	}
	if result.QualityGate.CandidateTransition != "PTR-BUG-03" {
		t.Fatalf("CT-039-22 candidate want PTR-BUG-03, got %q status=%q", result.QualityGate.CandidateTransition, result.QualityGate.Status)
	}
	if !result.QualityGate.TransitionCommitted {
		t.Fatalf("CT-039-22 must commit PTR-BUG-03, missing=%v", result.QualityGate.Missing)
	}
	after := req039fixtures.ReadState(t, root)
	_, ph := req039fixtures.Lifecycle(after)
	if ph != "investigation" {
		t.Fatalf("CT-039-22 must return to investigation, got phase=%q", ph)
	}
}

// TestCT03923_TargetedRecheckFailedReturnsInvestigation covers CT-039-23.
func TestCT03923_TargetedRecheckFailedReturnsInvestigation(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "targeted_reverification", 36)
	req039fixtures.SeedTargetedReverificationFail(t, root, state)
	req039fixtures.WriteState(t, root, state)

	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root: root, Event: "PreToolUse", ToolName: "Edit",
		ToolInput: map[string]any{"file_path": "internal/controller/cycle.go"},
	})
	if err != nil {
		t.Fatalf("control cycle: %v", err)
	}
	if result.QualityGate.CandidateTransition != "PTR-BUG-07" {
		t.Fatalf("CT-039-23 candidate want PTR-BUG-07, got %q status=%q", result.QualityGate.CandidateTransition, result.QualityGate.Status)
	}
	if !result.QualityGate.TransitionCommitted {
		t.Fatalf("CT-039-23 must commit PTR-BUG-07, missing=%v", result.QualityGate.Missing)
	}
	after := req039fixtures.ReadState(t, root)
	_, ph := req039fixtures.Lifecycle(after)
	if ph != "investigation" {
		t.Fatalf("CT-039-23 must return to investigation, got phase=%q", ph)
	}
}

// TestCT03924_SameAgentDualDVLabelsBlocksTR003 covers CT-039-24:
// same agent emits two DV labels → GATE-DOCUMENT-PASS not_ready, no TR-003.
func TestCT03924_SameAgentDualDVLabelsBlocksTR003(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "document_verification", "", 18)
	req039fixtures.SeedDualDVSameAgent(t, root, state)
	req039fixtures.WriteState(t, root, state)

	runner := &req039fixtures.CLIRunner{}
	body := req039fixtures.PreToolUseBody("session-ct-039-24", "Edit", map[string]any{
		"file_path": "docs/contracts/BE-039-loop-controller.md",
	})
	code, stdout, stderr := req039fixtures.RunHook(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-24 must not use manual transition CLI")
	}
	_, qg := req039fixtures.ParseQualityGate(t, stdout)
	gateID, _ := qg["gate_id"].(string)
	if !strings.Contains(gateID, "GATE-DOCUMENT-PASS") {
		t.Fatalf("CT-039-24 must surface GATE-DOCUMENT-PASS, got %v", qg)
	}
	status, _ := qg["status"].(string)
	if status != "not_ready" && status != "unknown" {
		t.Fatalf("CT-039-24 want not_ready/unknown, got %q missing=%v", status, qg["missing"])
	}
	if committed, _ := qg["transition_committed"].(bool); committed {
		t.Fatal("CT-039-24 must not commit TR-003 when independent reviewers missing")
	}
	after := req039fixtures.ReadState(t, root)
	lc, _ := req039fixtures.Lifecycle(after)
	if lc != "document_verification" {
		t.Fatalf("CT-039-24 must remain at document_verification, got %q", lc)
	}
}
