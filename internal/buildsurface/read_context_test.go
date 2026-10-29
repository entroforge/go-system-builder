package buildsurface

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

func TestPinnedReadsReuseObjectsButKeepCheckoutLive(t *testing.T) {
	root := t.TempDir()
	gitFixture(t, root, "init", "-b", "main")
	gitFixture(t, root, "config", "user.name", "Fixture")
	gitFixture(t, root, "config", "user.email", "fixture@example.invalid")
	writeGitFixture(t, root, "product.go", "before\n")
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "commit", "-m", "start")
	start, _ := gitText(root, "rev-parse", "HEAD")
	writeGitFixture(t, root, "product.go", "after\n")
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "commit", "-m", "final")
	final, _ := gitText(root, "rev-parse", "HEAD")
	ctx := WithReadCache(context.Background())
	first, err := changedPathsContext(ctx, root, start, final)
	if err != nil {
		t.Fatal(err)
	}
	cache := ctx.Value(readCacheKey{}).(*immutableReads)
	if len(cache.data) == 0 {
		t.Fatal("fixed product reconstruction did not populate request cache")
	}
	first[0].SHA256 = "caller mutation"
	second, err := changedPathsContext(ctx, root, start, final)
	if err != nil || second[0].SHA256 != hashBytes([]byte("after\n")) {
		t.Fatalf("caller mutated reconstructed data: %+v %v", second, err)
	}
	if err := verifyCheckoutContext(ctx, root, final); err != nil {
		t.Fatal(err)
	}
	writeGitFixture(t, root, "product.go", "dirty\n")
	if err := verifyCheckoutContext(ctx, root, final); err == nil {
		t.Fatal("fixed-object cache hid working tree drift")
	}
	// The fixed range remains the original Git content while disk is dirty.
	third, err := changedPathsContext(ctx, root, start, final)
	if err != nil || !reflect.DeepEqual(second, third) {
		t.Fatalf("fixed range changed with checkout: %+v %v", third, err)
	}
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "commit", "-m", "new head")
	if err := verifyCheckoutContext(ctx, root, final); err == nil {
		t.Fatal("fixed-object cache hid current HEAD drift")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := changedPathsContext(canceled, root, start, final); !errors.Is(err, context.Canceled) {
		t.Fatalf("warm cache hid cancellation: %v", err)
	}
	if _, err := ReadContext(canceled, root, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadContext lost cancellation: %v", err)
	}
	if err := VerifyContext(canceled, root, nil, Snapshot{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifyContext lost cancellation: %v", err)
	}
}

func TestProductGitStopsWithParentCancellation(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(bin, "started")
	script := "#!/bin/sh\nprintf ready > \"$GSB_BASELINE_GIT_MARKER\"\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GSB_BASELINE_GIT_MARKER", marker)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := gitBytesContext(ctx, bin, 4096, "status"); done <- err }()
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("Git exited before cancellation: %v", err)
		case <-deadline:
			t.Fatal("Git did not start")
		case <-ticker.C:
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Git lost parent cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Git received a detached local budget")
	}
}

func TestPinnedGitCacheOwnsBytesAndSeparatesRepositoryAndLimit(t *testing.T) {
	bin := t.TempDir()
	counter := filepath.Join(bin, "calls")
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nprintf x >> \"$GSB_BASELINE_GIT_CALLS\"\nprintf object\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GSB_BASELINE_GIT_CALLS", counter)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx := WithReadCache(context.Background())
	root, other := t.TempDir(), t.TempDir()
	args := []string{"cat-file", "blob", strings.Repeat("a", 40)}
	first, err := fixedGitBytes(ctx, root, 4096, args...)
	if err != nil {
		t.Fatal(err)
	}
	first[0] = 'X'
	again, err := fixedGitBytes(ctx, root, 4096, args...)
	if err != nil || string(again) != "object" {
		t.Fatalf("cache bytes escaped: %q %v", again, err)
	}
	if _, err := fixedGitBytes(ctx, other, 4096, args...); err != nil {
		t.Fatal(err)
	}
	if _, err := fixedGitBytes(ctx, root, 2, args...); err == nil {
		t.Fatal("larger cached output bypassed read limit")
	}
	calls, err := os.ReadFile(counter)
	if err != nil || string(calls) != "xxx" {
		t.Fatalf("fixed cache identity/reuse: %q %v", calls, err)
	}
}

func TestLockedTaskReadPreservesChildCancellationWithLiveCaller(t *testing.T) {
	view, err := fileview.New(t.TempDir(), "", []fileview.Rule{{Path: ".", Source: "disk"}})
	if err != nil {
		t.Fatal(err)
	}
	child, cancel := context.WithCancel(context.Background())
	cancel()
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "documents": []any{map[string]any{"kind": "task", "generation": 1, "status": "locked", "id": "TASK-001", "path": "task.md", "version": "v1", "sha256": strings.Repeat("a", 64)}}}
	if _, err := currentTasks(context.Background(), state, view.WithContext(child)); !errors.Is(err, context.Canceled) {
		t.Fatalf("child read cancellation became changed-source diagnosis: %v", err)
	}
}
