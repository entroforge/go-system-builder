package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeContextAssetPreservesUnknownCandidates(t *testing.T) {
	for _, kind := range []string{"plan", "result", "schema", "context"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			storage := ".claude/evidence"
			if kind == "schema" || kind == "context" {
				storage = ".claude/assignment-context"
			}
			original := storage + "/original/" + kind + ".json"
			same := []byte("{\"unregistered\":true}\n")
			write := func(path string, data []byte) {
				t.Helper()
				full := filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(original, same)
			first := fmt.Sprintf("%s/native-%s-candidate-1/%s.json", storage, kind, kind)
			write(first, same)
			// A symlink candidate must not redirect publication, even inside root.
			target := filepath.Join(root, "unknown-target")
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			secondDir := fmt.Sprintf("%s/native-%s-candidate-2", storage, kind)
			if err := os.Symlink(target, filepath.Join(root, secondDir)); err != nil {
				t.Fatal(err)
			}
			actual, err := publishNativeContextAsset(root, original, storage, kind, same)
			want := fmt.Sprintf("%s/native-%s-candidate-3/%s.json", storage, kind, kind)
			if err != nil || actual != want {
				t.Fatalf("actual=%s want=%s err=%v", actual, want, err)
			}
			for _, path := range []string{original, first, actual} {
				if data, err := os.ReadFile(filepath.Join(root, path)); err != nil || !bytes.Equal(data, same) {
					t.Fatalf("asset %s changed: %q %v", path, data, err)
				}
			}
			if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
				t.Fatalf("wrote through symlink: %v %v", entries, err)
			}
			if link, err := os.Readlink(filepath.Join(root, secondDir)); err != nil || link != target {
				t.Fatalf("symlink changed: %s %v", link, err)
			}
		})
	}
}

func TestNativeContextAssetKeepsControlRootFailure(t *testing.T) {
	for _, kind := range []string{"schema", "context"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".claude"), 0700); err != nil {
				t.Fatal(err)
			}
			occupied := filepath.Join(root, ".claude/assignment-context")
			if err := os.WriteFile(occupied, []byte("unknown control root"), 0600); err != nil {
				t.Fatal(err)
			}
			if actual, err := publishNativeContextAsset(root, ".claude/assignment-context/native/"+kind+".json", ".claude/assignment-context", kind, []byte("{}")); err == nil || actual != "" {
				t.Fatalf("control root silently changed: %s %v", actual, err)
			}
			if data, err := os.ReadFile(occupied); err != nil || string(data) != "unknown control root" {
				t.Fatalf("control root changed: %q %v", data, err)
			}
			entries, err := os.ReadDir(filepath.Join(root, ".claude"))
			if err != nil || len(entries) != 1 {
				t.Fatalf("unexpected alternate control root: %v %v", entries, err)
			}
		})
	}
}
