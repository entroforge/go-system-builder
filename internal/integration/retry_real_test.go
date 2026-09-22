package integration

import (
	"context"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRealGitPreservedMergedRetry(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	git := func(args ...string) string {
		t.Helper()
		b, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %v %s", args, e, b)
		}
		return string(b)
	}
	git("init", "-b", "develop")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.invalid")
	write := func(p, v string) {
		t.Helper()
		p = filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(p), 0755)
		if e := os.WriteFile(p, []byte(v), 0644); e != nil {
			t.Fatal(e)
		}
	}
	write("product.txt", "base")
	write(".claude/loop-state.json", "{}")
	git("add", ".")
	git("commit", "-m", "base")
	wt := filepath.Join(t.TempDir(), "worker")
	git("worktree", "add", "-b", "feature", wt)
	if e := os.WriteFile(filepath.Join(wt, "product.txt"), []byte("feature"), 0644); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"add", "product.txt"}, {"commit", "-m", "feature"}} {
		b, e := exec.Command("git", append([]string{"-C", wt}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	assignment := hookctx.AssignmentContext{AssignmentID: "assignment-test", TaskID: "TASK-1", WorktreePath: wt, Branch: "feature", TargetBranch: "develop", WritePaths: []string{"product.txt"}}
	inspect := func(base string) Inspection {
		t.Helper()
		i, e := Inspect(ctx, InspectRequest{Root: root, Assignment: assignment, TargetBranch: "develop", BaselineGeneration: 1}, InspectConfig{SkipCompletionCheck: true, RecoveryBase: base})
		if e != nil {
			t.Fatal(e)
		}
		return i
	}
	initial := inspect("")
	if !initial.Ready {
		t.Fatal(initial.Blockers)
	}
	// Actual tracked product dirt blocks integration and persists a failed attempt.
	write("product.txt", "dirty")
	cfg := IntegrateConfig{Root: root, RuntimeID: "loop-test", RequiredChecks: []string{"check"}, CheckRunner: func(context.Context, string, string) error { return nil }}
	first, e := Integrate(ctx, IntegrateRequest{Inspection: initial}, cfg)
	if e == nil || first.Checkpoint.State != StatePreserved {
		t.Fatalf("%+v %v", first, e)
	}
	git("restore", "product.txt")
	git("merge", "--no-ff", "feature", "-m", "external merge")
	if i := inspect(""); i.Ready {
		t.Fatal("normal inspect should require recovery after external merge")
	}
	recovered := inspect(initial.MergeBase)
	if !recovered.Ready {
		t.Fatal(recovered.Blockers)
	}
	// Runtime-only projection changes do not force users to commit live state.
	write(".claude/loop-state.json", "{\"revision\":2}")
	clean, e := worktreeClean(ctx, root)
	if e != nil || !clean {
		t.Fatalf("runtime dirt: %v %v status=%q", clean, e, git("status", "--porcelain", "-z", "--untracked-files=all"))
	}
	same, e := Integrate(ctx, IntegrateRequest{Inspection: recovered}, cfg)
	if e != nil || same.Checkpoint.State != StatePreserved {
		t.Fatalf("implicit retry: %+v %v", same, e)
	}
	checks := 0
	cfg.CheckRunner = func(context.Context, string, string) error { checks++; return fmt.Errorf("failing actual check") }
	failed, e := Integrate(ctx, IntegrateRequest{Inspection: recovered, RetryPreserved: true}, cfg)
	if e == nil || failed.Checkpoint.State == StateVerified || checks != 1 {
		t.Fatalf("failed check granted verified: %+v %v", failed, e)
	}
	cfg.CheckRunner = func(context.Context, string, string) error { checks++; return nil }
	verified, e := Integrate(ctx, IntegrateRequest{Inspection: recovered, RetryPreserved: true}, cfg)
	if e != nil || verified.Checkpoint.State != StateVerified || checks != 2 {
		t.Fatalf("retry: %+v %v", verified, e)
	}
	// Original base continues to detect scope violations even after merge.
	assignment.WritePaths = []string{"unrelated.txt"}
	if i := inspect(initial.MergeBase); i.Ready || len(i.OutOfScopeDiff) != 1 {
		t.Fatalf("scope lost: %+v", i)
	}
	// Config edits and staged runtime edits are not covered by the exception.
	write("AGENTS.md", "policy")
	git("add", "AGENTS.md")
	git("commit", "-m", "policy")
	write("AGENTS.md", "dirty")
	if clean, _ := worktreeClean(ctx, root); clean {
		t.Fatal("ignored config edit")
	}
	git("restore", "AGENTS.md")
	git("add", ".claude/loop-state.json")
	if clean, _ := worktreeClean(ctx, root); clean {
		t.Fatal("ignored staged runtime edit")
	}
}
