package repair

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/integration"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// AcceptIssuedRepairResultBlockerRequest names the issued S9 answer and its
// registered Worker source. AuthorizeSource is retained for request compatibility and is not called.
// The current questionnaire and execution attempt locate the answer.
type AcceptIssuedRepairResultBlockerRequest struct {
	ExpectedRevision int
	PacketID         string
	AnswerPath       string
	OwnerAgentID     string
	SourceRoot       string
	AuthorizeSource  func(root string, state map[string]any) error
}

// IssuedRepairResultBlockerCapability is an opaque, one-transition capability
// for the original Agent lifecycle CAS. Its fields are private so callers
// cannot manufacture evidence IDs, answer bytes, source identity, or authority.
// It deliberately stores no context; every operation receives its caller's
// context and can reuse an outer workspace lock.
type IssuedRepairResultBlockerCapability struct {
	packet         workpacket.RepairResultPacket
	artifact       runtimepkg.RepairResultBlockerArtifact
	artifactRef    ArtifactRef
	artifactBytes  []byte
	messageRef     string
	messageBytes   []byte
	answer         json.RawMessage
	sourceRoot     string
	owner          string
	taskID         string
	mainRoot       string
	beforeRevision int
	generation     int
}

// PrepareIssuedRepairResultBlocker locates the current attempt, validates its
// new professional answer once, and stages message/answer artifacts for the original
// AGENT-LIFECYCLE writer. A nil capability with a receipt means an exact
// previously accepted answer was found; no new Runtime mutation is implied.
func PrepareIssuedRepairResultBlocker(
	ctx context.Context,
	root, statePath, journalPath string,
	req AcceptIssuedRepairResultBlockerRequest,
) (*IssuedRepairResultBlockerCapability, *runtimepkg.RepairResultBlockerReceipt, error) {
	if ctx == nil {
		return nil, nil, errors.New("S9 blocker preparation requires the caller context")
	}
	lockedCtx, unlock, err := integration.LockWorkspace(ctx, root)
	if err != nil {
		return nil, nil, err
	}
	defer unlock()
	ctx = lockedCtx

	snapshot, _, err := readIssuedRepairResultRuntime(ctx, root, statePath, journalPath)
	if err != nil {
		return nil, nil, err
	}
	if mainRoot := workspaceMainRoot(root, snapshot.State); !sameAbsolutePath(mainRoot, root) {
		return nil, nil, errors.New("S9 blocker receiver root must be the original registered Main checkout")
	}
	packet, found, err := runtimepkg.FindRepairResultPacketContext(runtimepkg.WithPacketBodyRoot(ctx, root), snapshot.State, req.PacketID)
	if err != nil || !found {
		return nil, nil, fmt.Errorf("S9 issued RepairResult packet %q is unavailable: %v", req.PacketID, err)
	}
	req.OwnerAgentID = packet.Binding.Sources.OwnerAgentID
	if req.AnswerPath != packet.AnswerPath {
		return nil, nil, errors.New("S9 blocker answer path differs from the exact issued packet")
	}
	existing, accepted, err := runtimepkg.VerifiedRepairResultBlockerReceipt(root, snapshot.State, packet.ID, req.OwnerAgentID)
	if err != nil {
		return nil, nil, err
	}
	if accepted {
		if err := verifyAcceptedBlockerBytes(ctx, root, snapshot.State, packet, existing, req.SourceRoot, "", existing.Artifact.SubmittedAnswer); err != nil {
			return nil, nil, err
		}
		return nil, &existing, nil
	}
	if req.ExpectedRevision < 0 || req.ExpectedRevision != snapshot.Revision {
		return nil, nil, runtimepkg.ErrStaleRevision
	}
	if err := rejectRepairResultBlockerOutcome(root, snapshot.State, packet, req.OwnerAgentID); err != nil {
		return nil, nil, err
	}
	if req.SourceRoot == "" {
		return nil, nil, errors.New("S9 blocker requires the exact registered Worker execution root")
	}

	execution, head, err := verifyIssuedRepairResultBlockerSource(ctx, root, snapshot.State, packet, req.SourceRoot, "")
	if err != nil {
		return nil, nil, err
	}
	head, _ = workspace.Git(ctx, execution.Path, "rev-parse", "HEAD")
	answer, answerSHA, err := readIssuedRepairResultBlockerAnswer(req.SourceRoot, packet)
	if err != nil {
		return nil, nil, err
	}
	issueAt := time.Now().UTC()
	packetSHA, err := workpacket.RepairResultPacketDigest(packet)
	if err != nil {
		return nil, nil, err
	}
	manifestAssignment, err := blockerManifestAssignment(packet)
	if err != nil {
		return nil, nil, err
	}
	b := packet.Binding.Sources
	base := path.Join(".claude/evidence", b.RuntimeID, fmt.Sprintf("g%d", b.BaselineGeneration), "S9")
	message := map[string]any{
		"schema_version":       "1.0.0",
		"message_type":         "blocker_report",
		"message_id":           blockerMessageID(packet.ID, answerSHA),
		"correlation_id":       blockerCorrelationID(packet.ID),
		"runtime_id":           b.RuntimeID,
		"agent_id":             b.OwnerAgentID,
		"agent_definition_ref": manifestAssignment["agent_definition_ref"],
		"task_id":              b.TaskID,
		"bug_id":               nil,
		"team_id":              b.TeamID,
		"occurred_at":          issueAt.UTC().Format(time.RFC3339Nano),
		"body":                 "S9 issued RepairResult answer reports a blocker; the complete professional answer is preserved with this message.",
		"s9_blocker":           map[string]any{"packet_id": packet.ID, "packet_sha256": packetSHA, "answer_path": packet.AnswerPath, "answer_sha256": answerSHA},
	}
	messageRef, messageBytes, err := writeBlockerImmutable(root, path.Join(base, "blocker-messages", packet.ID), "agent-message.schema.json", message)
	if err != nil {
		return nil, nil, err
	}
	artifact := runtimepkg.RepairResultBlockerArtifact{
		Contract: "s9-repair-result-blocker-v1", SchemaVersion: "1.0.0",
		PacketID: packet.ID, PacketSHA256: packetSHA, IssuanceEventID: packet.IssuanceEventID,
		Binding: packet.Binding, ExecutionAssignmentID: execution.AssignmentID,
		ExecutionGeneration: execution.Generation, ExecutionRoot: execution.Path,
		PlatformSessionID: execution.PlatformSessionID, BaseCommit: execution.BaseCommit,
		SourceHead: head, AnswerPath: packet.AnswerPath,
		SubmittedAnswer: append(json.RawMessage(nil), answer...), AnswerSHA256: answerSHA,
		MessageRef: messageRef.Path, MessageSHA256: messageRef.SHA256,
	}
	artifactRef, artifactBytes, err := writeBlockerImmutable(root, path.Join(base, "blockers", packet.ID), "repair-result-blocker.schema.json", artifact)
	if err != nil {
		return nil, nil, err
	}
	capability := &IssuedRepairResultBlockerCapability{
		packet: packet, artifact: artifact, artifactRef: artifactRef,
		artifactBytes: append([]byte(nil), artifactBytes...), messageRef: messageRef.Path,
		messageBytes: append([]byte(nil), messageBytes...), answer: append(json.RawMessage(nil), answer...),
		sourceRoot: execution.Path, owner: req.OwnerAgentID,
		taskID: b.TaskID, mainRoot: root, beforeRevision: snapshot.Revision,
		generation: b.BaselineGeneration,
	}
	return capability, nil, nil
}

// MessageRef returns the exact immutable blocker message path that the
// original Agent lifecycle request must consume.
func (capability *IssuedRepairResultBlockerCapability) MessageRef() string {
	if capability == nil {
		return ""
	}
	return capability.messageRef
}

// EvidenceIDs returns the only two original lifecycle evidence references:
// the immutable message and its complete answer artifact.
func (capability *IssuedRepairResultBlockerCapability) EvidenceIDs() []string {
	if capability == nil || capability.messageRef == "" || capability.artifactRef.Path == "" {
		return nil
	}
	return []string{capability.messageRef, capability.artifactRef.Path}
}

// ClearResolvedBlockerIndex clears the previous resolution pointer when the
// current work becomes blocked again. Accepted history remains available.
func (capability *IssuedRepairResultBlockerCapability) ClearResolvedBlockerIndex() bool {
	return capability != nil
}

// ValidateLifecycleRequest binds the pre-existing lifecycle entry to this
// capability. The lifecycle writer remains the sole owner of Agent state.
func (capability *IssuedRepairResultBlockerCapability) ValidateLifecycleRequest(root, owner, event, messagePath string, beforeRevision, generation int) error {
	if err := capability.validate(); err != nil {
		return err
	}
	if !sameAbsolutePath(root, capability.mainRoot) || owner != capability.owner || event != "work_blocked" ||
		!blockerRequestPathMatches(root, capability.messageRef, messagePath) || beforeRevision != capability.beforeRevision || generation != capability.generation {
		return runtimepkg.ErrSubmissionConflict
	}
	return nil
}

// AcceptedReplay reads the same recorded blocker without repeating the
// lifecycle event or checking the live Worker checkout.
func (capability *IssuedRepairResultBlockerCapability) AcceptedReplay(ctx context.Context, root string, state map[string]any, owner, event, messagePath string) (bool, error) {
	if err := capability.validate(); err != nil {
		return false, err
	}
	if ctx == nil {
		return false, errors.New("S9 blocker replay requires the original operation context")
	}
	receipt, found, err := runtimepkg.VerifiedRepairResultBlockerReceipt(root, state, capability.packet.ID, owner)
	if err != nil || !found {
		return false, err
	}
	if !sameAbsolutePath(root, capability.mainRoot) || owner != capability.owner || event != "work_blocked" ||
		!blockerRequestPathMatches(root, capability.messageRef, messagePath) || receipt.ArtifactRef != capability.artifactRef.Path ||
		receipt.Artifact.ExecutionGeneration != capability.artifact.ExecutionGeneration ||
		!sameAbsolutePath(receipt.Artifact.ExecutionRoot, capability.sourceRoot) ||
		!bytesEqualRepairPlanReportAnswer(receipt.Artifact.SubmittedAnswer, capability.answer) {
		return false, runtimepkg.ErrSubmissionConflict
	}
	return true, nil
}

// ApplyLifecycleCAS is called only from the original Agent Runtime mutation's
// Apply callback. It checks the current responsibility and execution attempt
// before recording one blocker evidence row. Agent/graph changes stay in the
// original lifecycle Writer.
func (capability *IssuedRepairResultBlockerCapability) ApplyLifecycleCAS(
	ctx context.Context,
	root string,
	state map[string]any,
	messageBytes []byte,
	owner, taskID string,
	beforeRevision, generation int,
) error {
	if err := capability.validate(); err != nil {
		return err
	}
	if ctx == nil {
		return errors.New("S9 blocker lifecycle Apply requires the original writer context")
	}
	if !sameAbsolutePath(root, capability.mainRoot) || owner != capability.owner || taskID != capability.taskID ||
		beforeRevision != capability.beforeRevision || generation != capability.generation {
		return runtimepkg.ErrStaleRevision
	}
	if got, ok := state["revision"]; !ok || integerRepairResult(got) != beforeRevision {
		return runtimepkg.ErrStaleRevision
	}
	if state["runtime_id"] != capability.packet.Binding.Sources.RuntimeID {
		return errors.New("S9 blocker Runtime identity changed before the original lifecycle CAS")
	}
	baseline, _ := state["baseline"].(map[string]any)
	if integerRepairResult(baseline["generation"]) != generation {
		return errors.New("S9 blocker baseline generation changed before the original lifecycle CAS")
	}
	packet := capability.packet
	pointer := repairPointer(state)
	if pointer == nil || stringField(pointer["session_id"]) != packet.Binding.Sources.SessionID ||
		stringField(pointer["plan_ref"]) != packet.Binding.Sources.PlanRef {
		return errors.New("S9 blocker responsibility changed before recording")
	}
	execution, _, err := verifyIssuedRepairResultBlockerSource(ctx, root, state, packet, capability.sourceRoot, "")
	if err != nil {
		return err
	}
	if execution.AssignmentID != capability.artifact.ExecutionAssignmentID || execution.Generation != capability.artifact.ExecutionGeneration || execution.BaseCommit != capability.artifact.BaseCommit {
		return errors.New("S9 blocker execution attempt changed before recording")
	}
	if !reflect.DeepEqual(messageBytes, capability.messageBytes) {
		return runtimepkg.ErrSubmissionConflict
	}
	answer, digest, err := readIssuedRepairResultBlockerAnswer(capability.sourceRoot, packet)
	if err != nil {
		return err
	}
	if digest != capability.artifact.AnswerSHA256 || !bytesEqualRepairPlanReportAnswer(answer, capability.answer) {
		return runtimepkg.ErrSubmissionConflict
	}
	return appendRepairResultBlockerEvidence(state, packet, capability.artifactRef)
}

func (capability *IssuedRepairResultBlockerCapability) validate() error {
	if capability == nil || capability.packet.ID == "" || capability.owner == "" || capability.taskID == "" ||
		capability.mainRoot == "" || capability.sourceRoot == "" ||
		capability.messageRef == "" || len(capability.messageBytes) == 0 || len(capability.answer) == 0 ||
		capability.artifactRef.Path == "" || capability.artifactRef.SHA256 == "" ||
		len(capability.artifactBytes) == 0 || capability.beforeRevision < 0 || capability.generation < 1 {
		return errors.New("invalid or zero S9 RepairResult blocker capability")
	}
	return nil
}

func verifyAcceptedBlockerBytes(ctx context.Context, root string, state map[string]any, packet workpacket.RepairResultPacket, receipt runtimepkg.RepairResultBlockerReceipt, sourceRoot, expectedHead string, expectedAnswer json.RawMessage) error {
	if receipt.Artifact.PacketID != packet.ID || receipt.Artifact.AnswerPath != packet.AnswerPath || !sameAbsolutePath(sourceRoot, receipt.Artifact.ExecutionRoot) {
		return runtimepkg.ErrSubmissionConflict
	}
	execution, _, err := verifyIssuedRepairResultBlockerSource(ctx, root, state, packet, sourceRoot, "")
	if err != nil || execution.AssignmentID != receipt.Artifact.ExecutionAssignmentID || execution.Generation != receipt.Artifact.ExecutionGeneration {
		return runtimepkg.ErrSubmissionConflict
	}
	answer, answerSHA, err := readIssuedRepairResultBlockerAnswer(sourceRoot, packet)
	if err != nil || answerSHA != receipt.Artifact.AnswerSHA256 || !bytesEqualRepairPlanReportAnswer(answer, expectedAnswer) || !bytesEqualRepairPlanReportAnswer(answer, receipt.Artifact.SubmittedAnswer) {
		return runtimepkg.ErrSubmissionConflict
	}
	return nil
}

func verifyIssuedRepairResultBlockerSource(ctx context.Context, root string, state map[string]any, packet workpacket.RepairResultPacket, requestedRoot, expectedHead string) (workspace.Execution, string, error) {
	raw, err := json.Marshal(state["workspace"])
	if err != nil {
		return workspace.Execution{}, "", err
	}
	var registry workspace.ExecutionRegistry
	if err := json.Unmarshal(raw, &registry); err != nil {
		return workspace.Execution{}, "", err
	}
	b := packet.Binding.Sources
	var selected *workspace.Execution
	for _, execution := range registry.Executions {
		if (execution.AssignmentID != b.RepairAssignmentID && execution.AssignmentID != b.ManifestAssignmentID) || execution.RuntimeID != b.RuntimeID || execution.BaselineGeneration != b.BaselineGeneration || execution.AgentID != b.OwnerAgentID {
			continue
		}
		if selected != nil {
			return workspace.Execution{}, "", errors.New("S9 blocker has ambiguous registered work locations")
		}
		copy := execution
		selected = &copy
	}
	if selected == nil || selected.DeliveryRef != "" || selected.ReworkRef != packet.Binding.ReworkRef {
		return workspace.Execution{}, "", errors.New("S9 blocker has no current ready workspace attempt")
	}
	execution := *selected
	if requestedRoot == "" || !sameAbsolutePath(requestedRoot, execution.Path) {
		return workspace.Execution{}, "", errors.New("S9 blocker answer root differs from the current Worker attempt")
	}
	var frozen map[string]any
	if err := json.Unmarshal(packet.Inputs.Execution, &frozen); err != nil || frozen == nil {
		return workspace.Execution{}, "", errors.New("S9 blocker packet has an invalid frozen execution definition")
	}
	if frozen["assignment_id"] != execution.AssignmentID || execution.RuntimeID != packet.Binding.Sources.RuntimeID ||
		execution.BaselineGeneration != packet.Binding.Sources.BaselineGeneration || execution.AgentID != packet.Binding.Sources.OwnerAgentID ||
		frozen["base_commit"] != execution.BaseCommit || frozen["worktree_path"] != execution.Path ||
		integerRepairResult(frozen["execution_generation"]) != execution.Generation {
		return workspace.Execution{}, "", errors.New("S9 blocker current execution owner/base tuple differs from the frozen assignment")
	}
	// Explicit Main resolution retains its legacy source diagnostic. Normal
	// blocker reception/progress never requests or depends on this proof.
	if expectedHead != "" {
		head, err := workspace.Git(ctx, execution.Path, "rev-parse", "HEAD")
		if err != nil || head != expectedHead {
			return workspace.Execution{}, "", errors.New("S9 resolution Worker HEAD differs from its recorded blocker")
		}
		return execution, head, nil
	}
	return execution, "", nil
}

func readIssuedRepairResultBlockerAnswer(sourceRoot string, packet workpacket.RepairResultPacket) (json.RawMessage, string, error) {
	data, err := workpacket.ReadFile(sourceRoot, packet.AnswerPath, repairResultAnswerLimit)
	if err != nil {
		return nil, "", err
	}
	answer, problems := workpacket.ValidateRepairResultAnswer(packet, data)
	if len(problems) != 0 {
		return nil, "", &workpacket.AnswerError{Label: "S9 blocker answer", Fields: problems}
	}
	var professional workpacket.RepairResultProfessionalAnswer
	if err := json.Unmarshal(answer, &professional); err != nil {
		return nil, "", err
	}
	switch professional.Verdict {
	case "blocked", "contract_conflict", "stale":
	default:
		return nil, "", fmt.Errorf("S9 blocker receipt only preserves blocked, contract_conflict, or stale professional verdicts")
	}
	return answer, workpacket.Digest(answer), nil
}

func rejectRepairResultBlockerOutcome(root string, state map[string]any, packet workpacket.RepairResultPacket, owner string) error {
	_, execution, err := currentRepairExecution(context.Background(), root, state, packet, true)
	if err != nil {
		return err
	}
	if execution.DeliveryRef != "" {
		return errors.New("S9 packet already has a candidate; a different blocker answer conflicts")
	}
	if _, _, found, err := existingIssuedRepairResult(root, state, packet.ResultID); err != nil {
		return err
	} else if found {
		return errors.New("S9 packet already has a Final; a different blocker answer conflicts")
	}
	return nil
}

func appendRepairResultBlockerEvidence(state map[string]any, packet workpacket.RepairResultPacket, ref ArtifactRef) error {
	rows, ok := state["evidence"].([]any)
	if !ok {
		return errors.New("Runtime evidence index is unavailable for the original blocker lifecycle CAS")
	}
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && (row["id"] == ref.Path || row["path"] == ref.Path) {
			return fmt.Errorf("S9 blocker evidence path already exists; recover or inspect the original lifecycle event")
		}
	}
	b := packet.Binding.Sources
	row := map[string]any{
		"id": ref.Path, "kind": "repair_result_blocker", "path": ref.Path, "sha256": ref.SHA256,
		"status": "valid", "baseline_generation": b.BaselineGeneration, "review_round": b.ReviewRound,
		"produced_by": []any{b.OwnerAgentID}, "invalidated_by": nil, "invalidation_rule": nil,
		"invalidation_reason": nil, "responsibility_id": "BUILD-WORK-PACKAGE",
		"scope_refs": []any{packet.ID, b.RepairAssignmentID},
	}
	state["evidence"] = append(rows, row)
	return nil
}

func blockerManifestAssignment(packet workpacket.RepairResultPacket) (map[string]any, error) {
	var assignment map[string]any
	if err := json.Unmarshal(packet.Inputs.Sources.ManifestAssignment, &assignment); err != nil || assignment == nil {
		return nil, errors.New("S9 blocker packet has no valid frozen manifest Assignment")
	}
	if stringField(assignment["agent_definition_ref"]) == "" {
		return nil, errors.New("S9 blocker frozen manifest Assignment does not name the issued owner/Agent definition")
	}
	return assignment, nil
}

func blockerMessageID(packetID, answerSHA string) string {
	return "msg-s9-blocker-" + strings.TrimPrefix(packetID, "packet-") + "-" + answerSHA
}

func blockerCorrelationID(packetID string) string {
	return "corr-s9-blocker-" + strings.TrimPrefix(packetID, "packet-")
}

func blockerRequestPathMatches(root, ref, supplied string) bool {
	if supplied == ref {
		return true
	}
	want, err := repositoryPath(root, ref)
	if err != nil || !filepath.IsAbs(supplied) {
		return false
	}
	got, err := filepath.Abs(supplied)
	if err != nil {
		return false
	}
	return filepath.Clean(got) == filepath.Clean(want)
}

func writeBlockerImmutable(root, directory, schemaName string, document any) (ArtifactRef, []byte, error) {
	data, err := canonicalJSON(document)
	if err != nil {
		return ArtifactRef{}, nil, err
	}
	limit := int64(20 << 20)
	if schemaName == "agent-message.schema.json" {
		limit = 1 << 20
	}
	ref := fileRef(path.Join(directory, sha256Bytes(data)+".json"), data)
	stored, err := workpacket.PublishBounded(root, ref.Path, data, limit)
	if err != nil {
		return ArtifactRef{}, nil, err
	}
	if !reflect.DeepEqual(stored, data) {
		return ArtifactRef{}, nil, runtimepkg.ErrSubmissionConflict
	}
	return ref, stored, nil
}
