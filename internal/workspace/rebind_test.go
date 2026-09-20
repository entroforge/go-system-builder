package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitRelocationPreservesBranchBaseAndPointer(t *testing.T) {
	b, e, state, ctx := workerFixture(t)
	oldRoot := b.MainRoot
	oldCommon := b.CommonDir
	newRoot := filepath.Join(t.TempDir(), "relocated")
	req := RebindRequest{RuntimeID: RuntimeID(state), OldMainRoot: oldRoot, OldCommonDir: oldCommon, NewMainRoot: newRoot, NewCommonDir: filepath.Join(newRoot, ".git"), Branch: b.Branch, ExpectedHead: b.BoundHead, Paths: map[string]string{e.Path: filepath.Join(newRoot, ".worktrees", filepath.Base(e.Path))}}
	if _, err := b.PlanRebind(ctx, state, req); err == nil {
		t.Fatal("copied/ambiguous Main accepted")
	}
	if err := os.Rename(oldRoot, newRoot); err != nil {
		t.Fatal(err)
	}
	// Native Git owns its own link repair; the Harness does not guess it.
	if _, err := Git(ctx, newRoot, "worktree", "repair", req.Paths[e.Path]); err != nil {
		t.Fatal(err)
	}
	next, err := b.PlanRebind(ctx, state, req)
	if err != nil {
		t.Fatal(err)
	}
	moved := next.Executions[e.AssignmentID]
	if moved.BaseCommit != e.BaseCommit || next.Branch != "test2" || next.BoundHead != b.BoundHead {
		t.Fatal("relocation changed history")
	}
	state["workspace"] = Encode(next)
	saveControlState(t, newRoot, state)
	for i := 0; i < 2; i++ {
		if err = next.RelocatePointer(moved, oldRoot); err != nil {
			t.Fatal(err)
		}
	}
	root, err := ResolveControl(ctx, moved.Path)
	if err != nil || root != newRoot {
		t.Fatalf("moved pointer root=%s err=%v", root, err)
	}
	req.ExpectedHead = "wrong"
	if _, err = b.PlanRebind(ctx, state, req); err == nil {
		t.Fatal("wrong repository head accepted")
	}
}
