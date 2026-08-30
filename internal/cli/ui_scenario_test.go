package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProtoHeaderFailureNamesMissingTokens pins round-3 NEW-3: the UI
// prototype gate must name the page and the missing 4-field header tokens
// instead of failing silently (the skill previously taught 3 fields).
func TestProtoHeaderFailureNamesMissingTokens(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "design", "prototypes", "mod1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"stories.md", "flows.md", "scenario-model.json", "fixture-contract.json", "cross-matrix.json", "cases.json", "scenario-coverage.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// index + page carrying only the 3 fields the old skill taught.
	page := "<header class=\"proto-meta\"><span>更新: 2026-07-09</span><span>路由: /x</span><span><a href=\"index.html\">idx</a></span></header>"
	for _, name := range []string{"index.html", "page.html"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(page), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := hasCompleteUIDesignPackageForModule(root, "mod1")
	if err == nil || !strings.Contains(err.Error(), "设计代数") {
		t.Fatalf("missing 设计代数 must be named in the gate error, got: %v", err)
	}
}
