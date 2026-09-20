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

func TestIgnoredEnvironmentMarkerDoesNotHideUnignoredCode(t *testing.T) {
	root := t.TempDir()
	boundaryGit(t, root, "init")
	boundaryWrite(t, root, ".gitignore", "tools/pyvenv.cfg\n")
	boundaryWrite(t, root, "tools/pyvenv.cfg", "home = /python\n")
	boundaryWrite(t, root, "tools/business.py", "important = True\n")

	artifacts, _, err := captureRepositoryBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range artifacts {
		if artifact.Path == "tools/business.py" {
			return
		}
	}
	t.Fatal("unignored business code hidden by ignored environment marker")
}
