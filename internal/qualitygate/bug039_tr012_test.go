package qualitygate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestGateTargetedReverificationCompleteAcceptsRepairChangeImpact(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	registry, err := NewRegistry(catalog)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	evaluator := NewEvaluator(registry)
	input := targetedReverificationCompleteInput(t)

	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != StatusSatisfied {
		t.Fatalf("status = %q, want satisfied (missing=%v conflicts=%v refs=%v)",
			result.Status, result.Missing, result.Conflicts, result.EvidenceRefs)
	}
	for _, want := range []string{"ev-tgt-pass", "ev-change-impact"} {
		if !containsString(result.EvidenceRefs, want) {
			t.Fatalf("evidence refs = %#v, want %q included", result.EvidenceRefs, want)
		}
	}
}

func targetedReverificationCompleteInput(t *testing.T) Input {
	t.Helper()
	files := map[string][]byte{}
	add := func(id, kind, responsibility, conclusion string, reviewRound int) map[string]any {
		envelope := map[string]any{
			"schema_version":          "1.0.0",
			"evidence_id":             id,
			"kind":                    kind,
			"runtime_id":              "loop-test",
			"baseline_generation":     1,
			"producer_agent_id":       "agent-1",
			"producer_responsibility": responsibility,
			"subject_refs":            []any{},
			"conclusion":              conclusion,
		}
		if reviewRound > 0 {
			envelope["review_round"] = reviewRound
		}
		data, _ := json.Marshal(envelope)
		path := "evidence/" + id + ".json"
		files[path] = data
		idx := map[string]any{
			"id": id, "kind": kind, "path": path, "sha256": sha256Hex(data),
			"status": "valid", "baseline_generation": 1,
			"produced_by": []any{"agent-1"}, "invalidated_by": nil,
			"responsibility_id": responsibility,
		}
		if reviewRound > 0 {
			idx["review_round"] = reviewRound
		} else {
			idx["review_round"] = nil
		}
		return idx
	}
	evidence := []any{
		add("ev-tgt-pass", "targeted_reverification", "Original Finder", "pass", 1),
		add("ev-change-impact", "change_impact", "BUILD-WORK-PACKAGE", "recorded", 0),
	}
	return Input{
		Snapshot: runtime.Snapshot{
			Revision: 1,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "bug_resolution", "phase": "ready_for_full_review"},
				"baseline":   map[string]any{"generation": 1},
				"review":     map[string]any{"round": 1},
				"documents":  []any{},
				"evidence":   evidence,
			},
		},
		GateID:       "GATE-TARGETED-REVERIFICATION-COMPLETE",
		TransitionID: "TR-012",
		Files:        memoryFileView(files),
	}
}
