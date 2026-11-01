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
func TestDispatchRegistrationUsesCurrentPlanWithoutDigestGate(t *testing.T) {
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
	if err := dispatchPlanDocuments(state, ctx, false); err != nil {
		t.Fatalf("ordinary plan edit blocked execution: %v", err)
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
		t.Fatal("missing current dispatch plan accepted")
	}
}
func TestDispatchTaskChangeDoesNotRequireDigestRegistration(t *testing.T) {
	root, state, ctx := dispatchTransitionFixture(t)
	if _, e := actionRegisterPlanningTasks(state, ctx); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "docs/dev/tasks/TASK-042-01.md")
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.ReplaceAll(string(b), "共享库", "new goal")), 0644)
	if e := dispatchPlanDocuments(state, ctx, false); e != nil {
		t.Fatalf("TASK edit introduced a digest gate: %v", e)
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

func TestPacketTR002RegistrationRequiresCurrentS4Followup(t *testing.T) {
	root := t.TempDir()
	state := map[string]any{
		"schema_version": "1.2.0",
		"configuration":  map[string]any{"optimization": map[string]any{"packet": true}},
		"baseline":       map[string]any{"generation": 1},
		"documents":      []any{},
	}
	ctx := &ActionContext{
		Root: root, Spec: TransitionSpec{ID: "TR-002"},
		Request: &Request{Actor: "hook_controller", Files: fileview.Disk{Root: root}},
	}
	for _, action := range []ActionFn{actionRegisterLockedContracts, actionRegisterPlanningTasks} {
		if _, err := action(state, ctx); err == nil || !strings.Contains(err.Error(), "current accepted S4 planning_task followup") {
			t.Fatalf("packet-mode TR-002 action accepted a missing S4 followup: %v", err)
		}
	}
	if len(state["documents"].([]any)) != 0 {
		t.Fatalf("rejected TR-002 registration mutated documents: %#v", state["documents"])
	}
}

func TestPlanningRegistrationPreservesUnrelatedHistoricalTaskFields(t *testing.T) {
	_, state, ctx := dispatchTransitionFixture(t)
	state["entities"] = map[string]any{"tasks": []any{map[string]any{
		"id": "TASK-042-01", "path": "old/path.md", "state": "in_progress", "sha256": map[string]any{"historical": true},
	}}}
	if _, err := actionRegisterPlanningTasks(state, ctx); err != nil {
		t.Fatal(err)
	}
	row := state["entities"].(map[string]any)["tasks"].([]any)[0].(map[string]any)
	if row["path"] != "docs/dev/tasks/TASK-042-01.md" || row["state"] != "in_progress" {
		t.Fatalf("original planning registration lost task path or responsibility: %#v", row)
	}
	if historical, ok := row["sha256"].(map[string]any); !ok || historical["historical"] != true {
		t.Fatalf("registration altered an unrelated historical value: %#v", row)
	}
	for _, raw := range state["documents"].([]any) {
		doc := raw.(map[string]any)
		if doc["id"] == "TASK-042-01" {
			if _, exists := doc["sha256"]; exists {
				t.Fatalf("new TASK registration copied a digest: %#v", doc)
			}
			return
		}
	}
	t.Fatal("actual TASK document was not registered")
}
