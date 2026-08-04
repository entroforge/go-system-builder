package team_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSKILLCitationsResolve asserts a template-factory invariant:
// skills/team-planning/SKILL.md must NOT cite any `internal/...` Go source
// path. SKILL.md is a template asset shipped to target projects via the
// release tarball; `internal/` is Go source that target projects do not
// have. A citation there would be a broken pointer in every installed
// project.
//
// History: BUG-003 C1 originally had this test assert the opposite — that
// every `internal/.../*.go:symbol` citation in SKILL.md resolved to a real
// Go file and symbol. After the template-cleanup pass removed all such
// citations (because target projects lack `internal/`), the test was
// inverted to prevent regressions: any new `internal/.../*.go` reference
// in SKILL.md fails this test.
func TestSKILLCitationsResolve(t *testing.T) {
	skillPath := locateSKILL(t)
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read SKILL: %v", err)
	}
	content := string(data)

	citationPattern := regexp.MustCompile("`internal/[A-Za-z0-9_/]+\\.go(?::[A-Za-z0-9_]+)?`")
	hits := citationPattern.FindAllString(content, -1)
	if len(hits) > 0 {
		t.Fatalf("template-factory invariant: skills/team-planning/SKILL.md must not cite internal/ Go source paths "+
			"(target projects do not have internal/) — found: %s", strings.Join(hits, ", "))
	}
}

// locateSKILL finds skills/team-planning/SKILL.md relative to the test working
// directory. The test runs from internal/team/, so the path is ../../skills/...
// but the helper also tolerates being run from elsewhere.
func locateSKILL(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "skills", "team-planning", "SKILL.md"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Fatalf("skills/team-planning/SKILL.md not found under any candidate from %s", wd)
	return ""
}
