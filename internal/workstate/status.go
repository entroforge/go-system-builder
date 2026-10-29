// Package workstate defines the single vocabulary for a responsibility
// attempt. Domain consumers provide facts; status grants no permissions.
package workstate

type Status string

const (
	Pending   Status = "pending"
	Active    Status = "active"
	Blocked   Status = "blocked"
	Done      Status = "done"
	Cancelled Status = "cancelled"
)

// Facts describes the current bound responsibility, never a document name or
// a historical receipt alone. Completion must come from its original consumer.
type Facts struct {
	Started                            bool
	ExplicitBlocker                    bool
	OriginalConsumerAcceptedCompletion bool
	OriginalCancellationOrReplacement  bool
}

func Project(f Facts) Status {
	switch {
	case f.OriginalCancellationOrReplacement:
		return Cancelled
	case f.ExplicitBlocker:
		return Blocked
	case f.OriginalConsumerAcceptedCompletion:
		return Done
	case f.Started:
		return Active
	default:
		return Pending
	}
}

func Valid(s Status) bool {
	switch s {
	case Pending, Active, Blocked, Done, Cancelled:
		return true
	default:
		return false
	}
}
