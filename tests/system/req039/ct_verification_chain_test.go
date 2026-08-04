// ct_verification_chain_test.go — CT-039-13 L4 intent: Delivery→QA→E2E→clean, one Hook per step → TR-009.

package req039_test

import (
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestCT03913_VerificationChainOneHookPerStep drives the SYNC-039 §12 chain
// with one PreToolUse per committed transition ending at TR-009.
// Skips when loop-state schema or angle guards block commit (product blocker).
func TestCT03913_VerificationChainOneHookPerStep(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}

	state := systemPlanningState(t, root, "design", 25)
	req039fixtures.SeedVerificationDelivery(t, root, state)
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	req039fixtures.EnsureREQDoc(t, root, state, "none")
	req039fixtures.EnsureStateRoot(state, root)
	writeSystemState(t, root, state)

	toolInput := map[string]any{"command": "go test ./..."}
	const bugID = "CT-039-13/PTR-VERIFY"

	// Step 1: delivery PASS → PTR-VERIFY-01 → qa
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteVerificationDimensionPass(t, root, state, "delivery")
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct13-delivery", "Bash", toolInput,
		"PTR-VERIFY-01", "verification", "qa", bugID)

	// Step 2: qa PASS → PTR-VERIFY-02 → e2e_browser
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteVerificationDimensionPass(t, root, state, "qa")
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct13-qa", "Bash", toolInput,
		"PTR-VERIFY-02", "verification", "e2e_browser", bugID)

	// Step 3: e2e PASS → PTR-VERIFY-03 → clean_round_evaluation
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteVerificationDimensionPass(t, root, state, "e2e_browser")
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct13-e2e", "Bash", toolInput,
		"PTR-VERIFY-03", "verification", "clean_round_evaluation", bugID)

	// Step 4: clean round valid → PTR-VERIFY-04 → clean_round_passed
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteCleanRoundEvaluationPass(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct13-clean-eval", "Bash", toolInput,
		"PTR-VERIFY-04", "verification", "clean_round_passed", bugID)

	// Step 5: TR-009 → acceptance
	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct13-tr009", "Bash", toolInput,
		"TR-009", "acceptance", "", bugID)

	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-13 must not use manual transition CLI")
	}
}

// TestCT03921_CleanRoundIncompleteSystem drives PTR-VERIFY-05 via Hook at system level.
func TestCT03921_CleanRoundIncompleteSystem(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "design", 40)
	req039fixtures.SeedCleanRoundIncomplete(t, root, state)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-ct-039-21-sys", "Bash", map[string]any{"command": "go test ./..."})
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-21 must not use manual transition CLI")
	}
	_, qg := parseEnv(t, stdout)
	if candidate, _ := qg["candidate_transition"].(string); candidate != "PTR-VERIFY-05" {
		t.Fatalf("want PTR-VERIFY-05, got %q qg=%v", candidate, qg)
	}
	after := req039fixtures.ReadState(t, root)
	_, ph := req039fixtures.Lifecycle(after)
	if ph != "delivery" {
		t.Fatalf("PTR-VERIFY-05 must return to delivery, got phase=%q", ph)
	}
}
