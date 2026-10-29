package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestOwnedCheckRelocationMovesOnlyEvidenceLocator(t *testing.T) {
	oldMain := filepath.Join(t.TempDir(), "old-main")
	newMain := filepath.Join(t.TempDir(), "new-main")
	oldReceipt := filepath.Join(oldMain, ".claude/evidence/workspace-checks/receipt.json")
	ref := OwnedCheckReceipt{WorkerCheckIndex: 3, Receipt: checkexec.ToolReceipt{
		Path: oldReceipt, Checkout: filepath.Join(oldMain, ".worktrees/assignment-owner"),
		SHA256: "receipt-digest", LogSHA256: "log-digest", Command: "true", ProfileDigest: "profile-digest",
		Binding: checkexec.Binding{RuntimeID: "loop-rebind", Generation: 4, AssignmentID: "assignment-owner", Owner: "agent-owner", ExecutionGeneration: 2, Responsibility: "owned_check"},
	}}
	moved, err := relocateOwnedCheckReceipts(oldMain, newMain, []OwnedCheckReceipt{ref})
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || moved[0].Receipt.Path != filepath.Join(newMain, ".claude/evidence/workspace-checks/receipt.json") {
		t.Fatalf("owned receipt path was not relocated: %+v", moved)
	}
	if moved[0].Receipt.Checkout != ref.Receipt.Checkout || moved[0].Receipt.SHA256 != ref.Receipt.SHA256 || moved[0].Receipt.Binding != ref.Receipt.Binding || moved[0].WorkerCheckIndex != ref.WorkerCheckIndex {
		t.Fatal("relocation rewrote captured source identity or receipt bytes")
	}
	if !reflect.DeepEqual(ref, OwnedCheckReceipt{WorkerCheckIndex: 3, Receipt: checkexec.ToolReceipt{
		Path: oldReceipt, Checkout: filepath.Join(oldMain, ".worktrees/assignment-owner"),
		SHA256: "receipt-digest", LogSHA256: "log-digest", Command: "true", ProfileDigest: "profile-digest",
		Binding: checkexec.Binding{RuntimeID: "loop-rebind", Generation: 4, AssignmentID: "assignment-owner", Owner: "agent-owner", ExecutionGeneration: 2, Responsibility: "owned_check"},
	}}) {
		t.Fatal("relocation modified the caller's original history slice")
	}
	ref.Receipt.Path = filepath.Join(t.TempDir(), "outside.json")
	if _, err := relocateOwnedCheckReceipts(oldMain, newMain, []OwnedCheckReceipt{ref}); err == nil {
		t.Fatal("relocation accepted an evidence locator outside original Main")
	}
}

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
	state["bound_req"].(map[string]any)["workspace"].(map[string]any)["project_root"] = newRoot
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
