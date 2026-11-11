package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/submission"
)

// Exercise the original native hook, after real preparation/worktree creation.
// Any subsequent Git call makes Plan reception fail; Main integration is separate.
func TestNativeS9OriginalPlanReceptionDoesNotRequireGitOrResult(t *testing.T) {
	root, worker, p, input, _ := nativeS9WorkerFixture(t)
	plan, result := nativeS9TestAuthorPaths(t, root, p)
	original, err := os.ReadFile(filepath.Join(worker, plan))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(worker, result)); err != nil {
		t.Fatal(err)
	}
	mainDraft, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexit 97\n"), 0755); err != nil {
		t.Fatal(err)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+originalPath)
	input.Event = "SubagentStop"
	var out, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 {
		t.Fatalf("original hook: handled=%t code=%d %s %s", handled, code, &out, &diagnostic)
	}
	t.Setenv("PATH", originalPath)
	service, err := submission.NewReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.RepairPlanReportReceipt(p.Binding.OwnerAgentID, p.ID); err != nil || !receipt.Accepted {
		t.Fatalf("Plan receipt=%+v err=%v diagnostic=%s", receipt, err, &diagnostic)
	}
	if got, err := os.ReadFile(filepath.Join(worker, plan)); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("author Plan was changed: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, p.AnswerPath)); err != nil || !bytes.Equal(got, mainDraft) {
		t.Fatalf("Main draft was rewritten as a second author copy: %v", err)
	}
	if err := os.Remove(filepath.Join(worker, plan)); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	out.Reset()
	diagnostic.Reset()
	if _, code := nativeS9Hook(root, input, &out, &diagnostic); code != 0 {
		t.Fatalf("accepted replay reread original: %s %s", &out, &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
}
