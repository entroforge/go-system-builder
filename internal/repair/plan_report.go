package repair

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// PlanReportRequest is the immutable input envelope for one Assignment's
// pre-write plan and red-check report.
type PlanReportRequest struct {
	Session       ArtifactRef   `json:"session_ref"`
	Plan          ArtifactRef   `json:"plan_ref"`
	AssignmentID  string        `json:"assignment_id"`
	AssertionIDs  []string      `json:"assertion_ids"`
	AgentID       string        `json:"agent_id"`
	ReportID      string        `json:"report_id"`
	PlanText      string        `json:"plan"`
	RedChecks     []RepairCheck `json:"red_checks"`
	ProposedPaths []string      `json:"proposed_paths"`
	OccurredAt    time.Time     `json:"occurred_at"`
}

// CreatePlanReport persists the mandatory pre-write report. At least one red
// check must fail (or be explicitly blocked); a green-only report cannot
// authorize implementation because it has not demonstrated the original
// failure.
func CreatePlanReport(root string, request PlanReportRequest) (PlanReport, ArtifactRef, error) {
	if !strings.HasPrefix(request.ReportID, "repair-plan-report-") {
		return PlanReport{}, ArtifactRef{}, fmt.Errorf("request.ReportID must carry the repair-plan-report- prefix so Runtime can bind it (got %q)", request.ReportID)
	}
	if strings.TrimSpace(request.AssignmentID) == "" || strings.TrimSpace(request.AgentID) == "" || strings.TrimSpace(request.PlanText) == "" {
		return PlanReport{}, ArtifactRef{}, errors.New("assignment_id, agent_id, and plan are required")
	}
	session, err := ValidateRepairSession(root, request.Session)
	if err != nil {
		return PlanReport{}, ArtifactRef{}, err
	}
	plan, err := ValidateRepairPlan(root, request.Plan)
	if err != nil {
		return PlanReport{}, ArtifactRef{}, err
	}
	if plan.SessionID != session.SessionID {
		return PlanReport{}, ArtifactRef{}, errors.New("PlanReport is not bound to the supplied Session")
	}
	assignment, ok := assignmentByID(plan.Assignments, request.AssignmentID)
	if !ok {
		return PlanReport{}, ArtifactRef{}, fmt.Errorf("assignment %q is not part of RepairPlan", request.AssignmentID)
	}
	if len(request.RedChecks) == 0 {
		return PlanReport{}, ArtifactRef{}, errors.New("PlanReport requires at least one red check")
	}
	failed := false
	for _, check := range request.RedChecks {
		if strings.TrimSpace(check.Name) == "" || strings.TrimSpace(check.Command) == "" || len(check.EvidenceRefs) == 0 {
			return PlanReport{}, ArtifactRef{}, errors.New("every red check requires name, command, and evidence_refs")
		}
		// RC-14 (S9-L3): red evidence_ref must be an evidence anchor, not a bare prose token.
		// At this artifact boundary the red check must cite at least one
		// execution-anchor (scheme://) reference — a bare prose token or an
		// evidence/... file path is no longer accepted because the bare
		// non-empty loophole let a red verdict authorize implementation
		// without a verifiable failure trace. Heavy execution replay is left
		// to the S9 commit boundary; the artifact boundary enforces shape.
		anchored := false
		for _, ref := range check.EvidenceRefs {
			if strings.TrimSpace(ref) == "" {
				continue
			}
			if strings.Contains(strings.TrimSpace(ref), "://") {
				anchored = true
				break
			}
		}
		if !anchored {
			return PlanReport{}, ArtifactRef{}, fmt.Errorf("red check %q evidence_refs must contain at least one execution-anchor (test://, runtime://, etc.); a red verdict without a verifiable failure trace cannot authorize implementation", check.Name)
		}
		if check.Result == "fail" || check.Result == "blocked" {
			failed = true
		}
	}
	if !failed {
		return PlanReport{}, ArtifactRef{}, errors.New("PlanReport red_checks must contain a fail or blocked result before implementation writes")
	}
	assertionIDs := append([]string(nil), request.AssertionIDs...)
	if len(assertionIDs) == 0 {
		assertionIDs = append(assertionIDs, assignment.AssertionIDs...)
	}
	if len(assertionIDs) == 0 {
		return PlanReport{}, ArtifactRef{}, fmt.Errorf("PlanReport must carry the derived unit-to-assertion map from RepairPlan")
	}
	if err := exactIDs(assignment.AssertionIDs, assertionIDs); err != nil {
		return PlanReport{}, ArtifactRef{}, fmt.Errorf("PlanReport assertion coverage does not match Assignment %s: %w", assignment.AssignmentID, err)
	}
	paths := append([]string(nil), request.ProposedPaths...)
	if len(paths) == 0 {
		paths = append(paths, assignment.Scope...)
	}
	for i := range paths {
		paths[i] = normalizePath(paths[i])
		if err := scopeAllows(paths[i], plan.ProspectiveScope, plan.ForbiddenScope); err != nil {
			return PlanReport{}, ArtifactRef{}, err
		}
		assignmentScoped := false
		for _, rule := range assignment.Scope {
			if pathMatches(paths[i], rule) {
				assignmentScoped = true
				break
			}
		}
		if !assignmentScoped {
			return PlanReport{}, ArtifactRef{}, fmt.Errorf("proposed path %q is outside RepairAssignment %s scope", paths[i], assignment.AssignmentID)
		}
	}
	report := PlanReport{SchemaVersion: "1.0.0", RecordType: "repair_plan_report", ReportID: request.ReportID, SessionID: session.SessionID, PlanID: plan.PlanID, AssignmentID: assignment.AssignmentID, AssertionIDs: assertionIDs, AgentID: request.AgentID, Plan: request.PlanText, RedChecks: append([]RepairCheck(nil), request.RedChecks...), ProposedPaths: paths, Status: "reported", ReportedAt: nowOr(request.OccurredAt)}
	ref, err := writeImmutable(root, filepath.Join(artifactRoot, "plan-reports", request.ReportID+".json"), "repair-plan-report.schema.json", report)
	return report, ref, err
}

func ValidatePlanReport(root string, ref ArtifactRef) (PlanReport, error) {
	var report PlanReport
	if err := decodeArtifact(root, ref, "repair-plan-report.schema.json", &report); err != nil {
		return PlanReport{}, err
	}
	return report, nil
}

func assignmentByID(assignments []RepairAssignment, id string) (RepairAssignment, bool) {
	for _, assignment := range assignments {
		if assignment.AssignmentID == id {
			return assignment, true
		}
	}
	return RepairAssignment{}, false
}
