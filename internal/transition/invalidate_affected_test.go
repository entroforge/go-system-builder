package transition_test

import (
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/impact"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestInvalidateAffectedEvidencePreservesRepairBatchEvidence(t *testing.T) {
	transition.InitActionRegistry()
	state := map[string]any{
		"baseline": map[string]any{"generation": 1},
		"evidence": []any{
			validEvidenceIndex("ev-delivery-pass", "delivery_review", []any{"internal/controller/cycle.go"}),
			validEvidenceIndex("ev-repair", "bug", nil),
			validEvidenceIndex("ev-change-impact", "change_impact", nil),
		},
	}
	fn, ok := transition.LookupAction("invalidate_affected_evidence")
	if !ok {
		t.Fatal("invalidate_affected_evidence not registered")
	}
	ctx := &transition.ActionContext{
		Spec: transition.TransitionSpec{ID: "PTR-BUG-05"},
		Evidence: map[string]string{
			"repair_record":        "ev-repair",
			"change_impact_record": "ev-change-impact",
		},
		Request: &transition.Request{
			AffectedPaths: []string{"internal/controller/cycle.go"},
		},
		OccurredAt: time.Now().UTC(),
	}
	if _, err := fn(state, ctx); err != nil {
		t.Fatalf("action: %v", err)
	}
	for _, id := range []string{"ev-repair", "ev-change-impact"} {
		entry := evidenceByID(state, id)
		if entry == nil {
			t.Fatalf("missing evidence %s", id)
		}
		if entry["status"] != "valid" {
			t.Errorf("%s status = %v, want valid", id, entry["status"])
		}
		if entry["invalidated_by"] != nil {
			t.Errorf("%s invalidated_by = %v, want nil", id, entry["invalidated_by"])
		}
	}
	delivery := evidenceByID(state, "ev-delivery-pass")
	if delivery == nil || delivery["status"] != "invalid" {
		t.Fatalf("scoped delivery pass should be invalidated, got %#v", delivery)
	}
}

func validEvidenceIndex(id, kind string, scope []any) map[string]any {
	if scope == nil {
		scope = []any{}
	}
	return map[string]any{
		"id": id, "kind": kind, "path": "evidence/" + id + ".json", "sha256": "abc",
		"status": "valid", "baseline_generation": 1, "review_round": 1,
		"produced_by": []any{"agent-1"}, "invalidated_by": nil,
		"responsibility_id": "Builder", "scope_refs": scope,
	}
}

func evidenceByID(state map[string]any, id string) map[string]any {
	raw, _ := state["evidence"].([]any)
	for _, item := range raw {
		entry, _ := item.(map[string]any)
		if entry != nil && entry["id"] == id {
			return entry
		}
	}
	return nil
}

func TestComputeImpactDoesNotInvalidateUnscopedRepairEvidence(t *testing.T) {
	state := map[string]any{
		"baseline": map[string]any{"generation": 1},
		"evidence": []any{
			validEvidenceIndex("ev-change-impact", "change_impact", nil),
		},
	}
	impacts := impact.ComputeImpact(state, []string{"internal/controller/cycle.go"})
	for _, item := range impacts {
		if item.EvidenceID == "ev-change-impact" {
			t.Fatal("unscoped change_impact must not be impacted by source path change")
		}
	}
}
