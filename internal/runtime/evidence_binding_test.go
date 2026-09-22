package runtime

import (
	"encoding/json"
	"testing"
)

func TestBoundDocumentRegistrationBindings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any, *EvidenceRequest)
		want   bool
	}{
		{"valid", nil, true},
		{"wrong ID", func(e map[string]any, r *EvidenceRequest) { r.ID = "other" }, false},
		{"wrong runtime", func(e map[string]any, r *EvidenceRequest) { e["runtime_id"] = "other" }, false},
		{"wrong generation", func(e map[string]any, r *EvidenceRequest) { e["baseline_generation"] = 2 }, false},
		{"wrong producer", func(e map[string]any, r *EvidenceRequest) { e["producer_agent_id"] = "other" }, false},
		{"wrong responsibility", func(e map[string]any, r *EvidenceRequest) { r.ResponsibilityID = "other" }, false},
		{"wrong kind", func(e map[string]any, r *EvidenceRequest) { e["kind"] = "qa" }, false},
		{"missing schema", func(e map[string]any, r *EvidenceRequest) { delete(e, "schema_version") }, false},
		{"registration round", func(e map[string]any, r *EvidenceRequest) { n := 3; r.ReviewRound = &n }, false},
		{"envelope round", func(e map[string]any, r *EvidenceRequest) { e["review_round"] = 3 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := map[string]any{"runtime_id": "loop-test", "bound_req": map[string]any{"id": "REQ-1"}, "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "document_verification"}}
			r := EvidenceRequest{ID: "review-r3", Kind: "document_review", ProducedBy: []string{"dv"}, ResponsibilityID: "DV-SPEC-CONSISTENCY"}
			e := map[string]any{"schema_version": "1.0.0", "evidence_id": r.ID, "kind": r.Kind, "runtime_id": "loop-test", "baseline_generation": 1, "producer_agent_id": "dv", "producer_responsibility": r.ResponsibilityID}
			if tc.change != nil {
				tc.change(e, &r)
			}
			b, _ := json.Marshal(e)
			err := validateDocumentRegistration(s, r, b)
			if (err == nil) != tc.want {
				t.Fatalf("err=%v", err)
			}
			if err := validateDocumentRegistration(s, r, []byte("not json")); err == nil {
				t.Fatal("accepted malformed envelope")
			}
		})
	}
}
