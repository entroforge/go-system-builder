package repair

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func boundaryGit(t *testing.T, root string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}
func boundaryWrite(t *testing.T, root, path, value string) {
	t.Helper()
	p := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(value), 0644); err != nil {
		t.Fatal(err)
	}
}
func TestBaselineSeparatesRegisteredWorktreeAndMarkedEnvironment(t *testing.T) {
	root := t.TempDir()
	boundaryGit(t, root, "init")
	boundaryWrite(t, root, ".gitignore", ".worktrees/\n.venv/\npretend/\n")
	boundaryWrite(t, root, "app.go", "package app\n")
	boundaryGit(t, root, "add", ".")
	boundaryGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "base")
	boundaryGit(t, root, "worktree", "add", "--detach", filepath.Join(root, ".worktrees", "worker"))
	boundaryWrite(t, root, ".venv/pyvenv.cfg", "home = /python\n")
	boundaryWrite(t, root, ".venv/lib/pkg.py", "runtime")
	boundaryWrite(t, root, ".worktrees/not-a-worktree/code.go", "protected")
	boundaryWrite(t, root, "pretend/code.go", "protected")
	paths := []string{".worktrees/worker/app.go", ".worktrees/worker/.git", ".venv/pyvenv.cfg", ".venv/lib/pkg.py", ".worktrees/not-a-worktree/code.go", "pretend/code.go", "app.go"}
	excluded := excludedBaselinePaths(root, paths)
	for i, p := range paths {
		if excluded[p] != (i < 4) {
			t.Fatalf("classification %s=%v", p, excluded[p])
		}
	}
	before, _, err := captureRepositoryBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	boundaryWrite(t, root, ".worktrees/worker/app.go", "external task change")
	boundaryWrite(t, root, ".venv/lib/pkg.py", "cache changes")
	boundaryWrite(t, root, "app.go", "package changed\n")
	// Legacy sessions may already contain excluded artifacts; neither mutation
	// nor their absence from capture may create a phantom deletion.
	before = append(before, ArtifactRef{Path: paths[0], SHA256: "legacy"}, ArtifactRef{Path: paths[3], SHA256: "legacy"})
	changes, err := ComputeSessionChangeset(root, RepairSession{BaselineArtifacts: before})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "app.go" {
		t.Fatalf("changes=%v", changes)
	}
	boundaryGit(t, root, "add", "-f", ".venv/lib/pkg.py")
	if excludedBaselinePaths(root, paths)[".venv/lib/pkg.py"] {
		t.Fatal("tracked environment content must remain protected")
	}
}
func TestBaselineMissingGitDoesNotInventWorktreeBoundary(t *testing.T) {
	root := t.TempDir()
	boundaryWrite(t, root, ".worktrees/worker/code.go", "protected")
	boundaryWrite(t, root, ".venv/pyvenv.cfg", "marker")
	if len(excludedBaselinePaths(root, []string{".worktrees/worker/code.go", ".venv/pyvenv.cfg"})) != 0 {
		t.Fatal("unverifiable environment must remain protected")
	}
}
