package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestApplyS7BudgetDecisionAtomicallyExtendsBudgetAndRecordsEvidence(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)
	patchS7BudgetRuntime(t, statePath, "bug_resolution", "ready_for_full_review", 5, 5)

	decisionPath := "s7-budget-decision.json"
	writeS7Decision(t, filepath.Join(root, decisionPath), runtime.S7BudgetDecision{
		Decision:                    runtime.S7BudgetDecisionIncrease,
		RuntimeID:                   "loop-REQ-002-example",
		ReviewRound:                 5,
		PreviousMaxFullReviewRounds: 5,
		NewMaxFullReviewRounds:      8,
		Reason:                      "S7 found a broad cross-layer regression surface and needs a complete additional round.",
		AuthorizedBy:                "user",
	})

	receipt, err := runtime.ApplyS7BudgetDecision(root, statePath, journalPath, runtime.S7BudgetDecisionRequest{
		ExpectedRevision: -1,
		DecisionPath:     decisionPath,
		Actor:            "user",
		Validator:        testCandidateValidator(),
	})
	if err != nil {
		t.Fatalf("ApplyS7BudgetDecision failed: %v", err)
	}
	if receipt.Snapshot.Revision != 2 {
		t.Fatalf("revision = %d, want 2", receipt.Snapshot.Revision)
	}
	repair := receipt.Snapshot.State["configuration"].(map[string]any)["repair"].(map[string]any)
	if repair["max_full_review_rounds"] != 8 {
		t.Fatalf("max_full_review_rounds = %#v, want 8", repair["max_full_review_rounds"])
	}
	last := repair["last_budget_decision"].(map[string]any)
	if last["decision"] != runtime.S7BudgetDecisionIncrease || last["evidence_id"] != receipt.EvidenceID {
		t.Fatalf("last_budget_decision = %#v", last)
	}
	evidence := receipt.Snapshot.State["evidence"].([]any)
	if len(evidence) != 1 {
		t.Fatalf("evidence count = %d, want 1", len(evidence))
	}
	item := evidence[0].(map[string]any)
	if item["kind"] != "human_decision" || item["path"] != "s7-budget-decision.json" {
		t.Fatalf("decision evidence = %#v", item)
	}
	if !containsString(item["scope_refs"].([]string), "runtime_budget:loop-REQ-002-example") {
		t.Fatalf("decision evidence scope_refs = %#v", item["scope_refs"])
	}
}

func TestApplyS7BudgetDecisionRejectsLowerBudgetWithoutMutation(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)
	patchS7BudgetRuntime(t, statePath, "bug_resolution", "ready_for_full_review", 5, 5)
	decisionPath := "decision.json"
	writeS7Decision(t, filepath.Join(root, decisionPath), runtime.S7BudgetDecision{
		Decision:                    runtime.S7BudgetDecisionIncrease,
		RuntimeID:                   "loop-REQ-002-example",
		ExpectedRevision:            1,
		ReviewRound:                 5,
		PreviousMaxFullReviewRounds: 5,
		NewMaxFullReviewRounds:      4,
		Reason:                      "invalid lower budget",
		AuthorizedBy:                "user",
	})
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ApplyS7BudgetDecision(root, statePath, journalPath, runtime.S7BudgetDecisionRequest{
		ExpectedRevision: 1, DecisionPath: decisionPath, Actor: "user", Validator: testCandidateValidator(),
	}); err == nil || !strings.Contains(err.Error(), "greater than current limit") {
		t.Fatalf("lower budget error = %v", err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected lower budget decision mutated runtime")
	}
}

func TestApplyS7BudgetDecisionRejectsSymlinkedDecisionPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)
	patchS7BudgetRuntime(t, statePath, "bug_resolution", "ready_for_full_review", 5, 5)
	decision := runtime.S7BudgetDecision{
		Decision: runtime.S7BudgetDecisionIncrease, RuntimeID: "loop-REQ-002-example",
		ExpectedRevision: 1, ReviewRound: 5, PreviousMaxFullReviewRounds: 5,
		NewMaxFullReviewRounds: 8, Reason: "symlink escape must fail", AuthorizedBy: "user",
	}
	writeS7Decision(t, filepath.Join(outside, "decision.json"), decision)
	if err := os.Symlink(outside, filepath.Join(root, "linked-evidence")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := runtime.ApplyS7BudgetDecision(root, statePath, journalPath, runtime.S7BudgetDecisionRequest{
		ExpectedRevision: 1, DecisionPath: "linked-evidence/decision.json", Actor: "user", Validator: testCandidateValidator(),
	}); err == nil || !strings.Contains(err.Error(), "within repository") {
		t.Fatalf("symlinked decision path must be rejected, got %v", err)
	}
}

func patchS7BudgetRuntime(t *testing.T, path, lifecycleState, phase string, round, max int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["lifecycle"] = map[string]any{"state": lifecycleState, "phase": phase, "phase_revision": 1}
	state["review"].(map[string]any)["round"] = round
	state["configuration"].(map[string]any)["repair"].(map[string]any)["max_full_review_rounds"] = max
	updated, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(updated, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeS7Decision(t *testing.T, path string, decision runtime.S7BudgetDecision) {
	t.Helper()
	data, err := json.MarshalIndent(decision, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
