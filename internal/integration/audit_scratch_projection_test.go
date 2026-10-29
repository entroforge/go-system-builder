package integration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeProjectionRecognizesOnlyExactAuditIndexScratchFiles(t *testing.T) {
	for _, path := range []string{".claude/hook-audit-index-0.scratch", ".claude/hook-audit-index-1234567890.scratch"} {
		if !runtimeProjection(path) {
			t.Fatalf("audit cache scratch was product drift: %s", path)
		}
	}
	for _, path := range []string{
		".claude/hook-audit-index-.scratch", ".claude/hook-audit-index--123.scratch", ".claude/hook-audit-index-abc.scratch",
		".claude/hook-audit-index-123.scratch.go", ".claude/hook-audit-index-123.scratch/product.go", ".claude/nested/hook-audit-index-123.scratch",
		"hook-audit-index-123.scratch", ".claude/hooks/hook-audit-index-123.scratch", ".claude/hook-audit-index-１２３.scratch",
	} {
		if runtimeProjection(path) {
			t.Fatalf("neighbor acquired audit cache exemption: %s", path)
		}
	}
	root := t.TempDir()
	rel := ".claude/hook-audit-index-123.scratch"
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("partially written cache"), 0600); err != nil {
		t.Fatal(err)
	}
	if !runtimeProjection(rel, root) {
		t.Fatal("regular audit scratch was not recognized")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if runtimeProjection(rel, root) {
		t.Fatal("directory with audit scratch name became a file projection")
	}
}
