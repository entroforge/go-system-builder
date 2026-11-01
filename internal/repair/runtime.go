package repair

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
	reviewpkg "github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/targetedauthoring"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

type RuntimeRequest struct {
	Context          context.Context `json:"-"`
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
type submitIssuedPlanReportRequest struct {
	RuntimeRequest
	Report ArtifactRef
	// issued is a private capability installed only by the issued S9 packet
	// adapter. An artifact carrying a receipt cannot call the original writer
	// without this in-memory authorization and same-CAS revalidation.
	issued *issuedPlanReportCapability
}
type BeginRepairExecutionRequest struct {
	RuntimeRequest
}
type submitIssuedResultRuntimeRequest struct {
	RuntimeRequest
	Result RepairResultRequest
	issued *issuedRepairResultFinalCapability
}
type CommitImpactRequest struct {
	RuntimeRequest
	Impact ArtifactRef
	Record *ChangeImpact `json:"-"`
}
type CommitTargetedRequest struct {
	RuntimeRequest
	Reverification ArtifactRef
	Record         *TargetedReverification `json:"-"`
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
	current, writer, err := readRepairRuntime(root, statePath, journalPath, req.Context)
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
			// Defect #26 recovery: the repair pointer only ever clears through a
			// fresh open, so a closed session pinned by a previous repair would
			// block every later approved contract forever. Allow a fresh session
			// when the pinned session is closed, the cursor sits at
			// bug_resolution.repair_readback, and a DIFFERENT InvestigationCase
			// has an approved contract.
			closed := stringField(existing["status"]) == "closed"
			readback := lifecycleState(current.State) == "bug_resolution" && lifecyclePhase(current.State) == "repair_readback"
			prior := investigationPointer(current.State)
			newContract := prior != nil && stringField(prior["status"]) == "contract_approved" && stringField(prior["case_id"]) != stringField(existing["case_id"])
			// A session without an issued or received responsibility can be
			// superseded while preserving its original artifacts and work. Missing
			// receipts do not establish that the working tree is unchanged.
			voidEmpty := stringField(existing["result_ref"]) == "" && stringField(existing["changeset_ref"]) == "" && stringField(existing["handoff_ref"]) == "" && lifecycleState(current.State) == "bug_resolution"
			if voidEmpty && !(closed && readback && newContract) {
				if err := validateEmptySessionReplacement(root, existing); err != nil {
					return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, err
				}
			}
			if !(closed && readback && newContract) && !voidEmpty {
				return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, errors.New("an active S9 RepairSession already exists; inspect runtime repair status")
			}
		} else {
			ref := ArtifactRef{ID: req.SessionID, Path: stringField(existing["path"])}
			session, err := ValidateRepairSession(root, ref)
			if err != nil {
				return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, err
			}
			return current, session, ref, nil
		}
	}
	if lifecycleState(current.State) != "bug_resolution" || lifecyclePhase(current.State) != "repair_readback" {
		// An unissued session may be superseded from the fixing phase without
		// assuming that no work was done or altering the original workspace.
		stale := repairPointer(current.State)
		voidEmpty := stale != nil && stringField(stale["session_id"]) != req.SessionID && stringField(stale["result_ref"]) == "" && stringField(stale["changeset_ref"]) == "" && stringField(stale["handoff_ref"]) == "" && lifecycleState(current.State) == "bug_resolution"
		if !voidEmpty {
			return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, errors.New("S9 session open requires bug_resolution.repair_readback; consume the approved Contract first")
		}
	}
	investigation := investigationPointer(current.State)
	if investigation == nil || stringField(investigation["status"]) != "contract_approved" {
		return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, errors.New("S9 session open requires review.investigation status=contract_approved")
	}
	contract, err := ValidateApprovedContractRef(root, ContractRef{Path: stringField(investigation["repair_contract_ref"])})
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
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	snapshot, err := updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-session-open-%s-r%d", req.SessionID, commitRevision+1), TransitionID: whitelistChecked("S9-SESSION-OPEN"), Event: "repair_session_opened", Actor: req.Actor, IdempotencyKey: fmt.Sprintf("runtime:s9:session-open:%s:%d", req.SessionID, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.SessionID, contract.ContractID}, From: cursor(current.State), To: cursor(current.State), RequestID: "s9-session-open", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-SESSION-OPEN", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
		ensureObject(state, "review")["repair"] = map[string]any{"session_id": session.SessionID, "case_id": contract.CaseID, "contract_id": contract.ContractID, "contract_ref": contract.Ref.Path, "path": ref.Path, "questionnaire_root": QuestionnaireRoot(stringField(state["runtime_id"]), session.SessionID), "revision": 1, "status": "contract_ready", "updated_at": at.Format(time.RFC3339Nano), "next_action": "runtime repair plan compile --root <root> --plan-id <plan> --created-by <agent>"}
		return nil
	}})
	if err != nil {
		return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, cleanupStagedArtifact(writer, req.ExpectedRevision, ref, current.State, err)
	}
	return snapshot, session, ref, nil
}

func CompileRepairPlan(root, statePath, journalPath string, req CompilePlanRequest) (runtimepkg.Snapshot, RepairPlan, ArtifactRef, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath, req.Context)
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
	contractRef := ContractRef{Path: stringField(p["contract_ref"])}
	sessionRef := ArtifactRef{ID: stringField(p["session_id"]), Path: stringField(p["path"])}
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
	snapshot, err := updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-plan-%s-r%d", plan.PlanID, commitRevision+1), TransitionID: whitelistChecked("PTR-BUG-09"), Event: "repair_plan_compiled", Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:plan:%s:%d", plan.PlanID, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{plan.PlanID}, From: cursor(current.State), To: map[string]any{"state": "bug_resolution", "phase": "planning"}, RequestID: "s9-plan-compile", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-PLAN-COMPILE", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
		updateRepairPointer(state, map[string]any{"plan_ref": ref.Path, "status": "planning", "updated_at": at.Format(time.RFC3339Nano), "next_action": "record each Builder plan and actual precheck before that Builder implements"})
		setLifecycle(state, "bug_resolution", "planning")
		return nil
	}})
	if err != nil {
		return runtimepkg.Snapshot{}, RepairPlan{}, ArtifactRef{}, cleanupStagedArtifact(writer, req.ExpectedRevision, ref, current.State, err)
	}
	return snapshot, plan, ref, nil
}

// submitIssuedRepairPlanReportToRuntime records a Builder's issued implementation plan.
// Other Builders may already be implementing their recorded plans.
func submitIssuedRepairPlanReportToRuntime(root, statePath, journalPath string, req submitIssuedPlanReportRequest) (runtimepkg.Snapshot, PlanReport, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath, req.Context)
	if err != nil {
		return runtimepkg.Snapshot{}, PlanReport{}, err
	}
	p := repairPointer(current.State)
	if p == nil || p["session_id"] == "" {
		return current, PlanReport{}, errors.New("S9 PlanReport requires a current repair responsibility")
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(req.Report.Path)))
	if err != nil {
		return current, PlanReport{}, err
	}
	var report PlanReport
	if err := json.Unmarshal(data, &report); err != nil {
		return current, report, err
	}
	if report.SessionID != stringField(p["session_id"]) || report.AssignmentID == "" || report.AgentID == "" ||
		(stringMapField(p["assignment_owners"])[report.AssignmentID] != "" && stringMapField(p["assignment_owners"])[report.AssignmentID] != report.AgentID) {
		return current, report, errors.New("PlanReport does not correspond to the current repair responsibility")
	}
	if req.issued == nil {
		return current, report, errors.New("S9 Plan requires its issued responsibility")
	}
	binding := req.issued.packet.Binding
	if req.Report.Path != req.issued.receipt.ReportRef || report.ReportID != req.issued.packet.ReportID ||
		report.PlanID != binding.PlanID || report.SessionID != binding.SessionID ||
		report.AssignmentID != binding.RepairAssignmentID || report.AgentID != binding.OwnerAgentID ||
		stringField(p["plan_ref"]) != binding.PlanRef {
		return current, report, errors.New("PlanReport path or responsibility differs from the current packet")
	}
	for _, ref := range existingArtifactRefs(p["plan_report_refs"]) {
		if ref.Path == req.Report.Path {
			return current, report, nil
		}
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair plan report"); err != nil {
		return current, report, err
	}
	at := occurred(req.OccurredAt)
	actor := req.Actor
	if actor == "" {
		actor = report.AgentID
	}
	if actor != report.AgentID {
		return current, report, errors.New("PlanReport owner differs from its current responsibility")
	}
	nextStatus, nextCursor := "repairing", map[string]any{"state": "bug_resolution", "phase": "fixing"}
	if lifecycleState(current.State) != "bug_resolution" || (p["status"] != "planning" && p["status"] != "reproducing" && p["status"] != "repairing") {
		nextStatus, nextCursor = stringField(p["status"]), cursor(current.State)
	}
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	eventID := fmt.Sprintf("evt-s9-plan-report-%s-r%d", report.ReportID, commitRevision+1)
	idempotencyKey := fmt.Sprintf("runtime:s9:plan-report:%s:%d", report.ReportID, commitRevision)
	if req.issued != nil {
		eventID = req.issued.receipt.AcceptedEventID
		idempotencyKey = req.issued.receipt.AcceptedIdempotencyKey
	}
	snapshot, err := updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{
		EventID: eventID, TransitionID: whitelistChecked("PTR-BUG-10"), Event: "repair_plan_reported", Actor: actor,
		IdempotencyKey: idempotencyKey, RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.Report.Path},
		From: cursor(current.State), To: nextCursor,
		RequestID: "s9-plan-report", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-PLAN-REPORT",
		ProducerResponsibility: "S9 Repair", OccurredAt: at,
		Apply: func(state map[string]any) error {
			latest := stateRepairPointer(state)
			if latest == nil || state["runtime_id"] != current.State["runtime_id"] || baselineGeneration(state) != baselineGeneration(current.State) ||
				latest["session_id"] != p["session_id"] || latest["plan_ref"] != p["plan_ref"] ||
				(stringMapField(latest["assignment_owners"])[report.AssignmentID] != "" && stringMapField(latest["assignment_owners"])[report.AssignmentID] != report.AgentID) {
				return errors.New("current Plan responsibility changed before recording")
			}
			refs := existingArtifactRefs(latest["plan_report_refs"])
			for _, ref := range refs {
				if ref.Path == req.Report.Path {
					return runtimepkg.ErrSubmissionConflict
				}
			}
			merged := appendArtifactRefFromPointer(latest["plan_report_refs"], req.Report)
			owners := stringMapField(latest["assignment_owners"])
			owners[report.AssignmentID] = report.AgentID
			updateRepairPointer(state, map[string]any{"plan_report_ref": req.Report.Path,
				"plan_report_refs": merged, "assignment_owners": owners, "assignment_id": report.AssignmentID, "status": nextStatus,
				"updated_at": at.Format(time.RFC3339Nano), "next_action": "continue this Builder's original implementation responsibility at its recorded work location; preserve its existing dispatch and return its actual RepairResult through the original receiver; Main retains integration and necessary checks"})
			if nextStatus == "repairing" {
				setLifecycle(state, "bug_resolution", "fixing")
			}
			return nil
		},
	})
	if err != nil {
		return current, report, cleanupStagedArtifact(writer, req.ExpectedRevision, req.Report, current.State, err)
	}
	return snapshot, report, nil
}

// BeginRepairExecution records normal work startup after a Builder PlanReport.
func BeginRepairExecution(root, statePath, journalPath string, req BeginRepairExecutionRequest) (runtimepkg.Snapshot, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath, req.Context)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	p := repairPointer(current.State)
	if p != nil && p["status"] == "repairing" && lifecyclePhase(current.State) == "fixing" {
		return current, nil
	}
	if p == nil || lifecycleState(current.State) != "bug_resolution" ||
		(p["status"] != "planning" && p["status"] != "reproducing") || stringField(p["plan_report_ref"]) == "" {
		return current, errors.New("normal work startup awaits a recorded Builder Plan; no sibling Plan barrier applies")
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair execution"); err != nil {
		return current, err
	}
	at := occurred(req.OccurredAt)
	actor := req.Actor
	if actor == "" {
		actor = "Main"
	}
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	return updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-execution-begin-r%d", commitRevision+1),
		TransitionID: whitelistChecked("PTR-BUG-11"), Event: "repair_execution_started", Actor: actor,
		IdempotencyKey: fmt.Sprintf("runtime:s9:execution:%s", stringField(p["session_id"])), RuntimeID: stringField(current.State["runtime_id"]),
		EvidenceIDs: []string{stringField(p["plan_report_ref"])}, From: cursor(current.State), To: map[string]any{"state": "bug_resolution", "phase": "fixing"},
		RequestID: "s9-execution-begin", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-EXECUTION-BEGIN",
		ProducerResponsibility: "S9 Repair", OccurredAt: at,
		Apply: func(state map[string]any) error {
			latest := stateRepairPointer(state)
			if latest == nil || latest["session_id"] != p["session_id"] || latest["plan_ref"] != p["plan_ref"] ||
				state["runtime_id"] != current.State["runtime_id"] || baselineGeneration(state) != baselineGeneration(current.State) ||
				lifecycleState(state) != "bug_resolution" || stringField(latest["plan_report_ref"]) == "" ||
				(latest["status"] != "planning" && latest["status"] != "reproducing") {
				return errors.New("current repair work changed before startup fact was recorded")
			}
			updateRepairPointer(state, map[string]any{"status": "repairing", "updated_at": at.Format(time.RFC3339Nano), "next_action": "continue the recorded Builder's original implementation responsibility at its recorded work location; preserve its existing dispatch and return its actual Result through the original receiver"})
			setLifecycle(state, "bug_resolution", "fixing")
			return nil
		}})
}

func submitIssuedRepairResultToRuntime(root, statePath, journalPath string, req submitIssuedResultRuntimeRequest) (runtimepkg.Snapshot, RepairResult, ArtifactRef, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath, req.Context)
	if err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
	}
	if req.issued == nil || req.ExpectedRevision < 0 || req.Result.issuedFinal != req.issued {
		return current, RepairResult{}, ArtifactRef{}, errors.New("S9 Result requires its issued responsibility and Runtime Writer")
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
	}
	p := repairPointer(current.State)
	if p == nil || stringField(p["status"]) != "repairing" {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, errors.New("S9 result submit requires status=repairing; compile a plan first")
	}
	req.Result.Contract = ContractRef{Path: stringField(p["contract_ref"])}
	req.Result.Session = ArtifactRef{ID: stringField(p["session_id"]), Path: stringField(p["path"])}
	req.Result.Plan = ArtifactRef{ID: stringField(p["plan_ref"]), Path: stringField(p["plan_ref"])}
	var plan RepairPlan
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(stringField(p["plan_ref"]))))
	if err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
	}
	if err := json.Unmarshal(data, &plan); err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
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
	resultRefs, err := repairResultPointerRows(p, ref, root)
	if err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
	}
	nextStatus := "repairing"
	nextAction := "submit one exact-unit RepairResult for every RepairAssignment before committing ChangeImpact"
	batchState := func(refs []any) (bool, bool, []string, error) {
		done, passing := map[string]bool{}, true
		for _, ref := range existingArtifactRefs(refs) {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
			if err != nil {
				return false, false, nil, err
			}
			var recorded RepairResult
			if err := json.Unmarshal(data, &recorded); err != nil {
				return false, false, nil, err
			}
			done[recorded.AssignmentID] = true
			if recorded.Result != "pass" {
				passing = false
			}
		}
		missing := []string{}
		for _, assignment := range plan.Assignments {
			if !done[assignment.AssignmentID] {
				missing = append(missing, assignment.AssignmentID)
			}
		}
		return len(missing) == 0, passing, missing, nil
	}
	if complete, allPass, missing, batchErr := batchState(resultRefs); batchErr != nil {
		return current, RepairResult{}, ArtifactRef{}, batchErr
	} else {
		nextStatus, nextAction = repairResultNextState(complete, allPass, missing)
	}
	eventID := req.issued.receipt.AcceptedEventID
	idempotencyKey := req.issued.receipt.AcceptedIdempotencyKey
	evidenceIDs := []string{ref.Path, req.issued.receipt.CandidateRef, req.issued.packet.ID, req.issued.packet.Binding.Sources.RepairAssignmentID}
	snapshot, err := updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: eventID, TransitionID: whitelistChecked("S9-RESULT-SUBMIT"), Event: "repair_result_submitted", Actor: actor, IdempotencyKey: idempotencyKey, RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: evidenceIDs, From: cursor(current.State), To: cursor(current.State), RequestID: "s9-result-submit", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-RESULT-SUBMIT", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
		applyRefs, applyStatus, applyAction := resultRefs, nextStatus, nextAction
		{
			if err := req.issued.validate(root, state, req.Result); err != nil {
				return fmt.Errorf("typed S9 Result original-writer CAS revalidation: %w", err)
			}
			freshPointer := repairPointer(state)
			if freshPointer == nil {
				return errors.New("S9 repair pointer disappeared before typed Result CAS")
			}
			applyRefs, err = repairResultPointerRows(freshPointer, ref, root)
			if err != nil {
				return err
			}
			complete, allPass, missing, err := batchState(applyRefs)
			if err != nil {
				return err
			}
			applyStatus, applyAction = repairResultNextState(complete, allPass, missing)
		}
		updateRepairPointer(state, map[string]any{"result_ref": ref.Path, "result_refs": applyRefs, "status": applyStatus, "updated_at": at.Format(time.RFC3339Nano), "next_action": applyAction})
		return nil
	}})
	if err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
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
	results := map[string]RepairResult{}
	for _, ref := range existingArtifactRefs(pointer["result_refs"]) {
		result, err := ValidateRepairResult(root, ref)
		if err != nil {
			return fmt.Errorf("validate existing S9 Result %s before dispatch: %w", ref.Path, err)
		}
		results[result.AssignmentID] = result
	}
	return RepairAssignmentQueueReady(plan, target, results, func(assignment string) bool {
		for _, ref := range existingArtifactRefs(pointer["plan_report_refs"]) {
			if report, err := ValidatePlanReport(root, ref); err == nil && report.AssignmentID == assignment {
				return true
			}
		}
		return false
	})
}

// RepairAssignmentQueueReady observes the existing dependency/lock queue from
// already read facts. It does not validate sources, authorize an Agent or
// prepare an execution; the original consumer retains those responsibilities.
func RepairAssignmentQueueReady(plan RepairPlan, target RepairAssignment, results map[string]RepairResult, reported func(string) bool) error {
	unitOwners := map[string]RepairAssignment{}
	for _, assignment := range plan.Assignments {
		for _, unitID := range assignment.UnitIDs {
			unitOwners[unitID] = assignment
		}
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
		if !reported(sibling.AssignmentID) {
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

// BindTargetedReverificationIdentitiesForTest exposes the RC-01/RC-15
// identity-binding gate for tests that need to exercise owner-binding
// semantics against a synthetic pointer.
func BindTargetedReverificationIdentitiesForTest(p map[string]any, plan RepairPlan, value TargetedReverification) error {
	return bindTargetedReverificationIdentities(p, plan, value)
}

// requiredReverificationIDs projects the existing impact obligations for
// current work reminders; it does not establish an exact-set acceptance gate.
func requiredReverificationIDs(impact ChangeImpact) []any {
	outstanding := make([]any, 0, len(impact.RequiredReverificationIDs))
	for _, id := range impact.RequiredReverificationIDs {
		outstanding = append(outstanding, id)
	}
	return outstanding
}

// CommitChangeImpact records the current professional impact judgment and
// projects the existing next responsibility from already recorded work facts.
func CommitChangeImpact(root, statePath, journalPath string, req CommitImpactRequest) (runtimepkg.Snapshot, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath, req.Context)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	p := repairPointer(current.State)
	if p == nil || stringField(p["session_id"]) == "" {
		return runtimepkg.Snapshot{}, errors.New("S9 impact commit requires the current repair or impact responsibility")
	}
	var impactDocument ChangeImpact
	if req.Record != nil {
		impactDocument = *req.Record
	} else {
		data, readErr := workpacket.ReadFile(root, req.Impact.Path, 20<<20)
		if readErr != nil {
			return current, readErr
		}
		if err := json.Unmarshal(data, &impactDocument); err != nil {
			return current, err
		}
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
	// This is Main's impact judgment, not a certificate of integration or
	// completion. Independent verification can start while original Builders
	// continue; no Candidate is promoted to a Final by reading this answer.
	at := occurred(req.OccurredAt)
	nextPhase, nextAction := "targeted_reverification", "Main continues actual integration and checks, any unfinished Builder work, and independent targeted verification; Impact reception certifies none of these."
	lifecyclePhase := nextPhase
	// A late judgment belongs to the original repair without rewinding a new
	// S7 round or erasing its already recorded professional work.
	late := lifecycleState(current.State) != "bug_resolution"
	if late {
		nextPhase = stringField(p["status"])
		// A late Main judgment must not replace the next-stage guidance with
		// stale instructions to repeat an already completed S9 verification.
		nextAction = stringField(p["next_action"])
	}

	actor := req.Actor
	if actor == "" {
		actor = "orchestrator"
	}
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	return updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-impact-r%d", commitRevision+1), TransitionID: whitelistChecked("PTR-BUG-05"), Event: "change_impact_reconciled", Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:impact:%s:%d", req.Impact.Path, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.Impact.Path}, From: cursor(current.State), To: impactCursor(current.State, late, lifecyclePhase), RequestID: "s9-impact-reconcile", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-IMPACT-RECONCILE", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {

		updateRepairPointer(state, map[string]any{"impact_ref": req.Impact.Path, "status": nextPhase, "required_reverification_ids": requiredReverificationIDs(impactDocument), "updated_at": at.Format(time.RFC3339Nano), "next_action": nextAction})
		invalidateListedEvidence(state, impactDocument.InvalidatedEvidenceIDs, impactDocument.ImpactID, "declared by ChangeImpact")
		supersedeListedEvidence(state, impactDocument.SupersededEvidenceIDs, impactDocument.ImpactID)
		if !late {
			setLifecycle(state, "bug_resolution", lifecyclePhase)
		}
		return nil
	}})
}

func impactCursor(state map[string]any, late bool, phase string) map[string]any {
	if late {
		return cursor(state)
	}
	return map[string]any{"state": "bug_resolution", "phase": phase}
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
	current, writer, err := readRepairRuntime(root, statePath, journalPath, req.Context)
	if err != nil {
		return current, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return current, err
	}
	p := repairPointer(current.State)
	if p == nil {
		return current, errors.New("S9 targeted reverification requires the current verification responsibility")
	}
	var value TargetedReverification
	if req.Record != nil {
		value = *req.Record
	} else {
		value, err = ValidateTargetedReverification(root, req.Reverification)
		if err != nil {
			return current, err
		}
	}
	status := stringField(p["status"])
	late := status == "closed" && lifecycleState(current.State) != "bug_resolution"
	if !late && status != "targeted_reverification" && status != "ready_for_full_review" && !(status == "blocked" && stringField(p["failure_route"]) == "blocked") {
		return current, errors.New("S9 targeted reverification requires the current verification responsibility")
	}
	if value.RuntimeID != stringField(current.State["runtime_id"]) || value.BaselineGeneration != baselineGeneration(current.State) {
		return current, errors.New("Targeted report is not associated with the current responsibility")
	}
	owners := stringMapField(p["assignment_owners"])
	actor := dispatchedVerifierAgentID(value.PerformingAssignmentID, owners)
	if actor == "" {
		return current, errors.New("Targeted performing_assignment_id is not a dispatched verifier identity in the current responsibility; Main must resolve its original assignment owner")
	}
	if requested := strings.TrimSpace(req.Actor); requested != "" && requested != actor {
		return current, errors.New("Targeted actor does not match its recorded responsibility owner; Main must resolve the original handoff")
	}
	// Route the recorded conclusion for each dispatched responsibility. The
	// verifier and Main own independence and actual assertion coverage.
	refs := appendUnique(stringSliceFromAny(p["targeted_reverification_refs"]), req.Reverification.Path)
	targetedArtifacts := appendArtifactRefFromPointer(p["targeted_reverification_artifacts"], req.Reverification)
	if late {
		// Only an already dispatched, exact original responsibility may reach
		// this branch. A late result is historical work for Main, never a reason
		// to rewind the new stage or repeat the completed S9 handoff.
		for _, raw := range anySlice(p["targeted_reverification_artifacts"]) {
			row, _ := raw.(map[string]any)
			if stringField(row["path"]) == req.Reverification.Path {
				return current, nil
			}
		}
		for i, raw := range targetedArtifacts {
			row, _ := raw.(map[string]any)
			if stringField(row["path"]) == req.Reverification.Path {
				row = copyRepairReceiptRow(row)
				row["performing_assignment_id"], row["result"] = value.PerformingAssignmentID, value.Result
				targetedArtifacts[i] = row
			}
		}
		at := occurred(req.OccurredAt)
		commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
		return updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{
			EventID: fmt.Sprintf("evt-s9-targeted-r%d", commitRevision+1), TransitionID: whitelistChecked("PTR-BUG-06"),
			Event: "targeted_reverification_recorded", Actor: actor,
			IdempotencyKey: fmt.Sprintf("runtime:s9:targeted:%s:%d", req.Reverification.Path, commitRevision),
			RuntimeID:      stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.Reverification.Path},
			From: cursor(current.State), To: cursor(current.State), RequestID: "s9-targeted-reverification",
			BaselineGeneration: baselineGeneration(current.State), GateID: "S9-TARGETED-REVERIFICATION",
			ProducerResponsibility: "S9 QA", OccurredAt: at,
			Apply: func(state map[string]any) error {
				updateRepairPointer(state, map[string]any{"targeted_reverification_refs": refs, "targeted_reverification_artifacts": targetedArtifacts, "updated_at": at.Format(time.RFC3339Nano)})
				return nil
			},
		})
	}
	// Consume the original accepted conclusions without reopening reports.
	conclusions := map[string]string{}
	for _, raw := range anySlice(p["targeted_reverification_artifacts"]) {
		row, _ := raw.(map[string]any)
		id, result := stringField(row["performing_assignment_id"]), stringField(row["result"])
		if id != "" && result != "" {
			conclusions[canonicalTargetedAssignment(id)] = result
		}
	}
	performing := canonicalTargetedAssignment(value.PerformingAssignmentID)
	for _, raw := range targetedArtifacts {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		if stringField(row["path"]) == req.Reverification.Path {
			// Keep the direct conclusion on its original accepted reference,
			// never in a second ledger or a copy of the professional report.
			row = copyRepairReceiptRow(row)
			row["performing_assignment_id"], row["result"] = value.PerformingAssignmentID, value.Result
			for i, prior := range targetedArtifacts {
				entry, _ := prior.(map[string]any)
				if stringField(entry["path"]) == req.Reverification.Path {
					targetedArtifacts[i] = row
				}
			}
		}
	}
	conclusions[performing] = value.Result
	missing, unknownResponsibilities := targetedPendingResponsibilities(root, current.State, p, owners, conclusions, false)
	otherBlocked := false
	for id, result := range conclusions {
		if id == performing || result != "blocked" {
			continue
		}
		if active, known := targetedResponsibilityActive(root, current.State, stringField(p["session_id"]), id); known && active {
			otherBlocked = true
			break
		}
	}
	phase, status, action := "targeted_reverification", "targeted_reverification", "finish the missing actual independent observations: "+strings.Join(missing, ", ")
	transitionID, event := "PTR-BUG-06", "targeted_reverification_recorded"
	route := ""
	if value.Result != "pass" {
		route = value.FailureClass
		if value.Result == "blocked" {
			route = "blocked"
		} else if value.Result == "scope_changed" {
			route = "scope_changed"
		}
		phase, status = "investigation", "blocked"
		if route == "blocked" {
			phase = "targeted_reverification"
		}
		action = targetedFailureNextAction(stringField(p["case_id"]), route, req.Reverification)
		transitionID, event = "S9-TARGETED-FAILURE", "targeted_reverification_failed"
	} else if otherBlocked {
		status, route = "blocked", "blocked"
		action = "This original Targeted result is received; another active independent responsibility remains blocked. Main continues the actual unfinished work without resubmission."
	} else if len(missing) == 0 {
		phase, status, action = "ready_for_full_review", "ready_for_full_review", "Continue the original automatic Handoff; accepted Targeted observations need no resubmission."
		if unknownResponsibilities {
			action += " Some earlier accepted observation or responsibility applicability is unknown; Main should assess the preserved original facts without inventing a PASS."
		}
	}
	var failureRoute any
	if route != "" {
		failureRoute = route
	}
	at := occurred(req.OccurredAt)
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	return updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-targeted-r%d", commitRevision+1), TransitionID: whitelistChecked(transitionID), Event: event, Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:targeted:%s:%d", req.Reverification.Path, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.Reverification.Path}, From: cursor(current.State), To: map[string]any{"state": "bug_resolution", "phase": phase}, RequestID: "s9-targeted-reverification", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-TARGETED-REVERIFICATION", ProducerResponsibility: "S9 QA", OccurredAt: at, Apply: func(state map[string]any) error {
		updateRepairPointer(state, map[string]any{"targeted_reverification_refs": refs, "targeted_reverification_artifacts": targetedArtifacts, "status": status, "failure_route": failureRoute, "updated_at": at.Format(time.RFC3339Nano), "next_action": action})
		setLifecycle(state, "bug_resolution", phase)
		return nil
	}})
}

func targetedPendingResponsibilities(root string, state, pointer map[string]any, owners, conclusions map[string]string, unknownReceipt bool) ([]string, bool) {
	var missing []string
	unknown := false
	for id, owner := range owners {
		if owner == "" || !strings.HasPrefix(id, verificationAssignmentPrefix) || conclusions[id] == "pass" {
			continue
		}
		active, known := targetedResponsibilityActive(root, state, stringField(pointer["session_id"]), id)
		if !known || (active && unknownReceipt) {
			// An unidentified old receipt or absent lifecycle projection is
			// unknown, not proof of PASS or a demand to repeat work.
			unknown = true
			continue
		}
		if active {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return missing, unknown
}

// targetedResponsibilityActive consults the existing dispatch's TASK activity;
// absent or unreadable lifecycle facts remain unknown rather than adding a
// new evidence/qualification requirement to the author's answer.
func targetedResponsibilityActive(root string, state map[string]any, sessionID, performing string) (bool, bool) {
	if sessionID == "" || performing == "" {
		return false, false
	}
	owners := stringMapField(repairPointer(state)["assignment_owners"])
	owner := owners[canonicalTargetedAssignment(performing)]
	if owner == "" {
		return false, false
	}
	taskID, err := targetedauthoring.RegisteredTaskID(root, state, sessionID, canonicalTargetedAssignment(performing), owner)

	if err != nil || taskID == "" {
		return false, false
	}
	for _, raw := range anySlice(mapField(state, "entities")["tasks"]) {
		task, _ := raw.(map[string]any)
		if stringField(task["id"]) != taskID {
			continue
		}
		projection, err := workstate.TaskProjection(task, stringField(state["schema_version"]))
		if err != nil {
			return false, false
		}
		return projection.Status != workstate.Cancelled, true
	}

	return false, false
}

func copyRepairReceiptRow(row map[string]any) map[string]any {
	copy := make(map[string]any, len(row)+2)
	for key, value := range row {
		copy[key] = value
	}
	return copy
}

func targetedFailureNextAction(caseID, route string, reverification ArtifactRef) string {
	if route == "blocked" {
		return "Main resolves the actual environment blocker; the original independent verifier continues and saves a new actual Targeted result at its original answer path. Accepted blocked history remains recorded; no separate resume or submit is required."
	}
	if route == "" {
		return "The original Targeted result failed; its cause is not classified. Main should assess the recorded observation and arrange the original S8 investigation as needed; do not invent a cause or request a new submission."
	}
	if strings.TrimSpace(caseID) == "" {
		return "route the targeted failure to S8 root-cause investigation; do not patch the symptom locally"
	}
	return fmt.Sprintf("re-open Case %s in S8 with `runtime investigation route --case-id %s --route investigate_more --reason \"targeted reverification %s requires causal reassessment\" --reassessment-evidence %s`; then re-read the Case and investigate before approving a new RepairContract", caseID, caseID, route, reverification.Path)
}

func CommitRepairHandoff(root, statePath, journalPath string, req CommitHandoffRequest) (runtimepkg.Snapshot, error) {
	for {
		snapshot, err := commitRepairHandoff(root, statePath, journalPath, req)
		if req.ExpectedRevision >= 0 || !errors.Is(err, runtimepkg.ErrStaleRevision) {
			return snapshot, err
		}
		if req.Context != nil && req.Context.Err() != nil {
			return runtimepkg.Snapshot{}, req.Context.Err()
		}
	}
}

func commitRepairHandoff(root, statePath, journalPath string, req CommitHandoffRequest) (runtimepkg.Snapshot, error) {
	req.Context = buildsurface.WithReadCache(req.Context)
	current, writer, err := readRepairRuntime(root, statePath, journalPath, req.Context)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	p := repairPointer(current.State)
	if p != nil && req.Handoff.Path != "" && stringField(p["handoff_ref"]) == req.Handoff.Path {
		return current, nil
	}
	if p == nil || stringField(p["status"]) != "ready_for_full_review" {
		return runtimepkg.Snapshot{}, errors.New("S9 handoff requires status=ready_for_full_review")
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(req.Handoff.Path)))
	if err != nil {
		return current, err
	}
	var handoff RepairHandoff
	if err := json.Unmarshal(data, &handoff); err != nil {
		return current, err
	}
	if handoff.SessionRef.Path != stringField(p["path"]) || handoff.ChangeImpactRef.Path != stringField(p["impact_ref"]) {
		return current, errors.New("Handoff does not identify the current accepted repair responsibility")
	}
	data, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(stringField(p["impact_ref"]))))
	if err != nil {
		return current, err
	}
	var impactDocument ChangeImpact
	if err := json.Unmarshal(data, &impactDocument); err != nil {
		return current, err
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
	eventID := fmt.Sprintf("evt-s9-handoff-r%d", current.Revision+1)
	planID := reviewmeta.NewPlanID(stringField(current.State["runtime_id"]), eventID)
	if planID == "" {
		return current, errors.New("S9 handoff cannot establish S7 identity without the original runtime identity")
	}
	if _, err := time.Parse(time.RFC3339Nano, handoff.HandedOffAt); err != nil {
		return current, fmt.Errorf("S9 handoff timestamp: %w", err)
	}
	seedRef, err := createS7ReviewPlanSeedForEntry(root, planID, baselineGeneration(current.State), impactDocument, ContractRef{Path: handoff.ContractRef.Path}, taskIDs, handoff.ChangeImpactRef.Path, handoff.HandedOffAt, req.Context)
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
	// first register its professional Plan before dispatch. Validating the seed
	// through ValidatePlanArtifactForRegistration would reject every handoff.
	// The original S7 registration gate, not the handoff installer, enforces refinement.
	at := occurred(req.OccurredAt)
	actor := req.Actor
	if actor == "" {
		actor = "orchestrator"
	}
	commitRevision := current.Revision
	return func() (runtimepkg.Snapshot, error) {
		snapshot, updateErr := writer.Update(current.Revision, runtimepkg.Mutation{EventID: eventID, TransitionID: whitelistChecked("TR-012"), Event: "repair_handoff_ready", Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:handoff:%s:%d", req.Handoff.Path, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.Handoff.Path, seedRef.Path}, From: cursor(current.State), To: map[string]any{"state": "verification", "phase": "running"}, RequestID: "s9-handoff", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-REPAIR-HANDOFF", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
			updates := map[string]any{"handoff_ref": req.Handoff.Path, "status": "closed", "updated_at": at.Format(time.RFC3339Nano), "next_action": "author the first professional S7 plan from the staged seed, preserving its original identity and generation; use change paths as review scope, without inventing fingerprints. Complete and save the original ReviewPlan questionnaire; its original consumer registers the plan. Decide actual E2E applicability and arrange independent verification.", "review_plan_seed_ref": seedRef.Path}
			if handoff.ChangesetRef.Path != "" {
				updates["changeset_ref"] = handoff.ChangesetRef.Path
			}
			updateRepairPointer(state, updates)
			review := ensureObject(state, "review")
			review["clean_round"] = nil
			review["plan"] = nil
			review["claims"] = map[string]any{}
			review["assignments"] = map[string]any{}
			review["investigation"] = nil
			review["round_entry"] = map[string]any{"transition_id": "TR-012", "plan_id": planID, "baseline_generation": baselineGeneration(state), "change_impact_ref": handoff.ChangeImpactRef.Path, "repair_handoff_ref": req.Handoff.Path, "review_plan_seed_ref": seedRef.Path}
			// Index the change_impact artifact as runtime evidence: the S7
			// repair-baseline gate resolves the seed's change_impact
			// source_ref against the evidence index (sandbox-verified: the
			// round could not otherwise re-open).
			indexHandoffEvidence(state, req.Handoff, handoff, baselineGeneration(state), at, planID)
			return reviewpkg.ApplyRegisteredPlanProjection(state, seedPlan, seedRef.Path, "", at)
		}})
		if updateErr != nil {
			return runtimepkg.Snapshot{}, updateErr
		}
		return snapshot, nil
	}()
}

func readRepairRuntime(root, statePath, journalPath string, contexts ...context.Context) (runtimepkg.Snapshot, *runtimepkg.Store, error) {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	store := runtimepkg.NewStore(statePath, journalPath).WithContext(ctx)
	snapshot, err := store.Snapshot()
	if err != nil {
		return runtimepkg.Snapshot{}, nil, err
	}
	return snapshot, runtimepkg.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(ctx), nil
}

// repairAllowedTransitions lists every Loop Definition transition the repair
// package is allowed to journal, mapped from the internal emission site to
// the declared ID.
var repairAllowedTransitions = []string{
	"PTR-BUG-05", "PTR-BUG-06", "PTR-BUG-09", "PTR-BUG-10", "PTR-BUG-11", "TR-012",
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
	return fmt.Errorf("TransitionID %q is not declared in docs/control/loop-definition.json; repair checkpoints may only journal whitelisted transitions (%s)",
		transitionID, strings.Join(append(append([]string{}, repairAllowedTransitions...), "S9-SESSION-OPEN", "S9-RESULT-SUBMIT", "S9-TARGETED-FAILURE"), ", "))
}

// whitelistChecked is the RC-09 (S9-8) emission-site guard for compile-time
// pinned TransitionID literals. Because the argument is a literal reviewed
// against docs/control/loop-definition.json, a whitelist violation is a programming
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
		refs = append(refs, ArtifactRef{Path: stringField(item["path"])})
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

func pointerArtifact(pointer map[string]any, pathKey, label string) (ArtifactRef, error) {
	ref := ArtifactRef{Path: stringField(pointer[pathKey])}
	if ref.Path == "" {
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
	contractRef, err := pointerArtifact(pointer, "contract_ref", "current RepairContract")
	if err != nil {
		return nil, err
	}
	sessionRef, err := pointerArtifact(pointer, "path", "current RepairSession")
	if err != nil {
		return nil, err
	}
	planRef, err := pointerArtifact(pointer, "plan_ref", "current RepairPlan")
	if err != nil {
		return nil, err
	}
	contract, err := ValidateApprovedContractRef(root, ContractRef{Path: contractRef.Path})
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
	ref, err := pointerArtifact(pointer, "result_ref", "current RepairResult")
	if err != nil {
		return nil, err
	}
	return []ArtifactRef{ref}, nil
}

func aggregateRepairResultArtifacts(results []RepairResult) ([]ChangedArtifact, error) {
	byPath := map[string]ChangedArtifact{}
	// G12: results arrive in submission order (result_refs is append-only and
	// dispatch serializes shared-authority lanes via dependencies + resource
	// locks). When one path repeats — a later lane edited a file an earlier
	// lane also changed, e.g. the shared field-registry authority — the later
	// result describes the current on-disk state, so it wins; the earlier
	// record remains in result_refs for audit. Rejecting the batch instead
	// would make any sequentially-edited shared file uncommittable: results
	// are immutable and a second result per Assignment is refused.
	for _, result := range results {
		for _, artifact := range result.ChangedArtifacts {
			path := normalizePath(artifact.Path)
			artifact.Path = path
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
	missing := []string{}
	extra := []string{}
	for path, hash := range leftSet {
		if !artifactSetEntryMatches(hash, rightSet[path]) {
			missing = append(missing, path)
		}
	}
	for path, hash := range rightSet {
		if !artifactSetEntryMatches(hash, leftSet[path]) {
			extra = append(extra, path)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		sort.Strings(missing)
		sort.Strings(extra)
		return fmt.Errorf("exact artifact set failed between %s and %s: missing_or_changed=%v extra_or_changed=%v", leftName, rightName, missing, extra)
	}
	return nil
}

func changedArtifactSet(values []ChangedArtifact, label string) (map[string]string, error) {
	result := map[string]string{}
	for _, value := range values {
		path := normalizePath(value.Path)
		if path == "." {
			return nil, fmt.Errorf("%s contains an empty artifact path", label)
		}
		if _, exists := result[path]; exists {
			return nil, fmt.Errorf("%s contains duplicate artifact path %q", label, path)
		}
		status := value.Status
		if status == "" {
			status = "modified"
		}
		result[path] = ":" + status
	}
	return result, nil
}

func artifactRefSet(values []ArtifactRef, label string) (map[string]string, error) {
	result := map[string]string{}
	for _, value := range values {
		path := normalizePath(value.Path)
		if path == "." {
			return nil, fmt.Errorf("%s contains an empty artifact path", label)
		}
		if _, exists := result[path]; exists {
			return nil, fmt.Errorf("%s contains duplicate artifact path %q", label, path)
		}
		// ArtifactRef.status is optional in ChangeImpact. An omitted status is
		// intentionally a wildcard; the authoritative status comes from the
		// Session diff or RepairResult when those are reconciled.
		result[path] = ":" + value.Status
	}
	return result, nil
}

// isNonProductSurface reports whether a path is bookkeeping rather than the
// repaired product: control plane, round documentation, the cold-start
// verification workspace, generated docs, or build caches. Such files drift
// during the round by design and are never claimable through a RepairResult
// (contract scope is product-only), so the exact-set reconciliation must not
// demand them.
func isNonProductSurface(path string) bool {
	if ignoreBaselinePath(path) {
		return true
	}
	for _, prefix := range []string{".claude/", "docs/", "e2e-workspace/", "blueprint/", "schema/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func compareArtifactSets(left, right map[string]string, leftName, rightName string) error {
	missing := []string{}
	extra := []string{}
	for path, hash := range left {
		if isNonProductSurface(path) {
			continue
		}
		if !artifactSetEntryMatches(hash, right[path]) {
			missing = append(missing, path)
		}
	}
	for path, hash := range right {
		if isNonProductSurface(path) {
			continue
		}
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
	out := make([]any, 0, len(values)+1)
	seen := false
	for _, value := range values {
		item, _ := value.(map[string]any)
		if item == nil {
			continue
		}
		path := stringField(item["path"])
		if path == "" {
			continue
		}
		// This is a new current reference, not a rewrite of its original
		// artifact. Keep professional extensions and omit only the retired
		// machine member at this output boundary.
		row := map[string]any{}
		for key, value := range item {
			if key != "sha256" && key != "path" && key != "performing_assignment_id" && key != "result" {
				row[key] = value
			}
		}
		row["path"] = path
		if owner := stringField(item["performing_assignment_id"]); owner != "" {
			row["performing_assignment_id"] = owner
		}
		if result := stringField(item["result"]); result != "" {
			row["result"] = result
		}
		out = append(out, row)
		if path == ref.Path {
			seen = true
		}
	}
	if !seen {
		out = append(out, map[string]any{"path": ref.Path})
	}
	return out
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
		refs = append(refs, ArtifactRef{Path: stringField(item["path"])})
	}
	return refs, nil
}

// Read completed work from the current recorded batch, not reference proofs.
func validateHandoffAgainstCurrentRepair(root string, state, pointer map[string]any, handoff RepairHandoff) error {
	for _, source := range []struct {
		name, path string
		actual     ArtifactRef
	}{
		{"session", "path", handoff.SessionRef},
		{"plan", "plan_ref", handoff.PlanRef},
		{"contract", "contract_ref", handoff.ContractRef},
		{"change_impact", "impact_ref", handoff.ChangeImpactRef},
	} {
		if source.actual.Path != stringField(pointer[source.path]) {
			return fmt.Errorf("Handoff %s does not reference the current accepted source; Main/system must rebuild its automatic summary from the original receipts", source.name)
		}
	}
	refs, err := currentRepairResultRefs(pointer)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return errors.New("repair work and Main checks remain unfinished")
	}
	resultFound := false
	for _, ref := range refs {
		resultFound = resultFound || ref.Path == handoff.ResultRef.Path
	}
	if !resultFound {
		return errors.New("Handoff result does not reference the current accepted Final batch; Main/system must rebuild its automatic summary from the original receipts")
	}
	acceptedTargeted := map[string]bool{}
	for _, ref := range existingArtifactRefs(pointer["targeted_reverification_artifacts"]) {
		acceptedTargeted[ref.Path] = true
	}
	for _, ref := range handoff.TargetedReverificationRefs {
		if !acceptedTargeted[ref.Path] {
			return fmt.Errorf("Handoff targeted_reverification %q is not an accepted current observation; Main/system must rebuild its automatic summary from the original receipts", ref.Path)
		}
	}
	// Targeted reception already recorded the current completion fact. Do not
	// replay Contract slots or reject a recovered pass because of old reports.
	if stringField(pointer["status"]) != "ready_for_full_review" {
		return errors.New("necessary independent verification remains unfinished")
	}
	return nil
}

func cleanupStagedArtifact(writer *runtimepkg.Store, expectedRevision int, ref ArtifactRef, state map[string]any, operationErr error) error {
	// No saved original bytes means the failed writer cannot prove a file is
	// safe to remove. Keep it for ordinary cleanup, independent of acceptance.
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

// indexHandoffEvidence carries existing S9 references into the S7 handoff.
func indexHandoffEvidence(state map[string]any, handoffRef ArtifactRef, handoff RepairHandoff, generation int, at time.Time, planIDs ...string) {
	existing, _ := state["evidence"].([]any)
	known := map[string]bool{}
	for _, raw := range existing {
		if entry, _ := raw.(map[string]any); entry != nil {
			known[stringField(entry["id"])] = true
		}
	}
	add := func(id, kind, path, responsibility string) {
		if id == "" || path == "" || known[id] {
			return
		}
		existing = append(existing, map[string]any{
			"id": id, "kind": kind, "path": path,
			"status": "valid", "baseline_generation": generation,
			"responsibility_id": responsibility, "produced_by": []any{"s9-handoff"},
			"scope_refs": []any{}, "invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil,
		})
		if len(planIDs) > 0 {
			row := existing[len(existing)-1].(map[string]any)
			row["review_plan_id"] = planIDs[0]
		}
	}
	add(filepath.Base(handoffRef.Path), "repair_handoff", handoffRef.Path, "S9 Repair")
	add(filepath.Base(handoff.ChangeImpactRef.Path), "change_impact", handoff.ChangeImpactRef.Path, "S9 Repair")
	for _, ref := range handoff.TargetedReverificationRefs {
		add(filepath.Base(ref.Path), "targeted_reverification", ref.Path, "Original Finder")
	}
	state["evidence"] = existing
}
