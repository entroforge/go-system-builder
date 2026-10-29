package hook

import (
	"context"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// RepairResultAssignmentBinding identifies the S9 Builder responsibility
// from the current repairing pointer's original Assignment→owner projection.
// A missing or ambiguous owner projection remains bound with an empty ID so a
// generic completion report cannot make the known responsibility disappear.
func RepairResultAssignmentBinding(root string, state map[string]any, owner string) (assignmentID string, bound bool) {
	if state == nil || owner == "" || !loopruntime.PacketEnabled(state) {
		return "", false
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "bug_resolution" {
		return "", false
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	if pointer == nil || pointer["session_id"] == "" || pointer["plan_ref"] == "" {
		return "", false
	}
	owners, _ := pointer["assignment_owners"].(map[string]any)
	for id, rawOwner := range owners {
		if id == "" || rawOwner != owner {
			continue
		}
		if bound {
			return "", true
		}
		assignmentID, bound = id, true
	}
	if !bound {
		return "", false
	}
	packets, err := loopruntime.RepairResultPacketsForOwnerContext(loopruntime.WithPacketBodyRoot(context.Background(), root), state, owner)
	if err != nil {
		// A malformed packet registry must not turn an issued typed task back
		// into a generic completion path.
		return assignmentID, true
	}
	runtimeID := workspace.RuntimeID(state)
	generation := workspace.Generation(state)
	sessionID, _ := pointer["session_id"].(string)
	planRef, _ := pointer["plan_ref"].(string)
	for _, packet := range packets {
		b := packet.Binding.Sources
		if b.RepairAssignmentID == assignmentID && b.RuntimeID == runtimeID && b.BaselineGeneration == generation &&
			b.SessionID == sessionID && b.PlanRef == planRef {
			return assignmentID, true
		}
	}
	return "", false
}

// RepairResultWorkComplete reads the current Builder's accepted delivery
// facts. Main integration/checks remain separate work; Stop does not re-prove
// them or require the cleaned-up Worker checkout to still exist.
func RepairResultWorkComplete(root string, state map[string]any, agent *policy.AgentContext) bool {
	if agent == nil || !agent.RepairResultAssignment || agent.ID == "" || agent.RepairAssignmentID == "" || state == nil {
		return false
	}
	assignmentID, bound := RepairResultAssignmentBinding(root, state, agent.ID)
	if !bound || assignmentID != agent.RepairAssignmentID {
		return false
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	packets, err := loopruntime.RepairResultPacketsForOwnerContext(loopruntime.WithPacketBodyRoot(context.Background(), root), state, agent.ID)
	if err != nil {
		return false
	}
	completed := 0
	for _, packet := range packets {
		b := packet.Binding.Sources
		if b.OwnerAgentID != agent.ID || b.RepairAssignmentID != assignmentID ||
			b.RuntimeID != workspace.RuntimeID(state) || b.BaselineGeneration != workspace.Generation(state) ||
			b.SessionID != pointer["session_id"] || b.PlanRef != pointer["plan_ref"] {
			continue
		}
		// The domain projection associates the retained candidate with the
		// current execution generation/rework/path. An old attempt cannot
		// complete the current one, even after its historical Final exists.
		complete, err := repair.RepairResultWorkComplete(root, state, packet.ID, agent.ID)
		if err != nil {
			return false
		}
		if complete {
			completed++
		}
	}
	return completed == 1
}
