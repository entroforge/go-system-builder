package buildsurface

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitFixture(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Git fixture %v: %v %s", args, err, out)
	}
	return string(out)
}
func writeGitFixture(t *testing.T, root, file, body string) {
	t.Helper()
	p := filepath.Join(root, file)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestProductDiffPreservesLiteralPathsAndRealDeletionPreimage(t *testing.T) {
	root := t.TempDir()
	gitFixture(t, root, "init", "-b", "main")
	gitFixture(t, root, "config", "user.name", "Fixture")
	gitFixture(t, root, "config", "user.email", "fixture@example.invalid")
	file := "src/removed [literal]\n.go"
	writeGitFixture(t, root, file, "original\n")
	writeGitFixture(t, root, "src/modified.go", "before\n")
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "commit", "-m", "start")
	start, err := gitText(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	writeGitFixture(t, root, file, "latest preimage\n")
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "commit", "-m", "modify before delete")
	preimage, _ := gitText(root, "rev-parse", "HEAD")
	if err := os.Remove(filepath.Join(root, file)); err != nil {
		t.Fatal(err)
	}
	writeGitFixture(t, root, "src/added space.go", "new\n")
	writeGitFixture(t, root, "src/modified.go", "after\n")
	writeGitFixture(t, root, "docs/feature.md", "product documentation\n")
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "commit", "-m", "delete and add")
	final, _ := gitText(root, "rev-parse", "HEAD")
	rows, err := changedPaths(root, start, final)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("full product diff omitted a path: %+v", rows)
	}
	found := false
	for _, row := range rows {
		if row.Path == file {
			found = true
			if row.Status != "deleted" || row.PreimageCommit != preimage || row.DeletedByCommit != final || row.SHA256 != hashBytes([]byte("latest preimage\n")) {
				t.Fatalf("false deletion source: %+v", row)
			}
		}
	}
	if !found {
		t.Fatal("literal newline/bracket path was lost")
	}
	if err := verifyCheckout(root, final); err != nil {
		t.Fatal(err)
	}
	writeGitFixture(t, root, "src/dirty space.go", "dirty\n")
	if err := verifyCheckout(root, final); err == nil {
		t.Fatal("untracked spaced product path bypassed pinned checkout")
	}
}

func TestCanonicalSnapshotRequiresOriginalAuthority(t *testing.T) {
	state := map[string]any{"runtime_id": "loop-test", "baseline": map[string]any{"generation": 1}, "entities": map[string]any{"assignments": []any{map[string]any{"runtime_id": "loop-test", "baseline_generation": 1}}}}
	if !HasCanonical(state) {
		t.Fatal("canonical row silently became legacy")
	}
	if _, err := Read(t.TempDir(), state); err == nil {
		t.Fatal("canonical start inferred without original root/receipt")
	}
	if err := Verify(t.TempDir(), state, Snapshot{Contract: Contract, RuntimeID: "loop-test", BaselineGeneration: 1, Digest: "self-authored"}); err == nil {
		t.Fatal("self-authored snapshot became authority")
	}
}
