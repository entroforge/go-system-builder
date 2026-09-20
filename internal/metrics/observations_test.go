package metrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestObservationDoesNotWaitForSummaryLockOrDoubleCount(t *testing.T) {
	root := t.TempDir()
	// A held legacy summary lock cannot block Hook telemetry.
	os.MkdirAll(filepath.Join(root, ".claude/loop-metrics.json.lock"), 0700)
	start := time.Now()
	if err := ObserveCASConflict(root); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("observation waited for summary lock")
	}
	os.RemoveAll(filepath.Join(root, ".claude/loop-metrics.json.lock"))
	if err := RecordCASConflict(root); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		snap, err := NewStore(root).Read()
		if err != nil {
			t.Fatal(err)
		}
		if snap.CASConflicts != 2 {
			t.Fatalf("count=%d", snap.CASConflicts)
		}
	}
}
