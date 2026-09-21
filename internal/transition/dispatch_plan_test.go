package transition

import (
	"github.com/entroforge/go-system-builder/internal/fileview"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func dispatchTransitionFixture(t *testing.T) (string, map[string]any, *ActionContext) {
	t.Helper()
	root := t.TempDir()
	source := "../../docs/examples/dispatch-plan/project"
	err := filepath.WalkDir(source, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(source, p)
		out := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0755)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(out, b, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "bound_req": map[string]any{"id": "REQ-042"}, "documents": []any{}}
	return root, state, &ActionContext{Root: root, OccurredAt: time.Now(), Request: &Request{Actor: "planner", Files: fileview.Disk{Root: root}}}
}
func TestDispatchRegistrationAndFreezeDrift(t *testing.T) {
	root, state, ctx := dispatchTransitionFixture(t)
	if _, err := actionRegisterPlanningTasks(state, ctx); err != nil {
		t.Fatal(err)
	}
	docs := state["documents"].([]any)
	if len(docs) != 7 {
		t.Fatal(docs)
	}
	if err := dispatchPlanDocuments(state, ctx, false); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "docs/dev/tasks/index-REQ-042.md")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, append(b, []byte("\nchanged after review\n")...), 0644)
	if err := dispatchPlanDocuments(state, ctx, false); err == nil {
		t.Fatal("freeze laundered changed plan")
	}
	if _, err := actionRegisterPlanningTasks(state, ctx); err != nil {
		t.Fatal(err)
	}
	if len(state["documents"].([]any)) != 7 {
		t.Fatal("re-registration duplicated subjects")
	}
	if err := dispatchPlanDocuments(state, ctx, false); err != nil {
		t.Fatal(err)
	}
	os.Remove(p)
	if err := dispatchPlanDocuments(state, ctx, false); err == nil {
		t.Fatal("removed plan accepted")
	}
}
func TestDispatchFreezeDoesNotRefreshTaskHash(t *testing.T) {
	root, state, ctx := dispatchTransitionFixture(t)
	if _, e := actionRegisterPlanningTasks(state, ctx); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "docs/dev/tasks/TASK-042-01.md")
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.ReplaceAll(string(b), "共享库", "new goal")), 0644)
	if e := dispatchPlanDocuments(state, ctx, false); e == nil {
		t.Fatal("TASK change accepted")
	}
}

func TestDispatchFreezeProjectsExistingArtifactLocks(t *testing.T) {
	_, state, ctx := dispatchTransitionFixture(t)
	if _, e := actionRegisterPlanningTasks(state, ctx); e != nil {
		t.Fatal(e)
	}
	ctx.Evidence = map[string]string{"execution_batch": "reviewed"}
	if _, e := actionRegisterExecutionBatch(state, ctx); e != nil {
		t.Fatal(e)
	}
	for _, raw := range state["documents"].([]any) {
		d := raw.(map[string]any)
		if d["status"] != "locked" {
			t.Fatalf("not frozen: %v", d)
		}
	}
}
