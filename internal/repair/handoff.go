package repair

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	if request.Session.Path == "" || request.Plan.Path == "" || request.Contract.Path == "" || request.ChangeImpact.Path == "" {
		return RepairHandoff{}, ArtifactRef{}, errors.New("handoff requires session, plan, contract, and change-impact references")
	}
	handoff := RepairHandoff{
		SchemaVersion: "1.0.0", RecordType: "repair_handoff", HandoffID: request.HandoffID,
		SessionRef: ArtifactRef{Path: request.Session.Path}, PlanRef: ArtifactRef{Path: request.Plan.Path},
		ContractRef: ArtifactRef{Path: request.Contract.Path}, ResultRef: ArtifactRef{Path: request.Result.Path},
		ChangesetRef: ArtifactRef{Path: request.Changeset.Path}, ChangeImpactRef: ArtifactRef{Path: request.ChangeImpact.Path}, TargetedReverificationRefs: handoffPaths(request.TargetedReverifications),
		NextAction: request.NextAction, HandedOffBy: request.HandedOffBy, HandedOffAt: nowOr(request.OccurredAt),
	}
	ref, err := writeImmutable(root, responsibilityArtifactPath("handoffs", handoff.HandoffID), "repair-handoff.schema.json", handoff)
	return handoff, ref, err
}

func ValidateRepairHandoff(root string, ref ArtifactRef) (RepairHandoff, error) {
	var value RepairHandoff
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
	if err != nil {
		return RepairHandoff{}, err
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return RepairHandoff{}, err
	}
	if value.HandoffID == "" || value.SessionRef.Path == "" || strings.TrimSpace(value.NextAction) == "" {
		return RepairHandoff{}, errors.New("Handoff requires its current session, record identity and next action")
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
	contract, contractErr := ValidateApprovedContractRef(root, ContractRef{Path: handoff.ContractRef.Path})
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
	var result RepairResult
	var resultErr error
	if handoff.ResultRef.Path != "" {
		result, resultErr = ValidateRepairResult(root, handoff.ResultRef)
		if resultErr != nil {
			invalid = append(invalid, "result: "+resultErr.Error())
		}
	}
	var changeset Changeset
	var changesetErr error
	if handoff.ChangesetRef.Path != "" {
		changeset, changesetErr = ValidateChangeset(root, handoff.ChangesetRef)
		if changesetErr != nil {
			invalid = append(invalid, "changeset: "+changesetErr.Error())
		}
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
	if contractErr == nil && sessionErr == nil && session.ContractRef != contract.Ref.Path {
		invalid = append(invalid, "session is not bound to the approved Contract")
	}
	if sessionErr == nil && planErr == nil && (plan.SessionID != session.SessionID || plan.ContractID != contract.ContractID) {
		invalid = append(invalid, "plan is not bound to the Session and Contract")
	}
	if handoff.ResultRef.Path != "" && sessionErr == nil && planErr == nil && resultErr == nil && (result.SessionID != session.SessionID || result.PlanID != plan.PlanID || result.ContractID != contract.ContractID) {
		invalid = append(invalid, "result is not bound to the Session, Plan, and Contract")
	}
	if handoff.ChangesetRef.Path != "" && sessionErr == nil && changesetErr == nil && changeset.SessionID != session.SessionID {
		invalid = append(invalid, "changeset is not bound to the Session")
	}
	// RC-15: with a multi-Assignment plan the handoff's singular ResultRef is
	// only a compatibility pointer; the result_refs batch union is the exact
	// authority and is enforced at commit (validateHandoffAgainstCurrentRepair).
	// Here the cited result merely has to sit inside the repaired surface.
	if handoff.ResultRef.Path != "" && handoff.ChangesetRef.Path != "" && resultErr == nil && changesetErr == nil {
		if err := changedArtifactsWithinActual(result.ChangedArtifacts, changeset.Artifacts); err != nil {
			invalid = append(invalid, err.Error())
		}
	}
	if handoff.ResultRef.Path != "" && resultErr == nil && impactErr == nil {
		if err := changedArtifactsWithinActual(result.ChangedArtifacts, impact.ChangedArtifacts); err != nil {
			invalid = append(invalid, err.Error())
		}
	}
	return HandoffCompleteness{Complete: len(invalid) == 0, Invalid: invalid}, nil
}

// handoffPaths carries original accepted paths without copying historical metadata.
func handoffPaths(refs []ArtifactRef) []ArtifactRef {
	out := make([]ArtifactRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ArtifactRef{Path: ref.Path})
	}
	return out
}
