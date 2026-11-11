package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupUnknownIgnoredBlocksOtherwiseCleanShortcut(t *testing.T) {
	root, worker, in, cfg, git := newRecoveryGitRepo(t)
	// Runtime's own integration lock must not appear as product dirt.
	excludeMain := git(root, "rev-parse", "--git-path", "info/exclude")
	if !filepath.IsAbs(excludeMain) {
		excludeMain = filepath.Join(root, excludeMain)
	}
	mainIgnore, err := os.OpenFile(excludeMain, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mainIgnore.WriteString("\n/.claude/\n"); err != nil {
		t.Fatal(err)
	}
	if err := mainIgnore.Close(); err != nil {
		t.Fatal(err)
	}
	verified, err := Integrate(context.Background(), IntegrateRequest{Inspection: in}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	exclude := git(worker, "rev-parse", "--git-path", "info/exclude")
	body, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exclude, append(body, []byte("\nprivate.bin\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worker, "private.bin"), []byte("owner bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if status := git(worker, "status", "--porcelain"); status != "" {
		t.Fatalf("fixture should be Git-clean: %s", status)
	}
	got, err := Integrate(context.Background(), IntegrateRequest{Inspection: in, Acknowledge: true, Cleanup: true}, cfg)
	if !errors.Is(err, ErrCleanupPending) || got.Checkpoint.VerifiedAt != verified.Checkpoint.VerifiedAt {
		t.Fatalf("clean shortcut lost protection: %+v %v", got, err)
	}
	if raw, err := os.ReadFile(filepath.Join(worker, "private.bin")); err != nil || string(raw) != "owner bytes" {
		t.Fatalf("owner bytes lost: %q %v", raw, err)
	}
}
