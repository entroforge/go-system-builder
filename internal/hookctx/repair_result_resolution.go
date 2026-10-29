package hookctx

import (
	"encoding/json"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

func projectRepairResultResolution(root string, snapshot runtime.Snapshot, owner string, context *policy.RuntimeContext) {
	if context == nil || !context.PacketMode || context.CurrentState != "bug_resolution" || context.CurrentPhase != "fixing" {
		return
	}
	if owner != "" && context.Agent != nil && context.Agent.RepairResultAssignment {
		pending, err := runtime.RepairResultResumePending(root, snapshot.State, owner, context.Agent.RepairAssignmentID)
		context.Agent.RepairResultResumePending = pending || err != nil
		return
	}
	if owner != "" || context.Agent != nil {
		return
	}
	packets, err := runtime.CurrentRepairResultPackets(root, snapshot.State, "")
	if err != nil {
		return
	}
	for _, packet := range packets {
		owner := packet.Binding.Sources.OwnerAgentID
		blocker, accepted, err := runtime.VerifiedRepairResultBlockerReceipt(root, snapshot.State, packet.ID, owner)
		if err != nil || !accepted {
			continue
		}
		entities, _ := snapshot.State["entities"].(map[string]any)
		agents, _ := entities["agents"].([]any)
		matches := 0
		for _, raw := range agents {
			agent, _ := raw.(map[string]any)
			if agent["id"] == owner && workstate.AgentActivity(agent) == "blocked" &&
				agent["work_blocked_ref"] == blocker.Artifact.MessageRef &&
				(agent["blocker_resolved_ref"] == nil || agent["blocker_resolved_ref"] == "") {
				matches++
			}
		}
		var answer struct {
			Verdict string `json:"verdict"`
		}
		if matches != 1 || json.Unmarshal(blocker.Artifact.SubmittedAnswer, &answer) != nil || answer.Verdict == "contract_conflict" {
			continue
		}
		path, err := workpacket.RepairResultResolutionAuthoringPath(packet)
		if err == nil {
			context.RepairResultResolutionPaths = append(context.RepairResultResolutionPaths, path)
		}
	}
}
