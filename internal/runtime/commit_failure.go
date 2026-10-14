package runtime

// CommitFailureStep identifies a durable boundary in the existing Runtime
// artifact/state/journal transaction. This is an explicit in-process testing
// seam; production commands never select it from environment or user input.
type CommitFailureStep string

const (
	CommitBeforePendingMarker  CommitFailureStep = "before_pending_marker"
	CommitAfterPendingMarker   CommitFailureStep = "after_pending_marker"
	CommitAfterArtifactPublish CommitFailureStep = "after_artifact_publish"
	CommitAfterStateWrite      CommitFailureStep = "after_state_write"
	CommitAfterJournalAppend   CommitFailureStep = "after_journal_append"
	CommitAfterMarkerClear     CommitFailureStep = "after_marker_clear"
)

type CommitFailureInjector interface{ Inject(CommitFailureStep) error }

// WithCommitFailureInjector returns a copy. An injector may return an error or
// pause at a boundary so a separate test process can issue an actual SIGKILL.
// No fallback, recovery bypass, or synthetic successful receipt is supplied.
func (s *Store) WithCommitFailureInjector(injector CommitFailureInjector) *Store {
	copy := *s
	copy.commitFailure = injector
	return &copy
}

func (s *Store) injectCommitFailure(step CommitFailureStep) error {
	if s.commitFailure != nil {
		return s.commitFailure.Inject(step)
	}
	return nil
}
