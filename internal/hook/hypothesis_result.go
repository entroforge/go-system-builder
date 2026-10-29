package hook

import (
	"encoding/json"

	"github.com/entroforge/go-system-builder/internal/policy"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const hypothesisResultTemplate = "s8-hypothesis-result-v1"

// HypothesisResultAssignmentBinding identifies a current registered S8
// responsibility from the same Runtime snapshot used by the stop/idle hook.
// A malformed current Case falls back only to its persisted packet tuple so a
// known owner cannot escape through a generic completion report; that fallback
// never establishes completion authority.
func HypothesisResultAssignmentBinding(root string, state map[string]any, owner string) (assignmentID string, bound bool) {
	if state == nil || owner == "" {
		return "", false
	}
	packets, err := loopruntime.CurrentHypothesisResultPackets(root, state, owner)
	if err == nil {
		for _, packet := range packets {
			if packet.Binding.OwnerAgentID != owner {
				continue
			}
			if !bound {
				assignmentID = packet.Binding.AssignmentID
				bound = true
				continue
			}
			if assignmentID != packet.Binding.AssignmentID {
				return "", true
			}
		}
		if bound {
			return assignmentID, true
		}
		// A current Case may still name this issued Assignment after its
		// question or route has drifted enough that the selector no longer
		// considers the packet consumable. Keep the responsibility known until
		// the original Runtime tuple is formally retired; a generic report must
		// not silently turn stale work into completion.
		assignmentID, bound = rawCurrentHypothesisResultBinding(state, owner)
		if !bound {
			return "", false
		}
		// ConsumeCaseRoute is the original authority that formally retires
		// S8 responsibility. It updates the Runtime Case pointer, not the
		// immutable Case bytes. The receipt remains available through the
		// original read-only receipt command, but a retired question cannot
		// keep Stop/Idle responsibility alive.
		if s8RouteFormallyConsumed(state) {
			return "", false
		}
		return assignmentID, true
	}
	assignmentID, bound = rawCurrentHypothesisResultBinding(state, owner)
	if !bound {
		return "", false
	}
	if s8RouteFormallyConsumed(state) {
		return "", false
	}
	return assignmentID, true
}

// HypothesisResultConsumed proves only the exact current registered
// Investigator's original immutable Case receipt. It does not require the
// whole Case to be routed or approved and never reads the author's answer
// file. The Runtime verifier binds the packet issue, full answer, original
// Case successor and journal transaction.
func HypothesisResultConsumed(root string, state map[string]any, agent *policy.AgentContext) bool {
	if agent == nil || !agent.HypothesisResultAssignment || agent.ID == "" || agent.AssignmentID == "" || state == nil {
		return false
	}
	assignmentID, bound := HypothesisResultAssignmentBinding(root, state, agent.ID)
	if !bound || assignmentID != agent.AssignmentID || agent.TeamID == "" {
		return false
	}
	packets, err := loopruntime.CurrentHypothesisResultPackets(root, state, agent.ID)
	currentSelection := err == nil && len(packets) > 0
	if err != nil || len(packets) == 0 {
		// The active Case may have a changed question/route tuple after an
		// immutable acceptance. That makes it ineligible for a fresh grant,
		// but the exact original receipt remains a valid completion fact.
		packets, err = loopruntime.HypothesisResultPackets(state)
		if err != nil {
			return false
		}
	}
	var selected *workpacket.HypothesisResultPacket
	for i := range packets {
		packet := packets[i]
		binding := packet.Binding
		if binding.OwnerAgentID != agent.ID || binding.AssignmentID != agent.AssignmentID ||
			binding.TeamID != agent.TeamID || !hasResponsibilityTask(agent.TaskIDs, binding.TaskID) {
			continue
		}
		if !currentSelection && !sameCurrentHypothesisTuple(state, binding) {
			continue
		}
		if selected != nil {
			return false
		}
		selected = &packet
	}
	if selected == nil || loopruntime.VerifyHypothesisResultPacketIssue(root, state, *selected) != nil {
		return false
	}
	receipt, accepted, err := loopruntime.VerifiedHypothesisResultReceipt(root, state, selected.ID, agent.ID)
	if err != nil || !accepted {
		return false
	}
	binding := selected.Binding
	return receipt.PacketID == selected.ID && receipt.OwnerAgentID == agent.ID &&
		receipt.RuntimeID == binding.RuntimeID && receipt.BaselineGeneration == binding.BaselineGeneration &&
		receipt.ReviewRound == binding.ReviewRound && receipt.AssignmentID == binding.AssignmentID &&
		receipt.TeamID == binding.TeamID && receipt.TaskID == binding.TaskID &&
		receipt.ManifestRef == binding.ManifestRef && receipt.ManifestSHA256 == binding.ManifestSHA256 &&
		receipt.SuccessorCaseRef != "" && receipt.SuccessorCaseRevision > binding.CaseRevision && receipt.AnswerSHA256 != ""
}

func s8RouteFormallyConsumed(state map[string]any) bool {
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	return responsibilityText(pointer["route_consumed_at"]) != "" &&
		responsibilityText(pointer["route_consumer"]) == "S8-ROUTE-CONSUME"
}

func sameCurrentHypothesisTuple(state map[string]any, binding workpacket.HypothesisResultBinding) bool {
	if state == nil {
		return false
	}
	reviewState, _ := state["review"].(map[string]any)
	pointer, _ := reviewState["investigation"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	return pointer != nil && binding.RuntimeID == responsibilityText(state["runtime_id"]) &&
		binding.BaselineGeneration == responsibilityInt(baseline["generation"]) &&
		binding.ReviewRound == responsibilityInt(reviewState["round"]) &&
		binding.CaseID == responsibilityText(pointer["case_id"])
}

func rawCurrentHypothesisResultBinding(state map[string]any, owner string) (string, bool) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	reviewState, _ := state["review"].(map[string]any)
	pointer, _ := reviewState["investigation"].(map[string]any)
	if pointer == nil {
		return "", false
	}
	runtimeID := responsibilityText(state["runtime_id"])
	baseline, _ := state["baseline"].(map[string]any)
	generation := responsibilityInt(baseline["generation"])
	round := responsibilityInt(reviewState["round"])
	caseID := responsibilityText(pointer["case_id"])
	if runtimeID == "" || generation < 1 || round < 1 || caseID == "" {
		return "", false
	}
	assignmentID := ""
	bound := false
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil || row["template_id"] != hypothesisResultTemplate {
			continue
		}
		binding, _ := row["binding"].(map[string]any)
		if responsibilityText(binding["owner_agent_id"]) != owner ||
			responsibilityText(binding["runtime_id"]) != runtimeID ||
			responsibilityInt(binding["baseline_generation"]) != generation ||
			responsibilityInt(binding["review_round"]) != round ||
			responsibilityText(binding["case_id"]) != caseID {
			continue
		}
		candidate := responsibilityText(binding["assignment_id"])
		if !bound {
			assignmentID, bound = candidate, true
			continue
		}
		if candidate != assignmentID {
			return "", true
		}
	}
	return assignmentID, bound
}

func hasResponsibilityTask(values []string, want string) bool {
	if want == "" {
		return false
	}
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func responsibilityText(value any) string {
	text, _ := value.(string)
	return text
}

func responsibilityInt(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		parsed, _ := number.Int64()
		return int(parsed)
	default:
		return 0
	}
}
