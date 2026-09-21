package assignment_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// assignmentTestRoot creates the smallest project-shaped root needed by the
// assignment integration tests. Assignment operations write evidence,
// workgroup manifests, and review plans relative to this root, so the tests
// must never use the checkout itself as their runtime project.
func assignmentTestRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	source := filepath.Join("..", "..", "docs", "control")
	destination := filepath.Join(root, "docs", "control")
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatalf("copy docs/control into isolated assignment test root: %v", err)
	}
	return root
}
