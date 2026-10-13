package qualitygate_test

import (
	"context"
	"testing"

	"github.com/entroforge/go-system-builder/internal/qualitygate"
)

func TestPlanningGateRecoversFromSupersededEvidenceRegistration(t *testing.T) {
	for _, withReplacement := range []bool{false, true} {
		name := "without_qualified_replacement"
		if withReplacement {
			name = "with_qualified_replacement"
		}
		t.Run(name, func(t *testing.T) {
			input := planningDesignInput(t, true)
			current := input.Snapshot.State["evidence"].([]any)[0].(map[string]any)
			previous := make(map[string]any, len(current))
			for key, value := range current {
				previous[key] = value
			}
			// Both registrations reference the current envelope and hash, but
			// the previous registration no longer matches its evidence ID.
			previous["id"] = "ev-design-old"
			rows := []any{previous}
			if withReplacement {
				rows = append(rows, current)
			}
			input.Snapshot.State["evidence"] = rows

			result, err := newTestEvaluator(t).Evaluate(context.Background(), input)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if !withReplacement {
				if result.Status != qualitygate.StatusUnknown || !contains(result.Conflicts, "evidence:ev-design-old:schema") {
					t.Fatalf("unqualified registration must still block: status=%q conflicts=%v", result.Status, result.Conflicts)
				}
				if len(result.EvidenceRefs) != 0 {
					t.Fatalf("unqualified evidence refs = %v", result.EvidenceRefs)
				}
				return
			}
			if result.Status != qualitygate.StatusSatisfied || len(result.Conflicts) != 0 {
				t.Fatalf("replacement must satisfy gate: status=%q conflicts=%v missing=%v", result.Status, result.Conflicts, result.Missing)
			}
			if len(result.EvidenceRefs) != 1 || result.EvidenceRefs[0] != "ev-design" {
				t.Fatalf("evidence refs = %v, want only qualified replacement", result.EvidenceRefs)
			}
		})
	}
}
