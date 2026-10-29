package fileview

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
)

func TestRequestReadCacheReusesOnlyPinnedGitObjects(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(path, content string) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-qb", "dev")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.test")
	write("docs/source.txt", "original")
	git("add", ".")
	git("commit", "-qm", "base")
	rules := []Rule{{Path: ".", Source: "git_tree"}, {Path: ".claude", Source: "disk"}}
	ctx, timings := metrics.WithCallTimings(WithReadCache(context.Background()))
	view, err := NewContext(ctx, root, "refs/heads/dev", rules)
	if err != nil {
		t.Fatal(err)
	}
	first, err := view.ReadFile("docs/source.txt")
	if err != nil {
		t.Fatal(err)
	}
	first[0] = 'X'
	gitReads := timings.Snapshot()["source/git"].Count
	again, err := view.ReadFile("docs/source.txt")
	if err != nil || string(again) != "original" || timings.Snapshot()["source/git"].Count != gitReads {
		t.Fatalf("blob reuse changed bytes or reran Git: %q %v", again, err)
	}
	dir, err := view.ReadDir("docs")
	if err != nil || len(dir) != 1 {
		t.Fatalf("directory: %v %v", dir, err)
	}
	gitReads = timings.Snapshot()["source/git"].Count
	if _, err := view.ReadDir("docs"); err != nil || timings.Snapshot()["source/git"].Count != gitReads {
		t.Fatalf("directory was not reused: %v", err)
	}
	write(".claude/evidence.json", "first")
	if _, err := view.ReadFile(".claude/evidence.json"); err != nil {
		t.Fatal(err)
	}
	write(".claude/evidence.json", "second")
	if data, err := view.ReadFile(".claude/evidence.json"); err != nil || string(data) != "second" {
		t.Fatalf("cached mutable disk evidence: %q %v", data, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := view.ReadFile("docs/source.txt")
			if err != nil || string(data) != "original" {
				t.Errorf("concurrent pinned read: %q %v", data, err)
				return
			}
			data[0] = 'Y'
		}()
	}
	wg.Wait()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := view.WithContext(canceled).ReadFile("docs/source.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cache concealed request cancellation: %v", err)
	}
	if err := view.WithContext(canceled).Verify(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify concealed cancellation: %v", err)
	}
	write("docs/source.txt", "updated")
	git("add", "docs/source.txt")
	git("commit", "-qm", "advance")
	if err := view.Verify(); err == nil {
		t.Fatal("Verify cached a mutable branch identity")
	}
	newView, err := NewContext(ctx, root, "refs/heads/dev", rules)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := newView.ReadFile("docs/source.txt"); err != nil || string(data) != "updated" {
		t.Fatalf("new pinned commit used old bytes: %q %v", data, err)
	}
	// Reusing a source view under a new request must not share the old cache.
	newCtx, newTimings := metrics.WithCallTimings(WithReadCache(context.Background()))
	if _, err := view.WithContext(newCtx).ReadFile("docs/source.txt"); err != nil || newTimings.Snapshot()["source/git"].Count != 2 {
		t.Fatalf("read cache crossed requests: %v %+v", err, newTimings.Snapshot())
	}
}
