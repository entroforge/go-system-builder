package audit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
)

// TestAcquireLockTimeoutSurfacesAsError covers acquireLock's deadline
// path: when the lock is held by another process and never released,
// acquireLock must return a non-nil error after the timeout elapses
// (not block forever). This pins the round-4 B1 fix at the
// package-private level so a future contributor cannot regress the
// timeout cap without breaking this test.
func TestAcquireLockTimeoutSurfacesAsError(t *testing.T) {
	dir := t.TempDir()
	lockPath := dir + "/test.lock"

	releaseHolder, err := filelock.Acquire(context.Background(), lockPath+".process")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseHolder)

	// Use a 200ms timeout so the test is fast but still exercises
	// the deadline branch. The production 30s timeout is exercised
	// in TestOutboxAcquireLockRetriesOnContention with a held-then-
	// released lock; this test exercises the inverse (never
	// released).
	release, err := acquireLock(lockPath, 200*time.Millisecond)
	if err == nil {
		release()
		t.Fatal("acquireLock must return an error when the lock is held past the timeout")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("error must mention timeout, got: %v", err)
	}
}

// TestAcquireLockRejectsNonExistError covers acquireLock's non-ErrExist
// error path (e.g. permission denied). When the lockfile path cannot be
// created for a reason other than ErrExist, acquireLock must surface
// that error immediately rather than retry.
func TestAcquireLockRejectsNonExistError(t *testing.T) {
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(parentFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(parentFile, ".lock")
	release, err := acquireLock(lockPath, 200*time.Millisecond)
	if err == nil {
		release()
		t.Fatal("acquireLock must return an error when the lock parent is not a directory")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquireLock must surface non-contention errors immediately, got: %v", err)
	}
}
