package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplacementRetainsOldTreeAndUsesCurrentMain(t *testing.T) {
	b, e, state, ctx := workerFixture(t)
	e.CheckLocation = "main"
	b.Executions[e.AssignmentID] = e
	os.WriteFile(filepath.Join(e.Path, "input.md"), []byte("unfinished worker edits"), 0600)
	// Main can legitimately advance while the lost Worker retains its old base.
	os.WriteFile(filepath.Join(b.MainRoot, "new.md"), []byte("new independent input"), 0600)
	if _, err := Git(ctx, b.MainRoot, "add", "new.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := Git(ctx, b.MainRoot, "commit", "-m", "independent"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReplaceExecution(ctx, state, e.AssignmentID, e.AgentID, e.Generation+1); err == nil {
		t.Fatal("wrong generation accepted")
	}
	next, err := b.ReplaceExecution(ctx, state, e.AssignmentID, e.AgentID, e.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if next.CheckLocation != "main" || b.History[0].CheckLocation != "main" {
		t.Fatal("replacement lost runner")
	}
	if next.Generation != e.Generation+1 || next.Path == e.Path || len(b.History) != 1 || b.History[0].Status != "retired" {
		t.Fatalf("bad replacement: %+v", next)
	}
	if err = b.Materialize(ctx, next); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(e.Path, "input.md"))
	if err != nil || string(data) != "unfinished worker edits" {
		t.Fatal("lost old edits")
	}
	state["workspace"] = Encode(b)
	saveControlState(t, b.MainRoot, state)
	if _, err = ResolveControl(ctx, e.Path); err == nil {
		t.Fatal("retired Worker retained authority")
	}
	head, _ := Git(ctx, next.Path, "rev-parse", "HEAD")
	mainHead, _ := Git(ctx, b.MainRoot, "rev-parse", "HEAD")
	if head != mainHead {
		t.Fatal("replacement did not use current Main")
	}
}

func TestAdoptionRequiresHistoricalIdentityAndRetainsWork(t *testing.T) {
	b, e, state, ctx := workerFixture(t)
	os.WriteFile(filepath.Join(e.Path, "input.md"), []byte("historical work"), 0600)
	if _, err := Git(ctx, e.Path, "add", "input.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := Git(ctx, e.Path, "commit", "-m", "historical"); err != nil {
		t.Fatal(err)
	}
	delete(b.Executions, e.AssignmentID)
	e.Inputs = map[string]string{"input.md": "input.md"}
	for _, kind := range []string{"wrong_target", "duplicate_runtime"} {
		t.Run(kind, func(t *testing.T) {
			bad := e
			switch kind {
			case "wrong_target":
				bad.TargetBranch = "test3"
			case "duplicate_runtime":
				p := filepath.Join(e.Path, ".claude/loop-state.json")
				os.WriteFile(p, []byte("{}"), 0600)
				defer os.Remove(p)
			}
			if _, err := b.AdoptExecution(ctx, state, bad); err == nil {
				t.Fatal("unsafe historical adoption accepted")
			}
		})
	}
	adopted, err := b.AdoptExecution(ctx, state, e)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Status != "preparing" {
		t.Fatalf("invented baseline: %+v", adopted)
	}
	data, err := os.ReadFile(filepath.Join(e.Path, "input.md"))
	if err != nil || string(data) != "historical work" {
		t.Fatal("historical work removed")
	}
}
