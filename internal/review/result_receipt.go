package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"path/filepath"
	"reflect"
	"strings"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const resultReceiptContract = "s7-review-result-receipt-v1"
const resultReceiptReadLimit int64 = 20 << 20

// ResultReceipt is the durable replay proof written beside the consumed S7
// Assignment in the same Runtime CAS as its Result evidence and dispositions.
// It contains only coordinates needed to re-verify the original acceptance;
// the immutable answer itself remains in ResultRef.
type ResultReceipt struct {
	Contract                   string  `json:"contract"`
	SubmissionKey              string  `json:"submission_key"`
	RuntimeID                  string  `json:"runtime_id"`
	BaselineGeneration         int     `json:"baseline_generation"`
	ReviewPlanID               string  `json:"review_plan_id"`
	ReviewPlanRef              string  `json:"review_plan_ref"`
	ReviewPlanSHA256           string  `json:"review_plan_sha256"`
	ReviewPlanRevision         int     `json:"review_plan_revision"`
	ReviewRound                int     `json:"review_round"`
	AssignmentID               string  `json:"assignment_id"`
	AssignmentRevision         int     `json:"assignment_revision"`
	AssignmentSHA256           string  `json:"assignment_sha256"`
	ProducerAgentID            string  `json:"producer_agent_id"`
	SubjectDigest              string  `json:"subject_digest"`
	VerificationArtifactDigest *string `json:"verification_artifact_digest"`
	AnswerSHA256               string  `json:"answer_sha256"`
	EffectiveAnswerSHA256      string  `json:"effective_answer_sha256"`
	ResultRef                  string  `json:"result_ref"`
	ResultSHA256               string  `json:"result_sha256"`
	EventID                    string  `json:"event_id"`
	IdempotencyKey             string  `json:"idempotency_key"`
	AcceptedAfterRevision      int     `json:"accepted_after_revision"`
	AcceptedSequence           int     `json:"accepted_sequence"`
	PacketID                   string  `json:"packet_id,omitempty"`
	PacketSHA256               string  `json:"packet_sha256,omitempty"`
	ExecutionRoot              string  `json:"execution_root,omitempty"`
}

var errExactResultReplay = errors.New("exact ReviewResult replay")

func typedResultReceiptsEnabled(state map[string]any) bool {
	return workstate.SupportsAssignments(state["schema_version"]) && loopruntime.PacketEnabled(state)
}

// professionalAnswerDigest fingerprints only answer fields. Machine tuple
// fields are independently closed by ResultReceipt and remain in the complete
// submitted/effective answers stored in the immutable envelope.
func professionalAnswerDigest(data []byte, issued ...bool) (string, error) {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return "", fmt.Errorf("validate professional answer JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return "", fmt.Errorf("decode professional answer: %w", err)
	}
	answer := make(map[string]any, 7)
	for _, key := range []string{"claim_results", "blocked_claims", "checks", "findings", "deviations", "site_lost", "verdict"} {
		if value, ok := object[key]; ok {
			answer[key] = value
		}
	}
	if len(issued) > 0 && issued[0] {
		answer["verification_artifact_digest"] = object["verification_artifact_digest"]
	}
	canonical, err := json.Marshal(answer)
	if err != nil {
		return "", fmt.Errorf("canonicalize professional answer: %w", err)
	}
	return sha256Of(canonical), nil
}

func marshalEffectiveResult(result Result, issued ...bool) ([]byte, string, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, "", fmt.Errorf("encode effective ReviewResult: %w", err)
	}
	digest, err := professionalAnswerDigest(data, issued...)
	if err != nil {
		return nil, "", err
	}
	return data, digest, nil
}

func receiptEventIdentity(runtimeID string, generation int, ptr *PlanPointer, result *Result, assignmentSHA string, packetBinding ...string) (string, string) {
	identity, _ := json.Marshal(struct {
		Contract                   string `json:"contract"`
		RuntimeID                  string `json:"runtime_id"`
		Generation                 int    `json:"generation"`
		PlanID                     string `json:"plan_id"`
		PlanRevision               int    `json:"plan_revision"`
		PlanSHA256                 string `json:"plan_sha256"`
		Round                      int    `json:"round"`
		AssignmentID               string `json:"assignment_id"`
		AssignmentRevision         int    `json:"assignment_revision"`
		AssignmentSHA              string `json:"assignment_sha256"`
		Owner                      string `json:"owner"`
		SubjectDigest              string `json:"subject_digest"`
		VerificationArtifactDigest string `json:"verification_artifact_digest"`
		SubmissionKey              string `json:"submission_key"`
	}{resultReceiptContract, runtimeID, generation, ptr.PlanID, ptr.Revision, ptr.SHA256, result.ReviewRound, result.AssignmentID, result.AssignmentRevision, assignmentSHA, result.ProducerAgentID, result.SubjectDigest, optionalStringIdentity(result.VerificationArtifactDigest), result.ResultID})
	if len(packetBinding) > 0 {
		identity, _ = json.Marshal(struct {
			Original      json.RawMessage `json:"original"`
			PacketID      string          `json:"packet_id"`
			PacketSHA256  string          `json:"packet_sha256"`
			ExecutionRoot string          `json:"execution_root"`
		}{identity, packetBinding[0], packetBinding[1], packetBinding[2]})
	}
	keyDigest := sha256Of(identity)
	return "evt-review-result-" + keyDigest[:32], "runtime:review-result:" + keyDigest
}

func newResultReceipt(root string, state map[string]any, plan *Plan, ptr *PlanPointer, assignment *PlanAssignment, result *Result, answerSHA, effectiveSHA, resultRef, resultSHA string, packet *workpacket.ReviewResultPacket, executionRoot string) (ResultReceipt, error) {
	runtimeID := stringField(state["runtime_id"])
	planRef := filepath.ToSlash(filepath.Clean(filepath.FromSlash(ptr.Path)))
	if filepath.IsAbs(filepath.FromSlash(ptr.Path)) {
		planRef = repositoryPath(root, ptr.Path)
	}
	if planRef == "" || filepath.IsAbs(filepath.FromSlash(planRef)) || strings.HasPrefix(planRef, "../") || planRef == ".." {
		return ResultReceipt{}, fmt.Errorf("typed ReviewResult receipt requires a repository-relative pinned ReviewPlan ref")
	}
	if _, err := repositoryContainedPath(root, planRef); err != nil {
		return ResultReceipt{}, fmt.Errorf("typed ReviewResult pinned ReviewPlan ref: %w", err)
	}
	assignmentSHA, err := planAssignmentDigest(assignment)
	if err != nil {
		return ResultReceipt{}, err
	}
	eventID, idem := receiptEventIdentity(runtimeID, result.BaselineGeneration, ptr, result, assignmentSHA)
	packetID, packetSHA := "", ""
	if packet != nil {
		registeredRoot, _, sourceErr := registeredResultPacketSourceRoot(root, state, *packet, false)
		if sourceErr != nil {
			return ResultReceipt{}, fmt.Errorf("resolve registered S7 execution root for receipt: %w", sourceErr)
		}
		if executionRoot == "" || executionRoot != registeredRoot {
			return ResultReceipt{}, errors.New("S7 receipt execution root differs from the current registered source")
		}
		packetID = packet.ID
		packetSHA, err = workpacket.ReviewResultPacketDigest(*packet)
		if err != nil {
			return ResultReceipt{}, err
		}
		identityPlan := &PlanPointer{PlanID: ptr.PlanID, Path: planRef, SHA256: ptr.SHA256, Revision: ptr.Revision}
		eventID, idem = receiptEventIdentity(runtimeID, result.BaselineGeneration, identityPlan, result, assignmentSHA, packetID, packetSHA, executionRoot)
	}
	journal, _ := state["journal"].(map[string]any)
	revision := intField(state["revision"]) + 1
	sequence := intField(journal["last_sequence"]) + 1
	return ResultReceipt{
		Contract: resultReceiptContract, SubmissionKey: result.ResultID,
		RuntimeID: runtimeID, BaselineGeneration: result.BaselineGeneration,
		ReviewPlanID: plan.ReviewPlanID, ReviewPlanRef: planRef, ReviewPlanSHA256: ptr.SHA256,
		ReviewPlanRevision: ptr.Revision, ReviewRound: result.ReviewRound,
		AssignmentID: result.AssignmentID, AssignmentRevision: result.AssignmentRevision,
		AssignmentSHA256: assignmentSHA, ProducerAgentID: result.ProducerAgentID,
		SubjectDigest: result.SubjectDigest, VerificationArtifactDigest: cloneStringPtr(result.VerificationArtifactDigest),
		AnswerSHA256: answerSHA, EffectiveAnswerSHA256: effectiveSHA,
		ResultRef: resultRef, ResultSHA256: resultSHA,
		EventID: eventID, IdempotencyKey: idem,
		AcceptedAfterRevision: revision, AcceptedSequence: sequence,
		PacketID: packetID, PacketSHA256: packetSHA, ExecutionRoot: executionRoot,
	}, nil
}

func planAssignmentDigest(assignment *PlanAssignment) (string, error) {
	if assignment == nil {
		return "", errors.New("ReviewPlan assignment is missing")
	}
	data, err := json.Marshal(assignment)
	if err != nil {
		return "", fmt.Errorf("encode ReviewPlan assignment: %w", err)
	}
	return sha256Of(data), nil
}

func verifyTypedSubmissionStillCurrent(root string, state map[string]any, plan *Plan, ptr *PlanPointer, assignment *PlanAssignment, result *Result) error {
	if !typedResultReceiptsEnabled(state) {
		return errors.New("Runtime left typed ReviewResult receipt mode before commit")
	}
	currentPlan, currentPtr, err := loadReceiptPlan(root, state)
	if err != nil {
		return fmt.Errorf("re-read current ReviewPlan under Runtime lock: %w", err)
	}
	if currentPtr.PlanID != ptr.PlanID || currentPtr.Path != ptr.Path || currentPtr.SHA256 != ptr.SHA256 || currentPtr.Revision != ptr.Revision || currentPlan.ReviewRound != plan.ReviewRound || currentPlan.BaselineGeneration != plan.BaselineGeneration || SubjectDigest(currentPlan) != SubjectDigest(plan) {
		return errors.New("ReviewPlan changed before typed ReviewResult commit")
	}
	currentAssignment := findPlanAssignment(currentPlan, assignment.AssignmentID)
	currentSHA, err := planAssignmentDigest(currentAssignment)
	if err != nil {
		return err
	}
	originalSHA, err := planAssignmentDigest(assignment)
	if err != nil || currentSHA != originalSHA {
		return errors.New("ReviewPlan Assignment changed before typed ReviewResult commit")
	}
	if result.ReviewPlanID != currentPtr.PlanID || result.AssignmentRevision != currentPtr.Revision || result.ReviewRound != currentPlan.ReviewRound || result.BaselineGeneration != currentPlan.BaselineGeneration || result.SubjectDigest != SubjectDigest(currentPlan) {
		return errors.New("typed ReviewResult tuple changed before commit")
	}
	reviewMap, _ := state["review"].(map[string]any)
	rows, _ := reviewMap["assignments"].(map[string]any)
	row, _ := rows[assignment.AssignmentID].(map[string]any)
	if row == nil || (workstate.ReviewAssignmentActivity(row) != "dispatched" && workstate.ReviewAssignmentActivity(row) != "blocked") || row["agent_id"] != result.ProducerAgentID {
		return errors.New("typed ReviewResult Assignment owner or status changed before commit")
	}
	return nil
}

// loadReceiptPlan reads a Runtime-pinned ReviewPlan with the same bounded,
// symlink-resistant reader used for durable receipt artifacts.
func loadReceiptPlan(root string, state map[string]any) (*Plan, *PlanPointer, error) {
	ptr := PlanPointerFromState(state)
	if ptr == nil {
		return nil, nil, errors.New("no ReviewPlan is registered for the current review round")
	}
	rel := ptr.Path
	if filepath.IsAbs(filepath.FromSlash(rel)) {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve ReviewPlan root: %w", err)
		}
		planAbs, err := filepath.Abs(filepath.FromSlash(rel))
		if err != nil {
			return nil, nil, fmt.Errorf("resolve ReviewPlan path: %w", err)
		}
		rel, err = filepath.Rel(rootAbs, planAbs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return nil, nil, errors.New("pinned ReviewPlan path is outside the repository")
		}
	}
	rel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if filepath.IsAbs(filepath.FromSlash(rel)) || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, nil, errors.New("pinned ReviewPlan path is not repository-relative")
	}
	data, err := workpacket.ReadFile(root, rel, resultReceiptReadLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("read ReviewPlan %s: %w", ptr.Path, err)
	}
	if sha256Of(data) != ptr.SHA256 {
		return nil, nil, fmt.Errorf("ReviewPlan %s drifted: pinned sha256 %s does not match the file on disk", ptr.Path, ptr.SHA256)
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, nil, fmt.Errorf("decode ReviewPlan %s: %w", ptr.Path, err)
	}
	if plan.ReviewPlanID != ptr.PlanID {
		return nil, nil, fmt.Errorf("ReviewPlan id mismatch: state pins %s but the file declares %s", ptr.PlanID, plan.ReviewPlanID)
	}
	return &plan, ptr, nil
}

func stateArtifactReferences(state map[string]any, candidate string) []string {
	if containsStringValue(state, candidate) {
		return []string{candidate}
	}
	return nil
}

func removeStagedArtifactIfUnreferenced(store *loopruntime.Store, state map[string]any, expectedRevision int, relative, sha256 string) (bool, error) {
	if store == nil {
		return false, errors.New("Runtime writer is required for ReviewResult artifact cleanup")
	}
	return store.RemoveUnreferencedArtifact(loopruntime.ArtifactCleanupRequest{
		ExpectedRevision: expectedRevision,
		ArtifactPath:     relative,
		ArtifactSHA256:   sha256,
		ReferencedPaths:  stateArtifactReferences(state, relative),
	})
}

func containsStringValue(value any, expected string) bool {
	switch typed := value.(type) {
	case string:
		return typed == expected
	case map[string]any:
		for _, child := range typed {
			if containsStringValue(child, expected) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsStringValue(child, expected) {
				return true
			}
		}
	}
	return false
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func setAssignmentResultReceipt(state map[string]any, assignmentID string, receipt ResultReceipt) error {
	reviewMap, _ := state["review"].(map[string]any)
	assignments, _ := reviewMap["assignments"].(map[string]any)
	row, _ := assignments[assignmentID].(map[string]any)
	if row == nil {
		return fmt.Errorf("assignment %s is missing while recording its ReviewResult receipt", assignmentID)
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("encode ReviewResult receipt for Runtime state: %w", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return fmt.Errorf("normalize ReviewResult receipt for Runtime state: %w", err)
	}
	row["result_receipt"] = normalized
	return nil
}

// replayAcceptedResult proves an already-consumed v1.2 packet result from its
// current assignment row, pinned Plan bytes, Runtime evidence index, immutable
// artifacts, and the original committed journal event. It never reads a live
// capture buffer or changes Runtime state.
func replayAcceptedResult(root string, state map[string]any, incoming *Result, answerSHA string, contexts ...context.Context) (bool, error) {
	if !typedResultReceiptsEnabled(state) {
		return false, nil
	}
	reviewMap, _ := state["review"].(map[string]any)
	assignments, _ := reviewMap["assignments"].(map[string]any)
	row, _ := assignments[incoming.AssignmentID].(map[string]any)
	if row == nil {
		return false, nil
	}
	status := workstate.ReviewAssignmentActivity(row)
	if status != "consumed" && status != "result_submitted" {
		return false, nil
	}
	rawReceipt, exists := row["result_receipt"]
	if !exists || rawReceipt == nil {
		return false, fmt.Errorf("assignment %s already consumed a legacy receiptless ReviewResult; exact replay is unsupported and a new result cannot replace it", incoming.AssignmentID)
	}
	encoded, err := json.Marshal(rawReceipt)
	if err != nil {
		return false, fmt.Errorf("encode ReviewResult receipt: %w", err)
	}
	var receipt ResultReceipt
	if err := json.Unmarshal(encoded, &receipt); err != nil {
		return false, fmt.Errorf("decode ReviewResult receipt: %w", err)
	}
	if receipt.Contract != resultReceiptContract || receipt.SubmissionKey == "" {
		return false, fmt.Errorf("assignment %s has an unsupported or malformed ReviewResult receipt", incoming.AssignmentID)
	}
	if receipt.SubmissionKey != incoming.ResultID {
		return false, fmt.Errorf("assignment %s already consumed result %s; a new result_id cannot replace it", incoming.AssignmentID, receipt.SubmissionKey)
	}
	if receipt.AnswerSHA256 != answerSHA {
		return false, fmt.Errorf("ReviewResult submission key %s conflicts with the originally accepted professional answer", incoming.ResultID)
	}
	if err := verifyResultReceiptClosure(root, state, row, receipt, incoming, contexts...); err != nil {
		return false, fmt.Errorf("ReviewResult receipt %s is not replayable: %w", incoming.ResultID, err)
	}
	return true, nil
}

func verifyResultReceiptClosure(root string, state, assignmentRow map[string]any, receipt ResultReceipt, incoming *Result, contexts ...context.Context) error {
	ctx := loopruntime.WithPacketBodyRoot(reviewRequestContext(contexts), root)
	if err := ctx.Err(); err != nil {
		return err
	}
	if receipt.PacketID != "" || receipt.PacketSHA256 != "" {
		p, found, err := loopruntime.FindReviewResultPacketContext(ctx, state, receipt.PacketID)
		if err != nil {
			return fmt.Errorf("S7 receipt original packet: %w", err)
		}
		if !found {
			return errors.New("S7 receipt lacks original packet")
		}
		digest, err := workpacket.ReviewResultPacketDigest(p)
		if err != nil || digest != receipt.PacketSHA256 {
			return errors.New("S7 receipt packet digest differs")
		}
		if err := loopruntime.VerifyReviewResultPacketIssueContext(ctx, root, state, p); err != nil {
			return err
		}
		b := p.Binding
		if p.ResultID != receipt.SubmissionKey || b.RuntimeID != receipt.RuntimeID || b.BaselineGeneration != receipt.BaselineGeneration || b.ReviewPlanID != receipt.ReviewPlanID || b.ReviewPlanRef != receipt.ReviewPlanRef || b.ReviewPlanSHA256 != receipt.ReviewPlanSHA256 || b.ReviewPlanRevision != receipt.ReviewPlanRevision || b.ReviewRound != receipt.ReviewRound || b.AssignmentID != receipt.AssignmentID || b.AssignmentSHA256 != receipt.AssignmentSHA256 || b.OwnerAgentID != receipt.ProducerAgentID || b.SubjectDigest != receipt.SubjectDigest || (b.VerificationArtifactDigest != nil && !equalOptionalString(b.VerificationArtifactDigest, receipt.VerificationArtifactDigest)) {
			return errors.New("S7 receipt tuple differs from issued packet")
		}
		registeredRoot, _, sourceErr := registeredResultPacketSourceRoot(root, state, p, false)
		if sourceErr != nil || receipt.ExecutionRoot == "" || registeredRoot != receipt.ExecutionRoot {
			return errors.New("S7 receipt execution root differs from the current registered source")
		}
	} else if receipt.ExecutionRoot != "" {
		return errors.New("legacy ReviewResult receipt unexpectedly declares an execution root")
	}
	runtimeID := stringField(state["runtime_id"])
	if receipt.RuntimeID != runtimeID || receipt.BaselineGeneration != baselineGeneration(state) || receipt.SubmissionKey != incoming.ResultID || receipt.AssignmentID != incoming.AssignmentID || receipt.ProducerAgentID != incoming.ProducerAgentID || receipt.ReviewRound != incoming.ReviewRound || receipt.BaselineGeneration != incoming.BaselineGeneration || receipt.AssignmentRevision != incoming.AssignmentRevision || receipt.ReviewPlanID != incoming.ReviewPlanID || receipt.SubjectDigest != incoming.SubjectDigest || !equalOptionalString(receipt.VerificationArtifactDigest, incoming.VerificationArtifactDigest) {
		return errors.New("incoming result tuple differs from the accepted receipt")
	}
	if stringField(assignmentRow["result_ref"]) != receipt.ResultRef || stringField(assignmentRow["agent_id"]) != receipt.ProducerAgentID {
		return errors.New("current Assignment result ref or dispatched owner differs from the receipt")
	}
	if receipt.ResultRef == "" || receipt.ResultSHA256 == "" || receipt.EventID == "" || receipt.IdempotencyKey == "" || receipt.AcceptedAfterRevision < 1 || receipt.AcceptedSequence < 1 {
		return errors.New("receipt is missing original acceptance coordinates")
	}

	currentPlan, currentPtr, err := loadReceiptPlan(root, state)
	if err != nil {
		return fmt.Errorf("current ReviewPlan: %w", err)
	}
	if currentPtr.PlanID != receipt.ReviewPlanID || currentPlan.ReviewRound != receipt.ReviewRound || currentPlan.BaselineGeneration != receipt.BaselineGeneration || SubjectDigest(currentPlan) != receipt.SubjectDigest || currentPtr.Revision < receipt.ReviewPlanRevision {
		return errors.New("current ReviewPlan no longer preserves the accepted Assignment tuple")
	}
	currentAssignment := findPlanAssignment(currentPlan, receipt.AssignmentID)
	currentAssignmentSHA, err := planAssignmentDigest(currentAssignment)
	if err != nil || currentAssignmentSHA != receipt.AssignmentSHA256 {
		return errors.New("current ReviewPlan changed the accepted Assignment")
	}
	originalPlanBytes, err := workpacket.ReadFile(root, receipt.ReviewPlanRef, resultReceiptReadLimit)
	if err != nil {
		return fmt.Errorf("read original pinned ReviewPlan: %w", err)
	}
	if sha256Of(originalPlanBytes) != receipt.ReviewPlanSHA256 {
		return errors.New("original pinned ReviewPlan bytes no longer match the receipt")
	}
	var originalPlan Plan
	if err := json.Unmarshal(originalPlanBytes, &originalPlan); err != nil {
		return fmt.Errorf("decode original pinned ReviewPlan: %w", err)
	}
	if err := ValidatePlan(&originalPlan); err != nil {
		return fmt.Errorf("validate original pinned ReviewPlan: %w", err)
	}
	if originalPlan.ReviewPlanID != receipt.ReviewPlanID || originalPlan.ReviewRound != receipt.ReviewRound || originalPlan.BaselineGeneration != receipt.BaselineGeneration {
		return errors.New("original pinned ReviewPlan identity or round differs from the receipt")
	}
	originalAssignment := findPlanAssignment(&originalPlan, receipt.AssignmentID)
	originalAssignmentSHA, err := planAssignmentDigest(originalAssignment)
	if err != nil || originalAssignmentSHA != receipt.AssignmentSHA256 || SubjectDigest(&originalPlan) != receipt.SubjectDigest {
		return errors.New("original pinned ReviewPlan does not prove the receipt Assignment and subject tuple")
	}
	identityPlan := &PlanPointer{PlanID: receipt.ReviewPlanID, Path: receipt.ReviewPlanRef, SHA256: receipt.ReviewPlanSHA256, Revision: receipt.ReviewPlanRevision}
	wantEventID, wantIdempotencyKey := receiptEventIdentity(receipt.RuntimeID, receipt.BaselineGeneration, identityPlan, incoming, receipt.AssignmentSHA256)
	if receipt.PacketID != "" {
		wantEventID, wantIdempotencyKey = receiptEventIdentity(receipt.RuntimeID, receipt.BaselineGeneration, identityPlan, incoming, receipt.AssignmentSHA256, receipt.PacketID, receipt.PacketSHA256, receipt.ExecutionRoot)
	}
	if originalAssignment == nil || receipt.EventID != wantEventID || receipt.IdempotencyKey != wantIdempotencyKey {
		return errors.New("ReviewResult receipt event identity does not bind its immutable source tuple")
	}

	evidence, err := uniqueEvidenceByID(state, receipt.SubmissionKey)
	if err != nil {
		return err
	}
	if evidence["kind"] != "review_result" || evidence["status"] != "valid" || !evidenceInvalidationClear(evidence) || evidence["path"] != receipt.ResultRef || evidence["sha256"] != receipt.ResultSHA256 || intField(evidence["baseline_generation"]) != receipt.BaselineGeneration || intField(evidence["review_round"]) != receipt.ReviewRound || !sameStringSlice(evidence["produced_by"], receipt.ProducerAgentID) {
		return errors.New("Result evidence index differs from the receipt")
	}
	resultBytes, err := workpacket.ReadFile(root, receipt.ResultRef, resultReceiptReadLimit)
	if err != nil {
		return fmt.Errorf("read original Result artifact: %w", err)
	}
	if sha256Of(resultBytes) != receipt.ResultSHA256 {
		return errors.New("Result artifact SHA-256 differs from the receipt")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(resultBytes, &envelope); err != nil {
		return fmt.Errorf("decode original Result artifact: %w", err)
	}
	if stringFieldFromRaw(envelope["packet_id"]) != receipt.PacketID || stringFieldFromRaw(envelope["packet_sha256"]) != receipt.PacketSHA256 {
		return errors.New("Result artifact issuance differs from receipt")
	}
	if receipt.PacketID != "" {
		if stringFieldFromRaw(envelope["execution_root"]) != receipt.ExecutionRoot {
			return errors.New("Result artifact execution root differs from receipt")
		}
	} else if len(envelope["execution_root"]) != 0 {
		return errors.New("legacy Result artifact unexpectedly declares an execution root")
	}
	var submittedRaw, effectiveRaw json.RawMessage
	if err := json.Unmarshal(envelope["submitted_answer"], &submittedRaw); err != nil || len(submittedRaw) == 0 {
		return errors.New("Result artifact has no complete submitted_answer")
	}
	if err := json.Unmarshal(envelope["effective_answer"], &effectiveRaw); err != nil || len(effectiveRaw) == 0 {
		return errors.New("Result artifact has no complete effective_answer")
	}
	submittedSHA, err := professionalAnswerDigest(submittedRaw, receipt.PacketID != "")
	if err != nil || submittedSHA != receipt.AnswerSHA256 || stringFieldFromRaw(envelope["answer_sha256"]) != receipt.AnswerSHA256 {
		return errors.New("submitted answer digest differs from the receipt")
	}
	effectiveSHA, err := professionalAnswerDigest(effectiveRaw, receipt.PacketID != "")
	if err != nil || effectiveSHA != receipt.EffectiveAnswerSHA256 || stringFieldFromRaw(envelope["effective_answer_sha256"]) != receipt.EffectiveAnswerSHA256 {
		return errors.New("effective answer digest differs from the receipt")
	}
	if err := verifyAnswerMatchesReceipt(submittedRaw, receipt); err != nil {
		return err
	}
	if err := schema.NewValidator(root).ValidateBytes("review-result.schema.json", submittedRaw); err != nil {
		return fmt.Errorf("validate original submitted answer: %w", err)
	}
	if err := schema.NewValidator(root).ValidateBytes("review-result.schema.json", effectiveRaw); err != nil {
		return fmt.Errorf("validate original effective answer: %w", err)
	}
	if err := verifyAnswerMatchesReceipt(effectiveRaw, receipt); err != nil {
		return err
	}
	var effective Result
	if err := json.Unmarshal(effectiveRaw, &effective); err != nil {
		return fmt.Errorf("decode effective accepted ReviewResult: %w", err)
	}
	if err := verifyResultEnvelopeProjection(envelope, effectiveRaw, &effective, receipt, originalPlan); err != nil {
		return err
	}
	if err := verifyStoredFindingClosure(root, state, receipt, &effective); err != nil {
		return err
	}
	if err := verifyReceiptJournal(root, state, receipt, &effective); err != nil {
		return err
	}
	return nil
}

func verifyAnswerMatchesReceipt(raw json.RawMessage, receipt ResultReceipt) error {
	var submitted Result
	if err := json.Unmarshal(raw, &submitted); err != nil {
		return fmt.Errorf("decode submitted answer: %w", err)
	}
	if submitted.ResultID != receipt.SubmissionKey || submitted.AssignmentID != receipt.AssignmentID || submitted.AssignmentRevision != receipt.AssignmentRevision || submitted.ReviewPlanID != receipt.ReviewPlanID || submitted.ReviewRound != receipt.ReviewRound || submitted.BaselineGeneration != receipt.BaselineGeneration || submitted.ProducerAgentID != receipt.ProducerAgentID || submitted.SubjectDigest != receipt.SubjectDigest || !equalOptionalString(receipt.VerificationArtifactDigest, submitted.VerificationArtifactDigest) {
		return errors.New("submitted answer machine tuple differs from the receipt")
	}
	return nil
}

func verifyResultEnvelopeProjection(envelope map[string]json.RawMessage, effectiveRaw json.RawMessage, effective *Result, receipt ResultReceipt, originalPlan Plan) error {
	var decoded map[string]any
	if err := json.Unmarshal(mustMarshalRawMap(envelope), &decoded); err != nil {
		return fmt.Errorf("decode Result envelope projection: %w", err)
	}
	for key, expected := range map[string]any{
		"evidence_id": receipt.SubmissionKey, "kind": "review_result", "runtime_id": receipt.RuntimeID,
		"baseline_generation": receipt.BaselineGeneration, "review_round": receipt.ReviewRound,
		"producer_agent_id": receipt.ProducerAgentID, "review_plan_id": receipt.ReviewPlanID,
		"assignment_id": receipt.AssignmentID, "assignment_revision": receipt.AssignmentRevision,
		"subject_digest": receipt.SubjectDigest, "conclusion": effective.Verdict, "verdict": effective.Verdict,
	} {
		if !jsonValueEqual(decoded[key], expected) {
			return fmt.Errorf("Result envelope projection field %s differs from its receipt or effective answer", key)
		}
	}
	if !equalOptionalString(receipt.VerificationArtifactDigest, effective.VerificationArtifactDigest) || !jsonValueEqual(decoded["verification_artifact_digest"], optionalStringValue(receipt.VerificationArtifactDigest)) {
		return errors.New("Result envelope verification artifact digest differs from its receipt")
	}
	assignment := findPlanAssignment(&originalPlan, receipt.AssignmentID)
	if assignment == nil || decoded["producer_responsibility"] != LensToResponsibility(assignment.Lens) {
		return errors.New("Result envelope producer responsibility differs from its original Assignment")
	}
	var effectiveObject map[string]json.RawMessage
	if err := json.Unmarshal(effectiveRaw, &effectiveObject); err != nil {
		return fmt.Errorf("decode effective answer projection: %w", err)
	}
	for _, key := range []string{"claim_results", "blocked_claims", "checks", "deviations"} {
		expected := effectiveObject[key]
		if len(expected) == 0 {
			if key == "blocked_claims" {
				expected = json.RawMessage("[]")
			} else {
				expected = json.RawMessage("null")
			}
		}
		if !rawJSONEqual(envelope[key], expected) {
			return fmt.Errorf("Result envelope answer projection %s differs from effective_answer", key)
		}
	}
	return nil
}

func mustMarshalRawMap(value map[string]json.RawMessage) []byte {
	data, _ := json.Marshal(value)
	return data
}

func jsonValueEqual(value, expected any) bool {
	left, leftErr := json.Marshal(value)
	right, rightErr := json.Marshal(expected)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

func rawJSONEqual(left, right json.RawMessage) bool {
	canonical := func(raw json.RawMessage) []byte {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) != nil {
			return nil
		}
		encoded, _ := json.Marshal(value)
		return encoded
	}
	leftCanonical, rightCanonical := canonical(left), canonical(right)
	return leftCanonical != nil && rightCanonical != nil && bytes.Equal(leftCanonical, rightCanonical)
}

func optionalStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalStringIdentity(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func verifyStoredFindingClosure(root string, state map[string]any, receipt ResultReceipt, result *Result) error {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["findings"].([]any)
	for _, finding := range result.Findings {
		entry, err := uniqueEvidenceByID(state, finding.FindingID)
		if err != nil {
			return err
		}
		ref := filepath.ToSlash(filepath.Join(".claude", "evidence", receipt.RuntimeID, fmt.Sprintf("g%d", receipt.BaselineGeneration), "findings", finding.FindingID+".json"))
		if entry["kind"] != "finding" || entry["status"] != "valid" || !evidenceInvalidationClear(entry) || entry["path"] != ref || intField(entry["baseline_generation"]) != receipt.BaselineGeneration || intField(entry["review_round"]) != receipt.ReviewRound || !sameStringSlice(entry["produced_by"], receipt.ProducerAgentID) {
			return fmt.Errorf("Finding %s evidence index differs from the accepted answer", finding.FindingID)
		}
		data, err := workpacket.ReadFile(root, ref, resultReceiptReadLimit)
		if err != nil {
			return fmt.Errorf("read Finding %s artifact: %w", finding.FindingID, err)
		}
		if sha256Of(data) != stringField(entry["sha256"]) {
			return fmt.Errorf("Finding %s artifact SHA-256 differs from its index", finding.FindingID)
		}
		var stored Finding
		if err := json.Unmarshal(data, &stored); err != nil || !reflect.DeepEqual(stored, finding) {
			return fmt.Errorf("Finding %s artifact differs from the accepted answer", finding.FindingID)
		}
		found := 0
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			if row != nil && row["finding_id"] == finding.FindingID {
				found++
				if row["path"] != ref || row["sha256"] != entry["sha256"] || row["assignment_id"] != receipt.AssignmentID || row["original_finder"] != receipt.ProducerAgentID || intField(row["review_round"]) != receipt.ReviewRound {
					return fmt.Errorf("Finding %s entity projection differs from its evidence", finding.FindingID)
				}
			}
		}
		if found != 1 {
			return fmt.Errorf("Finding %s entity projection is missing or duplicated", finding.FindingID)
		}
	}
	return nil
}

func evidenceInvalidationClear(evidence map[string]any) bool {
	for _, key := range []string{"invalidated_by", "invalidation_rule", "invalidation_reason"} {
		value, exists := evidence[key]
		if !exists || value != nil {
			return false
		}
	}
	return true
}

func verifyReceiptJournal(root string, state map[string]any, receipt ResultReceipt, result *Result) error {
	journal, err := loopruntime.ReadJournalForState(root, state)
	if err != nil {
		return fmt.Errorf("read complete original Runtime journal: %w", err)
	}
	var found map[string]any
	for _, line := range bytes.Split(journal, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			return fmt.Errorf("decode original Runtime journal: %w", err)
		}
		if event["event_id"] == receipt.EventID {
			if found != nil {
				return errors.New("original result event id is duplicated")
			}
			found = event
		}
	}
	if found == nil {
		return errors.New("original committed ReviewResult event is absent from the journal")
	}
	actor, _ := found["actor"].(map[string]any)
	if found["runtime_id"] != receipt.RuntimeID || found["event"] != "transition_committed" || found["outcome"] != "committed" || found["transition_id"] != "REVIEW-RESULT" || found["idempotency_key"] != receipt.IdempotencyKey || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" || intField(found["baseline_generation"]) != receipt.BaselineGeneration || intField(found["after_revision"]) != receipt.AcceptedAfterRevision || intField(found["before_revision"])+1 != receipt.AcceptedAfterRevision || intField(found["sequence"]) != receipt.AcceptedSequence {
		return errors.New("original journal event identity or commit coordinates differ from the receipt")
	}
	wantEvidence := append([]string{receipt.SubmissionKey}, findingIDs(result.Findings)...)
	if !sameStringListValue(found["evidence_ids"], wantEvidence) {
		return errors.New("original journal event evidence IDs differ from the accepted Result and Findings")
	}
	return nil
}

func uniqueEvidenceByID(state map[string]any, id string) (map[string]any, error) {
	rows, _ := state["evidence"].([]any)
	var found map[string]any
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["id"] == id {
			if found != nil {
				return nil, fmt.Errorf("evidence ID %s is duplicated", id)
			}
			found = row
		}
	}
	if found == nil {
		return nil, fmt.Errorf("evidence ID %s is missing", id)
	}
	return found, nil
}

func sameStringSlice(raw any, expected string) bool {
	values, ok := raw.([]any)
	return ok && len(values) == 1 && values[0] == expected
}

func sameStringListValue(raw any, expected []string) bool {
	values, ok := raw.([]any)
	if !ok || len(values) != len(expected) {
		return false
	}
	for i := range expected {
		if values[i] != expected[i] {
			return false
		}
	}
	return true
}

func stringFieldFromRaw(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
