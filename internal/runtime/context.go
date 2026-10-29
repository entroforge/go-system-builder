package runtime

import (
	"context"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
)

// WithContext copies the Store; it never changes another caller's budget or
// grants write capability to an observation Store.
func (s *Store) WithContext(ctx context.Context) *Store {
	copy := *s
	copy.ctx = ctx
	return &copy
}

func (s *Store) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *Store) acquireLock(path string, budget time.Duration) (func(), error) {
	endWait := metrics.Span(s.Context(), "runtime/lock_wait")
	release, err := acquireLockContext(s.Context(), path, budget)
	endWait()
	if err != nil {
		return nil, err
	}
	endHold := metrics.Span(s.Context(), "runtime/lock_hold")
	return func() { release(); endHold() }, nil
}
