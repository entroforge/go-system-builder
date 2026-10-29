package repair

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const repairPlanReportAnswerLimit int64 = 20 << 20

const repairPlanReportReceiptContract = "s9-plan-report-receipt-v1"

type SubmitIssuedRepairPlanReportRequest struct {
	Context          context.Context `json:"-"`
	ExpectedRevision int
	PacketID         string
	AnswerPath       string
	OwnerAgentID     string
	SourceRoot       string
	AuthorizeSource  func(root string, state map[string]any) error
	OccurredAt       time.Time
}

// IssuedPlanReportRequest is a concise alias for Hook/service callers.
type IssuedPlanReportRequest = SubmitIssuedRepairPlanReportRequest

type issuedPlanReportCapability struct {
	packet    workpacket.RepairPlanReportPacket
	receipt   runtimepkg.RepairPlanReportReceipt
	answer    json.RawMessage
	answerSHA string
	source    string
}

func validateIssuedPlanReportCapability(root string, state map[string]any, req SubmitPlanReportRequest, report PlanReport, _ bool) error {
	capability := req.issued
	if capability == nil {
		return errors.New("missing private S9 issued PlanReport capability")
	}
	packet, found, err := runtimepkg.FindRepairPlanReportPacketContext(runtimepkg.WithPacketBodyRoot(req.Context, root), state, capability.packet.ID)
	if err != nil || !found {
		return fmt.Errorf("S9 current Plan packet is unavailable: %v", err)
	}
	b := packet.Binding
	pointer := repairPointer(state)
	baseline, _ := state["baseline"].(map[string]any)
	generation, _ := integerForRepairReport(baseline["generation"])
	if state["runtime_id"] != b.RuntimeID || generation != b.BaselineGeneration ||
		pointer["session_id"] != b.SessionID || pointer["plan_ref"] != b.PlanRef ||
		stringMapField(pointer["assignment_owners"])[b.RepairAssignmentID] != b.OwnerAgentID ||
		req.Actor != b.OwnerAgentID || report.AgentID != b.OwnerAgentID || report.AssignmentID != b.RepairAssignmentID ||
		report.PlanID != b.PlanID || report.SessionID != b.SessionID || report.ReportID != packet.ReportID {
		return errors.New("S9 Plan report does not correspond to the current responsibility")
	}
	return nil
}

func bytesEqualRepairPlanReportAnswer(left, right []byte) bool {
	var a, b bytes.Buffer
	if json.Compact(&a, left) != nil || json.Compact(&b, right) != nil {
		return false
	}
	return bytes.Equal(a.Bytes(), b.Bytes())
}

// IssueRepairPlanReportPackets is the S9 adapter to the original runtime
// PACKET-ISSUE writer. It only issues packets for registered, current Builder
// Assignments; the packet is an input contract, not authority to write or to
// begin execution.
func IssueRepairPlanReportPackets(root, statePath, journalPath string, expectedRevision int, ownerFilter string) ([]workpacket.RepairPlanReportPacket, error) {
	return IssueRepairPlanReportPacketsContext(context.Background(), root, statePath, journalPath, expectedRevision, ownerFilter)
}

func IssueRepairPlanReportPacketsContext(ctx context.Context, root, statePath, journalPath string, expectedRevision int, ownerFilter string) ([]workpacket.RepairPlanReportPacket, error) {
	current, writer, err := readIssuedRepairPlanReportRuntime(root, statePath, journalPath, ctx)
	if err != nil {
		return nil, err
	}
	ctx = writer.Context()
	candidates, err := buildRepairPlanReportPackets(root, current.State, ownerFilter)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		if err := checkRevision(expectedRevision, current.Revision, "runtime S9 PlanReport packet issue"); err != nil {
			return nil, err
		}
		return []workpacket.RepairPlanReportPacket{}, nil
	}
	issued := make([]workpacket.RepairPlanReportPacket, 0, len(candidates))
	for _, packet := range candidates {
		if prior, found, err := runtimepkg.FindRepairPlanReportPacketContext(runtimepkg.WithPacketBodyRoot(ctx, root), current.State, packet.ID); err != nil {
			return nil, err
		} else if found {
			if !sameRepairPlanReportPacket(prior, packet) {
				return nil, fmt.Errorf("S9 packet identity %s is already registered with different frozen sources", packet.ID)
			}
			issued = append(issued, prior)
			continue
		}
		// A template update changes the derived packet ID, but cannot create a
		// second question for the same issued responsibility. Find its original
		// current owner/assignment and verify the frozen sources before reusing it.
		retained, err := runtimepkg.CurrentRepairPlanReportPacketsContext(ctx, root, current.State, packet.Binding.OwnerAgentID)
		if err != nil {
			return nil, err
		}
		var original *workpacket.RepairPlanReportPacket
		for i := range retained {
			prior := &retained[i]
			if prior.Binding.RepairAssignmentID != packet.Binding.RepairAssignmentID || prior.Binding.ReviewRound != packet.Binding.ReviewRound {
				continue
			}
			if original != nil {
				return nil, fmt.Errorf("S9 Assignment %s has multiple issued PlanReport questionnaires", packet.Binding.RepairAssignmentID)
			}
			if _, err := workpacket.RepairPlanReportPacketDigest(*prior); err != nil {
				return nil, fmt.Errorf("S9 original PlanReport packet %s: %w", prior.ID, err)
			}
			if !reflect.DeepEqual(prior.Binding, packet.Binding) || !reflect.DeepEqual(prior.Inputs, packet.Inputs) {
				return nil, fmt.Errorf("S9 original PlanReport packet %s differs from current frozen sources", prior.ID)
			}
			if err := runtimepkg.VerifyRepairPlanReportPacketIssueContext(ctx, root, current.State, *prior); err != nil {
				return nil, fmt.Errorf("S9 original PlanReport packet %s issuance: %w", prior.ID, err)
			}
			original = prior
		}
		if original != nil {
			issued = append(issued, *original)
			continue
		}
		// An exact prior PACKET-ISSUE is replayed above before enforcing the
		// caller's original CAS. This lets a mutation-capable Snapshot recover
		// its pending write and then return the exact committed packet without
		// treating that recovery as a conflicting fresh issuance.
		if err := checkRevision(expectedRevision, current.Revision, "runtime S9 PlanReport packet issue"); err != nil {
			return nil, err
		}
		if err := issueOneRepairPlanReportPacket(root, writer, expectedRevision, current, packet); err != nil {
			return nil, err
		}
		latest, err := writer.Snapshot()
		if err != nil {
			return nil, err
		}
		stored, found, err := runtimepkg.FindRepairPlanReportPacketContext(runtimepkg.WithPacketBodyRoot(ctx, root), latest.State, packet.ID)
		if err != nil || !found || !sameRepairPlanReportPacket(stored, packet) {
			return nil, fmt.Errorf("S9 packet %s was not retained exactly by the original Runtime writer: %v", packet.ID, err)
		}
		issued = append(issued, stored)
		current = latest
		expectedRevision = latest.Revision
	}
	return issued, nil
}

func issueOneRepairPlanReportPacket(root string, writer *runtimepkg.Store, expected int, current runtimepkg.Snapshot, packet workpacket.RepairPlanReportPacket) error {
	rowBytes, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	var packetRow map[string]any
	if err := json.Unmarshal(rowBytes, &packetRow); err != nil {
		return err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-plan-report-work-packet.schema.json", rowBytes); err != nil {
		return err
	}
	cursor := cursor(current.State)
	mutation := runtimepkg.Mutation{
		EventID: packet.IssuanceEventID, TransitionID: "PACKET-ISSUE", Event: "work_packet_issued",
		Actor: "orchestrator", IdempotencyKey: "packet:" + packet.ID, RuntimeID: packet.Binding.RuntimeID,
		From: cursor, To: cursor, BaselineGeneration: packet.Binding.BaselineGeneration,
		OccurredAt: time.Now().UTC(), RetainLastTransition: true,
		Apply: func(state map[string]any) error {
			review, _ := state["review"].(map[string]any)
			pointer, _ := review["repair"].(map[string]any)
			baseline, _ := state["baseline"].(map[string]any)
			generation, _ := integerForRepairReport(baseline["generation"])
			b := packet.Binding
			entities, _ := state["entities"].(map[string]any)
			if state["runtime_id"] != b.RuntimeID || generation != b.BaselineGeneration ||
				pointer["session_id"] != b.SessionID || pointer["plan_ref"] != b.PlanRef || pointer["plan_sha256"] != b.PlanSHA256 ||
				stringMapField(pointer["assignment_owners"])[b.RepairAssignmentID] != b.OwnerAgentID {
				return errors.New("S9 current repair responsibility changed before packet publication")
			}
			if existing, found, err := runtimepkg.FindRepairPlanReportPacketContext(runtimepkg.WithPacketBodyRoot(writer.Context(), root), state, packet.ID); err != nil {
				return err
			} else if found {
				if sameRepairPlanReportPacket(existing, packet) {
					return errors.New("identical S9 packet appeared during issuance; recover from the original PACKET-ISSUE event")
				}
				return fmt.Errorf("S9 packet ID %s conflicts with an existing Runtime packet", packet.ID)
			}
			all, err := runtimepkg.RepairPlanReportPacketsForSourcesContext(writer.Context(), root, state, "")
			if err != nil {
				return err
			}
			for _, other := range all {
				b := other.Binding
				if b.RuntimeID == packet.Binding.RuntimeID && b.BaselineGeneration == packet.Binding.BaselineGeneration &&
					b.ReviewRound == packet.Binding.ReviewRound && b.SessionID == packet.Binding.SessionID &&
					b.RepairAssignmentID == packet.Binding.RepairAssignmentID {
					return fmt.Errorf("RepairAssignment %s already has issued S9 packet %s; inspect its receipt or use named recovery", b.RepairAssignmentID, other.ID)
				}
			}
			if entities == nil {
				return errors.New("S9 packet issue requires Runtime entities")
			}
			rows, ok := entities["work_packets"].([]any)
			if !ok || rows == nil {
				rows = []any{}
			}
			entities["work_packets"] = append(rows, packetRow)
			return nil
		},
	}
	if expected < 0 {
		_, err = writer.UpdateCurrent(mutation)
	} else {
		_, err = writer.Update(expected, mutation)
	}
	if err == nil {
		return nil
	}
	latest, readErr := writer.Snapshot()
	if readErr == nil {
		stored, found, findErr := runtimepkg.FindRepairPlanReportPacketContext(runtimepkg.WithPacketBodyRoot(writer.Context(), root), latest.State, packet.ID)
		if findErr == nil && found && sameRepairPlanReportPacket(stored, packet) {
			return nil
		}
	}
	return fmt.Errorf("issue S9 PlanReport packet %s: %w", packet.ID, err)
}

func buildRepairPlanReportPackets(root string, state map[string]any, ownerFilter string) ([]workpacket.RepairPlanReportPacket, error) {
	if !runtimepkg.PacketEnabled(state) {
		return nil, errors.New("S9 typed PlanReport packets require the explicit v1.2 packet contract")
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	casePointer, _ := review["investigation"].(map[string]any)
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if pointer == nil || (pointer["status"] != "planning" && pointer["status"] != "reproducing" && pointer["status"] != "repairing") || lifecycle["state"] != "bug_resolution" ||
		(lifecycle["phase"] != "planning" && lifecycle["phase"] != "reproducing" && lifecycle["phase"] != "fixing") {
		return []workpacket.RepairPlanReportPacket{}, nil
	}
	base, _ := state["baseline"].(map[string]any)
	generation, ok := integerForRepairReport(base["generation"])
	if !ok || generation < 0 {
		return nil, errors.New("S9 PlanReport packet requires a valid current baseline generation")
	}
	round, ok := integerForRepairReport(review["round"])
	if !ok || round < 1 {
		return nil, errors.New("S9 PlanReport packet requires a valid current Review round")
	}
	runtimeID := stringField(state["runtime_id"])
	caseID, caseRef, caseSHA := stringField(casePointer["case_id"]), stringField(casePointer["path"]), stringField(casePointer["sha256"])
	contractRef, contractSHA := stringField(pointer["contract_ref"]), stringField(pointer["contract_sha256"])
	sessionID, sessionRef, sessionSHA := stringField(pointer["session_id"]), stringField(pointer["path"]), stringField(pointer["sha256"])
	planRef, planSHA := stringField(pointer["plan_ref"]), stringField(pointer["plan_sha256"])
	if runtimeID == "" || caseID == "" || caseRef == "" || caseSHA == "" || contractRef == "" || contractSHA == "" || sessionID == "" || sessionRef == "" || sessionSHA == "" || planRef == "" || planSHA == "" {
		return nil, errors.New("S9 PlanReport packet requires exact current Case, Contract, Session, and Plan references")
	}
	caseBytes, err := workpacket.ReadFile(root, caseRef, 20<<20)
	if err != nil || workpacket.Digest(caseBytes) != caseSHA {
		return nil, fmt.Errorf("S9 current Case bytes/SHA differ: %v", err)
	}
	contractBytes, err := readArtifact(root, ArtifactRef{Path: contractRef, SHA256: contractSHA}, "repair-contract.schema.json")
	if err != nil {
		return nil, err
	}
	sessionBytes, err := readArtifact(root, ArtifactRef{Path: sessionRef, SHA256: sessionSHA}, "repair-session.schema.json")
	if err != nil {
		return nil, err
	}
	planBytes, err := readArtifact(root, ArtifactRef{Path: planRef, SHA256: planSHA}, "repair-plan.schema.json")
	if err != nil {
		return nil, err
	}
	if err := workpacket.ValidateUniqueJSON(caseBytes); err != nil {
		return nil, fmt.Errorf("S9 Case source: %w", err)
	}
	var caseDoc map[string]any
	if err := json.Unmarshal(caseBytes, &caseDoc); err != nil || caseDoc["case_id"] != caseID || caseDoc["status"] != "contract_approved" {
		return nil, errors.New("S9 current Case identity/status differs from approved Contract pointer")
	}
	contractRefSHA := ArtifactRef{Path: contractRef, SHA256: contractSHA}
	contract, err := ValidateApprovedContractRef(root, ContractRef{Path: contractRefSHA.Path, SHA256: contractRefSHA.SHA256})
	if err != nil {
		return nil, err
	}
	session, err := ValidateRepairSession(root, ArtifactRef{ID: sessionID, Path: sessionRef, SHA256: sessionSHA})
	if err != nil {
		return nil, err
	}
	plan, err := ValidateRepairPlan(root, ArtifactRef{ID: stringField(pointer["plan_id"]), Path: planRef, SHA256: planSHA})
	if err != nil {
		return nil, err
	}
	if contract.CaseID != caseID || contract.ContractID != stringField(pointer["contract_id"]) || session.SessionID != sessionID ||
		session.RuntimeID != runtimeID || session.BaselineGeneration != generation || session.ContractRef != contractRef || session.ContractSHA256 != contractSHA ||
		plan.SessionID != sessionID || plan.ContractRef != contractRef || plan.ContractSHA256 != contractSHA {
		return nil, errors.New("S9 Case/Contract/Session/Plan source chain differs")
	}
	if err := workpacket.ValidateUniqueJSON(planBytes); err != nil {
		return nil, err
	}
	var planDoc map[string]any
	if err := json.Unmarshal(planBytes, &planDoc); err != nil || planDoc == nil {
		return nil, errors.New("S9 RepairPlan source must be an object")
	}
	entities, _ := state["entities"].(map[string]any)
	owners := stringMapField(pointer["assignment_owners"])
	packets := make([]workpacket.RepairPlanReportPacket, 0)
	for _, assignment := range plan.Assignments {
		owner := owners[assignment.AssignmentID]
		if owner == "" || (ownerFilter != "" && owner != ownerFilter) {
			continue
		}
		if assignment.OwnerAgentID != "" && assignment.OwnerAgentID != owner {
			return nil, fmt.Errorf("S9 RepairAssignment %s registered owner differs from its frozen Plan", assignment.AssignmentID)
		}
		agent, count := uniqueRepairEntity(entities["agents"], owner)
		if count != 1 || agent["role"] != "backend-builder" && agent["role"] != "frontend-builder" && agent["role"] != "test-builder" {
			return nil, fmt.Errorf("S9 RepairAssignment %s requires one registered Builder owner", assignment.AssignmentID)
		}
		teamID := stringField(agent["team_id"])
		team, teamCount := uniqueRepairEntity(entities["teams"], teamID)
		if teamID == "" || teamCount != 1 || team["kind"] != "builder" {
			return nil, fmt.Errorf("S9 RepairAssignment %s requires one registered Builder Team", assignment.AssignmentID)
		}
		manifestRef, manifestSHA := stringField(team["manifest_ref"]), stringField(team["manifest_sha256"])
		manifestBytes, err := workpacket.ReadFile(root, manifestRef, 20<<20)
		if err != nil || workpacket.Digest(manifestBytes) != manifestSHA {
			return nil, fmt.Errorf("S9 registered Builder manifest bytes/SHA differ: %v", err)
		}
		if err := workpacket.ValidateUniqueJSON(manifestBytes); err != nil {
			return nil, err
		}
		var manifest map[string]any
		if err := json.Unmarshal(manifestBytes, &manifest); err != nil || manifest == nil {
			return nil, errors.New("S9 Builder manifest source must be an object")
		}
		prompt := stringField(agent["prompt_ref"])
		manifestAssignmentID := ""
		if prefix := manifestRef + "#"; strings.HasPrefix(prompt, prefix) {
			manifestAssignmentID = strings.TrimPrefix(prompt, prefix)
		}
		if manifestAssignmentID == "" {
			return nil, fmt.Errorf("S9 Builder Agent %s has no exact registered manifest Assignment", owner)
		}
		manifestAssignment, manifestAssignmentCount := uniqueRepairEntityByField(manifest["assignments"], "assignment_id", manifestAssignmentID)
		if manifestAssignmentCount != 1 || manifestAssignment["agent_id"] != owner {
			return nil, fmt.Errorf("S9 Builder manifest Assignment %s does not bind owner %s", manifestAssignmentID, owner)
		}
		manifestAssignmentBytes, err := json.Marshal(manifestAssignment)
		if err != nil {
			return nil, err
		}
		var taskRow map[string]any
		var taskBytes []byte
		taskCount := 0
		taskIDs, ok := stringSliceForRepairReport(agent["task_ids"])
		if !ok {
			return nil, fmt.Errorf("S9 Builder Agent %s task_ids is invalid", owner)
		}
		for _, taskID := range taskIDs {
			task, n := uniqueRepairEntity(entities["tasks"], taskID)
			if n != 1 {
				return nil, fmt.Errorf("S9 Builder Agent %s task %s does not resolve to one registered Runtime TASK", owner, taskID)
			}
			taskOwners, ok := stringSliceForRepairReport(task["owner_agent_ids"])
			if !ok || !containsStringForRepairReport(taskOwners, owner) {
				return nil, fmt.Errorf("S9 Runtime TASK %s is not registered to Builder %s", taskID, owner)
			}
			taskRef, taskSHA := stringField(task["path"]), stringField(task["sha256"])
			registeredTaskBytes, readErr := workpacket.ReadFile(root, taskRef, 20<<20)
			if readErr != nil || taskSHA == "" || workpacket.Digest(registeredTaskBytes) != taskSHA {
				return nil, fmt.Errorf("S9 registered TASK %s bytes/SHA differ: %v", taskID, readErr)
			}
			if err := workpacket.ValidateUniqueJSON(registeredTaskBytes); err != nil {
				return nil, fmt.Errorf("S9 registered TASK %s is invalid JSON: %w", taskID, err)
			}
			var taskDocument map[string]any
			if err := json.Unmarshal(registeredTaskBytes, &taskDocument); err != nil || taskDocument == nil {
				return nil, fmt.Errorf("S9 registered TASK %s must be an object: %v", taskID, err)
			}
			if taskDocument["task_id"] == taskID && taskDocument["session_id"] == sessionID && taskDocument["plan_id"] == plan.PlanID && taskDocument["assignment_id"] == assignment.AssignmentID {
				taskRow = task
				taskBytes = registeredTaskBytes
				taskCount++
			}
		}
		if taskCount != 1 {
			return nil, fmt.Errorf("S9 Builder %s must have exactly one registered TASK for RepairAssignment %s", owner, assignment.AssignmentID)
		}
		taskRef, taskSHA := stringField(taskRow["path"]), stringField(taskRow["sha256"])
		assignmentRaw, err := exactPlanAssignmentRaw(planDoc, assignment.AssignmentID)
		if err != nil {
			return nil, err
		}
		inputs := workpacket.RepairPlanReportInputs{
			Case: json.RawMessage(caseBytes), Contract: json.RawMessage(contractBytes), Session: json.RawMessage(sessionBytes), Plan: json.RawMessage(planBytes),
			Task: json.RawMessage(taskBytes), RepairAssignment: assignmentRaw, ManifestAssignment: json.RawMessage(manifestAssignmentBytes),
			ProspectiveScope: append([]string{}, plan.ProspectiveScope...), ForbiddenScope: append([]string{}, plan.ForbiddenScope...),
		}
		binding := workpacket.RepairPlanReportBinding{
			RuntimeID: runtimeID, BaselineGeneration: generation, ReviewRound: round,
			CaseID: caseID, CaseRef: caseRef, CaseSHA256: caseSHA,
			ContractID: contract.ContractID, ContractRef: contractRef, ContractSHA256: contractSHA,
			SessionID: sessionID, SessionRef: sessionRef, SessionSHA256: sessionSHA,
			PlanID: plan.PlanID, PlanRef: planRef, PlanSHA256: planSHA,
			RepairAssignmentID: assignment.AssignmentID, RepairAssignmentSHA256: workpacket.Digest(assignmentRaw),
			ManifestAssignmentID: manifestAssignmentID, ManifestAssignmentSHA256: workpacket.Digest(manifestAssignmentBytes),
			TeamID: teamID, TaskID: stringField(taskRow["id"]), TaskRef: taskRef, TaskSHA256: taskSHA,
			ManifestRef: manifestRef, ManifestSHA256: manifestSHA, OwnerAgentID: owner,
		}
		packet, err := workpacket.BuildRepairPlanReport(binding, inputs)
		if err != nil {
			return nil, fmt.Errorf("build S9 PlanReport packet for %s: %w", assignment.AssignmentID, err)
		}
		if _, err := repairPlanReportSourceRoot(root, state, packet); err != nil {
			return nil, err
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func exactPlanAssignmentRaw(plan map[string]any, assignmentID string) (json.RawMessage, error) {
	rows, ok := plan["assignments"].([]any)
	if !ok {
		return nil, errors.New("S9 RepairPlan assignments must be an array")
	}
	var found json.RawMessage
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok || row["assignment_id"] != assignmentID {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("S9 RepairPlan duplicates Assignment %s", assignmentID)
		}
		data, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		found = data
	}
	if found == nil {
		return nil, fmt.Errorf("S9 RepairPlan lacks Assignment %s", assignmentID)
	}
	return found, nil
}

func uniqueRepairEntity(values any, id string) (map[string]any, int) {
	return uniqueRepairEntityByField(values, "id", id)
}

func uniqueRepairEntityByField(values any, field, id string) (map[string]any, int) {
	var selected map[string]any
	count := 0
	rows, _ := values.([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row[field] == id {
			selected = row
			count++
		}
	}
	return selected, count
}

func stringSliceForRepairReport(value any) ([]string, bool) {
	switch values := value.(type) {
	case []string:
		return append([]string(nil), values...), true
	case []any:
		result := make([]string, len(values))
		for i, raw := range values {
			text, ok := raw.(string)
			if !ok {
				return nil, false
			}
			result[i] = text
		}
		return result, true
	default:
		return nil, false
	}
}

func containsStringForRepairReport(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// SubmitIssuedRepairPlanReport accepts one professional S9 answer through the
// original immutable PlanReport writer. The receipt only records planning
// evidence (work_complete=false); the same original Writer submission records
// the Builder work-start fact without a second Begin permission transaction.
func SubmitIssuedRepairPlanReport(root, statePath, journalPath string, req SubmitIssuedRepairPlanReportRequest) (runtimepkg.Snapshot, PlanReport, error) {
	snapshot, writer, err := readIssuedRepairPlanReportRuntime(root, statePath, journalPath, req.Context)
	if err != nil {
		return runtimepkg.Snapshot{}, PlanReport{}, err
	}
	req.Context = writer.Context()
	packet, found, err := runtimepkg.FindRepairPlanReportPacketContext(runtimepkg.WithPacketBodyRoot(req.Context, root), snapshot.State, req.PacketID)
	if err != nil {
		return snapshot, PlanReport{}, fmt.Errorf("S9 issued PlanReport packet %q is unavailable: %w", req.PacketID, err)
	}
	if !found {
		return snapshot, PlanReport{}, fmt.Errorf("S9 issued PlanReport packet %q is unavailable", req.PacketID)
	}
	if req.OwnerAgentID == "" || req.OwnerAgentID != packet.Binding.OwnerAgentID {
		return snapshot, PlanReport{}, errors.New("S9 PlanReport owner binding differs from registered Builder")
	}
	if req.AnswerPath != packet.AnswerPath {
		return snapshot, PlanReport{}, errors.New("S9 answer path differs from the exact issued packet")
	}
	actualSource, err := repairPlanReportSourceRoot(root, snapshot.State, packet)
	if err != nil {
		return snapshot, PlanReport{}, err
	}
	if req.SourceRoot == "" || !sameAbsolutePath(req.SourceRoot, actualSource) {
		return snapshot, PlanReport{}, errors.New("S9 answer source root differs from the current registered Main/Worker source")
	}
	answerBytes, err := workpacket.ReadFile(actualSource, packet.AnswerPath, repairPlanReportAnswerLimit)
	if err != nil {
		return snapshot, PlanReport{}, err
	}
	answer, problems := workpacket.ValidateRepairPlanReportAnswer(packet, answerBytes)
	if len(problems) != 0 {
		return snapshot, PlanReport{}, fmt.Errorf("S9 answer %s: %s (%s)", problems[0].Pointer, problems[0].Expected, problems[0].Observed)
	}
	answerSHA := workpacket.Digest(answer)
	// Same-answer replay reads only its indexed report; it does not replay
	// historical issuance or source certification.
	for _, ref := range existingArtifactRefs(repairPointer(snapshot.State)["plan_report_refs"]) {
		if ref.Path != filepath.ToSlash(filepath.Join(artifactRoot, "plan-reports", packet.ReportID+".json")) {
			continue
		}
		data, err := workpacket.ReadFile(root, ref.Path, repairPlanReportAnswerLimit)
		if err != nil {
			return snapshot, PlanReport{}, err
		}
		var report PlanReport
		if err := json.Unmarshal(data, &report); err != nil {
			return snapshot, PlanReport{}, err
		}
		if report.Receipt == nil || report.Receipt.PacketID != packet.ID || report.AgentID != req.OwnerAgentID ||
			!bytesEqualRepairPlanReportAnswer(report.Receipt.SubmittedAnswer, answer) {
			return snapshot, PlanReport{}, runtimepkg.ErrSubmissionConflict
		}
		return snapshot, report, nil
	}
	if err := checkRevision(req.ExpectedRevision, snapshot.Revision, "runtime S9 issued PlanReport answer"); err != nil {
		return snapshot, PlanReport{}, err
	}
	issueAt, err := repairPlanReportIssueTime(root, snapshot.State, packet, req.Context)
	if err != nil {
		return snapshot, PlanReport{}, err
	}
	var professional workpacket.RepairPlanReportProfessionalAnswer
	if err := json.Unmarshal(answer, &professional); err != nil {
		return snapshot, PlanReport{}, err
	}
	checks := make([]RepairCheck, len(professional.RedChecks))
	for i, check := range professional.RedChecks {
		checks[i] = RepairCheck{Name: check.Name, Command: check.Command, Result: check.Result, EvidenceRefs: append([]string(nil), check.EvidenceRefs...)}
	}
	receipt, err := newRepairPlanReportReceipt(root, snapshot, packet, answer, answerSHA, actualSource, issueAt)
	if err != nil {
		return snapshot, PlanReport{}, err
	}
	if err := cleanupUnacceptedIssuedPlanReport(root, writer, snapshot, packet, receipt, professional, issueAt); err != nil {
		return snapshot, PlanReport{}, err
	}
	var assignment struct {
		AssertionIDs []string `json:"assertion_ids"`
	}
	if err := json.Unmarshal(packet.Inputs.RepairAssignment, &assignment); err != nil {
		return snapshot, PlanReport{}, err
	}
	_, ref, err := CreatePlanReport(root, PlanReportRequest{
		Session:      ArtifactRef{ID: packet.Binding.SessionID, Path: packet.Binding.SessionRef, SHA256: packet.Binding.SessionSHA256},
		Plan:         ArtifactRef{ID: packet.Binding.PlanID, Path: packet.Binding.PlanRef, SHA256: packet.Binding.PlanSHA256},
		AssignmentID: packet.Binding.RepairAssignmentID, AgentID: req.OwnerAgentID, ReportID: packet.ReportID, AssertionIDs: assignment.AssertionIDs,
		PlanText: professional.Plan, RedChecks: checks, ProposedPaths: append([]string(nil), professional.ProposedPaths...),
		OccurredAt: issueAt, issuedReceipt: &receipt,
	})
	if err != nil {
		return snapshot, PlanReport{}, err
	}
	capability := &issuedPlanReportCapability{packet: packet, receipt: receipt, answer: answer, answerSHA: answerSHA, source: actualSource}
	occurredAt := req.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	next, committed, err := SubmitRepairPlanReportToRuntime(root, statePath, journalPath, SubmitPlanReportRequest{
		RuntimeRequest: RuntimeRequest{Context: req.Context, ExpectedRevision: snapshot.Revision, Actor: req.OwnerAgentID, OccurredAt: occurredAt},
		Report:         ref, issued: capability,
	})
	if err != nil {
		return snapshot, PlanReport{}, err
	}
	return next, committed, nil
}

func newRepairPlanReportReceipt(root string, snapshot runtimepkg.Snapshot, packet workpacket.RepairPlanReportPacket, answer json.RawMessage, answerSHA, executionRoot string, issueAt time.Time) (runtimepkg.RepairPlanReportReceipt, error) {
	journal, _ := snapshot.State["journal"].(map[string]any)
	sequence, _ := integerForRepairReport(journal["last_sequence"])
	commitRevision := snapshot.Revision + 1
	packetSHA, err := workpacket.RepairPlanReportPacketDigest(packet)
	if err != nil {
		return runtimepkg.RepairPlanReportReceipt{}, err
	}
	b := packet.Binding
	return runtimepkg.RepairPlanReportReceipt{
		Contract: repairPlanReportReceiptContract, PacketID: packet.ID,
		PacketSHA256: packetSHA, AnswerSHA256: answerSHA, SubmittedAnswer: append(json.RawMessage(nil), answer...),
		OwnerAgentID: b.OwnerAgentID, RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, ReviewRound: b.ReviewRound,
		CaseID: b.CaseID, CaseRef: b.CaseRef, CaseSHA256: b.CaseSHA256,
		ContractID: b.ContractID, ContractRef: b.ContractRef, ContractSHA256: b.ContractSHA256,
		SessionID: b.SessionID, SessionRef: b.SessionRef, SessionSHA256: b.SessionSHA256,
		PlanID: b.PlanID, PlanRef: b.PlanRef, PlanSHA256: b.PlanSHA256,
		RepairAssignmentID: b.RepairAssignmentID, RepairAssignmentSHA256: b.RepairAssignmentSHA256,
		ManifestAssignmentID: b.ManifestAssignmentID, ManifestAssignmentSHA256: b.ManifestAssignmentSHA256,
		TeamID: b.TeamID, TaskID: b.TaskID, TaskRef: b.TaskRef, TaskSHA256: b.TaskSHA256,
		ManifestRef: b.ManifestRef, ManifestSHA256: b.ManifestSHA256, ExecutionRoot: executionRoot,
		ReportID: packet.ReportID, ReportRef: filepath.ToSlash(filepath.Join(artifactRoot, "plan-reports", packet.ReportID+".json")),
		WorkComplete: false, IssuanceEventID: packet.IssuanceEventID,
		AcceptedEventID: "evt-s9-issued-plan-report-" + packet.ID, AcceptedIdempotencyKey: "runtime:s9:issued-plan-report:" + packet.ID,
		AcceptedAfterRuntimeRevision: commitRevision, AcceptedSequence: sequence + 1,
	}, nil
}

func repairPlanReportIssueTime(root string, state map[string]any, packet workpacket.RepairPlanReportPacket, contexts ...context.Context) (time.Time, error) {
	ctx := context.Background()
	if len(contexts) != 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	ctx = controlproof.WithJournalReadCache(ctx)
	data, err := controlproof.ReadJournalForStateContext(ctx, root, state)
	if err != nil {
		return time.Time{}, err
	}
	journal, err := controlproof.InspectJournalSegmentDataFromContext(ctx, data, 1)
	if err != nil {
		return time.Time{}, err
	}
	index, ok := journal.EventIndex[packet.IssuanceEventID]
	if !ok {
		return time.Time{}, errors.New("S9 packet issue event has no retained original occurred_at")
	}
	at, err := time.Parse(time.RFC3339Nano, stringField(journal.Events[index]["occurred_at"]))
	if err != nil {
		return time.Time{}, fmt.Errorf("S9 packet issue occurred_at: %w", err)
	}
	return at.UTC(), nil
}

func repairPlanReportSourceRoot(root string, state map[string]any, packet workpacket.RepairPlanReportPacket) (string, error) {
	mainRoot, err := workspace.Canonical(root)
	if err != nil {
		return "", err
	}
	b := packet.Binding
	pointer := repairPointer(state)
	baseline, _ := state["baseline"].(map[string]any)
	generation, _ := integerForRepairReport(baseline["generation"])
	if state["runtime_id"] != b.RuntimeID || generation != b.BaselineGeneration ||
		pointer["session_id"] != b.SessionID || pointer["plan_ref"] != b.PlanRef ||
		stringMapField(pointer["assignment_owners"])[b.RepairAssignmentID] != b.OwnerAgentID {
		return "", errors.New("S9 Plan packet is not the current responsibility")
	}
	return mainRoot, nil
}

func repairPlanReportWorkerDeclared(registry *workspace.ExecutionRegistry, packet workpacket.RepairPlanReportPacket) bool {
	if registry == nil {
		return false
	}
	binding := packet.Binding
	identities := map[string]bool{
		binding.RepairAssignmentID:                          true,
		binding.ManifestAssignmentID:                        true,
		manifestAssignmentAlias(binding.RepairAssignmentID): true,
	}
	for key, execution := range registry.Executions {
		if !identities[key] && !identities[execution.AssignmentID] {
			continue
		}
		if execution.RuntimeID == binding.RuntimeID && execution.BaselineGeneration == binding.BaselineGeneration && execution.AgentID == binding.OwnerAgentID {
			return true
		}
	}
	return false
}

func cleanupUnacceptedIssuedPlanReport(root string, writer *runtimepkg.Store, snapshot runtimepkg.Snapshot, packet workpacket.RepairPlanReportPacket, expected runtimepkg.RepairPlanReportReceipt, answer workpacket.RepairPlanReportProfessionalAnswer, issueAt time.Time) error {
	path := filepath.ToSlash(filepath.Join(artifactRoot, "plan-reports", packet.ReportID+".json"))
	data, err := workpacket.ReadFile(root, path, repairPlanReportAnswerLimit)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("preserve unrecognized S9 PlanReport staging path %s: %w", path, err)
	}
	sha := workpacket.Digest(data)
	staged, err := ValidatePlanReport(root, ArtifactRef{Path: path, SHA256: sha})
	if err != nil || staged.Receipt == nil || staged.ReportID != packet.ReportID || staged.AssignmentID != packet.Binding.RepairAssignmentID ||
		staged.AgentID != packet.Binding.OwnerAgentID || staged.SessionID != packet.Binding.SessionID || staged.PlanID != packet.Binding.PlanID ||
		staged.Receipt.PacketID != packet.ID || staged.Receipt.ReportID != packet.ReportID || staged.Receipt.ReportRef != path ||
		staged.Receipt.IssuanceEventID != packet.IssuanceEventID || staged.Receipt.WorkComplete || staged.Receipt.AcceptedEventID != "evt-s9-issued-plan-report-"+packet.ID {
		return fmt.Errorf("preserve existing S9 PlanReport path %s: it is not an exact unaccepted packet staging artifact (%v)", path, err)
	}
	stagedReceipt := *staged.Receipt
	// The only fields allowed to differ from a fresh deterministic retry are
	// the predicted original Runtime revision and journal sequence. A prior
	// failed CAS can make those two coordinates stale while all frozen source,
	// answer and actor evidence remains the same.
	stagedReceipt.AcceptedAfterRuntimeRevision = expected.AcceptedAfterRuntimeRevision
	stagedReceipt.AcceptedSequence = expected.AcceptedSequence
	assignmentRaw, assignmentErr := json.Marshal(packet.Inputs.RepairAssignment)
	var assignment map[string]any
	if assignmentErr == nil {
		assignmentErr = json.Unmarshal(assignmentRaw, &assignment)
	}
	assertionIDs, assertionOK := stringSliceForRepairReport(assignment["assertion_ids"])
	wantChecks := make([]RepairCheck, len(answer.RedChecks))
	for i, check := range answer.RedChecks {
		wantChecks[i] = RepairCheck{Name: check.Name, Command: check.Command, Result: check.Result, EvidenceRefs: append([]string(nil), check.EvidenceRefs...)}
	}
	if assignmentErr != nil || !assertionOK || !reflect.DeepEqual(stagedReceipt, expected) || staged.Status != "reported" ||
		staged.ReportedAt != issueAt.UTC().Format(time.RFC3339Nano) || !reflect.DeepEqual(staged.AssertionIDs, assertionIDs) || staged.Plan != answer.Plan ||
		!reflect.DeepEqual(staged.ProposedPaths, answer.ProposedPaths) || !reflect.DeepEqual(staged.RedChecks, wantChecks) {
		return fmt.Errorf("preserve existing S9 PlanReport path %s: the staged document does not equal this unaccepted answer/source retry", path)
	}
	for _, ref := range existingArtifactRefs(repairPointer(snapshot.State)["plan_report_refs"]) {
		if ref.Path == path {
			return fmt.Errorf("preserve S9 PlanReport staging artifact %s because the current Runtime pointer references it", path)
		}
	}
	removed, err := writer.RemoveUnreferencedArtifact(runtimepkg.ArtifactCleanupRequest{
		ExpectedRevision: snapshot.Revision, ArtifactPath: path, ArtifactSHA256: sha,
		ReferencedPaths: stateArtifactPaths(snapshot.State),
	})
	if err != nil {
		return fmt.Errorf("clean exact unaccepted S9 Report staging artifact %s: %w", path, err)
	}
	if !removed {
		return fmt.Errorf("preserve S9 Report staging artifact %s: pending Runtime state or reference prevents safe cleanup; inspect recovery before retry", path)
	}
	return nil
}

func sameAbsolutePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	a, errA := filepath.Abs(left)
	b, errB := filepath.Abs(right)
	return errA == nil && errB == nil && filepath.Clean(a) == filepath.Clean(b)
}

func sameRepairPlanReportPacket(a, b workpacket.RepairPlanReportPacket) bool {
	left, err := workpacket.RepairPlanReportPacketDigest(a)
	if err != nil {
		return false
	}
	right, err := workpacket.RepairPlanReportPacketDigest(b)
	return err == nil && left == right
}

func containsRepairPlanReportPacket(values []workpacket.RepairPlanReportPacket, expected workpacket.RepairPlanReportPacket) bool {
	for _, value := range values {
		if value.ID == expected.ID && sameRepairPlanReportPacket(value, expected) {
			return true
		}
	}
	return false
}

func integerForRepairReport(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case int64:
		return int(n), int64(int(n)) == n
	case float64:
		return int(n), float64(int(n)) == n
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil && int64(int(i)) == i
	default:
		return 0, false
	}
}

func loadAcceptedRepairPlanReport(root string, state map[string]any, receipt runtimepkg.RepairPlanReportReceipt) (PlanReport, error) {
	pointer := repairPointer(state)
	for _, ref := range existingArtifactRefs(pointer["plan_report_refs"]) {
		if ref.Path == receipt.ReportRef {
			report, err := ValidatePlanReport(root, ref)
			if err != nil {
				return PlanReport{}, err
			}
			if report.Receipt == nil || !reflect.DeepEqual(*report.Receipt, receipt) {
				return PlanReport{}, errors.New("S9 accepted report artifact does not contain its exact original receipt")
			}
			return report, nil
		}
	}
	if pointer != nil && stringField(pointer["plan_report_ref"]) == receipt.ReportRef {
		ref := ArtifactRef{Path: receipt.ReportRef, SHA256: stringField(pointer["plan_report_sha256"])}
		report, err := ValidatePlanReport(root, ref)
		if err != nil {
			return PlanReport{}, err
		}
		if report.Receipt == nil || !reflect.DeepEqual(*report.Receipt, receipt) {
			return PlanReport{}, errors.New("S9 accepted report artifact does not contain its exact original receipt")
		}
		return report, nil
	}
	return PlanReport{}, errors.New("S9 accepted receipt is not retained by the original repair pointer")
}

// This binding keeps normal startup attached to the recorded task and report.
// It does not establish a write grant or require sibling PlanReports.
type repairExecutionBeginBinding struct {
	runtimeID          string
	baselineGeneration int
	sessionID          string
	planRef            ArtifactRef
	reportRef          ArtifactRef
}

func captureRepairExecutionBeginBinding(root string, state map[string]any) (repairExecutionBeginBinding, PlanReport, error) {
	pointer := repairPointer(state)
	planRef, err := pointerArtifact(pointer, "plan_ref", "plan_sha256", "current RepairPlan")
	if err != nil {
		return repairExecutionBeginBinding{}, PlanReport{}, err
	}
	reportRef, err := pointerArtifact(pointer, "plan_report_ref", "plan_report_sha256", "current PlanReport")
	if err != nil {
		return repairExecutionBeginBinding{}, PlanReport{}, err
	}
	binding := repairExecutionBeginBinding{runtimeID: stringField(state["runtime_id"]), baselineGeneration: baselineGeneration(state), sessionID: stringField(pointer["session_id"]), planRef: planRef, reportRef: reportRef}
	report, err := validateRepairExecutionBeginState(root, state, binding)
	return binding, report, err
}

func validateRepairExecutionBeginState(root string, state map[string]any, expected repairExecutionBeginBinding) (PlanReport, error) {
	pointer := repairPointer(state)
	if pointer == nil || stringField(state["runtime_id"]) != expected.runtimeID || baselineGeneration(state) != expected.baselineGeneration || stringField(pointer["session_id"]) != expected.sessionID || lifecycleState(state) != "bug_resolution" || lifecyclePhase(state) != "reproducing" || pointer["status"] != "reproducing" {
		return PlanReport{}, errors.New("S9 current task changed before normal work startup")
	}
	planRef, err := pointerArtifact(pointer, "plan_ref", "plan_sha256", "current RepairPlan")
	if err != nil || planRef != expected.planRef {
		return PlanReport{}, errors.New("S9 current Plan changed before normal work startup")
	}
	reportRef, err := pointerArtifact(pointer, "plan_report_ref", "plan_report_sha256", "current PlanReport")
	if err != nil || reportRef != expected.reportRef {
		return PlanReport{}, errors.New("S9 current PlanReport changed before normal work startup")
	}
	plan, err := ValidateRepairPlan(root, planRef)
	if err != nil {
		return PlanReport{}, err
	}
	report, err := ValidatePlanReport(root, reportRef)
	if err != nil {
		return PlanReport{}, err
	}
	if report.SessionID != expected.sessionID || plan.SessionID != expected.sessionID || report.PlanID != plan.PlanID {
		return PlanReport{}, errors.New("S9 recorded PlanReport is not attached to the current task")
	}
	if _, found := assignmentByID(plan.Assignments, report.AssignmentID); !found {
		return PlanReport{}, errors.New("S9 recorded PlanReport Assignment is absent from the current Plan")
	}
	if owner := stringMapField(pointer["assignment_owners"])[report.AssignmentID]; owner != "" && owner != report.AgentID {
		return PlanReport{}, errors.New("S9 recorded PlanReport differs from its current responsibility")
	}
	return report, nil
}

func strictPlanReportRefs(raw any, singular ArtifactRef) ([]ArtifactRef, error) {
	refs := []ArtifactRef{}
	values, exists := raw.([]any)
	if !exists || len(values) == 0 {
		if raw != nil {
			return nil, errors.New("S9 plan_report_refs is not a non-empty reference array")
		}
		refs = append(refs, singular)
	} else {
		seen := map[string]string{}
		for index, value := range values {
			row, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("S9 plan_report_refs[%d] is not an object", index)
			}
			ref := ArtifactRef{Path: stringField(row["path"]), SHA256: stringField(row["sha256"])}
			if ref.Path == "" || ref.SHA256 == "" {
				return nil, fmt.Errorf("S9 plan_report_refs[%d] lacks exact path/SHA", index)
			}
			if old, duplicate := seen[ref.Path]; duplicate {
				if old != ref.SHA256 {
					return nil, fmt.Errorf("S9 plan_report_refs has conflicting SHA values for %s", ref.Path)
				}
				return nil, fmt.Errorf("S9 plan_report_refs duplicates %s", ref.Path)
			}
			seen[ref.Path] = ref.SHA256
			refs = append(refs, ref)
		}
	}
	for _, ref := range refs {
		if ref.Path == singular.Path && ref.SHA256 == singular.SHA256 {
			return refs, nil
		}
	}
	return nil, errors.New("S9 singular current PlanReport is absent from plan_report_refs")
}

type recoveredRepairPlanReport struct {
	snapshot runtimepkg.Snapshot
	report   PlanReport
}

func recoverAcceptedRepairPlanReport(root, statePath, journalPath string, packet workpacket.RepairPlanReportPacket, req SubmitIssuedRepairPlanReportRequest, answerSHA, sourceRoot string) (bool, recoveredRepairPlanReport, error) {
	snapshot, _, err := readIssuedRepairPlanReportRuntime(root, statePath, journalPath, req.Context)
	if err != nil {
		return false, recoveredRepairPlanReport{}, err
	}
	actual, err := repairPlanReportSourceRoot(root, snapshot.State, packet)
	if err != nil || !sameAbsolutePath(actual, sourceRoot) {
		return false, recoveredRepairPlanReport{}, errors.New("S9 replay source root changed")
	}
	answerBytes, err := workpacket.ReadFile(actual, packet.AnswerPath, repairPlanReportAnswerLimit)
	if err != nil {
		return false, recoveredRepairPlanReport{}, err
	}
	answer, problems := workpacket.ValidateRepairPlanReportAnswer(packet, answerBytes)
	if len(problems) != 0 || workpacket.Digest(answer) != answerSHA {
		return false, recoveredRepairPlanReport{}, runtimepkg.ErrSubmissionConflict
	}
	receipt, ok, err := runtimepkg.VerifiedRepairPlanReportReceipt(root, snapshot.State, packet.ID, req.OwnerAgentID, req.Context)
	if err != nil || !ok {
		return false, recoveredRepairPlanReport{}, err
	}
	if receipt.AnswerSHA256 != answerSHA || !sameAbsolutePath(receipt.ExecutionRoot, sourceRoot) {
		return false, recoveredRepairPlanReport{}, runtimepkg.ErrSubmissionConflict
	}
	report, err := loadAcceptedRepairPlanReport(root, snapshot.State, receipt)
	if err != nil {
		return false, recoveredRepairPlanReport{}, err
	}
	return true, recoveredRepairPlanReport{snapshot: snapshot, report: report}, nil
}

// readIssuedRepairPlanReportRuntime is limited to the typed S9 consumer and
// its explicit recovery check. Unlike observation-only Runtime readers, the
// original mutation-capable Store recovers a durable pending write under the
// Runtime lock before this path reads author bytes or attempts receipt lookup.
// A recovered write is still accepted only after the exact original receipt,
// packet issue, answer bytes, owner, and source/session proof below verify.
func readIssuedRepairPlanReportRuntime(root, statePath, journalPath string, contexts ...context.Context) (runtimepkg.Snapshot, *runtimepkg.Store, error) {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	ctx = controlproof.WithJournalReadCache(ctx)
	writer := runtimepkg.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(ctx)
	snapshot, err := writer.Snapshot()
	if err != nil {
		return runtimepkg.Snapshot{}, nil, err
	}
	return snapshot, writer, nil
}

// IssuedRepairPlanReportReceipt only reconstructs an accepted result from the
// original Report artifact and Runtime journal. It never interprets the draft
// as permission or reads an unaccepted authoring file.
func IssuedRepairPlanReportReceipt(root string, state map[string]any, packetID, owner string) (runtimepkg.RepairPlanReportReceipt, bool, error) {
	packet, found, err := runtimepkg.FindRepairPlanReportPacketContext(runtimepkg.WithPacketBodyRoot(context.Background(), root), state, packetID)
	if err != nil || !found {
		return runtimepkg.RepairPlanReportReceipt{}, false, err
	}
	if owner == "" || packet.Binding.OwnerAgentID != owner {
		return runtimepkg.RepairPlanReportReceipt{}, false, errors.New("Plan receipt does not match its recorded responsibility")
	}
	for _, ref := range existingArtifactRefs(repairPointer(state)["plan_report_refs"]) {
		if ref.Path != filepath.ToSlash(filepath.Join(artifactRoot, "plan-reports", packet.ReportID+".json")) {
			continue
		}
		data, err := workpacket.ReadFile(root, ref.Path, repairPlanReportAnswerLimit)
		if err != nil {
			return runtimepkg.RepairPlanReportReceipt{}, false, err
		}
		var report PlanReport
		if err := json.Unmarshal(data, &report); err != nil {
			return runtimepkg.RepairPlanReportReceipt{}, false, err
		}
		if report.Receipt != nil && report.ReportID == packet.ReportID && report.AgentID == owner && report.Receipt.PacketID == packetID {
			return *report.Receipt, true, nil
		}
	}
	return runtimepkg.RepairPlanReportReceipt{}, false, nil
}
