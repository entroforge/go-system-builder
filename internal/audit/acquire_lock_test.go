package audit

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
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

	// Pre-create the lockfile to simulate a holder that never
	// releases. The acquireLock polling loop will see ErrExist on
	// every iteration until the timeout elapses.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = f.Close()
		_ = os.Remove(lockPath)
	})

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
	// Make the parent directory read-only so OpenFile(O_CREATE) fails
	// with EACCES, not ErrExist.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	lockPath := dir + "/.lock"
	release, err := acquireLock(lockPath, 200*time.Millisecond)
	if err == nil {
		release()
		t.Fatal("acquireLock must return an error when OpenFile fails with EACCES")
	}
	if errors.Is(err, os.ErrExist) {
		t.Fatalf("acquireLock must not retry on non-ErrExist errors, got: %v", err)
	}
}
