package review

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyVerifiedEmptyS7SurfaceStillSupported(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"kind":"completion_report","changed_paths":[]}`)
	if err := os.WriteFile(filepath.Join(root, "completion.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	paths, err := completionChangedPaths(root, map[string]any{"path": "completion.json"})
	if err != nil || len(paths) != 0 {
		t.Fatalf("legacy empty surface changed: paths=%v err=%v", paths, err)
	}
}
