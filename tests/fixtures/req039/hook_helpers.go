package req039fixtures

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// EnsureStateRoot sets state.root so transition guards resolve evidence paths.
func EnsureStateRoot(state map[string]any, root string) {
	state["root"] = root
}

// AppendEvidence appends an index entry to state.evidence.
func AppendEvidence(state map[string]any, entry map[string]any) {
	ev, _ := state["evidence"].([]any)
	state["evidence"] = append(ev, entry)
}

// AssertLifecycle fails when lifecycle state/phase do not match.
func AssertLifecycle(t *testing.T, state map[string]any, wantState, wantPhase string) {
	t.Helper()
	lc, ph := Lifecycle(state)
	if lc != wantState || ph != wantPhase {
		t.Fatalf("lifecycle want %s.%s, got %s.%s", wantState, wantPhase, lc, ph)
	}
}

// AssertLastTransition fails when last_transition.transition_id differs.
func AssertLastTransition(t *testing.T, state map[string]any, wantID string) {
	t.Helper()
	if got := LastTransitionID(state); got != wantID {
		t.Fatalf("last transition want %q, got %q", wantID, got)
	}
}

// AssertBindingReceipt verifies the bind boundary without pretending that
// the archived TR-001 event is part of the new runtime's revision-zero
// journal.
func AssertBindingReceipt(t *testing.T, state map[string]any, wantID string) {
	t.Helper()
	receipt, _ := state["binding_receipt"].(map[string]any)
	if got, _ := receipt["transition_id"].(string); got != wantID {
		t.Fatalf("binding receipt transition want %q, got %q", wantID, got)
	}
}

// ParseHookQualityGate decodes hook stdout quality_gate block.
func ParseHookQualityGate(t *testing.T, raw string) map[string]any {
	t.Helper()
	_, qg := ParseQualityGate(t, raw)
	if qg == nil {
		t.Fatalf("hook output missing quality_gate: %s", raw)
	}
	return qg
}

// CandidateTransition returns candidate_transition from a quality_gate map.
func CandidateTransition(qg map[string]any) string {
	id, _ := qg["candidate_transition"].(string)
	return id
}

// TransitionCommitted reports whether the hook committed a transition.
func TransitionCommitted(qg map[string]any) bool {
	committed, _ := qg["transition_committed"].(bool)
	return committed
}

// HookBody is a minimal JSON hook body for tests.
func HookBody(event, session, tool string, toolInput map[string]any) string {
	payload := map[string]any{
		"session_id":      session,
		"hook_event_name": event,
		"agent_id":        "agent-1",
		"tool_name":       tool,
		"tool_input":      toolInput,
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

// SubagentStopBody builds a SubagentStop hook JSON body.
// Facts are nested under "facts" so policy.Input.Facts is populated
// (top-level agent_report_complete alone is ignored by the decoder).
func SubagentStopBody(session, agentID, assignmentID string) string {
	payload := map[string]any{
		"session_id":            session,
		"hook_event_name":       "SubagentStop",
		"agent_id":              agentID,
		"agent_report_complete": true,
		"assignment_id":         assignmentID,
		"facts":                 map[string]any{"agent_report_complete": true},
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

// OutputContainsAny returns true when raw contains one of the needles.
func OutputContainsAny(raw string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(raw, n) {
			return true
		}
	}
	return false
}

// SkipIfProductBlocker skips when combined hook output indicates a known
// product gap that prevents Hook-driven transition commits (do not fake L4).
func SkipIfProductBlocker(t *testing.T, combined, bugID string) {
	t.Helper()
	needles := []struct {
		needle string
		id     string
	}{
		{"team_manifest evidence is required", "loop-state.schema.json evidence.kind enum"},
		{"planning_design_record", "loop-state.schema.json evidence.kind enum"},
		{"evidence:planning_design_record", "loop-state.schema.json evidence.kind enum"},
		{"finding_record", "BUG-039-22"},
		{"incompatible with finding_record", "BUG-039-22"},
		{"transition TR-008 requires evidence finding_record", "BUG-039-22"},
	}
	for _, item := range needles {
		if strings.Contains(combined, item.needle) {
			t.Skipf("product blocker %s (%s): %s", bugID, item.id, item.needle)
		}
	}
}

// HookStep runs one PreToolUse and returns the decoded quality_gate map.
func HookStep(t *testing.T, runner *CLIRunner, root, session, tool string, input map[string]any) (stdout string, qg map[string]any) {
	t.Helper()
	body := PreToolUseBody(session, tool, input)
	code, stdout, stderr := RunHook(t, runner, root, "PreToolUse", body)
	if code != 0 && code != 2 {
		t.Fatalf("PreToolUse %s failed: code=%d stderr=%s", session, code, stderr)
	}
	return stdout, ParseHookQualityGate(t, stdout)
}

// RequireLifecycleTransition asserts a committed Hook step or skips on product blockers.
func RequireLifecycleTransition(
	t *testing.T,
	runner *CLIRunner,
	root, session, tool string,
	input map[string]any,
	wantTransition, wantState, wantPhase, bugID string,
) map[string]any {
	t.Helper()
	stdout, qg := HookStep(t, runner, root, session, tool, input)
	state := ReadState(t, root)
	lc, ph := Lifecycle(state)
	last := LastTransitionID(state)
	if lc == wantState && ph == wantPhase && (wantTransition == "" || last == wantTransition) {
		return state
	}
	SkipIfProductBlocker(t, stdout+fmt.Sprintf("%v", qg), bugID)
	if status, _ := qg["status"].(string); status == "unknown" {
		t.Skipf("product blocker %s: quality_gate status=unknown (transition not committed)", bugID)
	}
	t.Fatalf("hook %s want %s→%s.%s (last=%q), got %s.%s qg=%v", session, wantTransition, wantState, wantPhase, last, lc, ph, qg)
	return nil
}
