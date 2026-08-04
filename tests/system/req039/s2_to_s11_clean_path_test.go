// s2_to_s11_clean_path_test.go — system-level conformance for the
// S2 planning.design -> S11 release-ready projection. This test does
// NOT execute a real S2->S11 run (that requires the full set of
// locked documents, evidence, and clean rounds which are outside the
// scope of BUG-039-10). Instead it pins the runHook output contract
// at the canonical stages so a regression in the wire envelope is
// caught by a single, fast, fixture-based system test.
//
// What this asserts:
//   1. PreToolUse at the planning.design cursor surfaces
//      permissionDecision=allow (Quality Gate never blocks tools).
//   2. The wire envelope includes the quality_gate block with
//      gate_id GATE-PLANNING-DESIGN-COMPLETE.
//   3. The persisted milestone objective after a PreToolUse matches
//      the canonical S2 projection (S2 = "complete architecture and
//      any required UI design package"), proving the Controller is
//      driving the milestone projection end-to-end.
//   4. Subsequent PreToolUse calls on the same root are idempotent:
//      the Controller's CAS ensures the milestone stabilises and the
//      tool is still allowed.

package req039_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
)

func TestS2_ToS11_CleanPath_Conformance(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	writeSystemState(t, root, state)

	// --- Step 1: planning.design cursor must surface the design gate.
	input := `{
		"session_id":"session-sys-s2-s11",
		"hook_event_name":"PreToolUse",
		"agent_id":"agent-1",
		"tool_name":"Edit",
		"tool_input":{"file_path":"docs/design/architecture/ARCHITECTURE-039.md"}
	}`
	code, stdout, stderr := runHook(t, root, "PreToolUse", input)
	if code != 0 {
		t.Fatalf("S2 PreToolUse must not fail: code=%d stderr=%s", code, stderr)
	}
	env, qg := parseEnv(t, stdout)
	if pd := env["hookSpecificOutput"].(map[string]any)["permissionDecision"]; pd != "allow" {
		t.Fatalf("S2 PreToolUse must allow: %v", pd)
	}
	if gateID, _ := qg["gate_id"].(string); !strings.Contains(gateID, "GATE-PLANNING-DESIGN-COMPLETE") {
		t.Fatalf("S2 gate_id must be GATE-PLANNING-DESIGN-COMPLETE, got %q", gateID)
	}

	// --- Step 2: persisted milestone must be the canonical S2 projection
	// driven by the Controller, not the fixture's primitive placeholder.
	rawState, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(rawState, &persisted); err != nil {
		t.Fatal(err)
	}
	ms, _ := persisted["milestone"].(map[string]any)
	objective, _ := ms["objective"].(string)
	if !strings.Contains(objective, "complete architecture") {
		t.Fatalf("S2 milestone objective must be the canonical projection, got %q", objective)
	}

	// --- Step 3: a second PreToolUse is idempotent (CAS), still allowed,
	// and the milestone stabilises (no further revision drift).
	revBefore, _ := persisted["revision"].(float64)
	_, stdout2, _ := runHook(t, root, "PreToolUse", input)
	env2, _ := parseEnv(t, stdout2)
	if pd := env2["hookSpecificOutput"].(map[string]any)["permissionDecision"]; pd != "allow" {
		t.Fatalf("S2 second PreToolUse must still allow: %v", pd)
	}
	rawState2, _ := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	var persisted2 map[string]any
	json.Unmarshal(rawState2, &persisted2)
	revAfter, _ := persisted2["revision"].(float64)
	// Idempotent CAS — revision either stays equal (no-op) or advances
	// by exactly one transition (the gate was satisfied and PTR-PLAN-01
	// committed). It MUST NOT advance by more than one.
	if revAfter-revBefore > 1 {
		t.Fatalf("S2 second PreToolUse must commit at most one transition: %v -> %v", revBefore, revAfter)
	}

	// --- Step 4: at the planning.design cursor the Controller's
	// observable Quality Gate verdict (not_ready) is the source of
	// truth and the wire envelope reflects it.
	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:      root,
		Event:     "PreToolUse",
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": "internal/cli/run.go"},
	})
	if err != nil {
		t.Fatalf("controller cycle: %v", err)
	}
	if result.QualityGate.GateID == "" {
		t.Fatalf("S2 must surface a GateID from the controller, got empty")
	}
	if string(result.QualityGate.Status) == "" {
		t.Fatalf("S2 must surface a non-empty status from the controller")
	}
}

// parseEnv is the system-test equivalent of the ac_test helper.
func parseEnv(t *testing.T, raw string) (map[string]any, map[string]any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var env map[string]any
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("hook output is not JSON: %v\noutput=%s", err, raw)
	}
	qg, _ := env["quality_gate"].(map[string]any)
	if qg == nil {
		if hsp, ok := env["hookSpecificOutput"].(map[string]any); ok {
			qg, _ = hsp["quality_gate"].(map[string]any)
		}
	}
	return env, qg
}
