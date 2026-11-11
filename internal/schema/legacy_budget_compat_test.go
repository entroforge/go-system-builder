package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestRuntimeBudgetsAreOptionalCompatibilityDataAllVersions(t *testing.T) {
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
			repair := doc["properties"].(map[string]any)["configuration"].(map[string]any)["properties"].(map[string]any)["repair"].(map[string]any)
			compiler := jsonschema.NewCompiler()
			const uri = "https://example.invalid/repair.json"
			if err := compiler.AddResource(uri, repair); err != nil {
				t.Fatal(err)
			}
			compiled, err := compiler.Compile(uri)
			if err != nil {
				t.Fatal(err)
			}
			current := map[string]any{"max_attempts_per_bug": 3, "max_same_contract_failures": 2}
			if err := compiled.Validate(current); err != nil {
				t.Fatalf("new Runtime requires legacy budget: %v", err)
			}
			current["max_full_review_rounds"], current["max_investigation_reentries"] = 5, 5
			if err := compiled.Validate(current); err != nil {
				t.Fatalf("issued legacy Runtime unreadable: %v", err)
			}
			for _, key := range []string{"max_full_review_rounds", "max_investigation_reentries"} {
				if _, exists := repair["properties"].(map[string]any)[key].(map[string]any)["default"]; exists {
					t.Fatalf("%s supplies a new default", key)
				}
			}
		})
	}
}
