package repair

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/integration"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const repairResultAnswerLimit int64 = 20 << 20

const repairResultCandidateContract = "s9-repair-result-candidate-v1"
const repairResultFinalContract = "s9-repair-result-receipt-v1"

// RepairResultBeforeCandidateError reports that this particular consumer call
// rejected its input before it could create a Candidate. It does not claim that
// another attempt has no staged Candidate or that any Result was received.
type RepairResultBeforeCandidateError struct{ Cause error }

func (e *RepairResultBeforeCandidateError) Error() string { return e.Cause.Error() }
func (e *RepairResultBeforeCandidateError) Unwrap() error { return e.Cause }

var errRepairResultExecutionNotRegistered = errors.New("S9 RepairResult assignment has no registered Worker execution")

type AcceptIssuedRepairResultCandidateRequest struct {
	AnswerBytes      []byte `json:"-"`
	ExpectedRevision int
	PacketID         string
	AnswerPath       string
	OwnerAgentID     string
	SourceRoot       string
	// AuthorizeSource is retained for request compatibility. S9 candidate
	// reception does not execute caller identity or permission callbacks.
	AuthorizeSource func(root string, state map[string]any) error
	OccurredAt      time.Time
}

type SubmitIssuedRepairResultRequest struct {
	Checkpoint       *integration.Checkpoint `json:"-"`
	MainChecks       []RepairCheck           `json:"-"`
	ExpectedRevision int
	PacketID         string
	OwnerAgentID     string
	MainProof        runtimepkg.RepairResultMainProof
	OccurredAt       time.Time
}

type issuedRepairResultCapability struct {
	packet         workpacket.RepairResultPacket
	provenance     runtimepkg.RepairResultCandidateProvenance
	beforeRevision int
}

type issuedRepairResultFinalCapability struct {
	packet     workpacket.RepairResultPacket
	provenance runtimepkg.RepairResultCandidateProvenance
	receipt    runtimepkg.RepairResultReceipt
	ctx        context.Context
	mainRoot   string
}

// IssueRepairResultPackets freezes the current, registered S9 PlanReport and
// execution definition into the neutral packet registry. The packet is only
// an input contract: it does not accept a result, deliver a candidate, or
// complete the original Main integration checkpoint.
type RepairResultPreparation func(context.Context, string, *workpacket.RepairResultPacket)

func IssueRepairResultPackets(ctx context.Context, root, statePath, journalPath string, expectedRevision int, ownerFilter string) ([]workpacket.RepairResultPacket, error) {
	return IssueRepairResultPacketsPrepared(ctx, root, statePath, journalPath, expectedRevision, ownerFilter, nil)
}

func IssueRepairResultPacketsPrepared(ctx context.Context, root, statePath, journalPath string, expectedRevision int, ownerFilter string, prepare RepairResultPreparation) ([]workpacket.RepairResultPacket, error) {
	ctx, unlock, err := integration.LockWorkspace(ctx, root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, writer, err := readIssuedRepairResultRuntime(ctx, root, statePath, journalPath)
	if err != nil {
		return nil, err
	}
	if mainRoot := workspaceMainRoot(root, current.State); !sameAbsolutePath(mainRoot, root) {
		return nil, errors.New("S9 packet issuance root must be the original registered Main checkout")
	}
	candidates, err := buildRepairResultPackets(ctx, root, current.State, ownerFilter)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		if err := checkRevision(expectedRevision, current.Revision, "runtime S9 RepairResult packet issue"); err != nil {
			return nil, err
		}
		return []workpacket.RepairResultPacket{}, nil
	}
	issued := make([]workpacket.RepairResultPacket, 0, len(candidates))
	callerRevisionChecked := false
	for _, packet := range candidates {
		if prior, found, err := runtimepkg.FindRepairResultPacketContext(runtimepkg.WithPacketBodyRoot(ctx, root), current.State, packet.ID); err != nil {
			return nil, err
		} else if found {
			if !sameRepairResultPacket(prior, packet) {
				return nil, fmt.Errorf("S9 packet identity %s is already registered with different frozen sources", packet.ID)
			}
			issued = append(issued, prior)
			continue
		}
		// The caller's explicit revision pins the initial issuance transaction.
		// Publishing a sibling packet advances Runtime; subsequent packets in
		// this same serialized batch use the refreshed internal snapshot/CAS.
		// Exact prior issuances above remain replayable before this check.
		if !callerRevisionChecked {
			if err := checkRevision(expectedRevision, current.Revision, "runtime S9 RepairResult packet issue"); err != nil {
				return nil, err
			}
			callerRevisionChecked = true
		}
		if prepare != nil {
			_, execution, executionErr := currentRepairExecution(ctx, root, current.State, packet, true)
			if executionErr == nil {
				prepare(ctx, execution.Path, &packet)
			} else {
				packet.ViewPath = ""
			}
		} else {
			packet.ViewPath = ""
		}
		latest, err := issueOneRepairResultPacket(ctx, root, writer, current, packet)
		if err != nil {
			return nil, err
		}
		stored, found, err := runtimepkg.FindRepairResultPacketContext(runtimepkg.WithPacketBodyRoot(ctx, root), latest.State, packet.ID)
		if err != nil || !found || !sameRepairResultPacket(stored, packet) {
			return nil, fmt.Errorf("S9 packet %s was not retained exactly by the original Runtime writer: %v", packet.ID, err)
		}
		issued = append(issued, stored)
		current = latest
	}
	return issued, nil
}

func issueOneRepairResultPacket(ctx context.Context, root string, writer *runtimepkg.Store, current runtimepkg.Snapshot, packet workpacket.RepairResultPacket) (runtimepkg.Snapshot, error) {
	rowBytes, err := json.Marshal(packet)
	if err != nil {
		return runtimepkg.Snapshot{}, err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-result-work-packet.schema.json", rowBytes); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	var row map[string]any
	if err := json.Unmarshal(rowBytes, &row); err != nil {
		return runtimepkg.Snapshot{}, err
	}
	from := cursor(current.State)
	baseline, _ := current.State["baseline"].(map[string]any)
	mutation := runtimepkg.Mutation{
		EventID: packet.IssuanceEventID, TransitionID: "PACKET-ISSUE", Event: "work_packet_issued",
		Actor: "orchestrator", IdempotencyKey: "packet:" + packet.ID, RuntimeID: packet.Binding.Sources.RuntimeID,
		From: from, To: from, BaselineGeneration: integerRepairResult(baseline["generation"]),
		OccurredAt: time.Now().UTC(), RetainLastTransition: true,
		Apply: func(state map[string]any) error {
			if _, _, err := currentRepairExecution(ctx, root, state, packet, true); err != nil {
				return err
			}
			if prior, found, err := runtimepkg.FindRepairResultPacketContext(runtimepkg.WithPacketBodyRoot(ctx, root), state, packet.ID); err != nil {
				return err
			} else if found {
				if sameRepairResultPacket(prior, packet) {
					return errors.New("identical S9 packet appeared during issuance; recover from the original PACKET-ISSUE event")
				}
				return fmt.Errorf("S9 packet ID %s conflicts with an existing Runtime packet", packet.ID)
			}
			entities, _ := state["entities"].(map[string]any)
			if entities == nil {
				entities = map[string]any{}
				state["entities"] = entities
			}
			rows, _ := entities["work_packets"].([]any)
			entities["work_packets"] = append(rows, row)
			return nil
		},
	}
	committed, err := writer.Update(current.Revision, mutation)
	if err == nil {
		return committed, nil
	}
	latest, readErr := writer.Snapshot()
	if readErr == nil {
		stored, found, findErr := runtimepkg.FindRepairResultPacketContext(runtimepkg.WithPacketBodyRoot(ctx, root), latest.State, packet.ID)
		if findErr == nil && found && sameRepairResultPacket(stored, packet) {
			return latest, nil
		}
	}
	return runtimepkg.Snapshot{}, fmt.Errorf("issue S9 RepairResult packet %s: %w", packet.ID, err)
}

func buildRepairResultPackets(ctx context.Context, root string, state map[string]any, ownerFilter string) ([]workpacket.RepairResultPacket, error) {
	if !runtimepkg.PacketEnabled(state) {
		return nil, errors.New("S9 typed RepairResult packets require the explicit v1.2 packet contract")
	}
	pointer := repairPointer(state)
	if pointer == nil || pointer["session_id"] == "" {
		return []workpacket.RepairResultPacket{}, nil
	}
	planPath := stringField(pointer["plan_ref"])
	planBytes, err := workpacket.ReadFile(root, planPath, repairResultAnswerLimit)
	if err != nil {
		return nil, fmt.Errorf("read current S9 Plan: %w", err)
	}
	var plan RepairPlan
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		return nil, err
	}
	planPackets, err := runtimepkg.CurrentRepairPlanReportPacketsContext(ctx, root, state, ownerFilter)
	if err != nil {
		return nil, err
	}
	retained, err := runtimepkg.RepairResultPacketsForSourcesContext(ctx, root, state, ownerFilter)
	if err != nil {
		return nil, err
	}
	owners := stringMapField(pointer["assignment_owners"])
	packets := []workpacket.RepairResultPacket{}
	ownerFound := false
	for _, assignment := range plan.Assignments {
		owner := owners[assignment.AssignmentID]
		if owner == "" || (ownerFilter != "" && owner != ownerFilter) {
			continue
		}
		ownerFound = true
		var report PlanReport
		var reportRef ArtifactRef
		var reportBytes []byte
		for _, ref := range existingArtifactRefs(pointer["plan_report_refs"]) {
			data, err := workpacket.ReadFile(root, ref.Path, repairResultAnswerLimit)
			if err != nil {
				continue
			}
			var recorded PlanReport
			if json.Unmarshal(data, &recorded) == nil && recorded.AssignmentID == assignment.AssignmentID {
				report, reportRef, reportBytes = recorded, ref, data
				break
			}
		}
		var reportPacket workpacket.RepairPlanReportPacket
		for _, p := range planPackets {
			b := p.Binding
			if (reportRef.Path == "" || p.ReportID == report.ReportID) && b.RepairAssignmentID == assignment.AssignmentID &&
				b.OwnerAgentID == owner && b.RuntimeID == workspace.RuntimeID(state) &&
				b.SessionID == stringField(pointer["session_id"]) && b.PlanRef == planPath {
				reportPacket = p
				break
			}
		}
		if reportPacket.ID == "" {
			return nil, fmt.Errorf("S9 Assignment %s is missing its current Plan questionnaire material", assignment.AssignmentID)
		}
		execution, err := repairResultExecutionFromState(ctx, state, reportPacket.Binding, root)
		if err != nil {
			if errors.Is(err, errRepairResultExecutionNotRegistered) && ownerFilter == "" {
				// A global issue call covers only Assignments that have reached an
				// original prepared Worker execution. An unprepared sibling is not a
				// malformed packet source and must not hide another ready target.
				continue
			}
			if errors.Is(err, errRepairResultExecutionNotRegistered) {
				return nil, fmt.Errorf("S9 Assignment %s has no prepared Worker execution for requested owner %s; prepare that Assignment before issuing its result packet", assignment.AssignmentID, ownerFilter)
			}
			return nil, err
		}
		executionRaw, err := json.Marshal(execution)
		if err != nil {
			return nil, err
		}
		var executionMap map[string]any
		if err := json.Unmarshal(executionRaw, &executionMap); err != nil {
			return nil, err
		}
		projection, err := runtimepkg.ProjectRepairResultExecution(executionMap)
		if err != nil {
			return nil, err
		}
		rework := json.RawMessage("null")
		if execution.ReworkRef != "" {
			reworkBytes, err := workpacket.ReadFile(root, execution.ReworkRef, repairResultAnswerLimit)
			if err != nil {
				return nil, fmt.Errorf("read exact S9 rework source: %w", err)
			}
			rework = json.RawMessage(reworkBytes)
		}
		binding := workpacket.RepairResultBinding{
			Sources: reportPacket.Binding, PlanReportRef: reportRef.Path, ReworkRef: execution.ReworkRef,
		}
		binding.Resume, err = runtimepkg.RepairResultResumeBinding(root, state, binding)
		if err != nil {
			return nil, err
		}
		var existing *workpacket.RepairResultPacket
		for i := range retained {
			p := &retained[i]
			b := p.Binding
			if b.Sources.RuntimeID != execution.RuntimeID || b.Sources.BaselineGeneration != execution.BaselineGeneration ||
				b.Sources.SessionID != reportPacket.Binding.SessionID || b.Sources.PlanRef != planPath ||
				b.Sources.RepairAssignmentID != assignment.AssignmentID || b.Sources.OwnerAgentID != owner ||
				b.PlanReportRef != reportRef.Path || b.ReworkRef != execution.ReworkRef || !sameS9ResumeResponsibility(b.Resume, binding.Resume) {
				continue
			}
			prior, decodeErr := workspace.DecodeExecution(p.Inputs.Execution)
			if decodeErr == nil && prior.Generation == execution.Generation &&
				prior.AssignmentID == execution.AssignmentID && sameAbsolutePath(prior.Path, execution.Path) {
				if existing != nil {
					return nil, fmt.Errorf("S9 Assignment %s has multiple Result questionnaires for its current attempt", assignment.AssignmentID)
				}
				existing = p
			}
		}
		if existing != nil {
			packets = append(packets, *existing)
			continue
		}
		planReport := json.RawMessage(reportBytes)
		if reportRef.Path == "" {
			planReport = json.RawMessage("null")
		}
		inputs := workpacket.RepairResultInputs{
			Sources: reportPacket.Inputs, PlanReport: planReport, Execution: projection, ReworkReceipt: rework,
		}
		packet, err := workpacket.BuildRepairResult(binding, inputs)
		if err != nil {
			return nil, fmt.Errorf("build S9 RepairResult packet for %s: %w", assignment.AssignmentID, err)
		}
		packets = append(packets, packet)
	}
	if ownerFilter != "" && !ownerFound {
		return nil, fmt.Errorf("S9 has no current repair assignment for owner %s", ownerFilter)
	}
	return packets, nil
}

// Resume retains the original accepted resolution and responsibility. Old
// digest fields in an issued packet are historical data, not matching keys.
func sameS9ResumeResponsibility(a, b *workpacket.RepairResultResume) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.PredecessorPacketID == b.PredecessorPacketID && a.BlockerRef == b.BlockerRef &&
		a.ResolutionMessageRef == b.ResolutionMessageRef && a.ResolutionEventID == b.ResolutionEventID &&
		a.ResolutionIdempotencyKey == b.ResolutionIdempotencyKey
}

func repairResultExecutionFromState(ctx context.Context, state map[string]any, binding workpacket.RepairPlanReportBinding, root string) (workspace.Execution, error) {
	raw, err := json.Marshal(state["workspace"])
	if err != nil {
		return workspace.Execution{}, err
	}
	registry, err := workspace.DecodeRegistry(raw)
	if err != nil {
		return workspace.Execution{}, fmt.Errorf("read S9 Worker executions: %w", err)
	}
	identities := map[string]bool{binding.RepairAssignmentID: true, binding.ManifestAssignmentID: true}
	var selected *workspace.Execution
	registeredIdentity := false
	for key, execution := range registry.Executions {
		if !identities[key] && !identities[execution.AssignmentID] {
			continue
		}
		registeredIdentity = true
		if key != execution.AssignmentID {
			return workspace.Execution{}, fmt.Errorf("S9 registered Worker execution key %q differs from its Assignment identity %q", key, execution.AssignmentID)
		}
		if execution.RuntimeID != binding.RuntimeID || execution.BaselineGeneration != binding.BaselineGeneration || execution.AgentID != binding.OwnerAgentID {
			return workspace.Execution{}, fmt.Errorf("S9 registered Worker execution %s differs from the issued Runtime/generation/owner tuple", execution.AssignmentID)
		}
		if selected != nil {
			return workspace.Execution{}, errors.New("S9 typed RepairResult has ambiguous canonical/manifest execution")
		}
		copy := execution
		selected = &copy
	}
	if selected == nil {
		if !registeredIdentity {
			return workspace.Execution{}, errRepairResultExecutionNotRegistered
		}
		return workspace.Execution{}, errors.New("S9 typed RepairResult has no unique registered execution for this owner/Assignment")
	}
	if (selected.Status != "ready" && selected.Status != "complete") || !filepath.IsAbs(selected.Path) || filepath.Clean(selected.Path) != selected.Path {
		return workspace.Execution{}, fmt.Errorf("S9 Worker %s is not prepared at an absolute execution path (status %s)", selected.AssignmentID, selected.Status)
	}

	return *selected, nil
}

func containsRepairResultPacket(values []workpacket.RepairResultPacket, wanted workpacket.RepairResultPacket) bool {
	for _, value := range values {
		if sameRepairResultPacket(value, wanted) {
			return true
		}
	}
	return false
}

func sameRepairResultPacket(a, b workpacket.RepairResultPacket) bool {
	as, bs := a.Binding.Sources, b.Binding.Sources
	return a.ID == b.ID && as.RuntimeID == bs.RuntimeID && as.SessionID == bs.SessionID &&
		as.RepairAssignmentID == bs.RepairAssignmentID && as.OwnerAgentID == bs.OwnerAgentID &&
		sameS9ResumeResponsibility(a.Binding.Resume, b.Binding.Resume)
}

func readIssuedRepairResultRuntime(ctx context.Context, root, statePath, journalPath string) (runtimepkg.Snapshot, *runtimepkg.Store, error) {
	writer := runtimepkg.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(ctx)
	snapshot, err := writer.Snapshot()
	if err != nil {
		return runtimepkg.Snapshot{}, nil, err
	}
	return snapshot, writer, nil
}

// AcceptIssuedRepairResultCandidate saves the original S9 answer through the
// existing WorkspaceCandidate writer and existing WORKSPACE Runtime CAS.
// Professional input never supplies source/check hashes, candidate identity,
// or a completion bit; the source tree and runner receipts are captured here.
func AcceptIssuedRepairResultCandidate(ctx context.Context, root, statePath, journalPath string, req AcceptIssuedRepairResultCandidateRequest) (runtimepkg.Snapshot, WorkspaceCandidate, ArtifactRef, error) {
	defer metrics.Span(ctx, "reception/s9")()
	lockedCtx, unlock, err := integration.LockWorkspace(ctx, root)
	if err != nil {
		return runtimepkg.Snapshot{}, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	defer unlock()
	ctx = lockedCtx
	mainSnapshot, writer, err := readIssuedRepairResultRuntime(ctx, root, statePath, journalPath)
	if err != nil {
		return runtimepkg.Snapshot{}, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	if mainRoot := workspaceMainRoot(root, mainSnapshot.State); !sameAbsolutePath(mainRoot, root) {
		return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, errors.New("S9 candidate receiver root must be the original registered Main checkout")
	}
	packet, found, err := runtimepkg.FindRepairResultPacketContext(runtimepkg.WithPacketBodyRoot(ctx, root), mainSnapshot.State, req.PacketID)
	if err != nil || !found {
		return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, fmt.Errorf("S9 issued RepairResult packet %q is unavailable: %v", req.PacketID, err)
	}
	req.OwnerAgentID = packet.Binding.Sources.OwnerAgentID
	if req.AnswerPath != packet.AnswerPath {
		return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, errors.New("S9 answer path differs from the exact issued packet")
	}
	registry, execution, err := currentRepairExecution(ctx, root, mainSnapshot.State, packet, true)
	if err != nil {
		return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	actualSource := execution.Path
	acceptedOK := execution.DeliveryRef != ""
	var accepted runtimepkg.RepairResultCandidateReceipt
	var acceptedCandidate WorkspaceCandidate
	if acceptedOK {
		data, err := workpacket.ReadFile(root, execution.DeliveryRef, repairResultAnswerLimit)
		if err != nil {
			return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, err
		}
		acceptedCandidate, err = DecodeWorkspaceCandidate(data)
		if err != nil {
			return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, err
		}
		if acceptedCandidate.Provenance == nil || acceptedCandidate.Provenance.PacketID != packet.ID || acceptedCandidate.Provenance.OwnerAgentID != req.OwnerAgentID ||
			acceptedCandidate.Provenance.ExecutionGeneration != execution.Generation ||
			acceptedCandidate.Provenance.ExecutionAssignmentID != execution.AssignmentID {
			return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, runtimepkg.ErrSubmissionConflict
		}
		accepted.Provenance = *acceptedCandidate.Provenance
		accepted.CandidateRef = execution.DeliveryRef
	}
	if acceptedOK {
		return mainSnapshot, acceptedCandidate, ArtifactRef{Path: accepted.CandidateRef}, nil
	}
	if req.SourceRoot == "" || !sameAbsolutePath(req.SourceRoot, actualSource) {
		return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, errors.New("S9 answer source root differs from the registered Worker execution")
	}
	answerBytes := req.AnswerBytes
	if answerBytes == nil {
		answerBytes, err = ReadRepairResultInput(ctx, root, mainSnapshot.State, packet, execution)
	}
	if err != nil {
		return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	answer, problems := workpacket.ValidateRepairResultAnswer(packet, answerBytes)
	if len(problems) != 0 {
		return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, &RepairResultBeforeCandidateError{Cause: &workpacket.AnswerError{Label: "S9 answer", Fields: problems}}
	}
	// G18 §3.2: the issued RepairResult answer is one professional answer and
	// it must not be refused because the Runtime moved for an unrelated
	// reason. The Runtime write count is therefore not compared here. The
	// protection stays in the three domain facts: the received row (the
	// accepted candidate of this Worker attempt, read back above and
	// rechecked inside Apply), the journal-level dedup (the issued candidate
	// event id and idempotency key of this packet) and the Worker attempt's
	// own version (generation, rework reference and execution path).
	current := mainSnapshot
	provenance, err := newRepairResultCandidateProvenance(ctx, root, current, packet, execution, answer)
	if err != nil {
		return current, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	proposal, err := repairResultProposal(packet, answer, execution, registry.MainRoot, provenance)
	if err != nil {
		return current, WorkspaceCandidate{}, ArtifactRef{}, &RepairResultBeforeCandidateError{Cause: err}
	}
	proposal.issued = &issuedRepairResultCapability{packet: packet, provenance: provenance, beforeRevision: current.Revision}
	// The same original consumer accepts either a live original or an already
	// retained original. Source inspection and Git integration are Main work,
	// not a prerequisite for recording the author's professional result.
	//
	// A Candidate is registered only when the author's own answer states a
	// professional conclusion. Readiness is read from that answer alone: the
	// original Plan's unit set is not replayed against it, and no digest,
	// revision or round is compared.
	var professional workpacket.RepairResultProfessionalAnswer
	if err := json.Unmarshal(provenance.SubmittedAnswer, &professional); err != nil {
		return current, WorkspaceCandidate{}, ArtifactRef{}, &RepairResultBeforeCandidateError{Cause: err}
	}
	if err := RepairResultAnswerReady(professional); err != nil {
		return current, WorkspaceCandidate{}, ArtifactRef{}, &RepairResultBeforeCandidateError{Cause: fmt.Errorf("%s: %w", filepath.Join(execution.Path, packet.AnswerPath), err)}
	}
	candidate, ref, err := createReceivedRepairCandidate(root, packet, execution, proposal, provenance)
	if err != nil {
		return current, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	key := "runtime:s9:issued-repair-candidate:" + packet.ID
	eventID := "evt-s9-issued-repair-candidate-" + packet.ID
	evidenceIDs := []string{ref.Path, packet.ID, packet.Binding.Sources.RepairAssignmentID}
	at := occurred(req.OccurredAt)
	currentBaseline, _ := current.State["baseline"].(map[string]any)
	mutation := runtimepkg.Mutation{
		EventID: eventID, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: req.OwnerAgentID,
		IdempotencyKey: key, RuntimeID: packet.Binding.Sources.RuntimeID, EvidenceIDs: evidenceIDs,
		From: cursor(current.State), To: cursor(current.State), BaselineGeneration: integerRepairResult(currentBaseline["generation"]),
		RetainLastTransition: true, OccurredAt: at, Message: "accepted issued S9 professional result as an immutable workspace candidate; Main integration remains pending",
		Apply: func(state map[string]any) error {
			workspaceRegistry, freshExecution, err := currentRepairExecution(ctx, root, state, packet, true)
			if err != nil {
				return err
			}
			if freshExecution.Generation != execution.Generation || freshExecution.ReworkRef != execution.ReworkRef || !sameAbsolutePath(freshExecution.Path, execution.Path) {
				return errors.New("S9 registered Worker attempt changed before candidate CAS")
			}
			// CAS rechecks the selected responsibility, never mutable author
			// bytes or Git/evidence after the original reception read.
			entry := workspaceRegistry.Executions[freshExecution.AssignmentID]
			if entry.AssignmentID != freshExecution.AssignmentID || entry.DeliveryRef != "" && entry.DeliveryRef != ref.Path {
				return errors.New("S9 Worker already has a conflicting delivery candidate")
			}
			if entry.DeliveryRef == ref.Path {
				return errors.New("identical S9 candidate appeared during receipt; recover its original WORKSPACE event")
			}
			entry.DeliveryRef = ref.Path
			workspaceRegistry.Executions[freshExecution.AssignmentID] = entry
			state["workspace"] = workspace.Encode(workspaceRegistry)
			return nil
		},
	}
	// G18 §3.2: the candidate is recorded against the current Runtime state
	// (-1 is the omission marker). The Runtime write count is not asserted
	// here, so this professional answer is not refused because the Runtime
	// moved for an unrelated reason. Apply revalidates the domain facts
	// (the selected Worker attempt and the unique delivery candidate of this
	// attempt) and the journal deduplicates this packet's candidate event.
	next, err := writer.UpdateCurrent(mutation)
	if err != nil {
		if recovered, recoveredSnapshot, recoveredCandidate, recoveredRef, recoverErr := recoverAcceptedRepairResultCandidate(root, writer, packet, req, answer, actualSource); recoverErr == nil && recovered {
			return recoveredSnapshot, recoveredCandidate, recoveredRef, nil
		}
		return current, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	return next, candidate, ref, nil
}

// RepairResultAnswerReady reports whether the author's own professional answer
// states a conclusion that may be registered as a Candidate. Readiness is a
// property of that answer alone: a completed verdict plus at least one
// recorded unit result. A non-passing unit is still a professional conclusion
// and stays receivable; only the completion bit changes. The original Plan's
// unit set is never replayed against the answer, and no digest, revision or
// round is compared.
func RepairResultAnswerReady(answer workpacket.RepairResultProfessionalAnswer) error {
	if answer.Verdict != "completed" {
		return fmt.Errorf("S9 typed result verdict %q is preserved for named recovery; this slice only accepts completed candidate delivery", answer.Verdict)
	}
	if len(answer.UnitResults) == 0 {
		return errors.New("S9 completed result records no unit result; the author's professional conclusion is missing at /answer/unit_results")
	}
	return nil
}

// ensureRepairResultCompleted reports whether the author's own answer states a
// finished professional result. It reads that submitted answer only: no digest,
// revision or round is compared, and the original Plan's unit set is not
// replayed against it.
func ensureRepairResultCompleted(raw json.RawMessage) error {
	var answer workpacket.RepairResultProfessionalAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return err
	}
	if err := RepairResultAnswerReady(answer); err != nil {
		return err
	}
	for _, unit := range answer.UnitResults {
		if unit.Status != "pass" {
			return errors.New("S9 completed result has a non-passing unit; preserve it for named recovery")
		}
	}
	return nil
}

func currentRepairExecution(ctx context.Context, root string, state map[string]any, packet workpacket.RepairResultPacket, allowDelivered bool) (*workspace.ExecutionRegistry, workspace.Execution, error) {
	b := packet.Binding.Sources
	if stringField(state["runtime_id"]) != b.RuntimeID {
		return nil, workspace.Execution{}, errors.New("S9 packet is outside the current Runtime generation")
	}
	raw, err := json.Marshal(state["workspace"])
	if err != nil {
		return nil, workspace.Execution{}, err
	}
	decoded, err := workspace.DecodeRegistry(raw)
	if err != nil {
		return nil, workspace.Execution{}, err
	}
	registry := &decoded
	var selected *workspace.Execution
	for key, execution := range registry.Executions {
		if key != execution.AssignmentID || (execution.AssignmentID != b.RepairAssignmentID && execution.AssignmentID != b.ManifestAssignmentID) || execution.RuntimeID != b.RuntimeID || execution.BaselineGeneration != b.BaselineGeneration || execution.AgentID != b.OwnerAgentID {
			continue
		}
		if selected != nil {
			return nil, workspace.Execution{}, errors.New("S9 packet resolves to multiple current canonical/manifest Worker executions")
		}
		copy := execution
		selected = &copy
	}
	if selected == nil {
		return nil, workspace.Execution{}, errors.New("S9 packet has no exact current registered Worker execution")
	}
	if (selected.Status != "ready" && !(allowDelivered && selected.Status == "complete")) || (!allowDelivered && selected.DeliveryRef != "") || selected.ReworkRef != packet.Binding.ReworkRef {
		return nil, workspace.Execution{}, errors.New("S9 candidate requires the current ready execution and attempt")
	}
	assigned, decodeErr := workspace.DecodeExecution(packet.Inputs.Execution)
	if decodeErr != nil ||
		assigned.AssignmentID != selected.AssignmentID || assigned.RuntimeID != selected.RuntimeID ||
		assigned.AgentID != selected.AgentID || assigned.BaselineGeneration != selected.BaselineGeneration ||
		assigned.Generation != selected.Generation || !sameAbsolutePath(assigned.Path, selected.Path) {
		return nil, workspace.Execution{}, errors.New("S9 result packet belongs to a different Worker attempt")
	}
	return registry, *selected, nil
}

func latestRepairResultToolReceipt(refs []checkexec.ToolReceipt, profile checkexec.Profile) (checkexec.ToolReceipt, bool) {
	for index := len(refs) - 1; index >= 0; index-- {
		if refs[index].Command == profile.Command {
			return refs[index], true
		}
	}
	return checkexec.ToolReceipt{}, false
}

func newRepairResultCandidateProvenance(ctx context.Context, root string, snapshot runtimepkg.Snapshot, packet workpacket.RepairResultPacket, execution workspace.Execution, answer json.RawMessage) (runtimepkg.RepairResultCandidateProvenance, error) {
	// Existing check outputs remain candidate facts; receiving a Result does
	// not re-prove registered tool receipts or their source observations.
	workerReceipts := []checkexec.ToolReceipt{}
	if execution.CheckLocation == "worker" {
		for index := range execution.Checks {
			if profile, ok := workspace.ProfileAt(execution, index); ok {
				if ref, ok := latestRepairResultToolReceipt(execution.ToolCheckReceipts, profile); ok {
					workerReceipts = append(workerReceipts, ref)
				}
			}
		}
	}
	ownedReceipts := make([]runtimepkg.RepairResultOwnedCheckReceipt, len(execution.OwnedCheckReceipts))
	for i, ref := range execution.OwnedCheckReceipts {
		ownedReceipts[i] = runtimepkg.RepairResultOwnedCheckReceipt{WorkerCheckIndex: ref.WorkerCheckIndex, Receipt: ref.Receipt}
	}
	b := packet.Binding
	return runtimepkg.RepairResultCandidateProvenance{
		Contract: repairResultCandidateContract, PacketID: packet.ID,
		IssuanceEventID: packet.IssuanceEventID, SubmittedAnswer: append(json.RawMessage(nil), answer...),
		OwnerAgentID: b.Sources.OwnerAgentID, RuntimeID: b.Sources.RuntimeID, BaselineGeneration: b.Sources.BaselineGeneration,
		ExecutionAssignmentID: execution.AssignmentID, ExecutionGeneration: execution.Generation,
		ExecutionRoot: execution.Path,
		PlanReportRef: b.PlanReportRef, ReworkRef: b.ReworkRef,
		Resume:              b.Resume,
		WorkerCheckReceipts: workerReceipts, OwnedCheckReceipts: ownedReceipts,
	}, nil
}

func repairResultProposal(packet workpacket.RepairResultPacket, raw json.RawMessage, execution workspace.Execution, mainRoot string, provenance runtimepkg.RepairResultCandidateProvenance) (RepairResultRequest, error) {
	var answer workpacket.RepairResultProfessionalAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return RepairResultRequest{}, err
	}
	units := make([]RepairUnitResult, len(answer.UnitResults))
	for i, unit := range answer.UnitResults {
		units[i] = RepairUnitResult{UnitID: unit.UnitID, Status: unit.Status, EvidenceRefs: append([]string{}, unit.EvidenceRefs...), ImplementationSummary: unit.ImplementationSummary, RestoredInvariantRefs: append([]string{}, unit.RestoredInvariantRefs...)}
	}
	before := make([]RepairBeforeFixResult, len(answer.BeforeFixResults))
	for i, result := range answer.BeforeFixResults {
		before[i] = RepairBeforeFixResult{AssertionID: result.AssertionID, ObservedRed: result.ObservedRed, EvidenceRefs: append([]string{}, result.EvidenceRefs...)}
	}
	checks := []RepairCheck{}
	if execution.CheckLocation == "worker" {
		for _, ref := range provenance.WorkerCheckReceipts {
			checks = append(checks, RepairCheck{Name: ref.Command, Command: ref.Command, Result: "pass", EvidenceRefs: []string{ref.Path}})
		}
	}
	// The issued packet already carries the original accepted PlanReport.
	// Preserve its professional precheck observations when accepting the
	// original Result; no evidence files or historical digests are read here.
	var report struct {
		RedChecks []RepairCheck `json:"red_checks"`
	}
	if err := json.Unmarshal(packet.Inputs.PlanReport, &report); err != nil {
		return RepairResultRequest{}, fmt.Errorf("decode original PlanReport prechecks: %w", err)
	}
	return RepairResultRequest{
		Contract: ContractRef{Path: packet.Binding.Sources.ContractRef},
		Session:  ArtifactRef{ID: packet.Binding.Sources.SessionID, Path: packet.Binding.Sources.SessionRef},
		Plan:     ArtifactRef{ID: packet.Binding.Sources.PlanID, Path: packet.Binding.Sources.PlanRef},
		ResultID: packet.ResultID, ProducerAgentID: packet.Binding.Sources.OwnerAgentID, UnitResults: units,
		Verdict: answer.Verdict, BeforeFixResults: before, DetectionGapArtifacts: append([]string{}, answer.DetectionGapArtifacts...), EvidenceRefs: append([]string{}, answer.EvidenceRefs...),
		ChangedArtifacts: []ChangedArtifact{}, Result: repairResultUnitOutcome(units), AssignmentID: packet.Binding.Sources.RepairAssignmentID,
		PlanReport:      ArtifactRef{Path: packet.Binding.PlanReportRef},
		BeforeFixChecks: append([]RepairCheck{}, report.RedChecks...), Checks: checks, ScopeDeviations: append([]string{}, answer.ScopeDeviations...),
		MigrationRef: answer.MigrationRef, RollbackRef: answer.RollbackRef, ResidualRisks: append([]string{}, answer.ResidualRisks...),
	}, nil
}

// repairResultUnitOutcome derives the completion bit from the author's own
// recorded unit results. The bit is never inferred from the checks, from the
// mere presence of a verdict, or from an unanswered unit set: a unit that is
// not recorded as passed is never a pass.
func repairResultUnitOutcome(units []RepairUnitResult) string {
	if len(units) == 0 {
		return "blocked"
	}
	outcome := "pass"
	for _, unit := range units {
		switch unit.Status {
		case "fail":
			return "fail"
		case "blocked":
			outcome = "blocked"
		case "pass":
		default:
			outcome = "blocked"
		}
	}
	return outcome
}

func validateRepairResultMainThreadlessCandidate(root string, state map[string]any, packet workpacket.RepairResultPacket, execution workspace.Execution, provenance runtimepkg.RepairResultCandidateProvenance, candidate ArtifactRef) error {
	if candidate.Path == "" {
		return errors.New("S9 candidate original path is missing")
	}
	if provenance.PacketID != packet.ID || provenance.OwnerAgentID != packet.Binding.Sources.OwnerAgentID || provenance.ExecutionRoot != execution.Path || provenance.ExecutionAssignmentID != execution.AssignmentID {
		return errors.New("S9 candidate typed provenance differs from current original execution")
	}
	return nil
}

func workspaceMainRoot(fallback string, state map[string]any) string {
	raw, err := json.Marshal(state["workspace"])
	if err != nil {
		return fallback
	}
	registry, decodeErr := workspace.DecodeRegistry(raw)
	if decodeErr == nil && registry.MainRoot != "" {
		return registry.MainRoot
	}
	return fallback
}

func integerRepairResult(value any) int {
	switch n := value.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		v, _ := n.Int64()
		return int(v)
	default:
		return 0
	}
}

func recoverAcceptedRepairResultCandidate(root string, writer *runtimepkg.Store, packet workpacket.RepairResultPacket, req AcceptIssuedRepairResultCandidateRequest, answer json.RawMessage, sourceRoot string) (bool, runtimepkg.Snapshot, WorkspaceCandidate, ArtifactRef, error) {
	snapshot, err := writer.Snapshot()
	if err != nil {
		return false, runtimepkg.Snapshot{}, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	_, execution, err := currentRepairExecution(context.Background(), root, snapshot.State, packet, true)
	if err != nil || execution.DeliveryRef == "" {
		return false, snapshot, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	if !sameAbsolutePath(execution.Path, sourceRoot) {
		return false, snapshot, WorkspaceCandidate{}, ArtifactRef{}, runtimepkg.ErrSubmissionConflict
	}
	candidateRef := ArtifactRef{Path: execution.DeliveryRef}
	data, err := workpacket.ReadFile(root, candidateRef.Path, repairResultAnswerLimit)
	if err != nil {
		return false, snapshot, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	candidate, err := DecodeWorkspaceCandidate(data)
	if err != nil {
		return false, snapshot, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	if candidate.Provenance == nil || candidate.Provenance.PacketID != packet.ID ||
		candidate.Provenance.ExecutionGeneration != execution.Generation || !sameSubmittedAnswer(candidate.Provenance.SubmittedAnswer, answer) {
		return false, snapshot, WorkspaceCandidate{}, ArtifactRef{}, runtimepkg.ErrSubmissionConflict
	}
	return true, snapshot, candidate, candidateRef, nil
}

// RepairResultWorkComplete reads accepted delivery facts for the owner's current
// attempt. Main integration and its actual checks remain separate work.
func RepairResultWorkComplete(root string, state map[string]any, packetID, owner string) (bool, error) {
	return RepairResultWorkCompleteWithContext(context.Background(), root, state, packetID, owner)
}

// RepairResultWorkCompleteWithContext projects progress from the supplied Runtime
// snapshot without replaying candidate submission or checking the live checkout.
func RepairResultWorkCompleteWithContext(ctx context.Context, root string, state map[string]any, packetID, owner string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return false, err
		}
	}
	packet, found, err := runtimepkg.FindRepairResultPacketContext(runtimepkg.WithPacketBodyRoot(ctx, root), state, packetID)
	if err != nil || !found {
		return false, err
	}
	raw, err := json.Marshal(state["workspace"])
	if err != nil {
		return false, err
	}
	registry, err := workspace.DecodeRegistry(raw)
	if err != nil {
		return false, err
	}
	executions, err := RepairResultCandidateExecutions(packet, registry, owner, false)
	if err != nil {
		return false, err
	}
	for _, execution := range executions {
		if execution.DeliveryRef == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		data, err := workpacket.ReadFile(root, execution.DeliveryRef, 20<<20)
		if err != nil {
			return false, err
		}
		candidate, err := DecodeWorkspaceCandidate(data)
		if err != nil {
			return false, err
		}
		if candidate.Provenance == nil || candidate.Provenance.PacketID != packetID || candidate.Provenance.OwnerAgentID != owner {
			continue
		}
		if candidate.Proposal.Verdict != "completed" || candidate.Proposal.Result != "pass" {
			return false, nil
		}
		return repairResultAttemptStillCurrent(state, *candidate.Provenance, execution.DeliveryRef), nil
	}
	return false, nil
}

// RepairResultCandidateExecutions locates only the execution frozen in this
// issued packet. Historical receipt reads may retain that slot in History;
// current work-complete projection never treats History as current work.
func RepairResultCandidateExecutions(packet workpacket.RepairResultPacket, registry workspace.ExecutionRegistry, owner string, includeHistory bool) ([]workspace.Execution, error) {
	frozen, err := workspace.DecodeExecution(packet.Inputs.Execution)
	if err != nil {
		return nil, err
	}
	b := packet.Binding.Sources
	if owner == "" || b.OwnerAgentID != owner || frozen.AgentID != owner || frozen.RuntimeID != b.RuntimeID || frozen.BaselineGeneration != b.BaselineGeneration ||
		(frozen.AssignmentID != b.ManifestAssignmentID && frozen.AssignmentID != b.RepairAssignmentID) || frozen.Path == "" {
		return nil, fmt.Errorf("S9 original packet execution locator differs from its responsibility")
	}
	var executions []workspace.Execution
	for _, e := range registry.Executions {
		executions = append(executions, e)
	}
	if includeHistory {
		executions = append(executions, registry.History...)
	}
	var selected []workspace.Execution
	for _, e := range executions {
		if e.AssignmentID != frozen.AssignmentID || e.AgentID != owner || e.RuntimeID != frozen.RuntimeID || e.BaselineGeneration != frozen.BaselineGeneration ||
			e.Generation != frozen.Generation || !sameAbsolutePath(e.Path, frozen.Path) || e.ReworkRef != packet.Binding.ReworkRef {
			continue
		}
		// Historical acceptance survives subsequent definition drift in the
		// same retained slot. Current completion still needs its frozen work.
		if includeHistory {
			selected = append(selected, e)
			continue
		}
		selected = append(selected, e)
	}
	return selected, nil
}

func repairResultAttemptStillCurrent(state map[string]any, provenance runtimepkg.RepairResultCandidateProvenance, candidateRef string, _ ...string) bool {
	if stringField(state["runtime_id"]) != provenance.RuntimeID {
		return false
	}
	raw, err := json.Marshal(state["workspace"])
	if err != nil {
		return false
	}
	registry, err := workspace.DecodeRegistry(raw)
	if err != nil {
		return false
	}
	var selected *workspace.Execution
	for _, execution := range registry.Executions {
		if execution.RuntimeID != provenance.RuntimeID || execution.BaselineGeneration != provenance.BaselineGeneration ||
			execution.AgentID != provenance.OwnerAgentID || execution.AssignmentID != provenance.ExecutionAssignmentID ||
			execution.Generation != provenance.ExecutionGeneration {
			continue
		}
		if selected != nil {
			return false
		}
		copy := execution
		selected = &copy
	}
	if selected == nil || (selected.Status != "ready" && selected.Status != "complete") || selected.ReworkRef != provenance.ReworkRef ||
		selected.DeliveryRef != candidateRef || !sameAbsolutePath(selected.Path, provenance.ExecutionRoot) {
		return false
	}
	return true
}

// SubmitIssuedRepairResult records the accepted candidate after Main's real
// integration and necessary checks. Previously accepted Finals are read-only.
func SubmitIssuedRepairResult(ctx context.Context, root, statePath, journalPath string, req SubmitIssuedRepairResultRequest) (runtimepkg.Snapshot, RepairResult, ArtifactRef, error) {
	ctx, unlock, err := integration.LockWorkspace(ctx, root)
	if err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
	}
	defer unlock()
	current, _, err := readIssuedRepairResultRuntime(ctx, root, statePath, journalPath)
	if err != nil {
		return runtimepkg.Snapshot{}, RepairResult{}, ArtifactRef{}, err
	}
	if !sameAbsolutePath(workspaceMainRoot(root, current.State), root) {
		return current, RepairResult{}, ArtifactRef{}, errors.New("S9 final Result root differs from the current Main checkout")
	}
	packet, found, err := runtimepkg.FindRepairResultPacketContext(runtimepkg.WithPacketBodyRoot(ctx, root), current.State, req.PacketID)
	if err != nil || !found {
		return current, RepairResult{}, ArtifactRef{}, fmt.Errorf("S9 Result questionnaire %q is unavailable: %v", req.PacketID, err)
	}
	req.OwnerAgentID = packet.Binding.Sources.OwnerAgentID
	if result, ref, found, err := existingIssuedRepairResult(root, current.State, packet.ResultID); err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
	} else if found {
		if result.Receipt == nil || result.Receipt.Provenance.PacketID != packet.ID {
			return current, RepairResult{}, ArtifactRef{}, runtimepkg.ErrSubmissionConflict
		}
		// This original packet's result was already accepted. Replaying it must
		// not authenticate the caller's re-supplied, possibly historical Main
		// facts or read/check the original checks a second time.
		return current, result, ref, nil
	}
	// G18 §3.2: recording the accepted Final is a professional act and must
	// not be refused because the Runtime moved for an unrelated reason. The
	// Runtime write count is therefore not compared here. The protection
	// stays in the three domain facts: the received row (the already accepted
	// Final of this packet, replayed above), the journal-level dedup (the
	// issued receipt's accepted event id and idempotency key) and the
	// candidate's own version (the Worker attempt and its delivery
	// candidate, revalidated inside Apply).
	_, execution, err := currentRepairExecution(ctx, root, current.State, packet, true)
	if err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
	}
	if execution.DeliveryRef == "" {
		return current, RepairResult{}, ArtifactRef{}, errors.New("S9 Worker candidate remains pending")
	}
	candidateRef := ArtifactRef{Path: execution.DeliveryRef}
	candidate, err := ValidateWorkspaceCandidate(root, current.State, execution, candidateRef)
	if err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
	}
	if candidate.Provenance == nil || candidate.Provenance.PacketID != packet.ID {
		return current, RepairResult{}, ArtifactRef{}, errors.New("S9 candidate belongs to a different Result questionnaire")
	}
	provenance := *candidate.Provenance
	// The final Result must not cover a unit the author did not complete. This
	// guard reads the author's own submitted answer only: no digest, revision
	// or round is compared, and the original Plan's unit set is not replayed.
	if err := ensureRepairResultCompleted(provenance.SubmittedAnswer); err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
	}
	var checks []RepairCheck
	var checkpoint integration.Checkpoint
	var proof runtimepkg.RepairResultMainProof
	if req.Checkpoint != nil {
		// Main's actual integration/check operation supplies its already
		// executed facts. Saving Final does not rerun that operation or reopen
		// Builder evidence, private originals, source proofs or check logs.
		checkpoint, checks, proof = *req.Checkpoint, append([]RepairCheck{}, req.MainChecks...), req.MainProof
		if checkpoint.AssignmentID != execution.AssignmentID || (checkpoint.State != integration.StateVerified && checkpoint.State != integration.StateAcknowledged && checkpoint.State != integration.StateCleanupPending && checkpoint.State != integration.StateComplete) || !allChecksPass(checks) || len(checks) == 0 {
			return current, RepairResult{}, ArtifactRef{}, errors.New("Main actual integration and necessary checks remain incomplete")
		}
		checkpointData, err := json.Marshal(checkpoint)
		if err != nil {
			return current, RepairResult{}, ArtifactRef{}, fmt.Errorf("encode actual Main checkpoint: %w", err)
		}
		proof.VerifiedFacts, err = runtimepkg.ProjectRepairResultMainFacts(checkpointData)
		if err != nil {
			return current, RepairResult{}, ArtifactRef{}, fmt.Errorf("project actual Main checkpoint: %w", err)
		}
		candidate.Proposal.ChangedArtifacts, err = IntegratedRepairChanges(ctx, root, checkpoint)
		if err != nil {
			return current, RepairResult{}, ArtifactRef{}, err
		}
	} else {
		checks, err = repairResultMainChecks(root, packet, provenance)
		if err != nil {
			return current, RepairResult{}, ArtifactRef{}, err
		}
		checkpoint, proof, err = readRepairResultMainProof(root, packet, provenance, checks)
		if err != nil {
			return current, RepairResult{}, ArtifactRef{}, err
		}
	}
	journal, _ := current.State["journal"].(map[string]any)
	receipt := runtimepkg.RepairResultReceipt{
		Contract: repairResultFinalContract, Provenance: provenance,
		CandidateRef: candidateRef.Path,
		ResultID:     packet.ResultID, ResultRef: responsibilityArtifactPath("results", packet.ResultID), MainProof: proof, WorkComplete: true,
		AcceptedEventID:              "evt-s9-issued-repair-result-" + packet.ID,
		AcceptedIdempotencyKey:       "runtime:s9:issued-repair-result:" + packet.ID,
		AcceptedAfterRuntimeRevision: current.Revision + 1, AcceptedSequence: integerRepairResult(journal["last_sequence"]) + 1,
	}
	capability := &issuedRepairResultFinalCapability{packet: packet, provenance: provenance, receipt: receipt, ctx: ctx, mainRoot: root}
	proposal := candidate.Proposal
	proposal.Checks, proposal.ResultID = checks, packet.ResultID
	proposal.ProducerAgentID, proposal.AssignmentID = execution.AgentID, packet.Binding.Sources.RepairAssignmentID
	proposal.PlanReport = ArtifactRef{Path: packet.Binding.PlanReportRef}
	proposal.Verdict, proposal.Result = "completed", "pass"
	proposal.Receipt, proposal.issuedFinal = &receipt, capability
	proposal.OccurredAt = occurred(req.OccurredAt)
	return submitIssuedRepairResultToRuntime(root, statePath, journalPath, submitIssuedResultRuntimeRequest{
		// G18 §3.2: the caller no longer asserts a Runtime write count (-1 is
		// the omission marker). The original Writer records the Final against
		// the current state and revalidates the candidate association inside
		// Apply.
		RuntimeRequest: RuntimeRequest{Context: ctx, ExpectedRevision: -1, Actor: execution.AgentID, OccurredAt: proposal.OccurredAt},
		Result:         proposal, issued: capability,
	})
}

// validate is the Writer's current-association check, not another replay of
// packet/source receipts or Main's already recorded checks.
func (capability *issuedRepairResultFinalCapability) validate(root string, state map[string]any, request RepairResultRequest) error {
	if capability == nil || request.issuedFinal != capability {
		return errors.New("S9 final Result is missing its current candidate association")
	}
	packet := capability.packet
	pointer := repairPointer(state)
	if pointer == nil || stringField(pointer["session_id"]) != packet.Binding.Sources.SessionID ||
		stringField(pointer["plan_ref"]) != packet.Binding.Sources.PlanRef {
		return errors.New("S9 current Session/Plan changed before Result recording")
	}
	_, execution, err := currentRepairExecution(capability.ctx, root, state, packet, true)
	if err != nil {
		return err
	}
	p := capability.provenance
	if execution.DeliveryRef != capability.receipt.CandidateRef || execution.Generation != p.ExecutionGeneration ||
		execution.AssignmentID != p.ExecutionAssignmentID || execution.AgentID != p.OwnerAgentID ||
		execution.ReworkRef != p.ReworkRef || !sameAbsolutePath(execution.Path, p.ExecutionRoot) ||
		request.ResultID != packet.ResultID || request.AssignmentID != packet.Binding.Sources.RepairAssignmentID ||
		request.ProducerAgentID != execution.AgentID {
		return errors.New("S9 current execution attempt/candidate changed before Result recording")
	}
	return nil
}

func repairResultIssueCoordinates(root string, state map[string]any, packet workpacket.RepairResultPacket) (int, time.Time, error) {
	data, err := controlproof.ReadJournalForState(root, state)
	if err != nil {
		return 0, time.Time{}, err
	}
	journal, err := controlproof.InspectJournalData(data)
	if err != nil {
		return 0, time.Time{}, err
	}
	if err := controlproof.ValidateStateJournalPair(state, journal); err != nil {
		return 0, time.Time{}, err
	}
	index, found := journal.EventIndex[packet.IssuanceEventID]
	if !found {
		return 0, time.Time{}, errors.New("S9 original RepairResult PACKET-ISSUE event is missing")
	}
	event := journal.Events[index]
	after, err := integerRepairResultEvent(event, "after_revision")
	before, beforeErr := integerRepairResultEvent(event, "before_revision")
	baselineGeneration, generationErr := integerRepairResultEvent(event, "baseline_generation")
	currentRevision := integerRepairResult(state["revision"])
	actor, _ := event["actor"].(map[string]any)
	from, _ := event["from"].(map[string]any)
	to, _ := event["to"].(map[string]any)
	if err != nil || beforeErr != nil || generationErr != nil ||
		event["event"] != "transition_committed" || event["outcome"] != "committed" ||
		event["transition_id"] != "PACKET-ISSUE" || event["event_id"] != packet.IssuanceEventID ||
		event["idempotency_key"] != "packet:"+packet.ID || event["runtime_id"] != packet.Binding.Sources.RuntimeID ||
		actor["type"] != "orchestrator" || actor["id"] != "orchestrator" ||
		baselineGeneration < 1 || after != before+1 || after > currentRevision ||
		!reflect.DeepEqual(from, to) {
		return 0, time.Time{}, errors.New("S9 original RepairResult issue event coordinates are invalid")
	}
	text, _ := event["occurred_at"].(string)
	at, err := time.Parse(time.RFC3339Nano, text)
	if err != nil || text == "" {
		return 0, time.Time{}, errors.New("S9 original RepairResult issue timestamp is invalid")
	}
	return after, at.UTC(), nil
}

func integerRepairResultEvent(event map[string]any, key string) (int, error) {
	switch n := event[key].(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case json.Number:
		v, err := n.Int64()
		return int(v), err
	default:
		return 0, fmt.Errorf("S9 event %s is not an integer", key)
	}
}

func repairResultCheckpointPath(root string, packet workpacket.RepairResultPacket) (string, string, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	root = absoluteRoot
	var execution struct {
		AssignmentID string                            `json:"assignment_id"`
		Record       *assignmentrecord.RecordReference `json:"assignment_record"`
	}
	if err := json.Unmarshal(packet.Inputs.Execution, &execution); err != nil || execution.AssignmentID == "" {
		return "", "", errors.New("S9 frozen execution lacks an original checkpoint slot identity")
	}
	path := ""
	if execution.Record != nil {
		path = assignmentrecord.IntegrationCheckpointPath(root, *execution.Record)
	} else {
		path = filepath.Join(root, ".claude", "evidence", packet.Binding.Sources.RuntimeID, fmt.Sprintf("g%d", packet.Binding.Sources.BaselineGeneration), "worktree", execution.AssignmentID, "checkpoint.json")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || !filepath.IsLocal(relative) {
		return "", "", errors.New("S9 original checkpoint path escaped the Main root")
	}
	return absolute, filepath.ToSlash(relative), nil
}

func repairResultMainChecks(root string, packet workpacket.RepairResultPacket, provenance runtimepkg.RepairResultCandidateProvenance) ([]RepairCheck, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root = absoluteRoot
	_, checkpointPath, err := repairResultCheckpointPath(root, packet)
	if err != nil {
		return nil, err
	}
	data, err := workpacket.ReadFile(root, checkpointPath, 20<<20)
	if err != nil {
		return nil, fmt.Errorf("read current S9 Main checkpoint: %w", err)
	}
	checkpoint, err := integration.DecodeCheckpoint(data)
	if err != nil {
		return nil, fmt.Errorf("decode S9 Main checkpoint: %w", err)
	}
	out := []RepairCheck{}
	byCommand := map[string]int{}
	for _, path := range checkpoint.CheckReceipts {
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var executed struct {
			Command string `json:"command"`
			CWD     string `json:"cwd"`
			Status  string `json:"status"`
			Exit    int    `json:"exit_code"`
		}
		if err := json.Unmarshal(data, &executed); err != nil {
			return nil, err
		}
		if executed.Status != "pass" || executed.Exit != 0 {
			return nil, errors.New("S9 Main checks remain pending: a necessary check did not pass")
		}
		cwd := executed.CWD
		if cwd != "" && !filepath.IsAbs(cwd) {
			cwd = filepath.Join(root, cwd)
		}
		if strings.TrimSpace(executed.Command) == "" || cwd == "" || !sameAbsolutePath(cwd, root) {
			return nil, errors.New("S9 Main check lacks its actual command or Main checkout")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || !filepath.IsLocal(relative) {
			return nil, errors.New("S9 Main check receipt escaped the Main root")
		}
		check := RepairCheck{Name: "verified Main integration", Command: executed.Command, Result: "pass", EvidenceRefs: []string{"path:" + filepath.ToSlash(relative)}}
		if index, exists := byCommand[executed.Command]; exists {
			out[index] = check
		} else {
			byCommand[executed.Command] = len(out)
			out = append(out, check)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("S9 Main checks remain pending: no successful actual check")
	}
	_ = provenance // Candidate association is checked against the checkpoint below.
	return out, nil
}

func readRepairResultMainProof(root string, packet workpacket.RepairResultPacket, provenance runtimepkg.RepairResultCandidateProvenance, checks []RepairCheck) (integration.Checkpoint, runtimepkg.RepairResultMainProof, error) {
	var zero integration.Checkpoint
	_, relative, err := repairResultCheckpointPath(root, packet)
	if err != nil {
		return zero, runtimepkg.RepairResultMainProof{}, err
	}
	data, err := workpacket.ReadFile(root, relative, 20<<20)
	if err != nil {
		return zero, runtimepkg.RepairResultMainProof{}, fmt.Errorf("read registered S9 Main checkpoint: %w", err)
	}
	checkpoint, err := integration.DecodeCheckpoint(data)
	if err != nil {
		return zero, runtimepkg.RepairResultMainProof{}, err
	}
	switch checkpoint.State {
	case integration.StateVerified, integration.StateAcknowledged, integration.StateCleanupPending, integration.StateComplete:
	default:
		return zero, runtimepkg.RepairResultMainProof{}, fmt.Errorf("S9 Main checkpoint state %q is not verified", checkpoint.State)
	}
	facts, err := runtimepkg.ProjectRepairResultMainFacts(data)
	if err != nil {
		return zero, runtimepkg.RepairResultMainProof{}, err
	}
	proof := runtimepkg.RepairResultMainProof{
		CheckpointRef: relative, VerifiedFacts: facts,
	}
	if checkpoint.AssignmentID != provenance.ExecutionAssignmentID || checkpoint.BaselineGeneration != provenance.BaselineGeneration ||
		!sameAbsolutePath(checkpoint.WorktreePath, provenance.ExecutionRoot) {
		return zero, runtimepkg.RepairResultMainProof{}, errors.New("S9 Main integration has not checked this candidate's actual source")
	}
	if len(checks) == 0 || !allChecksPass(checks) {
		return zero, runtimepkg.RepairResultMainProof{}, errors.New("S9 necessary Main checks remain pending")
	}
	return checkpoint, proof, nil
}

func existingIssuedRepairResult(root string, state map[string]any, resultID string) (RepairResult, ArtifactRef, bool, error) {
	pointer := repairPointer(state)
	if pointer == nil {
		return RepairResult{}, ArtifactRef{}, false, nil
	}
	refs := existingArtifactRefs(pointer["result_refs"])
	if len(refs) == 0 && stringField(pointer["result_ref"]) != "" {
		refs = []ArtifactRef{{Path: stringField(pointer["result_ref"])}}
	}
	for _, ref := range refs {
		data, err := workpacket.ReadFile(root, ref.Path, repairResultAnswerLimit)
		var result RepairResult
		if err == nil {
			err = json.Unmarshal(data, &result)
		}
		if err != nil {
			return RepairResult{}, ArtifactRef{}, false, err
		}
		if result.ResultID == resultID {
			return result, ref, true, nil
		}
	}
	return RepairResult{}, ArtifactRef{}, false, nil
}

func repairResultPointerRows(pointer map[string]any, next ArtifactRef, root string) ([]any, error) {
	var refs []ArtifactRef
	if raw, exists := pointer["result_refs"]; exists && raw != nil {
		parsed, err := artifactRefsFromAny(raw, "result_refs")
		if err != nil {
			return nil, err
		}
		refs = parsed
	} else if stringField(pointer["result_ref"]) != "" {
		ref, err := pointerArtifact(pointer, "result_ref", "current RepairResult")
		if err != nil {
			return nil, err
		}
		refs = []ArtifactRef{ref}
	}
	rows := make([]any, 0, len(refs)+1)
	seenPaths := map[string]bool{}
	seenAssignments := map[string]bool{}
	for _, ref := range refs {
		if ref.Path == next.Path || seenPaths[ref.Path] {
			return nil, fmt.Errorf("S9 current result index repeats or already contains %s", ref.Path)
		}
		seenPaths[ref.Path] = true
		data, err := workpacket.ReadFile(root, ref.Path, repairResultAnswerLimit)
		var result RepairResult
		if err == nil {
			err = json.Unmarshal(data, &result)
		}
		if err != nil {
			return nil, fmt.Errorf("validate retained S9 Result before appending typed sibling: %w", err)
		}
		if seenAssignments[result.AssignmentID] {
			return nil, fmt.Errorf("S9 current Result index repeats Assignment %s", result.AssignmentID)
		}
		seenAssignments[result.AssignmentID] = true
		rows = append(rows, map[string]any{"path": ref.Path})
	}
	rows = append(rows, map[string]any{"path": next.Path})
	return rows, nil
}

// sameSubmittedAnswer recognizes the same actual answer during reception replay.
func sameSubmittedAnswer(left, right []byte) bool {
	var a, b bytes.Buffer
	if json.Compact(&a, left) != nil || json.Compact(&b, right) != nil {
		return false
	}
	return bytes.Equal(a.Bytes(), b.Bytes())
}
