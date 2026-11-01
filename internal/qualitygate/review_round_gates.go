package qualitygate

import (
	"context"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/review"
)

// applyCleanRoundGate reads accepted conclusions and the current plan's
// required responsibility progress. It does not certify checks or coverage.
func applyCleanRoundGate(input Input, result *Evaluation, contexts ...context.Context) {
	state := input.Snapshot.State
	if review.PlanPointerFromState(state) != nil && review.RequiredResponsibilitiesComplete(state) && len(review.RoundFindings(state)) == 0 {
		return
	}
	result.Missing = append(result.Missing, "review:necessary_responsibilities_unfinished_or_finding")
	result.Status = StatusNotReady
}

// applyObservationBatchGate retains the legacy name while routing directly
// from accepted current-round Findings, including unfinished review rounds.
func applyObservationBatchGate(input Input, result *Evaluation) {
	if len(review.RoundFindings(input.Snapshot.State)) == 0 {
		result.Missing = append(result.Missing, "review:finding_missing")
		result.Status = StatusNotReady
	}
}

// reviewRoundGateIDs are the two S7 exit gates whose semantics are computed
// over the ReviewPlan projection rather than evidence presence alone.
func isReviewRoundGate(gateID string) bool {
	return gateID == "GATE-VERIFY-CLEAN-ROUND-PASSED" || gateID == "GATE-VERIFY-BLOCKING-FINDING"
}

// describeReviewRoundGate renders the gate's missing-token legend rows.
func describeReviewRoundGate(gateID string) []string {
	if gateID == "GATE-VERIFY-CLEAN-ROUND-PASSED" {
		return []string{fmt.Sprintf("review:necessary_responsibilities_unfinished_or_finding — inspect accepted original conclusions and responsibility progress")}
	}
	return []string{"review:finding_missing — no accepted current-round Finding is available for S8"}
}
