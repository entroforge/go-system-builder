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
func TestBaselineMissingGitDoesNotInventWorktreeBoundary(t *testing.T) {
	root := t.TempDir()
	boundaryWrite(t, root, ".worktrees/worker/code.go", "protected")
	boundaryWrite(t, root, ".venv/pyvenv.cfg", "marker")
	if len(excludedBaselinePaths(root, []string{".worktrees/worker/code.go", ".venv/pyvenv.cfg"})) != 0 {
		t.Fatal("unverifiable environment must remain protected")
	}
}
