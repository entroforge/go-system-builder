package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

type noPlanReread struct {
	fileview.Reader
	targets map[string]bool
	reads   []string
}

func (r *noPlanReread) ReadFile(path string) ([]byte, error) {
	if r.targets[filepath.Clean(path)] {
		r.reads = append(r.reads, path)
		return nil, fmt.Errorf("selected plan material was read twice: %s", path)
	}
	return r.Reader.ReadFile(path)
}

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
		docs = append(docs, map[string]any{"id": id, "kind": kind, "path": path, "generation": 1})
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
	if e := ValidateWorkPackage(root, state, "TASK-042-02", taskPath, append(b, []byte("\nDIRTY\n")...), []string{"web/pages"}, 1); e != nil {
		t.Fatalf("supplied bytes became a second source-identity gate: %v", e)
	}
	if e := ValidateWorkPackage(root, state, "TASK-042-02", filepath.Join(root, "other.md"), b, []string{"web/pages"}, 1); e == nil {
		t.Fatal("different TASK path accepted")
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

func TestBoardShowsUnknownOwnerWithoutSpendingBuilderCapacity(t *testing.T) {
	root, state := boardFixture(t)
	state["entities"].(map[string]any)["tasks"] = []any{map[string]any{"id": "TASK-042-02", "state": "in_progress", "owner_agent_ids": []any{"unresolved-owner"}}}
	board, err := Load(root, state, 1)
	if err != nil {
		t.Fatal(err)
	}
	var unknown bool
	for _, row := range board.Rows {
		if row.Task.ID == "TASK-042-02" {
			unknown = row.Unknown && !row.Dispatchable && strings.Contains(row.Reason, "unknown")
		}
	}
	if !unknown || len(board.Next) != 1 || board.Next[0] == "TASK-042-02" {
		t.Fatalf("unknown responsibility hid independent capacity: %+v", board)
	}
	if err := CheckBoardRegistration(board, "TASK-042-02"); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown TASK was silently declared free: %v", err)
	}
}
func TestRegisteredPlanUsesCurrentMaterialAndReportsMissing(t *testing.T) {
	root, state := boardFixture(t)
	p := filepath.Join(root, "docs/dev/tasks/index-REQ-042.md")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, append(b, []byte("\nnew reason\n")...), 0644)
	if _, e := LoadWithFiles(root, state, fileview.Disk{Root: root}, 2); e != nil {
		t.Fatalf("current readable plan rejected as historical byte drift: %v", e)
	}
	os.Remove(p)
	if _, e := LoadWithFiles(root, state, fileview.Disk{Root: root}, 2); e == nil {
		t.Fatal("deleted plan passed")
	}
}

func TestRegisteredBoardIgnoresHistoricalDigests(t *testing.T) {
	for _, tc := range []struct {
		name, historical string
	}{
		{name: "missing"},
		{name: "outdated", historical: strings.Repeat("0", 64)},
		{name: "old format", historical: "old-unrelated-value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, state := boardFixture(t)
			if tc.historical != "" {
				for _, raw := range state["documents"].([]any) {
					raw.(map[string]any)["sha256"] = tc.historical
				}
			}
			board, err := Load(root, state, 3)
			if err != nil || len(board.Next) != 3 {
				t.Fatalf("registered current plan should not depend on historical digest: %+v %v", board, err)
			}
			if err := CheckRegistration(root, state, "TASK-042-02"); err != nil {
				t.Fatalf("eligible TASK rejected: %v", err)
			}
		})
	}
}

func TestRegisteredBoardReportsActualMissingTaskAndIdentity(t *testing.T) {
	t.Run("missing task", func(t *testing.T) {
		root, state := boardFixture(t)
		path := "docs/dev/tasks/TASK-042-02.md"
		if err := os.Remove(filepath.Join(root, path)); err != nil {
			t.Fatal(err)
		}
		_, err := LoadWithFiles(root, state, fileview.Disk{Root: root}, 3)
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("missing TASK lacks its actual path: %v", err)
		}
	})
	t.Run("registered task path", func(t *testing.T) {
		root, state := boardFixture(t)
		for _, raw := range state["documents"].([]any) {
			doc := raw.(map[string]any)
			if doc["id"] == "TASK-042-02" {
				doc["path"] = "docs/dev/tasks/TASK-042-03.md"
			}
		}
		_, err := Load(root, state, 3)
		if err == nil || !strings.Contains(err.Error(), "TASK-042-02") || !strings.Contains(err.Error(), "path") {
			t.Fatalf("registered TASK path conflict was not reported: %v", err)
		}
	})
}

func TestSelectedPlanProjectsLatestRuntimeWithoutRereadingTasks(t *testing.T) {
	root, state := boardFixture(t)
	selected, err := Load(root, state, 3)
	if err != nil || selected.Plan == nil {
		t.Fatalf("select original plan: %+v %v", selected, err)
	}
	reader := &noPlanReread{Reader: fileview.Disk{Root: root}, targets: map[string]bool{}}
	reader.targets[filepath.Clean(filepath.Join(root, selected.Plan.Path))] = true
	reader.targets[filepath.Clean(selected.Plan.Path)] = true
	for _, task := range selected.Plan.Tasks {
		reader.targets[filepath.Clean(filepath.Join(root, task.Path))] = true
		reader.targets[filepath.Clean(task.Path)] = true
	}
	entities := state["entities"].(map[string]any)
	entities["tasks"] = []any{map[string]any{"id": "TASK-042-02", "state": "in_progress"}}
	projected, err := ProjectSelectedPlan(root, state, reader, selected.Plan, 3)
	if err != nil || projected.Plan != selected.Plan || len(reader.reads) != 0 {
		t.Fatalf("selected plan reread or failed: reads=%v err=%v", reader.reads, err)
	}
	if err := CheckBoardRegistration(projected, "TASK-042-02"); err == nil {
		t.Fatal("stale selected board ignored the latest active responsibility")
	}
	if err := CheckBoardRegistration(projected, "TASK-042-04"); err == nil {
		t.Fatal("unintegrated predecessor was ignored")
	}
	if err := ValidateBoardWorkPackage(root, projected, "TASK-042-02", filepath.Join(root, "docs/dev/tasks/TASK-042-02.md"), []string{"web/pages/order.ts"}, 1); err != nil {
		t.Fatalf("original selected TASK scope rejected: %v", err)
	}
	if err := ValidateBoardWorkPackage(root, projected, "TASK-042-02", filepath.Join(root, "docs/dev/tasks/TASK-042-02.md"), []string{"server/api"}, 1); err == nil {
		t.Fatal("write scope conflict was ignored")
	}
}
