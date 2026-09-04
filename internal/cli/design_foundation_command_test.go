package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesignFoundationCheckIsAdvisoryOnTemplateFactory(t *testing.T) {
	root := filepath.Join("..", "..")
	stdout, stderr, code := runCLI(t, root, "design-foundation", "check", "--root", root)
	if code != 0 {
		t.Fatalf("factory check must exit 0: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if strings.Contains(stdout, "[warning]") {
		t.Fatalf("template factory should not warn: %s", stdout)
	}
}

func TestDesignFoundationCheckStrictFailsOnWarnings(t *testing.T) {
	root := t.TempDir()
	stdout, stderr, code := runCLI(t, root, "design-foundation", "check", "--root", root)
	if code != 0 {
		t.Fatalf("advisory check must exit 0: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	stdout, stderr, code = runCLI(t, root, "design-foundation", "check", "--root", root, "--strict")
	if code != 1 {
		t.Fatalf("strict check must exit 1 when warnings exist: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestDesignFoundationCheckJSONAndEmitCSS(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join("..", "..", "packages", "design-tokens", "tokens.json")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "packages", "design-tokens", "tokens.json")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, root, "design-foundation", "check", "--root", root, "--json")
	if code != 0 {
		t.Fatalf("json check failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("json report: %v (%s)", err, stdout)
	}

	stdout, stderr, code = runCLI(t, root, "design-foundation", "emit-css", "--root", root)
	if code != 0 {
		t.Fatalf("emit-css failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "packages", "design-tokens", "tokens.css")); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code = runCLI(t, root, "design-foundation", "export-portable", "--root", root)
	if code != 0 {
		t.Fatalf("export-portable failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	body, err := os.ReadFile(filepath.Join(root, "docs", "design", "proof", "portable", "DESIGN.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "## Components") {
		t.Fatal("portable snapshot must omit Components")
	}
	if !strings.Contains(stdout, "not authority") {
		t.Fatalf("export should say the snapshot is not authority: %s", stdout)
	}
}
