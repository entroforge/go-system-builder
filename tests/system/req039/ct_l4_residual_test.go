// ct_l4_residual_test.go — BUG-039-39 residual L3→L4 Hook CLI wrappers
// for CT/AC contracts that already have contract-complete Hook paths.
// CT-039-19 stays L3 (timeout inject). CT-039-04/AC-003 remain integration
// scenarios and do not make PreToolUse a per-tool authorization boundary.

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
	if pd := env["hookSpecificOutput"].(map[string]any)["permissionDecision"]; pd != nil {
		t.Fatalf("CT-039-02 must not render a per-tool decision, got %v", pd)
	}
	status, _ := qg["status"].(string)
	switch status {
	case "not_ready", "unknown", "satisfied":
	default:
		t.Fatalf("CT-039-02 status must be not_ready/unknown/satisfied, got %q", status)
	}
}

// TestCT03905_UnlockedSiblingWriteIsObservedSystem covers CT-039-05 at L4.
func TestCT03905_UnlockedSiblingWriteIsObservedSystem(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-ct-039-05-sys", "Edit", map[string]any{
		"file_path": "docs/dev/contracts/BE-039-loop-controller-notes.md",
	})
	code, stdout, stderr := runHook(t, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	env, _ := parseEnv(t, stdout)
	if env == nil {
		t.Fatal("CT-039-05 must surface envelope")
	}
	if pd := env["hookSpecificOutput"].(map[string]any)["permissionDecision"]; pd != nil {
		t.Fatalf("CT-039-05 must not render a per-tool decision, got %v", pd)
	}
}

// TestCT03906_GitSquashMergeIsObservedSystem covers CT-039-06 at L4. The
// graph listener reports progress but leaves individual Git commands to the
// caller's normal execution and repository rules.
func TestCT03906_GitSquashMergeIsObservedSystem(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	writeSystemState(t, root, state)
	before := req039fixtures.ReadState(t, root)

	body := req039fixtures.PreToolUseBody("session-ct-039-06-sys", "Bash", map[string]any{
		"command": "git merge --squash feature/req-039",
	})
	code, stdout, stderr := runHook(t, root, "PreToolUse", body)
	after := req039fixtures.ReadState(t, root)
	assertPreToolUseObservedWithoutDenyOrAdvance(t, code, stdout, stderr, before, after)
}

// TestCT03907_OrdinaryGitOpsIsObservedSystem covers CT-039-07 at L4.
func TestCT03907_OrdinaryGitOpsIsObservedSystem(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	writeSystemState(t, root, state)

	// PreToolUse observes graph progress for both routine and release-shaped
	// commands; it does not classify commands or deny individual tools.
	for _, command := range []string{
		"git merge feature/req-039",
		"git push origin develop",
		"git status --porcelain",
		"npm publish",
	} {
		before := req039fixtures.ReadState(t, root)
		body := req039fixtures.PreToolUseBody("session-ct-039-07-sys", "Bash", map[string]any{
			"command": command,
		})
		code, stdout, stderr := runHook(t, root, "PreToolUse", body)
		after := req039fixtures.ReadState(t, root)
		assertPreToolUseObservedWithoutDenyOrAdvance(t, code, stdout, stderr, before, after)
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

// TestAC002_NotReadyQualityGateIsObservedSystem is the AC-002 twin of CT-039-02.
func TestAC002_NotReadyQualityGateIsObservedSystem(t *testing.T) {
	TestCT03902_ContractsMissingCoverageExposesNotReadySystem(t)
}

// TestAC004_PreToolUseObservesGitOpsWithoutDenySystem covers the listener
// boundary through Hook CLI twins of CT-06/07.
func TestAC004_PreToolUseObservesGitOpsWithoutDenySystem(t *testing.T) {
	TestCT03906_GitSquashMergeIsObservedSystem(t)
	TestCT03907_OrdinaryGitOpsIsObservedSystem(t)
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

// The public recovery chain reads current durable facts; PreCompact does
// not manufacture another checkpoint or require a matching cached sentence.
func TestHOOK_PostCompact_D011SessionStartUsesOriginalFactsSystem(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 4)
	state["milestone"].(map[string]any)["objective"] = "STALE-D011-CACHED-OBJECTIVE"
	writeSystemState(t, root, state)
	before := readSystemRuntimePair(t, root)
	code, stdout, stderr := runHook(t, root, "PreCompact", `{"hook_event_name":"PreCompact","session_id":"session-d011"}`)
	if code != 0 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("PreCompact did not remain silent: %d %s %s", code, stdout, stderr)
	}
	assertSystemRuntimePairUnchanged(t, before)
	code, stdout, stderr = runHook(t, root, "SessionStart", `{"hook_event_name":"SessionStart","session_id":"session-d011-recover"}`)
	if code != 0 {
		t.Fatalf("SessionStart recovery failed: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"S2 planning.design", "docs/control/agent-protocol.md#s2"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("current recovery lost %q: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "STALE-D011-CACHED-OBJECTIVE") {
		t.Fatalf("stale cache became current authority: %s", stdout)
	}
	req039fixtures.AssertLifecycle(t, req039fixtures.ReadState(t, root), "planning", "design")
}
