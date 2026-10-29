package cli

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHookCompletionSharesOriginalDeadlineAndRetainsExplicitCancellation(t *testing.T) {
	deadline := time.Now().Add(-time.Millisecond)
	work, cancelWork := context.WithDeadline(context.Background(), deadline)
	defer cancelWork()
	<-work.Done()
	tail, cancelTail := hookCompletionContext(work)
	defer cancelTail()
	if got, ok := tail.Deadline(); !ok || !got.Equal(deadline.Add(1400*time.Millisecond)) {
		t.Fatalf("completion obtained a fresh budget: %v %v", got, ok)
	}
	if tail.Err() != nil {
		t.Fatalf("recovered receipt inherited an already-expired work lifetime: %v", tail.Err())
	}
	old, cancelOld := context.WithDeadline(context.Background(), time.Now().Add(-2*time.Second))
	defer cancelOld()
	<-old.Done()
	exhausted, cancelExhausted := hookCompletionContext(old)
	defer cancelExhausted()
	<-exhausted.Done()
	if !errors.Is(exhausted.Err(), context.DeadlineExceeded) {
		t.Fatal("exhausted completion budget was renewed")
	}
	explicit, cancelExplicit := context.WithTimeout(context.Background(), time.Minute)
	cancelExplicit()
	stopped, cancelStopped := hookCompletionContext(explicit)
	defer cancelStopped()
	if !errors.Is(stopped.Err(), context.Canceled) {
		t.Fatal("explicit cancellation was detached")
	}
}
