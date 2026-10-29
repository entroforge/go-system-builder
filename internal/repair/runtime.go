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
	impactanalysis "github.com/entroforge/go-system-builder/internal/impact"
	reviewpkg "github.com/entroforge/go-system-builder/internal/review"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
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
type SubmitPlanReportRequest struct {
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
type SubmitResultRuntimeRequest struct {
	RuntimeRequest
	Result RepairResultRequest
	issued *issuedRepairResultFinalCapability
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
			// Defect #27 recovery: a session that captured its baseline but never
			// recorded any result/changeset/handoff made zero progress and is
			// voidable — a fresh open replaces the pointer and restarts S9.
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
			ref := ArtifactRef{ID: req.SessionID, Path: stringField(existing["path"]), SHA256: stringField(existing["sha256"])}
			var session RepairSession
			if err := decodeArtifact(root, ref, "repair-session.schema.json", &session); err != nil {
				return runtimepkg.Snapshot{}, RepairSession{}, ArtifactRef{}, err
			}
			return current, session, ref, nil
		}
	}
	if lifecycleState(current.State) != "bug_resolution" || lifecyclePhase(current.State) != "repair_readback" {
		// Defect #27 recovery: a voided zero-progress session may be restarted
		// from the fixing phase itself (the replacement session starts S9 over).
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
	authorityDigest := session.BaselineDigest
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
		updateRepairPointer(state, map[string]any{"plan_ref": ref.Path, "plan_sha256": ref.SHA256, "status": "planning", "updated_at": at.Format(time.RFC3339Nano), "next_action": "record each Builder plan and actual precheck before that Builder implements"})
		setLifecycle(state, "bug_resolution", "planning")
		return nil
	}})
	if err != nil {
		return runtimepkg.Snapshot{}, RepairPlan{}, ArtifactRef{}, cleanupStagedArtifact(writer, req.ExpectedRevision, ref, current.State, err)
	}
	return snapshot, plan, ref, nil
}

// SubmitRepairPlanReportToRuntime records a Builder's implementation plan.
// Other Builders may already be implementing their recorded plans.
func SubmitRepairPlanReportToRuntime(root, statePath, journalPath string, req SubmitPlanReportRequest) (runtimepkg.Snapshot, PlanReport, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath, req.Context)
	if err != nil {
		return runtimepkg.Snapshot{}, PlanReport{}, err
	}
	p := repairPointer(current.State)
	if p == nil || lifecycleState(current.State) != "bug_resolution" ||
		(p["status"] != "planning" && p["status"] != "reproducing" && p["status"] != "repairing") {
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
	if req.issued != nil {
		// The professional answer was checked once by the issued receiver.
		// These are its known packet associations, not source proofs.
		binding := req.issued.packet.Binding
		if req.Report.Path != req.issued.receipt.ReportRef || report.ReportID != req.issued.packet.ReportID ||
			report.PlanID != binding.PlanID || report.SessionID != binding.SessionID ||
			report.AssignmentID != binding.RepairAssignmentID || report.AgentID != binding.OwnerAgentID ||
			stringField(p["plan_ref"]) != binding.PlanRef {
			return current, report, errors.New("PlanReport path or responsibility differs from the current packet")
		}
	} else {
		// The direct legacy entry still reads its own professional report;
		// it cannot submit a packet-backed answer through a second envelope.
		if report.Receipt != nil {
			return current, report, errors.New("issued PlanReport uses its original answer receiver")
		}
	}
	for _, ref := range existingArtifactRefs(p["plan_report_refs"]) {
		if ref.Path == req.Report.Path {
			if ref.SHA256 != req.Report.SHA256 {
				return current, report, runtimepkg.ErrSubmissionConflict
			}
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
		From: cursor(current.State), To: map[string]any{"state": "bug_resolution", "phase": "fixing"},
		RequestID: "s9-plan-report", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-PLAN-REPORT",
		GateFingerprint: "sha256:s9-plan-report-v1", ProducerResponsibility: "S9 Repair", OccurredAt: at,
		Apply: func(state map[string]any) error {
			latest := stateRepairPointer(state)
			if latest == nil || state["runtime_id"] != current.State["runtime_id"] || baselineGeneration(state) != baselineGeneration(current.State) ||
				latest["session_id"] != p["session_id"] || latest["plan_ref"] != p["plan_ref"] ||
				(stringMapField(latest["assignment_owners"])[report.AssignmentID] != "" && stringMapField(latest["assignment_owners"])[report.AssignmentID] != report.AgentID) ||
				lifecycleState(state) != "bug_resolution" ||
				(latest["status"] != "planning" && latest["status"] != "reproducing" && latest["status"] != "repairing") {
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
			updateRepairPointer(state, map[string]any{"plan_report_ref": req.Report.Path, "plan_report_sha256": req.Report.SHA256,
				"plan_report_refs": merged, "assignment_owners": owners, "assignment_id": report.AssignmentID, "status": "repairing",
				"updated_at": at.Format(time.RFC3339Nano), "next_action": "continue this Builder's original implementation responsibility at its recorded work location; preserve its existing dispatch and return its actual RepairResult through the original receiver; Main retains integration and necessary checks"})
			setLifecycle(state, "bug_resolution", "fixing")
			return nil
		},
	})
	if err != nil {
		return current, report, cleanupStagedArtifact(writer, req.ExpectedRevision, req.Report, current.State, err)
	}
	return snapshot, report, nil
}

// appendablePlanReportRefs accepts the one valid initial state in which no
// PlanReport has been submitted yet. Once either singular current pointer or
// the accumulated reference list is present, the complete strict invariant
// applies to the stored report list.
func appendablePlanReportRefs(pointer map[string]any) ([]ArtifactRef, error) {
	if pointer == nil {
		return nil, errors.New("S9 repair pointer is missing")
	}
	path := stringField(pointer["plan_report_ref"])
	sha := stringField(pointer["plan_report_sha256"])
	raw := pointer["plan_report_refs"]
	if path == "" && sha == "" {
		if raw == nil {
			return []ArtifactRef{}, nil
		}
		rows, ok := raw.([]any)
		if ok && len(rows) == 0 {
			return []ArtifactRef{}, nil
		}
		return nil, errors.New("S9 PlanReport refs exist without a singular current report")
	}
	if path == "" || sha == "" {
		return nil, errors.New("S9 singular current PlanReport reference is incomplete")
	}
	return strictPlanReportRefs(raw, ArtifactRef{Path: path, SHA256: sha})
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
		GateFingerprint: "sha256:s9-execution-begin-v1", ProducerResponsibility: "S9 Repair", OccurredAt: at,
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

func SubmitRepairResultToRuntime(root, statePath, journalPath string, req SubmitResultRuntimeRequest) (runtimepkg.Snapshot, RepairResult, ArtifactRef, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath, req.Context)
	if err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
	}
	if req.issued != nil {
		if req.ExpectedRevision < 0 || req.Result.issuedFinal != req.issued {
			return current, RepairResult{}, ArtifactRef{}, errors.New("typed S9 Result requires its exact private capability and explicit Runtime CAS revision")
		}
	} else if req.Result.Verdict != "" || req.Result.Receipt != nil {
		return current, RepairResult{}, ArtifactRef{}, errors.New("issued S9 typed RepairResult cannot use the legacy Runtime submit route")
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
	var plan RepairPlan
	var planRef ArtifactRef
	if req.issued != nil {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(stringField(p["plan_ref"]))))
		if err != nil {
			return current, RepairResult{}, ArtifactRef{}, err
		}
		if err := json.Unmarshal(data, &plan); err != nil {
			return current, RepairResult{}, ArtifactRef{}, err
		}
	} else {
		var planErr error
		planRef, planErr = pointerArtifact(p, "plan_ref", "plan_sha256", "current RepairPlan")
		if planErr != nil {
			return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, planErr
		}
		plan, planErr = ValidateRepairPlan(root, planRef)
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
		if req.issued == nil {
			if err := rejectLegacyRepairResultForIssuedTarget(root, current.State, p, plan, planRef, req.Result.AssignmentID); err != nil {
				return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
			}
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
	}
	result, ref, err := SubmitRepairResult(root, req.Result)
	if err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
	}
	cleanupTypedStage := func(operationErr error) error {
		if req.issued != nil && req.issued.cleanup != nil {
			if cleanupErr := req.issued.cleanup(result); cleanupErr != nil {
				return fmt.Errorf("%w; exact unaccepted S9 Result staging cleanup: %v", operationErr, cleanupErr)
			}
			return operationErr
		}
		return cleanupStagedArtifact(writer, current.Revision, ref, current.State, operationErr)
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
	if req.issued == nil {
		if err := checkS9AuthorityFreshness(root, p, result.ChangedArtifacts); err != nil {
			return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, cleanupTypedStage(err)
		}
	}
	resultRefs := appendArtifactRefFromPointer(p["result_refs"], ref)
	if req.issued != nil {
		resultRefs, err = repairResultPointerRows(p, ref, root)
		if err != nil {
			return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, cleanupTypedStage(err)
		}
	}
	if req.issued == nil {
		var planErr error
		planRef, planErr = pointerArtifact(p, "plan_ref", "plan_sha256", "current RepairPlan")
		if planErr != nil {
			return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, cleanupTypedStage(planErr)
		}
		plan, planErr = ValidateRepairPlan(root, planRef)
		if planErr != nil {
			return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, cleanupTypedStage(planErr)
		}
	}
	nextStatus := "repairing"
	nextAction := "submit one exact-unit RepairResult for every RepairAssignment before committing ChangeImpact"
	batchState := func(refs []any) (bool, bool, []string, error) {
		if req.issued == nil {
			return repairResultBatchState(root, plan, refs)
		}
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
		return current, RepairResult{}, ArtifactRef{}, cleanupTypedStage(batchErr)
	} else {
		nextStatus, nextAction = repairResultNextState(complete, allPass, missing)
	}
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	eventID := fmt.Sprintf("evt-s9-result-%s-r%d", result.ResultID, commitRevision+1)
	idempotencyKey := fmt.Sprintf("runtime:s9:result:%s:%d", result.ResultID, commitRevision)
	evidenceIDs := []string{result.ResultID}
	if req.issued != nil {
		eventID = req.issued.receipt.AcceptedEventID
		idempotencyKey = req.issued.receipt.AcceptedIdempotencyKey
		evidenceIDs = []string{ref.Path, req.issued.receipt.CandidateRef, req.issued.packet.ID, req.issued.packet.Binding.Sources.RepairAssignmentID}
	}
	snapshot, err := updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: eventID, TransitionID: whitelistChecked("S9-RESULT-SUBMIT"), Event: "repair_result_submitted", Actor: actor, IdempotencyKey: idempotencyKey, RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: evidenceIDs, From: cursor(current.State), To: cursor(current.State), RequestID: "s9-result-submit", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-RESULT-SUBMIT", GateFingerprint: "sha256:s9-result-submit-v1", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
		applyRefs, applyStatus, applyAction := resultRefs, nextStatus, nextAction
		if req.issued != nil {
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
		updateRepairPointer(state, map[string]any{"result_ref": ref.Path, "result_sha256": ref.SHA256, "result_refs": applyRefs, "status": applyStatus, "updated_at": at.Format(time.RFC3339Nano), "next_action": applyAction})
		return nil
	}})
	if err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, cleanupTypedStage(err)
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
	live, _, err := captureSessionBaseline(root, session)
	if err != nil {
		return err
	}
	liveByPath := make(map[string]string, len(live))
	for _, artifact := range live {
		liveByPath[artifact.Path] = artifact.SHA256
	}
	// Contract scope is the authorized mutation surface for this session: a
	// baseline artifact inside it that drifted is an in-flight unit repair
	// whose result will claim it; outside it (or forbidden) a drift is
	// unauthorized and blocks the commit.
	var contractScope *ApprovedContract
	if ref := stringField(pointer["contract_ref"]); ref != "" {
		if approved, err := ValidateApprovedContractRef(root, ContractRef{Path: ref, SHA256: stringField(pointer["contract_sha256"])}); err == nil {
			contractScope = &approved
		}
	}
	// Stored membership survives changes to current Git ownership/ignore rules.
	// Legacy logs without historical provenance remain protected.
	runtimeLogs := excludedBaselinePaths(root, artifactPaths(session.BaselineArtifacts))
	var drifted []string

	for _, artifact := range session.BaselineArtifacts {
		if claimed[artifact.Path] {
			continue
		}
		if runtimeLogs[normalizePath(artifact.Path)] {
			continue
		}
		// .claude/tmp is transient controller scratch (staging copies, probe
		// files); it is not an implementation surface and must never stale the
		// authority fingerprint — same class as the control-plane drift paths.
		if strings.HasPrefix(artifact.Path, ".claude/tmp/") {
			continue
		}
		if liveByPath[artifact.Path] != artifact.SHA256 {
			// Sessions opened before the capture-side filter excluded
			// control-plane journals and dependency/build caches may carry
			// captured copies; they drift during ordinary verification runs
			// and are never authority-relevant, so forgive them here as well
			// as at capture.
			if ignoreBaselinePath(artifact.Path) {
				continue
			}
			if contractScope != nil && scopeAllows(artifact.Path, contractScope.ProspectiveScope, contractScope.ForbiddenScope) == nil {
				continue
			}
			drifted = append(drifted, fmt.Sprintf("%q (session=%s live=%s)", artifact.Path, shortDigest(artifact.SHA256), shortDigest(liveByPath[artifact.Path])))
		}
	}
	if len(drifted) > 0 {
		total := len(drifted)
		if len(drifted) > 50 {
			drifted = drifted[:50]
		}
		return fmt.Errorf("S9 authority fingerprint is stale: %d baseline artifact(s) drifted after RepairSession %s opened: %s; preserve the original baseline, inspect all drift, and resume the authorized repair", total, session.SessionID, strings.Join(drifted, "; "))
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
	if p == nil || stringField(p["status"]) != "impact_reconciliation" {
		return runtimepkg.Snapshot{}, errors.New("S9 impact commit requires status=impact_reconciliation")
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(req.Impact.Path)))
	if err != nil {
		return current, err
	}
	var impactDocument ChangeImpact
	if err := json.Unmarshal(data, &impactDocument); err != nil {
		return current, err
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
	// Read already accepted Final facts; no recursive receipt/source proof.
	complete := (len(impactDocument.ChangedArtifacts) == 0 || len(impactDocument.ChangeTypes) > 0) && len(impactDocument.Decisions) > 0
	results, err := currentRepairResults(root, p)
	if err != nil {
		return current, err
	}
	accepted, err := repairResultChangeset(stringField(p["session_id"]), results)
	if err != nil {
		return current, err
	}
	reported, err := repairChangesetRecord(stringField(p["session_id"]), "explicit", impactDocument.ChangedArtifacts)
	if err != nil {
		return current, err
	}
	if accepted.Digest != reported.Digest {
		return current, errors.New("ChangeImpact changed_artifacts do not describe the current accepted Final contribution; Main/system must prepare its original request from those Final facts")
	}
	if len(results) == 0 {
		complete = false
	}
	for _, result := range results {
		if result.Result != "pass" {
			complete = false
		}
		for _, check := range result.Checks {
			if check.Result != "pass" {
				complete = false
			}
		}
	}
	nextPhase, nextAction := "targeted_reverification", "submit independent targeted reverification"
	if !complete {
		nextPhase, nextAction = "impact_reconciliation", "Impact recorded; finish actual repair/Main checks and professional impact judgments before independent verification"
	}
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
	return updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-impact-r%d", commitRevision+1), TransitionID: whitelistChecked("PTR-BUG-05"), Event: "change_impact_reconciled", Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:impact:%s:%d", req.Impact.Path, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.Impact.Path}, From: cursor(current.State), To: map[string]any{"state": "bug_resolution", "phase": nextPhase}, RequestID: "s9-impact-reconcile", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-IMPACT-RECONCILE", GateFingerprint: "sha256:s9-impact-reconcile-v1", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
		updateRepairPointer(state, map[string]any{"impact_ref": req.Impact.Path, "impact_sha256": req.Impact.SHA256, "status": nextPhase, "required_reverification_ids": requiredReverificationIDs(impactDocument), "updated_at": at.Format(time.RFC3339Nano), "next_action": nextAction})
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
		setLifecycle(state, "bug_resolution", nextPhase)
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
	current, writer, err := readRepairRuntime(root, statePath, journalPath, req.Context)
	if err != nil {
		return current, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return current, err
	}
	p := repairPointer(current.State)
	if p == nil || (stringField(p["status"]) != "targeted_reverification" && stringField(p["status"]) != "ready_for_full_review") {
		return current, errors.New("S9 targeted reverification requires the current verification responsibility")
	}
	value, err := ValidateTargetedReverification(root, req.Reverification)
	if err != nil {
		return current, err
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(stringField(p["impact_ref"]))))
	if err != nil {
		return current, err
	}
	var impact ChangeImpact
	if err := json.Unmarshal(data, &impact); err != nil {
		return current, err
	}
	if value.ImpactID != impact.ImpactID || value.RuntimeID != stringField(current.State["runtime_id"]) || value.BaselineGeneration != baselineGeneration(current.State) {
		return current, errors.New("Targeted report is not associated with the current impact responsibility")
	}
	owners := stringMapField(p["assignment_owners"])
	actor := dispatchedVerifierAgentID(value.PerformingAssignmentID, owners)
	if actor == "" {
		return current, errors.New("Targeted performing_assignment_id is not a dispatched verifier identity in the current responsibility; Main must resolve its original assignment owner")
	}
	if requested := strings.TrimSpace(req.Actor); requested != "" && requested != actor {
		return current, errors.New("Targeted actor does not match its recorded responsibility owner; Main must resolve the original handoff")
	}
	// Main arranges a person who did not implement or merge this repair.
	// Existing implementation facts can expose a conflict; labels grant nothing.
	independent := true
	implementers := map[string]bool{}
	resultRefs, err := currentRepairResultRefs(p)
	if err != nil {
		return current, err
	}
	for _, ref := range resultRefs {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
		if err != nil {
			return current, err
		}
		var result RepairResult
		if err := json.Unmarshal(data, &result); err != nil {
			return current, err
		}
		if result.ProducerAgentID != "" {
			implementers[result.ProducerAgentID] = true
			if result.ProducerAgentID == actor {
				independent = false
			}
		}
	}
	refs := appendUnique(stringSliceFromAny(p["targeted_reverification_refs"]), req.Reverification.Path)
	targetedArtifacts := appendArtifactRefFromPointer(p["targeted_reverification_artifacts"], req.Reverification)
	// Latest observations of each existing verification responsibility replace
	// its old failed/blocked attempt; historical reports remain recorded.
	latest := map[string]TargetedReverification{}
	for _, ref := range existingArtifactRefs(targetedArtifacts) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
		if err != nil {
			return current, err
		}
		var report TargetedReverification
		if err := json.Unmarshal(data, &report); err != nil {
			return current, err
		}
		latest[report.PerformingAssignmentID] = report
	}
	observed := map[string]string{}
	for _, report := range latest {
		performer := dispatchedVerifierAgentID(report.PerformingAssignmentID, owners)
		if performer == "" || report.Result != "pass" || implementers[performer] {
			continue
		}
		for _, result := range report.AssertionResults {
			// A current non-pass observation cannot be hidden by another pass.
			if previous, exists := observed[result.AssertionID]; !exists || previous == "pass" {
				observed[result.AssertionID] = result.Result
			}
		}
	}
	data, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(stringField(p["contract_ref"]))))
	if err != nil {
		return current, err
	}
	var contract map[string]any
	if err := json.Unmarshal(data, &contract); err != nil {
		return current, err
	}
	missing := []string{}
	for _, field := range []struct{ name, prefix string }{{"symptom_assertions", "symptom"}, {"root_invariant_assertions", "root"}, {"detection_gap_assertions", "gap"}} {
		for i := range anySlice(contract[field.name]) {
			if observed[fmt.Sprintf("%s-%d", field.prefix, i+1)] != "pass" {
				missing = append(missing, fmt.Sprintf("%s %d", field.name, i+1))
			}
		}
	}
	phase, status, action := "targeted_reverification", "targeted_reverification", "finish the missing actual independent observations: "+strings.Join(missing, ", ")
	transitionID, event := "PTR-BUG-06", "targeted_reverification_recorded"
	route := ""
	if value.Result != "pass" {
		route = value.FailureClass
		if route == "" {
			route = "fail_same_cause"
		}
		if value.Result == "blocked" {
			route = "blocked"
		}
		phase, status = "investigation", "blocked"
		if route == "blocked" {
			phase = "targeted_reverification"
		}
		action = targetedFailureNextAction(stringField(p["case_id"]), route, req.Reverification)
		transitionID, event = "S9-TARGETED-FAILURE", "targeted_reverification_failed"
	} else if !independent {
		action = "report recorded; Main must arrange an independent verifier who did not implement or merge this repair"
	} else if len(missing) == 0 {
		phase, status, action = "ready_for_full_review", "ready_for_full_review", "complete the original Handoff with runtime repair handoff complete --root <root>; do not resubmit accepted Targeted observations"
	}
	var failureRoute any
	if route != "" {
		failureRoute = route
	}
	at := occurred(req.OccurredAt)
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	return updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-targeted-r%d", commitRevision+1), TransitionID: whitelistChecked(transitionID), Event: event, Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:targeted:%s:%d", req.Reverification.Path, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.Reverification.Path}, From: cursor(current.State), To: map[string]any{"state": "bug_resolution", "phase": phase}, RequestID: "s9-targeted-reverification", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-TARGETED-REVERIFICATION", GateFingerprint: "sha256:s9-targeted-reverification-v1", ProducerResponsibility: "S9 QA", OccurredAt: at, Apply: func(state map[string]any) error {
		updateRepairPointer(state, map[string]any{"targeted_reverification_refs": refs, "targeted_reverification_artifacts": targetedArtifacts, "status": status, "failure_route": failureRoute, "updated_at": at.Format(time.RFC3339Nano), "next_action": action})
		setLifecycle(state, "bug_resolution", phase)
		return nil
	}})
}

// ResumeTargetedReverification reopens the existing targeted verification
// checkpoint after an explicitly recorded environmental/authority blocker
// has been resolved. It is deliberately narrower than a generic S9 resume:
// only a blocked targeted result may use it, and the next step remains an
// independent TargetedReverification create/commit.
func ResumeTargetedReverification(root, statePath, journalPath string, req ResumeTargetedRequest) (runtimepkg.Snapshot, error) {
	current, writer, err := readRepairRuntime(root, statePath, journalPath, req.Context)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime repair status"); err != nil {
		return runtimepkg.Snapshot{}, err
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
			if reason := strings.TrimSpace(req.Reason); reason != "" {
				pointer["blocker_resolution"] = reason
			} else {
				delete(pointer, "blocker_resolution")
			}
			updateRepairPointer(state, map[string]any{
				"status":              "targeted_reverification",
				"blocker_resolved_by": actor,
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
	req.Context = buildsurface.WithReadCache(req.Context)
	current, writer, err := readRepairRuntime(root, statePath, journalPath, req.Context)
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
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(req.Handoff.Path)))
	if err != nil {
		return current, err
	}
	var handoff RepairHandoff
	if err := json.Unmarshal(data, &handoff); err != nil {
		return current, err
	}
	if err := validateHandoffAgainstCurrentRepair(root, current.State, p, handoff); err != nil {
		return current, err
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
	var canonicalBaseline *buildsurface.Snapshot
	if buildsurface.HasCanonical(current.State) {
		baseline, readErr := buildsurface.ReadContext(req.Context, root, current.State)
		if readErr != nil {
			return runtimepkg.Snapshot{}, fmt.Errorf("read original canonical post-repair S7 baseline before TR-012: %w", readErr)
		}
		canonicalBaseline = &baseline
	}
	seedRef, err := createS7ReviewPlanSeed(root, integerValue(mapField(current.State, "review")["round"])+1, baselineGeneration(current.State), impactDocument, ContractRef{Path: handoff.ContractRef.Path, SHA256: handoff.ContractRef.SHA256}, taskIDs, handoff.ChangeImpactRef.Path, current.State, canonicalBaseline, req.Context)
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
	newBaselineDigest := seedBaselineDigest(impactDocument.ChangedArtifacts)
	at := occurred(req.OccurredAt)
	actor := req.Actor
	if actor == "" {
		actor = "orchestrator"
	}
	commitRevision := runtimeCommitRevision(req.ExpectedRevision, current)
	return func() (runtimepkg.Snapshot, error) {
		snapshot, updateErr := updateRuntime(writer, req.ExpectedRevision, runtimepkg.Mutation{EventID: fmt.Sprintf("evt-s9-handoff-r%d", commitRevision+1), TransitionID: whitelistChecked("TR-012"), Event: "repair_handoff_ready", Actor: actor, IdempotencyKey: fmt.Sprintf("runtime:s9:handoff:%s:%d", req.Handoff.Path, commitRevision), RuntimeID: stringField(current.State["runtime_id"]), EvidenceIDs: []string{req.Handoff.Path, seedRef.Path}, From: cursor(current.State), To: map[string]any{"state": "verification", "phase": "running"}, RequestID: "s9-handoff", BaselineGeneration: baselineGeneration(current.State), GateID: "S9-REPAIR-HANDOFF", GateFingerprint: "sha256:s9-repair-handoff-v3", ProducerResponsibility: "S9 Repair", OccurredAt: at, Apply: func(state map[string]any) error {
			updateRepairPointer(state, map[string]any{"changeset_ref": handoff.ChangesetRef.Path, "changeset_sha256": handoff.ChangesetRef.SHA256, "handoff_ref": req.Handoff.Path, "handoff_sha256": req.Handoff.SHA256, "status": "closed", "updated_at": at.Format(time.RFC3339Nano), "next_action": "author the first professional S7 plan from the exact staged seed, preserving its id, round, generation and pinned baseline; complete and save the original current S7 ReviewPlan questionnaire before any dispatch; its original save consumer registers the plan. Decide actual E2E applicability; an applicable browser surface gets its original cold-start/regression work, genuine N/A has no E2E Assignment and a null verification_artifact_workspace. Do not add Runtime-only digest fields. Later discovery changes retain the original one controlled revision through runtime review-plan revise", "review_plan_seed_ref": seedRef.Path, "review_plan_seed_sha256": seedRef.SHA256, "implementation_baseline_digest": newBaselineDigest})
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

// Read completed work from the current recorded batch, not reference proofs.
func validateHandoffAgainstCurrentRepair(root string, state, pointer map[string]any, handoff RepairHandoff) error {
	for _, source := range []struct {
		name, path, sha string
		actual          ArtifactRef
	}{
		{"session", "path", "sha256", handoff.SessionRef},
		{"plan", "plan_ref", "plan_sha256", handoff.PlanRef},
		{"contract", "contract_ref", "contract_sha256", handoff.ContractRef},
		{"change_impact", "impact_ref", "impact_sha256", handoff.ChangeImpactRef},
	} {
		if source.actual.Path != stringField(pointer[source.path]) || source.actual.SHA256 != stringField(pointer[source.sha]) {
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
		resultFound = resultFound || ref.Path == handoff.ResultRef.Path && ref.SHA256 == handoff.ResultRef.SHA256
	}
	if !resultFound {
		return errors.New("Handoff result does not reference the current accepted Final batch; Main/system must rebuild its automatic summary from the original receipts")
	}
	acceptedTargeted := map[string]string{}
	for _, ref := range existingArtifactRefs(pointer["targeted_reverification_artifacts"]) {
		acceptedTargeted[ref.Path] = ref.SHA256
	}
	for _, ref := range handoff.TargetedReverificationRefs {
		if sha, found := acceptedTargeted[ref.Path]; !found || sha != ref.SHA256 {
			return fmt.Errorf("Handoff targeted_reverification %q is not an accepted current observation; Main/system must rebuild its automatic summary from the original receipts", ref.Path)
		}
	}
	for _, ref := range refs {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
		if err != nil {
			return err
		}
		var result RepairResult
		if err := json.Unmarshal(data, &result); err != nil {
			return err
		}
		if result.Result != "pass" {
			return errors.New("repair result remains failed or blocked")
		}
		for _, check := range result.Checks {
			if check.Result != "pass" {
				return fmt.Errorf("Main check %s remains unfinished", check.Name)
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(stringField(pointer["impact_ref"]))))
	if err != nil {
		return err
	}
	var impact ChangeImpact
	if err := json.Unmarshal(data, &impact); err != nil {
		return err
	}
	if (len(impact.ChangedArtifacts) > 0 && len(impact.ChangeTypes) == 0) || len(impact.Decisions) == 0 {
		return errors.New("professional impact judgment remains unfinished")
	}
	// Targeted reception already recorded the current completion fact. Do not
	// replay Contract slots or reject a recovered pass because of old reports.
	if stringField(pointer["status"]) != "ready_for_full_review" {
		return errors.New("necessary independent verification remains unfinished")
	}
	return nil
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
