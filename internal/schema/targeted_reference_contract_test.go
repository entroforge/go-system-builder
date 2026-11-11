package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// Validate the embedded reference contract for each supported Runtime version:
// projected conclusions are optional internal metadata, not author fields.
func TestTargetedReceiptReferenceSupportsOldAndNewRuntimeVersions(t *testing.T) {
	for _, name := range []string{"loop-state.schema.json", "loop-state-v1.1.schema.json", "loop-state-v1.2.schema.json", "loop-state-v1.3.schema.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := schema.ReadAsset(name)
			if err != nil {
				t.Fatal(err)
			}
			var doc any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			var find func(any) map[string]any
			find = func(v any) map[string]any {
				switch x := v.(type) {
				case map[string]any:
					if item, ok := x["targeted_reverification_artifacts"].(map[string]any); ok {
						return item
					}
					for _, v := range x {
						if found := find(v); found != nil {
							return found
						}
					}
				case []any:
					for _, v := range x {
						if found := find(v); found != nil {
							return found
						}
					}
				}
				return nil
			}
			array := find(doc)
			if array == nil {
				t.Fatal("targeted reference definition not found")
			}
			item, ok := array["items"].(map[string]any)
			if !ok {
				t.Fatal("targeted reference item schema missing")
			}
			compiler := jsonschema.NewCompiler()
			if err := compiler.AddResource("https://example.invalid/targeted-reference.json", item); err != nil {
				t.Fatal(err)
			}
			compiled, err := compiler.Compile("https://example.invalid/targeted-reference.json")
			if err != nil {
				t.Fatal(err)
			}
			sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			for _, row := range []map[string]any{{"path": "old.json", "sha256": sha}, {"path": "new.json", "sha256": sha, "performing_assignment_id": "repair-assignment-verification-one", "result": "pass"}, {"path": "unknown.json", "sha256": sha, "performing_assignment_id": "", "result": ""}} {
				if err := compiled.Validate(row); err != nil {
					t.Fatalf("original or projected accepted reference was rejected: %v", err)
				}
			}
			if err := compiled.Validate(map[string]any{"sha256": sha}); err == nil {
				t.Fatal("a reference without its path must not silently become a known receipt")
			}
		})
	}
}
