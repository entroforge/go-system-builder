package review

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

// A parent deadline observation signals entry to the budgeted Writer lock.
// Both callers finish staging before that lock opens, without polling sleeps
// or a production failure-injection API.
type registrationLockContext struct {
	context.Context
	arrived chan struct{}
	once    sync.Once
}

func TestRegisterPlanResumesExactPublishedArtifactBeforeCAS(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	planPath := writePlanFile(t, root)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: 1, PlanPath: planPath})
	if err != nil {
		t.Fatal(err)
	}
	ptr := PlanPointerFromState(accepted.State)
	// Retain the real immutable output while restoring the exact prior pair:
	// this models interruption after artifact publication and before CAS.
	if err := os.WriteFile(statePath, before, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	resumed, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: 1, PlanPath: planPath})
	if err != nil {
		t.Fatalf("original published Plan required a new ID/manual deletion to resume: %v", err)
	}
	if resumed.Revision != accepted.Revision {
		t.Fatal("resumption added another registration")
	}
	if _, err := os.Stat(filepath.Join(root, ptr.Path)); err != nil {
		t.Fatal(err)
	}
}

func (c *registrationLockContext) Deadline() (time.Time, bool) {
	c.once.Do(func() { close(c.arrived) })
	return c.Context.Deadline()
}

func TestRegisterPlanExclusivePublicationPreservesConcurrentWinner(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	planPath := writePlanFile(t, root)
	parent, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := filelock.Acquire(parent, statePath+".lock.process")
	if err != nil {
		t.Fatal(err)
	}
	var unlockOnce sync.Once
	unlock := func() { unlockOnce.Do(release) }
	defer unlock()
	results := make(chan error, 2)
	earlyRejected := 0
	for i := 0; i < 2; i++ {
		ctx := &registrationLockContext{Context: parent, arrived: make(chan struct{})}
		go func() {
			_, err := RegisterPlan(root, statePath, journalPath, PlanRequest{Context: ctx, ExpectedRevision: 1, PlanPath: planPath})
			results <- err
		}()
		select {
		case <-ctx.arrived:
		case err := <-results:
			if err == nil {
				t.Fatal("registration unexpectedly completed under held Writer lock")
			}
			earlyRejected++
		case <-parent.Done():
			t.Fatal(parent.Err())
		}
	}
	unlock()
	accepted := 0
	for i := 0; i < 2-earlyRejected; i++ {
		if err := <-results; err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d concurrent plans", accepted)
	}
	snapshot, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadPlan(root, snapshot.State); err != nil {
		t.Fatalf("CAS loser removed the winning accepted Plan: %v", err)
	}
}
