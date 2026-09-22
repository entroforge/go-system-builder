// System-level recovery after directory removal succeeds but completion
// persistence is lost. The explicit command must not re-merge.

package req039_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCT03917_SubagentStopIdempotentResumeAfterMerge covers CT-039-17:
// repeated explicit integration completes cleanup without re-merging.
func TestCT03917_SubagentStopIdempotentResumeAfterMerge(t *testing.T) {
	root := freshRoot(t)
	wt := seedIntegrableAssignment(t, root)
	code, out, errText := runTaskIntegrate(t, root, "assignment-ti")
	if code != 0 {
		t.Fatalf("initial integration: %d %s %s", code, out, errText)
	}
	head := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
	cpPath, _ := readIntegrationCheckpoint(t, root)
	data, err := os.ReadFile(cpPath)
	if err != nil {
		t.Fatal(err)
	}
	var cp map[string]any
	if err = json.Unmarshal(data, &cp); err != nil {
		t.Fatal(err)
	}
	// Simulate lost completion persistence after successful directory removal.
	cp["state"] = "cleanup_pending"
	data, _ = json.Marshal(cp)
	if err = os.WriteFile(cpPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	code, out, errText = runTaskIntegrate(t, root, "assignment-ti")
	if code != 0 {
		t.Fatalf("cleanup recovery: %d %s %s", code, out, errText)
	}
	if readIntegrationCheckpointState(t, root) != "complete" {
		t.Fatalf("cleanup response loss did not recover: %s %s", out, errText)
	}
	if strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD")) != head {
		t.Fatal("recovery remerged")
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("removed checkout was recreated")
	}
}

type gitWorktreeFixture struct {
	wtPath      string
	branch      string
	developHEAD func() string
}

func setupGitWorktreeFixture(t *testing.T, root string) gitWorktreeFixture {
	t.Helper()
	for _, args := range [][]string{
		{"init", "--initial-branch=develop"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"checkout", "-b", "develop"},
		{"commit", "--allow-empty", "-m", "initial"},
	} {
		runGitIn(t, root, args...)
	}
	wtPath := filepath.Join(root, "wt-feature")
	runGitIn(t, root, "worktree", "add", "-b", "codex/feature", wtPath, "develop")
	runGitIn(t, wtPath, "commit", "--allow-empty", "-m", "feature")
	head := func() string {
		out := runGitIn(t, root, "rev-parse", "develop")
		return strings.TrimSpace(out)
	}
	return gitWorktreeFixture{wtPath: wtPath, branch: "codex/feature", developHEAD: head}
}

func runGitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	// Fixtures must not depend on the developer's global Git identity.
	cmd := exec.Command("git", append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}
