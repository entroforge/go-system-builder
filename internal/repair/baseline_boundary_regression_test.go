package repair

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRelativeRootKeepsRegisteredWorktreeBoundary(t *testing.T) {
	root := t.TempDir()
	boundaryGit(t, root, "init")
	boundaryWrite(t, root, "app.go", "package app\n")
	boundaryGit(t, root, "add", ".")
	boundaryGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "base")
	boundaryGit(t, root, "worktree", "add", "--detach", filepath.Join(root, "workers", "one"))

	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	if !excludedBaselinePaths(".", []string{"workers/one/app.go"})["workers/one/app.go"] {
		t.Fatal("relative root loses registered-worktree exclusion")
	}
}
