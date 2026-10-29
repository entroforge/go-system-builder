// Package factory creates isolated factory assets for tests. It intentionally
// excludes the developer checkout's ignored Runtime and other execution state.
package factory

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func Clone(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"docs", "agents", "skills", "internal/cli/testdata", "tests/fixtures/hook", "AGENTS-template.md", "README.md", "settings.json", "go.mod"} {
		base := filepath.Join(source, name)
		if _, err := os.Stat(base); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			target := filepath.Join(root, rel)
			if entry.IsDir() {
				return os.MkdirAll(target, 0755)
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fs.ErrInvalid
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
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
	return root
}
