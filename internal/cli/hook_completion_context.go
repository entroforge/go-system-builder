package cli

import (
	"context"

	"github.com/entroforge/go-system-builder/internal/review"
)

// A recovered receipt must not pass an already-expired work context into its
// original successor. The completion deadline is anchored to the original
// work deadline, so reception recovery and handoff share the same finite tail.
// Explicit cancellation is never detached. This is internal only; platform
// process limits remain the outer bound.
func hookCompletionContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return review.ResultCompletionContext(ctx)
}
