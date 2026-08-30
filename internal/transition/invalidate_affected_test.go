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

// TestComputeImpactInvalidatesUnscopedEvidence pins the RC-09 (S9-3)
// fail-closed semantics: an evidence entry that declares no scope_refs is
// full-surface sensitive, so ANY changed path invalidates it. The previous
// behavior (unscoped evidence can never match a rule and therefore never
// auto-invalidates) is exactly the S9-3 finding — an unrelated path change
// left an unscoped PASS evidence row "valid" forever.
func TestComputeImpactInvalidatesUnscopedEvidence(t *testing.T) {
	state := map[string]any{
		"baseline": map[string]any{"generation": 1},
		"evidence": []any{
			validEvidenceIndex("ev-change-impact", "change_impact", nil),
			validEvidenceIndex("ev-repair", "bug", nil),
			validEvidenceIndex("ev-scoped", "delivery_review", []any{"docs/requirements/REQ-039.md"}),
		},
	}
	impacts := impact.ComputeImpact(state, []string{"internal/controller/cycle.go"})
	invalidated := map[string]string{}
	for _, item := range impacts {
		invalidated[item.EvidenceID] = item.Rule
	}
	for _, id := range []string{"ev-change-impact", "ev-repair"} {
		rule, ok := invalidated[id]
		if !ok {
			t.Fatalf("unscoped evidence %s must be invalidated by an unrelated path change (RC-09 S9-3 fail-closed)", id)
		}
		if rule != "unscoped_evidence" {
			t.Fatalf("evidence %s invalidation rule = %q, want unscoped_evidence", id, rule)
		}
	}
	if _, ok := invalidated["ev-scoped"]; ok {
		t.Fatal("evidence scoped to docs/requirements must not be impacted by a source path change")
	}
}
