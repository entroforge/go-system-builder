package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestControlledCommitRetryPreservesMain(t *testing.T) {
	b, e, _, ctx := workerFixture(t)
	before, _ := Git(ctx, b.MainRoot, "rev-parse", "HEAD")
	cwd, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(e.Path, "input.md"), []byte("worker implementation"), 0600); err != nil {
		t.Fatal(err)
	}
	req := CommitRequest{ExpectedHead: e.BaseCommit, Message: "implement assignment", Paths: []string{"input.md"}}
	first, err := b.Commit(ctx, e, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.Commit(ctx, e, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Commit == e.BaseCommit || second.Commit != first.Commit || second.State != "committed" {
		t.Fatalf("invalid retry: %+v %+v", first, second)
	}
	after, _ := Git(ctx, b.MainRoot, "rev-parse", "HEAD")
	branch, _ := Git(ctx, b.MainRoot, "branch", "--show-current")
	afterCWD, _ := os.Getwd()
	if before != after || branch != "test2" || cwd != afterCWD {
		t.Fatal("Main moved")
	}
	data, err := os.ReadFile(filepath.Join(b.MainRoot, "input.md"))
	if err != nil || string(data) != "approved" {
		t.Fatalf("Main content changed: %q %v", data, err)
	}
}

func TestControlledCommitRejectsUnsafeInputWithoutConsumingIndex(t *testing.T) {
	for _, kind := range []string{"scope", "directory", "traversal", "control", "staged", "head"} {
		t.Run(kind, func(t *testing.T) {
			b, e, _, ctx := workerFixture(t)
			os.WriteFile(filepath.Join(e.Path, "input.md"), []byte("change"), 0600)
			req := CommitRequest{ExpectedHead: e.BaseCommit, Message: "change", Paths: []string{"input.md"}}
			switch kind {
			case "scope":
				req.Paths = []string{"other.md"}
			case "directory":
				req.Paths = []string{"."}
			case "traversal":
				req.Paths = []string{"../input.md"}
			case "control":
				req.Paths = []string{".git"}
			case "head":
				req.ExpectedHead = "wrong"
			case "staged":
				os.WriteFile(filepath.Join(e.Path, "other.md"), []byte("user staging"), 0600)
				if _, err := Git(ctx, e.Path, "add", "other.md"); err != nil {
					t.Fatal(err)
				}
			}
			before, err := GitBytes(ctx, e.Path, "diff", "--cached", "--binary")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.Commit(ctx, e, req); err == nil {
				t.Fatal("unsafe commit accepted")
			}
			after, _ := GitBytes(ctx, e.Path, "diff", "--cached", "--binary")
			head, _ := Git(ctx, e.Path, "rev-parse", "HEAD")
			if string(before) != string(after) || head != e.BaseCommit {
				t.Fatal("rejected request changed staging or HEAD")
			}
		})
	}
}

func TestControlledCommitHookFailureCanRetry(t *testing.T) {
	b, e, _, ctx := workerFixture(t)
	hooks := t.TempDir()
	hook := filepath.Join(hooks, "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Git(ctx, e.Path, "config", "core.hooksPath", hooks); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(e.Path, "input.md"), []byte("change"), 0600)
	req := CommitRequest{ExpectedHead: e.BaseCommit, Message: "change", Paths: []string{"input.md"}}
	if _, err := b.Commit(ctx, e, req); err == nil || !strings.Contains(err.Error(), "commit failed") {
		t.Fatalf("hook bypassed: %v", err)
	}
	staged, _ := Git(ctx, e.Path, "diff", "--cached", "--name-only")
	if staged != "input.md" {
		t.Fatalf("lost staging: %s", staged)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Commit(ctx, e, req); err != nil {
		t.Fatal(err)
	}
}

func TestControlledCommitDetectsHookAddingUndeclaredFile(t *testing.T) {
	b, e, _, ctx := workerFixture(t)
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\nprintf unexpected > other.md\ngit add -- other.md\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Git(ctx, e.Path, "config", "core.hooksPath", hooks); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(e.Path, "input.md"), []byte("change"), 0600)
	req := CommitRequest{ExpectedHead: e.BaseCommit, Message: "change", Paths: []string{"input.md"}}
	for i := 0; i < 2; i++ {
		if _, err := b.Commit(ctx, e, req); err == nil || !strings.Contains(err.Error(), "undeclared file") {
			t.Fatalf("unexpected commit blessed: %v", err)
		}
	}
	head, _ := Git(ctx, e.Path, "rev-parse", "HEAD")
	if head == e.BaseCommit {
		t.Fatal("failure evidence removed")
	}
}
