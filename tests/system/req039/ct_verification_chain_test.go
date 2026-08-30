// ct_verification_chain_test.go — CT-039-13 L4 intent in the L3-S7 shape:
// the round advances through the ReviewPlan lifecycle, and the exit
// transitions (TR-008 sealed batch / TR-009 clean round) fire from the hook.

package req039_test

import (
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestCT03913_VerificationChainOneHookPerStep drives the L3-S7 clean path:
// ReviewPlan registered and fully consumed clean → one PreToolUse commits
// TR-009 into acceptance. The claim-level machinery (plan registration,
// result submit, machine CleanRound) is covered by internal/review tests;
// this system test proves the hook-driven exit.
func TestCT03913_VerificationChainOneHookPerStep(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}

	state := systemPlanningState(t, root, "design", 25)
	req039fixtures.SeedCleanRoundReady(t, root, state)
	req039fixtures.EnsureStateRoot(state, root)
	writeSystemState(t, root, state)

	toolInput := map[string]any{"command": "go test ./..."}
	const bugID = "CT-039-13/TR-009"

	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct13-tr009", "Bash", toolInput,
		"TR-009", "acceptance", "", bugID)

	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-13 must not use manual transition CLI")
	}
}

// TestCT03913ObservationBatchHandoff drives the finding path: a sealed
// ObservationBatch commits TR-008, then the explicit S8 intake verb creates
// one InvestigationCase for the exact Finding set. TR-008 itself only moves
// the cursor; it must not create a per-Finding BUG draft.
func TestCT03913ObservationBatchHandoff(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}

	state := systemPlanningState(t, root, "design", 26)
	req039fixtures.SeedSealedObservationBatch(t, root, state)
	req039fixtures.EnsureStateRoot(state, root)
	writeSystemState(t, root, state)

	// The verification stage freezes the product baseline, so the hook is
	// driven with a Bash read (product writes are denied during S7).
	toolInput := map[string]any{"command": "go test ./..."}
	req039fixtures.RequireLifecycleTransition(t, runner, root, "ct13-tr008", "Bash", toolInput,
		"TR-008", "bug_resolution", "investigation", "CT-039-13/TR-008")

	state = req039fixtures.ReadState(t, root)
	var intakeStdout, intakeStderr strings.Builder
	if code := runner.Run(t, []string{
		"runtime", "investigation", "ingest", "--root", root,
		"--grouping-rationale", "the sealed S7 batch is the provisional investigation boundary",
	}, strings.NewReader(""), &intakeStdout, &intakeStderr); code != 0 {
		t.Fatalf("S8 InvestigationCase intake failed: code=%d stdout=%s stderr=%s", code, intakeStdout.String(), intakeStderr.String())
	}

	state = req039fixtures.ReadState(t, root)
	review := state["review"].(map[string]any)
	casePointer, ok := review["investigation"].(map[string]any)
	if !ok || casePointer == nil {
		t.Fatalf("TR-008 + investigation ingest must pin an InvestigationCase pointer: %#v", review)
	}
	if casePointer["status"] != "investigating" {
		t.Fatalf("InvestigationCase must start investigating, got %v", casePointer["status"])
	}
	entities := state["entities"].(map[string]any)
	bugs := entities["bugs"].([]any)
	if len(bugs) != 0 {
		t.Fatalf("S8 intake must not create a BUG draft before RepairContract approval, got %d", len(bugs))
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("TR-008 must not use manual transition CLI")
	}
}

// TestCT03921_OpenRoundDoesNotAdvanceSystem supersedes the legacy
// PTR-VERIFY-05 restart: an open round with pending required Claims commits
// nothing and the cursor stays at verification.running.
func TestCT03921_OpenRoundDoesNotAdvanceSystem(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "design", 40)
	req039fixtures.SeedReviewPlanRound(t, root, state)
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
	if committed, _ := qg["transition_committed"].(bool); committed {
		t.Fatalf("an open round must not commit any exit transition, qg=%v", qg)
	}
	after := req039fixtures.ReadState(t, root)
	lc, ph := req039fixtures.Lifecycle(after)
	if lc != "verification" || ph != "running" {
		t.Fatalf("open round must stay verification.running, got %s/%s", lc, ph)
	}
}
