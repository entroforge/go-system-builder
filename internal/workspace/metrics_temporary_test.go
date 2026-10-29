package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanInputsMetricsTemporaryKeepsProductAndStagedChanges(t *testing.T) {
	b, ctx := repository(t)
	root := b.MainRoot
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".worktrees/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", ".gitignore"}, {"commit", "-m", "expose control files"}} {
		if _, err := Git(ctx, root, args...); err != nil {
			t.Fatal(err)
		}
	}
	path := ".claude/.loop-metrics-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.tmp"
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.WriteFile(full, []byte("partially written"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.CleanInputs(ctx); err != nil {
		t.Fatalf("metrics staging blocked Main dispatch: %v", err)
	}
	neighbor := filepath.Join(root, ".claude/product.go")
	if err := os.WriteFile(neighbor, []byte("user product"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.CleanInputs(ctx); err == nil || !strings.Contains(err.Error(), ".claude/product.go") {
		t.Fatalf("product neighbor hidden: %v", err)
	}
	if err := os.Remove(neighbor); err != nil {
		t.Fatal(err)
	}
	if _, err := Git(ctx, root, "add", "--", path); err != nil {
		t.Fatal(err)
	}
	if err := b.CleanInputs(ctx); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("staged temporary-shaped file hidden: %v", err)
	}
	if _, err := Git(ctx, root, "commit", "-m", "tracked name"); err != nil {
		t.Fatal(err)
	}
	if _, err := Git(ctx, root, "rm", "--", path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("untracked replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.CleanInputs(ctx); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("staged deletion plus untracked replacement hidden: %v", err)
	}
	if data, err := os.ReadFile(full); err != nil || string(data) != "untracked replacement" {
		t.Fatalf("check changed unknown file: %q %v", data, err)
	}
}
