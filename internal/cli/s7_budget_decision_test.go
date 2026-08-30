package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestS7BudgetDecisionRequiresStructuredDecisionFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "s7-budget-decision"}, strings.NewReader(""), &stdout, &stderr)

	if code != 2 {
		t.Fatalf("missing S7 budget decision inputs exit code = %d, want 2; stderr=%q", code, stderr.String())
	}
	for _, want := range []string{"--file", "increase_budget", "return_to_governance"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("S7 budget decision error missing %q: %q", want, stderr.String())
		}
	}
}

func TestS7BudgetDecisionReturnToGovernanceReachesPlanning(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}
	stateData, err := os.ReadFile(filepath.Join("..", "..", "internal", "schema", "assets", "loop-state.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["revision"] = 1
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["lifecycle"] = map[string]any{"state": "bug_resolution", "phase": "ready_for_full_review", "phase_revision": 1}
	state["review"].(map[string]any)["round"] = 5
	state["review"].(map[string]any)["plan"] = nil
	state["configuration"].(map[string]any)["repair"].(map[string]any)["max_full_review_rounds"] = 5
	state["evidence"] = []any{}
	stateBytes, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(stateBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	decisionPath := filepath.Join(root, "human-budget-decision.json")
	decision := map[string]any{
		"decision": "return_to_governance", "runtime_id": state["runtime_id"],
		"expected_revision": 1, "review_round": 5, "previous_max_full_review_rounds": 5,
		"new_max_full_review_rounds": 0,
		"reason":                     "the repeated findings indicate an architecture-level governance gap", "authorized_by": "user",
	}
	decisionBytes, err := json.MarshalIndent(decision, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decisionPath, append(decisionBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "s7-budget-decision", "--root", root, "--file", decisionPath,
		"--expected-revision", "1", "--actor", "user",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("return_to_governance exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode command result: %v; output=%s", err, stdout.String())
	}
	if result["next"] != "planning" {
		t.Fatalf("command result next=%v, want planning", result["next"])
	}
	updated, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var finalState map[string]any
	if err := json.Unmarshal(updated, &finalState); err != nil {
		t.Fatal(err)
	}
	if finalState["lifecycle"].(map[string]any)["state"] != "planning" {
		t.Fatalf("lifecycle after governance return = %#v", finalState["lifecycle"])
	}
	if len(finalState["evidence"].([]any)) != 1 {
		t.Fatalf("human decision evidence not retained: %#v", finalState["evidence"])
	}
	if finalState["review"].(map[string]any)["round"] != float64(0) {
		t.Fatalf("review round after governance return = %#v, want 0", finalState["review"])
	}
}

func TestS7BudgetDecisionIncreaseLeavesControllerOnAutomaticRetryPath(t *testing.T) {
	root := t.TempDir()
	state := writeS7BudgetCLIState(t, root, "bug_resolution", "ready_for_full_review", 5, 5)
	decisionPath := filepath.Join(root, "human-budget-decision.json")
	decision := map[string]any{
		"decision": "increase_budget", "runtime_id": state["runtime_id"],
		"expected_revision": 1, "review_round": 5, "previous_max_full_review_rounds": 5,
		"new_max_full_review_rounds": 8,
		"reason":                     "the repaired surface needs another complete S7 round", "authorized_by": "user",
	}
	decisionBytes, err := json.MarshalIndent(decision, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decisionPath, append(decisionBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "s7-budget-decision", "--root", root, "--file", decisionPath,
		"--expected-revision", "1", "--actor", "user",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("increase_budget exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode command result: %v; output=%s", err, stdout.String())
	}
	if result["pending_transition"] != "TR-012" {
		t.Fatalf("pending transition=%v, want TR-012", result["pending_transition"])
	}
	if !strings.Contains(result["next"].(string), "next PreToolUse retries TR-012 automatically") {
		t.Fatalf("increase result must disclose automatic retry path: %#v", result)
	}

	updated, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var finalState map[string]any
	if err := json.Unmarshal(updated, &finalState); err != nil {
		t.Fatal(err)
	}
	repair := finalState["configuration"].(map[string]any)["repair"].(map[string]any)
	if repair["max_full_review_rounds"] != float64(8) {
		t.Fatalf("max_full_review_rounds=%#v, want 8", repair["max_full_review_rounds"])
	}
	if len(finalState["evidence"].([]any)) != 1 {
		t.Fatalf("human decision evidence not retained: %#v", finalState["evidence"])
	}

	// After the same CAS commit, the normal next projection must no longer
	// stop at the exhausted budget. The next PreToolUse can retry TR-012.
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{"next", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("next projection after increase exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var next map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &next); err != nil {
		t.Fatalf("decode next projection: %v; output=%s", err, stdout.String())
	}
	if next["human_required"] != false {
		t.Fatalf("next projection must not remain human-gated after increase: %#v", next)
	}
}

func writeS7BudgetCLIState(t *testing.T, root, lifecycleState, phase string, round, max int) map[string]any {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}
	stateData, err := os.ReadFile(filepath.Join("..", "..", "internal", "schema", "assets", "loop-state.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["revision"] = 1
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["lifecycle"] = map[string]any{"state": lifecycleState, "phase": phase, "phase_revision": 1}
	state["review"].(map[string]any)["round"] = round
	state["review"].(map[string]any)["plan"] = nil
	state["configuration"].(map[string]any)["repair"].(map[string]any)["max_full_review_rounds"] = max
	state["evidence"] = []any{}
	stateBytes, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(stateBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return state
}
