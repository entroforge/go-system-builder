package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProjectionObject(t *testing.T, root, path string, data []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0600); err != nil {
		t.Fatal(err)
	}
}

// The old content-addressed cleanup proof is no longer a deletion authority.
// Unknown ignored files are preserved even in an otherwise clean worktree.
func TestCleanupPreservesUnknownIgnoredAndUntrackedFiles(t *testing.T) {
	root := t.TempDir()
	gitRevisionTest(t, root, "init", "-b", "main")
	gitRevisionTest(t, root, "config", "user.email", "test@example.com")
	gitRevisionTest(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".claude/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRevisionTest(t, root, "add", ".gitignore")
	gitRevisionTest(t, root, "commit", "-m", "initial")
	worktree := filepath.Join(t.TempDir(), "worker")
	gitRevisionTest(t, root, "worktree", "add", "-b", "worker", worktree, "main")
	if err := os.MkdirAll(filepath.Join(worktree, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(worktree, ".claude", "unknown.json")
	if err := os.WriteFile(unknown, []byte("original private material"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupUnmodifiedWorktree(context.Background(), root, worktree); err == nil || !strings.Contains(err.Error(), "preserve") {
		t.Fatalf("unknown ignored file should preserve worktree: %v", err)
	}
	data, err := os.ReadFile(unknown)
	if err != nil || string(data) != "original private material" {
		t.Fatalf("unknown file changed or removed: %q, %v", data, err)
	}
	if err := os.Remove(unknown); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(worktree, ".claude")); err != nil {
		t.Fatal(err)
	}
	if err := cleanupUnmodifiedWorktree(context.Background(), root, worktree); err != nil {
		t.Fatalf("clean worktree should be removed: %v", err)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("clean worktree remains: %v", err)
	}
}
