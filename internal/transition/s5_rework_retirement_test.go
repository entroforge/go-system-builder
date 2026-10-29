package transition

import "testing"

func TestTR004RetiresExactS5BatchAndInvalidatesAllItsReviews(t *testing.T) {
	currentExecution := map[string]any{
		"assignment_id": "review-assignment", "runtime_id": "loop-REQ-001", "baseline_generation": float64(3),
		"execution_generation": float64(1), "agent_id": "reviewer-one", "purpose": "document_review",
		"stage_entry": float64(7), "status": "ready", "work_packet_id": "packet-one",
	}
	state := map[string]any{
		"schema_version": "1.3.0", "runtime_id": "loop-REQ-001",
		"entities": map[string]any{
			"teams": []any{
				map[string]any{"id": "team-s5", "kind": "document_verifier", "status": "active", "packet_generation": float64(3), "packet_stage_entry": float64(7), "agent_ids": []any{"reviewer-one"}},
				map[string]any{"id": "team-old", "kind": "document_verifier", "status": "active", "packet_generation": float64(2), "packet_stage_entry": float64(7), "agent_ids": []any{"reviewer-old"}},
			},
			"agents": []any{
				map[string]any{"id": "reviewer-one", "role": "document-verifier", "team_id": "team-s5", "state": "active", "last_lifecycle_event": "work_started"},
				map[string]any{"id": "reviewer-old", "role": "document-verifier", "team_id": "team-old", "state": "active", "last_lifecycle_event": "work_started"},
			},
			"work_packets": []any{map[string]any{
				"template_id": "s5-document-review-v1", "packet_id": "packet-one",
				"binding": map[string]any{"runtime_id": "loop-REQ-001", "generation": float64(3), "stage_entry": float64(7)},
			}},
			"submissions": []any{map[string]any{"packet_id": "packet-one", "evidence_id": "review-current"}},
		},
		"evidence": []any{
			map[string]any{"id": "review-current", "kind": "document_review", "status": "valid"},
			map[string]any{"id": "review-fix", "kind": "document_review", "status": "valid"},
			map[string]any{"id": "review-old", "kind": "document_review", "status": "valid"},
			map[string]any{"id": "other-current", "kind": "planning_task", "status": "valid"},
		},
		"workspace": map[string]any{
			"executions": map[string]any{
				"review-assignment":  currentExecution,
				"old-assignment":     map[string]any{"assignment_id": "old-assignment", "runtime_id": "loop-REQ-001", "baseline_generation": float64(2), "purpose": "document_review", "stage_entry": float64(7), "status": "ready"},
				"builder-assignment": map[string]any{"assignment_id": "builder-assignment", "runtime_id": "loop-REQ-001", "baseline_generation": float64(3), "purpose": "builder", "stage_entry": float64(7), "status": "ready"},
			},
			"execution_history": []any{map[string]any{"assignment_id": "prior", "status": "retired"}},
		},
	}
	ctx := &ActionContext{
		Spec:     TransitionSpec{ID: "TR-004"},
		From:     map[string]any{"state": "document_verification", "phase_revision": float64(7), "baseline_generation": float64(3)},
		Evidence: map[string]string{"document_review_record": "review-fix"},
	}
	result, err := actionInvalidateConsumedReviewEvidence(state, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !result.MutationApplied {
		t.Fatal("TR-004 did not record its batch retirement")
	}
	entities := state["entities"].(map[string]any)
	team := entities["teams"].([]any)[0].(map[string]any)
	if team["status"] != "retired" {
		t.Fatalf("current S5 team status = %v", team["status"])
	}
	if got := entities["teams"].([]any)[1].(map[string]any)["status"]; got != "active" {
		t.Fatalf("different-generation S5 team was changed: %v", got)
	}
	agent := entities["agents"].([]any)[0].(map[string]any)
	if agent["state"] != "cancelled" || agent["last_lifecycle_event"] != "shutdown_approved" {
		t.Fatalf("current reviewer responsibility was not cancelled: %#v", agent)
	}
	if got := entities["agents"].([]any)[1].(map[string]any)["state"]; got != "active" {
		t.Fatalf("different-generation reviewer was changed: %v", got)
	}

	workspace := state["workspace"].(map[string]any)
	executions := workspace["executions"].(map[string]any)
	if _, found := executions["review-assignment"]; found {
		t.Fatal("current S5 execution remained active")
	}
	if _, found := executions["old-assignment"]; !found {
		t.Fatal("different-generation execution was removed")
	}
	history := workspace["execution_history"].([]any)
	if len(history) != 2 || history[1].(map[string]any)["status"] != "retired" || history[1].(map[string]any)["work_packet_id"] != "packet-one" {
		t.Fatalf("retired execution history did not retain the original binding: %#v", history)
	}

	for _, id := range []string{"review-current", "review-fix"} {
		if got := evidenceStatus(entitiesEvidence(state), id); got != "invalid" {
			t.Fatalf("current review %s status = %v", id, got)
		}
	}
	if got := evidenceStatus(entitiesEvidence(state), "review-old"); got != "valid" {
		t.Fatalf("noncurrent review was invalidated: %v", got)
	}
	if got := evidenceStatus(entitiesEvidence(state), "other-current"); got != "valid" {
		t.Fatalf("non-review evidence was invalidated: %v", got)
	}
}

func entitiesEvidence(state map[string]any) []any { return state["evidence"].([]any) }

func evidenceStatus(rows []any, id string) any {
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["id"] == id {
			return row["status"]
		}
	}
	return nil
}
