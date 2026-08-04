// s2_to_s11_hook_driven_test.go — SPINE-S2-S11 L4 intent: organic Hook-driven path (FR-024/025).

package req039_test

import (
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestS2ToS11_HookDrivenCleanPath advances S2→S11 with one Hook per stage;
// compressed seeds establish preconditions only — transitions under test come
// from Hook + evidence in-scenario (BUG-039-25 / TASK-039-09 OrganicSpine).
func TestS2ToS11_HookDrivenCleanPath(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}

	state := systemPlanningState(t, root, "design", 1)
	req039fixtures.SeedPlanningDesignComplete(t, root, state)
	req039fixtures.EnsureStateRoot(state, root)
	writeSystemState(t, root, state)

	const bugID = "SPINE-S2-S11"
	archEdit := map[string]any{"file_path": "docs/design/architecture/ARCHITECTURE-039-loop-control-plane.md"}
	bash := map[string]any{"command": "go test ./..."}

	// S2 → S3 (PTR-PLAN-01)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "spine-s2", "Edit", archEdit,
		"PTR-PLAN-01", "planning", "contracts", bugID)

	// S3 → S4 (PTR-PLAN-02)
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WritePlanningContractPass(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "spine-s3", "Edit",
		map[string]any{"file_path": "docs/contracts/BE-039-loop-controller.md"},
		"PTR-PLAN-02", "planning", "tasks", bugID)

	// S4 → S5 (TR-002)
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WritePlanningTaskPass(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "spine-s4", "Edit",
		map[string]any{"file_path": "docs/tasks/TASK-039-01-loop-definition.md"},
		"TR-002", "document_verification", "", bugID)

	// S5 → S6 (TR-003)
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteDocumentVerificationPassEvidence(t, root, state, "dv-spec", "dv-task")
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "spine-s5", "Edit",
		map[string]any{"file_path": "docs/contracts/BE-039-loop-controller.md"},
		"TR-003", "building", "", bugID)

	// S6 → S7 delivery (TR-006)
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteBuilderBatchReadyEvidence(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "spine-s6", "Bash", bash,
		"TR-006", "verification", "delivery", bugID)

	// S7 verification chain (PTR-VERIFY-01..04 + TR-009)
	state = req039fixtures.ReadState(t, root)
	req039fixtures.EnsureREQDoc(t, root, state, "none")
	req039fixtures.WriteVerificationDimensionPass(t, root, state, "delivery")
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "spine-s7-delivery", "Bash", bash,
		"PTR-VERIFY-01", "verification", "qa", bugID)

	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteVerificationDimensionPass(t, root, state, "qa")
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "spine-s7-qa", "Bash", bash,
		"PTR-VERIFY-02", "verification", "e2e_browser", bugID)

	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteVerificationDimensionPass(t, root, state, "e2e_browser")
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "spine-s7-e2e", "Bash", bash,
		"PTR-VERIFY-03", "verification", "clean_round_evaluation", bugID)

	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteCleanRoundEvaluationPass(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "spine-s7-clean-eval", "Bash", bash,
		"PTR-VERIFY-04", "verification", "clean_round_passed", bugID)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "spine-s7-tr009", "Bash", bash,
		"TR-009", "acceptance", "", bugID)

	// S10 acceptance → release_audit (TR-015)
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteAcceptancePassEvidence(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "spine-s10-acc", "Bash", bash,
		"TR-015", "release_audit", "", bugID)

	// S10 release_audit → S11 (TR-017)
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteReleaseAuditPassEvidence(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "spine-s10-audit", "Bash", bash,
		"TR-017", "awaiting_human_release", "", bugID)

	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("FR-024: manual transition CLI calls = %d, want 0", runner.ManualTransitionCalls)
	}

	final := req039fixtures.ReadState(t, root)
	finalLC, finalPh := req039fixtures.Lifecycle(final)
	ms, _ := final["milestone"].(map[string]any)
	stage, _ := ms["stage"].(string)
	if finalLC != "awaiting_human_release" && stage != "S11" {
		t.Fatalf("FR-024/FR-025: did not reach S11 (lifecycle=%s/%s stage=%s)", finalLC, finalPh, stage)
	}
	assertS11TerminalStop(t, runner, root, final)
}

func assertS11TerminalStop(t *testing.T, runner *req039fixtures.CLIRunner, root string, before map[string]any) {
	t.Helper()
	beforeLC, beforePh := req039fixtures.Lifecycle(before)

	body := req039fixtures.PreToolUseBody("s11-terminal-check", "Bash", map[string]any{"command": "ls"})
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("S11 terminal hook failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("FR-024 at S11: manual transition calls = %d", runner.ManualTransitionCalls)
	}

	after := req039fixtures.ReadState(t, root)
	afterLC, afterPh := req039fixtures.Lifecycle(after)
	if afterLC != beforeLC || afterPh != beforePh {
		t.Fatalf("FR-025: lifecycle changed at terminal: %s/%s -> %s/%s", beforeLC, beforePh, afterLC, afterPh)
	}
	_, qg := parseEnv(t, stdout)
	if committed, _ := qg["transition_committed"].(bool); committed {
		t.Fatalf("FR-025: terminal state committed transition, qg=%v", qg)
	}
	if !strings.Contains(stdout, "release") && !strings.Contains(stdout, "human") && !strings.Contains(stdout, "Gateway") {
		t.Fatalf("FR-025: must surface human Gateway guidance at S11, got %s", stdout)
	}
}
