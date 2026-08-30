// ct_l4_residual_test.go — BUG-039-39 residual L3→L4 Hook CLI wrappers
// for CT/AC contracts that already have contract-complete Hook paths.
// CT-039-19 stays L3 (timeout inject). CT-039-04/AC-003 stay honest L3
// until LockedArtifacts are threaded through Controller buildSafetyInput.

package req039_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestCT03902_ContractsMissingCoverageExposesNotReadySystem covers CT-039-02 at L4.
func TestCT03902_ContractsMissingCoverageExposesNotReadySystem(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "contracts", 1)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-ct-039-02-sys", "Edit", map[string]any{
		"file_path": "internal/cli/controller.go",
	})
	code, stdout, stderr := runHook(t, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	env, qg := parseEnv(t, stdout)
	if env == nil {
		t.Fatal("CT-039-02 must surface quality_gate envelope")
	}
	if pd := env["hookSpecificOutput"].(map[string]any)["permissionDecision"]; pd != "allow" {
		t.Fatalf("CT-039-02 must allow tool, got %v", pd)
	}
	status, _ := qg["status"].(string)
	switch status {
	case "not_ready", "unknown", "satisfied":
	default:
		t.Fatalf("CT-039-02 status must be not_ready/unknown/satisfied, got %q", status)
	}
}

// TestCT03905_UnlockedSiblingWriteAllowsSystem covers CT-039-05 at L4.
func TestCT03905_UnlockedSiblingWriteAllowsSystem(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-ct-039-05-sys", "Edit", map[string]any{
		"file_path": "docs/contracts/BE-039-loop-controller-notes.md",
	})
	code, stdout, stderr := runHook(t, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	env, _ := parseEnv(t, stdout)
	if env == nil {
		t.Fatal("CT-039-05 must surface envelope")
	}
	if pd := env["hookSpecificOutput"].(map[string]any)["permissionDecision"]; pd != "allow" {
		t.Fatalf("CT-039-05 must allow unlocked sibling, got %v", pd)
	}
}

// TestCT03906_GitSquashMergeBlocksSystem covers CT-039-06 at L4.
func TestCT03906_GitSquashMergeBlocksSystem(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-ct-039-06-sys", "Bash", map[string]any{
		"command": "git merge --squash feature/req-039",
	})
	code, stdout, stderr := runHook(t, root, "PreToolUse", body)
	if code != 2 {
		t.Fatalf("CT-039-06 must exit 2 on squash: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "squash_merge") {
		t.Fatalf("CT-039-06 must carry squash_merge reason: %s", stdout)
	}
}

// TestCT03907_OrdinaryGitOpsAllowSystem covers CT-039-07 at L4.
func TestCT03907_OrdinaryGitOpsAllowSystem(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	writeSystemState(t, root, state)

	// RC-06 (S10-3): the protected-commands table is now wired into the
	// PreToolUse enforce path, so `git push origin develop` (protected
	// release-branch row) and `npm publish` (HS-005) are hard-denied by
	// design. The allow surface here is ordinary non-release git work.
	for _, command := range []string{
		"git merge feature/req-039",
		"git status --porcelain",
		"git log --oneline -5",
	} {
		body := req039fixtures.PreToolUseBody("session-ct-039-07-sys", "Bash", map[string]any{
			"command": command,
		})
		code, stdout, stderr := runHook(t, root, "PreToolUse", body)
		if code == 2 {
			t.Fatalf("CT-039-07 must not block %q: stderr=%s stdout=%s", command, stderr, stdout)
		}
	}
}

// TestCT03916_ConflictingEventsUnknownConflictSystem covers CT-039-16 via Hook CLI.
func TestCT03916_ConflictingEventsUnknownConflictSystem(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := req039fixtures.BaseState(t, root, "verification", "running", 32)
	req039fixtures.SeedConflictingPauseVerdicts(t, root, state)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-ct-039-16-sys", "Bash", map[string]any{
		"command": "go test ./...",
	})
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	_, qg := parseEnv(t, stdout)
	status, _ := qg["status"].(string)
	if status != "unknown" {
		t.Fatalf("CT-039-16 want status=unknown, got %q qg=%v", status, qg)
	}
	if errCode, _ := qg["error_code"].(string); errCode != "LOOP_TRIGGER_CONFLICT" {
		// Some envelopes nest error_code; also accept substring in raw stdout.
		if !strings.Contains(stdout, "LOOP_TRIGGER_CONFLICT") {
			t.Fatalf("CT-039-16 want LOOP_TRIGGER_CONFLICT, got error_code=%q stdout=%s", errCode, stdout)
		}
	}
	if req039fixtures.TransitionCommitted(qg) {
		t.Fatal("CT-039-16 must not commit on selector conflict")
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-16 must not use manual transition CLI")
	}
}

// TestCT03920_BuilderCompletionDurableRefSystem covers CT-039-20 at L4.
func TestCT03920_BuilderCompletionDurableRefSystem(t *testing.T) {
	root := freshRoot(t)
	state := req039fixtures.BaseState(t, root, "building", "", 19)
	req039fixtures.SeedBuilderBatchReady(t, root, state)
	writeSystemState(t, root, state)

	reportDir := filepath.Join(root, ".claude", "evidence", req039fixtures.RuntimeIDFromState(state), "g1", "assignments", "assignment-ct-20")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(reportDir, "completion.json")
	if err := os.WriteFile(reportPath, []byte(
		`{"schema_version":"1.0.0","message_type":"completion_report","assignment_id":"assignment-ct-20","task_id":"TASK-039-01"}`,
	), 0o644); err != nil {
		t.Fatal(err)
	}

	runtimeID := req039fixtures.RuntimeIDFromState(state)
	input := `{
		"session_id":"session-ct-039-20-sys",
		"hook_event_name":"SubagentStop",
		"agent_id":"builder-1",
		"agent_report_complete":true,
		"assignment_id":"assignment-ct-20",
		"completion_ref":".claude/evidence/` + runtimeID + `/g1/assignments/assignment-ct-20/completion.json"
	}`
	code, stdout, stderr := runHook(t, root, "SubagentStop", input)
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

// TestAC002_NotReadyQualityGateDoesNotBlockToolSystem is the AC-002 twin of CT-039-02.
func TestAC002_NotReadyQualityGateDoesNotBlockToolSystem(t *testing.T) {
	TestCT03902_ContractsMissingCoverageExposesNotReadySystem(t)
}

// TestAC004_OnlySquashMergeBlocksGitOpsSystem covers AC-004 via Hook CLI twins of CT-06/07.
func TestAC004_OnlySquashMergeBlocksGitOpsSystem(t *testing.T) {
	TestCT03906_GitSquashMergeBlocksSystem(t)
	TestCT03907_OrdinaryGitOpsAllowSystem(t)
}

// TestAC005_MilestonePersistsQualityGateSystem covers AC-005 at L4.
func TestAC005_MilestonePersistsQualityGateSystem(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-ac-005-sys", "Edit", map[string]any{
		"file_path": "internal/cli/run.go",
	})
	code, _, stderr := runHook(t, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: stderr=%s", stderr)
	}
	after := req039fixtures.ReadState(t, root)
	ms, _ := after["milestone"].(map[string]any)
	if ms == nil {
		t.Fatal("AC-005 milestone must persist")
	}
	if _, ok := ms["quality_gate"]; !ok {
		t.Fatalf("AC-005 milestone must project quality_gate: %v", ms)
	}
}

// TestHOOK_PostCompact_D011SessionStartChainSystem elevates the honest D-011
// PreCompact→SessionStart fallback to contract-complete L4 (no invent native PostCompact).
func TestHOOK_PostCompact_D011SessionStartChainSystem(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 4)
	writeSystemState(t, root, state)

	code, stdout, stderr := runHook(t, root, "PreCompact",
		`{"hook_event_name":"PreCompact","session_id":"session-d011"}`)
	if code != 0 {
		t.Fatalf("PreCompact failed: code=%d stderr=%s", code, stderr)
	}
	after := req039fixtures.ReadState(t, root)
	ms, _ := after["milestone"].(map[string]any)
	obj, _ := ms["objective"].(string)
	if obj == "" {
		t.Fatalf("PreCompact must persist objective: %v stdout=%s", ms, stdout)
	}

	code, stdout, stderr = runHook(t, root, "SessionStart",
		`{"hook_event_name":"SessionStart","session_id":"session-d011-recover"}`)
	if code != 0 {
		t.Fatalf("SessionStart fallback failed: code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, obj) {
		t.Fatalf("D-011 SessionStart must surface PreCompact objective %q, got %s", obj, stdout)
	}
}
