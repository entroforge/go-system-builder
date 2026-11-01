package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeS6ReceivedPreservesUnknownOccupants(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink", "parent-file"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			original := ".claude/assignments/native/a/r1/received-plan.json"
			occupied := filepath.Join(root, original)
			if kind == "parent-file" {
				occupied = filepath.Dir(occupied)
			}
			if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file", "parent-file":
				if err := os.WriteFile(occupied, []byte("same bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(occupied, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("unrelated", occupied); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(occupied)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := publishNativeS6Received(root, original, "plan", []byte("same bytes"))
			if err != nil {
				t.Fatal(err)
			}
			if actual == original {
				t.Fatal("claimed an unknown occupant")
			}
			if _, err := os.Stat(filepath.Join(root, actual)); err != nil {
				t.Fatal(err)
			}
			after, err := os.Lstat(occupied)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("occupied entry changed: %v", err)
			}
		})
	}
}
