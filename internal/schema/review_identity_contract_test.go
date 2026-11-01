package schema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestReviewIdentityArtifactVersionsPreserveIssuedMaterials(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	for _, kind := range []string{"review-plan", "review-result"} {
		t.Run(kind, func(t *testing.T) {
			data, err := schema.ReadAsset(kind + ".example.json")
			if err != nil {
				t.Fatal(err)
			}
			var artifact map[string]any
			if err := json.Unmarshal(data, &artifact); err != nil {
				t.Fatal(err)
			}
			validate := func(want bool) {
				t.Helper()
				raw, _ := json.Marshal(artifact)
				err := validator.ValidateBytes(kind+".schema.json", raw)
				if (err == nil) != want {
					t.Fatalf("valid=%v want %v: %v", err == nil, want, err)
				}
			}
			validate(true) // Existing 1.0.0 stays readable without rewriting history.
			delete(artifact, "review_round")
			validate(true) // Original responsibility does not require a round number.
			artifact["schema_version"] = "1.1.0"
			validate(true)
			artifact["review_round"] = 2
			validate(true) // Mixed original materials remain readable.
			artifact["schema_version"] = "unknown"
			validate(false)
		})
	}
}

func TestReviewIdentityRuntimeProjectionAllVersions(t *testing.T) {
	for _, name := range []string{"loop-state.schema.json", "loop-state-v1.1.schema.json", "loop-state-v1.2.schema.json", "loop-state-v1.3.schema.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := schema.ReadAsset(name)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			review := doc["properties"].(map[string]any)["review"]
			compiler := jsonschema.NewCompiler()
			const uri = "https://example.invalid/review.json"
			if err := compiler.AddResource(uri, review); err != nil {
				t.Fatal(err)
			}
			compiled, err := compiler.Compile(uri)
			if err != nil {
				t.Fatal(err)
			}
			plan := map[string]any{"plan_id": "review-plan-event", "path": "plan.json", "sha256": strings.Repeat("a", 64), "revision": 1, "status": "running", "e2e_coverage_state": "not_applicable", "submitted_at": "time"}
			for _, numbered := range []bool{false, true} {
				if numbered {
					plan["review_round"] = 2
				}
				if err := compiled.Validate(map[string]any{"clean_round": nil, "plan": plan}); err != nil {
					t.Fatalf("original/new plan pointer rejected: %v", err)
				}
			}
			oldEntry := map[string]any{"transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil, "round": 2}
			newEntry := map[string]any{"transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil, "plan_id": "review-plan-event"}
			for _, row := range []map[string]any{
				{"round": 2, "clean_round": nil, "round_entry": oldEntry},
				{"clean_round": nil, "round_entry": newEntry},
				{"round": 2, "clean_round": nil, "round_entry": newEntry},
				{"clean_round": nil, "round_entry": nil},
			} {
				if err := compiled.Validate(row); err != nil {
					t.Fatalf("original/new projection rejected: %v", err)
				}
			}
			for _, value := range []any{nil, "", 0, 1.5} {
				oldEntry["round"] = value
				if err := compiled.Validate(map[string]any{"round": 2, "clean_round": nil, "round_entry": oldEntry}); err == nil {
					t.Fatalf("invalid legacy number %#v silently accepted", value)
				}
			}
		})
	}
}
