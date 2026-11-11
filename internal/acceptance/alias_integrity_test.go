package acceptance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestS10AliasUsesRecordedIdentityWithoutHashCertification(t *testing.T) {
	for _, scenario := range []string{"missing case alias", "tampered original", "unrelated path", "different identity", "different kind"} {
		t.Run(scenario, func(t *testing.T) {

			root := t.TempDir()
			write := func(rel string, data []byte) {
				t.Helper()
				path := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			req := []byte("# REQ-001\n| FR-001 | first |\n| FR-002 | second |\n")
			contract := []byte("# BE-001\n")
			task := []byte("# TASK-001\n## Closing Contract\n")
			plan := []byte(`{"review_plan_id":"review-plan-2","review_round":2,"baseline_generation":1,"claims":[{"claim_id":"claim-qa-1"}]}`)
			write("docs/requirements/REQ-001.md", req)
			write("docs/dev/contracts/BE-001.md", contract)
			write("docs/dev/tasks/TASK-001.md", task)
			write(".claude/review/plans/review-plan-2.json", plan)
			state := map[string]any{
				"bound_req": map[string]any{"id": "REQ-001", "path": "docs/requirements/REQ-001.md"},
				"baseline":  map[string]any{"generation": 1},
				"review": map[string]any{
					"plan": map[string]any{"path": ".claude/review/plans/review-plan-2.json"},
				},
				"documents": []any{
					map[string]any{"id": "BE-001", "kind": "contract", "path": "docs/dev/contracts/BE-001.md", "generation": 1},
					map[string]any{"id": "TASK-001", "kind": "task", "path": "docs/dev/tasks/TASK-001.md", "generation": 1},
				},
			}
			if _, err := os.Stat(filepath.Join(root, "docs/dev/contracts/be-001.md")); err == nil {
				t.Skip("missing case-alias scenario requires a case-sensitive filesystem")
			}
			alias := map[string]any{"id": "BE-001", "kind": "contract", "path": "docs/dev/contracts/be-001.md", "generation": 1}
			switch scenario {
			case "tampered original":
				write("docs/dev/contracts/be-001.md", []byte("tampered"))
			case "unrelated path":
				alias["path"] = "docs/dev/contracts/unrelated.md"
			case "different identity":
				alias["id"] = "BE-999"
			case "different kind":
				alias["kind"] = "task"
			}
			state["documents"] = append(state["documents"].([]any), alias)
			_, err := BuildS10InventoryAuthority(root, state, Baseline{})
			if (err == nil) != (scenario == "missing case alias" || scenario == "tampered original") {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}
