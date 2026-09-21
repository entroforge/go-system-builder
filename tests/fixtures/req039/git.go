package req039fixtures

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// CommitFixture represents a completed stage's delivered fixture. Tests that
// exercise dirty/index-only behavior must mutate after this explicit boundary.
func CommitFixture(t *testing.T, root string) string        { return commitFixture(t, root, true) }
func CommitCurrentFixture(t *testing.T, root string) string { return commitFixture(t, root, false) }
func commitFixture(t *testing.T, root string, rename bool) string {
	t.Helper()
	abs, _ := filepath.Abs(root)
	repo, _ := filepath.Abs(RepoRoot(t))
	if abs == repo {
		t.Fatal("refusing to commit the development repository")
	}
	git := func(args ...string) {
		t.Helper()
		if out, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); e != nil {
			t.Fatalf("fixture git %v: %v %s", args, e, out)
		}
	}
	if _, e := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(e) {
		git("init", "-qb", "test-development")
	} else if rename {
		git("branch", "-M", "test-development")
	}
	// Harness integration invokes Git itself, so command-scoped commit flags
	// are insufficient. Linked worktrees inherit this fixture-local identity;
	// no developer or CI runner global configuration is required.
	git("config", "--local", "user.name", "Fixture")
	git("config", "--local", "user.email", "fixture@example.com")
	git("config", "--local", "commit.gpgsign", "false")
	_ = os.MkdirAll(filepath.Join(root, ".git/info"), 0755)
	_ = os.WriteFile(filepath.Join(root, ".git/info/exclude"), []byte(".claude/\n.worktrees/\nwt/\nwt-feature/\n"), 0644)
	git("add", "-A")
	git("-c", "commit.gpgsign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.com", "commit", "--allow-empty", "-qm", "delivered fixture")
	return root
}
