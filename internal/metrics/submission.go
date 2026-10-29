package metrics

import (
	"fmt"
	"sort"
	"strings"
)

// ObserveSubmissionSave records a recognized S5 save invocation, including
// replay. It cannot count unique receptions, first submissions or gate PASS.
// One publication carries the counter and duration, with no summary lock.
func ObserveSubmissionSave(root, outcome string, elapsedMS int64) error {
	if !validSubmissionOutcome(outcome) {
		return fmt.Errorf("metrics: unsupported S5 save outcome")
	}
	if elapsedMS < 0 {
		elapsedMS = 0
	}
	return observe(root, "submission_save", outcome, elapsedMS)
}

func validSubmissionOutcome(outcome string) bool {
	switch outcome {
	case "interrupted", "read_error", "incomplete", "complete_rejected", "received_pass", "received_fix_required", "received_req_change_required":
		return true
	default:
		return false
	}
}

func writeSubmissionSaves(b *strings.Builder, snap Snapshot) {
	if len(snap.SubmissionSaves) == 0 {
		return
	}
	b.WriteString("  S5 save invocations (includes replay; first-submit rate unavailable)\n")
	labels := make([]string, 0, len(snap.SubmissionSaves))
	for label := range snap.SubmissionSaves {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		fmt.Fprintf(b, "  %s{stage=\"S5\",role=\"document-verifier\",outcome=%q} %d\n", metricSubmissionSave, label, snap.SubmissionSaves[label])
		stats := snap.SubmissionSaveDuration[label]
		fmt.Fprintf(b, "  %s{stage=\"S5\",role=\"document-verifier\",outcome=%q} count=%d sum_ms=%d\n", metricSubmissionSaveDuration, label, stats.Count, stats.SumMS)
	}
}
