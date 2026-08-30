package repair

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

func CreateRepairHandoff(root string, request HandoffRequest) (RepairHandoff, ArtifactRef, error) {
	if !strings.HasPrefix(request.HandoffID, "repair-handoff-") {
		return RepairHandoff{}, ArtifactRef{}, fmt.Errorf("request.HandoffID must carry the repair-handoff- prefix so Runtime can bind it (got %q)", request.HandoffID)
	}
	if strings.TrimSpace(request.HandedOffBy) == "" || strings.TrimSpace(request.NextAction) == "" {
		return RepairHandoff{}, ArtifactRef{}, errors.New("handed_off_by and next_action are required")
	}
	if len(request.TargetedReverifications) == 0 {
		return RepairHandoff{}, ArtifactRef{}, errors.New("handoff requires at least one targeted reverification")
	}
	if request.Session.Path == "" || request.Plan.Path == "" || request.Contract.Path == "" || request.Result.Path == "" || request.Changeset.Path == "" || request.ChangeImpact.Path == "" {
		return RepairHandoff{}, ArtifactRef{}, errors.New("handoff requires session, plan, contract, result, changeset, and change-impact references")
	}
	session, err := ValidateRepairSession(root, request.Session)
	if err != nil {
		return RepairHandoff{}, ArtifactRef{}, fmt.Errorf("handoff session: %w", err)
	}
	plan, err := ValidateRepairPlan(root, request.Plan)
	if err != nil {
		return RepairHandoff{}, ArtifactRef{}, fmt.Errorf("handoff plan: %w", err)
	}
	contract, err := ValidateApprovedContractRef(root, request.Contract)
	if err != nil {
		return RepairHandoff{}, ArtifactRef{}, fmt.Errorf("handoff contract: %w", err)
	}
	result, err := ValidateRepairResult(root, request.Result)
	if err != nil {
		return RepairHandoff{}, ArtifactRef{}, fmt.Errorf("handoff result: %w", err)
	}
	changeset, err := ValidateChangeset(root, request.Changeset)
	if err != nil {
		return RepairHandoff{}, ArtifactRef{}, fmt.Errorf("handoff changeset: %w", err)
	}
	impact, err := ValidateChangeImpact(root, request.ChangeImpact)
	if err != nil {
		return RepairHandoff{}, ArtifactRef{}, fmt.Errorf("handoff change impact: %w", err)
	}
	if session.ContractRef != contract.Ref.Path || session.ContractSHA256 != contract.Ref.SHA256 || plan.SessionID != session.SessionID || plan.ContractID != contract.ContractID || result.SessionID != session.SessionID || result.PlanID != plan.PlanID || result.ContractID != contract.ContractID || changeset.SessionID != session.SessionID {
		return RepairHandoff{}, ArtifactRef{}, fmt.Errorf("handoff artifacts are not transitively bound to the approved Contract, Session, and Plan")
	}
	for _, ref := range request.TargetedReverifications {
		target, err := ValidateTargetedReverification(root, ref)
		if err != nil {
			return RepairHandoff{}, ArtifactRef{}, fmt.Errorf("handoff targeted reverification: %w", err)
		}
		if target.ImpactID != impact.ImpactID {
			return RepairHandoff{}, ArtifactRef{}, fmt.Errorf("handoff targeted reverification %s points to impact %q, want %q", target.ReverificationID, target.ImpactID, impact.ImpactID)
		}
	}
	handoff := RepairHandoff{
		SchemaVersion: "1.0.0", RecordType: "repair_handoff", HandoffID: request.HandoffID,
		SessionRef: request.Session, PlanRef: request.Plan,
		ContractRef: ArtifactRef{Path: request.Contract.Path, SHA256: request.Contract.SHA256}, ResultRef: request.Result,
		ChangesetRef: request.Changeset, ChangeImpactRef: request.ChangeImpact, TargetedReverificationRefs: append([]ArtifactRef(nil), request.TargetedReverifications...),
		NextAction: request.NextAction, HandedOffBy: request.HandedOffBy, HandedOffAt: nowOr(request.OccurredAt),
	}
	ref, err := writeImmutable(root, filepath.Join(artifactRoot, "handoffs", handoff.HandoffID+".json"), "repair-handoff.schema.json", handoff)
	return handoff, ref, err
}

func ValidateRepairHandoff(root string, ref ArtifactRef) (RepairHandoff, error) {
	var value RepairHandoff
	if err := decodeArtifact(root, ref, "repair-handoff.schema.json", &value); err != nil {
		return RepairHandoff{}, err
	}
	check, err := checkHandoffRefs(root, value)
	if err != nil {
		return RepairHandoff{}, err
	}
	if !check.Complete {
		return RepairHandoff{}, fmt.Errorf("RepairHandoff incomplete: missing=%s invalid=%s", strings.Join(check.Missing, ", "), strings.Join(check.Invalid, ", "))
	}
	return value, nil
}

func CheckRepairHandoff(root string, handoff RepairHandoff) (HandoffCompleteness, error) {
	return checkHandoffRefs(root, handoff)
}

func checkHandoffRefs(root string, handoff RepairHandoff) (HandoffCompleteness, error) {
	missing := []string{}
	if handoff.SessionRef.Path == "" {
		missing = append(missing, "session")
	}
	if handoff.PlanRef.Path == "" {
		missing = append(missing, "plan")
	}
	if handoff.ContractRef.Path == "" {
		missing = append(missing, "contract")
	}
	if handoff.ResultRef.Path == "" {
		missing = append(missing, "result")
	}
	if handoff.ChangesetRef.Path == "" {
		missing = append(missing, "changeset")
	}
	if handoff.ChangeImpactRef.Path == "" {
		missing = append(missing, "change_impact")
	}
	if len(handoff.TargetedReverificationRefs) == 0 {
		missing = append(missing, "targeted_reverification")
	}
	if strings.TrimSpace(handoff.NextAction) == "" {
		missing = append(missing, "next_action")
	}
	if strings.TrimSpace(handoff.HandedOffBy) == "" {
		missing = append(missing, "handed_off_by")
	}
	if len(missing) > 0 {
		return HandoffCompleteness{Complete: false, Missing: missing}, nil
	}
	invalid := []string{}
	contract, contractErr := ValidateApprovedContractRef(root, ContractRef{Path: handoff.ContractRef.Path, SHA256: handoff.ContractRef.SHA256})
	if contractErr != nil {
		invalid = append(invalid, "contract: "+contractErr.Error())
	}
	session, sessionErr := ValidateRepairSession(root, handoff.SessionRef)
	if sessionErr != nil {
		invalid = append(invalid, "session: "+sessionErr.Error())
	}
	plan, planErr := ValidateRepairPlan(root, handoff.PlanRef)
	if planErr != nil {
		invalid = append(invalid, "plan: "+planErr.Error())
	}
	result, resultErr := ValidateRepairResult(root, handoff.ResultRef)
	if resultErr != nil {
		invalid = append(invalid, "result: "+resultErr.Error())
	}
	changeset, changesetErr := ValidateChangeset(root, handoff.ChangesetRef)
	if changesetErr != nil {
		invalid = append(invalid, "changeset: "+changesetErr.Error())
	}
	impact, impactErr := ValidateChangeImpact(root, handoff.ChangeImpactRef)
	if impactErr != nil {
		invalid = append(invalid, "change_impact: "+impactErr.Error())
	}
	for index, reference := range handoff.TargetedReverificationRefs {
		target, targetErr := ValidateTargetedReverification(root, reference)
		if targetErr != nil {
			invalid = append(invalid, fmt.Sprintf("targeted_reverification[%d]: %v", index, targetErr))
			continue
		}
		if impactErr == nil && target.ImpactID != impact.ImpactID {
			invalid = append(invalid, fmt.Sprintf("targeted_reverification[%d]: impact_id %q does not match %q", index, target.ImpactID, impact.ImpactID))
		}
	}
	if contractErr == nil && sessionErr == nil && (session.ContractRef != contract.Ref.Path || session.ContractSHA256 != contract.Ref.SHA256) {
		invalid = append(invalid, "session is not bound to the approved Contract")
	}
	if sessionErr == nil && planErr == nil && (plan.SessionID != session.SessionID || plan.ContractID != contract.ContractID) {
		invalid = append(invalid, "plan is not bound to the Session and Contract")
	}
	if sessionErr == nil && planErr == nil && resultErr == nil && (result.SessionID != session.SessionID || result.PlanID != plan.PlanID || result.ContractID != contract.ContractID) {
		invalid = append(invalid, "result is not bound to the Session, Plan, and Contract")
	}
	if sessionErr == nil && changesetErr == nil && changeset.SessionID != session.SessionID {
		invalid = append(invalid, "changeset is not bound to the Session")
	}
	if resultErr == nil && changesetErr == nil {
		if err := exactChangedArtifactSet(result.ChangedArtifacts, changeset.Artifacts, "RepairResult", "Changeset"); err != nil {
			invalid = append(invalid, err.Error())
		}
	}
	if resultErr == nil && impactErr == nil {
		if err := exactChangedArtifactSet(result.ChangedArtifacts, impact.ChangedArtifacts, "RepairResult", "ChangeImpact"); err != nil {
			invalid = append(invalid, err.Error())
		}
	}
	return HandoffCompleteness{Complete: len(invalid) == 0, Invalid: invalid}, nil
}
