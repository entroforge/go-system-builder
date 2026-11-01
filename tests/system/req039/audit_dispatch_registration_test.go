package req039_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// Seed a reviewed S6 projection. The separate Hook spine test covers creating
// that projection through S4/S5; these cases exercise the actual Writer entry.
func s4AuditRegistrationRoot(t *testing.T) string {
	t.Helper()
	root := freshRoot(t)
	state := systemPlanningState(t, root, "tasks", 1)
	if err := os.RemoveAll(filepath.Join(root, "docs/dev/tasks")); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(repoRoot(t), "docs/examples/dispatch-plan/project/docs/dev/tasks")
	if err := filepath.WalkDir(source, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, p)
		if err != nil {
			return err
		}
		dest := filepath.Join(root, "docs/dev/tasks", rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, bytes.ReplaceAll(b, []byte("REQ-042"), []byte("REQ-039")), 0644)
	}); err != nil {
		t.Fatal(err)
	}
	p, err := semantic.LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-039")
	if err != nil || len(p.Problems) > 0 {
		t.Fatalf("plan fixture: %v %+v", err, p)
	}
	docs := []any{}
	add := func(id, kind, path string) {
		docs = append(docs, map[string]any{"id": id, "kind": kind, "path": path, "version": "v1.0.0", "status": "locked", "generation": 1})
	}
	add("dispatch-plan:REQ-039", "dispatch_plan", p.Path)
	for _, task := range p.Tasks {
		add(task.ID, "task", task.Path)
	}
	state["documents"] = docs
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	ms := state["milestone"].(map[string]any)
	ms["stage"], ms["lifecycle_state"], ms["lifecycle_phase"] = "S6", "building", nil
	writeSystemState(t, root, state)
	return root
}
