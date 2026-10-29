package review

import (
	"context"
	"errors"
	"time"
)

type reviewWorkDeadlineKey struct{}
type reviewWorkDeadline struct {
	deadline time.Time
	bounded  bool
}

func originalReviewWorkDeadline(ctx context.Context) (time.Time, bool) {
	if original, ok := ctx.Value(reviewWorkDeadlineKey{}).(reviewWorkDeadline); ok {
		return original.deadline, original.bounded
	}
	return ctx.Deadline()
}

// reviewDeadlineTail detaches cancellation only for the caller's existing
// cleanup/recovery contract. The cutoff remains anchored to the original work
// deadline, including when a synchronous operation returns long after it.
// Carry the original deadline through derived tails so nested callers cannot
// turn a previous tail's deadline into a newly extended work deadline.
func reviewDeadlineTail(ctx context.Context, localCap, allowance time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	original, bounded := originalReviewWorkDeadline(ctx)
	base := context.WithValue(context.WithoutCancel(ctx), reviewWorkDeadlineKey{}, reviewWorkDeadline{deadline: original, bounded: bounded})
	deadline := time.Now().Add(localCap)
	if bounded {
		cutoff := original.Add(allowance)
		if cutoff.Before(deadline) {
			deadline = cutoff
		}
	}
	return context.WithDeadline(base, deadline)
}

// ResultCompletionContext allows only an already accepted Result's original
// receipt/handoff to use the remainder of its fixed completion allowance.
// Explicit cancellation and unbounded compatibility callers never detach.
func ResultCompletionContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, bounded := originalReviewWorkDeadline(ctx); !bounded || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return context.WithCancel(ctx)
	}
	allowance := resultCleanupBudget + resultRecoveryBudget
	return reviewDeadlineTail(ctx, allowance, allowance)
}
