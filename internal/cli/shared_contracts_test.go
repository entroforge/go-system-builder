package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestSharedContractsJSONFailureExit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs/dev/contracts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs/dev/contracts/CONTRACTS-001.md"), []byte("> Shared model policy: json-schema-v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := cli.Run([]string{"contracts", "check", "--root", root, "--json"}, strings.NewReader(""), &out, &stderr)
	if code != 1 || !strings.Contains(out.String(), "requires a Shared model baseline table") {
		t.Fatalf("code=%d out=%s err=%s", code, out.String(), stderr.String())
	}
}
