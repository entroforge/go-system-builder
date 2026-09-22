package semantic

import (
	"github.com/entroforge/go-system-builder/internal/fileview"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func dispatchFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := "../../docs/examples/dispatch-plan/project"
	err := filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(source, path)
		target := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		return os.WriteFile(target, b, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func mutatePlanFile(t *testing.T, root, rel, old, new string) {
	t.Helper()
	p := filepath.Join(root, rel)
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(b), old) {
		t.Fatalf("missing %s", old)
	}
	if e = os.WriteFile(p, []byte(strings.ReplaceAll(string(b), old, new)), 0644); e != nil {
		t.Fatal(e)
	}
}
func TestDispatchPlanBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, path, old, new, want string }{
		{"missing", "index-REQ-042.md", "- [ ] [前端骨架](TASK-042-02.md)", "", "missing from dispatch plan"},
		{"duplicate", "index-REQ-042.md", "- [ ] [前端骨架](TASK-042-02.md)", "- [ ] [前端骨架](TASK-042-02.md)\n- [ ] [dup](TASK-042-02.md)", "duplicate"},
		{"checkbox", "index-REQ-042.md", "- [ ] [前端骨架]", "- [x] [前端骨架]", "unchecked"},
		{"resource cycle", "index-REQ-042.md", "| --- | --- | --- | --- |", "| --- | --- | --- | --- |\n| TASK-042-04 | TASK-042-01 | shared port | serialized |", "cycle"},
		{"overlap", "TASK-042-02.md", "web/pages", "packages/validation", "same-wave"},
		{"cross req", "TASK-042-02.md", "REQ-042", "REQ-099", "cross-REQ"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := dispatchFixture(t)
			mutatePlanFile(t, root, "docs/dev/tasks/"+tc.path, tc.old, tc.new)
			p, e := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
			if e != nil || !strings.Contains(strings.Join(append(p.Problems, p.Warnings...), ";"), tc.want) {
				t.Fatalf("%+v %v", p, e)
			}
		})
	}
}
func TestDispatchNoWaveBarrierAndCapacity(t *testing.T) {
	root := dispatchFixture(t)
	p, e := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
	if e != nil || len(p.Problems) > 0 {
		t.Fatalf("%+v %v", p, e)
	}
	_, next := NextDispatch(p, nil, 3)
	if strings.Join(next, ",") != "TASK-042-01,TASK-042-02,TASK-042-03" {
		t.Fatal(next)
	}
	facts := map[string]DispatchFact{"TASK-042-01": {State: "integrated"}, "TASK-042-02": {State: "integrated"}, "TASK-042-03": {State: "running"}}
	rows, next := NextDispatch(p, facts, 1)
	if strings.Join(next, ",") != "TASK-042-04" {
		t.Fatal(rows, next)
	}
	_, next = NextDispatch(p, facts, 0)
	if len(next) != 0 {
		t.Fatal(next)
	}
	facts["TASK-042-01"] = DispatchFact{State: "reported"}
	_, next = NextDispatch(p, facts, 3)
	if len(next) > 0 {
		t.Fatal("reported must not release", next)
	}
}
func TestNewPlanCannotBecomeLegacyByDeletingIndex(t *testing.T) {
	root := dispatchFixture(t)
	os.Remove(filepath.Join(root, "docs/dev/tasks/index-REQ-042.md"))
	p, e := LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
	if e != nil || len(p.Problems) == 0 {
		t.Fatalf("%+v %v", p, e)
	}
}
func TestDispatchScopedChecks(t *testing.T) {
	root := dispatchFixture(t)
	os.WriteFile(filepath.Join(root, "docs/dev/tasks/TASK-099-01.md"), []byte("> Source REQ refs: REQ-099\n> Status: draft\n"), 0644)
	r, e := TasksCheckWithFiles(root, fileview.Disk{Root: root}, "REQ-042")
	if e != nil || len(r.Problems) > 0 {
		t.Fatalf("%+v %v", r, e)
	}
}
