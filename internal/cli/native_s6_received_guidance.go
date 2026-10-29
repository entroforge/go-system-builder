package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Project only the original recorded professional answer. The original receipt
// and integrator remain responsible for evidence closure and Main's checks.
func nativeS6ReceivedGuidance(root string, state map[string]any, r assignmentrecord.Record, ref string) string {
	receipt := fmt.Sprintf("loop-harness runtime task-receipt --root %s --assignment-id %s --agent-id %s", s7CaptureShellQuote(root), s7CaptureShellQuote(r.AssignmentID), s7CaptureShellQuote(r.OwnerAgentID))
	result, err := assignmentresult.LoadRecordedAccepted(state, r, ref, func(path string) ([]byte, error) {
		data, err := workpacket.ReadFile(root, path, assignmentresult.EvidenceReadLimit)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filepath.Join(root, path), err)
		}
		return data, nil
	})
	if err != nil {
		return fmt.Sprintf("S6 %s: recorded Result source unavailable or invalid at %s: %v. Main preserves the received delivery and restores this original source; inspect `%s` for the original receipt diagnostic before deciding recovery. No completion or integration eligibility can be inferred from this failed projection.", r.Task.ID, filepath.Join(root, ref), err, receipt)
	}
	answer := result.Answer
	lines := []string{fmt.Sprintf("S6 %s: professional Result received in the original Runtime record at %s.", r.Task.ID, filepath.Join(root, ref))}
	unfinished := len(answer.RisksBlockers) != 0 || len(answer.ScopeDeviations) != 0
	if answer.ClosingAssessment != nil {
		lines = append(lines, "Builder's recorded disposition: "+answer.ClosingAssessment.Disposition+".")
		unfinished = unfinished || answer.ClosingAssessment.Disposition != "complete"
		for _, condition := range answer.ClosingAssessment.Conditions {
			if condition.Judgment != "satisfied" {
				unfinished = true
				lines = append(lines, fmt.Sprintf("Unmet Done when[%d] (%s): %s", condition.DoneWhenIndex, condition.Judgment, condition.Rationale))
			}
		}
	} else {
		lines = append(lines, "This historical Result contract has no recorded closing disposition; consult the original receipt for its completion requirements.")
	}
	for _, reason := range answer.RisksBlockers {
		lines = append(lines, "Recorded risk/blocker: "+reason)
	}
	for _, deviation := range answer.ScopeDeviations {
		lines = append(lines, "Recorded scope deviation: "+deviation)
	}
	if unfinished {
		lines = append(lines, fmt.Sprintf("Main resolves these actual unfinished obligations and preserves the received source/worktree. Source or report corrections require a new Assignment revision; do not edit the accepted delivery in place or resubmit the identical received Result. If no original blocked/preserved rework checkpoint exists, Main first runs `loop-harness runtime task-integrate --root %s --assignment-id %s --agent-id %s` to record the actual closing blocker and preserved rework checkpoint; the original chain rejects this unclosed delivery before merge. This records the blocker, not integration eligibility. With that original rework checkpoint, Main runs `loop-harness s6 prepare --root %s --task %s --role %s --revise-assignment %s --revision-intent-key '<stable-key>'`. Replace <stable-key> with Main's stable token for this correction intent and retain it on retries; the original revision Writer returns the new exact responsibility/brief. Unreceived reception failures instead recover the still-current original responsibility.", s7CaptureShellQuote(root), s7CaptureShellQuote(r.AssignmentID), s7CaptureShellQuote(r.OwnerAgentID), s7CaptureShellQuote(root), s7CaptureShellQuote(r.Task.ID), s7CaptureShellQuote(r.RoleFamily), s7CaptureShellQuote(r.AssignmentID)))
	} else {
		lines = append(lines, fmt.Sprintf("Main continues the original integration/checks through `loop-harness runtime task-integrate --root %s --assignment-id %s --agent-id %s`; follow its original checkpoint feedback to recover unfinished integration/checks. For a preserved checkpoint after an actual Main check failure, append `--retry-preserved` to that same command when retrying the unfinished checks. Do not ask the Builder to resubmit the identical received Result.", s7CaptureShellQuote(root), s7CaptureShellQuote(r.AssignmentID), s7CaptureShellQuote(r.OwnerAgentID)))
	}
	lines = append(lines, "This records the Builder's own conclusion; Main checks and the full-batch Gate remain authoritative. Original receipt: `"+receipt+"`.")
	return strings.Join(lines, " ")
}
