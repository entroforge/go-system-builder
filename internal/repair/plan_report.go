package repair

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
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
	// issuedReceipt is a package-private projection supplied only by the
	// issued-packet adapter. It cannot be authored through the legacy direct
	// PlanReport request API.
	issuedReceipt *runtimepkg.RepairPlanReportReceipt
}

// CreatePlanReport records the Assignment plan and actual prechecks.
func CreatePlanReport(root string, request PlanReportRequest) (PlanReport, ArtifactRef, error) {
	var session RepairSession
	var plan RepairPlan
	var assignment RepairAssignment
	assertionIDs := append([]string(nil), request.AssertionIDs...)
	var err error
	if request.issuedReceipt != nil {
		// The issued receiver already validated the professional answer. These
		// existing machine associations are supplied by its current packet.
		session.SessionID = request.issuedReceipt.SessionID
		plan.PlanID = request.issuedReceipt.PlanID
		assignment.AssignmentID = request.AssignmentID
	} else {
		if !strings.HasPrefix(request.ReportID, "repair-plan-report-") {
			return PlanReport{}, ArtifactRef{}, fmt.Errorf("request.ReportID must carry the repair-plan-report- prefix so Runtime can bind it (got %q)", request.ReportID)
		}
		if strings.TrimSpace(request.AssignmentID) == "" || strings.TrimSpace(request.AgentID) == "" || strings.TrimSpace(request.PlanText) == "" {
			return PlanReport{}, ArtifactRef{}, errors.New("assignment_id, agent_id, and plan are required")
		}
		session, err = ValidateRepairSession(root, request.Session)
		if err != nil {
			return PlanReport{}, ArtifactRef{}, err
		}
		plan, err = ValidateRepairPlan(root, request.Plan)
		if err != nil {
			return PlanReport{}, ArtifactRef{}, err
		}
		if plan.SessionID != session.SessionID {
			return PlanReport{}, ArtifactRef{}, errors.New("PlanReport is not bound to the supplied Session")
		}
		var ok bool
		assignment, ok = assignmentByID(plan.Assignments, request.AssignmentID)
		if !ok {
			return PlanReport{}, ArtifactRef{}, fmt.Errorf("assignment %q is not part of RepairPlan", request.AssignmentID)
		}
		if len(request.RedChecks) == 0 {
			return PlanReport{}, ArtifactRef{}, errors.New("PlanReport requires at least one red check")
		}
		for _, check := range request.RedChecks {
			if strings.TrimSpace(check.Name) == "" || strings.TrimSpace(check.Command) == "" || len(check.EvidenceRefs) == 0 {
				return PlanReport{}, ArtifactRef{}, errors.New("every red check requires name, command, and evidence_refs")
			}
			referenced := false
			for _, ref := range check.EvidenceRefs {
				if strings.TrimSpace(ref) != "" {
					referenced = true
					break
				}
			}
			if !referenced {
				return PlanReport{}, ArtifactRef{}, fmt.Errorf("red check %q requires a non-empty evidence reference", check.Name)
			}
		}
		assertionIDs = append([]string(nil), request.AssertionIDs...)
		if len(assertionIDs) == 0 {
			assertionIDs = append(assertionIDs, assignment.AssertionIDs...)
		}
		if len(assertionIDs) == 0 {
			return PlanReport{}, ArtifactRef{}, fmt.Errorf("PlanReport must carry the derived unit-to-assertion map from RepairPlan")
		}
		if err := exactIDs(assignment.AssertionIDs, assertionIDs); err != nil {
			return PlanReport{}, ArtifactRef{}, fmt.Errorf("PlanReport assertion coverage does not match Assignment %s: %w", assignment.AssignmentID, err)
		}
	}
	paths := append([]string{}, request.ProposedPaths...)
	for i := range paths {
		paths[i] = normalizePath(paths[i])

	}
	report := PlanReport{SchemaVersion: "1.0.0", RecordType: "repair_plan_report", ReportID: request.ReportID, SessionID: session.SessionID, PlanID: plan.PlanID, AssignmentID: assignment.AssignmentID, AssertionIDs: assertionIDs, AgentID: request.AgentID, Plan: request.PlanText, RedChecks: append([]RepairCheck(nil), request.RedChecks...), ProposedPaths: paths, Status: "reported", ReportedAt: nowOr(request.OccurredAt), Receipt: request.issuedReceipt}
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
