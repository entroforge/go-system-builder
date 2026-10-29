package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestFreezeAssignmentReadClosureUsesPinnedGitFilesAndExistingMatcher(t *testing.T) {
	files := map[string][]byte{
		"docs/architecture/data-model/model.md":  []byte("model source\n"),
		"docs/architecture/state/reservation.md": []byte("reservation source\n"),
		"docs/architecture/state/readme.txt":     []byte("not selected\n"),
	}
	root, state := newAssignmentReadGitRepo(t, files, nil)
	view, paths, hashes, err := freezeAssignmentReadClosure(context.Background(), root, state, []string{
		"docs/architecture/data-model/model.md",
		"docs/architecture/state/*.md",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "docs/architecture/data-model/model.md" || paths[1] != "docs/architecture/state/reservation.md" {
		t.Fatalf("declared read closure expanded to the wrong files: %v", paths)
	}
	branchHead, err := workspace.Git(context.Background(), root, "rev-parse", "refs/heads/closure-development")
	if err != nil || view.Commit != branchHead {
		t.Fatalf("closure is not pinned to the bound development commit: view=%s branch=%s err=%v", view.Commit, branchHead, err)
	}
	for _, rel := range paths {
		if hashes[rel] != workpacket.Digest(files[rel]) {
			t.Fatalf("pinned SHA differs for %s", rel)
		}
	}
}

func TestFreezeAssignmentReadClosureRejectsMissingEscapesAndLinks(t *testing.T) {
	root, state := newAssignmentReadGitRepo(t, map[string][]byte{"docs/model.md": []byte("model\n"), "target.txt": []byte("target\n")}, map[string]string{"docs/linked.md": "../target.txt"})
	for _, declaration := range []string{"docs/missing.md", "../outside.md", "docs/../../outside.md", "/outside.md", "docs\\model.md"} {
		t.Run(strings.ReplaceAll(declaration, "/", "_"), func(t *testing.T) {
			_, _, _, err := freezeAssignmentReadClosure(context.Background(), root, state, []string{declaration}, "")
			if err == nil {
				t.Fatalf("accepted missing or escaping declaration %q", declaration)
			}
		})
	}
	if _, _, _, err := freezeAssignmentReadClosure(context.Background(), root, state, []string{"docs/linked.md"}, ""); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("accepted a tracked Git symlink: %v", err)
	}
	if _, _, _, err := freezeAssignmentReadClosure(context.Background(), root, state, []string{"docs/"}, ""); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("directory expansion hid a tracked symlink: %v", err)
	}
}

func TestFreezeAssignmentReadClosureRejectsCheckoutDriftAndSymlink(t *testing.T) {
	t.Run("changed bytes", func(t *testing.T) {
		root, state := newAssignmentReadGitRepo(t, map[string][]byte{"docs/model.md": []byte("pinned\n")}, nil)
		if err := os.WriteFile(filepath.Join(root, "docs/model.md"), []byte("changed\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := freezeAssignmentReadClosure(context.Background(), root, state, []string{"docs/model.md"}, ""); err == nil || !strings.Contains(err.Error(), "differs from pinned Git blob") {
			t.Fatalf("accepted changed Main checkout bytes: %v", err)
		}
	})
	t.Run("checkout symlink", func(t *testing.T) {
		root, state := newAssignmentReadGitRepo(t, map[string][]byte{"docs/model.md": []byte("pinned\n"), "target.txt": []byte("target\n")}, nil)
		if err := os.Remove(filepath.Join(root, "docs/model.md")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../target.txt", filepath.Join(root, "docs/model.md")); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := freezeAssignmentReadClosure(context.Background(), root, state, []string{"docs/model.md"}, ""); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("accepted a checkout symlink over a regular Git blob: %v", err)
		}
	})
}

func TestFreezeAssignmentReadClosureEnforcesEntryAndByteBudgets(t *testing.T) {
	t.Run("entries", func(t *testing.T) {
		files := map[string][]byte{}
		for i := 0; i < assignmentReadEntryLimit+1; i++ {
			files[fmt.Sprintf("docs/bulk/%03d.md", i)] = []byte("source\n")
		}
		root, state := newAssignmentReadGitRepo(t, files, nil)
		if _, _, _, err := freezeAssignmentReadClosure(context.Background(), root, state, []string{"docs/bulk/"}, ""); err == nil || !strings.Contains(err.Error(), "256 Git entries") {
			t.Fatalf("directory expansion exceeded its entry bound: %v", err)
		}
	})
	t.Run("bytes", func(t *testing.T) {
		root, state := newAssignmentReadGitRepo(t, map[string][]byte{"docs/large.md": make([]byte, assignmentReadByteLimit+1)}, nil)
		if _, _, _, err := freezeAssignmentReadClosure(context.Background(), root, state, []string{"docs/large.md"}, ""); err == nil || !strings.Contains(err.Error(), "bytes") {
			t.Fatalf("read closure exceeded its byte bound: %v", err)
		}
	})
}

func newAssignmentReadGitRepo(t *testing.T, files map[string][]byte, symlinks map[string]string) (string, map[string]any) {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "closure-development"},
		{"config", "user.email", "read-closure@example.invalid"},
		{"config", "user.name", "Read Closure Test"},
	} {
		if _, err := workspace.Git(context.Background(), root, args...); err != nil {
			t.Fatal(err)
		}
	}
	for rel, data := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for rel, target := range symlinks {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, full); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", "source closure fixture"}} {
		if _, err := workspace.Git(context.Background(), root, args...); err != nil {
			t.Fatal(err)
		}
	}
	state := map[string]any{"bound_req": map[string]any{"workspace": map[string]any{"dev_branch": "closure-development"}}}
	return root, state
}
