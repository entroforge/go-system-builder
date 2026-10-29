package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLegacyLockNeverEvictsLiveOwnerByAge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	owner := []byte(fmt.Sprintf("%d:%d", os.Getpid(), time.Now().UnixNano()))
	if err := os.WriteFile(path, owner, 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if release, err := acquireLockContext(ctx, path, time.Second); err == nil {
		release()
		t.Fatal("evicted live legacy owner by age")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(owner) {
		t.Fatalf("live owner changed: %q %v", got, err)
	}
}
