package designfoundation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoadTokensAndEmitCSSMatchesCheckedInFile(t *testing.T) {
	root := repoRoot(t)
	tf, err := LoadTokens(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tf.ColorHexes()["#2563eb"]; !ok {
		t.Fatal("expected action.promise hex to be registered")
	}
	got := tf.CSS()
	want, err := os.ReadFile(filepath.Join(root, TokensCSSRel))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("tokens.css drifted from tokens.json; run loop-harness design-foundation emit-css --root .\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestCheckTemplateFactoryIsAdvisoryClean(t *testing.T) {
	root := repoRoot(t)
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if warns := report.Warnings(); len(warns) > 0 {
		t.Fatalf("template factory should not warn, got %#v", warns)
	}
}

func TestCheckWarnsOnChangedREQWithoutFoundation(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"packages/design-tokens/tokens.json": mustRead(t, filepath.Join(repoRoot(t), TokensJSONRel)),
		"docs/requirements/REQ-014.md":       "# REQ-014\n\n> 状态：locked\n> UI impact：changed\n\n| Foundation reference | pending-foundation |\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, f := range report.Warnings() {
		codes[f.Code] = true
	}
	for _, want := range []string{"foundation_missing", "req_foundation_ref", "derivation_missing"} {
		if !codes[want] {
			t.Fatalf("missing warning %s in %#v", want, report.Findings)
		}
	}
}

func TestLintUnregisteredHex(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"packages/design-tokens/tokens.json":         mustRead(t, filepath.Join(repoRoot(t), TokensJSONRel)),
		"docs/design/prototypes/fund/fund-list.html": "<div style=\"color:#ff00aa\"></div>\n",
	})
	findings, err := LintUnregisteredHex(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Code != "unregistered_hex" {
		t.Fatalf("got %#v", findings)
	}
}

func TestLintSkipsTemplatesPortableAndStyleTiles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"packages/design-tokens/tokens.json":                     mustRead(t, filepath.Join(repoRoot(t), TokensJSONRel)),
		"docs/design/proof/style-tiles/STYLE-TILE-template.html": "<div style=\"color:#ff00aa\"></div>\n",
		"docs/design/proof/style-tiles/direction-a.html":         "<div style=\"color:#b8422e\"></div>\n",
		"docs/design/proof/portable/DESIGN.md":                         "color: #ff00aa\n",
		"docs/design/README.md":                                  "# #ff00aa\n",
	})
	findings, err := LintUnregisteredHex(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("templates/portable/README/F2 style tiles must be skipped, got %#v", findings)
	}
}

func TestExportPortableOmitsComponents(t *testing.T) {
	root := repoRoot(t)
	body := renderPortable(mustTokens(t, root), "", "")
	if !strings.Contains(body, "section: components") {
		t.Fatal("expected components to be omitted")
	}
	if strings.Contains(body, "## Components") {
		t.Fatal("portable snapshot must not include a Components section")
	}
}

func TestLintUnregisteredRgbAndHsl(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"packages/design-tokens/tokens.json":         mustRead(t, filepath.Join(repoRoot(t), TokensJSONRel)),
		"docs/design/prototypes/fund/fund-list.html": "<div style=\"color:rgb(255, 0, 170)\"></div>\n",
	})
	findings, err := LintUnregisteredHex(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Code != "unregistered_rgb" {
		t.Fatalf("got %#v", findings)
	}

	writeTree(t, root, map[string]string{
		"docs/design/prototypes/fund/fund-list.html": "<div style=\"color:rgb(37, 99, 235)\"></div>\n",
	})
	findings, err = LintUnregisteredHex(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("token-equivalent rgb must pass, got %#v", findings)
	}
}

func TestDuplicatePopupOverlayCountsAsDialog(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"docs/design/prototypes/a/popup.html":   "<p/>",
		"docs/design/prototypes/b/overlay.html": "<p/>",
	})
	findings, err := LintDuplicateComponents(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) == 0 {
		t.Fatal("expected dialog repeat warning for popup+overlay")
	}
	if findings[0].Code != "component_repeat" || !strings.Contains(findings[0].Detail, "dialog") {
		t.Fatalf("got %#v", findings)
	}
}

func mustTokens(t *testing.T, root string) *tokenFile {
	t.Helper()
	tf, err := LoadTokens(root)
	if err != nil {
		t.Fatal(err)
	}
	return tf
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
