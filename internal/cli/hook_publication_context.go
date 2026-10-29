package cli

import (
	"context"
	"errors"
	"os"
	"time"
)

const hookDeadlineEnvironment = "GSB_HOOK_OUTER_DEADLINE"

type hookPublicationContextKey struct{}

// Both branches retain cancellation from the same request. The publication
// branch owns only the original reserved tail, never a renewed timeout.
func hookRequestContexts(parent context.Context, outer time.Time) (context.Context, context.CancelFunc) {
	if deadline, ok := parent.Deadline(); ok && deadline.Before(outer) {
		outer = deadline
	}
	publication, cancelPublication := context.WithDeadline(parent, outer)
	work, cancelWork := context.WithDeadline(parent, outer.Add(-2*time.Second))
	work = context.WithValue(work, hookPublicationContextKey{}, publication)
	return work, func() { cancelWork(); cancelPublication() }
}

func nativeS9PublicationContext(ctx context.Context) context.Context {
	// An explicit cancellation of a derived request must not escape into its
	// still-live sibling publication context. Only the reserved work deadline
	// may fall through to the original bounded publication tail.
	if errors.Is(ctx.Err(), context.Canceled) {
		return ctx
	}
	if publication, ok := ctx.Value(hookPublicationContextKey{}).(context.Context); ok {
		return publication
	}
	return ctx
}

func hookOuterDeadline(fallback time.Time) time.Time {
	if raw := os.Getenv(hookDeadlineEnvironment); raw != "" {
		if deadline, err := time.Parse(time.RFC3339Nano, raw); err == nil && deadline.Before(fallback) {
			return deadline
		}
	}
	return fallback
}
