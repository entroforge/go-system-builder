package schema_test

import (
	"fmt"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// The S7 examples double as the canonical scaffolds reviewers and planners
// copy; they must always validate against the embedded schemas.
func TestReviewPlanExampleValidates(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	if err := validator.ValidateEmbedded("review-plan.schema.json", "review-plan.example.json"); err != nil {
		t.Fatalf("review-plan example: %v", err)
	}
}

func TestReviewResultExampleValidates(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	if err := validator.ValidateEmbedded("review-result.schema.json", "review-result.example.json"); err != nil {
		t.Fatalf("review-result example: %v", err)
	}
}

// The per-mode encounter discrimination is what keeps a code-inspection
// Finding from having to fake a user journey (L3-S7 §3.6).
func TestFindingEncounterModeDiscrimination(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	base := `{
	  "schema_version": "1.0.0",
	  "finding_id": "finding-x-1",
	  "claim_id": "claim-x-1",
	  "lens": "e2e",
	  "severity": "P1",
	  "expected": "the save persists",
	  "authority_refs": ["REQ-001"],
	  "observed": "the value is gone after refresh",
	  "observation_mode": "user_flow",
	  "reproducibility": "always",
	  "evidence_refs": ["trace.jsonl"],
	  "encounter": %s
	}`
	// user_flow without entrypoint/timeline/terminal_state must fail.
	bad := `{"journey_summary": "enter -> edit -> save -> refresh", "wall_action": "click save", "first_bad_checkpoint": "value empty"}`
	if err := validator.ValidateBytes("finding.schema.json", []byte(fmt.Sprintf(base, bad))); err == nil {
		t.Fatal("user_flow finding without entrypoint/timeline/terminal_state must be rejected")
	}
	good := `{
	  "journey_summary": "enter -> edit -> save -> refresh",
	  "entrypoint": "/customers/42/edit",
	  "wall_action": "click save",
	  "first_bad_checkpoint": "tax id empty after refresh",
	  "terminal_state": "detail page shows empty tax id",
	  "timeline": [{"sequence": 1, "action": "click save", "observed_checkpoint": "toast success", "evidence_refs": ["shot-1.png"]}]
	}`
	if err := validator.ValidateBytes("finding.schema.json", []byte(fmt.Sprintf(base, good))); err != nil {
		t.Fatalf("complete user_flow encounter must validate: %v", err)
	}
}
