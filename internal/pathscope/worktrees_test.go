package pathscope

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWorktreeBoundary(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); e != nil {
			t.Fatalf("git %v: %s %v", args, out, e)
		}
	}
	git("init", "-q")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-qm", "initial")
	custom := filepath.Join(root, "scratch", "worker")
	git("worktree", "add", "--detach", custom, "HEAD")
	s := New(root)
	for _, p := range []string{".worktrees/a/file.go", ".claude/worktrees/b/file.go", "scratch/worker/new.go"} {
		if !s.Excludes(p) {
			t.Errorf("not excluded: %s", p)
		}
	}
	for _, p := range []string{"file.go", "scratch/worker-other/file.go", ".worktrees-other/file.go"} {
		if s.Excludes(p) {
			t.Errorf("false exclusion: %s", p)
		}
	}
	if e := os.Symlink(custom, filepath.Join(root, "alias")); e != nil {
		t.Fatal(e)
	}
	if !s.Excludes("alias/new.go") {
		t.Fatal("symlink escape included")
	}
	if New(custom).Excludes(filepath.Join(custom, "new.go")) {
		t.Fatal("linked authority excluded itself")
	}
}
