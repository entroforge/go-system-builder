package policy

import (
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func workPacketAnswerAllowed(input Input, path string) bool {
	if !input.Runtime.PacketMode || input.EffectiveAgentID() == "" {
		return false
	}
	rel := reviewerRelativePath(input, path)
	for _, allowed := range input.Runtime.WorkPacketAnswerPaths {
		if rel == allowed && workpacket.CheckPath(mutationRoot(input), rel) == nil {
			return true
		}
	}
	return false
}

// An unavailable platform identity is not evidence of another owner. Leave
// ordinary bug-resolution control files to their existing receivers instead
// of treating them as product writes or guessing a role from a teammate name.
func unidentifiedBugResolutionControlPath(input Input, path string) bool {
	if input.Runtime.CurrentState != "bug_resolution" || (input.AgentID != "" && input.Runtime.Agent != nil) {
		return false
	}
	rel := reviewerRelativePath(input, path)
	if rel != ".claude" && !strings.HasPrefix(rel, ".claude/") && rel != ".loop-submissions" && !strings.HasPrefix(rel, ".loop-submissions/") {
		return false
	}
	root := mutationRoot(input)
	return root == "" || reviewerPathContained(root, rel)
}

func workerSubmissionPathAllowed(input Input, path string) bool {
	if input.EffectiveAgentID() == "" || input.Runtime.Workspace == nil || input.Runtime.Agent == nil ||
		input.Runtime.Agent.ID != input.EffectiveAgentID() || input.Runtime.Agent.AssignmentID == "" {
		return false
	}
	e, ok := workspaceExecution(input)
	if !ok || e.AgentID != input.Runtime.Agent.ID || e.AssignmentID != input.Runtime.Agent.AssignmentID {
		return false
	}
	root := mutationRoot(input)
	if root == "" {
		return false
	}
	rel := reviewerRelativePath(input, path)
	for _, allowed := range input.Runtime.WorkerSubmissionPaths {
		if rel == allowed && workpacket.CheckPath(root, rel) == nil {
			return true
		}
	}
	return false
}

// workerPlanDraftAllowed narrows the first-write exception to the exact PLAN
// draft derived from this Worker's current frozen execution. Other lifecycle
// drafts stay behind their original consumers, and the issued answer still
// requires its separate current-packet authorization after activation.
func workerPlanDraftAllowed(input Input, path string) bool {
	if !workerSubmissionPathAllowed(input, path) || input.Runtime.Workspace == nil {
		return false
	}
	e, ok := workspaceExecution(input)
	if !ok {
		return false
	}
	paths, err := input.Runtime.Workspace.WorkerSubmissionPaths(e)
	if err != nil || paths.Plan == "" {
		return false
	}
	return reviewerRelativePath(input, path) == paths.Plan
}

// mainPlanningAnswerAllowed accepts only the exact current Main S2/S3/S4
// packet path projected by hookctx from Runtime. It deliberately rejects all
// platform agent identities so a Worker can never inherit cursor ownership.
func mainPlanningAnswerAllowed(input Input, path string) bool {
	if !input.Runtime.PacketMode || (input.EffectiveAgentID() != "" && input.EffectiveAgentID() != "Main") || input.Runtime.Agent != nil ||
		input.Runtime.CurrentState != "planning" || input.Runtime.RuntimeID == "" || input.Runtime.CurrentBaselineGeneration < 1 {
		return false
	}
	if input.EffectiveAgentID() == "Main" && input.Runtime.CurrentPhase != "design" {
		return false
	}
	expectedStage := ""
	switch input.Runtime.CurrentPhase {
	case "design":
		expectedStage = "S2"
	case "contracts":
		expectedStage = "S3"
	case "tasks":
		expectedStage = "S4"
	default:
		return false
	}
	if input.Runtime.CurrentStage != expectedStage {
		return false
	}
	root := mutationRoot(input)
	if root == "" {
		return false
	}
	rel := reviewerRelativePath(input, path)
	for _, allowed := range input.Runtime.MainPlanningAnswerPaths {
		if rel == allowed && workpacket.CheckPath(root, rel) == nil {
			return true
		}
	}
	return false
}

// reviewResultAnswerAllowed accepts only an exact S7 answer path copied from
// a current persisted ReviewResultPacket by hookctx. The packet is bound to
// the Runtime owner; policy additionally checks that this is the same loaded
// ReviewAssignment and that the filesystem target remains a prospective
// regular file beneath the execution root.
func reviewResultAnswerAllowed(input Input, path string) bool {
	if !input.Runtime.PacketMode || input.EffectiveAgentID() == "" || input.Runtime.Agent == nil ||
		input.Runtime.Agent.ID != input.EffectiveAgentID() || !input.Runtime.Agent.ReviewAssignment ||
		input.Runtime.Agent.AssignmentID == "" {
		return false
	}
	root := mutationRoot(input)
	if root == "" {
		return false
	}
	rel := reviewerRelativePath(input, path)
	for _, allowed := range input.Runtime.ReviewResultAnswerPaths {
		if rel == allowed && workpacket.CheckPath(root, rel) == nil {
			return true
		}
	}
	return false
}

// hypothesisResultAnswerAllowed accepts one exact issued S8 answer path. The
// hookctx projection already proved this packet against the current Case and
// original dispatch; policy rechecks the trusted owner/Assignment tuple and
// prospective regular-file path before allowing the write.
func hypothesisResultAnswerAllowed(input Input, path string) bool {
	// S8 save permission requires the explicit platform agent_id field. A
	// teammate name or another fallback identity is not a submitter identity.
	if !input.Runtime.PacketMode || input.AgentID == "" || input.Runtime.Agent == nil ||
		input.Runtime.Agent.ID != input.AgentID || !input.Runtime.Agent.HypothesisResultAssignment ||
		input.Runtime.Agent.AssignmentID == "" {
		return false
	}
	root := mutationRoot(input)
	if root == "" {
		return false
	}
	rel := reviewerRelativePath(input, path)
	for _, allowed := range input.Runtime.HypothesisResultAnswerPaths {
		if rel == allowed && workpacket.CheckPath(root, rel) == nil {
			return true
		}
	}
	return false
}

// repairPlanReportAnswerAllowed accepts only the exact current S9 PlanReport
// authoring file projected by hookctx. The grant is bound to the platform's
// explicit agent_id and the registered RepairAssignment; a teammate name or
// generic packet path cannot select another Builder's answer.
func repairPlanReportAnswerAllowed(input Input, path string) bool {
	if !input.Runtime.PacketMode || input.AgentID == "" || input.Runtime.Agent == nil ||
		input.Runtime.Agent.ID != input.AgentID || input.Runtime.Agent.RepairAssignmentID == "" ||
		input.Runtime.CurrentState != "bug_resolution" ||
		(input.Runtime.CurrentPhase != "planning" && input.Runtime.CurrentPhase != "reproducing") {
		return false
	}
	root := mutationRoot(input)
	if root == "" {
		return false
	}
	rel := reviewerRelativePath(input, path)
	for _, allowed := range input.Runtime.RepairPlanReportAnswerPaths {
		if rel == allowed && workpacket.CheckPath(root, rel) == nil {
			return true
		}
	}
	return false
}

// repairResultAnswerAllowed permits one exact S9 post-execution answer from
// the registered Worker's current typed packet. It does not grant product
// scope or let Main impersonate the Worker.
func repairResultAnswerAllowed(input Input, path string) bool {
	if !input.Runtime.PacketMode || input.AgentID == "" || input.Runtime.Agent == nil ||
		input.Runtime.Agent.ID != input.AgentID || !input.Runtime.Agent.RepairResultAssignment ||
		input.Runtime.Agent.RepairAssignmentID == "" || input.Runtime.CurrentState != "bug_resolution" ||
		input.Runtime.CurrentPhase != "fixing" {
		return false
	}
	root := mutationRoot(input)
	if root == "" {
		return false
	}
	rel := reviewerRelativePath(input, path)
	for _, allowed := range input.Runtime.RepairResultAnswerPaths {
		if rel == allowed && workpacket.CheckPath(root, rel) == nil {
			return true
		}
	}
	return false
}

// The exception is one exact issued authoring file, not a directory grant.
// The original plan barrier and the domain consumer remain independent.
func workPacketWriteDecision(input Input) (Decision, bool) {
	if !input.Runtime.PacketMode || input.Event != "PreToolUse" {
		return Decision{}, false
	}
	paths, mutating := repairMutationPaths(input)
	if !mutating {
		return Decision{}, false
	}
	for _, path := range paths {
		rel := reviewerRelativePath(input, path)
		if rel == ".loop-submissions" || strings.HasPrefix(rel, ".loop-submissions/") {
			if workPacketAnswerAllowed(input, path) || workerSubmissionPathAllowed(input, path) {
				continue
			}
			if unidentifiedBugResolutionControlPath(input, path) {
				continue
			}
			return Decision{Decision: "deny", RuleID: "worker_submission_scope", Reason: fmt.Sprintf("%s is not an exact submission path in the frozen Worker context", rel), AffectedPath: rel}, true
		}
		if rel == ".claude/work-packets" || strings.HasPrefix(rel, ".claude/work-packets/") {
			return Decision{Decision: "deny", RuleID: "work_packet_read_only", Reason: "issued packet views are Writer projections", AffectedPath: rel}, true
		}
		if rel == ".claude/submissions" || strings.HasPrefix(rel, ".claude/submissions/") {
			// These existing Worker messages retain their original adapters.
			if rel == ".claude/submissions/plan.json" || rel == ".claude/submissions/completion.json" {
				continue
			}
			if reviewResultAnswerAllowed(input, path) {
				continue
			}
			if hypothesisResultAnswerAllowed(input, path) {
				continue
			}
			if repairPlanReportAnswerAllowed(input, path) {
				continue
			}
			if repairResultAnswerAllowed(input, path) {
				continue
			}
			if repairResultResolutionAllowed(input, path) {
				continue
			}
			if mainPlanningAnswerAllowed(input, path) {
				continue
			}
			// A missing projection or an already received S2 answer is not a
			// reason to deny Main's ordinary tools. The original receiver still
			// decides whether an event belongs to an issued responsibility.
			if input.Runtime.CurrentState == "planning" && input.Runtime.CurrentPhase == "design" && (input.EffectiveAgentID() == "" || input.EffectiveAgentID() == "Main") && input.Runtime.Agent == nil {
				continue
			}
			// S8/S9 can be observed without a platform agent identity. Do not
			// infer an owner from a teammate name or turn that missing observation
			// into a denial; the original receiver associates the answer with its
			// issued responsibility when one is available.
			if unidentifiedBugResolutionControlPath(input, path) {
				continue
			}
			if !workPacketAnswerAllowed(input, path) {
				return Decision{Decision: "deny", RuleID: "work_packet_answer_scope", Reason: fmt.Sprintf("%s is not this activated owner's current issued answer", rel), AffectedPath: rel, Recovery: []string{"read your issued packet; complete the original plan/activation checkpoint before saving the exact assigned answer path"}}, true
			}
		}
	}
	return Decision{}, false
}

func repairResultResolutionAllowed(input Input, path string) bool {
	if !input.Runtime.PacketMode || input.EffectiveAgentID() != "" || input.TeammateName != "" ||
		input.Runtime.Agent != nil || input.SessionID == "" || input.Runtime.CurrentState != "bug_resolution" ||
		input.Runtime.CurrentPhase != "fixing" || input.Runtime.Workspace == nil {
		return false
	}
	root := mutationRoot(input)
	if root == "" || root != input.Runtime.Workspace.MainRoot {
		return false
	}
	if cwd, err := workspace.Canonical(input.CWD); err != nil || cwd != root {
		return false
	}
	if _, marked, err := workspace.PointerRoot(input.CWD); err != nil || marked {
		return false
	}
	for _, execution := range input.Runtime.Workspace.AllExecutions() {
		if execution.PlatformSessionID == input.SessionID {
			return false
		}
	}
	for _, allowed := range input.Runtime.RepairResultResolutionPaths {
		if reviewerRelativePath(input, path) == allowed && workpacket.CheckPath(root, allowed) == nil {
			return true
		}
	}
	return false
}
