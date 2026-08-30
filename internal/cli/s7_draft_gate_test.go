// s7_draft_gate_test.go proves the disclosure gate added to
// `loop-harness s7 draft`: the subcommand must NOT emit a ReviewPlan
// scaffold before the lifecycle has reached `verification`, and the
// error message must name the current stage, the legal entry path
// (TR-006/TR-012/TR-016), and one next action. The two paths tested
// correspond to the two distinct refusal modes:
//
//  1. round < 1 — the S6 batch has not committed; S7 is not open yet.
//  2. round >= 1 but stage != verification — the round was opened
//     but the lifecycle cursor is not on `verification`; the plan
//     would be register-rejected.
//
// Both messages must follow D3 (specific missing fact + one next
// action), as required by the controller recovery protocol.
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// writeDraftGateFixture builds a minimal loop-state.json with the
// caller-supplied lifecycle and review section so each test path
// can stand up its own non-verification cursor.
func writeDraftGateFixture(t *testing.T, root string, lifecycle map[string]any, review map[string]any) {
	t.Helper()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	state["lifecycle"] = lifecycle
	state["review"] = review
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestS7DraftRefusesBeforeRoundOpens asserts the round < 1 path:
// S7 has not opened yet (lifecycle is still `building` with
// review.round=0) and the disclosure message names the legal entry
// path TR-006 plus the recovery hint. A planner reading this output
// must NOT find encouragement to handcraft the runtime.
func TestS7DraftRefusesBeforeRoundOpens(t *testing.T) {
	root := t.TempDir()
	writeDraftGateFixture(t, root,
		map[string]any{"state": "building", "phase": "committed", "phase_revision": 0},
		map[string]any{"round": 0, "clean_round": nil},
	)

	var stdout, stderr bytes.Buffer
	code := runS7Command([]string{"draft", "--root", root}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("s7 draft must refuse when round < 1; stdout:\n%s", stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"current stage is building",
		"TR-006",
		"missing work",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("disclosure missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "the current change surface") {
		t.Errorf("refusal must not emit a placeholder draft: %s", out)
	}
}

// TestS7DraftRefusesNonVerificationStage asserts the open-round-but-
// wrong-stage path: review.round is 1 (a round was claimed by some
// test demo), but the lifecycle is `bug_resolution`. The disclosure
// message must name the rejection the register-verb would produce
// (so the agent does not go on to "register anyway and wonder why
// it failed") and must still point at one next action.
func TestS7DraftRefusesNonVerificationStage(t *testing.T) {
	root := t.TempDir()
	writeDraftGateFixture(t, root,
		map[string]any{"state": "bug_resolution", "phase": "investigation", "phase_revision": 0},
		map[string]any{"round": 1, "clean_round": nil},
	)

	var stdout, stderr bytes.Buffer
	code := runS7Command([]string{"draft", "--root", root}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("s7 draft must refuse when stage != verification; stdout:\n%s", stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"current stage is bug_resolution",
		"verification",
		"Finish the current stage",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("disclosure missing %q:\n%s", want, out)
		}
	}
	// A failed draft must not leak through to stdout; the file should
	// not exist (the subcommand was told to print, not write).
	files, err := filepath.Glob(filepath.Join(root, "plan*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		contents, _ := os.ReadFile(path)
		t.Errorf("unexpected draft file at %s:\n%s", path, string(contents))
	}
}

// TestS7DraftLifecycleCursorDefaultsMissing is a defensive contract:
// the disclosure helper never panics on a state map without a
// `lifecycle` block. The base loop-state fixture intentionally omits
// the lifecycle in some test paths; the readable error must still
// surface a stage token.
func TestS7DraftLifecycleCursorDefaultsMissing(t *testing.T) {
	root := t.TempDir()
	writeDraftGateFixture(t, root,
		map[string]any{"state": "building", "phase": "committed", "phase_revision": 0},
		map[string]any{"round": 0, "clean_round": nil},
	)
	// Now strip the lifecycle block to prove the helper's default.
	data, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	delete(state, "lifecycle")
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runS7Command([]string{"draft", "--root", root}, &stdout, &stderr); code == 0 {
		t.Fatalf("refusal expected even without a lifecycle block")
	}
	if !strings.Contains(stdout.String(), "current stage is") {
		t.Errorf("disclosure must render a stage token even when lifecycle is absent:\n%s", stdout.String())
	}
}
