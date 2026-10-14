package runtime

import (
	"context"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
)

// WithContext returns a copy sharing the caller's remaining deadline. It does
// not change the original Store and is safe for independent concurrent calls.
// Without it, existing non-Hook callers retain the five-second lock cap.
func (s *Store) WithContext(ctx context.Context) *Store {
	copy := *s
	copy.ctx = ctx
	return &copy
}

func (s *Store) context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *Store) lock() (func(), error) {
	stopWait := metrics.StartPhase(s.context(), "runtime_lock_wait")
	release, err := acquireLockContext(s.context(), s.statePath+".lock", 5*time.Second)
	stopWait()
	if err != nil {
		return nil, err
	}
	stopHold := metrics.StartPhase(s.context(), "runtime_lock_hold")
	return func() { release(); stopHold() }, nil
}

func (s *Store) inspectJournal() (journalInspection, error) {
	defer metrics.StartPhase(s.context(), "runtime_journal_scan")()
	return inspectJournalContext(s.context(), s.journalPath)
}
