package repair

import (
	"context"
	"encoding/json"
	"sort"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// ReceivedRepairContributions reads the original accepted contributions. It
// neither inspects a worktree nor qualifies Main's actual integration/checks.
// A newer accepted Candidate remains visible beside historical integration.
func ReceivedRepairContributions(root string, state map[string]any) ([]RepairResult, error) {
	p := repairPointer(state)
	planBytes, err := workpacket.ReadFile(root, stringField(p["plan_ref"]), 20<<20)
	if err != nil {
		return nil, err
	}
	return receivedRepairContributions(root, state, planBytes)
}

// ReceivedRepairContributionCount reports how many contributions the current
// original responsibility already received. The count never depends on the
// original RepairPlan content: accepted Results come from the Runtime pointer
// and workspace candidates come from the execution registry. A response that
// already read the original Plan for its own consumer therefore does not read
// the same original Plan a second time just to decide the current phase.
// L3-S9-repair.md:502 requires read and consumer counts to prove the cost
// boundary of one response.
func ReceivedRepairContributionCount(root string, state map[string]any) (int, error) {
	results, err := receivedRepairContributions(root, state, nil)
	if err != nil {
		return 0, err
	}
	return len(results), nil
}

// receivedRepairContributions reads the original accepted contributions
// against the original RepairPlan. planBytes is nil when the caller does not
// need Plan content; only the synthesized Contract identity uses it.
func receivedRepairContributions(root string, state map[string]any, planBytes []byte) ([]RepairResult, error) {
	p := repairPointer(state)
	results := []RepairResult{}
	if len(existingArtifactRefs(p["result_refs"])) > 0 || stringField(p["result_ref"]) != "" {
		var err error
		results, err = currentRepairResults(root, p)
		if err != nil {
			return nil, err
		}
	}
	positions := map[string]int{}
	for i, result := range results {
		positions[result.AssignmentID] = i
	}
	var plan RepairPlan
	if planBytes != nil {
		if err := json.Unmarshal(planBytes, &plan); err != nil {
			return nil, err
		}
	}
	registry, err := workspace.Decode(state)
	if err != nil {
		return nil, err
	}
	if registry == nil {
		return results, nil
	}
	packets, err := runtimepkg.RepairPlanReportPacketsForSourcesContext(context.Background(), root, state, "")
	if err != nil {
		return nil, err
	}
	responsibilities := map[string]workpacket.RepairPlanReportBinding{}
	for _, packet := range packets {
		if packet.Binding.SessionID == stringField(p["session_id"]) {
			responsibilities[packet.Binding.ManifestAssignmentID] = packet.Binding
			responsibilities[packet.Binding.RepairAssignmentID] = packet.Binding
		}
	}
	ids := make([]string, 0, len(registry.Executions))
	for id := range registry.Executions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		execution := registry.Executions[id]
		if execution.DeliveryRef == "" {
			continue
		}
		binding, selected := responsibilities[execution.AssignmentID]
		if !selected || execution.AgentID != binding.OwnerAgentID {
			continue
		}
		raw, err := workpacket.ReadFile(root, execution.DeliveryRef, 20<<20)
		if err != nil {
			return nil, err
		}
		candidate, err := DecodeWorkspaceCandidate(raw)
		if err != nil {
			return nil, err
		}
		if candidate.RecordType != "repair_workspace_candidate" || candidate.SessionID != stringField(p["session_id"]) {
			continue
		}
		proposal := candidate.Proposal
		position, existing := positions[proposal.AssignmentID]
		if existing && results[position].ResultID == proposal.ResultID {
			continue
		}
		result := RepairResult{SchemaVersion: "1.0.0", RecordType: "repair_result", ResultID: proposal.ResultID + "-main-impact", SessionID: candidate.SessionID, PlanID: candidate.PlanID,
			ContractID: plan.ContractID, BaselineGeneration: candidate.BaselineGeneration,
			ProducerAgentID: proposal.ProducerAgentID, AssignmentID: proposal.AssignmentID,
			BeforeFixChecks: append([]RepairCheck{}, proposal.BeforeFixChecks...), Checks: append([]RepairCheck{}, proposal.Checks...), UnitResults: append([]RepairUnitResult{}, proposal.UnitResults...),
			ChangedArtifacts: append([]ChangedArtifact{}, proposal.ChangedArtifacts...), ScopeDeviations: append([]string{}, proposal.ScopeDeviations...), ResidualRisks: append([]string{}, proposal.ResidualRisks...),
			Result: proposal.Result, Verdict: proposal.Verdict, BeforeFixResults: append([]RepairBeforeFixResult{}, proposal.BeforeFixResults...), DetectionGapArtifacts: append([]string{}, proposal.DetectionGapArtifacts...), EvidenceRefs: append([]string{}, proposal.EvidenceRefs...),
			MigrationRef: proposal.MigrationRef, RollbackRef: proposal.RollbackRef,
		}
		if proposal.PlanReport.Path != "" {
			result.PlanReportRef = &proposal.PlanReport
		}
		if existing {
			results[position] = result
		} else {
			positions[result.AssignmentID] = len(results)
			results = append(results, result)
		}
	}
	return results, nil
}
