package schema

import (
	"encoding/json"
	"testing"
)

func TestWorkspaceRelocationRetainsUnknownHistoryAcrossRuntimeVersions(t *testing.T) {
	for _, name := range []string{"loop-state.schema.json", "loop-state-v1.1.schema.json", "loop-state-v1.2.schema.json", "loop-state-v1.3.schema.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := ReadAsset(name)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			compiler := newEmbeddedCompiler()
			id := doc["$id"].(string)
			if err := compiler.AddResource(id, doc); err != nil {
				t.Fatal(err)
			}
			compiled, err := compiler.Compile(id + "#/properties/workspace")
			if err != nil {
				t.Fatal(err)
			}
			execution := map[string]any{
				"assignment_id": "assignment", "runtime_id": "runtime", "agent_id": "owner",
				"worktree_path": "/old/worker", "branch": "worker", "target_branch": "dev",
				"baseline_generation": 1, "execution_generation": 1, "status": "ready",
				"write_paths": []any{"src/**"}, "required_checks": []any{}, "inputs": map[string]any{},
			}
			registry := map[string]any{"main_root": "/old", "common_dir": "/old/.git", "integration_branch": "dev", "binding_version": 1, "executions": map[string]any{"assignment": execution}}
			for _, value := range []any{nil, "historical", 7, []any{true}, map[string]any{"business": "kept"}} {
				registry["historical_extension"], execution["historical_extension"] = value, value
				if err := compiled.Validate(registry); err != nil {
					t.Fatalf("unknown history rejected: %v", err)
				}
			}
			execution["runtime_id"] = 7
			if err := compiled.Validate(registry); err == nil {
				t.Fatal("current responsibility field stopped validating")
			}
			defs, _ := doc["$defs"].(map[string]any)
			for _, receiptName := range []string{"tool_check_receipt", "native_owned_tool_receipt"} {
				if defs[receiptName] == nil {
					continue
				}
				receiptSchema, err := compiler.Compile(id + "#/$defs/" + receiptName)
				if err != nil {
					t.Fatal(err)
				}
				binding := map[string]any{"runtime_id": "runtime", "baseline_generation": 1, "assignment_id": "assignment", "owner": "owner", "execution_generation": 0, "responsibility": "owned_check", "historical_extension": map[string]any{"kept": true}}
				if receiptName == "tool_check_receipt" {
					binding["execution_generation"] = 1
				}
				receipt := map[string]any{"path": "/old/receipt.json", "checkout": "/old/worker", "command": "true", "binding": binding, "historical_extension": []any{nil, 7}}
				if err := receiptSchema.Validate(receipt); err != nil {
					t.Fatalf("original receipt history rejected: %v", err)
				}
				binding["owner"] = 7
				if err := receiptSchema.Validate(receipt); err == nil {
					t.Fatal("current receipt owner stopped validating")
				}
			}
		})
	}
}
