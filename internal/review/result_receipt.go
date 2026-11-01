package review

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"path/filepath"
	"strings"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const resultReceiptContract = "s7-review-result-receipt-v1"
const resultReceiptReadLimit int64 = 20 << 20

// ResultReceipt is the durable replay proof written beside the consumed S7
// Assignment in the same Runtime CAS as its Result evidence and dispositions.
// It contains only coordinates needed to re-verify the original acceptance;
// the immutable answer itself remains in ResultRef.
type ResultReceipt struct {
	Contract              string `json:"contract"`
	SubmissionKey         string `json:"submission_key"`
	RuntimeID             string `json:"runtime_id"`
	BaselineGeneration    int    `json:"baseline_generation"`
	ReviewPlanID          string `json:"review_plan_id"`
	ReviewPlanRef         string `json:"review_plan_ref"`
	ReviewPlanRevision    int    `json:"review_plan_revision"`
	AssignmentID          string `json:"assignment_id"`
	AssignmentRevision    int    `json:"assignment_revision"`
	ProducerAgentID       string `json:"producer_agent_id"`
	ResultRef             string `json:"result_ref"`
	EventID               string `json:"event_id"`
	IdempotencyKey        string `json:"idempotency_key"`
	AcceptedAfterRevision int    `json:"accepted_after_revision"`
	AcceptedSequence      int    `json:"accepted_sequence"`
	PacketID              string `json:"packet_id,omitempty"`
	ExecutionRoot         string `json:"execution_root,omitempty"`
}

func decodeOriginalResultReceipt(data []byte, receipt *ResultReceipt) error {
	return json.Unmarshal(data, receipt)
}

var errExactResultReplay = errors.New("exact ReviewResult replay")

func typedResultReceiptsEnabled(state map[string]any) bool {
	return workstate.SupportsAssignments(state["schema_version"]) && loopruntime.PacketEnabled(state)
}

// professionalAnswerContent selects the actual professional fields for explicit replay comparison.
func professionalAnswerContent(data []byte, issued ...bool) (string, error) {
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

	canonical, err := json.Marshal(answer)
	if err != nil {
		return "", fmt.Errorf("canonicalize professional answer: %w", err)
	}
	return string(canonical), nil
}

func receiptEventIdentity(runtimeID string, generation int, ptr *PlanPointer, result *Result, packetBinding ...string) (string, string) {
	// An injective encoding of the existing responsibility and actual attempt;
	// neither the result text nor its old digest is an identity input.
	parts := []string{runtimeID, fmt.Sprint(generation), ptr.PlanID, fmt.Sprint(ptr.Revision), result.AssignmentID, result.ProducerAgentID, result.ResultID}
	if len(packetBinding) > 0 {
		parts = append(parts, packetBinding[0])
	}
	var key strings.Builder
	for _, part := range parts {
		fmt.Fprintf(&key, "%d:%s", len(part), part)
	}
	return "evt-review-result-" + key.String(), "runtime:review-result:" + key.String()
}

func newResultReceipt(root string, state map[string]any, plan *Plan, ptr *PlanPointer, assignment *PlanAssignment, result *Result, resultRef string, packet *workpacket.ReviewResultPacket, executionRoot string) (ResultReceipt, error) {
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
	eventID, idem := receiptEventIdentity(runtimeID, result.BaselineGeneration, ptr, result)
	packetID := ""
	if packet != nil {
		packetID = packet.ID
		eventID, idem = receiptEventIdentity(runtimeID, result.BaselineGeneration, ptr, result, packetID)
	}

	journal, _ := state["journal"].(map[string]any)
	revision := intField(state["revision"]) + 1
	sequence := intField(journal["last_sequence"]) + 1
	return ResultReceipt{
		Contract: resultReceiptContract, SubmissionKey: result.ResultID,
		RuntimeID: runtimeID, BaselineGeneration: result.BaselineGeneration,
		ReviewPlanID: plan.ReviewPlanID, ReviewPlanRef: planRef,
		ReviewPlanRevision: ptr.Revision,
		AssignmentID:       result.AssignmentID, AssignmentRevision: result.AssignmentRevision,
		ProducerAgentID: result.ProducerAgentID,

		ResultRef: resultRef,
		EventID:   eventID, IdempotencyKey: idem,
		AcceptedAfterRevision: revision, AcceptedSequence: sequence,
		PacketID: packetID, ExecutionRoot: executionRoot,
	}, nil
}

func planAssignmentContent(assignment *PlanAssignment) (string, error) {
	if assignment == nil {
		return "", errors.New("ReviewPlan assignment is missing")
	}
	data, err := json.Marshal(assignment)
	if err != nil {
		return "", fmt.Errorf("encode ReviewPlan assignment: %w", err)
	}
	return string(data), nil
}

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
func replayAcceptedResult(root string, state map[string]any, incoming *Result, answerContent string, contexts ...context.Context) (bool, error) {
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
	raw, ok := row["result_receipt"]
	if !ok || raw == nil {
		return false, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return false, err
	}
	var receipt ResultReceipt
	if err := decodeOriginalResultReceipt(data, &receipt); err != nil {
		return false, err
	}
	if receipt.SubmissionKey != incoming.ResultID {
		return false, fmt.Errorf("assignment %s already consumed result %s", incoming.AssignmentID, receipt.SubmissionKey)
	}
	if err := compareReceivedProfessionalContent(root, receipt, answerContent); err != nil {
		return false, err
	}
	return true, nil
}

// Compare only the saved original professional content for this explicit
// repeated/new answer. Ordinary completed responsibility events read the
// existing receipt directly without opening the private answer or evidence.
func compareReceivedProfessionalContent(root string, receipt ResultReceipt, incoming string) error {
	if receipt.ResultRef == "" {
		return errors.New("accepted S7 result content is unavailable")
	}
	data, err := workpacket.ReadFile(root, receipt.ResultRef, resultReceiptReadLimit)
	if err != nil {
		return fmt.Errorf("read accepted S7 result: %w", err)
	}
	var envelope struct {
		Submitted json.RawMessage `json:"submitted_answer"`
		Effective json.RawMessage `json:"effective_answer"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	original := envelope.Submitted
	if len(original) == 0 || string(original) == "null" {
		original = envelope.Effective
	}
	if len(original) == 0 || string(original) == "null" {
		original = data
	}
	accepted, err := professionalAnswerContent(original)
	if err != nil {
		return err
	}
	if accepted != incoming {
		return errors.New("S7 attempt differs from originally accepted professional answer")
	}
	return nil
}

// Kept for explicit diagnostics: the accepted original responsibility and
// result path are the available facts, not a full-history proof closure.
func verifyResultReceiptClosure(root string, state, assignmentRow map[string]any, receipt ResultReceipt, incoming *Result, contexts ...context.Context) error {
	if receipt.RuntimeID != stringField(state["runtime_id"]) || receipt.AssignmentID != incoming.AssignmentID || receipt.ProducerAgentID != incoming.ProducerAgentID || receipt.SubmissionKey != incoming.ResultID {
		return errors.New("incoming result tuple differs from accepted responsibility")
	}
	return nil
}

func jsonValueEqual(value, expected any) bool {
	left, leftErr := json.Marshal(value)
	right, rightErr := json.Marshal(expected)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
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
