package repair

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/evidence"
	impactanalysis "github.com/entroforge/go-system-builder/internal/impact"
	reviewpkg "github.com/entroforge/go-system-builder/internal/review"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
)

type RuntimeRequest struct {
	ExpectedRevision int
	Actor            string
	OccurredAt       time.Time
}
type OpenSessionRequest struct {
	RuntimeRequest
	SessionID string
	CreatedBy string
	ReqID     string
}
type CompilePlanRequest struct {
	RuntimeRequest
	PlanID    string
	CreatedBy string
}
type SubmitPlanReportRequest struct {
	RuntimeRequest
	Report ArtifactRef
}
type BeginRepairExecutionRequest struct {
	RuntimeRequest
}
type SubmitResultRuntimeRequest struct {
	RuntimeRequest
	Result RepairResultRequest
}
type CommitImpactRequest struct {
	RuntimeRequest
	Impact ArtifactRef
}
type CommitTargetedRequest struct {
	RuntimeRequest
	Reverification ArtifactRef
}
type ResumeTargetedRequest struct {
	RuntimeRequest
	Reason string
}
type CommitHandoffRequest struct {
	RuntimeRequest
	Handoff ArtifactRef
}

// runtimeCommitRevision is an internal event/idempotency detail. A negative
// request means the normal single-writer path: use the snapshot read by the
// domain operation to name the commit, while the Writer assigns the actual
// next Runtime revision under its lock.
func runtimeCommitRevision(expected int, current runtimepkg.Snapshot) int {
	if expected >= 0 {
		return expected
	}
	return current.Revision
}

func updateRuntime(writer *runtimepkg.Store, expected int, mutation runtimepkg.Mutation) (runtimepkg.Snapshot, error) {
	if expected < 0 {
		return writer.UpdateCurrent(mutation)
	}
	return writer.Update(expected, mutation)
}

func OpenRepairSession(root, statePath, journalPath string, req OpenSessionRequest) (runtimepkg.Snapshot, RepairSession, ArtifactRef, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath)
	if err != nil {
		return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, err
	}
	if req.Actor == "" {
		req.Actor = req.CreatedBy
	}
	if req.CreatedBy == "" {
		req.CreatedBy = req.Actor
	}
	if req.SessionID == "" || req.CreatedBy == "" {
		return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, errors.New("S9 session open requires session_id and created_by")
	}
	if existing := repairPointer(current.State); existing != nil {
		if stringField(existing["session_id"]) != req.SessionID {
			return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, errors.New("an active S9 RepairSession already exists; inspect runtime repair status")
		}
		ref := ArtifactRef{ID: req.SessionID, Path: stringField(existing["path"]), SHA256: stringField(existing["sha256"])}
		var session RepairSession
		if err := decodeArtifact(root, ref, "repair-session.schema.json", &session); err != nil {
			return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, err
		}
		return current, session, ref, nil
	}
	if lifecycleState(current.State) != "bug_resolution" || lifecyclePhase(current.State) != "repair_readback" {
		return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, errors.New("S9 session open requires bug_resolution.repair_readback; consume the approved Contract first")
	}
	investigation := investigationPointer(current.State)
	if investigation == nil || stringField(investigation["status"]) != "contract_approved" {
		return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, errors.New("S9 session open requires review.investigation status=contract_approved")
	}
	contract, err := ValidateApprovedContractRef(root, ContractRef{Path: stringField(investigation["repair_contract_ref"]), SHA256: stringField(investigation["repair_contract_sha256"])})
	if err != nil {
		return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, err
	}
	reqID := req.ReqID
	if reqID == "" {
		reqID = boundReqID(current.State)
	}
	if reqID == "" {
		return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, errors.New("S9 session open requires bound REQ id")
	}
	session, ref, err := CreateRepairSession(root, SessionRequest{Contract: contract.Ref, SessionID: req.SessionID, RuntimeID: stringField(current.State["runtime_id"]), ReqID: reqID, BaselineGeneration: baselineGeneration(current.State), CreatedBy: req.CreatedBy, OccurredAt: req.OccurredAt})
	if err != nil {
		return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, err
	}
	at := occurred(req.OccurredAt)
	// RC-09 (S9-4): record the session's authority fingerprint. The digest is
	// the exact baseline the RepairSession captured; every later S9 checkpoint
	// re-captures it and blocks the commit when the repository drifted.
	_, authorityDigest, err := captureRepositoryBaseline(root)
	if err != nil {
		return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, err
	}
	fingerprints := stringMapField(repairPointer(current.State)["authority_fingerprint"])
	fingerprints[session.SessionID] = authorityDigest
	anyFingerprints := make(map[string]any, len(fingerprints))
	for sessionID, digest := range fingerprints {
		anyFingerprints[sessionID] = digest
	}
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	snapshot, err := updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-session-open-%s-r%d", req.SessionID, commitRevision+1), TransitionID: whitelistChecked("S9-SESSION-OPEN"), Event: "repair_session_opened", Actor: req.Actor, IdempotencyKey: fmt.Sprintf("runtime:s9:session-open:%s:%d", req.SessionID, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.SessionID, contract.ContractID}, From: cursor(current.State), To: cursor(current.State), RequestID: "s9-session-open", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-SESSION-OPEN", GateFingerprint: "sha256:s9-session-open-v1", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
		ensureObject(state, "review")["repair"] = map[string]any{"session_id": session.SessionID, "case_id": contract.CaseID, "contract_id": contract.ContractID, "contract_ref": contract.Ref.Path, "contract_sha256": contract.Ref.SHA256, "path": ref.Path, "sha256": ref.SHA256, "revision": 1, "status": "contract_ready", "authority_fingerprint": anyFingerprints, "updated_at": at.Format(time.RFC3339Nano), "next_action": "runtime repair plan compile --root <root> --plan-id <plan> --created-by <agent>"}
		return nil
	}})
	if err != nil {
		return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, cleanupStagedArtifact(writer, req.ExpectedRevision, ref, current.State, err)
	}
	return snapshot, session, ref, nil
}

func CompileRepairPlan(root, statePath, journalPath string, req CompilePlanRequest) (runtimepkg.Snapshot, RepairPlan, ArtifactRef, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath)
	if err != nil {
		return runtimepkg.Snapshot{}, RepairPlan{}, ArtifactRef{}, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return runtimepkg.Snapshot{}, RepairPlan{}, ArtifactRef{}, err
	}
	p := repairPointer(current.State)
	if p == nil || stringField(p["status"]) != "contract_ready" {
		return runtimepkg.Snapshot{}, RepairPlan{}, ArtifactRef{}, errors.New("S9 plan compile requires status=contract_ready; open a session first")
	}
	contractRef := ContractRef{Path: stringField(p["contract_ref"]), SHA256: stringField(p["contract_sha256"])}
	sessionRef := ArtifactRef{ID: stringField(p["session_id"]), Path: stringField(p["path"]), SHA256: stringField(p["sha256"])}
	plan, ref, err := CreateRepairPlan(root, PlanRequest{Contract: contractRef, Session: sessionRef, PlanID: req.PlanID, CreatedBy: req.CreatedBy, OccurredAt: req.OccurredAt})
	if err != nil {
		return runtimepkg.Snapshot{}, RepairPlan{}, ArtifactRef{}, err
	}
	at := occurred(req.OccurredAt)
	actor := req.Actor
	if actor == "" {
		actor = req.CreatedBy
	}
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	snapshot, err := updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-plan-%s-r%d", plan.PlanID, commitRevision+1), TransitionID: whitelistChecked("PTR-BUG-09"), Event: "repair_plan_compiled", Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:plan:%s:%d", plan.PlanID, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{plan.PlanID}, From: cursor(current.State), To: map[string]any{"state": "bug_resolution", "phase": "planning"}, RequestID: "s9-plan-compile", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-PLAN-COMPILE", GateFingerprint: "sha256:s9-plan-compile-v2", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
		updateRepairPointer(state, map[string]any{"plan_ref": ref.Path, "plan_sha256": ref.SHA256, "status": "planning", "updated_at": at.Format(time.RFC3339Nano), "next_action": "submit one PlanReport per repair assignment with a failing pre-fix check"})
		setLifecycle(state, "bug_resolution", "planning")
		return nil
	}})
	if err != nil {
		return runtimepkg.Snapshot{}, RepairPlan{}, ArtifactRef{}, cleanupStagedArtifact(writer, req.ExpectedRevision, ref, current.State, err)
	}
	return snapshot, plan, ref, nil
}

// SubmitRepairPlanReportToRuntime consumes the builder's pre-write plan. It
// does not authorize writes yet; BeginRepairExecution is the explicit second
// checkpoint that moves the Runtime from reproducing to repairing.
func SubmitRepairPlanReportToRuntime(root, statePath, journalPath string, req SubmitPlanReportRequest) (runtimepkg.Snapshot, PlanReport, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath)
	if err != nil {
		return runtimepkg.Snapshot{}, PlanReport{}, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair plan report"); err != nil {
		return runtimepkg.Snapshot{}, PlanReport{}, err
	}
	p := repairPointer(current.State)
	if p == nil || (stringField(p["status"]) != "planning" && stringField(p["status"]) != "reproducing") {
		return runtimepkg.Snapshot{}, PlanReport{}, errors.New("S9 PlanReport requires status=planning or reproducing; compile a RepairPlan first")
	}
	planRef, err := pointerArtifact(p, "plan_ref", "plan_sha256", "current RepairPlan")
	if err != nil {
		return runtimepkg.Snapshot{}, PlanReport{}, err
	}
	report, err := ValidatePlanReport(root, req.Report)
	if err != nil {
		return runtimepkg.Snapshot{}, PlanReport{}, err
	}
	plan, err := ValidateRepairPlan(root, planRef)
	if err != nil {
		return runtimepkg.Snapshot{}, PlanReport{}, err
	}
	if report.PlanID != plan.PlanID || report.SessionID != stringField(p["session_id"]) {
		return runtimepkg.Snapshot{}, PlanReport{}, errors.New("PlanReport is not bound to the current S9 Session and RepairPlan")
	}
	assignment, ok := assignmentByID(plan.Assignments, report.AssignmentID)
	if !ok {
		return runtimepkg.Snapshot{}, PlanReport{}, fmt.Errorf("PlanReport assignment %q is not in the current RepairPlan", report.AssignmentID)
	}
	if err := exactIDs(assignment.AssertionIDs, report.AssertionIDs); err != nil {
		return runtimepkg.Snapshot{}, PlanReport{}, fmt.Errorf("PlanReport assertion coverage does not match Assignment %s: %w", assignment.AssignmentID, err)
	}
	owners := stringMapField(p["assignment_owners"])
	if owner := owners[report.AssignmentID]; owner != "" && owner != report.AgentID {
		return runtimepkg.Snapshot{}, PlanReport{}, fmt.Errorf("RepairAssignment %s is already owned by Agent %s; do not replace ownership mid-session", report.AssignmentID, owner)
	}
	for _, existingRef := range existingArtifactRefs(p["plan_report_refs"]) {
		if existingReport, existingErr := ValidatePlanReport(root, existingRef); existingErr == nil && existingReport.AssignmentID == report.AssignmentID {
			if existingReport.AgentID != report.AgentID {
				return runtimepkg.Snapshot{}, PlanReport{}, fmt.Errorf("RepairAssignment %s already has a PlanReport from Agent %s", report.AssignmentID, existingReport.AgentID)
			}
			if existingRef.Path != req.Report.Path || existingRef.SHA256 != req.Report.SHA256 {
				return runtimepkg.Snapshot{}, PlanReport{}, fmt.Errorf("RepairAssignment %s already has a PlanReport; revise the existing plan before resubmitting", report.AssignmentID)
			}
		}
	}
	at := occurred(req.OccurredAt)
	actor := req.Actor
	if actor == "" {
		actor = report.AgentID
	}
	reportRefs := appendArtifactRefFromPointer(p["plan_report_refs"], req.Report)
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	snapshot, err := updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-plan-report-%s-r%d", report.ReportID, commitRevision+1), TransitionID: whitelistChecked("PTR-BUG-10"), Event: "repair_plan_reported", Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:plan-report:%s:%d", report.ReportID, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.Report.Path}, From: cursor(current.State), To: map[string]any{"state": "bug_resolution", "phase": "reproducing"}, RequestID: "s9-plan-report", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-PLAN-REPORT", GateFingerprint: "sha256:s9-plan-report-v1", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
		owners := stringMapField(stateRepairPointer(state)["assignment_owners"])
		owners[report.AssignmentID] = report.AgentID
		updateRepairPointer(state, map[string]any{"plan_report_ref": req.Report.Path, "plan_report_sha256": req.Report.SHA256, "plan_report_refs": reportRefs, "assignment_owners": owners, "assignment_id": report.AssignmentID, "status": "reproducing", "updated_at": at.Format(time.RFC3339Nano), "next_action": "submit PlanReport for every repair assignment, then begin repair execution"})
		setLifecycle(state, "bug_resolution", "reproducing")
		return nil
	}})
	if err != nil {
		return runtimepkg.Snapshot{}, PlanReport{}, cleanupStagedArtifact(writer, req.ExpectedRevision, req.Report, current.State, err)
	}
	return snapshot, report, err
}

// BeginRepairExecution is the write barrier between pre-fix reproduction and
// implementation. The plan report is immutable and must already be bound to
// the current Runtime pointer.
func BeginRepairExecution(root, statePath, journalPath string, req BeginRepairExecutionRequest) (runtimepkg.Snapshot, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair execution"); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	p := repairPointer(current.State)
	if p == nil || stringField(p["status"]) != "reproducing" {
		return runtimepkg.Snapshot{}, errors.New("S9 execution begin requires status=reproducing; submit a PlanReport first")
	}
	reportRef, err := pointerArtifact(p, "plan_report_ref", "plan_report_sha256", "current RepairPlan report")
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	planRef, err := pointerArtifact(p, "plan_ref", "plan_sha256", "current RepairPlan")
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	plan, err := ValidateRepairPlan(root, planRef)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	reports, err := artifactRefsFromAny(p["plan_report_refs"], "plan_report_refs")
	if err != nil {
		reports = []ArtifactRef{reportRef}
	}
	reportedAssignments := map[string]bool{}
	var firstReport PlanReport
	for index, ref := range reports {
		report, reportErr := ValidatePlanReport(root, ref)
		if reportErr != nil {
			return runtimepkg.Snapshot{}, fmt.Errorf("PlanReport[%d] is invalid: %w", index, reportErr)
		}
		if firstReport.ReportID == "" {
			firstReport = report
		}
		reportedAssignments[report.AssignmentID] = true
	}
	missingAssignments := []string{}
	for _, assignment := range plan.Assignments {
		if !reportedAssignments[assignment.AssignmentID] {
			missingAssignments = append(missingAssignments, assignment.AssignmentID)
		}
	}
	if len(missingAssignments) > 0 {
		return runtimepkg.Snapshot{}, fmt.Errorf("S9 execution begin is blocked: missing PlanReport for assignments %v; submit one report per assignment", missingAssignments)
	}
	report := firstReport
	at := occurred(req.OccurredAt)
	actor := req.Actor
	if actor == "" {
		actor = report.AgentID
	}
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	return updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-execution-begin-r%d", commitRevision+1), TransitionID: whitelistChecked("PTR-BUG-11"), Event: "repair_execution_started", Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:execution:%s:%d", report.ReportID, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{reportRef.Path}, From: cursor(current.State), To: map[string]any{"state": "bug_resolution", "phase": "fixing"}, RequestID: "s9-execution-begin", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-EXECUTION-BEGIN", GateFingerprint: "sha256:s9-execution-begin-v1", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
		updateRepairPointer(state, map[string]any{"status": "repairing", "updated_at": at.Format(time.RFC3339Nano), "next_action": "continue the already-dispatched Builder(s) within their bound scope; submit one exact-unit RepairResult per Assignment"})
		setLifecycle(state, "bug_resolution", "fixing")
		return nil
	}})
}

func SubmitRepairResultToRuntime(root, statePath, journalPath string, req SubmitResultRuntimeRequest) (runtimepkg.Snapshot, RepairResult, ArtifactRef, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath)
	if err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
	}
	p := repairPointer(current.State)
	if p == nil || stringField(p["status"]) != "repairing" {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, errors.New("S9 result submit requires status=repairing; compile a plan first")
	}
	req.Result.Contract = ContractRef{Path: stringField(p["contract_ref"]), SHA256: stringField(p["contract_sha256"])}
	req.Result.Session = ArtifactRef{ID: stringField(p["session_id"]), Path: stringField(p["path"]), SHA256: stringField(p["sha256"])}
	req.Result.Plan = ArtifactRef{ID: stringField(p["plan_ref"]), Path: stringField(p["plan_ref"]), SHA256: stringField(p["plan_sha256"])}
	planRef, planErr := pointerArtifact(p, "plan_ref", "plan_sha256", "current RepairPlan")
	if planErr != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, planErr
	}
	plan, planErr := ValidateRepairPlan(root, planRef)
	if planErr != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, planErr
	}
	if req.Result.AssignmentID == "" {
		if len(plan.Assignments) != 1 {
			return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, errors.New("multi-Assignment S9 result submit requires assignment_id explicitly")
		}
		req.Result.AssignmentID = plan.Assignments[0].AssignmentID
	}
	assignment, assignmentOK := assignmentByID(plan.Assignments, req.Result.AssignmentID)
	if !assignmentOK {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, fmt.Errorf("RepairResult assignment %q is not in the current RepairPlan", req.Result.AssignmentID)
	}
	if err := validateRepairAssignmentReady(root, p, plan, assignment); err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
	}
	if reportRef, ok := planReportForAssignment(root, p["plan_report_refs"], req.Result.AssignmentID); ok {
		if req.Result.PlanReport.Path == "" || planReportAssignment(root, req.Result.PlanReport) != req.Result.AssignmentID {
			req.Result.PlanReport = reportRef
		}
	}
	if len(req.Result.BeforeFixChecks) == 0 && req.Result.PlanReport.Path != "" {
		if report, reportErr := ValidatePlanReport(root, req.Result.PlanReport); reportErr == nil {
			req.Result.BeforeFixChecks = append([]RepairCheck{}, report.RedChecks...)
		}
	}
	if owner := stringMapField(p["assignment_owners"])[req.Result.AssignmentID]; owner != "" && owner != req.Result.ProducerAgentID {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, fmt.Errorf("RepairResult producer %s does not own Assignment %s (owner=%s)", req.Result.ProducerAgentID, req.Result.AssignmentID, owner)
	}
	result, ref, err := SubmitRepairResult(root, req.Result)
	if err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
	}
	at := occurred(req.OccurredAt)
	actor := req.Actor
	if actor == "" {
		actor = result.ProducerAgentID
	}
	// RC-09 (S9-4): the result batch is the repair authority. If the
	// repository baseline drifted after the session opened, a passing result
	// describes code that no longer exists — block the submit. The result's
	// own changed artifacts are the only mutations the gate forgives.
	if err := checkS9AuthorityFreshness(root, p, result.ChangedArtifacts); err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
	}
	resultRefs := appendArtifactRefFromPointer(p["result_refs"], ref)
	planRef, planErr = pointerArtifact(p, "plan_ref", "plan_sha256", "current RepairPlan")
	if planErr != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, planErr
	}
	plan, planErr = ValidateRepairPlan(root, planRef)
	if planErr != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, planErr
	}
	nextStatus := "repairing"
	nextAction := "submit one exact-unit RepairResult for every RepairAssignment before committing ChangeImpact"
	if complete, allPass, missing, batchErr := repairResultBatchState(root, plan, resultRefs); batchErr != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, batchErr
	} else {
		nextStatus, nextAction = repairResultNextState(complete, allPass, missing)
	}
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	snapshot, err := updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-result-%s-r%d", result.ResultID, commitRevision+1), TransitionID: whitelistChecked("S9-RESULT-SUBMIT"), Event: "repair_result_submitted", Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:result:%s:%d", result.ResultID, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{result.ResultID}, From: cursor(current.State), To: cursor(current.State), RequestID: "s9-result-submit", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-RESULT-SUBMIT", GateFingerprint: "sha256:s9-result-submit-v1", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
		updateRepairPointer(state, map[string]any{"result_ref": ref.Path, "result_sha256": ref.SHA256, "result_refs": resultRefs, "status": nextStatus, "updated_at": at.Format(time.RFC3339Nano), "next_action": nextAction})
		return nil
	}})
	if err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, cleanupStagedArtifact(writer, req.ExpectedRevision, ref, current.State, err)
	}
	return snapshot, result, ref, nil
}

// validateRepairAssignmentReady is the small S9 scheduling bridge. The
// immutable RepairPlan owns the dependency and lock declarations; Runtime
// result/report refs provide the current progress. A result can be consumed
// only when all unit dependencies have passed and no reported sibling still
// holds one of the target's resource locks. This keeps queueing deterministic
// without introducing another scheduler or mutable plan authority.
func validateRepairAssignmentReady(root string, pointer map[string]any, plan RepairPlan, target RepairAssignment) error {
	unitOwners := map[string]RepairAssignment{}
	for _, assignment := range plan.Assignments {
		for _, unitID := range assignment.UnitIDs {
			unitOwners[unitID] = assignment
		}
	}
	results := map[string]RepairResult{}
	for _, ref := range existingArtifactRefs(pointer["result_refs"]) {
		result, err := ValidateRepairResult(root, ref)
		if err != nil {
			return fmt.Errorf("validate existing S9 Result %s before dispatch: %w", ref.Path, err)
		}
		results[result.AssignmentID] = result
	}
	if previous, ok := results[target.AssignmentID]; ok {
		return fmt.Errorf("RepairAssignment %s already has Result %s; do not submit a second result for the same Assignment", target.AssignmentID, previous.ResultID)
	}
	for _, dependencyUnit := range target.DependsOn {
		dependency, ok := unitOwners[dependencyUnit]
		if !ok {
			return fmt.Errorf("RepairAssignment %s is blocked by unknown dependency unit %s; repair the immutable RepairPlan before dispatch", target.AssignmentID, dependencyUnit)
		}
		result, ok := results[dependency.AssignmentID]
		if !ok || result.Result != "pass" {
			status := "no Result"
			if ok {
				status = "Result=" + result.Result
			}
			return fmt.Errorf("RepairAssignment %s is queued behind dependency %s (Assignment %s, %s); submit its passing RepairResult first, then retry the queued Assignment", target.AssignmentID, dependencyUnit, dependency.AssignmentID, status)
		}
	}
	targetLocks := make(map[string]bool, len(target.ResourceLocks))
	for _, lock := range target.ResourceLocks {
		if strings.TrimSpace(lock) != "" {
			targetLocks[lock] = true
		}
	}
	if len(targetLocks) == 0 {
		return nil
	}
	targetIndex := -1
	for index, assignment := range plan.Assignments {
		if assignment.AssignmentID == target.AssignmentID {
			targetIndex = index
			break
		}
	}
	for index, sibling := range plan.Assignments {
		if sibling.AssignmentID == target.AssignmentID {
			continue
		}
		// A shared lock is ordered by the immutable plan sequence. This keeps
		// two already-reported siblings from waiting on each other forever:
		// the first Assignment consumes its Result, then the next retry sees
		// the lock released. The plan remains the only scheduling authority.
		if targetIndex >= 0 && index > targetIndex {
			continue
		}
		if _, done := results[sibling.AssignmentID]; done {
			continue
		}
		reported := false
		for _, ref := range existingArtifactRefs(pointer["plan_report_refs"]) {
			if report, err := ValidatePlanReport(root, ref); err == nil && report.AssignmentID == sibling.AssignmentID {
				reported = true
				break
			}
		}
		if !reported {
			continue
		}
		for _, lock := range sibling.ResourceLocks {
			if targetLocks[lock] {
				return fmt.Errorf("RepairAssignment %s is queued: resource lock %q is held by Assignment %s; wait for that Result to be consumed, then retry", target.AssignmentID, lock, sibling.AssignmentID)
			}
		}
	}
	return nil
}

// repairResultNextState makes a non-pass result terminal for the current S9
// attempt. Waiting for unrelated assignments after one builder has already
// reported a blocker creates a false sense of progress and hides the route
// back to S8. A pass batch still requires every assignment before impact
// reconciliation can begin.
func repairResultNextState(complete, allPass bool, missing []string) (status, action string) {
	if !allPass {
		return "blocked", "a RepairResult is non-pass; inspect the recorded blocker and route the case to S8 for causal reassessment"
	}
	if complete {
		return "impact_reconciliation", "create the complete changeset and ChangeImpact, then commit impact"
	}
	if len(missing) > 0 {
		return "repairing", fmt.Sprintf("submit RepairResult for the remaining assignments: %s", strings.Join(missing, ", "))
	}
	return "repairing", "submit one exact-unit RepairResult for every RepairAssignment before committing ChangeImpact"
}

// checkS9AuthorityFreshness is the RC-09 (S9-4) stale-authority gate. At S9
// session open the repository baseline digest is recorded on the Runtime
// repair pointer as the session's authority fingerprint. Every later S9
// checkpoint re-captures the live repository baseline and compares it against
// the Session baseline: if a baseline path changed or disappeared without a
// committed RepairResult claiming it (and without the pending result claiming
// it), the implementation surface drifted after the session opened (upstream
// commit, hand edit, out-of-band write), the Session/Plan/Result chain no
// longer describes the code being repaired, and the commit is blocked until
// the case is re-baselined through S8/S9 planning.
//
// New paths that appear after session open are the repair's own output and
// are not drift; only silent mutation or deletion of a baseline path is.
// The digest comparison is exact-set over path:sha256 — not a heuristic like
// mtime or a git dirty flag.
func checkS9AuthorityFreshness(root string, pointer map[string]any, pending []ChangedArtifact) error {
	sessionRef, err := pointerArtifact(pointer, "path", "sha256", "current RepairSession")
	if err != nil {
		return err
	}
	session, err := ValidateRepairSession(root, sessionRef)
	if err != nil {
		return fmt.Errorf("current RepairSession is invalid: %w", err)
	}
	fingerprint := stringMapField(pointer["authority_fingerprint"])[session.SessionID]
	if fingerprint == "" {
		// RC-15 (S9-T2/L1): an empty fingerprint means the session carries no
		// authority claim at all — the gate cannot verify the repair surface,
		// so fail closed instead of silently passing. Sessions opened before
		// the fingerprint channel existed (legacy state) are released only
		// through the explicit migration escape hatch
		// LOOP_ALLOW_EMPTY_FINGERPRINT=1; a fresh session always records the
		// fingerprint at open (runtime.go OpenRepairSession).
		if os.Getenv("LOOP_ALLOW_EMPTY_FINGERPRINT") == "1" {
			return nil
		}
		return fmt.Errorf("RepairSession %s has no authority_fingerprint on the Runtime pointer; the S9 authority gate cannot verify the repair surface — re-baseline the case through S8/S9 planning (migration escape: LOOP_ALLOW_EMPTY_FINGERPRINT=1)", session.SessionID)
	}
	claimed, err := claimedRepairChanges(root, pointer, pending)
	if err != nil {
		return err
	}
	live, _, err := captureRepositoryBaseline(root)
	if err != nil {
		return err
	}
	liveByPath := make(map[string]string, len(live))
	for _, artifact := range live {
		liveByPath[artifact.Path] = artifact.SHA256
	}
	for _, artifact := range session.BaselineArtifacts {
		if claimed[artifact.Path] {
			continue
		}
		if liveByPath[artifact.Path] != artifact.SHA256 {
			return fmt.Errorf(
				"S9 authority fingerprint is stale: baseline artifact %q drifted after RepairSession %s opened (session=%s live=%s); the Session/Plan/Result chain no longer describes the code being repaired — claim the change through a RepairResult or re-baseline the case through S8/S9 planning before committing further repair artifacts",
				artifact.Path, session.SessionID, shortDigest(artifact.SHA256), shortDigest(liveByPath[artifact.Path]))
		}
	}
	return nil
}

// ValidateAuthorityFreshness exposes the RC-09 (S9-4) authority gate (with the
// RC-15 empty-fingerprint fail-closed behavior) for tests and tooling that
// need to probe the gate without committing an S9 artifact.
func ValidateAuthorityFreshness(root string, pointer map[string]any, pending []ChangedArtifact) error {
	return checkS9AuthorityFreshness(root, pointer, pending)
}

// BindTargetedReverificationIdentitiesForTest exposes the RC-01/RC-15
// identity-binding gate for tests that need to exercise owner-binding
// semantics against a synthetic pointer.
func BindTargetedReverificationIdentitiesForTest(p map[string]any, plan RepairPlan, value TargetedReverification) error {
	return bindTargetedReverificationIdentities(p, plan, value)
}

// claimedRepairChanges collects every changed-artifact path the repair has
// declared: the pending request plus every RepairResult already bound to the
// Runtime pointer. Those paths are the only baseline mutations the authority
// gate forgives.
func claimedRepairChanges(root string, pointer map[string]any, pending []ChangedArtifact) (map[string]bool, error) {
	claimed := make(map[string]bool, len(pending))
	for _, artifact := range pending {
		claimed[normalizePath(artifact.Path)] = true
	}
	refs, err := currentRepairResultRefs(pointer)
	if err != nil {
		// No result committed yet; only the pending claim exists.
		return claimed, nil
	}
	for _, ref := range refs {
		result, resultErr := ValidateRepairResult(root, ref)
		if resultErr != nil {
			return nil, fmt.Errorf("S9 authority check cannot validate committed RepairResult: %w", resultErr)
		}
		for _, artifact := range result.ChangedArtifacts {
			claimed[normalizePath(artifact.Path)] = true
		}
	}
	return claimed, nil
}

func shortDigest(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}

// requiredReverificationIDs projects the impact's required set into the
// runtime pointer so the outstanding obligations are auditable state. The
// projection is a plain copy: the producer registers the obligation at Impact
// commit, and the consuming gate is the exact-set check in
// CommitRepairHandoff, which compares this list against the actually
// committed reverification IDs.
func requiredReverificationIDs(impact ChangeImpact) []any {
	outstanding := make([]any, 0, len(impact.RequiredReverificationIDs))
	for _, id := range impact.RequiredReverificationIDs {
		outstanding = append(outstanding, id)
	}
	return outstanding
}

// validateChangeImpactEvidenceLedger is the S9 disposition consumer. The
// ChangeImpact artifact is allowed to describe a rich ledger, but its lists
// only become authoritative after Runtime proves that every named evidence
// item exists, is in the active baseline, and has a coherent disposition.
// RecoveryEvidence is checked against either the current Runtime evidence
// index, a content-addressed local path, or a hash-pinned S9 artifact already
// held by the repair pointer; arbitrary path strings are not recovery proof.
func validateChangeImpactEvidenceLedger(root string, state, pointer map[string]any, impact ChangeImpact, committed bool) error {
	byID := map[string]map[string]any{}
	if raw, ok := state["evidence"].([]any); ok {
		for _, item := range raw {
			entry, _ := item.(map[string]any)
			if entry != nil && strings.TrimSpace(stringField(entry["id"])) != "" {
				byID[stringField(entry["id"])] = entry
			}
		}
	}

	dispositions := []struct {
		name   string
		ids    []string
		status string
	}{
		{"invalidated_evidence_ids", impact.InvalidatedEvidenceIDs, "invalid"},
		{"superseded_evidence_ids", impact.SupersededEvidenceIDs, "superseded"},
		{"retained_evidence_ids", impact.RetainedEvidenceIDs, "valid"},
	}
	seen := map[string]string{}
	for _, disposition := range dispositions {
		for _, rawID := range disposition.ids {
			id := strings.TrimSpace(rawID)
			if id == "" {
				return fmt.Errorf("ChangeImpact %s contains an empty evidence id", disposition.name)
			}
			if prior := seen[id]; prior != "" {
				return fmt.Errorf("ChangeImpact evidence %q appears in both %s and %s", id, prior, disposition.name)
			}
			seen[id] = disposition.name
			entry, ok := byID[id]
			if !ok {
				return fmt.Errorf("ChangeImpact %s references unregistered Runtime evidence %q", disposition.name, id)
			}
			status := stringField(entry["status"])
			if status == "" {
				status = "valid"
			}
			if committed {
				if status != disposition.status {
					return fmt.Errorf("ChangeImpact %s evidence %q has status %q after commit, want %q", disposition.name, id, status, disposition.status)
				}
			} else if disposition.name == "superseded_evidence_ids" && status != "valid" {
				return fmt.Errorf("ChangeImpact superseded evidence %q has status %q before commit, want valid", id, status)
			} else if disposition.name == "retained_evidence_ids" && status != "valid" {
				return fmt.Errorf("ChangeImpact retained evidence %q has status %q, want valid", id, status)
			}
			if generation := integerValue(entry["baseline_generation"]); generation != baselineGeneration(state) {
				return fmt.Errorf("ChangeImpact %s evidence %q belongs to baseline_generation %d, want %d", disposition.name, id, generation, baselineGeneration(state))
			}
		}
	}

	// A retained item needs a decision row whose target is that exact evidence
	// id. Its rationale is the machine-auditable reason for retaining it; this
	// prevents a top-level retained list from becoming an unconnected claim.
	for _, id := range impact.RetainedEvidenceIDs {
		matched := false
		for _, decision := range impact.Decisions {
			if decision.Decision == "retain" && strings.TrimSpace(decision.TargetID) == strings.TrimSpace(id) && strings.TrimSpace(decision.Rationale) != "" {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("ChangeImpact retained evidence %q has no retain decision targeting that id with a rationale", id)
		}
	}

	// Retained evidence must be outside the mechanically impacted surface. A
	// human assertion cannot override the dependency graph's conservative
	// invalidation result.
	changedPaths := make([]string, 0, len(impact.ChangedArtifacts))
	for _, artifact := range impact.ChangedArtifacts {
		changedPaths = append(changedPaths, artifact.Path)
	}
	for _, item := range impactanalysis.ComputeImpact(state, changedPaths) {
		if seen[item.EvidenceID] == "retained_evidence_ids" {
			return fmt.Errorf("ChangeImpact retained evidence %q overlaps changed surface %q; retain requires dependency/content proof outside the impact", item.EvidenceID, item.ScopeRef)
		}
	}

	for index, decision := range impact.Decisions {
		if strings.TrimSpace(decision.SourceID) == "" || strings.TrimSpace(decision.TargetID) == "" || strings.TrimSpace(decision.Rationale) == "" {
			return fmt.Errorf("ChangeImpact decision %d requires source_id, target_id, and rationale", index)
		}
		if decision.Decision == "retain" && len(decision.RecoveryEvidence) == 0 {
			return fmt.Errorf("ChangeImpact retain decision %q requires recovery_evidence proving the retained item", decision.TargetID)
		}
		if decision.Decision == "supersede" && len(decision.RecoveryEvidence) == 0 {
			return fmt.Errorf("ChangeImpact supersede decision %q requires recovery_evidence for the replacement", decision.TargetID)
		}
		for _, ref := range decision.RecoveryEvidence {
			if err := validateImpactRecoveryEvidence(root, state, pointer, ref); err != nil {
				return fmt.Errorf("ChangeImpact decision %q recovery_evidence %q: %w", decision.TargetID, ref, err)
			}
		}
	}
	return nil
}

func validateImpactRecoveryEvidence(root string, state, pointer map[string]any, rawRef string) error {
	ref := strings.TrimSpace(rawRef)
	if ref == "" {
		return errors.New("reference is empty")
	}
	if strings.Contains(ref, "://") {
		parts := strings.SplitN(ref, "://", 2)
		if strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return errors.New("external recovery reference must contain a scheme and subject")
		}
		return nil
	}
	if strings.HasPrefix(ref, "path:") {
		rel, want, err := parseImpactPathEvidenceRef(ref)
		if err != nil {
			return err
		}
		path, err := repositoryPath(root, rel)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read local recovery artifact: %w", err)
		}
		if got := sha256Bytes(data); got != want {
			return fmt.Errorf("local recovery artifact digest mismatch: got %s, want %s", got, want)
		}
		return nil
	}
	if err := evidence.ValidateRefs(state, []string{ref}, evidence.RefsOptions{Root: root}); err == nil {
		return nil
	}
	for _, artifact := range currentRepairArtifactRefs(pointer) {
		if normalizePath(artifact.Path) != normalizePath(ref) {
			continue
		}
		if _, err := readArtifact(root, artifact, ""); err != nil {
			return fmt.Errorf("pinned recovery artifact is not readable: %w", err)
		}
		return nil
	}
	return errors.New("recovery reference is neither a current Runtime evidence id nor a hash-pinned S9 artifact or execution anchor")
}

func parseImpactPathEvidenceRef(ref string) (string, string, error) {
	rel := strings.TrimPrefix(ref, "path:")
	marker := "#sha256="
	index := strings.Index(rel, marker)
	if index < 0 {
		return "", "", errors.New("local recovery path must carry #sha256=<64 hex>")
	}
	want := rel[index+len(marker):]
	rel = rel[:index]
	if strings.TrimSpace(rel) == "" || len(want) != 64 {
		return "", "", errors.New("local recovery path requires a non-empty path and exactly 64 hexadecimal digest characters")
	}
	if _, err := hex.DecodeString(want); err != nil {
		return "", "", fmt.Errorf("local recovery path digest is not hexadecimal: %w", err)
	}
	return rel, want, nil
}

func currentRepairArtifactRefs(pointer map[string]any) []ArtifactRef {
	if pointer == nil {
		return nil
	}
	refs := []ArtifactRef{}
	for _, pair := range [][2]string{
		{"path", "sha256"}, {"contract_ref", "contract_sha256"}, {"plan_ref", "plan_sha256"},
		{"plan_report_ref", "plan_report_sha256"}, {"result_ref", "result_sha256"}, {"changeset_ref", "changeset_sha256"},
		{"impact_ref", "impact_sha256"}, {"handoff_ref", "handoff_sha256"}, {"review_plan_seed_ref", "review_plan_seed_sha256"},
	} {
		if path, hash := stringField(pointer[pair[0]]), stringField(pointer[pair[1]]); path != "" && hash != "" {
			refs = append(refs, ArtifactRef{Path: path, SHA256: hash})
		}
	}
	for _, key := range []string{"plan_report_refs", "result_refs", "targeted_reverification_artifacts"} {
		refs = append(refs, existingArtifactRefs(pointer[key])...)
	}
	return refs
}

func CommitChangeImpact(root, statePath, journalPath string, req CommitImpactRequest) (runtimepkg.Snapshot, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	p := repairPointer(current.State)
	if p == nil || stringField(p["status"]) != "impact_reconciliation" {
		return runtimepkg.Snapshot{}, errors.New("S9 impact commit requires status=impact_reconciliation")
	}
	// RC-09 (S9-4): reconciling impact against a drifted baseline would pin
	// the wrong artifact set into the next review round — block the commit.
	if err := checkS9AuthorityFreshness(root, p, nil); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	impactDocument, err := ValidateChangeImpact(root, req.Impact)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	resultDocuments, err := validateCurrentRepairResults(root, current.State, p)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	resultArtifacts, err := aggregateRepairResultArtifacts(resultDocuments)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if impactDocument.RuntimeID != stringField(current.State["runtime_id"]) {
		return runtimepkg.Snapshot{}, fmt.Errorf("ChangeImpact runtime_id %q does not match current Runtime %q", impactDocument.RuntimeID, stringField(current.State["runtime_id"]))
	}
	if impactDocument.ReqID != boundReqID(current.State) {
		return runtimepkg.Snapshot{}, fmt.Errorf("ChangeImpact req_id %q does not match bound REQ %q", impactDocument.ReqID, boundReqID(current.State))
	}
	if impactDocument.BaselineGeneration != baselineGeneration(current.State) {
		return runtimepkg.Snapshot{}, fmt.Errorf("ChangeImpact baseline_generation %d does not match Runtime baseline_generation %d", impactDocument.BaselineGeneration, baselineGeneration(current.State))
	}
	// RC-09 (S9-4): the authority-fingerprint gate must speak before the
	// exact-set checks — if the baseline drifted after the session opened,
	// the actionable cause is the stale surface, not the bookkeeping.
	if err := checkS9AuthorityFreshness(root, p, nil); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := exactChangedArtifactSet(resultArtifacts, impactDocument.ChangedArtifacts, "RepairResult batch", "ChangeImpact"); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := validateChangeImpactEvidenceLedger(root, current.State, p, impactDocument, false); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	sessionRef, err := pointerArtifact(p, "path", "sha256", "current RepairSession")
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	session, err := ValidateRepairSession(root, sessionRef)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	actualArtifacts, err := ComputeSessionChangeset(root, session)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	// RC-09 (S9-4): the authority-fingerprint gate must speak first. If the
	// baseline drifted after the session opened, the Session diff contains an
	// out-of-band delta the repair never claimed, and reporting "exact-set
	// mismatch" would bury the actionable cause — the surface is stale, not
	// the bookkeeping.
	if err := checkS9AuthorityFreshness(root, p, nil); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := exactChangedArtifactSet(resultArtifacts, actualArtifacts, "RepairResult batch", "actual Session diff"); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	// RC-09 (S9-6): register required_reverification_ids as the durable
	// obligation for the next phase. The TargetedReverification artifact is a
	// downstream result and is intentionally created/committed after this
	// transaction. CommitRepairHandoff consumes the pointer's obligation and
	// rejects any missing required IDs before S9 can close.
	at := occurred(req.OccurredAt)
	actor := req.Actor
	if actor == "" {
		actor = "orchestrator"
	}
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	supersededIDs := map[string]bool{}
	retainedIDs := map[string]bool{}
	for _, id := range impactDocument.SupersededEvidenceIDs {
		supersededIDs[id] = true
	}
	for _, id := range impactDocument.RetainedEvidenceIDs {
		retainedIDs[id] = true
	}
	return updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-impact-r%d", commitRevision+1), TransitionID: whitelistChecked("PTR-BUG-05"), Event: "change_impact_reconciled", Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:impact:%s:%d", req.Impact.Path, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.Impact.Path}, From: cursor(current.State), To: map[string]any{"state": "bug_resolution", "phase": "targeted_reverification"}, RequestID: "s9-impact-reconcile", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-IMPACT-RECONCILE", GateFingerprint: "sha256:s9-impact-reconcile-v1", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
		updateRepairPointer(state, map[string]any{"impact_ref": req.Impact.Path, "impact_sha256": req.Impact.SHA256, "status": "targeted_reverification", "required_reverification_ids": requiredReverificationIDs(impactDocument), "updated_at": at.Format(time.RFC3339Nano), "next_action": "submit independent targeted reverification"})
		changedPaths := make([]string, 0, len(impactDocument.ChangedArtifacts))
		for _, artifact := range impactDocument.ChangedArtifacts {
			changedPaths = append(changedPaths, artifact.Path)
		}
		impacted := impactanalysis.ComputeImpact(state, changedPaths)
		unlisted := make([]impactanalysis.EvidenceImpact, 0, len(impacted))
		for _, item := range impacted {
			if supersededIDs[item.EvidenceID] || retainedIDs[item.EvidenceID] {
				continue
			}
			unlisted = append(unlisted, item)
		}
		impactanalysis.InvalidateEvidence(state, unlisted, impactDocument.ImpactID)
		invalidateListedEvidence(state, impactDocument.InvalidatedEvidenceIDs, impactDocument.ImpactID, "declared by ChangeImpact")
		supersedeListedEvidence(state, impactDocument.SupersededEvidenceIDs, impactDocument.ImpactID)
		setLifecycle(state, "bug_resolution", "targeted_reverification")
		return nil
	}})
}

// bindTargetedReverificationIdentities is the RC-01 identity-binding gate
// (L1 D6 three-way convergence). It binds the reverification to the dispatched
// repair chain before the S9 handoff can release a bug for full review:
//
//  1. original_assignment_id must be the actual repair assignment that owns
//     the bug's unit — either the plan assignment (repair-assignment-...) or
//     its dispatched manifest alias (assignment-s9-...), proven by holding
//     an entry in assignment_owners;
//  2. performing_assignment_id must be a dispatched verifier identity from
//     the active S9 verifier pool (a registered agent in state) and must not
//     be owned by the repair owner in assignment_owners — an assignment not
//     known to the session (or an alias of the owner's own assignment) is a
//     fabricated verifier and is rejected;
//  3. RC-15 (S9-H8): the original repair assignment must carry a non-empty
//     owner (a claimed PlanReport) and the performing identity must resolve
//     to a different agent — an unowned original or an unclaimed performing
//     assignment has no agent identity to bind and cannot prove independence.
func bindTargetedReverificationIdentities(p map[string]any, plan RepairPlan, value TargetedReverification) error {
	owners := stringMapField(p["assignment_owners"])
	if len(owners) == 0 {
		return errors.New("targeted reverification requires a dispatched repair assignment; assignment_owners is empty so no repair owner exists to verify against")
	}
	if !targetedAssignmentDispatched(p, plan, value.OriginalAssignmentID, owners) {
		return fmt.Errorf("targeted reverification original_assignment_id %q is not the dispatched repair assignment for this bug; use the repair assignment recorded in assignment_owners (%s) or its manifest alias", value.OriginalAssignmentID, strings.Join(ownedAssignments(owners), ", "))
	}
	// RC-15 (S9-H8): assignment-level owner binding. The original repair
	// assignment must be claimed by a known agent; an empty owner means the
	// PlanReport never bound an identity and the reverification would be
	// comparing against a ghost.
	originalOwner := dispatchedVerifierAgentID(value.OriginalAssignmentID, owners)
	if strings.TrimSpace(originalOwner) == "" {
		return fmt.Errorf("targeted reverification original_assignment_id %q has no recorded owner in assignment_owners; submit its PlanReport so the repair owner identity is bound before an independent verifier is selected", value.OriginalAssignmentID)
	}
	if strings.TrimSpace(value.PerformingAssignmentID) == "" {
		return errors.New("targeted reverification performing_assignment_id is required")
	}
	performingOwner := dispatchedVerifierAgentID(value.PerformingAssignmentID, owners)
	if performingOwner == originalOwner {
		return fmt.Errorf("targeted reverification is not independent: performing_assignment_id %q resolves to the repair owner %s of %q", value.PerformingAssignmentID, originalOwner, value.OriginalAssignmentID)
	}
	if !targetedAssignmentDispatched(p, plan, value.PerformingAssignmentID, owners) {
		return fmt.Errorf("targeted reverification performing_assignment_id %q is not a dispatched verifier identity in the active S9 assignment pool; the reverification must be performed by a dispatched assignment, not a fabricated ID", value.PerformingAssignmentID)
	}
	// RC-15 (S9-H8): a performing identity that dispatches nothing and is
	// claimed by nobody is still a fabricated verifier even if its spelling
	// passes the plan-member check — require a recorded owner so the agent
	// identity is always resolvable.
	if strings.TrimSpace(performingOwner) == "" {
		return fmt.Errorf("targeted reverification performing_assignment_id %q is not claimed by any agent in assignment_owners; an unclaimed verifier identity cannot be bound to a responsible agent", value.PerformingAssignmentID)
	}
	return nil
}

// targetedAssignmentDispatched reports whether an assignment identity belongs
// to the dispatched repair chain: it is either a repair assignment of the
// current RepairPlan, its registered alias in assignment_owners, or the
// manifest-alias spelling of such an assignment.
func targetedAssignmentDispatched(p map[string]any, plan RepairPlan, assignmentID string, owners map[string]string) bool {
	if assignmentID == "" {
		return false
	}
	for _, assignment := range plan.Assignments {
		if assignment.AssignmentID == assignmentID || manifestAssignmentAlias(assignment.AssignmentID) == assignmentID {
			return true
		}
	}
	if _, ok := owners[assignmentID]; ok {
		return true
	}
	for assignment := range owners {
		if manifestAssignmentAlias(assignment) == assignmentID {
			return true
		}
	}
	return false
}

// dispatchedVerifierAgentID resolves the performing assignment to the agent
// that owns it in assignment_owners. Only ownership recorded by the runtime
// (PlanReport submission or registration binding) counts; an unowned
// assignment has no agent identity and cannot collide with the owner.
func dispatchedVerifierAgentID(assignmentID string, owners map[string]string) string {
	if agent, ok := owners[assignmentID]; ok {
		return agent
	}
	for assignment, agent := range owners {
		if manifestAssignmentAlias(assignment) == assignmentID {
			return agent
		}
	}
	return ""
}

// ownedAssignments lists the assignment IDs recorded in assignment_owners.
func ownedAssignments(owners map[string]string) []string {
	result := make([]string, 0, len(owners))
	for assignment := range owners {
		result = append(result, assignment)
	}
	sort.Strings(result)
	return result
}

// ownedAssignmentAgents maps every recorded assignment alias to its owner
// agent so identity resolution works in both spellings.
func ownedAssignmentAgents(owners map[string]string) map[string]string {
	result := make(map[string]string, len(owners))
	for assignment, agent := range owners {
		result[assignment] = agent
		result[manifestAssignmentAlias(assignment)] = agent
	}
	return result
}

// manifestAssignmentAlias mirrors the CLI dispatch identity: dispatching a
// repair assignment registers the workgroup under
// assignment-s9-<unit-slug>. Both spellings name the same work item.
func manifestAssignmentAlias(assignmentID string) string {
	return "assignment-s9-" + dispatchSlugCompat(strings.TrimPrefix(assignmentID, "repair-assignment-"))
}

// dispatchSlugCompat reproduces the CLI dispatch slug (lowercase, non
// alphanumerics collapsed to one dash) so the repair package does not import
// the CLI.
func dispatchSlugCompat(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func CommitTargetedReverification(root, statePath, journalPath string, req CommitTargetedRequest) (runtimepkg.Snapshot, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	p := repairPointer(current.State)
	if p == nil || stringField(p["status"]) != "targeted_reverification" {
		return runtimepkg.Snapshot{}, errors.New("S9 targeted reverification requires status=targeted_reverification")
	}
	// RC-09 (S9-4): a reverification performed against drifted code endorses a
	// repair that is not on disk — block the commit before identity checks.
	if err := checkS9AuthorityFreshness(root, p, nil); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	value, err := ValidateTargetedReverification(root, req.Reverification)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	// RC-14: targeted reverification assertion evidence must be current-generation, valid, SHA-verified runtime evidence (or an execution anchor). Phantom `evidence/phantom.json` or stale-generation ids are rejected here before the Runtime CAS commits.
	if len(value.AssertionResults) > 0 {
		var allRefs []string
		for _, assertion := range value.AssertionResults {
			allRefs = append(allRefs, assertion.EvidenceRefs...)
		}
		if len(allRefs) > 0 {
			currentRound := 0
			if review, ok := current.State["review"].(map[string]any); ok {
				switch v := review["round"].(type) {
				case int:
					currentRound = v
				case int64:
					currentRound = int(v)
				case float64:
					currentRound = int(v)
				}
			}
			if verr := evidence.ValidateRefs(current.State, allRefs, evidence.RefsOptions{Root: root, RequireReviewRound: currentRound}); verr != nil {
				return runtimepkg.Snapshot{}, fmt.Errorf("targeted reverification evidence_refs: %w", verr)
			}
		}
	}
	impactRef, err := pointerArtifact(p, "impact_ref", "impact_sha256", "current ChangeImpact")
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	currentImpact, err := ValidateChangeImpact(root, impactRef)
	if err != nil {
		return runtimepkg.Snapshot{}, fmt.Errorf("current ChangeImpact is invalid: %w", err)
	}
	if value.ImpactID != currentImpact.ImpactID {
		return runtimepkg.Snapshot{}, fmt.Errorf("targeted reverification impact_id %q is not the current ChangeImpact %q", value.ImpactID, currentImpact.ImpactID)
	}
	if value.RuntimeID != currentImpact.RuntimeID || value.BaselineGeneration != currentImpact.BaselineGeneration {
		return runtimepkg.Snapshot{}, fmt.Errorf("targeted reverification runtime/baseline does not match current ChangeImpact")
	}
	planRef, err := pointerArtifact(p, "plan_ref", "plan_sha256", "current RepairPlan")
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	plan, err := ValidateRepairPlan(root, planRef)
	if err != nil {
		return runtimepkg.Snapshot{}, fmt.Errorf("current RepairPlan is invalid: %w", err)
	}
	contractRef := ArtifactRef{Path: stringField(p["contract_ref"]), SHA256: stringField(p["contract_sha256"])}
	contractBytes, err := readArtifact(root, contractRef, "repair-contract.schema.json")
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	var contractDocument map[string]any
	if err := json.Unmarshal(contractBytes, &contractDocument); err != nil {
		return runtimepkg.Snapshot{}, fmt.Errorf("decode approved RepairContract: %w", err)
	}
	if err := exactContractAssertionCoverage(contractDocument, value.AssertionResults); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	// RC-01 (S9-1): identity-bound independence. The string-inequality check
	// inside ValidateTargetedReverification is not enough — a Builder can fill
	// any fabricated "independent" verifier ID. Here the two identities are
	// bound to the dispatched repair chain: the original must be the actual
	// repair assignment for this bug (the plan assignment recorded by the
	// assignment_owners map, or its manifest alias), and the performing
	// verifier must be a dispatched identity that is not owned by the repair
	// owner.
	refs := appendUnique(stringSliceFromAny(p["targeted_reverification_refs"]), req.Reverification.Path)
	targetedArtifacts := appendArtifactRefFromPointer(p["targeted_reverification_artifacts"], req.Reverification)
	at := occurred(req.OccurredAt)
	// RC-01 (EH-11): the actor identity is part of the gate evidence. A
	// silent default would let a machine identity self-endorse the
	// reverification, so an omitted --actor is a hard rejection. The check
	// intentionally precedes the identity binding so the actor omission is
	// the first, most actionable failure.
	actor := strings.TrimSpace(req.Actor)
	if actor == "" {
		return runtimepkg.Snapshot{}, errors.New("targeted reverification commit requires an explicit actor identity: pass --actor <agent-id> (the independent verifier), not an implicit default")
	}
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	// RC-01 (S9-1): identity-bound independence. The string-inequality check
	// inside ValidateTargetedReverification is not enough — a Builder can fill
	// any fabricated "independent" verifier ID. Here the two identities are
	// bound to the dispatched repair chain: the original must be the actual
	// repair assignment for this bug (the plan assignment recorded by the
	// assignment_owners map, or its manifest alias), and the performing
	// verifier must be a dispatched identity that is not owned by the repair
	// owner.
	if err := bindTargetedReverificationIdentities(p, plan, value); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if value.Result != "pass" || value.ScopeCompliance != "pass" {
		route := value.FailureClass
		if route == "" {
			route = "fail_same_cause"
		}
		nextAction := targetedFailureNextAction(stringField(p["case_id"]), route, req.Reverification)
		return updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-targeted-fail-r%d", commitRevision+1), TransitionID: whitelistChecked("S9-TARGETED-FAILURE"), Event: "targeted_reverification_failed", Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:targeted-failure:%s:%d", req.Reverification.Path, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.Reverification.Path}, From: cursor(current.State), To: map[string]any{"state": "bug_resolution", "phase": "investigation"}, RequestID: "s9-targeted-reverification-failure", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-TARGETED-FAILURE", GateFingerprint: "sha256:s9-targeted-failure-v1", ProducerResponsibility: "S9 QA", OccurredAt: at, Apply: func(state map[string]any) error {
			updateRepairPointer(state, map[string]any{"targeted_reverification_refs": refs, "targeted_reverification_artifacts": targetedArtifacts, "failure_route": route, "status": "blocked", "updated_at": at.Format(time.RFC3339Nano), "next_action": nextAction})
			setLifecycle(state, "bug_resolution", "investigation")
			return nil
		}})
	}
	return updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-targeted-r%d", commitRevision+1), TransitionID: whitelistChecked("PTR-BUG-06"), Event: "targeted_reverification_passed", Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:targeted:%s:%d", req.Reverification.Path, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.Reverification.Path}, From: cursor(current.State), To: map[string]any{"state": "bug_resolution", "phase": "ready_for_full_review"}, RequestID: "s9-targeted-reverification", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-TARGETED-REVERIFICATION", GateFingerprint: "sha256:s9-targeted-reverification-v1", ProducerResponsibility: "S9 QA", OccurredAt: at, Apply: func(state map[string]any) error {
		updateRepairPointer(state, map[string]any{"targeted_reverification_refs": refs, "targeted_reverification_artifacts": targetedArtifacts, "status": "ready_for_full_review", "updated_at": at.Format(time.RFC3339Nano), "next_action": "create RepairHandoff; S7 must run a complete review round"})
		setLifecycle(state, "bug_resolution", "ready_for_full_review")
		return nil
	}})
}

// ResumeTargetedReverification reopens the existing targeted verification
// checkpoint after an explicitly recorded environmental/authority blocker
// has been resolved. It is deliberately narrower than a generic S9 resume:
// only a blocked targeted result may use it, and the next step remains an
// independent TargetedReverification create/commit.
func ResumeTargetedReverification(root, statePath, journalPath string, req ResumeTargetedRequest) (runtimepkg.Snapshot, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if strings.TrimSpace(req.Reason) == "" {
		return runtimepkg.Snapshot{}, errors.New("targeted verification resume requires a blocker resolution reason")
	}
	p := repairPointer(current.State)
	if p == nil || stringField(p["status"]) != "blocked" {
		return runtimepkg.Snapshot{}, errors.New("targeted verification resume requires status=blocked; inspect runtime repair status")
	}
	if stringField(p["failure_route"]) != "blocked" {
		return runtimepkg.Snapshot{}, fmt.Errorf("targeted verification status is blocked but failure_route=%q; use the S8 causal reassessment action instead of targeted resume", stringField(p["failure_route"]))
	}
	actor := strings.TrimSpace(req.Actor)
	if actor == "" {
		actor = "orchestrator"
	}
	at := occurred(req.OccurredAt)
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	return updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{
		EventID: fmt.Sprintf("evt-s9-targeted-resume-r%d", commitRevision+1), TransitionID: whitelistChecked("PTR-BUG-12"),
		Event: "targeted_reverification_unblocked", Actor: actor,
		IdempotencyKey: fmt.Sprintf("runtime:s9:targeted-resume:%s:%d", stringField(p["session_id"]), commitRevision),
		RuntimeID:      stringField(current.State["runtime_id"]), EvidenceIDs: stringSliceFromAny(p["targeted_reverification_refs"]),
		From: cursor(current.State), To: map[string]any{"state": "bug_resolution", "phase": "targeted_reverification"},
		RequestID: "s9-targeted-reverification-resume", BaselineGeneration: baselineGeneration(current.State),
		GateID: "S9-TARGETED-BLOCKER-RESUME", GateFingerprint: "sha256:s9-targeted-blocker-resume-v1",
		ProducerResponsibility: "S9 QA", OccurredAt: at,
		Apply: func(state map[string]any) error {
			pointer := repairPointer(state)
			if pointer == nil || stringField(pointer["status"]) != "blocked" || stringField(pointer["failure_route"]) != "blocked" {
				return errors.New("targeted verification blocker changed before resume; re-read runtime repair status")
			}
			updateRepairPointer(state, map[string]any{
				"status":              "targeted_reverification",
				"blocker_resolved_by": actor,
				"blocker_resolution":  strings.TrimSpace(req.Reason),
				"blocker_resolved_at": at.Format(time.RFC3339Nano),
				"updated_at":          at.Format(time.RFC3339Nano),
				"next_action":         "submit a new independent targeted reverification",
			})
			setLifecycle(state, "bug_resolution", "targeted_reverification")
			return nil
		},
	})
}

func targetedFailureNextAction(caseID, route string, reverification ArtifactRef) string {
	if route == "blocked" {
		return "after resolving the blocker, run `runtime repair targeted resume --actor <actor> --reason <resolution>`; then create and commit a new independent targeted reverification"
	}
	if strings.TrimSpace(caseID) == "" {
		return "route the targeted failure to S8 root-cause investigation; do not patch the symptom locally"
	}
	return fmt.Sprintf("re-open Case %s in S8 with `runtime investigation route --case-id %s --route investigate_more --reason \"targeted reverification %s requires causal reassessment\" --reassessment-evidence %s`; then re-read the Case and investigate before approving a new RepairContract", caseID, caseID, route, reverification.Path)
}

func exactContractAssertionCoverage(contract map[string]any, results []AssertionResult) error {
	expected := map[string]bool{}
	addSlots := func(field, prefix string) {
		for index := range anySlice(contract[field]) {
			expected[fmt.Sprintf("%s-%d", prefix, index+1)] = true
		}
	}
	addSlots("symptom_assertions", "symptom")
	addSlots("root_invariant_assertions", "root")
	for index := range anySlice(contract["detection_gap_assertions"]) {
		// gap-N is the established short form; detection-N is accepted as a
		// self-explanatory alias for newly authored verifier reports.
		expected[fmt.Sprintf("gap-%d", index+1)] = true
		expected[fmt.Sprintf("detection-%d", index+1)] = true
	}
	maxSlots := len(anySlice(contract["symptom_assertions"]))
	if rootSlots := len(anySlice(contract["root_invariant_assertions"])); rootSlots > maxSlots {
		maxSlots = rootSlots
	}
	if detectionSlots := len(anySlice(contract["detection_gap_assertions"])); detectionSlots > maxSlots {
		maxSlots = detectionSlots
	}
	requiredSlots := len(anySlice(contract["symptom_assertions"])) + len(anySlice(contract["root_invariant_assertions"])) + len(anySlice(contract["detection_gap_assertions"]))
	if len(results) != requiredSlots {
		return fmt.Errorf("exact RepairContract assertion coverage failed: submitted=%d required=%d; use one result for every symptom-N, root-N, and gap-N/detection-N slot", len(results), requiredSlots)
	}
	actual := map[string]bool{}
	for _, result := range results {
		actual[result.AssertionID] = true
	}
	missing := []string{}
	for index := 1; index <= maxSlots; index++ {
		// Detection slots have two accepted spellings but represent one slot.
		if expected[fmt.Sprintf("symptom-%d", index)] || expected[fmt.Sprintf("root-%d", index)] || expected[fmt.Sprintf("gap-%d", index)] || expected[fmt.Sprintf("detection-%d", index)] {
			for _, prefix := range []string{"symptom", "root"} {
				id := fmt.Sprintf("%s-%d", prefix, index)
				if expected[id] && !actual[id] {
					missing = append(missing, id)
				}
			}
			gapID, detectionID := fmt.Sprintf("gap-%d", index), fmt.Sprintf("detection-%d", index)
			if expected[gapID] && !actual[gapID] && !actual[detectionID] {
				missing = append(missing, gapID+" (or "+detectionID+")")
			}
		}
	}
	extra := []string{}
	for id := range actual {
		if !expected[id] {
			// detection-N is an alias for gap-N and is therefore not extra.
			if strings.HasPrefix(id, "detection-") {
				var index int
				if _, err := fmt.Sscanf(id, "detection-%d", &index); err == nil && expected[fmt.Sprintf("gap-%d", index)] {
					continue
				}
			}
			extra = append(extra, id)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		sort.Strings(missing)
		sort.Strings(extra)
		return fmt.Errorf("exact RepairContract assertion coverage failed: missing=%v extra=%v; use symptom-N, root-N, and gap-N (or detection-N) IDs in Contract order", missing, extra)
	}
	return nil
}

// checkS9HandoffBudget is the RC-15 (S9-M1/T1) explicit full-review-round
// budget gate for CommitRepairHandoff. It returns the typed
// *transition.RepairLimitError when review.round has already reached
// configuration.repair.max_full_review_rounds: the handoff is the only site
// that increments review.round, so an over-budget handoff would silently
// open an unbounded S7 round chain. The limit is structural (the same field
// the S7 budget-decision verb raises), reads 0 as "unlimited", and never
// fires on a missing configuration block.
func checkS9HandoffBudget(state map[string]any) error {
	repair, ok := mapField(state, "configuration")["repair"].(map[string]any)
	if !ok {
		return nil
	}
	max := 0
	switch v := repair["max_full_review_rounds"].(type) {
	case float64:
		max = int(v)
	case int:
		max = v
	}
	if max <= 0 {
		return nil
	}
	review, ok := state["review"].(map[string]any)
	if !ok {
		return nil
	}
	round := integerValue(review["round"])
	if round < max {
		return nil
	}
	return fmt.Errorf("%w", &transition.RepairLimitError{BugID: stringField(repairPointer(state)["case_id"]), Attempts: round, Max: max})
}

func CommitRepairHandoff(root, statePath, journalPath string, req CommitHandoffRequest) (runtimepkg.Snapshot, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	p := repairPointer(current.State)
	if p == nil || stringField(p["status"]) != "ready_for_full_review" {
		return runtimepkg.Snapshot{}, errors.New("S9 handoff requires status=ready_for_full_review")
	}
	// RC-09 (S9-4): the handoff seeds the next S7 round from the changed
	// surface; a drifted baseline would seed the wrong round — block it.
	if err := checkS9AuthorityFreshness(root, p, nil); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	// RC-15 (S9-M1/T1): the handoff is the only site that opens a new full
	// review round (review.round++ below), so the budget gate lives here, not
	// inside the transition engine. A handoff submitted when the round
	// counter has already reached max_full_review_rounds raises the typed
	// *transition.RepairLimitError so the cli bridge dispatches GTR-004 and
	// the Loop pauses for a human budget decision instead of silently
	// opening round N+1. Unlimited when the limit is absent or zero.
	if err := checkS9HandoffBudget(current.State); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	handoff, err := ValidateRepairHandoff(root, req.Handoff)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := validateHandoffAgainstCurrentRepair(root, current.State, p, handoff); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	impactDocument, err := ValidateChangeImpact(root, handoff.ChangeImpactRef)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	// S7_PLAN_TASK_COVERAGE rejects a plan whose Claims drop every
	// current-generation TASK from source_refs, so the seed must carry them
	// on the delivery Claim (sandbox-verified: the seed otherwise cannot be
	// registered as-is after TR-012).
	taskIDs := []string{}
	generation := baselineGeneration(current.State)
	if documents, ok := current.State["documents"].([]any); ok {
		for _, raw := range documents {
			doc, _ := raw.(map[string]any)
			if doc == nil || stringField(doc["kind"]) != "task" || integerValue(doc["generation"]) != generation {
				continue
			}
			if id := stringField(doc["id"]); id != "" {
				taskIDs = append(taskIDs, id)
			}
		}
	}
	seedRef, err := createS7ReviewPlanSeed(root, integerValue(mapField(current.State, "review")["round"])+1, baselineGeneration(current.State), impactDocument, ContractRef{Path: handoff.ContractRef.Path, SHA256: handoff.ContractRef.SHA256}, taskIDs, handoff.ChangeImpactRef.Path)
	if err != nil {
		return runtimepkg.Snapshot{}, fmt.Errorf("create S7 ReviewPlan seed: %w", err)
	}
	seedBytes, err := readArtifact(root, seedRef, "review-plan.schema.json")
	if err != nil {
		return runtimepkg.Snapshot{}, fmt.Errorf("read S7 ReviewPlan seed: %w", err)
	}
	var seedPlan reviewpkg.Plan
	if err := json.Unmarshal(seedBytes, &seedPlan); err != nil {
		return runtimepkg.Snapshot{}, fmt.Errorf("decode S7 ReviewPlan seed: %w", err)
	}
	// RC-17 PLANNER-REFINE gate lives on the normal RegisterPlan path
	// (ValidatePlan → rejectPlannerPlaceholders). The TR-012 handoff seed is
	// intentionally unrefined — `createS7ReviewPlanSeed` marks every substantive
	// Claim field with PLANNER-REFINE and the next action tells the Planner to
	// `runtime review-plan revise` once before dispatch. Validating the seed
	// through ValidatePlanArtifactForRegistration would reject every handoff.
	// The S7 revision gate, not the handoff installer, enforces refinement.
	newBaselineDigest := seedBaselineDigest(impactDocument.ChangedArtifacts)
	at := occurred(req.OccurredAt)
	actor := req.Actor
	if actor == "" {
		actor = "orchestrator"
	}
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	return func() (runtimepkg.Snapshot, error) {
		snapshot, updateErr := updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-handoff-r%d", commitRevision+1), TransitionID: whitelistChecked("TR-012"), Event: "repair_handoff_ready", Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:handoff:%s:%d", req.Handoff.Path, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.Handoff.Path, seedRef.Path}, From: cursor(current.State), To: map[string]any{"state": "verification", "phase": "running"}, RequestID: "s9-handoff", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-REPAIR-HANDOFF", GateFingerprint: "sha256:s9-repair-handoff-v3", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
			updateRepairPointer(state, map[string]any{"changeset_ref": handoff.ChangesetRef.Path, "changeset_sha256": handoff.ChangesetRef.SHA256, "handoff_ref": req.Handoff.Path, "handoff_sha256": req.Handoff.SHA256, "status": "closed", "updated_at": at.Format(time.RFC3339Nano), "next_action": "review the staged S7 seed; if coverage changes, refine it once with `runtime review-plan revise --file <plan-v2.json> --source-ref runtime:<change-impact-evidence-id> --affected-surface <surface>`, then dispatch Delivery + QA + E2E assignments", "review_plan_seed_ref": seedRef.Path, "review_plan_seed_sha256": seedRef.SHA256, "implementation_baseline_digest": newBaselineDigest})
			review := ensureObject(state, "review")
			round := integerValue(review["round"])
			round++
			review["round"] = round
			review["clean_round"] = nil
			review["plan"] = nil
			review["claims"] = map[string]any{}
			review["assignments"] = map[string]any{}
			review["observation_batch"] = nil
			review["investigation"] = nil
			review["round_entry"] = map[string]any{"transition_id": "TR-012", "round": round, "baseline_generation": baselineGeneration(state), "change_impact_ref": handoff.ChangeImpactRef.Path, "repair_handoff_ref": req.Handoff.Path, "repair_handoff_sha256": req.Handoff.SHA256, "implementation_baseline_digest": newBaselineDigest, "review_plan_seed_ref": seedRef.Path, "review_plan_seed_sha256": seedRef.SHA256}
			// Index the change_impact artifact as runtime evidence: the S7
			// repair-baseline gate resolves the seed's change_impact
			// source_ref against the evidence index (sandbox-verified: the
			// round could not otherwise re-open).
			indexHandoffEvidence(state, req.Handoff, handoff, baselineGeneration(state), round, at)
			return reviewpkg.ApplyRegisteredPlanProjection(state, seedPlan, seedRef.Path, seedRef.SHA256, round, "", "", at)
		}})
		if updateErr != nil {
			return runtimepkg.Snapshot{}, cleanupStagedArtifact(writer, req.ExpectedRevision, seedRef, current.State, updateErr)
		}
		return snapshot, nil
	}()
}

func readRepairRuntime(root, statePath, journalPath string) (runtimepkg.Snapshot, *runtimepkg.Store, error) {
	store := runtimepkg.NewStore(statePath, journalPath)
	snapshot, err := store.Snapshot()
	if err != nil {
		return runtimepkg.Snapshot{}, nil, err
	}
	return snapshot, runtimepkg.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}), nil
}

// repairAllowedTransitions lists every Loop Definition transition the repair
// package is allowed to journal, mapped from the internal emission site to
// the declared ID.
var repairAllowedTransitions = []string{
	"PTR-BUG-05", "PTR-BUG-06", "PTR-BUG-09", "PTR-BUG-10", "PTR-BUG-11", "PTR-BUG-12", "TR-012",
}

// s9RuntimeTransitionIDs maps the runtime emission sites that do not have a
// one-to-one catalog transition (checkpoint commits that keep the cursor) to
// their declared compatibility IDs. S9-SESSION-OPEN and S9-RESULT-SUBMIT
// keep the lifecycle cursor and S9-TARGETED-FAILURE routes via PTR-BUG-07 —
// these synthetic checkpoint IDs are accepted because they are pinned here,
// reviewed, and enumerated, not free-form.
var s9RuntimeTransitionIDs = map[string]bool{
	"S9-SESSION-OPEN":     true,
	"S9-RESULT-SUBMIT":    true,
	"S9-TARGETED-FAILURE": true,
}

// validateS9TransitionID is the RC-09 (S9-8) runtime whitelist check. It is
// called immediately before every repair writer.Update so an undeclared
// TransitionID fails closed at the emission site instead of silently entering
// the journal and corrupting audit traceability.
func validateS9TransitionID(transitionID string) error {
	for _, declared := range repairAllowedTransitions {
		if transitionID == declared {
			return nil
		}
	}
	if s9RuntimeTransitionIDs[transitionID] {
		return nil
	}
	return fmt.Errorf("TransitionID %q is not declared in docs/loop-definition.json; repair checkpoints may only journal whitelisted transitions (%s)",
		transitionID, strings.Join(append(append([]string{}, repairAllowedTransitions...), "S9-SESSION-OPEN", "S9-RESULT-SUBMIT", "S9-TARGETED-FAILURE"), ", "))
}

// whitelistChecked is the RC-09 (S9-8) emission-site guard for compile-time
// pinned TransitionID literals. Because the argument is a literal reviewed
// against docs/loop-definition.json, a whitelist violation is a programming
// error, not a runtime condition: it fails the process loudly rather than
// writing an undeclared transition into the journal.
func whitelistChecked(transitionID string) string {
	if err := validateS9TransitionID(transitionID); err != nil {
		panic(err)
	}
	return transitionID
}

// ValidateS9TransitionID exposes the RC-09 (S9-8) whitelist check for tests
// and tooling that need to verify a TransitionID is a declared repair
// emission before writing it anywhere.
func ValidateS9TransitionID(transitionID string) error {
	return validateS9TransitionID(transitionID)
}

func checkRevision(expected, actual int, next string) error {
	if expected < 0 {
		return nil
	}
	if expected != actual {
		return fmt.Errorf("%w: explicit expected revision %d does not match Runtime revision %d; next: %s", runtimepkg.ErrStaleRevision, expected, actual, next)
	}
	return nil
}
func occurred(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}
func mapField(state map[string]any, key string) map[string]any {
	value, _ := state[key].(map[string]any)
	return value
}
func ensureObject(state map[string]any, key string) map[string]any {
	value := mapField(state, key)
	if value == nil {
		value = map[string]any{}
		state[key] = value
	}
	return value
}
func stringField(value any) string {
	valueString, _ := value.(string)
	return strings.TrimSpace(valueString)
}
func lifecycleState(state map[string]any) string {
	return stringField(mapField(state, "lifecycle")["state"])
}
func lifecyclePhase(state map[string]any) string {
	return stringField(mapField(state, "lifecycle")["phase"])
}
func cursor(state map[string]any) map[string]any {
	life := mapField(state, "lifecycle")
	return map[string]any{"state": life["state"], "phase": life["phase"]}
}
func setLifecycle(state map[string]any, name, phase string) {
	life := ensureObject(state, "lifecycle")
	life["state"] = name
	life["phase"] = phase
	n, _ := life["phase_revision"].(float64)
	life["phase_revision"] = n + 1
}
func repairPointer(state map[string]any) map[string]any {
	return mapField(mapField(state, "review"), "repair")
}
func stateRepairPointer(state map[string]any) map[string]any { return repairPointer(state) }

func stringMapField(value any) map[string]string {
	result := map[string]string{}
	if raw, ok := value.(map[string]any); ok {
		for key, item := range raw {
			if text, ok := item.(string); ok {
				result[key] = text
			}
		}
	}
	return result
}

func existingArtifactRefs(raw any) []ArtifactRef {
	values, _ := raw.([]any)
	refs := make([]ArtifactRef, 0, len(values))
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		refs = append(refs, ArtifactRef{Path: stringField(item["path"]), SHA256: stringField(item["sha256"])})
	}
	return refs
}

func planReportForAssignment(root string, raw any, assignmentID string) (ArtifactRef, bool) {
	for _, ref := range existingArtifactRefs(raw) {
		if report, err := ValidatePlanReport(root, ref); err == nil && report.AssignmentID == assignmentID {
			return ref, true
		}
	}
	return ArtifactRef{}, false
}

func planReportAssignment(root string, ref ArtifactRef) string {
	if ref.Path == "" {
		return ""
	}
	report, err := ValidatePlanReport(root, ref)
	if err != nil {
		return ""
	}
	return report.AssignmentID
}
func investigationPointer(state map[string]any) map[string]any {
	return mapField(mapField(state, "review"), "investigation")
}
func updateRepairPointer(state map[string]any, values map[string]any) {
	p := repairPointer(state)
	if p == nil {
		p = map[string]any{}
		ensureObject(state, "review")["repair"] = p
	}
	for key, value := range values {
		p[key] = value
	}
}
func boundReqID(state map[string]any) string { return stringField(mapField(state, "bound_req")["id"]) }
func baselineGeneration(state map[string]any) int {
	value := mapField(state, "baseline")["generation"]
	switch n := value.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}
func integerValue(value any) int {
	switch n := value.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}
func stringSliceFromAny(value any) []string {
	if values, ok := value.([]string); ok {
		return append([]string(nil), values...)
	}
	raw, _ := value.([]any)
	result := []string{}
	for _, item := range raw {
		if value, ok := item.(string); ok {
			result = append(result, value)
		}
	}
	return result
}
func anySlice(value any) []any { values, _ := value.([]any); return values }
func appendUnique(values []string, value string) []string {
	for _, item := range values {
		if item == value {
			return values
		}
	}
	return append(values, value)
}

func pointerArtifact(pointer map[string]any, pathKey, hashKey, label string) (ArtifactRef, error) {
	ref := ArtifactRef{Path: stringField(pointer[pathKey]), SHA256: stringField(pointer[hashKey])}
	if ref.Path == "" || ref.SHA256 == "" {
		return ArtifactRef{}, fmt.Errorf("%s is missing from the current S9 Runtime pointer; recover the preceding S9 step before retrying", label)
	}
	return ref, nil
}

// validateCurrentRepairResult is the singular compatibility wrapper kept for
// callers that expect exactly one Assignment result. RC-15 (S9-H7/T2
// shadow-field convergence): the plural validateCurrentRepairResults is the
// single authority — it already accepts a single result_ref for legacy
// pointers — and every live S9 commit path calls the plural form directly.
func validateCurrentRepairResult(root string, state, pointer map[string]any) (RepairResult, error) {
	results, err := validateCurrentRepairResults(root, state, pointer)
	if err != nil {
		return RepairResult{}, err
	}
	if len(results) != 1 {
		return RepairResult{}, fmt.Errorf("current S9 Runtime has %d Assignment results; use batch validation", len(results))
	}
	return results[0], nil
}

// validateCurrentRepairResults validates the complete Assignment batch bound
// to the Runtime. A single result_ref is retained as a compatibility pointer,
// but result_refs is the authority once a plan has more than one Assignment.
func validateCurrentRepairResults(root string, state, pointer map[string]any) ([]RepairResult, error) {
	contractRef, err := pointerArtifact(pointer, "contract_ref", "contract_sha256", "current RepairContract")
	if err != nil {
		return nil, err
	}
	sessionRef, err := pointerArtifact(pointer, "path", "sha256", "current RepairSession")
	if err != nil {
		return nil, err
	}
	planRef, err := pointerArtifact(pointer, "plan_ref", "plan_sha256", "current RepairPlan")
	if err != nil {
		return nil, err
	}
	contract, err := ValidateApprovedContractRef(root, ContractRef{Path: contractRef.Path, SHA256: contractRef.SHA256})
	if err != nil {
		return nil, fmt.Errorf("current RepairContract is invalid: %w", err)
	}
	session, err := ValidateRepairSession(root, sessionRef)
	if err != nil {
		return nil, fmt.Errorf("current RepairSession is invalid: %w", err)
	}
	plan, err := ValidateRepairPlan(root, planRef)
	if err != nil {
		return nil, fmt.Errorf("current RepairPlan is invalid: %w", err)
	}
	if session.SessionID != stringField(pointer["session_id"]) || plan.SessionID != session.SessionID || plan.ContractID != contract.ContractID {
		return nil, errors.New("current S9 RepairSession/Plan chain is not transitively bound to the Runtime Contract pointer")
	}
	refs, err := currentRepairResultRefs(pointer)
	if err != nil {
		return nil, err
	}
	results := make([]RepairResult, 0, len(refs))
	seenAssignments := map[string]bool{}
	for index, ref := range refs {
		result, resultErr := ValidateRepairResult(root, ref)
		if resultErr != nil {
			return nil, fmt.Errorf("current RepairResult[%d] is invalid: %w", index, resultErr)
		}
		if result.SessionID != session.SessionID || result.PlanID != plan.PlanID || result.ContractID != contract.ContractID {
			return nil, errors.New("current S9 RepairSession/Plan/Result chain is not transitively bound to the Runtime Contract pointer")
		}
		if result.BaselineGeneration != baselineGeneration(state) {
			return nil, fmt.Errorf("current RepairResult baseline_generation %d does not match Runtime baseline_generation %d", result.BaselineGeneration, baselineGeneration(state))
		}
		assignment, ok := assignmentByID(plan.Assignments, result.AssignmentID)
		if !ok {
			return nil, fmt.Errorf("current RepairResult assignment %q is not in the current RepairPlan", result.AssignmentID)
		}
		if seenAssignments[result.AssignmentID] {
			return nil, fmt.Errorf("current Runtime has duplicate RepairResult for Assignment %s", result.AssignmentID)
		}
		seenAssignments[result.AssignmentID] = true
		unitIDs := make([]string, 0, len(result.UnitResults))
		for _, unit := range result.UnitResults {
			unitIDs = append(unitIDs, unit.UnitID)
		}
		if err := exactIDs(assignment.UnitIDs, unitIDs); err != nil {
			return nil, fmt.Errorf("RepairResult %s does not cover Assignment %s: %w", result.ResultID, result.AssignmentID, err)
		}
		results = append(results, result)
	}
	missing := []string{}
	for _, assignment := range plan.Assignments {
		if !seenAssignments[assignment.AssignmentID] {
			missing = append(missing, assignment.AssignmentID)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("current S9 RepairResult batch is incomplete; missing assignments: %s", strings.Join(missing, ", "))
	}
	return results, nil
}

func currentRepairResultRefs(pointer map[string]any) ([]ArtifactRef, error) {
	if raw, ok := pointer["result_refs"]; ok {
		if refs, err := artifactRefsFromAny(raw, "result_refs"); err == nil {
			return refs, nil
		}
	}
	ref, err := pointerArtifact(pointer, "result_ref", "result_sha256", "current RepairResult")
	if err != nil {
		return nil, err
	}
	return []ArtifactRef{ref}, nil
}

func repairResultBatchState(root string, plan RepairPlan, refs []any) (complete, allPass bool, missing []string, err error) {
	parsed, parseErr := artifactRefsFromAny(refs, "result_refs")
	if parseErr != nil {
		return false, false, assignmentIDs(plan.Assignments), nil
	}
	seen := map[string]bool{}
	allPass = true
	for index, ref := range parsed {
		result, resultErr := ValidateRepairResult(root, ref)
		if resultErr != nil {
			return false, false, nil, fmt.Errorf("RepairResult[%d] is invalid: %w", index, resultErr)
		}
		assignment, ok := assignmentByID(plan.Assignments, result.AssignmentID)
		if !ok {
			return false, false, nil, fmt.Errorf("RepairResult assignment %q is not in the current RepairPlan", result.AssignmentID)
		}
		unitIDs := make([]string, 0, len(result.UnitResults))
		for _, unit := range result.UnitResults {
			unitIDs = append(unitIDs, unit.UnitID)
		}
		if err := exactIDs(assignment.UnitIDs, unitIDs); err != nil {
			return false, false, nil, fmt.Errorf("RepairResult %s does not cover Assignment %s: %w", result.ResultID, result.AssignmentID, err)
		}
		if seen[result.AssignmentID] {
			return false, false, nil, fmt.Errorf("duplicate RepairResult for Assignment %s", result.AssignmentID)
		}
		seen[result.AssignmentID] = true
		if result.Result != "pass" {
			allPass = false
		}
	}
	for _, assignment := range plan.Assignments {
		if !seen[assignment.AssignmentID] {
			missing = append(missing, assignment.AssignmentID)
		}
	}
	return len(missing) == 0, allPass, missing, nil
}

func assignmentIDs(assignments []RepairAssignment) []string {
	ids := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		ids = append(ids, assignment.AssignmentID)
	}
	return ids
}

func aggregateRepairResultArtifacts(results []RepairResult) ([]ChangedArtifact, error) {
	byPath := map[string]ChangedArtifact{}
	for _, result := range results {
		for _, artifact := range result.ChangedArtifacts {
			path := normalizePath(artifact.Path)
			artifact.Path = path
			if prior, ok := byPath[path]; ok {
				if prior.SHA256 != artifact.SHA256 || prior.Status != artifact.Status {
					return nil, fmt.Errorf("RepairResult batch reports conflicting changes for %s", path)
				}
				continue
			}
			byPath[path] = artifact
		}
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	artifacts := make([]ChangedArtifact, 0, len(paths))
	for _, path := range paths {
		artifacts = append(artifacts, byPath[path])
	}
	return artifacts, nil
}

func exactChangedArtifactSet(changed []ChangedArtifact, refs []ArtifactRef, leftName, rightName string) error {
	left, err := changedArtifactSet(changed, leftName)
	if err != nil {
		return err
	}
	right, err := artifactRefSet(refs, rightName)
	if err != nil {
		return err
	}
	return compareArtifactSets(left, right, leftName, rightName)
}

func exactArtifactSet(left, right []ArtifactRef, leftName, rightName string) error {
	leftSet, err := artifactRefSet(left, leftName)
	if err != nil {
		return err
	}
	rightSet, err := artifactRefSet(right, rightName)
	if err != nil {
		return err
	}
	return compareArtifactSets(leftSet, rightSet, leftName, rightName)
}

func changedArtifactSet(values []ChangedArtifact, label string) (map[string]string, error) {
	result := map[string]string{}
	for _, value := range values {
		path := normalizePath(value.Path)
		if path == "." || value.SHA256 == "" {
			return nil, fmt.Errorf("%s contains %q without a sha256; every changed artifact must be content-bound", label, value.Path)
		}
		if _, exists := result[path]; exists {
			return nil, fmt.Errorf("%s contains duplicate artifact path %q", label, path)
		}
		status := value.Status
		if status == "" {
			status = "modified"
		}
		result[path] = value.SHA256 + ":" + status
	}
	return result, nil
}

func artifactRefSet(values []ArtifactRef, label string) (map[string]string, error) {
	result := map[string]string{}
	for _, value := range values {
		path := normalizePath(value.Path)
		if path == "." || value.SHA256 == "" {
			return nil, fmt.Errorf("%s contains %q without a sha256; every changed artifact must be content-bound", label, value.Path)
		}
		if _, exists := result[path]; exists {
			return nil, fmt.Errorf("%s contains duplicate artifact path %q", label, path)
		}
		// ArtifactRef.status is optional in ChangeImpact. An omitted status is
		// intentionally a wildcard; the authoritative status comes from the
		// Session diff or RepairResult when those are reconciled.
		result[path] = value.SHA256 + ":" + value.Status
	}
	return result, nil
}

func compareArtifactSets(left, right map[string]string, leftName, rightName string) error {
	missing := []string{}
	extra := []string{}
	for path, hash := range left {
		if !artifactSetEntryMatches(hash, right[path]) {
			missing = append(missing, path)
		}
	}
	for path, hash := range right {
		if !artifactSetEntryMatches(hash, left[path]) {
			extra = append(extra, path)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		sort.Strings(missing)
		sort.Strings(extra)
		return fmt.Errorf("exact changed-artifact set failed between %s and %s: missing_or_changed=%v extra_or_changed=%v", leftName, rightName, missing, extra)
	}
	return nil
}

func artifactSetEntryMatches(left, right string) bool {
	if left == right {
		return true
	}
	leftParts, rightParts := strings.SplitN(left, ":", 2), strings.SplitN(right, ":", 2)
	if len(leftParts) != 2 || len(rightParts) != 2 || leftParts[0] != rightParts[0] {
		return false
	}
	return leftParts[1] == "" || rightParts[1] == ""
}

func appendArtifactRefFromPointer(raw any, ref ArtifactRef) []any {
	values, _ := raw.([]any)
	out := append([]any(nil), values...)
	for _, value := range out {
		item, _ := value.(map[string]any)
		if item != nil && stringField(item["path"]) == ref.Path && stringField(item["sha256"]) == ref.SHA256 {
			return out
		}
	}
	return append(out, map[string]any{"path": ref.Path, "sha256": ref.SHA256})
}

func artifactRefsFromAny(raw any, label string) ([]ArtifactRef, error) {
	values, ok := raw.([]any)
	if !ok || len(values) == 0 {
		return nil, fmt.Errorf("%s is missing from the current S9 Runtime pointer; targeted reverification must be committed again before handoff", label)
	}
	refs := make([]ArtifactRef, 0, len(values))
	for index, rawValue := range values {
		item, ok := rawValue.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s[%d] is not an artifact reference", label, index)
		}
		refs = append(refs, ArtifactRef{Path: stringField(item["path"]), SHA256: stringField(item["sha256"])})
	}
	return refs, nil
}

func validateHandoffAgainstCurrentRepair(root string, state, pointer map[string]any, handoff RepairHandoff) error {
	checks := []struct {
		label string
		got   ArtifactRef
		path  string
		hash  string
	}{
		{"RepairSession", handoff.SessionRef, "path", "sha256"},
		{"RepairPlan", handoff.PlanRef, "plan_ref", "plan_sha256"},
		{"RepairContract", handoff.ContractRef, "contract_ref", "contract_sha256"},
		{"RepairResult", handoff.ResultRef, "result_ref", "result_sha256"},
		{"ChangeImpact", handoff.ChangeImpactRef, "impact_ref", "impact_sha256"},
	}
	for _, check := range checks {
		want, err := pointerArtifact(pointer, check.path, check.hash, "current "+check.label)
		if err != nil {
			return err
		}
		if want.Path != check.got.Path || want.SHA256 != check.got.SHA256 {
			return fmt.Errorf("RepairHandoff %s reference %s/%s is not the current Runtime-bound %s %s/%s", check.label, check.got.Path, check.got.SHA256, check.label, want.Path, want.SHA256)
		}
	}
	currentTargets, err := artifactRefsFromAny(pointer["targeted_reverification_artifacts"], "targeted_reverification_artifacts")
	if err != nil {
		return err
	}
	if err := exactArtifactSet(currentTargets, handoff.TargetedReverificationRefs, "Runtime targeted reverifications", "RepairHandoff targeted reverifications"); err != nil {
		return err
	}
	// RC-09 (S9-6): required_reverification_ids consumer gate. Every ID the
	// ChangeImpact declared as a required reverification must have been
	// committed to the Runtime before the handoff can release the bug.
	committedIDs := map[string]bool{}
	for _, ref := range currentTargets {
		if value, valueErr := ValidateTargetedReverification(root, ref); valueErr == nil {
			committedIDs[value.ReverificationID] = true
		}
	}
	missingRequired := []string{}
	for _, id := range stringSliceFromAny(pointer["required_reverification_ids"]) {
		if !committedIDs[id] {
			missingRequired = append(missingRequired, id)
		}
	}
	if len(missingRequired) > 0 {
		return fmt.Errorf("S9 handoff is blocked: ChangeImpact required_reverification_ids not committed: %s", strings.Join(missingRequired, ", "))
	}
	results, err := validateCurrentRepairResults(root, state, pointer)
	if err != nil {
		return err
	}
	resultArtifacts, err := aggregateRepairResultArtifacts(results)
	if err != nil {
		return err
	}
	changeset, err := ValidateChangeset(root, handoff.ChangesetRef)
	if err != nil {
		return err
	}
	impact, err := ValidateChangeImpact(root, handoff.ChangeImpactRef)
	if err != nil {
		return err
	}
	if err := validateChangeImpactEvidenceLedger(root, state, pointer, impact, true); err != nil {
		return err
	}
	if err := exactChangedArtifactSet(resultArtifacts, changeset.Artifacts, "RepairResult batch", "Changeset"); err != nil {
		return err
	}
	return exactChangedArtifactSet(resultArtifacts, impact.ChangedArtifacts, "RepairResult batch", "ChangeImpact")
}

func cleanupStagedArtifact(writer *runtimepkg.Store, expectedRevision int, ref ArtifactRef, state map[string]any, operationErr error) error {
	if writer == nil || ref.Path == "" || ref.SHA256 == "" {
		return operationErr
	}
	if expectedRevision < 0 {
		current, err := writer.Snapshot()
		if err != nil {
			return fmt.Errorf("%w; staged artifact %s could not be safely cleaned: read current Runtime: %v", operationErr, ref.Path, err)
		}
		expectedRevision = current.Revision
	}
	_, cleanupErr := writer.RemoveUnreferencedArtifact(runtimepkg.ArtifactCleanupRequest{ExpectedRevision: expectedRevision, ArtifactPath: ref.Path, ArtifactSHA256: ref.SHA256, ReferencedPaths: stateArtifactPaths(state)})
	if cleanupErr != nil {
		return fmt.Errorf("%w; staged artifact %s could not be safely cleaned: %v", operationErr, ref.Path, cleanupErr)
	}
	return operationErr
}

func stateArtifactPaths(state map[string]any) []string {
	seen := map[string]bool{}
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for _, child := range typed {
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		case string:
			if strings.HasPrefix(filepath.ToSlash(typed), ".claude/") {
				seen[filepath.ToSlash(typed)] = true
			}
		}
	}
	visit(state)
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func invalidateListedEvidence(state map[string]any, ids []string, by, reason string) {
	markEvidence(state, ids, "invalid", by, reason)
}
func supersedeListedEvidence(state map[string]any, ids []string, by string) {
	markEvidence(state, ids, "superseded", by, "declared by ChangeImpact")
}
func markEvidence(state map[string]any, ids []string, status, by, reason string) {
	if len(ids) == 0 {
		return
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	evidence, _ := state["evidence"].([]any)
	for _, raw := range evidence {
		entry, _ := raw.(map[string]any)
		if entry == nil || !wanted[stringField(entry["id"])] {
			continue
		}
		if stringField(entry["status"]) == "invalid" && status == "superseded" {
			continue
		}
		entry["status"] = status
		entry["invalidated_by"] = by
		entry["invalidation_reason"] = reason
		entry["invalidation_rule"] = "change_impact_" + status
	}
}

// indexHandoffEvidence registers the S9 repair artifacts the next S7 round
// consumes as gates: the repair handoff, change_impact (repair-baseline ref),
// and targeted re-verifications (TR-012 evidence). Rows mirror the runtime
// evidence index shape so qualifiedEvidence can hash-verify them.
func indexHandoffEvidence(state map[string]any, handoffRef ArtifactRef, handoff RepairHandoff, generation, round int, at time.Time) {
	existing, _ := state["evidence"].([]any)
	known := map[string]bool{}
	for _, raw := range existing {
		if entry, _ := raw.(map[string]any); entry != nil {
			known[stringField(entry["id"])] = true
		}
	}
	add := func(id, kind, path, sha, responsibility string) {
		if id == "" || sha == "" || known[id] {
			return
		}
		existing = append(existing, map[string]any{
			"id": id, "kind": kind, "path": path, "sha256": sha,
			"status": "valid", "baseline_generation": generation, "review_round": round,
			"responsibility_id": responsibility, "produced_by": []any{"s9-handoff"},
			"scope_refs": []any{}, "invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil,
		})
	}
	impactSHA := handoff.ChangeImpactRef.SHA256
	add(filepath.Base(handoffRef.Path), "repair_handoff", handoffRef.Path, handoffRef.SHA256, "S9 Repair")
	add(filepath.Base(handoff.ChangeImpactRef.Path), "change_impact", handoff.ChangeImpactRef.Path, impactSHA, "S9 Repair")
	for _, ref := range handoff.TargetedReverificationRefs {
		add(filepath.Base(ref.Path), "targeted_reverification", ref.Path, ref.SHA256, "Original Finder")
	}
	state["evidence"] = existing
}
