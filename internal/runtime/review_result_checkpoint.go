package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

const reviewResultArtifactLimit int64 = 20 << 20

// VerifyReviewResultPacketIssue verifies that p is the canonical packet in the
// original Runtime work_packets registry and that its exact PACKET-ISSUE event
// was committed by the original Writer. It is a reader only: it neither
// authorizes a save nor checks professional completion.
func VerifyReviewResultPacketIssue(root string, state map[string]any, p workpacket.ReviewResultPacket) error {
	return VerifyReviewResultPacketIssueContext(context.Background(), root, state, p)
}

func VerifyReviewResultPacketIssueContext(ctx context.Context, root string, state map[string]any, p workpacket.ReviewResultPacket) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = WithPacketBodyRoot(ctx, root)
	journalBytes, err := controlproof.ReadJournalForStateContext(ctx, root, state)
	if err != nil {
		return fmt.Errorf("read original Runtime journal for S7 packet: %w", err)
	}
	return verifyReviewResultPacketIssueJournalContext(ctx, root, state, p, journalBytes)
}

func verifyReviewResultPacketIssueJournal(root string, state map[string]any, p workpacket.ReviewResultPacket, journalBytes []byte) error {
	return verifyReviewResultPacketIssueJournalContext(context.Background(), root, state, p, journalBytes)
}

func verifyReviewResultPacketIssueJournalContext(ctx context.Context, root string, state map[string]any, p workpacket.ReviewResultPacket, journalBytes []byte) error {
	ctx = WithPacketBodyRoot(ctx, root)
	stored, found, err := FindReviewResultPacketContext(ctx, state, p.ID)
	if err != nil {
		return err
	}
	if !found || !reflect.DeepEqual(stored, p) {
		return errors.New("S7 ReviewResult packet is not the exact Runtime registry row")
	}
	if err := verifyCanonicalReviewResultPacketSources(ctx, root, state, p); err != nil {
		return err
	}
	inspection, err := controlproof.InspectJournalSegmentDataFromContext(ctx, journalBytes, 1)
	if err != nil {
		return fmt.Errorf("inspect original Runtime journal for S7 packet: %w", err)
	}
	return verifyReviewResultPacketIssueEvent(state, p, inspection)
}

// The caller must first prove the exact canonical Runtime registry row.
func verifyCanonicalReviewResultPacketSources(ctx context.Context, root string, state map[string]any, p workpacket.ReviewResultPacket) error {
	if !PacketEnabled(state) {
		return errors.New("S7 ReviewResult packet requires the explicit v1.2 packet marker")
	}
	// FindReviewResultPacketContext already decoded and canonically rebuilt
	// this exact registry row. Rebuilding it again for an unused digest adds
	// no proof; retain the independent original issuance-event identity check.
	if p.IssuanceEventID != "evt-review-result-packet-"+p.ID {
		return errors.New("S7 ReviewResult packet is not canonical")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyReviewResultPacketSources(root, p); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func verifyReviewResultPacketIssueEvent(state map[string]any, p workpacket.ReviewResultPacket, inspection controlproof.JournalInspection) error {
	if err := controlproof.ValidateStateJournalPair(state, inspection); err != nil {
		return fmt.Errorf("S7 packet Runtime/journal cursor: %w", err)
	}
	index, ok := inspection.EventIndex[p.IssuanceEventID]
	if !ok {
		return errors.New("S7 ReviewResult packet has no original issuance event")
	}
	event := inspection.Events[index]
	actor, _ := event["actor"].(map[string]any)
	from, _ := event["from"].(map[string]any)
	to, _ := event["to"].(map[string]any)
	before, beforeErr := integerField(event, "before_revision")
	after, afterErr := integerField(event, "after_revision")
	sequence, sequenceErr := integerField(event, "sequence")
	generation, generationErr := integerField(event, "baseline_generation")
	stateRevision, stateRevisionErr := integerField(state, "revision")
	if event["event_id"] != p.IssuanceEventID || event["event"] != "transition_committed" || event["outcome"] != "committed" ||
		event["transition_id"] != "PACKET-ISSUE" || event["idempotency_key"] != "packet:"+p.ID ||
		event["runtime_id"] != p.Binding.RuntimeID || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" ||
		generationErr != nil || generation != p.Binding.BaselineGeneration || beforeErr != nil || afterErr != nil || after != before+1 ||
		sequenceErr != nil || sequence < 1 || stateRevisionErr != nil || after > stateRevision || from["state"] != "verification" || to["state"] != "verification" || !reflect.DeepEqual(from, to) {
		return errors.New("S7 ReviewResult packet issuance event differs from its frozen binding")
	}
	return nil
}

type checkpointReviewResultReceipt struct {
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

// validateReviewResultCheckpointIndices proves S7 packet issuance and any
// packet-bound original Result receipt. It removes S7 rows only from the
// temporary state passed to the older S5 validator; durable state is untouched.
func validateReviewResultCheckpointIndices(root string, state map[string]any, journalBytes []byte) (map[string]any, error) {
	return validateReviewResultCheckpointIndicesContext(context.Background(), root, state, journalBytes)
}

func validateReviewResultCheckpointIndicesContext(ctx context.Context, root string, state map[string]any, journalBytes []byte) (map[string]any, error) {
	ctx = WithReviewResultReadCache(WithPacketBodyRoot(ctx, root))
	packets, err := reviewResultPacketsContext(ctx, state)
	if err != nil {
		return nil, err
	}
	inspection, err := controlproof.InspectJournalSegmentDataFromContext(ctx, journalBytes, 1)
	if err != nil {
		return nil, err
	}
	if err := controlproof.ValidateStateJournalPair(state, inspection); err != nil {
		return nil, fmt.Errorf("S7 checkpoint journal cursor: %w", err)
	}
	if len(packets) == 0 {
		if err := rejectOrphanReviewResultIssueEvents(inspection, nil); err != nil {
			return nil, err
		}
		if err := rejectOrphanReviewResultReceipts(state); err != nil {
			return nil, err
		}
		return state, nil
	}
	byID := make(map[string]workpacket.ReviewResultPacket, len(packets))
	issueSequence := make(map[string]int, len(packets))
	issueRevision := make(map[string]int, len(packets))
	byAssignmentTuple := map[string]string{}
	for _, packet := range packets {
		// packets and inspection belong to this exact local snapshot. Reuse
		// their already validated identities without rehashing/redecoding the
		// complete registry/journal for every historical row. Disk sources remain live.
		if err := verifyCanonicalReviewResultPacketSources(ctx, root, state, packet); err != nil {
			return nil, fmt.Errorf("S7 checkpoint packet %s: %w", packet.ID, err)
		}
		if err := verifyReviewResultPacketIssueEvent(state, packet, inspection); err != nil {
			return nil, fmt.Errorf("S7 checkpoint packet %s: %w", packet.ID, err)
		}
		issueIndex := inspection.EventIndex[packet.IssuanceEventID]
		issueSequence[packet.ID], _ = integerField(inspection.Events[issueIndex], "sequence")
		issueRevision[packet.ID], _ = integerField(inspection.Events[issueIndex], "after_revision")
		b := packet.Binding
		tuple := fmt.Sprintf("%s/%d/%s/%d/%d/%s", b.RuntimeID, b.BaselineGeneration, b.ReviewPlanID, b.ReviewPlanRevision, b.ReviewRound, b.AssignmentID)
		if old := byAssignmentTuple[tuple]; old != "" {
			return nil, fmt.Errorf("S7 Assignment has multiple frozen packet issuances: %s and %s", old, packet.ID)
		}
		byAssignmentTuple[tuple] = packet.ID
		byID[packet.ID] = packet
	}
	if err := rejectOrphanReviewResultIssueEvents(inspection, byID); err != nil {
		return nil, err
	}
	if err := validateReviewResultReceipts(ctx, root, state, inspection, packets, byID, issueSequence, issueRevision); err != nil {
		return nil, err
	}
	return withoutReviewResultPackets(state), nil
}

func validateReviewResultReceipts(ctx context.Context, root string, state map[string]any, journal controlproof.JournalInspection, packets []workpacket.ReviewResultPacket, byID map[string]workpacket.ReviewResultPacket, issueSequence, issueRevision map[string]int) error {
	reviewState, _ := state["review"].(map[string]any)
	assignments, _ := reviewState["assignments"].(map[string]any)
	if assignments == nil {
		return errors.New("S7 packet exists but Runtime Review Assignments are unavailable")
	}
	resultEvidence := map[string]map[string]any{}
	evidenceRows, _ := state["evidence"].([]any)
	for _, raw := range evidenceRows {
		row, _ := raw.(map[string]any)
		if row == nil || row["kind"] != "review_result" {
			continue
		}
		id := stringValue(row["id"])
		if id == "" || resultEvidence[id] != nil {
			return errors.New("S7 Runtime evidence index contains a duplicate or empty Result ID")
		}
		resultEvidence[id] = row
	}
	acceptedPacket := map[string]bool{}
	for assignmentID, rawAssignment := range assignments {
		assignment, _ := rawAssignment.(map[string]any)
		if assignment == nil {
			return fmt.Errorf("S7 Runtime Assignment %s is malformed", assignmentID)
		}
		rawReceipt, hasReceipt := assignment["result_receipt"]
		if !hasReceipt || rawReceipt == nil {
			if workstate.ReviewAssignmentActivity(assignment) == "consumed" {
				currentPackets, currentErr := CurrentReviewResultPacketsContext(ctx, state, stringValue(assignment["agent_id"]))
				if currentErr != nil {
					return currentErr
				}
				for _, packet := range currentPackets {
					if packet.Binding.AssignmentID == assignmentID {
						return fmt.Errorf("consumed S7 Assignment %s has no packet-bound Result receipt", assignmentID)
					}
				}
			}
			continue
		}
		receiptObject, objectOK := rawReceipt.(map[string]any)
		if !objectOK {
			return fmt.Errorf("S7 Result receipt for %s is not an object", assignmentID)
		}
		_, hasPacketID := receiptObject["packet_id"]
		_, hasPacketSHA := receiptObject["packet_sha256"]
		if hasPacketID != hasPacketSHA {
			return fmt.Errorf("S7 Result receipt for %s has an incomplete packet binding", assignmentID)
		}
		encoded, err := json.Marshal(rawReceipt)
		if err != nil {
			return fmt.Errorf("encode S7 Result receipt for %s: %w", assignmentID, err)
		}
		var receipt checkpointReviewResultReceipt
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&receipt); err != nil {
			return fmt.Errorf("decode S7 Result receipt for %s: %w", assignmentID, err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return fmt.Errorf("S7 Result receipt for %s has trailing JSON", assignmentID)
		}
		if receipt.PacketID == "" && receipt.PacketSHA256 == "" {
			if receipt.ExecutionRoot != "" {
				return fmt.Errorf("legacy S7 Result receipt for %s unexpectedly declares an execution root", assignmentID)
			}
			if receipt.Contract == "s7-review-result-receipt-v1" {
				for _, packet := range packets {
					if packet.Binding.AssignmentID == assignmentID && packet.Binding.RuntimeID == receipt.RuntimeID && packet.Binding.BaselineGeneration == receipt.BaselineGeneration && packet.Binding.ReviewPlanID == receipt.ReviewPlanID && packet.Binding.ReviewPlanRevision == receipt.ReviewPlanRevision && packet.Binding.ReviewRound == receipt.ReviewRound {
						if receipt.AcceptedSequence < 1 || issueSequence[packet.ID] < receipt.AcceptedSequence {
							return fmt.Errorf("legacy receipt for %s bypasses an earlier S7 packet issuance", assignmentID)
						}
					}
				}
			}
			continue
		}
		if receipt.PacketID == "" || !validCheckpointSHA(receipt.PacketSHA256) || !filepath.IsAbs(receipt.ExecutionRoot) || filepath.Clean(receipt.ExecutionRoot) != receipt.ExecutionRoot {
			return fmt.Errorf("S7 Result receipt for %s has an incomplete packet binding", assignmentID)
		}
		packet, ok := byID[receipt.PacketID]
		if !ok {
			return fmt.Errorf("S7 Result receipt for %s has no issued packet", assignmentID)
		}
		if acceptedPacket[packet.ID] {
			return fmt.Errorf("S7 packet %s has multiple accepted receipts", packet.ID)
		}
		acceptedPacket[packet.ID] = true
		digest, err := workpacket.ReviewResultPacketDigest(packet)
		if err != nil || digest != receipt.PacketSHA256 {
			return fmt.Errorf("S7 Result receipt for %s has a different packet digest", assignmentID)
		}
		if err := validateS7ReceiptTuple(root, state, assignmentID, assignment, packet, receipt); err != nil {
			return err
		}
		if err := validateS7AcceptedResult(root, state, journal, assignmentID, packet, receipt, resultEvidence[receipt.SubmissionKey], issueSequence[packet.ID], issueRevision[packet.ID]); err != nil {
			return err
		}
	}
	return nil
}

func validateS7ReceiptTuple(root string, state map[string]any, assignmentID string, row map[string]any, packet workpacket.ReviewResultPacket, receipt checkpointReviewResultReceipt) error {
	b := packet.Binding
	if receipt.Contract != "s7-review-result-receipt-v1" || receipt.SubmissionKey != packet.ResultID || receipt.RuntimeID != b.RuntimeID || receipt.RuntimeID != stringValue(state["runtime_id"]) ||
		receipt.BaselineGeneration != b.BaselineGeneration || receipt.ReviewPlanID != b.ReviewPlanID || receipt.ReviewPlanRef != b.ReviewPlanRef || receipt.ReviewPlanSHA256 != b.ReviewPlanSHA256 || receipt.ReviewPlanRevision != b.ReviewPlanRevision || receipt.ReviewRound != b.ReviewRound ||
		receipt.AssignmentID != b.AssignmentID || receipt.AssignmentID != assignmentID || receipt.AssignmentRevision != b.ReviewPlanRevision || receipt.AssignmentSHA256 != b.AssignmentSHA256 || receipt.ProducerAgentID != b.OwnerAgentID || row["agent_id"] != b.OwnerAgentID ||
		receipt.SubjectDigest != b.SubjectDigest || !optionalPacketDigestMatches(b.VerificationArtifactDigest, receipt.VerificationArtifactDigest) {
		return fmt.Errorf("S7 Result receipt for %s differs from its issued packet tuple", assignmentID)
	}
	activity := workstate.ReviewAssignmentActivity(row)
	if (activity != "consumed" && activity != "result_submitted") || row["result_ref"] != receipt.ResultRef || receipt.ResultRef == "" || !validCheckpointSHA(receipt.ResultSHA256) || !validCheckpointSHA(receipt.AnswerSHA256) || !validCheckpointSHA(receipt.EffectiveAnswerSHA256) || receipt.EventID == "" || receipt.IdempotencyKey == "" || receipt.AcceptedAfterRevision < 1 || receipt.AcceptedSequence < 1 {
		return fmt.Errorf("S7 Result receipt for %s is missing original acceptance coordinates", assignmentID)
	}
	if err := validateS7ExecutionRoot(root, state, packet, receipt.ExecutionRoot); err != nil {
		return fmt.Errorf("S7 Result receipt for %s execution source: %w", assignmentID, err)
	}
	return nil
}

// Runtime cannot import workspace (workspace depends on Runtime), so checkpoint
// restore parses only the persisted execution tuple needed to prove that the
// receipt's execution_root is the registered current source. Filesystem and
// Git freshness remain the review seal/gate reader's responsibility.
func validateS7ExecutionRoot(root string, state map[string]any, packet workpacket.ReviewResultPacket, executionRoot string) error {
	mainRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	mainRoot = filepath.Clean(mainRoot)
	if !filepath.IsAbs(executionRoot) || filepath.Clean(executionRoot) != executionRoot {
		return errors.New("execution root is not a canonical absolute path")
	}
	var registry struct {
		MainRoot   string `json:"main_root"`
		Executions map[string]struct {
			AssignmentID       string `json:"assignment_id"`
			RuntimeID          string `json:"runtime_id"`
			BaselineGeneration int    `json:"baseline_generation"`
			AgentID            string `json:"agent_id"`
			Path               string `json:"worktree_path"`
			Status             string `json:"status"`
		} `json:"executions"`
	}
	if raw, ok := state["workspace"]; ok && raw != nil {
		data, err := json.Marshal(raw)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &registry); err != nil {
			return fmt.Errorf("decode registered workspace tuple: %w", err)
		}
		registeredMain, err := filepath.Abs(registry.MainRoot)
		if err != nil || filepath.Clean(registeredMain) != mainRoot {
			return errors.New("registered workspace Main root differs from Runtime authority")
		}
	}
	if execution, found := registry.Executions[packet.Binding.AssignmentID]; found {
		if execution.AssignmentID != packet.Binding.AssignmentID || execution.RuntimeID != packet.Binding.RuntimeID || execution.BaselineGeneration != packet.Binding.BaselineGeneration || execution.AgentID != packet.Binding.OwnerAgentID || execution.Status != "ready" || !filepath.IsAbs(execution.Path) || filepath.Clean(execution.Path) != execution.Path || execution.Path == mainRoot || execution.Path != executionRoot {
			return errors.New("execution root differs from the current registered Assignment source")
		}
		return nil
	}
	if executionRoot != mainRoot {
		return errors.New("unregistered execution root is not the Main Runtime root")
	}
	return nil
}

func validateS7AcceptedResult(root string, state map[string]any, journal controlproof.JournalInspection, assignmentID string, packet workpacket.ReviewResultPacket, receipt checkpointReviewResultReceipt, evidence map[string]any, issueSequence, issueRevision int) error {
	if evidence == nil || evidence["kind"] != "review_result" || evidence["id"] != receipt.SubmissionKey || evidence["path"] != receipt.ResultRef || evidence["sha256"] != receipt.ResultSHA256 ||
		intValue(evidence["baseline_generation"]) != receipt.BaselineGeneration || intValue(evidence["review_round"]) != receipt.ReviewRound || !oneStringValue(evidence["produced_by"], receipt.ProducerAgentID) {
		return fmt.Errorf("S7 Result receipt for %s has no matching original evidence index", assignmentID)
	}
	resultBytes, err := workpacket.ReadFile(root, receipt.ResultRef, reviewResultArtifactLimit)
	if err != nil || sha256Bytes(resultBytes) != receipt.ResultSHA256 {
		return fmt.Errorf("S7 Result receipt for %s has no readable, matching Result artifact", assignmentID)
	}
	if err := workpacket.ValidateUniqueJSON(resultBytes); err != nil {
		return fmt.Errorf("S7 Result receipt for %s has ambiguous JSON: %w", assignmentID, err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(resultBytes, &envelope); err != nil {
		return fmt.Errorf("decode S7 Result artifact for %s: %w", assignmentID, err)
	}
	b := packet.Binding
	if rawString(envelope["evidence_id"]) != packet.ResultID || rawString(envelope["kind"]) != "review_result" || rawString(envelope["packet_id"]) != packet.ID || rawString(envelope["packet_sha256"]) != receipt.PacketSHA256 ||
		rawString(envelope["runtime_id"]) != b.RuntimeID || rawInteger(envelope["baseline_generation"]) != b.BaselineGeneration || rawInteger(envelope["review_round"]) != b.ReviewRound || rawString(envelope["review_plan_id"]) != b.ReviewPlanID || rawString(envelope["assignment_id"]) != b.AssignmentID || rawInteger(envelope["assignment_revision"]) != b.ReviewPlanRevision || rawString(envelope["producer_agent_id"]) != b.OwnerAgentID || rawString(envelope["subject_digest"]) != b.SubjectDigest {
		return fmt.Errorf("S7 Result artifact for %s differs from packet binding", assignmentID)
	}
	if b.VerificationArtifactDigest != nil && rawOptionalString(envelope["verification_artifact_digest"]) != *b.VerificationArtifactDigest {
		return fmt.Errorf("S7 Result artifact verification digest differs from packet binding")
	}
	if rawString(envelope["execution_root"]) != receipt.ExecutionRoot {
		return fmt.Errorf("S7 Result artifact execution root differs from its receipt")
	}
	if rawOptionalString(envelope["verification_artifact_digest"]) != optionalStringValue(receipt.VerificationArtifactDigest) {
		return fmt.Errorf("S7 Result artifact verification digest differs from its receipt")
	}
	submittedDigest, err := reviewPacketProfessionalDigest(envelope["submitted_answer"])
	if err != nil || submittedDigest != receipt.AnswerSHA256 || rawString(envelope["answer_sha256"]) != receipt.AnswerSHA256 {
		return fmt.Errorf("S7 submitted professional answer digest differs from its receipt")
	}
	effectiveDigest, err := reviewPacketProfessionalDigest(envelope["effective_answer"])
	if err != nil || effectiveDigest != receipt.EffectiveAnswerSHA256 || rawString(envelope["effective_answer_sha256"]) != receipt.EffectiveAnswerSHA256 {
		return fmt.Errorf("S7 effective professional answer digest differs from its receipt")
	}
	for _, key := range []string{"submitted_answer", "effective_answer"} {
		var answer map[string]json.RawMessage
		if err := json.Unmarshal(envelope[key], &answer); err != nil || answer == nil {
			return fmt.Errorf("S7 Result artifact for %s lacks complete %s", assignmentID, key)
		}
		if rawString(answer["result_id"]) != packet.ResultID || rawString(answer["assignment_id"]) != b.AssignmentID || rawInteger(answer["assignment_revision"]) != b.ReviewPlanRevision || rawString(answer["review_plan_id"]) != b.ReviewPlanID || rawInteger(answer["review_round"]) != b.ReviewRound || rawInteger(answer["baseline_generation"]) != b.BaselineGeneration || rawString(answer["producer_agent_id"]) != b.OwnerAgentID || rawString(answer["subject_digest"]) != b.SubjectDigest || rawOptionalString(answer["verification_artifact_digest"]) != optionalStringValue(receipt.VerificationArtifactDigest) || (b.VerificationArtifactDigest != nil && rawOptionalString(answer["verification_artifact_digest"]) != *b.VerificationArtifactDigest) {
			return fmt.Errorf("S7 Result artifact %s machine tuple differs from its packet", key)
		}
		if err := validateCheckpointReviewAnswer(root, packet, envelope[key], key); err != nil {
			return err
		}
	}
	eventIndex, ok := journal.EventIndex[receipt.EventID]
	if !ok {
		return fmt.Errorf("S7 Result receipt for %s has no original REVIEW-RESULT event", assignmentID)
	}
	event := journal.Events[eventIndex]
	actor, _ := event["actor"].(map[string]any)
	before, beforeErr := integerField(event, "before_revision")
	after, afterErr := integerField(event, "after_revision")
	sequence, sequenceErr := integerField(event, "sequence")
	generation, generationErr := integerField(event, "baseline_generation")
	stateRevision, stateRevisionErr := integerField(state, "revision")
	if event["event"] != "transition_committed" || event["outcome"] != "committed" || event["transition_id"] != "REVIEW-RESULT" || event["runtime_id"] != b.RuntimeID || event["idempotency_key"] != receipt.IdempotencyKey || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" || generationErr != nil || generation != b.BaselineGeneration || beforeErr != nil || afterErr != nil || after != before+1 || after != receipt.AcceptedAfterRevision || after > stateRevision || stateRevisionErr != nil || before < issueRevision || sequenceErr != nil || sequence != receipt.AcceptedSequence || sequence <= issueSequence {
		return fmt.Errorf("S7 Result receipt for %s differs from its original journal event", assignmentID)
	}
	if !containsCheckpointString(event["evidence_ids"], receipt.SubmissionKey) {
		return fmt.Errorf("S7 Result journal event for %s omits its Result evidence", assignmentID)
	}
	return nil
}

func validateCheckpointReviewAnswer(root string, packet workpacket.ReviewResultPacket, raw json.RawMessage, name string) error {
	if err := workpacket.ValidateUniqueJSON(raw); err != nil {
		return fmt.Errorf("S7 %s has duplicate JSON keys: %w", name, err)
	}
	if err := schema.NewValidator(root).ValidateBytes("review-result.schema.json", raw); err != nil {
		return fmt.Errorf("S7 %s is not a complete canonical ReviewResult: %w", name, err)
	}
	if err := workpacket.ValidateReviewResultCompleteness(packet.Binding, raw); err != nil {
		return fmt.Errorf("S7 %s Claim closure: %w", name, err)
	}
	return nil
}

func rejectOrphanReviewResultReceipts(state map[string]any) error {
	reviewState, _ := state["review"].(map[string]any)
	assignments, _ := reviewState["assignments"].(map[string]any)
	for id, raw := range assignments {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		if receipt, ok := row["result_receipt"].(map[string]any); ok {
			_, hasID := receipt["packet_id"]
			_, hasSHA := receipt["packet_sha256"]
			if hasID || hasSHA {
				return fmt.Errorf("S7 Result receipt for %s has no issued packet registry row", id)
			}
		}
	}
	return nil
}

func rejectOrphanReviewResultIssueEvents(journal controlproof.JournalInspection, packets map[string]workpacket.ReviewResultPacket) error {
	const eventPrefix = "evt-review-result-packet-"
	for _, event := range journal.Events {
		eventID := stringValue(event["event_id"])
		if !strings.HasPrefix(eventID, eventPrefix) {
			continue
		}
		packetID := strings.TrimPrefix(eventID, eventPrefix)
		packet, exists := packets[packetID]
		if !exists || event["transition_id"] != "PACKET-ISSUE" || event["idempotency_key"] != "packet:"+packetID || event["runtime_id"] != packet.Binding.RuntimeID {
			return fmt.Errorf("S7 packet issuance event %s has no exact original registry packet", eventID)
		}
	}
	return nil
}

func withoutReviewResultPackets(state map[string]any) map[string]any {
	copyState := make(map[string]any, len(state))
	for key, value := range state {
		copyState[key] = value
	}
	entities, _ := state["entities"].(map[string]any)
	copyEntities := make(map[string]any, len(entities))
	for key, value := range entities {
		copyEntities[key] = value
	}
	rows, _ := entities["work_packets"].([]any)
	kept := make([]any, 0, len(rows))
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["template_id"] == reviewResultPacketTemplateID {
			continue
		}
		kept = append(kept, raw)
	}
	copyEntities["work_packets"] = kept
	copyState["entities"] = copyEntities
	return copyState
}

func optionalPacketDigestMatches(binding, observed *string) bool {
	if binding == nil {
		return true
	}
	return observed != nil && *binding == *observed
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func reviewPacketProfessionalDigest(data json.RawMessage) (string, error) {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return "", errors.New("S7 professional answer must be a JSON object")
	}
	answer := make(map[string]any, 8)
	for _, key := range []string{"claim_results", "blocked_claims", "checks", "findings", "deviations", "site_lost", "verdict"} {
		if value, ok := object[key]; ok {
			answer[key] = value
		}
	}
	answer["verification_artifact_digest"] = object["verification_artifact_digest"]
	canonical, err := json.Marshal(answer)
	if err != nil {
		return "", err
	}
	return sha256Bytes(canonical), nil
}

func validCheckpointSHA(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

func sha256Bytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func rawString(value json.RawMessage) string {
	var result string
	_ = json.Unmarshal(value, &result)
	return result
}

func rawInteger(value json.RawMessage) int {
	var result int
	_ = json.Unmarshal(value, &result)
	return result
}

func rawOptionalString(value json.RawMessage) string {
	if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return ""
	}
	return rawString(value)
}

func oneStringValue(value any, expected string) bool {
	items, ok := value.([]any)
	return ok && len(items) == 1 && items[0] == expected
}

func containsCheckpointString(value any, expected string) bool {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if item == expected {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if item == expected {
				return true
			}
		}
	}
	return false
}
