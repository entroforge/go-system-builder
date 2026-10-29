package runtime_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestObservationSnapshotBoundedReadOnlyAndPending(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journal := filepath.Join(root, "loop-events.jsonl")
	writeState(t, statePath, 7)
	os.WriteFile(journal, nil, 0600)
	store := runtime.NewStore(statePath, journal)
	before, _ := os.ReadFile(statePath)
	snap, err := store.ObservationSnapshot()
	if err != nil || snap.Revision != 7 {
		t.Fatal(snap, err)
	}
	release, err := filelock.Acquire(context.Background(), statePath+".lock.process")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = store.ObservationSnapshot()
	elapsed := time.Since(started)
	release()
	if err == nil || elapsed > time.Second {
		t.Fatal("observation waited unboundedly", elapsed, err)
	}
	marker := statePath + ".commit-pending.json"
	os.WriteFile(marker, []byte("pending marker preserved"), 0600)
	if _, err := store.ObservationSnapshot(); err == nil {
		t.Fatal("observer repaired or ignored pending pair")
	}
	after, _ := os.ReadFile(statePath)
	saved, _ := os.ReadFile(marker)
	if !bytes.Equal(before, after) || string(saved) != "pending marker preserved" {
		t.Fatal("observer changed source or pending transaction")
	}
	if _, err := runtime.NewWriter(statePath, journal, root, nil).ObservationSnapshot(); err == nil {
		t.Fatal("Writer used observation capability")
	}
}

func TestObservationSnapshotSourceBoundAndLinks(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	journal := filepath.Join(root, "journal")
	os.WriteFile(statePath, make([]byte, (4<<20)+1), 0600)
	if _, err := runtime.NewStore(statePath, journal).ObservationSnapshot(); err == nil {
		t.Fatal("oversized Runtime observed")
	}
	os.Remove(statePath)
	target := filepath.Join(root, "actual.json")
	writeState(t, target, 0)
	if err := os.Symlink(target, statePath); err != nil {
		t.Skip("host does not support this link fixture", err)
	}
	if _, err := runtime.NewStore(statePath, journal).ObservationSnapshot(); err == nil {
		t.Fatal("linked Runtime observed")
	}
}
