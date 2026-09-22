package semantic_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// Tests own their Runtime instead of depending on another package's writes to
// the source checkout. Copy template assets only, never local .claude state.
func isolatedSemanticProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, entry := range []string{"docs", "skills", "agents", "settings.json", "AGENTS-template.md", "loop-template.md", "loop-harness.md"} {
		source := filepath.Join("..", "..", entry)
		err := filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(filepath.Join("..", ".."), path)
			if err != nil {
				return err
			}
			target := filepath.Join(root, rel)
			if d.IsDir() {
				return os.MkdirAll(target, 0755)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0644)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if code := cli.Run([]string{"init", "--root", root}, nil, &output, &output); code != 0 {
		t.Fatalf("initialize isolated project: exit=%d: %s", code, output.String())
	}
	return root
}
