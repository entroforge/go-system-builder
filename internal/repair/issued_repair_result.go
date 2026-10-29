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
	cleanup    func(RepairResult) error
}

// IssueRepairResultPackets freezes the current, registered S9 PlanReport and
// execution definition into the neutral packet registry. The packet is only
// an input contract: it does not accept a result, deliver a candidate, or
// complete the original Main integration checkpoint.
func IssueRepairResultPackets(ctx context.Context, root, statePath, journalPath string, expectedRevision int, ownerFilter string) ([]workpacket.RepairResultPacket, error) {
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
	mutation := runtimepkg.Mutation{
		EventID: packet.IssuanceEventID, TransitionID: "PACKET-ISSUE", Event: "work_packet_issued",
		Actor: "orchestrator", IdempotencyKey: "packet:" + packet.ID, RuntimeID: packet.Binding.Sources.RuntimeID,
		From: from, To: from, BaselineGeneration: packet.Binding.Sources.BaselineGeneration,
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
	life, _ := state["lifecycle"].(map[string]any)
	pointer := repairPointer(state)
	if pointer == nil || pointer["status"] != "repairing" || life["state"] != "bug_resolution" || life["phase"] != "fixing" {
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
		if reportRef.Path == "" {
			if ownerFilter != "" {
				return nil, fmt.Errorf("S9 Assignment %s is missing its Builder Plan; record that Plan before preparing the Result questionnaire", assignment.AssignmentID)
			}
			continue
		}
		var reportPacket workpacket.RepairPlanReportPacket
		for _, p := range planPackets {
			b := p.Binding
			if p.ReportID == report.ReportID && b.RepairAssignmentID == assignment.AssignmentID &&
				b.OwnerAgentID == owner && b.RuntimeID == workspace.RuntimeID(state) &&
				b.BaselineGeneration == workspace.Generation(state) && b.SessionID == stringField(pointer["session_id"]) && b.PlanRef == planPath {
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
		reworkSHA := ""
		if execution.ReworkRef != "" {
			reworkBytes, err := workpacket.ReadFile(root, execution.ReworkRef, repairResultAnswerLimit)
			if err != nil {
				return nil, fmt.Errorf("read exact S9 rework source: %w", err)
			}
			rework = json.RawMessage(reworkBytes)
			reworkSHA = workpacket.Digest(reworkBytes)
		}
		binding := workpacket.RepairResultBinding{
			Sources: reportPacket.Binding, PlanReportRef: reportRef.Path, PlanReportSHA256: reportRef.SHA256,
			ExecutionSHA256: workpacket.Digest(projection), ReworkRef: execution.ReworkRef, ReworkSHA256: reworkSHA,
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
				b.PlanReportRef != reportRef.Path || b.ReworkRef != execution.ReworkRef || !reflect.DeepEqual(b.Resume, binding.Resume) {
				continue
			}
			var prior workspace.Execution
			if json.Unmarshal(p.Inputs.Execution, &prior) == nil && prior.Generation == execution.Generation &&
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
		inputs := workpacket.RepairResultInputs{
			Sources: reportPacket.Inputs, PlanReport: json.RawMessage(reportBytes), Execution: projection, ReworkReceipt: rework,
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

func repairResultExecutionFromState(ctx context.Context, state map[string]any, binding workpacket.RepairPlanReportBinding, root string) (workspace.Execution, error) {
	raw, err := json.Marshal(state["workspace"])
	if err != nil {
		return workspace.Execution{}, err
	}
	var registry workspace.ExecutionRegistry
	if err := json.Unmarshal(raw, &registry); err != nil {
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
	if retained, err := RetainedNativeS9Source(ctx, root, *selected, binding); err != nil {
		return workspace.Execution{}, err
	} else if retained != nil {
		return *selected, nil
	}
	canonical, err := workspace.Canonical(selected.Path)
	if err != nil || canonical != selected.Path {
		return workspace.Execution{}, fmt.Errorf("S9 Worker checkout %s is unavailable or noncanonical: %v", selected.Path, err)
	}
	checkout, err := workspace.Git(ctx, selected.Path, "rev-parse", "--show-toplevel")
	if err != nil || !sameAbsolutePath(checkout, selected.Path) {
		return workspace.Execution{}, fmt.Errorf("S9 Worker checkout %s is not ready as its own Git worktree: %v", selected.Path, err)
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
	// Both packets must already come from the canonical builder or the Runtime
	// reader, which validates Schema and canonical rebuild before returning.
	// Equality is not issuance, source, current-slot or caller authorization.
	return reflect.DeepEqual(a, b)
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
	retained, err := RetainedNativeS9Source(ctx, root, execution, packet.Binding.Sources)
	if err != nil {
		return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	actualSource := execution.Path
	if retained == nil {
		actualSource, err = workspace.Canonical(execution.Path)
		if err != nil || actualSource != execution.Path {
			return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, errors.New("S9 RepairResult authoring Worker path is not its canonical registered root")
		}
	}
	acceptedOK := execution.DeliveryRef != ""
	var accepted runtimepkg.RepairResultCandidateReceipt
	var acceptedCandidate WorkspaceCandidate
	if acceptedOK {
		data, err := workpacket.ReadFile(root, execution.DeliveryRef, repairResultAnswerLimit)
		if err != nil {
			return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, err
		}
		if err := json.Unmarshal(data, &acceptedCandidate); err != nil {
			return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, err
		}
		if acceptedCandidate.Provenance == nil || acceptedCandidate.Provenance.PacketID != packet.ID || acceptedCandidate.Provenance.OwnerAgentID != req.OwnerAgentID ||
			acceptedCandidate.Provenance.ExecutionGeneration != execution.Generation ||
			acceptedCandidate.Provenance.ExecutionAssignmentID != execution.AssignmentID {
			return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, runtimepkg.ErrSubmissionConflict
		}
		accepted.Provenance = *acceptedCandidate.Provenance
		accepted.CandidateRef, accepted.CandidateSHA256 = execution.DeliveryRef, execution.DeliverySHA256
	}
	if req.SourceRoot == "" || !sameAbsolutePath(req.SourceRoot, actualSource) {
		return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, errors.New("S9 answer source root differs from the registered Worker execution")
	}
	answerBytes, err := ReadRepairResultInput(ctx, root, mainSnapshot.State, packet, execution)
	if err != nil {
		return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	answer, problems := workpacket.ValidateRepairResultAnswer(packet, answerBytes)
	if len(problems) != 0 {
		return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, &RepairResultBeforeCandidateError{Cause: &workpacket.AnswerError{Label: "S9 answer", Fields: problems}}
	}
	answerSHA := workpacket.Digest(answer)
	if acceptedOK {
		if accepted.Provenance.AnswerSHA256 != answerSHA || accepted.Provenance.SubmittedAnswer == nil || string(accepted.Provenance.SubmittedAnswer) != string(answer) || !sameAbsolutePath(accepted.Provenance.ExecutionRoot, actualSource) {
			return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, runtimepkg.ErrSubmissionConflict
		}
		ref := ArtifactRef{Path: accepted.CandidateRef, SHA256: accepted.CandidateSHA256}
		return mainSnapshot, acceptedCandidate, ref, nil
	}
	if err := checkRevision(req.ExpectedRevision, mainSnapshot.Revision, "runtime S9 issued RepairResult answer"); err != nil {
		return mainSnapshot, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	current := mainSnapshot
	provenance, err := newRepairResultCandidateProvenance(ctx, root, current, packet, execution, answer, answerSHA)
	if err != nil {
		return current, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	// Frozen executions retain their original evidence contract. Only new
	// native delivery captures local author sources at this boundary.
	if retained != nil {
		provenance.LocalEvidence = retained.Evidence
	} else if execution.Purpose == workspace.NativeDeliveryPurpose {
		provenance.LocalEvidence, err = captureS9LocalEvidence(registry.MainRoot, actualSource, packet.ID, answer)
		if err != nil {
			return current, WorkspaceCandidate{}, ArtifactRef{}, &RepairResultBeforeCandidateError{Cause: err}
		}
	}
	provenance.PrivateSubmissions, err = captureNativeS9Private(ctx, root, packet, execution, answer)
	if err != nil {
		return current, WorkspaceCandidate{}, ArtifactRef{}, &RepairResultBeforeCandidateError{Cause: err}
	}
	proposal, err := repairResultProposal(packet, answer, execution, registry.MainRoot, provenance)
	if err != nil {
		return current, WorkspaceCandidate{}, ArtifactRef{}, &RepairResultBeforeCandidateError{Cause: err}
	}
	proposal.issued = &issuedRepairResultCapability{packet: packet, provenance: provenance, beforeRevision: current.Revision}
	candidate, ref, err := CreateWorkspaceCandidate(ctx, root, current.State, execution, proposal)
	if err != nil {
		return current, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	key := "runtime:s9:issued-repair-candidate:" + packet.ID
	eventID := "evt-s9-issued-repair-candidate-" + packet.ID
	evidenceIDs := []string{ref.Path, packet.ID, packet.Binding.Sources.RepairAssignmentID}
	at := occurred(req.OccurredAt)
	mutation := runtimepkg.Mutation{
		EventID: eventID, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: req.OwnerAgentID,
		IdempotencyKey: key, RuntimeID: packet.Binding.Sources.RuntimeID, EvidenceIDs: evidenceIDs,
		From: cursor(current.State), To: cursor(current.State), BaselineGeneration: packet.Binding.Sources.BaselineGeneration,
		RetainLastTransition: true, OccurredAt: at, Message: "accepted issued S9 professional result as an immutable workspace candidate; Main integration remains pending",
		Apply: func(state map[string]any) error {
			workspaceRegistry, freshExecution, err := currentRepairExecution(ctx, root, state, packet, true)
			if err != nil {
				return err
			}
			if freshExecution.Generation != execution.Generation || freshExecution.ReworkRef != execution.ReworkRef || !sameAbsolutePath(freshExecution.Path, execution.Path) {
				return errors.New("S9 registered Worker attempt changed before candidate CAS")
			}
			actualAnswer, err := ReadRepairResultInput(ctx, root, state, packet, freshExecution)
			if err != nil {
				return err
			}
			if !bytes.Equal(actualAnswer, answerBytes) {
				return runtimepkg.ErrSubmissionConflict
			}
			if _, err := NativeS9PrivateProjection(ctx, root, packet, freshExecution, candidate, false); err != nil {
				return err
			}
			if len(candidate.Provenance.PrivateSubmissions) > 0 {
				head, err := workspace.Git(ctx, freshExecution.Path, "rev-parse", "HEAD")
				if err != nil || head != candidate.SourceHead {
					return errors.New("S9 source HEAD changed before candidate CAS")
				}
			}
			entry := workspaceRegistry.Executions[freshExecution.AssignmentID]
			if entry.AssignmentID != freshExecution.AssignmentID || entry.DeliveryRef != "" && (entry.DeliveryRef != ref.Path || entry.DeliverySHA256 != ref.SHA256) {
				return errors.New("S9 Worker already has a conflicting delivery candidate")
			}
			if entry.DeliveryRef == ref.Path && entry.DeliverySHA256 == ref.SHA256 {
				return errors.New("identical S9 candidate appeared during receipt; recover its original WORKSPACE event")
			}
			entry.DeliveryRef, entry.DeliverySHA256 = ref.Path, ref.SHA256
			workspaceRegistry.Executions[freshExecution.AssignmentID] = entry
			state["workspace"] = workspace.Encode(workspaceRegistry)
			return nil
		},
	}
	next, err := writer.Update(current.Revision, mutation)
	if err != nil {
		if recovered, recoveredSnapshot, recoveredCandidate, recoveredRef, recoverErr := recoverAcceptedRepairResultCandidate(root, writer, packet, req, answerSHA, actualSource); recoverErr == nil && recovered {
			return recoveredSnapshot, recoveredCandidate, recoveredRef, nil
		}
		return current, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	return next, candidate, ref, nil
}

func ensureRepairResultCompleted(raw json.RawMessage) error {
	var answer workpacket.RepairResultProfessionalAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return err
	}
	if answer.Verdict != "completed" {
		return fmt.Errorf("S9 typed result verdict %q is preserved for named recovery; this slice only accepts completed candidate delivery", answer.Verdict)
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
	if stringField(state["runtime_id"]) != b.RuntimeID || baselineGeneration(state) != b.BaselineGeneration {
		return nil, workspace.Execution{}, errors.New("S9 packet is outside the current Runtime generation")
	}
	raw, err := json.Marshal(state["workspace"])
	if err != nil {
		return nil, workspace.Execution{}, err
	}
	registry := &workspace.ExecutionRegistry{}
	if err := json.Unmarshal(raw, registry); err != nil {
		return nil, workspace.Execution{}, err
	}
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
	var assigned workspace.Execution
	if err := json.Unmarshal(packet.Inputs.Execution, &assigned); err != nil ||
		assigned.AssignmentID != selected.AssignmentID || assigned.RuntimeID != selected.RuntimeID ||
		assigned.AgentID != selected.AgentID || assigned.BaselineGeneration != selected.BaselineGeneration ||
		assigned.Generation != selected.Generation || !sameAbsolutePath(assigned.Path, selected.Path) {
		return nil, workspace.Execution{}, errors.New("S9 result packet belongs to a different Worker attempt")
	}
	return registry, *selected, nil
}

func latestRepairResultToolReceipt(refs []checkexec.ToolReceipt, profile checkexec.Profile) (checkexec.ToolReceipt, bool) {
	for index := len(refs) - 1; index >= 0; index-- {
		if refs[index].Command == profile.Command && refs[index].ProfileDigest == profile.Digest() {
			return refs[index], true
		}
	}
	return checkexec.ToolReceipt{}, false
}

func newRepairResultCandidateProvenance(ctx context.Context, root string, snapshot runtimepkg.Snapshot, packet workpacket.RepairResultPacket, execution workspace.Execution, answer json.RawMessage, answerSHA string) (runtimepkg.RepairResultCandidateProvenance, error) {
	packetSHA, err := workpacket.RepairResultPacketDigest(packet)
	if err != nil {
		return runtimepkg.RepairResultCandidateProvenance{}, err
	}
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
	resumeSHA, err := workpacket.RepairResultResumeSHA256(b.Resume)
	if err != nil {
		return runtimepkg.RepairResultCandidateProvenance{}, fmt.Errorf("digest S9 Resume predecessor tuple: %w", err)
	}
	return runtimepkg.RepairResultCandidateProvenance{
		Contract: repairResultCandidateContract, PacketID: packet.ID, PacketSHA256: packetSHA,
		IssuanceEventID: packet.IssuanceEventID, AnswerSHA256: answerSHA, SubmittedAnswer: append(json.RawMessage(nil), answer...),
		OwnerAgentID: b.Sources.OwnerAgentID, RuntimeID: b.Sources.RuntimeID, BaselineGeneration: b.Sources.BaselineGeneration,
		ReviewRound: b.Sources.ReviewRound, ExecutionAssignmentID: execution.AssignmentID, ExecutionGeneration: execution.Generation,
		ExecutionSHA256: b.ExecutionSHA256, ExecutionRoot: execution.Path, BaseCommit: execution.BaseCommit,
		PlanReportRef: b.PlanReportRef, PlanReportSHA256: b.PlanReportSHA256, ReworkRef: b.ReworkRef, ReworkSHA256: b.ReworkSHA256,
		Resume: b.Resume, ResumeSHA256: resumeSHA,
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
	return RepairResultRequest{
		Contract: ContractRef{Path: packet.Binding.Sources.ContractRef, SHA256: packet.Binding.Sources.ContractSHA256},
		Session:  ArtifactRef{ID: packet.Binding.Sources.SessionID, Path: packet.Binding.Sources.SessionRef, SHA256: packet.Binding.Sources.SessionSHA256},
		Plan:     ArtifactRef{ID: packet.Binding.Sources.PlanID, Path: packet.Binding.Sources.PlanRef, SHA256: packet.Binding.Sources.PlanSHA256},
		ResultID: packet.ResultID, ProducerAgentID: packet.Binding.Sources.OwnerAgentID, UnitResults: units,
		Verdict: answer.Verdict, BeforeFixResults: before, DetectionGapArtifacts: append([]string{}, answer.DetectionGapArtifacts...), EvidenceRefs: append([]string{}, answer.EvidenceRefs...),
		ChangedArtifacts: []ChangedArtifact{}, Result: "pass", AssignmentID: packet.Binding.Sources.RepairAssignmentID,
		PlanReport:      ArtifactRef{Path: packet.Binding.PlanReportRef, SHA256: packet.Binding.PlanReportSHA256},
		BeforeFixChecks: []RepairCheck{}, Checks: checks, ScopeDeviations: append([]string{}, answer.ScopeDeviations...),
		MigrationRef: answer.MigrationRef, RollbackRef: answer.RollbackRef, ResidualRisks: append([]string{}, answer.ResidualRisks...),
	}, nil
}

func validateRepairResultMainThreadlessCandidate(root string, state map[string]any, packet workpacket.RepairResultPacket, execution workspace.Execution, provenance runtimepkg.RepairResultCandidateProvenance, candidate ArtifactRef) error {
	if candidate.Path == "" || candidate.SHA256 == "" || candidate.SHA256 != sha256PathBase(candidate.Path) {
		return errors.New("S9 candidate must use its exact content-addressed original path")
	}
	if provenance.PacketID != packet.ID || provenance.OwnerAgentID != packet.Binding.Sources.OwnerAgentID || provenance.ExecutionRoot != execution.Path || provenance.ExecutionAssignmentID != execution.AssignmentID || provenance.SourceHead == "" {
		return errors.New("S9 candidate typed provenance differs from current original execution")
	}
	return nil
}

func sha256PathBase(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".json")
	return base
}

func workspaceMainRoot(fallback string, state map[string]any) string {
	raw, err := json.Marshal(state["workspace"])
	if err != nil {
		return fallback
	}
	var registry workspace.ExecutionRegistry
	if json.Unmarshal(raw, &registry) == nil && registry.MainRoot != "" {
		return registry.MainRoot
	}
	return fallback
}

// guardIssuedRepairResultCandidate checks the already selected attempt. The
// generic legacy route still directs current typed responsibilities to their answer.
func guardIssuedRepairResultCandidate(root string, state map[string]any, execution workspace.Execution, capability *issuedRepairResultCapability) error {
	if capability != nil {
		p := capability.provenance
		if p.PacketID != capability.packet.ID || p.ExecutionAssignmentID != execution.AssignmentID ||
			p.ExecutionGeneration != execution.Generation || p.RuntimeID != execution.RuntimeID ||
			p.BaselineGeneration != execution.BaselineGeneration || p.OwnerAgentID != execution.AgentID ||
			p.ReworkRef != execution.ReworkRef || !sameAbsolutePath(p.ExecutionRoot, execution.Path) {
			return errors.New("S9 candidate differs from the selected current Worker attempt")
		}
		return nil
	}
	packets, err := runtimepkg.RepairResultPacketsForSourcesContext(context.Background(), root, state, "")
	if err != nil {
		return err
	}
	baseline, _ := state["baseline"].(map[string]any)
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	life, _ := state["lifecycle"].(map[string]any)
	gen := integerRepairResult(baseline["generation"])
	round := integerRepairResult(review["round"])
	matched := []workpacket.RepairResultPacket{}
	for _, packet := range packets {
		b := packet.Binding.Sources
		var definition struct {
			AssignmentID string `json:"assignment_id"`
			RuntimeID    string `json:"runtime_id"`
			Generation   int    `json:"execution_generation"`
			AgentID      string `json:"agent_id"`
			Path         string `json:"worktree_path"`
		}
		if err := json.Unmarshal(packet.Inputs.Execution, &definition); err != nil {
			return err
		}
		if b.OwnerAgentID != execution.AgentID || definition.AssignmentID != execution.AssignmentID || definition.RuntimeID != execution.RuntimeID || definition.AgentID != execution.AgentID || definition.Generation != execution.Generation || !sameAbsolutePath(definition.Path, execution.Path) {
			continue
		}
		if b.RuntimeID != state["runtime_id"] || b.BaselineGeneration != gen || b.ReviewRound != round || pointer["session_id"] != b.SessionID || pointer["plan_ref"] != b.PlanRef || pointer["plan_sha256"] != b.PlanSHA256 || pointer["status"] != "repairing" || life["state"] != "bug_resolution" || life["phase"] != "fixing" {
			continue
		}
		matched = append(matched, packet)
	}
	if len(matched) > 0 {
		return fmt.Errorf("S9 Assignment %s has a current RepairResult answer; save that answer through its candidate receiver", matched[0].Binding.Sources.RepairAssignmentID)
	}
	return nil
}

// rejectLegacyRepairResultForIssuedTarget prevents callers of the original
// untyped Result API from bypassing an issued S9 packet. Once any packet exists
// for the current Session/Plan/Assignment tuple, later attempts remain in the
// typed path even when a fresh packet selector would no longer return them.
func rejectLegacyRepairResultForIssuedTarget(root string, state, pointer map[string]any, plan RepairPlan, planRef ArtifactRef, assignmentID string) error {
	packets, err := runtimepkg.RepairResultPacketsForSourcesContext(context.Background(), root, state, "")
	if err != nil {
		return err
	}
	review, _ := state["review"].(map[string]any)
	for _, packet := range packets {
		if repairResultPacketMatchesLegacyTarget(packet, stringField(state["runtime_id"]), baselineGeneration(state), integerRepairResult(review["round"]), stringField(pointer["session_id"]), plan, planRef, assignmentID) {
			return fmt.Errorf("S9 Assignment %s already has an issued typed RepairResult packet for this Session/Plan; use its exact answer and original candidate receiver", assignmentID)
		}
	}
	return nil
}

// repairResultPacketMatchesLegacyTarget resolves the canonical and registered
// manifest spellings of one Assignment before the original legacy Result
// writer decides whether its untyped path remains available.
func repairResultPacketMatchesLegacyTarget(packet workpacket.RepairResultPacket, runtimeID string, generation, round int, sessionID string, plan RepairPlan, planRef ArtifactRef, assignmentID string) bool {
	binding := packet.Binding.Sources
	assignmentMatches := binding.RepairAssignmentID == assignmentID || binding.ManifestAssignmentID == assignmentID || manifestAssignmentAlias(binding.RepairAssignmentID) == assignmentID
	return assignmentMatches && binding.RuntimeID == runtimeID && binding.BaselineGeneration == generation &&
		binding.ReviewRound == round && binding.SessionID == sessionID && binding.PlanID == plan.PlanID &&
		binding.PlanRef == planRef.Path && binding.PlanSHA256 == planRef.SHA256
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

func recoverAcceptedRepairResultCandidate(root string, writer *runtimepkg.Store, packet workpacket.RepairResultPacket, req AcceptIssuedRepairResultCandidateRequest, answerSHA, sourceRoot string) (bool, runtimepkg.Snapshot, WorkspaceCandidate, ArtifactRef, error) {
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
	candidateRef := ArtifactRef{Path: execution.DeliveryRef, SHA256: execution.DeliverySHA256}
	data, err := workpacket.ReadFile(root, candidateRef.Path, repairResultAnswerLimit)
	if err != nil {
		return false, snapshot, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	var candidate WorkspaceCandidate
	if err := json.Unmarshal(data, &candidate); err != nil {
		return false, snapshot, WorkspaceCandidate{}, ArtifactRef{}, err
	}
	if candidate.Provenance == nil || candidate.Provenance.PacketID != packet.ID ||
		candidate.Provenance.ExecutionGeneration != execution.Generation || candidate.Provenance.AnswerSHA256 != answerSHA {
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
	var assigned struct {
		UnitIDs []string `json:"unit_ids"`
	}
	if err := json.Unmarshal(packet.Inputs.Sources.RepairAssignment, &assigned); err != nil {
		return false, err
	}
	raw, err := json.Marshal(state["workspace"])
	if err != nil {
		return false, err
	}
	var registry workspace.ExecutionRegistry
	if err := json.Unmarshal(raw, &registry); err != nil {
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
		var candidate WorkspaceCandidate
		if err := json.Unmarshal(data, &candidate); err != nil {
			return false, err
		}
		if candidate.Provenance == nil || candidate.Provenance.PacketID != packetID || candidate.Provenance.OwnerAgentID != owner {
			continue
		}
		if candidate.Proposal.Verdict != "completed" || candidate.Proposal.Result != "pass" {
			return false, nil
		}
		if len(assigned.UnitIDs) == 0 {
			return false, nil
		}
		for _, id := range assigned.UnitIDs {
			done := false
			for _, unit := range candidate.Proposal.UnitResults {
				if unit.UnitID == id && unit.Status == "pass" {
					done = true
					break
				}
			}
			if !done {
				return false, nil
			}
		}
		return repairResultAttemptStillCurrent(state, *candidate.Provenance, execution.DeliveryRef, execution.DeliverySHA256), nil
	}
	return false, nil
}

// RepairResultCandidateExecutions locates only the execution frozen in this
// issued packet. Historical receipt reads may retain that slot in History;
// current work-complete projection never treats History as current work.
func RepairResultCandidateExecutions(packet workpacket.RepairResultPacket, registry workspace.ExecutionRegistry, owner string, includeHistory bool) ([]workspace.Execution, error) {
	var frozen workspace.Execution
	if err := json.Unmarshal(packet.Inputs.Execution, &frozen); err != nil {
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
		// Mutable delivery/status fields are excluded by the original frozen
		// projection. A different work definition is not this packet's source.
		raw, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		var row map[string]any
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil, err
		}
		projected, err := runtimepkg.ProjectRepairResultExecution(row)
		if err != nil {
			return nil, err
		}
		if workpacket.Digest(projected) != packet.Binding.ExecutionSHA256 {
			continue
		}
		selected = append(selected, e)
	}
	return selected, nil
}

func repairResultAttemptStillCurrent(state map[string]any, provenance runtimepkg.RepairResultCandidateProvenance, candidateRef, candidateSHA string) bool {
	if stringField(state["runtime_id"]) != provenance.RuntimeID || baselineGeneration(state) != provenance.BaselineGeneration {
		return false
	}
	raw, err := json.Marshal(state["workspace"])
	if err != nil {
		return false
	}
	var registry workspace.ExecutionRegistry
	if json.Unmarshal(raw, &registry) != nil {
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
		selected.DeliveryRef != candidateRef || selected.DeliverySHA256 != candidateSHA || !sameAbsolutePath(selected.Path, provenance.ExecutionRoot) {
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
	current, writer, err := readIssuedRepairResultRuntime(ctx, root, statePath, journalPath)
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
		if len(req.MainProof.VerifiedFacts) != 0 {
			facts, err := canonicalRuntimeFacts(req.MainProof.VerifiedFacts)
			if err != nil || string(facts) != string(result.Receipt.MainProof.VerifiedFacts) {
				return current, RepairResult{}, ArtifactRef{}, runtimepkg.ErrSubmissionConflict
			}
		}
		return current, result, ref, nil
	}
	if err := checkRevision(req.ExpectedRevision, current.Revision, "runtime S9 Result"); err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
	}
	_, execution, err := currentRepairExecution(ctx, root, current.State, packet, true)
	if err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
	}
	if execution.DeliveryRef == "" {
		return current, RepairResult{}, ArtifactRef{}, errors.New("S9 Worker candidate remains pending")
	}
	candidateRef := ArtifactRef{Path: execution.DeliveryRef, SHA256: execution.DeliverySHA256}
	candidate, err := ValidateWorkspaceCandidate(root, current.State, execution, candidateRef)
	if err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
	}
	if candidate.Provenance == nil || candidate.Provenance.PacketID != packet.ID {
		return current, RepairResult{}, ArtifactRef{}, errors.New("S9 candidate belongs to a different Result questionnaire")
	}
	provenance := *candidate.Provenance
	if err := verifyS9CapturedEvidence(root, provenance); err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
	}
	if err := ensureRepairResultCompleted(provenance.SubmittedAnswer); err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
	}
	var assigned struct {
		UnitIDs []string `json:"unit_ids"`
	}
	if err := json.Unmarshal(packet.Inputs.Sources.RepairAssignment, &assigned); err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
	}
	for _, id := range assigned.UnitIDs {
		done := false
		for _, unit := range candidate.Proposal.UnitResults {
			if unit.UnitID == id && unit.Status == "pass" {
				done = true
				break
			}
		}
		if !done {
			return current, RepairResult{}, ArtifactRef{}, fmt.Errorf("S9 repair unit %s still needs its actual completed result", id)
		}
	}
	checks, err := repairResultMainChecks(root, packet, provenance)
	if err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
	}
	checkpoint, proof, err := readRepairResultMainProof(root, packet, provenance, checks)
	if err != nil {
		return current, RepairResult{}, ArtifactRef{}, err
	}
	if head, err := workspace.Git(ctx, root, "rev-parse", "HEAD"); err != nil || head != checkpoint.TestedHead {
		return current, RepairResult{}, ArtifactRef{}, errors.New("Main HEAD differs from the actual checked commit; necessary checks remain pending")
	}
	journal, _ := current.State["journal"].(map[string]any)
	receipt := runtimepkg.RepairResultReceipt{
		Contract: repairResultFinalContract, Provenance: provenance,
		CandidateRef: candidateRef.Path, CandidateSHA256: candidateRef.SHA256,
		ResultID: packet.ResultID, ResultRef: artifactRoot + "/results/" + packet.ResultID + ".json", MainProof: proof, WorkComplete: true,
		AcceptedEventID:              "evt-s9-issued-repair-result-" + packet.ID,
		AcceptedIdempotencyKey:       "runtime:s9:issued-repair-result:" + packet.ID,
		AcceptedAfterRuntimeRevision: current.Revision + 1, AcceptedSequence: integerRepairResult(journal["last_sequence"]) + 1,
	}
	capability := &issuedRepairResultFinalCapability{packet: packet, provenance: provenance, receipt: receipt, ctx: ctx, mainRoot: root}
	proposal := candidate.Proposal
	proposal.Checks, proposal.ResultID = checks, packet.ResultID
	proposal.ProducerAgentID, proposal.AssignmentID = execution.AgentID, packet.Binding.Sources.RepairAssignmentID
	proposal.PlanReport = ArtifactRef{Path: packet.Binding.PlanReportRef, SHA256: packet.Binding.PlanReportSHA256}
	proposal.Verdict, proposal.Result = "completed", "pass"
	proposal.Receipt, proposal.issuedFinal = &receipt, capability
	proposal.OccurredAt = occurred(req.OccurredAt)
	capability.cleanup = func(expected RepairResult) error {
		return cleanUnacceptedRepairResultStaging(root, writer, current, expected)
	}
	return SubmitRepairResultToRuntime(root, statePath, journalPath, SubmitResultRuntimeRequest{
		RuntimeRequest: RuntimeRequest{Context: ctx, ExpectedRevision: current.Revision, Actor: execution.AgentID, OccurredAt: proposal.OccurredAt},
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
		baselineGeneration != packet.Binding.Sources.BaselineGeneration || after != before+1 || after > currentRevision ||
		from["state"] != "bug_resolution" || from["phase"] != "fixing" || !reflect.DeepEqual(from, to) {
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
	var checkpoint integration.Checkpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
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
			Before  string `json:"head_before"`
			After   string `json:"head_after"`
			Status  string `json:"status"`
			Exit    int    `json:"exit_code"`
		}
		if err := json.Unmarshal(data, &executed); err != nil {
			return nil, err
		}
		// Target advancement can legitimately require another Main check
		// before Final reception. Older tested commits remain history rather
		// than becoming unfinished checks for this newly verified commit.
		if checkpoint.TestedHead != "" && (executed.Before != checkpoint.TestedHead || executed.After != checkpoint.TestedHead) {
			continue
		}
		if executed.Status != "pass" || executed.Exit != 0 || executed.Before != checkpoint.TestedHead || executed.After != checkpoint.TestedHead || checkpoint.TestedHead == "" {
			return nil, errors.New("S9 Main checks remain pending: a necessary check did not pass at the tested commit")
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
		check := RepairCheck{Name: "verified Main integration", Command: executed.Command, Result: "pass", EvidenceRefs: []string{"path:" + filepath.ToSlash(relative) + "#sha256=" + workpacket.Digest(data)}}
		if index, exists := byCommand[executed.Command]; exists {
			out[index] = check
		} else {
			byCommand[executed.Command] = len(out)
			out = append(out, check)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("S9 Main checks remain pending: no successful actual check at tested HEAD")
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
	var checkpoint integration.Checkpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
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
		CheckpointRef: relative, CheckpointSHA256: workpacket.Digest(data),
		VerifiedFacts: facts, VerifiedFactsSHA256: workpacket.Digest(facts),
	}
	if checkpoint.AssignmentID != provenance.ExecutionAssignmentID || checkpoint.BaselineGeneration != provenance.BaselineGeneration ||
		checkpoint.SourceHead != provenance.SourceHead || checkpoint.TestedHead == "" ||
		!sameAbsolutePath(checkpoint.WorktreePath, provenance.ExecutionRoot) {
		return zero, runtimepkg.RepairResultMainProof{}, errors.New("S9 Main integration has not checked this candidate's actual source")
	}
	if len(checks) == 0 || !allChecksPass(checks) {
		return zero, runtimepkg.RepairResultMainProof{}, errors.New("S9 necessary Main checks remain pending")
	}
	return checkpoint, proof, nil
}

func canonicalRuntimeFacts(facts json.RawMessage) (json.RawMessage, error) {
	if err := workpacket.ValidateUniqueJSON(facts); err != nil {
		return nil, err
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(facts)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func existingIssuedRepairResult(root string, state map[string]any, resultID string) (RepairResult, ArtifactRef, bool, error) {
	pointer := repairPointer(state)
	if pointer == nil {
		return RepairResult{}, ArtifactRef{}, false, nil
	}
	refs := existingArtifactRefs(pointer["result_refs"])
	if len(refs) == 0 && stringField(pointer["result_ref"]) != "" {
		refs = []ArtifactRef{{Path: stringField(pointer["result_ref"]), SHA256: stringField(pointer["result_sha256"])}}
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
		ref, err := pointerArtifact(pointer, "result_ref", "result_sha256", "current RepairResult")
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
		rows = append(rows, map[string]any{"path": ref.Path, "sha256": ref.SHA256})
	}
	rows = append(rows, map[string]any{"path": next.Path, "sha256": next.SHA256})
	return rows, nil
}

// cleanUnacceptedRepairResultStaging removes only the exact immutable Result
// bytes produced for this unaccepted typed attempt. A pending Runtime write,
// current pointer reference, revision change, changed bytes, or any content
// difference outside the two predicted acceptance coordinates keeps the file
// in place for recovery rather than risking deletion of accepted evidence.
func cleanUnacceptedRepairResultStaging(root string, writer *runtimepkg.Store, snapshot runtimepkg.Snapshot, expected RepairResult) error {
	if writer == nil || expected.Receipt == nil || expected.ResultID == "" || expected.Receipt.ResultRef == "" {
		return errors.New("S9 unaccepted Result cleanup requires the exact staged typed Result")
	}
	path := expected.Receipt.ResultRef
	data, err := workpacket.ReadFile(root, path, repairResultAnswerLimit)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("preserve unrecognized S9 Result staging path %s: %w", path, err)
	}
	sha := workpacket.Digest(data)
	staged, err := ValidateRepairResult(root, ArtifactRef{Path: path, SHA256: sha})
	if err != nil || staged.Receipt == nil || staged.ResultID != expected.ResultID || staged.AssignmentID != expected.AssignmentID ||
		staged.SessionID != expected.SessionID || staged.PlanID != expected.PlanID || staged.ProducerAgentID != expected.ProducerAgentID ||
		staged.Receipt.CandidateRef != expected.Receipt.CandidateRef || staged.Receipt.CandidateSHA256 != expected.Receipt.CandidateSHA256 ||
		staged.Receipt.ResultRef != path || staged.Receipt.ResultID != expected.ResultID ||
		staged.Receipt.AcceptedEventID != expected.Receipt.AcceptedEventID || staged.Receipt.AcceptedIdempotencyKey != expected.Receipt.AcceptedIdempotencyKey ||
		staged.Receipt.WorkComplete != expected.Receipt.WorkComplete {
		return fmt.Errorf("preserve existing S9 Result staging path %s: it is not the exact unaccepted packet/candidate artifact (%v)", path, err)
	}
	// A retry can advance only after another sibling Result was accepted. The
	// stable packet issue time keeps SubmittedAt fixed; only predicted Runtime
	// revision and journal sequence may be refreshed before this Result's CAS.
	staged.Receipt.AcceptedAfterRuntimeRevision = expected.Receipt.AcceptedAfterRuntimeRevision
	staged.Receipt.AcceptedSequence = expected.Receipt.AcceptedSequence
	if !reflect.DeepEqual(staged, expected) {
		return fmt.Errorf("preserve existing S9 Result staging path %s: staged content differs from this exact unaccepted retry", path)
	}
	for _, ref := range existingArtifactRefs(repairPointer(snapshot.State)["result_refs"]) {
		if ref.Path == path {
			return fmt.Errorf("preserve S9 Result staging artifact %s because the current Runtime pointer already references it", path)
		}
	}
	removed, err := writer.RemoveUnreferencedArtifact(runtimepkg.ArtifactCleanupRequest{
		ExpectedRevision: snapshot.Revision, ArtifactPath: path, ArtifactSHA256: sha,
		ReferencedPaths: stateArtifactPaths(snapshot.State),
	})
	if err != nil {
		return fmt.Errorf("clean exact unaccepted S9 Result staging artifact %s: %w", path, err)
	}
	if !removed {
		return fmt.Errorf("preserve S9 Result staging artifact %s: pending Runtime state or a current reference prevents safe cleanup; inspect recovery before retry", path)
	}
	return nil
}
