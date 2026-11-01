package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Cleanup follows the already verified checkpoint and actual worktree state.
// Historical digests and Git object IDs do not authorize removal of files.
func TestCleanupNoHashPreservesUnknownAndRemovesCleanWorktree(t *testing.T) {
	for _, tc := range []struct {
		name   string
		file   string
		data   string
		cancel bool
	}{
		{name: "clean"},
		{name: "tracked modification", file: "tracked.txt", data: "changed"},
		{name: "untracked", file: "private.txt", data: "private"},
		{name: "ignored", file: "private/answer.txt", data: "answer"},
		{name: "observation failure", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			gitRevisionTest(t, root, "init", "-b", "main")
			gitRevisionTest(t, root, "config", "user.email", "test@example.com")
			gitRevisionTest(t, root, "config", "user.name", "Test")
			if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("private/\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			gitRevisionTest(t, root, "add", ".gitignore", "tracked.txt")
			gitRevisionTest(t, root, "commit", "-m", "initial")
			worker := filepath.Join(t.TempDir(), "worker")
			gitRevisionTest(t, root, "worktree", "add", "-b", "worker", worker, "main")
			if tc.file != "" {
				path := filepath.Join(worker, filepath.FromSlash(tc.file))
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			if tc.cancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			err := cleanupUnmodifiedWorktree(ctx, root, worker)
			if tc.file == "" && !tc.cancel {
				if err != nil {
					t.Fatalf("clean worktree should be removable without OIDs: %v", err)
				}
				if _, err := os.Stat(worker); !os.IsNotExist(err) {
					t.Fatalf("clean worktree remains: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("unknown content or observation failure must retain worktree")
			}
			if _, err := os.Stat(worker); err != nil {
				t.Fatalf("worktree was removed: %v", err)
			}
			if tc.file != "" {
				data, err := os.ReadFile(filepath.Join(worker, filepath.FromSlash(tc.file)))
				if err != nil || string(data) != tc.data {
					t.Fatalf("original content not preserved: %q %v", data, err)
				}
			}
		})
	}
}
