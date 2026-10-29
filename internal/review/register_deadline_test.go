package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/metrics"
)

// Interrupt precisely when the first original Writer tries to derive its
// lock budget, after the author bytes and all preflight inputs were frozen.
type planDeadlineAtWriter struct {
	context.Context
	once     sync.Once
	done     chan struct{}
	hit      atomic.Bool
	err      error
	atWriter func()
}

func (c *planDeadlineAtWriter) Deadline() (time.Time, bool) {
	c.once.Do(func() {
		if c.atWriter != nil {
			c.atWriter()
		}
		c.hit.Store(true)
		close(c.done)
	})
	return time.Time{}, false
}
func (c *planDeadlineAtWriter) Done() <-chan struct{} { return c.done }
func (c *planDeadlineAtWriter) Err() error {
	if c.hit.Load() {
		return c.err
	}
	return nil
}

func TestRegisterPlanDeadlineTailUsesFrozenBytesAndTime(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	planPath := writePlanFile(t, root)
	original, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, timings := metrics.WithCallTimings(context.Background())
	interrupted := &planDeadlineAtWriter{Context: ctx, done: make(chan struct{}), err: context.DeadlineExceeded}
	changed := []byte("author file changed after the original read")
	interrupted.atWriter = func() {
		if err := os.WriteFile(planPath, changed, 0600); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 10, 25, 12, 0, 0, 123, time.UTC)
	accepted, err := RegisterPlan(root, statePath, journalPath, PlanRequest{Context: interrupted, ExpectedRevision: 1, PlanPath: planPath, OccurredAt: at})
	if err != nil {
		t.Fatalf("same frozen Plan could not continue: %v", err)
	}
	ptr := PlanPointerFromState(accepted.State)
	if accepted.Revision != 2 || ptr == nil || ptr.SubmittedAt != at.Format(time.RFC3339Nano) {
		t.Fatalf("original registration changed identity/time: %+v %+v", accepted, ptr)
	}
	pinned, err := os.ReadFile(filepath.Join(root, ptr.Path))
	if err != nil || !bytes.Equal(pinned, append(canonicalJSON(original), '\n')) {
		t.Fatalf("retry reread the author's changed file: %v", err)
	}
	authored, err := os.ReadFile(planPath)
	if err != nil || !bytes.Equal(authored, changed) {
		t.Fatalf("recovery rewrote the author file: %v", err)
	}
	if got := timings.Snapshot()["review/register_plan/deadline_tail"].Count; got != 1 {
		t.Fatalf("tail calls=%d", got)
	}
}

func TestRegisterPlanCanceledDoesNotStartDeadlineTail(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	planPath := writePlanFile(t, root)
	before, _ := os.ReadFile(statePath)
	ctx, timings := metrics.WithCallTimings(context.Background())
	interrupted := &planDeadlineAtWriter{Context: ctx, done: make(chan struct{}), err: context.Canceled}
	_, err := RegisterPlan(root, statePath, journalPath, PlanRequest{Context: interrupted, ExpectedRevision: 1, PlanPath: planPath})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error lost: %v", err)
	}
	after, readErr := os.ReadFile(statePath)
	if readErr != nil || !bytes.Equal(before, after) || timings.Snapshot()["review/register_plan/deadline_tail"].Count != 0 {
		t.Fatalf("explicit cancel started more registration work: %v", readErr)
	}
}

func TestRegisterPlanDeadlineTailDoesNotRenewContendedLockBudget(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	planPath := writePlanFile(t, root)
	release, err := filelock.Acquire(context.Background(), statePath+".lock.process")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, timings := metrics.WithCallTimings(context.Background())
	interrupted := &planDeadlineAtWriter{Context: ctx, done: make(chan struct{}), err: context.DeadlineExceeded}
	_, err = RegisterPlan(root, statePath, journalPath, PlanRequest{Context: interrupted, ExpectedRevision: 1, PlanPath: planPath})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("system interruption lost: %v", err)
	}
	if got := timings.Snapshot()["review/register_plan/deadline_tail"].Count; got != 1 {
		t.Fatalf("bounded tail calls=%d", got)
	}
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("contended recovery committed work: %v", err)
	}
}

func TestRegisterPlanDeadlineTailRecoversExactPendingWithoutNewRegistration(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	planPath := writePlanFile(t, root)
	before, _ := os.ReadFile(statePath)
	accepted, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: 1, PlanPath: planPath})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(statePath)
	journal, _ := os.ReadFile(journalPath)
	var event map[string]any
	if err := json.Unmarshal(journal, &event); err != nil {
		t.Fatal(err)
	}
	marker, err := json.Marshal(map[string]any{
		"schema_version": "1.0.0", "previous_state_sha256": sha256Of(before), "previous_revision": 1,
		"state_sha256": sha256Of(after), "journal_event_sha256": sha256Of(journal),
		"request_id": event["request_id"], "idempotency_key": event["idempotency_key"],
		"state": accepted.State, "journal_event": event,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, before, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	markerPath := statePath + ".commit-pending.json"
	ctx := &planDeadlineAtWriter{Context: context.Background(), done: make(chan struct{}), err: context.DeadlineExceeded}
	ctx.atWriter = func() {
		if err := os.WriteFile(markerPath, marker, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(planPath); err != nil {
			t.Fatal(err)
		}
	}
	recovered, err := RegisterPlan(root, statePath, journalPath, PlanRequest{Context: ctx, ExpectedRevision: 1, PlanPath: planPath})
	if err != nil {
		t.Fatalf("original pending registration not recovered: %v", err)
	}
	if recovered.Revision != accepted.Revision {
		t.Fatalf("recovery registered again: %d != %d", recovered.Revision, accepted.Revision)
	}
	got, err := os.ReadFile(journalPath)
	if err != nil || !bytes.Equal(got, journal) {
		t.Fatalf("recovery changed original journal: %v", err)
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("original pending not cleared: %v", err)
	}
}

func TestRegisterPlanDeadlineTailRejectsOtherPlanOrChangedCursor(t *testing.T) {
	for _, change := range []string{"bytes", "digest", "artifact", "runtime", "generation", "round", "revision", "no_commit", "preexisting"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			initial := baseVerificationState()
			statePath, journalPath := writeState(t, root, initial)
			planPath := writePlanFile(t, root)
			data, _ := os.ReadFile(planPath)
			accepted, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: 1, PlanPath: planPath})
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "bytes":
				data = bytes.Replace(data, []byte(`"review_plan_id": "`), []byte(`"review_plan_id": "other-`), 1)
			case "digest":
				data = bytes.Replace(data, []byte("REQ covered"), []byte("Different authored claim"), 1)
			case "artifact":
				ptr := PlanPointerFromState(accepted.State)
				if err := os.WriteFile(filepath.Join(root, ptr.Path), []byte("different pinned bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			case "runtime":
				initial["runtime_id"] = "other-runtime"
			case "generation":
				initial["baseline"].(map[string]any)["generation"] = 2
			case "round":
				initial["review"].(map[string]any)["round"] = 2
			case "revision":
				initial["revision"] = 0
			case "preexisting":
				initial = accepted.State
			}
			if change == "revision" { // No attempted original commit means no accepted replay.
				data = bytes.Replace(data, []byte(`"review_plan_id": "`), []byte(`"review_plan_id": "other-`), 1)
			}
			input := &planRegistrationInput{data: data, prepared: true, initial: initial, commitStarted: true}
			if change == "no_commit" {
				input.commitStarted = false
			}
			ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
			defer cancel()
			_, err = resumePlanRegistration(root, statePath, journalPath, PlanRequest{Context: ctx, ExpectedRevision: 1, PlanPath: planPath}, input)
			if err == nil {
				t.Fatal("recovery adopted another Plan/cursor")
			}
			before, _ := json.Marshal(accepted.State)
			var current map[string]any
			stored, _ := os.ReadFile(statePath)
			if err := json.Unmarshal(stored, &current); err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(current)
			if !bytes.Equal(before, after) {
				t.Fatal("rejected recovery changed current Plan")
			}
		})
	}
}
