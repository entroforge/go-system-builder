package runtime_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestRuntimeLockNeverEvictsKnownLiveSentinelOwnerByAge(t *testing.T) {
	root := t.TempDir()
	sp, jp := filepath.Join(root, "loop-state.json"), filepath.Join(root, "loop-events.jsonl")
	writeState(t, sp, 1)
	owner := []byte(fmt.Sprintf("%d:%d", os.Getpid(), time.Now().Add(-time.Minute).UnixNano()))
	if err := os.WriteFile(sp+".lock", owner, 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(sp+".lock", old, old); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := runtime.NewStore(sp, jp).WithContext(ctx).Snapshot(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("known live sentinel did not retain exclusion: %v", err)
	}
	if !bytes.Equal(owner, mustRead(t, sp+".lock")) {
		t.Fatal("known live owner was evicted by age")
	}
	if _, err := os.Stat(sp + ".lock.process"); err != nil {
		t.Fatal("persistent OS-lock inode disappeared")
	}
}
