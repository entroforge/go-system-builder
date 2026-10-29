package integration

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
)

func TestIntegrationMetricsTemporaryDoesNotGrantWorkerCleanup(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	git := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(root, rel, text string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git(root, "init", "-b", "develop")
	git(root, "config", "user.name", "Test")
	git(root, "config", "user.email", "test@example.invalid")
	write(root, "product.txt", "base")
	git(root, "add", "product.txt")
	git(root, "commit", "-m", "base")
	worker := filepath.Join(t.TempDir(), "worker")
	git(root, "worktree", "add", "-b", "feature", worker)
	write(worker, "product.txt", "feature")
	git(worker, "add", "product.txt")
	git(worker, "commit", "-m", "feature")
	assignment := hookctx.AssignmentContext{AssignmentID: "assignment-test", TaskID: "TASK-1", WorktreePath: worker, Branch: "feature", TargetBranch: "develop", WritePaths: []string{"product.txt"}}
	inspection, err := Inspect(ctx, InspectRequest{Root: root, Assignment: assignment, TargetBranch: "develop", BaselineGeneration: 1}, InspectConfig{SkipCompletionCheck: true})
	if err != nil || !inspection.Ready {
		t.Fatalf("inspect: %+v %v", inspection, err)
	}
	path := ".claude/.loop-metrics-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.tmp"
	write(root, path, "partially written")
	write(root, ".claude/product.go", "user product")
	cfg := IntegrateConfig{Root: root, RuntimeID: "loop-test"}
	failed, err := Integrate(ctx, IntegrateRequest{Inspection: inspection}, cfg)
	if !errors.Is(err, ErrDirtyWorktree) || failed.Checkpoint.State != StatePreserved {
		t.Fatalf("product neighbor did not block integration: %+v %v", failed, err)
	}
	if err := os.Remove(filepath.Join(root, ".claude", "product.go")); err != nil {
		t.Fatal(err)
	}
	completed, err := Integrate(ctx, IntegrateRequest{Inspection: inspection, RetryPreserved: true}, cfg)
	if err != nil || completed.Checkpoint.State != StateVerified {
		t.Fatalf("metrics temporary blocked original integration retry: %+v %v", completed, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path))); err != nil || string(data) != "partially written" {
		t.Fatalf("Main integration changed temporary: %q %v", data, err)
	}

	// The same shape is not permission to remove a Worker checkout.
	write(worker, path, "unknown Worker file")
	if err := removeWorktree(ctx, root, worker); !errors.Is(err, ErrDirtyWorktree) {
		t.Fatalf("worker cleanup accepted unknown file: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(worker, filepath.FromSlash(path))); err != nil || string(data) != "unknown Worker file" {
		t.Fatalf("worker cleanup removed unknown file: %q %v", data, err)
	}
	git(root, "add", "--", path)
	if clean, err := mainWorktreeClean(ctx, root); err != nil || clean {
		t.Fatalf("staged Main file hidden: %v %v", clean, err)
	}
}
