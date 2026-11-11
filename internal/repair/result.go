package repair

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

func SubmitRepairResult(root string, request RepairResultRequest) (RepairResult, ArtifactRef, error) {
	if request.issuedFinal != nil {
		// The normal Main consumer already performed integration and checks.
		// Build this report from its known responsibility and actual proposal.
		b := request.issuedFinal.packet.Binding.Sources
		if request.ResultID == "" || request.AssignmentID == "" || request.ProducerAgentID == "" {
			return RepairResult{}, ArtifactRef{}, errors.New("Result requires its current record and responsibility")
		}
		if request.Result == "pass" {
			if len(request.UnitResults) == 0 || !allChecksPass(request.Checks) {
				return RepairResult{}, ArtifactRef{}, errors.New("passing Result requires actual unit results and completed Main checks")
			}
			for _, unit := range request.UnitResults {
				if unit.Status != "pass" {
					return RepairResult{}, ArtifactRef{}, errors.New("passing Result contains an unfinished unit")
				}
			}
		}
		document := RepairResult{
			SchemaVersion: "1.0.0", RecordType: "repair_result", ResultID: request.ResultID,
			SessionID: b.SessionID, PlanID: b.PlanID, ContractID: b.ContractID, BaselineGeneration: b.BaselineGeneration,
			ProducerAgentID: request.ProducerAgentID, AssignmentID: request.AssignmentID,
			BeforeFixChecks: append([]RepairCheck{}, request.BeforeFixChecks...), Checks: append([]RepairCheck{}, request.Checks...),
			UnitResults: append([]RepairUnitResult{}, request.UnitResults...), ChangedArtifacts: append([]ChangedArtifact{}, request.ChangedArtifacts...),
			ScopeDeviations: append([]string{}, request.ScopeDeviations...), MigrationRef: request.MigrationRef, RollbackRef: request.RollbackRef,
			ResidualRisks: append([]string{}, request.ResidualRisks...), Result: request.Result, SubmittedAt: nowOr(request.OccurredAt),
			Verdict: request.Verdict, BeforeFixResults: append([]RepairBeforeFixResult{}, request.BeforeFixResults...),
			DetectionGapArtifacts: append([]string{}, request.DetectionGapArtifacts...), EvidenceRefs: append([]string{}, request.EvidenceRefs...), Receipt: request.Receipt,
		}
		if request.PlanReport.Path != "" {
			document.PlanReportRef = &ArtifactRef{ID: request.PlanReport.ID, Path: request.PlanReport.Path}
		}
		if document.Receipt == nil {
			return RepairResult{}, ArtifactRef{}, errors.New("issued S9 Result is missing its original receipt")
		}
		receipt := *document.Receipt
		document.Receipt = &receipt
		ref, err := writeUniqueArtifactPrepared(root, responsibilityArtifactPath("results", request.ResultID), "repair-result.schema.json", func(path string) any {
			document.Receipt.ResultRef = path
			return document
		})
		if err == nil {
			request.Receipt.ResultRef = ref.Path
			request.issuedFinal.receipt.ResultRef = ref.Path
		}
		return document, ref, err

	}
	if request.Verdict != "" || request.Receipt != nil {
		if request.issuedFinal == nil || request.Receipt == nil || !reflect.DeepEqual(request.Receipt, &request.issuedFinal.receipt) {
			return RepairResult{}, ArtifactRef{}, errors.New("typed S9 RepairResult must enter through the private issued-result integration capability")
		}
	}
	contract, err := ValidateApprovedContractRef(root, request.Contract)
	if err != nil {
		return RepairResult{}, ArtifactRef{}, err
	}
	session, err := ValidateRepairSession(root, request.Session)
	if err != nil {
		return RepairResult{}, ArtifactRef{}, err
	}
	plan, err := ValidateRepairPlan(root, request.Plan)
	if err != nil {
		return RepairResult{}, ArtifactRef{}, err
	}
	if plan.SessionID != session.SessionID || plan.ContractID != contract.ContractID || plan.ContractRef != contract.Ref.Path {
		return RepairResult{}, ArtifactRef{}, fmt.Errorf("RepairPlan is not bound to the supplied Session and approved Contract")
	}
	if request.AssignmentID == "" && len(plan.Assignments) == 1 {
		request.AssignmentID = plan.Assignments[0].AssignmentID
	}
	assignment, ok := assignmentByID(plan.Assignments, request.AssignmentID)
	if !ok {
		return RepairResult{}, ArtifactRef{}, fmt.Errorf("RepairResult assignment %q is not part of RepairPlan", request.AssignmentID)
	}
	if request.PlanReport.Path != "" {
		report, reportErr := ValidatePlanReport(root, request.PlanReport)
		if reportErr != nil {
			return RepairResult{}, ArtifactRef{}, fmt.Errorf("validate RepairPlan report: %w", reportErr)
		}
		if report.SessionID != session.SessionID || report.PlanID != plan.PlanID || report.AssignmentID != assignment.AssignmentID {
			return RepairResult{}, ArtifactRef{}, errors.New("RepairResult PlanReport is not transitively bound to the Session, Plan, and Assignment")
		}
	}
	if !strings.HasPrefix(request.ResultID, "repair-result-") || strings.TrimSpace(request.ProducerAgentID) == "" {
		return RepairResult{}, ArtifactRef{}, fmt.Errorf("result_id and producer_agent_id are required")
	}
	unitIDs := make([]string, 0, len(request.UnitResults))
	allPass := true
	for _, result := range request.UnitResults {
		if result.Status != "pass" {
			allPass = false
		}
		unitIDs = append(unitIDs, result.UnitID)
	}
	expectedIDs := append([]string(nil), assignment.UnitIDs...)
	if err := exactIDs(expectedIDs, unitIDs); err != nil {
		return RepairResult{}, ArtifactRef{}, fmt.Errorf("RepairResult for Assignment %s must cover exactly its units: %w", assignment.AssignmentID, err)
	}
	resultValue := strings.TrimSpace(request.Result)
	if resultValue == "" {
		if allPass {
			resultValue = "pass"
		} else {
			resultValue = "fail"
		}
	}
	if resultValue == "pass" && !allPass {
		return RepairResult{}, ArtifactRef{}, fmt.Errorf("RepairResult result=pass requires every repair unit to pass")
	}
	changed := append([]ChangedArtifact{}, request.ChangedArtifacts...)
	seen := map[string]bool{}
	for index := range changed {
		changed[index].Path = normalizePath(changed[index].Path)
		if changed[index].Path == "." || strings.TrimSpace(changed[index].Path) == "" {
			return RepairResult{}, ArtifactRef{}, fmt.Errorf("changed artifact path is empty")
		}
		if seen[changed[index].Path] {
			return RepairResult{}, ArtifactRef{}, fmt.Errorf("duplicate changed artifact %q", changed[index].Path)
		}
		seen[changed[index].Path] = true
		if changed[index].Status == "" {
			changed[index].Status = "modified"
		}
		if err := scopeAllows(changed[index].Path, contract.ProspectiveScope, contract.ForbiddenScope); err != nil {
			return RepairResult{}, ArtifactRef{}, err
		}
		assignmentScoped := false
		for _, rule := range assignment.Scope {
			if pathMatches(changed[index].Path, rule) {
				assignmentScoped = true
				break
			}
		}
		if !assignmentScoped {
			return RepairResult{}, ArtifactRef{}, fmt.Errorf("changed artifact %q is outside RepairAssignment %s scope; return a scope deviation to S8/S9 planning", changed[index].Path, assignment.AssignmentID)
		}

	}
	beforeChecks := append([]RepairCheck{}, request.BeforeFixChecks...)
	checks := append([]RepairCheck{}, request.Checks...)
	scopeDeviations := append([]string{}, request.ScopeDeviations...)
	residualRisks := append([]string{}, request.ResidualRisks...)
	if len(beforeChecks) == 0 {
		return RepairResult{}, ArtifactRef{}, fmt.Errorf("RepairResult requires actual before_fix_checks records; reuse the recorded PlanReport prechecks")
	}
	if resultValue == "pass" && !allChecksPass(checks) {
		return RepairResult{}, ArtifactRef{}, fmt.Errorf("RepairResult result=pass requires non-empty checks and every check to pass")
	}
	document := RepairResult{
		SchemaVersion: "1.0.0", RecordType: "repair_result", ResultID: request.ResultID, SessionID: session.SessionID,
		PlanID: plan.PlanID, ContractID: contract.ContractID, BaselineGeneration: session.BaselineGeneration,
		ProducerAgentID: request.ProducerAgentID, AssignmentID: request.AssignmentID, BeforeFixChecks: beforeChecks, Checks: checks,
		UnitResults: append([]RepairUnitResult{}, request.UnitResults...), ChangedArtifacts: changed, ScopeDeviations: scopeDeviations,
		MigrationRef: request.MigrationRef, RollbackRef: request.RollbackRef, ResidualRisks: residualRisks, Result: resultValue, SubmittedAt: nowOr(request.OccurredAt),
		Verdict: request.Verdict, BeforeFixResults: append([]RepairBeforeFixResult{}, request.BeforeFixResults...),
		DetectionGapArtifacts: append([]string{}, request.DetectionGapArtifacts...), EvidenceRefs: append([]string{}, request.EvidenceRefs...),
		Receipt: request.Receipt,
	}
	if request.PlanReport.Path != "" {
		document.PlanReportRef = &ArtifactRef{ID: request.PlanReport.ID, Path: request.PlanReport.Path}
	}
	ref, err := writeImmutable(root, responsibilityArtifactPath("results", request.ResultID), "repair-result.schema.json", document)
	if err != nil {
		return RepairResult{}, ArtifactRef{}, err
	}
	return document, ref, nil
}

func ValidateRepairResult(root string, ref ArtifactRef) (RepairResult, error) {
	var document RepairResult
	if err := decodeArtifact(root, ref, "repair-result.schema.json", &document); err != nil {
		return RepairResult{}, err
	}
	if document.Result == "pass" {
		for _, unit := range document.UnitResults {
			if unit.Status != "pass" {
				return RepairResult{}, fmt.Errorf("pass RepairResult contains non-pass unit %q", unit.UnitID)
			}
		}
	}
	seen := map[string]bool{}
	for _, artifact := range document.ChangedArtifacts {
		path := normalizePath(artifact.Path)
		if path == "." {
			return RepairResult{}, fmt.Errorf("RepairResult changed artifact path is empty")
		}
		if seen[path] {
			return RepairResult{}, fmt.Errorf("RepairResult contains duplicate changed artifact %q", path)
		}
		seen[path] = true
	}
	return document, nil
}

func allChecksPass(checks []RepairCheck) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if check.Result != "pass" {
			return false
		}
	}
	return true
}

func changedArtifactsWithinActual(changed []ChangedArtifact, actual []ArtifactRef) error {
	actualSet, err := artifactRefSet(actual, "actual Session diff")
	if err != nil {
		return err
	}
	submittedSet, err := changedArtifactSet(changed, "submitted RepairResult")
	if err != nil {
		return err
	}
	missing := []string{}
	for path, value := range submittedSet {
		if !artifactSetEntryMatches(value, actualSet[path]) {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("submitted RepairResult contains artifacts outside the actual Session diff: %v", missing)
	}
	return nil
}
