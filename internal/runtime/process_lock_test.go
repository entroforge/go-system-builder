package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgedLiveRuntimeLockCannotBeReclaimed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	release, err := acquireLock(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	old := time.Now().Add(-time.Hour)
	if err = os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	second, err := acquireLock(path, 75*time.Millisecond)
	if err == nil {
		second()
		t.Fatal("evicted live Runtime writer based on sentinel age")
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("contender removed live sentinel")
	}
	release()
	third, err := acquireLock(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	third()
}
