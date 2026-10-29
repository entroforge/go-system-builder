package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

type cancelAtReviewCaptureGate struct {
	context.Context
	cancel       context.CancelFunc
	interruption error
	triggered    bool
}

func (c *cancelAtReviewCaptureGate) Err() error {
	if !c.triggered {
		callers := make([]uintptr, 64)
		count := goruntime.Callers(2, callers)
		frames := goruntime.CallersFrames(callers[:count])
		for {
			frame, more := frames.Next()
			if strings.HasSuffix(frame.Function, "/review.validateCapturePassProvenance") {
				c.triggered = true
				c.cancel()
				break
			}
			if !more {
				break
			}
		}
	}
	if c.triggered {
		return c.interruption
	}
	return c.Context.Err()
}

func TestCaptureProvenanceGatePreservesInterruptionOutcome(t *testing.T) {
	for _, interruption := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(interruption.Error(), func(t *testing.T) {
			root, sp, jp, before, plan := packetModeFixture(t)
			input := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-capture-cancel", "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &cancelAtReviewCaptureGate{Context: parent, cancel: cancel, interruption: interruption}
			_, err := SubmitResult(root, sp, jp, SubmitRequest{Context: ctx, ExpectedRevision: before.Revision, AssignmentID: "assignment-qa-1", ResultPath: input})
			if !ctx.triggered {
				t.Fatal("test did not reach capture provenance gate")
			}
			if !errors.Is(err, interruption) {
				t.Fatalf("capture cancellation became content rejection: %v", err)
			}
			after, err := loopruntime.NewStore(sp, jp).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if after.Revision != before.Revision {
				t.Fatal("capture interruption mutated Runtime")
			}
			counts, err := metrics.NewStore(root).Read()
			if err != nil {
				t.Fatal(err)
			}
			if counts.S7ResultSubmits["interrupted"] != 1 || counts.S7ResultSubmits["rejected"] != 0 {
				t.Fatalf("capture interruption counted as invalid answer: %+v", counts.S7ResultSubmits)
			}
		})
	}
}

func TestBudgetFailureNeverMarksOriginalReviewPlanStale(t *testing.T) {
	for _, interruption := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(interruption.Error(), func(t *testing.T) {
			root, sp, jp, before, _ := packetModeFixture(t)
			original, err := os.ReadFile(sp)
			if err != nil {
				t.Fatal(err)
			}
			_, err = staleReviewPlanAfterDrift(context.Background(), root, sp, jp, before.State, fmt.Errorf("baseline interrupted: %w", interruption))
			if !errors.Is(err, interruption) {
				t.Fatalf("interruption chain lost: %v", err)
			}
			current, err := os.ReadFile(sp)
			if err != nil || string(current) != string(original) {
				t.Fatalf("timeout manufactured stale-plan business fact: %v", err)
			}
		})
	}
}
