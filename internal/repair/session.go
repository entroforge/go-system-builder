package repair

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func CreateRepairSession(root string, request SessionRequest) (RepairSession, ArtifactRef, error) {
	contract, err := ValidateApprovedContractRef(root, request.Contract)
	if err != nil {
		return RepairSession{}, ArtifactRef{}, err
	}
	if !strings.HasPrefix(request.SessionID, "repair-session-") {
		return RepairSession{}, ArtifactRef{}, fmt.Errorf("session_id must carry the repair-session- prefix so Runtime can bind it (got %q)", request.SessionID)
	}
	if strings.TrimSpace(request.RuntimeID) == "" || strings.TrimSpace(request.ReqID) == "" || strings.TrimSpace(request.CreatedBy) == "" {
		return RepairSession{}, ArtifactRef{}, fmt.Errorf("runtime_id, req_id, and created_by are required")
	}
	createdAt := nowOr(request.OccurredAt)
	baselineArtifacts, baselineDigest, err := captureRepositoryBaseline(root)
	if err != nil {
		return RepairSession{}, ArtifactRef{}, err
	}
	document := RepairSession{
		SchemaVersion: "1.0.0", RecordType: "repair_session", SessionID: request.SessionID,
		ContractRef: contract.Ref.Path, ContractSHA256: contract.Ref.SHA256, RuntimeID: request.RuntimeID,
		ReqID: request.ReqID, BaselineGeneration: request.BaselineGeneration, BaselineArtifacts: baselineArtifacts, BaselineDigest: baselineDigest,
		Status: "planned", CreatedBy: request.CreatedBy, CreatedAtText: createdAt,
	}
	ref, err := writeImmutable(root, artifactRoot+"/sessions/"+request.SessionID+".json", "repair-session.schema.json", document)
	if err != nil {
		return RepairSession{}, ArtifactRef{}, err
	}
	document.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return document, ref, nil
}

func ValidateRepairSession(root string, ref ArtifactRef) (RepairSession, error) {
	var document RepairSession
	if err := decodeArtifact(root, ref, "repair-session.schema.json", &document); err != nil {
		return RepairSession{}, err
	}
	if document.Status != "planned" && document.Status != "planning" && document.Status != "reproducing" && document.Status != "executing" && document.Status != "result_submitted" && document.Status != "handed_off" {
		return RepairSession{}, fmt.Errorf("RepairSession %q has invalid status %q", ref.Path, document.Status)
	}
	return document, nil
}

func CreateRepairPlan(root string, request PlanRequest) (RepairPlan, ArtifactRef, error) {
	contract, err := ValidateApprovedContractRef(root, request.Contract)
	if err != nil {
		return RepairPlan{}, ArtifactRef{}, err
	}
	session, err := ValidateRepairSession(root, request.Session)
	if err != nil {
		return RepairPlan{}, ArtifactRef{}, err
	}
	if session.ContractRef != contract.Ref.Path || session.ContractSHA256 != contract.Ref.SHA256 {
		return RepairPlan{}, ArtifactRef{}, fmt.Errorf("RepairSession is not bound to the approved Contract reference")
	}
	if !strings.HasPrefix(request.PlanID, "repair-plan-") {
		return RepairPlan{}, ArtifactRef{}, fmt.Errorf("plan_id must carry the repair-plan- prefix so Runtime can bind it (got %q)", request.PlanID)
	}
	if strings.TrimSpace(request.CreatedBy) == "" {
		return RepairPlan{}, ArtifactRef{}, fmt.Errorf("created_by is required")
	}
	document := RepairPlan{
		SchemaVersion: "1.0.0", RecordType: "repair_plan", PlanID: request.PlanID, SessionID: session.SessionID,
		ContractID: contract.ContractID, ContractRef: contract.Ref.Path, ContractSHA256: contract.Ref.SHA256,
		Units: append([]RepairUnit(nil), contract.Units...), ProspectiveScope: append([]string(nil), contract.ProspectiveScope...),
		ForbiddenScope: append([]string(nil), contract.ForbiddenScope...), ExecutionPolicy: "coverage_complete", CreatedBy: request.CreatedBy, CreatedAt: nowOr(request.OccurredAt),
	}
	assertionIDs, err := contractAssertionIDs(root, contract.Ref)
	if err != nil {
		return RepairPlan{}, ArtifactRef{}, err
	}
	for _, unit := range contract.Units {
		scope := contract.ProspectiveScope
		if len(unit.Scope) > 0 {
			scope = unit.Scope
		}
		// RC-09 (S9-12): assertion coverage must be declared, not inherited.
		// A unit with its own assertion_ids keeps exactly those; a unit
		// without them no longer silently receives the FULL contract
		// assertion surface — that implicit copy coarsened the reverification
		// surface (a one-file fix forced the verifier to re-prove every
		// assertion). The full surface is still reachable, but only through
		// the explicit declaration assertion_ids: ["all"] in the approved
		// RepairContract, which the planner expands here to the exact slot
		// list.
		unitAssertions := unit.AssertionIDs
		if len(unitAssertions) == 1 && unitAssertions[0] == "all" {
			unitAssertions = assertionIDs
		} else if len(unitAssertions) == 0 {
			return RepairPlan{}, ArtifactRef{}, fmt.Errorf(
				"RepairContract unit %s declares no assertion_ids; S9 requires an explicit per-unit assertion list (e.g. [\"symptom-1\"]) or the explicit full-surface declaration [\"all\"] — coverage is no longer silently copied from the whole Contract",
				unit.ID)
		}
		document.Assignments = append(document.Assignments, RepairAssignment{
			AssignmentID: "repair-assignment-" + unit.ID, UnitIDs: []string{unit.ID}, Status: "queued",
			AssertionIDs: append([]string(nil), unitAssertions...), DependsOn: append([]string(nil), unit.DependsOn...), ResourceLocks: append([]string(nil), unit.ResourceLocks...), Scope: append([]string(nil), scope...), ContractRef: contract.Ref.Path,
		})
	}
	if err := validateRepairPlanSemantics(root, document); err != nil {
		return RepairPlan{}, ArtifactRef{}, err
	}
	ref, err := writeImmutable(root, artifactRoot+"/plans/"+request.PlanID+".json", "repair-plan.schema.json", document)
	if err != nil {
		return RepairPlan{}, ArtifactRef{}, err
	}
	return document, ref, nil
}

func ValidateRepairPlan(root string, ref ArtifactRef) (RepairPlan, error) {
	var document RepairPlan
	if err := decodeArtifact(root, ref, "repair-plan.schema.json", &document); err != nil {
		return RepairPlan{}, err
	}
	if len(document.Units) == 0 {
		return RepairPlan{}, fmt.Errorf("RepairPlan %q has no repair units", ref.Path)
	}
	if err := validateRepairPlanSemantics(root, document); err != nil {
		return RepairPlan{}, err
	}
	return document, nil
}

func validateRepairPlanSemantics(root string, document RepairPlan) error {
	units := make(map[string]RepairUnit, len(document.Units))
	for _, unit := range document.Units {
		if strings.TrimSpace(unit.ID) == "" {
			return fmt.Errorf("RepairPlan contains a unit with an empty id")
		}
		if _, exists := units[unit.ID]; exists {
			return fmt.Errorf("RepairPlan contains duplicate unit id %q", unit.ID)
		}
		units[unit.ID] = unit
		for _, path := range unit.Scope {
			if err := scopeAllows(path, document.ProspectiveScope, document.ForbiddenScope); err != nil {
				return fmt.Errorf("RepairPlan unit %s scope: %w", unit.ID, err)
			}
		}
		for _, dependency := range unit.DependsOn {
			if dependency == unit.ID {
				return fmt.Errorf("RepairPlan dependency cycle: unit %s depends on itself", unit.ID)
			}
		}
	}
	for _, unit := range units {
		for _, dependency := range unit.DependsOn {
			if _, exists := units[dependency]; !exists {
				return fmt.Errorf("RepairPlan unit %s depends_on unknown unit %q", unit.ID, dependency)
			}
		}
	}
	if cycle := repairPlanDependencyCycle(units); cycle != "" {
		return fmt.Errorf("RepairPlan dependency cycle detected at %s; order or split the repair units before dispatch", cycle)
	}
	assignmentIDs := map[string]bool{}
	assignedUnits := map[string]string{}
	for _, assignment := range document.Assignments {
		if strings.TrimSpace(assignment.AssignmentID) == "" || assignment.AssignmentID == "repair-assignment-" {
			return fmt.Errorf("RepairPlan contains an Assignment with an empty assignment_id")
		}
		if assignmentIDs[assignment.AssignmentID] {
			return fmt.Errorf("RepairPlan contains duplicate assignment_id %q", assignment.AssignmentID)
		}
		assignmentIDs[assignment.AssignmentID] = true
		if assignment.ContractRef != document.ContractRef {
			return fmt.Errorf("RepairAssignment %s is not bound to RepairPlan contract_ref %s", assignment.AssignmentID, document.ContractRef)
		}
		expectedDependencies := []string{}
		expectedLocks := []string{}
		for _, unitID := range assignment.UnitIDs {
			unit, exists := units[unitID]
			if !exists {
				return fmt.Errorf("RepairAssignment %s references unknown unit %q", assignment.AssignmentID, unitID)
			}
			if prior := assignedUnits[unitID]; prior != "" {
				return fmt.Errorf("repair unit %s is assigned to both %s and %s", unitID, prior, assignment.AssignmentID)
			}
			assignedUnits[unitID] = assignment.AssignmentID
			expectedDependencies = append(expectedDependencies, unit.DependsOn...)
			expectedLocks = append(expectedLocks, unit.ResourceLocks...)
			// RC-09 (S9-12): a unit declaring assertion_ids: ["all"] is the
			// explicit full-surface form; the Assignment already carries the
			// expanded slot list, so compare against that expansion instead
			// of the literal "all" token.
			expectedAssertions := unit.AssertionIDs
			if len(expectedAssertions) == 1 && expectedAssertions[0] == "all" {
				expectedAssertions = assignment.AssertionIDs
			}
			if err := exactIDs(expectedAssertions, assignment.AssertionIDs); err != nil && len(expectedAssertions) > 0 {
				return fmt.Errorf("RepairAssignment %s assertion coverage: %w", assignment.AssignmentID, err)
			}
			for _, path := range assignment.Scope {
				if err := scopeAllows(path, document.ProspectiveScope, document.ForbiddenScope); err != nil {
					return fmt.Errorf("RepairAssignment %s scope: %w", assignment.AssignmentID, err)
				}
			}
		}
		if err := exactIDs(expectedDependencies, assignment.DependsOn); err != nil && (len(expectedDependencies) > 0 || len(assignment.DependsOn) > 0) {
			return fmt.Errorf("RepairAssignment %s dependency coverage: %w; copy every unit depends_on entry into the Assignment", assignment.AssignmentID, err)
		}
		if err := exactStringSet(expectedLocks, assignment.ResourceLocks, "resource-lock coverage"); err != nil && (len(expectedLocks) > 0 || len(assignment.ResourceLocks) > 0) {
			return fmt.Errorf("RepairAssignment %s %w; copy every unit resource_locks entry into the Assignment", assignment.AssignmentID, err)
		}
		for _, dependency := range assignment.DependsOn {
			if _, exists := units[dependency]; !exists {
				return fmt.Errorf("RepairAssignment %s depends_on unknown unit %q", assignment.AssignmentID, dependency)
			}
		}
	}
	missingUnits := []string{}
	for unitID := range units {
		if assignedUnits[unitID] == "" {
			missingUnits = append(missingUnits, unitID)
		}
	}
	if len(missingUnits) > 0 {
		return fmt.Errorf("RepairPlan has unassigned repair units: %s", strings.Join(missingUnits, ", "))
	}
	return nil
}

func exactStringSet(expected, actual []string, label string) error {
	expectedSet, actualSet := map[string]bool{}, map[string]bool{}
	for _, value := range expected {
		if expectedSet[value] {
			return fmt.Errorf("%s has duplicate expected value %q", label, value)
		}
		expectedSet[value] = true
	}
	for _, value := range actual {
		if actualSet[value] {
			return fmt.Errorf("%s has duplicate submitted value %q", label, value)
		}
		actualSet[value] = true
	}
	missing, extra := []string{}, []string{}
	for value := range expectedSet {
		if !actualSet[value] {
			missing = append(missing, value)
		}
	}
	for value := range actualSet {
		if !expectedSet[value] {
			extra = append(extra, value)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		sort.Strings(missing)
		sort.Strings(extra)
		return fmt.Errorf("%s failed: missing=%v extra=%v", label, missing, extra)
	}
	return nil
}

func repairPlanDependencyCycle(units map[string]RepairUnit) string {
	state := map[string]uint8{}
	var visit func(string) string
	visit = func(id string) string {
		if state[id] == 1 {
			return id
		}
		if state[id] == 2 {
			return ""
		}
		state[id] = 1
		for _, dependency := range units[id].DependsOn {
			if cycle := visit(dependency); cycle != "" {
				return cycle
			}
		}
		state[id] = 2
		return ""
	}
	for id := range units {
		if cycle := visit(id); cycle != "" {
			return cycle
		}
	}
	return ""
}
