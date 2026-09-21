package dispatch

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func boardFixture(t *testing.T) (string, map[string]any) {
	t.Helper()
	root := t.TempDir()
	source := "../../docs/examples/dispatch-plan/project"
	err := filepath.WalkDir(source, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(source, p)
		target := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(target, b, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile("../../docs/control/loop-definition.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "control", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	git("init", "-b", "dev")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.invalid")
	git("add", ".")
	git("commit", "-m", "baseline")
	p, e := semantic.LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
	if e != nil || len(p.Problems) > 0 {
		t.Fatalf("%+v %v", p, e)
	}
	docs := []any{}
	add := func(id, kind, path string) {
		b, _ := os.ReadFile(filepath.Join(root, path))
		docs = append(docs, map[string]any{"id": id, "kind": kind, "path": path, "sha256": fmt.Sprintf("%x", sha256.Sum256(b)), "generation": 1})
	}
	add("dispatch-plan:REQ-042", "dispatch_plan", p.Path)
	for _, task := range p.Tasks {
		add(task.ID, "task", task.Path)
	}
	state := map[string]any{"runtime_id": "run", "revision": 1, "lifecycle": map[string]any{"state": "building"}, "baseline": map[string]any{"generation": 1}, "bound_req": map[string]any{"id": "REQ-042", "workspace": map[string]any{"dev_branch": "dev"}}, "documents": docs, "entities": map[string]any{"tasks": []any{}, "agents": []any{}}, "evidence": []any{}}
	return root, state
}
func TestCommittedBoardAndRegistration(t *testing.T) {
	root, state := boardFixture(t)
	board, e := Load(root, state, 3)
	if e != nil || len(board.Next) != 3 {
		t.Fatalf("%+v %v", board, e)
	}
	if e := CheckRegistration(root, state, "TASK-042-04"); e == nil {
		t.Fatal("consumer dispatched before integration")
	}
	if e := CheckRegistration(root, state, "TASK-042-02"); e != nil {
		t.Fatal(e)
	}
	taskPath := filepath.Join(root, "docs/dev/tasks/TASK-042-02.md")
	b, _ := os.ReadFile(taskPath)
	if e := ValidateWorkPackage(root, state, "TASK-042-02", taskPath, b, []string{"web/pages/order.ts"}, 1); e != nil {
		t.Fatal(e)
	}
	if e := ValidateWorkPackage(root, state, "TASK-042-02", taskPath, b, []string{"server/api"}, 1); e == nil {
		t.Fatal("scope escalation accepted")
	}
	os.WriteFile(taskPath, append(b, []byte("\nDIRTY\n")...), 0644)
	if _, e := Load(root, state, 3); e != nil {
		t.Fatal("dirty disk should not replace Git input", e)
	}
	if e := ValidateWorkPackage(root, state, "TASK-042-02", taskPath, append(b, []byte("\nDIRTY\n")...), []string{"web/pages"}, 1); e == nil {
		t.Fatal("dirty TASK sent to worker")
	}
	entities := state["entities"].(map[string]any)
	entities["tasks"] = []any{map[string]any{"id": "TASK-042-02", "state": "in_progress"}}
	if e := CheckRegistration(root, state, "TASK-042-02"); e == nil {
		t.Fatal("duplicate owner")
	}
	board, e = Load(root, state, 3)
	if e != nil || len(board.Next) != 2 {
		t.Fatalf("%+v %v", board, e)
	}
	if _, e := Load(root, state, 3); e != nil {
		t.Fatal("recovery", e)
	}
}
func TestPlanCannotDriftOrBecomeLegacy(t *testing.T) {
	root, state := boardFixture(t)
	p := filepath.Join(root, "docs/dev/tasks/index-REQ-042.md")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, append(b, []byte("\nnew reason\n")...), 0644)
	if _, e := LoadWithFiles(root, state, fileview.Disk{Root: root}, 2); e == nil || !strings.Contains(e.Error(), "drift") {
		t.Fatal(e)
	}
	os.Remove(p)
	if _, e := LoadWithFiles(root, state, fileview.Disk{Root: root}, 2); e == nil {
		t.Fatal("deleted plan passed")
	}
}
