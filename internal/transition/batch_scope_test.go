package transition

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBatchRegistrationExcludesForeignREQAndExecutionDoesNotRescan(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "docs/dev/tasks"), 0755)
	write := func(id, req string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "docs/dev/tasks", id+".md"), []byte("> Status: complete\n> Source REQ refs: "+req+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("TASK-1", "REQ-052")
	write("TASK-2", "REQ-053")
	state := map[string]any{"bound_req": map[string]any{"id": "REQ-053"}, "baseline": map[string]any{"generation": 1}, "documents": []any{}}
	ctx := &ActionContext{Root: root, Evidence: map[string]string{"review": "test"}}
	if _, err := actionRegisterPlanningTasks(state, ctx); err != nil {
		t.Fatal(err)
	}
	docs := state["documents"].([]any)
	if len(docs) != 1 || docs[0].(map[string]any)["id"] != "TASK-2" {
		t.Fatalf("docs=%v", docs)
	}
	write("TASK-3", "REQ-053") // not reviewed or registered
	if _, err := actionRegisterExecutionBatch(state, ctx); err != nil {
		t.Fatal(err)
	}
	if len(state["documents"].([]any)) != 1 {
		t.Fatal("execution rescanned unreviewed task")
	}
	write("TASK-2", "REQ-052")
	if _, err := actionRegisterExecutionBatch(state, ctx); err == nil {
		t.Fatal("foreign/changed registered task accepted")
	}
}
