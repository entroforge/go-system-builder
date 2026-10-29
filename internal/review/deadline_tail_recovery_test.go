package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

type planReturnsAfterCutoff struct {
	*planDeadlineAtWriter
	original time.Time
}

func (c *planReturnsAfterCutoff) Deadline() (time.Time, bool) {
	c.planDeadlineAtWriter.Deadline()
	return c.original, true
}

func TestRegisterPlanLateSynchronousReturnCannotStartFreshTail(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	planPath := writePlanFile(t, root)
	before := mustReadTestFile(t, statePath)
	ctx := &planReturnsAfterCutoff{
		planDeadlineAtWriter: &planDeadlineAtWriter{Context: context.Background(), done: make(chan struct{}), err: context.DeadlineExceeded},
		original:             time.Now().Add(-time.Second),
	}
	_, err := RegisterPlan(root, statePath, journalPath, PlanRequest{Context: ctx, ExpectedRevision: 1, PlanPath: planPath})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("late registration: %v", err)
	}
	if !bytes.Equal(before, mustReadTestFile(t, statePath)) {
		t.Fatal("late synchronous return committed registration using renewed tail")
	}
	if journal, err := os.ReadFile(journalPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	} else if len(journal) > 0 {
		t.Fatal("late synchronous return appended a new event")
	}
}

func TestResultAnchoredTailRecoversOriginalPendingReceiptOnlyBeforeCutoff(t *testing.T) {
	for _, mode := range []string{"within-tail", "after-cutoff", "explicit-cancel"} {
		t.Run(mode, func(t *testing.T) {
			root, sp, jp, snapshot, plan := packetModeFixture(t)
			input := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-anchored-pending", "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
			frozen := mustReadTestFile(t, input)
			before := mustReadTestFile(t, sp)
			previousJournal := mustReadTestFile(t, jp)
			accepted, err := SubmitResult(root, sp, jp, SubmitRequest{ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input})
			if err != nil {
				t.Fatal(err)
			}
			after := mustReadTestFile(t, sp)
			journal := mustReadTestFile(t, jp)
			lines := bytes.Split(bytes.TrimSuffix(journal, []byte{'\n'}), []byte{'\n'})
			eventBytes := append(append([]byte(nil), lines[len(lines)-1]...), '\n')
			var event map[string]any
			if err := json.Unmarshal(eventBytes, &event); err != nil {
				t.Fatal(err)
			}
			// Pending fingerprints describe the physical Runtime serializers,
			// independently of the author-file representation.
			var previousState map[string]any
			if err := json.Unmarshal(before, &previousState); err != nil {
				t.Fatal(err)
			}
			previousCanonical, _ := json.MarshalIndent(previousState, "", "  ")
			previousCanonical = append(previousCanonical, '\n')
			marker, err := json.Marshal(map[string]any{
				"schema_version": "1.0.0", "previous_state_sha256": sha256Of(previousCanonical), "previous_revision": snapshot.Revision,
				"state_sha256": sha256Of(after), "journal_event_sha256": sha256Of(eventBytes),
				"request_id": event["request_id"], "idempotency_key": event["idempotency_key"], "state": accepted.State, "journal_event": event,
			})
			if err != nil {
				t.Fatal(err)
			}
			markerPath := sp + ".commit-pending.json"
			for path, data := range map[string][]byte{sp: before, jp: previousJournal, markerPath: marker} {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Remove(input); err != nil {
				t.Fatal(err)
			}
			var ctx context.Context
			var cancel context.CancelFunc
			switch mode {
			case "within-tail":
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
			case "after-cutoff":
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-3*time.Second))
			default:
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
			}
			defer cancel()
			recovered, err := SubmitResult(root, sp, jp, SubmitRequest{Context: ctx, ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input, resultData: frozen})
			if mode == "within-tail" {
				if err != nil || recovered.Revision != accepted.Revision {
					t.Fatalf("original pending receipt recovery: r%d %v", recovered.Revision, err)
				}
				if !bytes.Equal(journal, mustReadTestFile(t, jp)) {
					t.Fatal("tail created a second reception event")
				}
				if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
					t.Fatalf("original pending not cleared: %v", err)
				}
			} else {
				want := context.DeadlineExceeded
				if mode == "explicit-cancel" {
					want = context.Canceled
				}
				if !errors.Is(err, want) {
					t.Fatalf("interruption: %v", err)
				}
				if !bytes.Equal(before, mustReadTestFile(t, sp)) || !bytes.Equal(previousJournal, mustReadTestFile(t, jp)) || !bytes.Equal(marker, mustReadTestFile(t, markerPath)) {
					t.Fatal("exhausted/canceled tail changed original pending pair")
				}
			}
			if _, err := os.Stat(input); !os.IsNotExist(err) {
				t.Fatalf("tail recreated author input: %v", err)
			}
		})
	}
}
