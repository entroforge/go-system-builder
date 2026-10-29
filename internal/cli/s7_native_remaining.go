package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

const s7RemainingOutletLimit = 16 << 10

// Project only original current-round facts. In particular, this never opens
// an unreceived professional answer or treats registration as platform liveness.
// Bounded output refers Main back to the original status entry for overflow.
func nativeS7RemainingOutletContext(root string, state map[string]any) string {
	facts := mapFieldCLI(state, "review")
	ptr := mapFieldCLI(facts, "plan")
	status := stringValue(ptr["status"])
	if !assignment.ReviewRoundOpenForDispatch(state) {
		phase := stringValue(mapFieldCLI(state, "lifecycle")["phase"])
		if facts["observation_batch"] != nil || phase == "observation_sealed" {
			status = "observation_sealed"
		}
		if state["pause"] != nil || lifecycleState(state) == "paused" || lifecycleState(state) == "awaiting_human_release" {
			status = "paused"
		}
		switch status {
		case "paused", "observation_sealed", "stale", "planned", "":
		default:
			return " S7 dispatch is closed at the current cursor; Main follows the original stage handoff or recorded pause and restores current round metadata before dispatch."
		}
		return " S7 remaining responsibilities follow the original current handoff: " + s7RecoveryNextAction(root, state, status, tolerantInt(facts["round"]), nil, nil, nil, nil, nil) + "."
	}
	var b strings.Builder
	fmt.Fprintf(&b, " S7 current round=%d status=%s remaining responsibilities (Runtime registration does not prove a native Reviewer is running; Main checks actual native dispatch/return):\n", tolerantInt(facts["round"]), status)
	footer := "Main observes the original current queue/receipts with `" + s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " s7 status --root " + s7CaptureShellQuote(root) + " --explain`. The accepted Reviewer's personal receipt remains done with no round-wide WaitingOn."
	appendBounded := func(s string) bool {
		if b.Len()+len(s)+len(footer)+200 > s7RemainingOutletLimit {
			return false
		}
		b.WriteString(s)
		return true
	}
	plan, _, planErr := review.LoadPlan(root, state)
	if planErr != nil {
		appendBounded(fmt.Sprintf("Main/system restores the original ReviewPlan source before dispatch: %v.\n", planErr))
	}
	// These coordinates are a handoff projection, not receipt or body
	// verification. The index retains shared identity checks without opening
	// unrelated bodies; the original consumer verifies a selected packet.
	packets, packetErr := runtime.ProjectCurrentReviewResultPacketIndex(state, "")
	byID := map[string]runtime.ReviewResultPacketIndex{}
	for _, p := range packets {
		byID[p.Binding.AssignmentID] = p
	}
	if packetErr != nil {
		appendBounded(fmt.Sprintf("Main/system restores original current issued packet metadata: %v.\n", packetErr))
	}
	rows := mapFieldCLI(facts, "assignments")
	var ids []string
	for id, raw := range rows {
		row, _ := raw.(map[string]any)
		if workstate.ReviewAssignmentActivity(row) != "consumed" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for i, id := range ids {
		if i >= 12 {
			fmt.Fprintf(&b, "%d further remaining Assignment(s); inspect original status below.\n", len(ids)-i)
			break
		}
		row, _ := rows[id].(map[string]any)
		activity := workstate.ReviewAssignmentActivity(row)
		owner := stringValue(row["agent_id"])
		if owner == "" {
			owner = stringValue(row["queued_agent_id"])
		}
		ownerLabel := owner
		if ownerLabel == "" {
			ownerLabel = "unregistered (Main selects actual Reviewer)"
		}
		line := fmt.Sprintf("S7 assignment=%s owner=%s registration=%s", id, ownerLabel, activity)
		if p, ok := byID[id]; ok {
			source, err := questionnaireSourceRoot(root, state, id, p.Binding.OwnerAgentID, p.Binding.RuntimeID, p.Binding.BaselineGeneration)
			if err == nil {
				line += fmt.Sprintf("; exact view=%s answer=%s", filepath.Join(source, filepath.FromSlash(p.ViewPath)), filepath.Join(source, filepath.FromSlash(p.AnswerPath)))
			} else {
				line += fmt.Sprintf("; Main/system restores original owner source: %v", err)
			}
		} else if owner != "" && activity != "planned" {
			line += "; Main/system restores the original registered source, then prepares missing exact view/answer through `" + s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " runtime stage-answer issue --root " + s7CaptureShellQuote(root) + " --agent Main`"
		}
		line += ".\n"
		switch activity {
		case "planned":
			if stringValue(row["queued_agent_id"]) == "" {
				line += "Original manifest preparation entry (dispatch still requires the prerequisites below):\n" + s7ManifestPreparationHint(root, state, id)
			}
			if queued := stringValue(row["queued_agent_id"]); queued != "" {
				line += fmt.Sprintf("Original queued Reviewer=%s queue_reason=%v.\n", queued, row["queue_reason"])
			}
			if planErr == nil {
				locks, err := assignment.ReviewDispatchPrerequisite(state, plan, id)
				switch {
				case err != nil:
					line += fmt.Sprintf("Main preserves original coverage/queue; prerequisite: %v.\n", err)
				case locks != "":
					line += fmt.Sprintf("Main preserves original queue; waits for resource locks %s.\n", locks)
				case stringValue(row["queued_agent_id"]) != "":
					line += "Main restores this original registered Reviewer's queued dispatch after prerequisites settle, preserving its original workgroup.\n"
				default:
					line += "Original dispatch prerequisites are settled; Main follows the manifest registration guidance above.\n"
				}
			}
		case "blocked":
			line += s7BlockedAssignmentGuidance(root, state, id)
		case "dispatched", "result_submitted":
			line += "Main checks this owner's actual native Agent/return and original receipt; carry its exact issued references when restoring this same responsibility. The Reviewer saves its ready original answer for PreToolUse reception, with successful PostToolUse and Stop/SubagentStop fallback; report results to Main when finished.\n"
		default:
			line += "Main/system restores the original Assignment lifecycle metadata before selecting any continuation.\n"
		}
		if !appendBounded(line) {
			fmt.Fprintf(&b, "%d remaining Assignment(s) omitted by output bound; inspect original status below.\n", len(ids)-i)
			break
		}
	}
	if len(ids) == 0 {
		b.WriteString("No remaining Runtime Assignments; Main follows original round routing/receipt diagnostics.\n")
	}
	b.WriteString(footer)
	return b.String()
}
