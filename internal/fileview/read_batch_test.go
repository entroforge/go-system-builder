package fileview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
)

func pinnedReadFixture(t testing.TB, size int) *View {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-qb", "dev")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.test")
	for i := range size {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("source-%02d", i)), []byte(fmt.Sprintf("bytes-%02d", i)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "-qm", "fixture")
	view, err := NewContext(WithReadCache(context.Background()), root, "refs/heads/dev", []Rule{{Path: ".", Source: "git_tree"}, {Path: "evidence", Source: "disk"}})
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func TestConcurrentColdPinnedReadStartsOnlyOneGitCommandPerObject(t *testing.T) {
	view := pinnedReadFixture(t, 1)
	ctx, timings := metrics.WithCallTimings(WithReadCache(context.Background()))
	view = view.WithContext(ctx)
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			data, err := view.ReadFile("source-00")
			if err != nil || string(data) != "bytes-00" {
				t.Errorf("read: %q %v", data, err)
			}
			if len(data) > 0 {
				data[0] = 'X'
			}
		}()
	}
	close(start)
	workers.Wait()
	if count := timings.Snapshot()["source/git"].Count; count != 2 {
		t.Fatalf("cold concurrent source started %d Git processes, want 2", count)
	}
	for range 2 {
		if _, err := immutableGit(ctx, view.Root, "cat-file", "blob", "missing-object"); err == nil {
			t.Fatal("missing object accepted")
		}
	}
	if count := timings.Snapshot()["source/git"].Count; count != 4 {
		t.Fatalf("failed object permanently cached: %d", count)
	}
}

func TestImmutableGitWaiterCancellationAndSiblingRetry(t *testing.T) {
	view := pinnedReadFixture(t, 1)
	ctx := WithReadCache(context.Background())
	cache := ctx.Value(readCacheKey{}).(*immutableReads)
	key := view.Root + "\x00cat-file\x00blob\x00missing-object"
	pending := &immutableRead{done: make(chan struct{})}
	cache.inflight[key] = pending
	canceled, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { _, err := immutableGit(canceled, view.Root, "cat-file", "blob", "missing-object"); done <- err }()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting read ignored cancellation: %v", err)
	}
	// A failed leading child is not shared as a durable failure with siblings.
	cache.mu.Lock()
	pending.err = context.Canceled
	delete(cache.inflight, key)
	close(pending.done)
	cache.mu.Unlock()
	if _, err := immutableGit(ctx, view.Root, "cat-file", "blob", "missing-object"); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("sibling inherited canceled leader: %v", err)
	}
}

func TestReadFilesReportsInputOrderAndKeepsDiskReadsSequential(t *testing.T) {
	view := pinnedReadFixture(t, 8)
	paths := []string{"source-07", "source-02", "missing-first", "missing-second"}
	var seen []string
	err := ReadFiles(view, paths, func(i int, data []byte, err error) error {
		seen = append(seen, paths[i])
		if err != nil {
			return err
		}
		return nil
	})
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || pathErr.Path != "missing-first" || !reflect.DeepEqual(seen, paths[:3]) {
		t.Fatalf("batch changed source error order: %v %v", seen, err)
	}
	if err := os.MkdirAll(filepath.Join(view.Root, "evidence"), 0700); err != nil {
		t.Fatal(err)
	}
	diskPath := filepath.Join(view.Root, "evidence", "answer")
	if err := os.WriteFile(diskPath, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	err = ReadFiles(view, []string{"evidence/answer", "evidence/answer"}, func(i int, data []byte, err error) error {
		if err != nil {
			return err
		}
		want := []string{"first", "second"}[i]
		if string(data) != want {
			t.Fatalf("disk read moved before preceding consumer: %q want %q", data, want)
		}
		return os.WriteFile(diskPath, []byte("second"), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(view.context())
	cancel()
	if err := ReadFiles(view.WithContext(canceled), paths, func(_ int, _ []byte, err error) error { return err }); !errors.Is(err, context.Canceled) {
		t.Fatalf("batch hid cancellation: %v", err)
	}
}

func BenchmarkPinnedGitSourceBatch(b *testing.B) {
	view := pinnedReadFixture(b, 16)
	paths := make([]string, 16)
	for i := range paths {
		paths[i] = fmt.Sprintf("source-%02d", i)
	}
	for _, concurrent := range []bool{false, true} {
		b.Run(fmt.Sprintf("files=16/parallel=%t", concurrent), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				request := view.WithContext(WithReadCache(context.Background()))
				if concurrent {
					if err := ReadFiles(request, paths, func(_ int, _ []byte, err error) error { return err }); err != nil {
						b.Fatal(err)
					}
				} else {
					for _, path := range paths {
						if _, err := request.ReadFile(path); err != nil {
							b.Fatal(err)
						}
					}
				}
			}
		})
	}
}
