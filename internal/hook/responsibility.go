package hook

import (
	"context"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// ResponsibilityFacts is a read-only execution projection. A recorded delivery
// is not proof of passing checks or permission to advance the global stage.
type ResponsibilityFacts struct {
	Owner                  string           `json:"owner"`
	CurrentResponsibility  string           `json:"current_responsibility,omitempty"`
	Status                 workstate.Status `json:"status"`
	NextAction             string           `json:"next_action"`
	AssignmentID           string           `json:"assignment_id,omitempty"`
	DeliveryRecorded       bool             `json:"delivery_recorded"`
	ExecutionMayEnd        bool             `json:"execution_may_end"`
	OwnPending             []string         `json:"own_pending"`
	ConsumerOwner          string           `json:"consumer_owner,omitempty"`
	ConsumerPending        bool             `json:"consumer_pending"`
	MissingClosingContract bool             `json:"missing_closing_contract"`
}

// ResponsibilityProofs carries the domain-consumer facts that may complete a
// packet-backed responsibility. S7 and S8 receipts come from their original
// committed consumers; blocker proof is supplied by the matching stage's
// original blocker route.
type ResponsibilityProofs struct {
	ReviewResultConsumed              bool
	ReviewBlockerRecorded             bool
	HypothesisResultConsumed          bool
	RepairResultWorkComplete          bool
	RepairResultBlockerCurrent        bool
	RepairResultResumePending         bool
	BuilderWorkComplete               bool
	CompletionAcknowledgementAccepted bool
}

// ProjectResponsibility shares the original Stop/Idle compatibility matrix.
// packetReceived must come from the original current S5 receiver, never from
// an agent message. For an S7 ReviewAssignment, callers may pass the trusted
// result-consumption fact as the fourth argument and an authenticated
// work_blocked/site_lost fact as the fifth. S7 dispatch mode and manifest
// status are not completion or blocker authority. Missing contracts remain
// visible without adding a gate.
func ProjectResponsibility(agent *policy.AgentContext, assignment *hookctx.AssignmentContext, packetReceived bool, reviewProofs ...bool) ResponsibilityFacts {
	proofs := ResponsibilityProofs{}
	if len(reviewProofs) > 0 {
		proofs.ReviewResultConsumed = reviewProofs[0]
	}
	if len(reviewProofs) > 1 {
		proofs.ReviewBlockerRecorded = reviewProofs[1]
	}
	return ProjectResponsibilityWithProofs(agent, assignment, packetReceived, proofs)
}

// ProjectResponsibilityWithProofs is the named form used by Stop/Idle and
// Controller projections. S8 and S9 use only their exact original domain
// receipts for delivery; generic completion refs, terminal Agent labels and
// one-shot modes cannot stand in for an issued professional answer.
func ProjectResponsibilityWithProofs(agent *policy.AgentContext, assignment *hookctx.AssignmentContext, packetReceived bool, proofs ResponsibilityProofs) ResponsibilityFacts {
	f := ResponsibilityFacts{Status: workstate.Pending, OwnPending: []string{}}
	if agent == nil {
		return f
	}
	f.Owner = agent.ID
	if assignment != nil && assignment.OwnerAgentID != agent.ID {
		assignment = nil
	}
	if assignment != nil {
		f.AssignmentID = assignment.AssignmentID
		f.MissingClosingContract = len(assignment.DoneWhen) == 0
	}
	f.CurrentResponsibility = firstResponsibilityID(agent, assignment)
	activity := agent.LifecycleActivity()
	currentBlocker := proofs.ReviewBlockerRecorded ||
		(agent.RepairResultAssignment && proofs.RepairResultBlockerCurrent) ||
		(!agent.RepairResultAssignment && agent.State == string(workstate.Blocked) && activity == "blocked" && agent.WorkBlockedRef != "" && agent.BlockerResolvedRef == "") ||
		(!agent.RepairResultAssignment && agent.State == string(workstate.Blocked) && activity == "blocked" && agent.LifecycleEvent == "document_conflict_reported" && agent.DocumentConflictRef != "")
	cancelled := agent.State == string(workstate.Cancelled) && activity == "stopped" && agent.ShutdownApprovedRef != ""
	reviewResultConsumed := proofs.ReviewResultConsumed
	reviewBlockerRecorded := proofs.ReviewBlockerRecorded
	if agent.ReviewAssignment {
		f.DeliveryRecorded = reviewResultConsumed
		f.ExecutionMayEnd = reviewResultConsumed || reviewBlockerRecorded || activity == "queued"
	} else if agent.RepairResultAssignment {
		f.DeliveryRecorded = proofs.RepairResultWorkComplete
		f.ExecutionMayEnd = proofs.RepairResultWorkComplete || currentBlocker
	} else if agent.HypothesisResultAssignment {
		f.DeliveryRecorded = proofs.HypothesisResultConsumed
		f.ExecutionMayEnd = f.DeliveryRecorded || activity == "queued" || currentBlocker
	} else {
		f.DeliveryRecorded = agent.CompletionReportedRef != "" || HasCompletionReport(assignment) || packetReceived || proofs.BuilderWorkComplete
		f.ExecutionMayEnd = f.DeliveryRecorded || activity == "queued" || currentBlocker
	}
	switch agent.DispatchMode {
	case "one_shot":
		if !agent.ReviewAssignment && !agent.HypothesisResultAssignment && !agent.RepairResultAssignment {
			f.ExecutionMayEnd = true
		}
	case "plan_approval_required":
		if !agent.ReviewAssignment && !agent.HypothesisResultAssignment && !agent.RepairResultAssignment {
			f.ExecutionMayEnd = true
		}
	}
	if f.DeliveryRecorded {
		f.ConsumerOwner = "main"
		if agent.RepairResultAssignment {
			// An accepted S9 candidate plus its current owner-owned checks ends
			// this Worker's responsibility, while Main integration and final
			// Result consumption remain pending.
			f.ConsumerPending = true
		} else if agent.ReviewAssignment || agent.HypothesisResultAssignment {
			// S7 SubmitResult and the round consumer commit together; there is
			// no separate completion acknowledgement to wait for. The original
			// S8 Case receipt similarly ends this Investigator's own work.
			f.ConsumerPending = false
		} else {
			f.ConsumerPending = assignment == nil || assignment.CompletionAckRef == ""
		}
	} else if agent.RepairResultAssignment && currentBlocker {
		// A typed non-pass receipt ends this Worker execution while leaving the
		// original blocker resolution consumer pending at Main.
		f.ConsumerOwner = "main"
		f.ConsumerPending = true
	}
	if agent.RepairResultAssignment && proofs.RepairResultResumePending {
		f.DeliveryRecorded = false
		f.ExecutionMayEnd = false
		f.ConsumerOwner, f.ConsumerPending = "main", true
		f.Status = workstate.Active
		f.NextAction = "await_consumer"
		return f
	}
	accepted := packetReceived || reviewResultConsumed || proofs.HypothesisResultConsumed || proofs.RepairResultWorkComplete || proofs.BuilderWorkComplete
	if !agent.ReviewAssignment && !agent.HypothesisResultAssignment && !agent.RepairResultAssignment && !requiresBuilderReceipt(assignment) {
		accepted = accepted || proofs.CompletionAcknowledgementAccepted
	}
	f.Status = workstate.Project(workstate.Facts{
		Started:                            workstate.AgentStarted(agent.State),
		ExplicitBlocker:                    currentBlocker,
		OriginalConsumerAcceptedCompletion: accepted,
		OriginalCancellationOrReplacement:  cancelled,
	})
	f.NextAction = responsibilityNextAction(f.Status, f.DeliveryRecorded, f.ConsumerPending)
	if !f.ExecutionMayEnd {
		if agent.PlanReportedRef == "" && !agent.DocumentReviewAssignment && !agent.ReviewAssignment && !agent.HypothesisResultAssignment {
			f.OwnPending = append(f.OwnPending, "PLAN_REPORT")
		}
		if agent.RepairResultAssignment {
			f.OwnPending = append(f.OwnPending, "RepairResult")
		} else if agent.HypothesisResultAssignment {
			f.OwnPending = append(f.OwnPending, "HypothesisResult")
		} else {
			f.OwnPending = append(f.OwnPending, "Result")
		}
	}
	return f
}

// CurrentResponsibility gathers the existing consumer proofs for one loaded
// owner/Assignment pair and projects their shared five-state responsibility.
// It is read-only and uses the caller's original Runtime snapshot.
func CurrentResponsibility(root string, snapshot loopruntime.Snapshot, agent *policy.AgentContext, assignment *hookctx.AssignmentContext, packetReceived bool) ResponsibilityFacts {
	if agent == nil {
		return ProjectResponsibilityWithProofs(nil, nil, false, ResponsibilityProofs{})
	}
	assignmentRequired := agent.AssignmentID != "" || agent.ReviewAssignment || agent.HypothesisResultAssignment || agent.RepairResultAssignment
	if assignment != nil && (agent.ID == "" || agent.AssignmentID == "" || assignment.AssignmentID == "" ||
		assignment.OwnerAgentID != agent.ID || assignment.AssignmentID != agent.AssignmentID) ||
		assignment == nil && assignmentRequired {
		facts := ProjectResponsibilityWithProofs(agent, nil, false, ResponsibilityProofs{})
		facts.ExecutionMayEnd = false
		facts.DeliveryRecorded = false
		facts.ConsumerOwner = ""
		facts.ConsumerPending = false
		facts.NextAction = responsibilityNextAction(facts.Status, false, false)
		return facts
	}
	proofs := ResponsibilityProofs{}
	if agent.ReviewAssignment {
		if loopruntime.PacketEnabled(snapshot.State) {
			proofs.ReviewResultConsumed = currentPacketReviewDelivery(snapshot.State, agent, root)
		} else {
			proofs.ReviewResultConsumed = ReviewResultConsumed(root, snapshot.State, agent)
		}
		if !loopruntime.PacketEnabled(snapshot.State) || !proofs.ReviewResultConsumed {
			proofs.ReviewBlockerRecorded = ReviewAssignmentBlocked(root, snapshot.State, agent, assignment)
		}
	}
	if agent.HypothesisResultAssignment {
		proofs.HypothesisResultConsumed = HypothesisResultConsumed(root, snapshot.State, agent)
	}
	if agent.RepairResultAssignment {
		resumePending, resumeErr := loopruntime.RepairResultResumePending(root, snapshot.State, agent.ID, agent.RepairAssignmentID)
		proofs.RepairResultResumePending = resumePending || resumeErr != nil
		if agent.State == string(workstate.Blocked) && agent.LifecycleActivity() == "blocked" {
			proofs.RepairResultBlockerCurrent = currentRepairResultBlocker(root, snapshot.State, agent)
		} else {
			proofs.RepairResultWorkComplete = RepairResultWorkComplete(root, snapshot.State, agent)
		}
	}
	if requiresBuilderReceipt(assignment) && !agent.ReviewAssignment && !agent.HypothesisResultAssignment && !agent.RepairResultAssignment {
		proofs.BuilderWorkComplete = currentBuilderWorkComplete(root, snapshot, assignment)
	} else if !agent.ReviewAssignment && !agent.HypothesisResultAssignment && !agent.RepairResultAssignment {
		proofs.CompletionAcknowledgementAccepted = currentCompletionAcknowledgementAccepted(root, agent, assignment)
	}
	return ProjectResponsibilityWithProofs(agent, assignment, packetReceived, proofs)
}

// currentPacketReviewDelivery reads the original consumer's committed delivery
// projection. Artifact integrity remains with the consumer and receipt API.
func currentPacketReviewDelivery(state map[string]any, agent *policy.AgentContext, roots ...string) bool {
	root := ""
	if len(roots) > 0 {
		root = roots[0]
	}
	packets, err := loopruntime.CurrentReviewResultPacketsContext(loopruntime.WithPacketBodyRoot(context.Background(), root), state, agent.ID)
	if err != nil {
		return false
	}
	for _, packet := range packets {
		if packet.Binding.AssignmentID != agent.AssignmentID {
			continue
		}
		reviewState, _ := state["review"].(map[string]any)
		assignments, _ := reviewState["assignments"].(map[string]any)
		row, _ := assignments[agent.AssignmentID].(map[string]any)
		ref := reviewProofText(row["result_ref"])
		receipt, _ := row["result_receipt"].(map[string]any)
		if workstate.ReviewAssignmentActivity(row) != "consumed" || ref == "" ||
			receipt["packet_id"] != packet.ID || receipt["producer_agent_id"] != agent.ID ||
			receipt["assignment_id"] != agent.AssignmentID || receipt["result_ref"] != ref {
			return false
		}
		evidence, count := reviewProofEvidenceAtPath(state, ref, "review_result")
		return count == 1 && evidence != nil && evidence["status"] == "valid" &&
			reviewProofInt(evidence["baseline_generation"]) == packet.Binding.BaselineGeneration &&
			reviewProofInt(evidence["review_round"]) == packet.Binding.ReviewRound &&
			evidence["invalidated_by"] == nil && evidence["invalidation_rule"] == nil && evidence["invalidation_reason"] == nil &&
			reviewProofText(evidence["sha256"]) != "" && evidence["sha256"] == receipt["result_sha256"]
	}
	return false
}

func currentRepairResultBlocker(root string, state map[string]any, agent *policy.AgentContext) bool {
	if agent == nil || agent.ID == "" || agent.RepairAssignmentID == "" || state == nil {
		return false
	}
	packets, err := loopruntime.RepairResultPacketsForOwnerContext(loopruntime.WithPacketBodyRoot(context.Background(), root), state, agent.ID)
	if err != nil {
		return false
	}
	verified := 0
	for _, packet := range packets {
		binding := packet.Binding.Sources
		if binding.OwnerAgentID != agent.ID || binding.RepairAssignmentID != agent.RepairAssignmentID {
			continue
		}
		current, currentErr := repair.RepairResultBlockerExecutionMayEnd(root, state, packet.ID, agent.ID)
		if currentErr != nil {
			return false
		}
		if current {
			verified++
		}
	}
	return verified == 1
}

func firstResponsibilityID(agent *policy.AgentContext, assignment *hookctx.AssignmentContext) string {
	if agent.RepairAssignmentID != "" {
		return agent.RepairAssignmentID
	}
	if agent.AssignmentID != "" {
		return agent.AssignmentID
	}
	if assignment != nil && assignment.AssignmentID != "" {
		return assignment.AssignmentID
	}
	return agent.ID
}

func requiresBuilderReceipt(assignment *hookctx.AssignmentContext) bool {
	if assignment == nil {
		return false
	}
	if assignment.AssignmentRecord != nil || assignment.RoleFamily == "backend-builder" || assignment.RoleFamily == "frontend-builder" || assignment.RoleFamily == "test-builder" {
		return true
	}
	for _, responsibility := range assignment.ResponsibilityIDs {
		if responsibility == "BUILD-WORK-PACKAGE" {
			return true
		}
	}
	return false
}

func responsibilityNextAction(status workstate.Status, deliveryRecorded, consumerPending bool) string {
	switch status {
	case workstate.Pending:
		return "start"
	case workstate.Active:
		if deliveryRecorded || consumerPending {
			return "await_consumer"
		}
		return "continue"
	case workstate.Blocked:
		if consumerPending {
			return "await_consumer"
		}
		return "resolve_blocker"
	case workstate.Done:
		if consumerPending {
			return "await_consumer"
		}
		return "await_next_assignment"
	case workstate.Cancelled:
		return "follow_replacement"
	default:
		return ""
	}
}
