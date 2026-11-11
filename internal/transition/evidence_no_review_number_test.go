package transition

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTransitionEvidenceIgnoresHistoricalReviewNumber(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "evidence.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ id, slot, kind string }{
		{"TR-013", "change_impact_record", "change_impact"},
		{"TR-015", "acceptance_record", "acceptance"},
		{"TR-016", "acceptance_record", "acceptance"},
		{"TR-017", "release_audit_record", "release_audit"},
		{"TR-018", "release_audit_record", "release_audit"},
	} {
		t.Run(route.id, func(t *testing.T) {
			for _, number := range []any{nil, float64(7), "old unrelated annotation"} {
				row := map[string]any{"id": "ev-original", "path": "evidence.json", "kind": route.kind, "status": "valid", "baseline_generation": 1}
				if number != nil {
					row["review_round"] = number
				}
				state := map[string]any{"baseline": map[string]any{"generation": 1}, "review": map[string]any{"round": 2}, "evidence": []any{row}}
				spec := TransitionSpec{ID: route.id, RequiredEvidence: []string{route.slot}}
				request := Request{Context: context.Background(), Actor: "Main", Evidence: map[string]string{route.slot: "ev-original"}}
				if err := validateRequest(root, state, spec, request); err != nil {
					t.Fatalf("historical value %v rejected original responsibility: %v", number, err)
				}
				row["status"] = "invalid"
				if err := validateRequest(root, state, spec, request); err == nil {
					t.Fatal("invalid original evidence was accepted")
				}
				row["status"], row["kind"] = "valid", "human_decision"
				if err := validateRequest(root, state, spec, request); err == nil {
					t.Fatal("wrong original evidence kind was accepted")
				}
			}
		})
	}
}
