package review

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReviewDeadlineTailCannotRenewAfterDelayedReturn(t *testing.T) {
	original := time.Now().Add(-3 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), original)
	defer cancel()
	for _, budget := range []struct {
		name           string
		cap, allowance time.Duration
	}{
		{"result cleanup", resultCleanupBudget, resultCleanupBudget + resultRecoveryBudget},
		{"result recovery", resultRecoveryBudget, resultCleanupBudget + resultRecoveryBudget},
		{"plan recovery", 400 * time.Millisecond, 400 * time.Millisecond},
	} {
		t.Run(budget.name, func(t *testing.T) {
			tail, stop := reviewDeadlineTail(ctx, budget.cap, budget.allowance)
			defer stop()
			deadline, _ := tail.Deadline()
			if !deadline.Equal(original.Add(budget.allowance)) || !errors.Is(tail.Err(), context.DeadlineExceeded) {
				t.Fatalf("late return obtained new tail: %s %v", deadline, tail.Err())
			}
			// A derived context must remember the work deadline rather than
			// treating the first tail's endpoint as a new starting point.
			again, stopAgain := reviewDeadlineTail(tail, budget.cap, budget.allowance)
			defer stopAgain()
			againDeadline, _ := again.Deadline()
			if !againDeadline.Equal(deadline) || !errors.Is(again.Err(), context.DeadlineExceeded) {
				t.Fatalf("nested tail renewed budget: %s %v", againDeadline, again.Err())
			}
		})
	}
}

func TestResultCleanupAndRecoveryShareOriginalCutoffInEitherOrder(t *testing.T) {
	original := time.Now().Add(-1200 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), original)
	defer cancel()
	cutoff := original.Add(resultCleanupBudget + resultRecoveryBudget)
	for _, caps := range [][]time.Duration{{resultCleanupBudget, resultRecoveryBudget}, {resultRecoveryBudget, resultCleanupBudget}} {
		parent := ctx
		var stops []context.CancelFunc
		for _, cap := range caps {
			tail, stop := reviewDeadlineTail(parent, cap, resultCleanupBudget+resultRecoveryBudget)
			stops = append(stops, stop)
			deadline, _ := tail.Deadline()
			if !deadline.Equal(cutoff) {
				t.Fatalf("shared result cutoff moved: %s != %s", deadline, cutoff)
			}
			parent = tail
		}
		for _, stop := range stops {
			stop()
		}
	}
}

func TestReviewDeadlineTailPreservesSafeCleanupAndUnboundedCompatibility(t *testing.T) {
	type identityKey struct{}
	parent := context.WithValue(context.Background(), identityKey{}, "original-request")
	ctx, cancel := context.WithCancel(parent)
	cancel()
	before := time.Now()
	cleanup, stop := reviewDeadlineTail(ctx, resultCleanupBudget, resultCleanupBudget+resultRecoveryBudget)
	defer stop()
	after := time.Now()
	deadline, _ := cleanup.Deadline()
	if cleanup.Err() != nil || cleanup.Value(identityKey{}) != "original-request" || deadline.Before(before.Add(resultCleanupBudget)) || deadline.After(after.Add(resultCleanupBudget)) {
		t.Fatalf("safe cleanup changed: %s %v", deadline, cleanup.Err())
	}
	stop()
	if !errors.Is(cleanup.Err(), context.Canceled) {
		t.Fatalf("tail cancel lost: %v", cleanup.Err())
	}
}

func TestResultCompletionUsesOriginalCutoffAndPreservesCancellation(t *testing.T) {
	original := time.Now().Add(-1200 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), original)
	defer cancel()
	completion, stop := ResultCompletionContext(ctx)
	defer stop()
	deadline, _ := completion.Deadline()
	if !deadline.Equal(original.Add(resultCleanupBudget + resultRecoveryBudget)) {
		t.Fatalf("receipt projection renewed cutoff: %s", deadline)
	}
	// A prior internal tail may have reached a smaller local cap. Its saved
	// original work deadline remains the completion anchor.
	derived := context.WithValue(ctx, reviewWorkDeadlineKey{}, reviewWorkDeadline{deadline: original.Add(-time.Second), bounded: true})
	exhausted, stopExhausted := ResultCompletionContext(derived)
	defer stopExhausted()
	if !errors.Is(exhausted.Err(), context.DeadlineExceeded) {
		t.Fatalf("saved work cutoff lost: %v", exhausted.Err())
	}
	live, cancelLive := context.WithTimeout(context.Background(), time.Second)
	child, stopChild := ResultCompletionContext(live)
	defer stopChild()
	cancelLive()
	if !errors.Is(child.Err(), context.Canceled) {
		t.Fatalf("normal completion detached explicit cancel: %v", child.Err())
	}
	canceled, stopCanceled := ResultCompletionContext(live)
	defer stopCanceled()
	if !errors.Is(canceled.Err(), context.Canceled) {
		t.Fatalf("canceled completion detached: %v", canceled.Err())
	}
	localOnly, stopLocal := reviewDeadlineTail(context.Background(), -time.Second, resultCleanupBudget+resultRecoveryBudget)
	defer stopLocal()
	localCompletion, stopLocalCompletion := ResultCompletionContext(localOnly)
	defer stopLocalCompletion()
	localDeadline, _ := localOnly.Deadline()
	completionDeadline, _ := localCompletion.Deadline()
	if !completionDeadline.Equal(localDeadline) || !errors.Is(localCompletion.Err(), context.DeadlineExceeded) {
		t.Fatal("unbounded request gained another budget from a local tail deadline")
	}
	unbounded, stopUnbounded := ResultCompletionContext(context.Background())
	defer stopUnbounded()
	if _, bounded := unbounded.Deadline(); bounded {
		t.Fatal("unbounded compatibility received new completion budget")
	}
}
