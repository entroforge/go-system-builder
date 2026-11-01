package metrics

import "strings"

// RecordS7ResultSubmit increments loop_s7_result_submits_total{outcome};
// outcome is accepted, rejected, blocked or interrupted. Transport deadlines
// and cancellation must not count as professional-content rejection (§14.2).
func RecordS7ResultSubmit(root, outcome string) error {
	return NewStore(root).mutate(func(snap *Snapshot) {
		snap.S7ResultSubmits[normalizeLabel(outcome, "unknown")]++
	})
}

// RecordS7SubmitPhase records one phase-duration sample for the review-result
// submit CAS transaction under loop_s7_submit_phase_ms{phase}. phase is a
// fixed vocabulary (result_consumption | findings | advance | seal | clean |
// pause); unknown phases are still recorded under their own label — the
// vocabulary is caller-owned, and the map is bounded by the phases submit.go
// actually emits. An empty root is a no-op (metrics are best-effort).
func RecordS7SubmitPhase(root string, phase string, durationMS int64) error {
	return RecordS7SubmitPhases(root, map[string]int64{phase: durationMS})
}

// RecordS7SubmitPhases persists the completed phases from one attempt in one
// metrics update. Callers collect samples in memory while holding the Runtime
// Writer lock and publish them only after that lock is released. These samples
// describe completed work, including work in a transaction that later failed;
// they never establish reception or affect the business commit.
func RecordS7SubmitPhases(root string, phases map[string]int64) error {
	if strings.TrimSpace(root) == "" || len(phases) == 0 {
		return nil
	}
	return NewStore(root).mutate(func(snap *Snapshot) {
		for phase, durationMS := range phases {
			if durationMS < 0 {
				durationMS = 0
			}
			label := normalizeLabel(phase, "unknown")
			stats := snap.S7SubmitPhases[label]
			stats.Count++
			stats.SumMS += durationMS
			snap.S7SubmitPhases[label] = stats
		}
	})
}
