package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBatchScopeRepairInspectionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any, string)
		want   bool
	}{
		{"mixed batch", nil, true},
		{"already executed", func(s map[string]any, r string) { s["evidence"] = []any{map[string]any{"kind": "completion_report"}} }, false},
		{"builder dispatched", func(s map[string]any, r string) {
			s["entities"] = map[string]any{"agents": []any{map[string]any{"role": "backend-builder"}}}
		}, false},
		{"review started", func(s map[string]any, r string) { s["review"] = map[string]any{"round": 1} }, false},
		{"paused", func(s map[string]any, r string) { s["pause"] = map[string]any{} }, false},
		{"tampered", func(s map[string]any, r string) {
			os.WriteFile(filepath.Join(r, "docs/tasks/TASK-1.md"), []byte("changed"), 0644)
		}, false},
		{"missing", func(s map[string]any, r string) { os.Remove(filepath.Join(r, "docs/tasks/TASK-1.md")) }, false},
		{"escape", func(s map[string]any, r string) { s["documents"].([]any)[0].(map[string]any)["path"] = "../outside.md" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write := func(rel, text string) string {
				t.Helper()
				p := filepath.Join(root, rel)
				os.MkdirAll(filepath.Dir(p), 0755)
				if err := os.WriteFile(p, []byte(text), 0644); err != nil {
					t.Fatal(err)
				}
				return scopeHash([]byte(text))
			}
			reqHash := write("docs/requirements/REQ-053.md", "> 状态：locked")
			docs := []any{}
			for _, row := range [][2]string{{"TASK-1", "REQ-052"}, {"TASK-2", "REQ-053"}} {
				rel := "docs/tasks/" + row[0] + ".md"
				h := write(rel, "> Source REQ refs: "+row[1])
				docs = append(docs, map[string]any{"id": row[0], "kind": "task", "path": rel, "sha256": h, "generation": 1})
			}
			s := map[string]any{"runtime_id": "loop-REQ-053", "revision": 1, "lifecycle": map[string]any{"state": "building"}, "baseline": map[string]any{"generation": 1}, "bound_req": map[string]any{"id": "REQ-053", "status": "locked", "path": "docs/requirements/REQ-053.md", "sha256": reqHash}, "documents": docs}
			if tc.change != nil {
				tc.change(s, root)
			}
			p, err := inspectBatchScope(root, s)
			if (err == nil) != tc.want {
				t.Fatalf("plan=%+v err=%v", p, err)
			}
			if err == nil && (len(p.Removed) != 1 || len(p.RetainedTasks) != 1) {
				t.Fatalf("plan=%+v", p)
			}
		})
	}
}
