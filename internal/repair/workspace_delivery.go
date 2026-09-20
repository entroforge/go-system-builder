package repair

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type WorkspaceAuthority struct {
	Assignment RepairAssignment
	Contract   ApprovedContract
	Session    RepairSession
	Plan       RepairPlan
	Inputs     []string
	Checks     []string
	Report     PlanReport
	ReportRef  ArtifactRef
}

// ResolveWorkspaceAuthority uses the same approved contract, ownership,
// dependency and resource-lock predicates as ordinary S9 result submission.
func ResolveWorkspaceAuthority(root string, state map[string]any, id, owner string) (WorkspaceAuthority, error) {
	var out WorkspaceAuthority
	p := repairPointer(state)
	if p == nil || stringField(p["status"]) != "repairing" {
		return out, fmt.Errorf("S9 Worker delivery requires repairing; submit plans and begin execution first")
	}
	planRef, err := pointerArtifact(p, "plan_ref", "plan_sha256", "RepairPlan")
	if err != nil {
		return out, err
	}
	out.Plan, err = ValidateRepairPlan(root, planRef)
	if err != nil {
		return out, err
	}
	canonical := canonicalPlanAssignmentID(out.Plan, id)
	var ok bool
	out.Assignment, ok = assignmentByID(out.Plan.Assignments, canonical)
	if !ok {
		return out, fmt.Errorf("unknown S9 assignment %s", id)
	}
	if stringMapField(p["assignment_owners"])[canonical] != owner || owner == "" {
		return out, fmt.Errorf("S9 assignment owner mismatch")
	}
	if err = validateRepairAssignmentReady(root, p, out.Plan, out.Assignment); err != nil {
		return out, err
	}
	out.Contract, err = ValidateApprovedContractRef(root, ContractRef{Path: stringField(p["contract_ref"]), SHA256: stringField(p["contract_sha256"])})
	if err != nil {
		return out, err
	}
	sessionRef, err := pointerArtifact(p, "path", "sha256", "RepairSession")
	if err != nil {
		return out, err
	}
	out.Session, err = ValidateRepairSession(root, sessionRef)
	if err != nil {
		return out, err
	}
	if out.Plan.SessionID != out.Session.SessionID || out.Plan.ContractSHA256 != out.Contract.Ref.SHA256 || out.Session.RuntimeID != workspace.RuntimeID(state) || out.Session.BaselineGeneration != workspace.Generation(state) {
		return out, fmt.Errorf("S9 workspace authority identity mismatch")
	}
	reportRef, ok := planReportForAssignment(root, p["plan_report_refs"], canonical)
	if !ok {
		return out, fmt.Errorf("S9 Workspace requires its recorded domain PlanReport")
	}
	report, err := ValidatePlanReport(root, reportRef)
	if err != nil {
		return out, err
	}
	if report.AgentID != owner || report.PlanID != out.Plan.PlanID || report.SessionID != out.Session.SessionID {
		return out, fmt.Errorf("S9 PlanReport identity mismatch")
	}
	out.Report, out.ReportRef = report, reportRef
	for _, check := range report.RedChecks {
		if strings.TrimSpace(check.Command) != "" {
			out.Checks = append(out.Checks, check.Command)
		}
	}
	if len(out.Checks) == 0 {
		return out, fmt.Errorf("S9 Worker requires executable checks from its recorded red-check plan")
	}
	out.Inputs = []string{planRef.Path, sessionRef.Path, out.Contract.Ref.Path, reportRef.Path}
	return out, nil
}

type WorkspaceCandidate struct {
	Version             int                 `json:"version"`
	RecordType          string              `json:"record_type"`
	RuntimeID           string              `json:"runtime_id"`
	BaselineGeneration  int                 `json:"baseline_generation"`
	ExecutionGeneration int                 `json:"execution_generation"`
	AssignmentID        string              `json:"assignment_id"`
	SourceHead          string              `json:"source_head"`
	BaseCommit          string              `json:"base_commit"`
	SessionID           string              `json:"session_id"`
	PlanID              string              `json:"plan_id"`
	ContractSHA256      string              `json:"contract_sha256"`
	Proposal            RepairResultRequest `json:"proposal"`
}

func CreateWorkspaceCandidate(ctx context.Context, root string, state map[string]any, e workspace.Execution, proposal RepairResultRequest) (WorkspaceCandidate, ArtifactRef, error) {
	a, err := ResolveWorkspaceAuthority(root, state, e.AssignmentID, e.AgentID)
	if err != nil {
		return WorkspaceCandidate{}, ArtifactRef{}, err
	}
	if proposal.ProducerAgentID != e.AgentID || canonicalPlanAssignmentID(a.Plan, proposal.AssignmentID) != a.Assignment.AssignmentID {
		return WorkspaceCandidate{}, ArtifactRef{}, fmt.Errorf("candidate proposal owner/assignment mismatch")
	}
	if !strings.HasPrefix(proposal.ResultID, "repair-result-") {
		return WorkspaceCandidate{}, ArtifactRef{}, fmt.Errorf("candidate needs a final RepairResult identity")
	}
	ids := []string{}
	for _, unit := range proposal.UnitResults {
		ids = append(ids, unit.UnitID)
	}
	if err = exactIDs(a.Assignment.UnitIDs, ids); err != nil {
		return WorkspaceCandidate{}, ArtifactRef{}, err
	}
	head, err := workspace.Git(ctx, e.Path, "rev-parse", "HEAD")
	if err != nil {
		return WorkspaceCandidate{}, ArtifactRef{}, err
	}
	status, err := workspace.Git(ctx, e.Path, "status", "--porcelain", "--untracked-files=all", "--", ".", ":(exclude).claude/submissions/**")
	if err != nil || status != "" {
		return WorkspaceCandidate{}, ArtifactRef{}, fmt.Errorf("candidate requires a clean committed Worker: %s %v", status, err)
	}
	paths, err := workspace.GitBytes(ctx, e.Path, "diff", "--no-renames", "--name-only", "-z", e.BaseCommit, head)
	if err != nil {
		return WorkspaceCandidate{}, ArtifactRef{}, err
	}
	changed := []ChangedArtifact{}
	for _, path := range strings.Split(string(paths), "\x00") {
		if path == "" {
			continue
		}
		if err = scopeAllows(path, a.Contract.ProspectiveScope, a.Contract.ForbiddenScope); err != nil {
			return WorkspaceCandidate{}, ArtifactRef{}, err
		}
		matched := false
		for _, scope := range a.Assignment.Scope {
			if pathMatches(path, scope) {
				matched = true
			}
		}
		if !matched {
			return WorkspaceCandidate{}, ArtifactRef{}, fmt.Errorf("candidate changed path outside domain assignment: %s", path)
		}
		kind := "modified"
		data, err := workspace.GitBytes(ctx, e.Path, "show", head+":"+path)
		if err != nil {
			kind = "deleted"
			data, err = workspace.GitBytes(ctx, e.Path, "show", e.BaseCommit+":"+path)
		} else if _, oldErr := workspace.GitBytes(ctx, e.Path, "show", e.BaseCommit+":"+path); oldErr != nil {
			kind = "added"
		}
		if err != nil {
			return WorkspaceCandidate{}, ArtifactRef{}, err
		}
		hash := sha256.Sum256(data)
		changed = append(changed, ChangedArtifact{Path: path, SHA256: hex.EncodeToString(hash[:]), Status: kind})
	}
	if len(changed) == 0 {
		return WorkspaceCandidate{}, ArtifactRef{}, fmt.Errorf("candidate has no source changes")
	}
	proposal.AssignmentID = a.Assignment.AssignmentID
	proposal.ChangedArtifacts = changed
	if err = validateCandidateProposal(a, &proposal); err != nil {
		return WorkspaceCandidate{}, ArtifactRef{}, err
	}
	c := WorkspaceCandidate{Version: 1, RecordType: "repair_workspace_candidate", RuntimeID: e.RuntimeID, BaselineGeneration: e.BaselineGeneration, ExecutionGeneration: e.Generation, AssignmentID: e.AssignmentID, SourceHead: head, BaseCommit: e.BaseCommit, SessionID: a.Session.SessionID, PlanID: a.Plan.PlanID, ContractSHA256: a.Contract.Ref.SHA256, Proposal: proposal}
	data, err := canonicalJSON(c)
	if err != nil {
		return c, ArtifactRef{}, err
	}
	key := sha256Bytes(data)
	refPath := artifactRoot + "/candidates/" + key + ".json"
	// Identical proposals reuse immutable bytes; no overwrite on retry.
	ref := fileRef(refPath, data)
	if existing, readErr := readArtifact(root, ref, "repair-workspace-candidate.schema.json"); readErr == nil && len(existing) > 0 {
		return c, ref, nil
	}
	ref, err = writeImmutable(root, refPath, "repair-workspace-candidate.schema.json", c)
	return c, ref, err
}

func ValidateWorkspaceCandidate(root string, state map[string]any, e workspace.Execution, ref ArtifactRef) (WorkspaceCandidate, error) {
	var c WorkspaceCandidate
	if err := decodeArtifact(root, ref, "repair-workspace-candidate.schema.json", &c); err != nil {
		return c, err
	}
	a, err := ResolveWorkspaceAuthority(root, state, e.AssignmentID, e.AgentID)
	if err != nil {
		return c, err
	}
	if c.RuntimeID != e.RuntimeID || c.BaselineGeneration != e.BaselineGeneration || c.ExecutionGeneration != e.Generation || c.AssignmentID != e.AssignmentID || c.BaseCommit != e.BaseCommit || c.SessionID != a.Session.SessionID || c.PlanID != a.Plan.PlanID || c.ContractSHA256 != a.Contract.Ref.SHA256 || c.Proposal.ProducerAgentID != e.AgentID {
		return c, fmt.Errorf("candidate no longer matches current S9 authority")
	}
	if err := validateCandidateProposal(a, &c.Proposal); err != nil {
		return c, err
	}
	return c, nil
}

func ReadWorkspaceCandidate(root string, ref ArtifactRef) (WorkspaceCandidate, error) {
	var c WorkspaceCandidate
	err := decodeArtifact(root, ref, "repair-workspace-candidate.schema.json", &c)
	return c, err
}

func RegisteredWorkspaceResult(root string, state map[string]any, c WorkspaceCandidate) (ArtifactRef, bool, error) {
	for _, ref := range existingArtifactRefs(repairPointer(state)["result_refs"]) {
		result, err := ValidateRepairResult(root, ref)
		if err != nil {
			return ArtifactRef{}, false, err
		}
		if result.ResultID == c.Proposal.ResultID {
			if result.Result != "pass" {
				return ArtifactRef{}, false, fmt.Errorf("registered candidate result is %s; preserve Worker and follow repair recovery", result.Result)
			}
			if result.AssignmentID != c.Proposal.AssignmentID || result.SessionID != c.SessionID || result.ProducerAgentID != c.Proposal.ProducerAgentID {
				return ArtifactRef{}, false, fmt.Errorf("candidate result identity collision")
			}
			return ref, true, nil
		}
	}
	return ArtifactRef{}, false, nil
}

// Validate the final result shape before a candidate may cause a merge. Main
// disk/Session checks still run after merge through the ordinary domain API.
func validateCandidateProposal(a WorkspaceAuthority, p *RepairResultRequest) error {
	if p.Result == "" {
		p.Result = "pass"
	}
	if p.Result != "pass" || !allChecksPass(p.Checks) || len(p.ScopeDeviations) > 0 {
		return fmt.Errorf("only passing, scoped candidates with non-empty passing checks may be integrated; report failures through domain recovery")
	}
	for _, u := range p.UnitResults {
		if u.Status != "pass" {
			return fmt.Errorf("candidate contains a non-passing repair unit")
		}
	}
	if len(p.BeforeFixChecks) == 0 {
		p.BeforeFixChecks = append([]RepairCheck{}, a.Report.RedChecks...)
	}
	if !hasFailedCheck(p.BeforeFixChecks) {
		return fmt.Errorf("candidate requires a recorded pre-fix failure")
	}
	if a.Contract.CompatibilityMigration != "" && (strings.TrimSpace(p.MigrationRef) == "" || strings.TrimSpace(p.RollbackRef) == "") {
		return fmt.Errorf("candidate requires contract migration and rollback references")
	}
	p.PlanReport = a.ReportRef
	document := RepairResult{SchemaVersion: "1.0.0", RecordType: "repair_result", ResultID: p.ResultID, SessionID: a.Session.SessionID, PlanID: a.Plan.PlanID, ContractID: a.Contract.ContractID, BaselineGeneration: a.Session.BaselineGeneration, ProducerAgentID: p.ProducerAgentID, AssignmentID: p.AssignmentID, BeforeFixChecks: append([]RepairCheck{}, p.BeforeFixChecks...), Checks: append([]RepairCheck{}, p.Checks...), UnitResults: append([]RepairUnitResult{}, p.UnitResults...), ChangedArtifacts: append([]ChangedArtifact{}, p.ChangedArtifacts...), ScopeDeviations: append([]string{}, p.ScopeDeviations...), ResidualRisks: append([]string{}, p.ResidualRisks...), MigrationRef: p.MigrationRef, RollbackRef: p.RollbackRef, Result: p.Result, SubmittedAt: "2000-01-01T00:00:00Z"}
	data, err := canonicalJSON(document)
	if err != nil {
		return err
	}
	return schema.NewEmbeddedValidator().ValidateBytes("repair-result.schema.json", data)
}
