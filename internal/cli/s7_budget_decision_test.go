package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// A real human decision may use the original governance transition without
// treating historical review numbers as a new budget or authorization.
func TestS7GovernanceHumanDecisionOriginalTransitionPreservesHistory(t *testing.T) {
	root := s7BudgetHookRoot(t)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	state := stageSnapshot(t, root).State
	rel := "docs/governance-decision.md"
	if err := os.WriteFile(filepath.Join(root, rel), []byte("Actual human decision: return to governance."), 0600); err != nil {
		t.Fatal(err)
	}
	recorded, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: -1, ID: "human-governance-decision", Kind: "human_decision", Path: rel,
		ProducedBy: []string{"user"}, ScopeRefs: []string{"runtime_governance:" + state["runtime_id"].(string)},
		Validator: semantic.RuntimeCandidateValidator{},
	})
	if err != nil {
		t.Fatalf("record actual human decision: %v", err)
	}
	next, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: "GTR-006", ExpectedRevision: recorded.Revision, Actor: "user",
		Evidence: map[string]string{"human_decision_record": "human-governance-decision"}, Files: fileview.Disk{Root: root},
	})
	if err != nil {
		t.Fatalf("consume actual human governance decision: %v", err)
	}
	if next.State["lifecycle"].(map[string]any)["state"] != "planning" {
		t.Fatalf("governance did not enter planning: %#v", next.State["lifecycle"])
	}
	review := next.State["review"].(map[string]any)
	if review["round"] != float64(5) || review["plan"] != nil || len(review["claims"].(map[string]any)) != 0 || len(review["assignments"].(map[string]any)) != 0 {
		t.Fatalf("governance changed history or kept retired review work: %#v", review)
	}
	evidence := next.State["evidence"].([]any)
	if len(evidence) != 1 || evidence[0].(map[string]any)["id"] != "human-governance-decision" || evidence[0].(map[string]any)["status"] != "valid" {
		t.Fatalf("original human decision not retained: %#v", evidence)
	}
}

func writeS7BudgetCLIState(t *testing.T, root, lifecycleState, phase string, round, max int) map[string]any {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "control", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "control", "loop-definition.json"), definition, 0o644); err != nil {
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
