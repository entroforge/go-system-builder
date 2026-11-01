package repair

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestS9EvidenceWriterPreservesOccupiedPathsAndPublishesConcurrentCandidates(t *testing.T) {
	root := t.TempDir()
	directory := ".claude/evidence/s9/p/packet/blockers"
	occupied := filepath.Join(root, filepath.FromSlash(directory), "blocker.json")
	if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"same":"content"}`)
	if err := os.WriteFile(occupied, original, 0600); err != nil {
		t.Fatal(err)
	}
	const workers = 8
	paths := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := publishUniqueS9Evidence(root, directory, "blocker.json", original, 1<<20)
			if err != nil {
				errs <- err
				return
			}
			paths <- p
		}()
	}
	wg.Wait()
	close(paths)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	seen := map[string]bool{}
	for p := range paths {
		if seen[p] || filepath.Join(root, filepath.FromSlash(p)) == occupied {
			t.Errorf("unknown or duplicate candidate: %s", p)
		}
		seen[p] = true
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil || string(data) != string(original) {
			t.Errorf("candidate %s: %q %v", p, data, err)
		}
	}
	if len(seen) != workers {
		t.Fatal(fmt.Sprintf("got %d/%d unique candidates", len(seen), workers))
	}
	if data, err := os.ReadFile(occupied); err != nil || string(data) != string(original) {
		t.Fatalf("unknown file changed: %q %v", data, err)
	}
}

func TestS9EvidenceWriterAvoidsOccupiedParentFileOrSymlink(t *testing.T) {
	for _, kind := range []string{"file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			parent := filepath.Join(root, ".claude", "evidence")
			if err := os.MkdirAll(parent, 0700); err != nil {
				t.Fatal(err)
			}
			occupied := filepath.Join(parent, "s9")
			if kind == "file" {
				if err := os.WriteFile(occupied, []byte("unknown"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink(outside, occupied); err != nil {
					t.Fatal(err)
				}
			}
			path, err := publishUniqueS9Evidence(root, ".claude/evidence/s9/p/packet/blockers", "blocker.json", []byte(`{"original":true}`), 1<<20)
			if err != nil || !strings.HasPrefix(path, ".claude/evidence-alt-") {
				t.Fatalf("alternate evidence: %s %v", path, err)
			}
			if kind == "file" {
				if data, err := os.ReadFile(occupied); err != nil || string(data) != "unknown" {
					t.Fatal("unknown parent changed")
				}
			} else if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
				t.Fatal("symlink target changed")
			}
		})
	}
}

func TestS9EvidenceWriterAvoidsOccupiedEntireEvidenceRoot(t *testing.T) {
	for _, kind := range []string{"file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
				t.Fatal(err)
			}
			occupied := filepath.Join(root, ".claude", "evidence")
			if kind == "file" {
				if err := os.WriteFile(occupied, []byte("unknown root"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(outside, occupied); err != nil {
				t.Fatal(err)
			}
			actual, err := publishUniqueS9Evidence(root, ".claude/evidence/s9/p/packet/blockers", "blocker.json", []byte(`{"original":true}`), 1<<20)
			if err != nil || !strings.HasPrefix(actual, ".claude/evidence-alt-") {
				t.Fatalf("original Writer alternate: %s %v", actual, err)
			}
			if kind == "file" {
				if got, err := os.ReadFile(occupied); err != nil || string(got) != "unknown root" {
					t.Fatal("unknown root changed")
				}
			} else if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
				t.Fatal("symlink target changed")
			}
		})
	}
}
