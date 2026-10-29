package workstate

// AgentStarted is the single compatibility reader for an Agent's execution
// progress. It cannot prove completion, a blocker, cancellation, or permission.
// New callers pass canonical state; old snapshots may contain activity names.
func AgentStarted(state string) bool {
	switch state {
	case string(Active), string(Blocked), string(Done), string(Cancelled),
		"reading", "understanding_submitted", "understanding_approved",
		"activated", "working", "reported", "stopped":
		return true
	default:
		return false
	}
}

// LegacyCompletionReportStatus recognizes historical report vocabulary only.
// A report still needs its original identity, source and consumer checks.
func LegacyCompletionReportStatus(status string) bool {
	switch status {
	case "complete", "completed", string(Done):
		return true
	default:
		return false
	}
}
