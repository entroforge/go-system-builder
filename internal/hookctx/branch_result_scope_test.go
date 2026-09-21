package hookctx_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
)

func TestAssignmentRecoversCanonicalTaskResultAndEffectiveScope(t *testing.T) {
	for _, mode := range []string{"owner", "owner-without-agent", "foreign-agent", "legacy-scope"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			write := func(path string, value any) {
				t.Helper()
				path = filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			owner := "worker"
			if mode == "foreign-agent" {
				owner = "other"
			}
			agents := []any{}
			if mode != "owner-without-agent" {
				agents = append(agents, map[string]any{"id": "worker", "state": "reported", "task_ids": []string{"TASK-001"}, "completion_reported_ref": "wire.json"})
			}
			write(".claude/loop-state.json", map[string]any{"runtime_id": "run", "revision": 1, "entities": map[string]any{
				"agents": agents, "tasks": []any{map[string]any{"id": "TASK-001", "state": "in_progress", "owner_agent_ids": []string{owner}, "completion_report_ref": "canonical.json"}},
			}})
			a := map[string]any{"assignment_id": "assignment-1", "agent_id": "worker", "write_paths": []string{"src"}, "output_paths": []string{"generated", "src"}, "scope": []string{"legacy"}}
			wantPaths := []string{"generated", "src"}
			if mode == "legacy-scope" {
				delete(a, "write_paths")
				wantPaths = []string{"generated", "legacy", "src"}
			}
			write(".claude/workgroups/REQ-001/TASK-001/manifest.json", map[string]any{"assignments": []any{a}})
			loaded, err := hookctx.LoadFull(root, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded.Assignments) != 1 {
				t.Fatalf("assignments: %+v", loaded.Assignments)
			}
			row := loaded.Assignments[0]
			wantRef := "canonical.json"
			if mode == "foreign-agent" {
				wantRef = "wire.json"
			}
			if row.CompletionRef != wantRef {
				t.Fatalf("Result ref %q, want %q", row.CompletionRef, wantRef)
			}
			if !reflect.DeepEqual(row.WritePaths, wantPaths) {
				t.Fatalf("scope %v, want %v", row.WritePaths, wantPaths)
			}
		})
	}
}
