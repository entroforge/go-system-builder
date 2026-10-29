package repair

import (
	"context"
	"fmt"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// RepairResultBlockerExecutionMayEnd projects current blocked work. Missing
// implementation remains pending with Main; this is not a source proof gate.
func RepairResultBlockerExecutionMayEnd(root string, state map[string]any, packetID, owner string) (bool, error) {
	packet, found, err := runtimepkg.FindRepairResultPacketContext(runtimepkg.WithPacketBodyRoot(context.Background(), root), state, packetID)
	if err != nil || !found {
		return false, err
	}
	if owner != packet.Binding.Sources.OwnerAgentID {
		return false, nil
	}
	agent, found, err := repairResultBlockerAgent(state, owner)
	if err != nil || !found {
		return false, err
	}
	if workstate.AgentActivity(agent) != "blocked" || stringField(agent["work_blocked_ref"]) == "" ||
		stringField(agent["blocker_resolved_ref"]) != "" {
		return false, nil
	}
	receipt, found, err := runtimepkg.VerifiedRepairResultBlockerReceipt(root, state, packetID, owner)
	if err != nil || !found {
		return false, err
	}
	if stringField(agent["work_blocked_ref"]) != receipt.Artifact.MessageRef {
		return false, nil
	}
	if !currentRepairResultBlockerEvidence(state, receipt.ArtifactRef) || repairResultBlockerResolved(state, packet.ID, receipt.ArtifactRef) {
		return false, nil
	}
	answer, sha, err := readIssuedRepairResultBlockerAnswer(receipt.Artifact.ExecutionRoot, packet)
	if err != nil || sha != receipt.Artifact.AnswerSHA256 || !bytesEqualRepairPlanReportAnswer(answer, receipt.Artifact.SubmittedAnswer) {
		return false, nil
	}
	execution, _, err := verifyIssuedRepairResultBlockerSource(context.Background(), root, state, packet, receipt.Artifact.ExecutionRoot, "")
	if err != nil {
		return false, err
	}
	return execution.AssignmentID == receipt.Artifact.ExecutionAssignmentID && execution.Generation == receipt.Artifact.ExecutionGeneration, nil
}

func repairResultBlockerAgent(state map[string]any, owner string) (map[string]any, bool, error) {
	entities, _ := state["entities"].(map[string]any)
	agents, ok := entities["agents"].([]any)
	if !ok {
		return nil, false, fmt.Errorf("Runtime Agent registry is unavailable for S9 blocker projection")
	}
	var selected map[string]any
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row == nil || stringField(row["id"]) != owner {
			continue
		}
		if selected != nil {
			return nil, false, fmt.Errorf("Runtime has ambiguous Agent rows for S9 blocker owner %q", owner)
		}
		selected = row
	}
	return selected, selected != nil, nil
}

func currentRepairResultBlockerEvidence(state map[string]any, artifactRef string) bool {
	rows, ok := state["evidence"].([]any)
	if !ok || artifactRef == "" {
		return false
	}
	count := 0
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil || stringField(row["id"]) != artifactRef {
			continue
		}
		count++
		if row["status"] != "valid" || row["invalidated_by"] != nil ||
			row["invalidation_rule"] != nil || row["invalidation_reason"] != nil {
			return false
		}
	}
	return count == 1
}

func repairResultBlockerResolved(state map[string]any, packetID, artifactRef string) bool {
	rows, _ := state["evidence"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil || row["kind"] != "repair_result_resolution" {
			continue
		}
		refs, ok := row["scope_refs"].([]any)
		if !ok {
			continue
		}
		packetMatch, blockerMatch := false, false
		for _, ref := range refs {
			value, _ := ref.(string)
			packetMatch = packetMatch || value == packetID
			blockerMatch = blockerMatch || value == artifactRef
		}
		if packetMatch || blockerMatch {
			return true
		}
	}
	return false
}

func repairResultStateRevision(state map[string]any) int {
	return integerRepairResult(state["revision"])
}
