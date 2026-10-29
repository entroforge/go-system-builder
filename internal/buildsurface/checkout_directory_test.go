package buildsurface

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyCheckoutHandlesGitDirectoryMarkersWithoutIgnoringProductDirectories(t *testing.T) {
	root := t.TempDir()
	gitFixture(t, root, "init", "-b", "main")
	gitFixture(t, root, "config", "user.name", "Fixture")
	gitFixture(t, root, "config", "user.email", "fixture@example.invalid")
	writeGitFixture(t, root, "src/product.go", "package product\n")
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "commit", "-m", "product baseline")
	head, err := gitText(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	gitFixture(t, root, "worktree", "add", "-b", "retained-control", ".worktrees/retained")
	status := gitFixture(t, root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if !strings.Contains(status, "?? .worktrees/retained/\x00") {
		t.Fatalf("fixture did not reproduce Git's nested worktree directory marker: %q", status)
	}
	if err := verifyCheckout(root, head); err != nil {
		t.Fatalf("retained control worktree should use the existing non-product projection: %v", err)
	}
	// Framework staging (.loop-submissions/) holds S7 review answers and
	// Worker drafts; an S7 answer landing there must not stale the round
	// that issued it (same-round S7 self-blocking).
	if err := os.MkdirAll(filepath.Join(root, ".loop-submissions/s7/packet-fixture/reviewer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".loop-submissions/s7/packet-fixture/reviewer/answer.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyCheckout(root, head); err != nil {
		t.Fatalf("framework staging must not stale the round: %v", err)
	}
	gitFixture(t, root, "worktree", "add", "-b", "untracked-product", "product-nested")
	if err := verifyCheckout(root, head); err == nil || !strings.Contains(err.Error(), "product checkout differs from pinned Main: product-nested") {
		t.Fatalf("untracked product directory must remain rejected: %v", err)
	}
	if current, err := gitText(root, "rev-parse", "HEAD"); err != nil || current != head {
		t.Fatalf("read-only checkout validation changed HEAD: %s %v", current, err)
	}
}
