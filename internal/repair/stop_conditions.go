package repair

import (
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/evidence"
)

func validateStopAssessmentShape(value TargetedReverification) error {
	for _, item := range value.StopConditionAssessments {
		if item.ConditionIndex < 0 || len(item.ContractSHA256) != 64 || strings.TrimSpace(item.Rationale) == "" || len(item.EvidenceRefs) == 0 {
			return fmt.Errorf("stop condition assessment requires contract_sha256, non-negative condition_index, rationale and evidence_refs")
		}
		for _, ref := range item.EvidenceRefs {
			if strings.TrimSpace(ref) == "" {
				return fmt.Errorf("stop condition assessment contains an empty evidence_ref")
			}
		}
		switch item.Outcome {
		case "not_triggered", "triggered", "unknown":
		default:
			return fmt.Errorf("invalid stop condition outcome %q", item.Outcome)
		}
		if value.Result == "pass" && item.Outcome != "not_triggered" {
			return fmt.Errorf("stop condition %d is %s; cannot pass: return to S8 for investigation or exact contract approval", item.ConditionIndex, item.Outcome)
		}
	}
	return nil
}

// Historical artifacts remain readable without assessments. Consuming them
// to finish a live repair requires fresh, complete independent assessments.
func validateStopConditionCoverage(contract ApprovedContract, value TargetedReverification) error {
	if err := validateStopAssessmentShape(value); err != nil {
		return err
	}
	if len(value.StopConditionAssessments) != len(contract.StopEscalationConditions) {
		return fmt.Errorf("stop condition coverage is incomplete: got %d, require %d assessments for contract %s; independently assess every stop_escalation_conditions entry", len(value.StopConditionAssessments), len(contract.StopEscalationConditions), contract.Ref.SHA256)
	}
	seen := map[int]bool{}
	for _, item := range value.StopConditionAssessments {
		if item.ContractSHA256 != contract.Ref.SHA256 || item.ConditionIndex >= len(contract.StopEscalationConditions) || seen[item.ConditionIndex] {
			return fmt.Errorf("stop condition assessment has a wrong contract SHA, out-of-range or duplicate condition_index %d", item.ConditionIndex)
		}
		seen[item.ConditionIndex] = true
	}
	return nil
}

func validateRuntimeStopConditions(root string, state, pointer map[string]any, plan RepairPlan, value TargetedReverification, actor string) error {
	// Failure must remain reportable even before all conditions can be assessed.
	// It cannot complete the repair or authorize a deviation.
	if value.Result != "pass" {
		return nil
	}
	contract, err := ValidateApprovedContractRef(root, ContractRef{Path: stringField(pointer["contract_ref"]), SHA256: stringField(pointer["contract_sha256"])})
	if err != nil {
		return err
	}
	if err := validateStopConditionCoverage(contract, value); err != nil {
		return err
	}
	if err := bindTargetedReverificationIdentities(pointer, plan, value); err != nil {
		return err
	}
	owners := stringMapField(pointer["assignment_owners"])
	verifier := dispatchedVerifierAgentID(value.PerformingAssignmentID, owners)
	if actor != "" && actor != verifier {
		return fmt.Errorf("stop condition review actor %q is not the dispatched verifier %q", actor, verifier)
	}
	// Independence covers the whole repair batch, including sibling builders.
	results, err := validateCurrentRepairResults(root, state, pointer)
	if err != nil {
		return err
	}
	for _, result := range results {
		if result.ProducerAgentID == verifier {
			return fmt.Errorf("stop condition reviewer %s produced repair Result %s and is not independent", verifier, result.ResultID)
		}
	}
	for _, item := range value.StopConditionAssessments {
		if err := evidence.ValidateRefs(state, item.EvidenceRefs, evidence.RefsOptions{Root: root, RequireReviewRound: integerValue(mapField(state, "review")["round"])}); err != nil {
			return fmt.Errorf("stop condition %d evidence: %w", item.ConditionIndex, err)
		}
	}
	return nil
}
