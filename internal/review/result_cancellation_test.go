package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"strings"
	"testing"
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
