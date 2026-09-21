package req039_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeCreateUsesBoundCommitAndReusesAssignment(t *testing.T) {
	root := freshRoot(t)
	existing := seedIntegrableAssignment(t, root)
	runGitIn(t, root, "worktree", "remove", existing)
	writeWorkgroupWithWorktree(t, root, "TASK-039-01", "assignment-ti", "builder-ti", "", "")
	baseline := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop"))
	runGitIn(t, root, "checkout", "-b", "unrelated")
	runGitIn(t, root, "commit", "--allow-empty", "-m", "unrelated branch commit")
	if err := os.WriteFile(filepath.Join(root, "uncommitted.md"), []byte("parent draft"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "add", "uncommitted.md")
	create := func() map[string]any {
		t.Helper()
		var out, stderr bytes.Buffer
		if code := runCLI(t, []string{"runtime", "worktree-create", "--root", root, "--assignment-id", "assignment-ti"}, strings.NewReader(""), &out, &stderr); code != 0 {
			t.Fatalf("create: %s %s", out.String(), stderr.String())
		}
		var data map[string]any
		if err := json.Unmarshal(out.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	first := create()
	path := first["worktree_path"].(string)
	if head := strings.TrimSpace(runGitIn(t, path, "rev-parse", "HEAD")); head != baseline {
		t.Fatalf("base=%s want bound %s", head, baseline)
	}
	if _, err := os.Stat(filepath.Join(path, "uncommitted.md")); !os.IsNotExist(err) {
		t.Fatalf("parent staged file was copied: %v", err)
	}
	before := runGitIn(t, root, "worktree", "list", "--porcelain")
	second := create()
	if second["worktree_path"] != path || runGitIn(t, root, "worktree", "list", "--porcelain") != before {
		t.Fatal("retry expanded worktrees")
	}
	if branch := strings.TrimSpace(runGitIn(t, root, "branch", "--show-current")); branch != "unrelated" {
		t.Fatal("creation switched authority branch")
	}
}

func TestIntegrationRecoversLostMergeReceiptWithoutMergingAgain(t *testing.T) {
	root := freshRoot(t)
	seedIntegrableAssignment(t, root)
	code, out, errout := runTaskIntegrate(t, root, "assignment-ti")
	if code != 0 {
		t.Fatalf("initial reception: %s %s", out, errout)
	}
	head := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop"))
	path := filepath.Join(root, ".claude", "evidence", "loop-system-test", "g1", "worktree", "assignment-ti", "checkpoint.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cp map[string]any
	if err := json.Unmarshal(raw, &cp); err != nil {
		t.Fatal(err)
	}
	// Simulate a ready receipt surviving while the merge response was lost.
	cp["state"] = "ready"
	delete(cp, "merge_commit")
	raw, _ = json.Marshal(cp)
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	code, out, errout = runTaskIntegrate(t, root, "assignment-ti")
	if code != 0 || !strings.Contains(out, "complete") {
		t.Fatalf("resume: %s %s", out, errout)
	}
	if got := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop")); got != head {
		t.Fatal("response-loss recovery merged again")
	}
}
