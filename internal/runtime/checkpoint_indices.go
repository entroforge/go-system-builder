package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// ValidateCheckpointIndices checks durable relationships, including historical
// attempts. It never derives reception from authoring files or re-enables PASS.
func ValidateCheckpointIndices(root string, state map[string]any, journalBytes []byte) error {
	return ValidateCheckpointIndicesContext(context.Background(), root, state, journalBytes)
}

func ValidateCheckpointIndicesContext(ctx context.Context, root string, state map[string]any, journalBytes []byte) error {
	ctx = WithReviewResultReadCache(WithPacketBodyRoot(ctx, root))
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateS6MainStartReceipts(state, journalBytes); err != nil {
		return err
	}
	if err := ValidateAssignmentRevisionReceipts(state, journalBytes); err != nil {
		return err
	}
	tasks, err := validateTaskPlanningCheckpointIndices(root, state, journalBytes)
	if err != nil {
		return err
	}
	state = tasks
	contracts, err := validateContractCheckpointIndices(root, state, journalBytes)
	if err != nil {
		return err
	}
	state = contracts
	filtered, err := validateDesignCheckpointIndices(root, state, journalBytes)
	if err != nil {
		return err
	}
	state = filtered
	s9ResultFiltered, err := validateRepairResultCheckpointIndices(root, state, journalBytes, ctx)
	if err != nil {
		return err
	}
	state = s9ResultFiltered
	s9Filtered, err := validateRepairPlanReportCheckpointIndices(root, state, journalBytes, ctx)
	if err != nil {
		return err
	}
	state = s9Filtered
	s8Filtered, err := validateHypothesisResultCheckpointIndices(root, state, journalBytes)
	if err != nil {
		return err
	}
	state = s8Filtered
	s7Filtered, err := validateReviewResultCheckpointIndicesContext(ctx, root, state, journalBytes)
	if err != nil {
		return err
	}
	state = s7Filtered
	if err := validateCheckpointToolReceipts(state, journalBytes); err != nil {
		return err
	}
	if err := validateCheckpointContextBindings(root, state, journalBytes); err != nil {
		return err
	}
	if !hasIssuedIndices(state) {
		return nil
	}
	journal, err := inspectJournalData(journalBytes)
	if err != nil {
		return err
	}
	if len(journal.Events) > 0 {
		after, err := integerField(journal.Events[len(journal.Events)-1], "after_revision")
		revision, revErr := integerField(state, "revision")
		if err != nil || revErr != nil || after != revision {
			return fmt.Errorf("checkpoint revision is not linked to the journal tail")
		}
	}
	entities, _ := state["entities"].(map[string]any)
	packets, _ := entities["work_packets"].([]any)
	byPacket := map[string]workpacket.Packet{}
	first := map[string]bool{}
	for _, raw := range packets {
		row, _ := raw.(map[string]any)
		p, ok, err := FindWorkPacket(state, stringValue(row["packet_id"]))
		if err != nil || !ok {
			return fmt.Errorf("checkpoint packet cannot be decoded")
		}
		if _, duplicate := byPacket[p.ID]; duplicate {
			return fmt.Errorf("duplicate checkpoint packet")
		}
		expected, err := workpacket.RebuildS5(p)
		if err != nil || !reflect.DeepEqual(expected, p) || p.Binding.RuntimeID != state["runtime_id"] || p.Binding.Generation > baselineGeneration(state) {
			return fmt.Errorf("checkpoint packet frozen binding differs")
		}
		if p.TaskAnalysis != nil {
			if _, err := workpacket.ReadTaskAnalysis(root, p); err != nil {
				return fmt.Errorf("checkpoint frozen task analysis: %w", err)
			}
		}
		if p.Binding.Attempt == 1 {
			key := fmt.Sprintf("%s/%d/%d/%s/%s", p.Binding.RuntimeID, p.Binding.Generation, p.Binding.StageEntry, p.Binding.Owner, p.Binding.Responsibility)
			if first[key] {
				return fmt.Errorf("duplicate initial checkpoint responsibility")
			}
			first[key] = true
		}
		byPacket[p.ID] = p
		issued := false
		issuanceKey := "packet:" + p.ID
		if p.Binding.Attempt > 1 {
			issuanceKey = "document-review-revision:" + p.Binding.PreviousSubmissionKey
		}
		for _, event := range journal.Events {
			if event["idempotency_key"] == issuanceKey && (event["transition_id"] == "PACKET-ISSUE" || event["transition_id"] == "DOCUMENT-REVIEW-REVISION") {
				issued = true
			}
		}
		if !issued {
			return fmt.Errorf("checkpoint packet has no original issuance commit")
		}
	}
	evidence := map[string]map[string]any{}
	rows, _ := state["evidence"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		id := stringValue(row["id"])
		if evidence[id] != nil {
			return fmt.Errorf("duplicate checkpoint evidence")
		}
		evidence[id] = row
	}
	received := map[string]SubmissionRecord{}
	keys := map[string]bool{}
	rows, _ = entities["submissions"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		r, ok, err := FindSubmission(state, stringValue(row["packet_id"]))
		if err != nil || !ok {
			return fmt.Errorf("checkpoint reception cannot be decoded")
		}
		p, exists := byPacket[r.PacketID]
		if !exists || received[r.PacketID].Key != "" || keys[r.Key] {
			return fmt.Errorf("duplicate or orphan checkpoint reception")
		}
		key := workpacket.Digest([]byte("submission:" + p.ID))
		if r.Key != key || r.FollowupID != workpacket.Digest([]byte(key+":after-domain")) || r.Owner != p.Binding.Owner || r.OriginState != "document_verification" || r.OriginPhase != "" || r.OriginEntry != p.Binding.StageEntry {
			return fmt.Errorf("checkpoint reception identity differs")
		}
		e := evidence[r.EvidenceID]
		producers, _ := e["produced_by"].([]any)
		generation, generationErr := integerField(e, "baseline_generation")
		if e == nil || e["kind"] != "document_review" || e["responsibility_id"] != p.Binding.Responsibility || len(producers) != 1 || producers[0] != p.Binding.Owner || generationErr != nil || generation != p.Binding.Generation {
			return fmt.Errorf("checkpoint reception has no matching domain evidence")
		}
		data, err := workpacket.ReadFile(root, stringValue(e["path"]), 1<<20)
		if err != nil || workpacket.Digest(data) != e["sha256"] {
			return fmt.Errorf("checkpoint reception artifact is unverifiable: %s", r.EvidenceID)
		}
		var envelope workpacket.DocumentReviewEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return err
		}
		answer := workpacket.Answer{Conclusion: &envelope.Conclusion, Findings: envelope.Findings}
		draft := p.Draft
		draft.Answer = answer
		authoring, err := json.Marshal(draft)
		if err != nil {
			return err
		}
		if _, issues := workpacket.ValidateAnswer(p, authoring); len(issues) > 0 {
			return fmt.Errorf("checkpoint answer is incomplete")
		}
		if err := workpacket.ValidateDocumentReview(p, r.EvidenceID, answer, data); err != nil {
			return err
		}
		digest, err := workpacket.AnswerDigest(answer)
		if err != nil || digest != r.ContentHash {
			return fmt.Errorf("checkpoint answer digest differs")
		}
		receptionFound := false
		for _, event := range journal.Events {
			if event["transition_id"] != "EVIDENCE-RECORD" {
				continue
			}
			ids, _ := event["evidence_ids"].([]any)
			for _, id := range ids {
				if id == r.EvidenceID {
					receptionFound = true
				}
			}
		}
		if !receptionFound {
			return fmt.Errorf("checkpoint reception has no original journal commit")
		}
		if r.ControlStatus == "no_transition" {
			index, ok := journal.EventIndex["evt-submission-control-"+r.FollowupID]
			if !ok || journal.Events[index]["transition_id"] != "SUBMISSION-EVALUATE" {
				return fmt.Errorf("checkpoint evaluated followup has no original control commit")
			}
		}
		if r.ControlStatus == "advanced" {
			index, ok := journal.EventIndex[r.TransitionEventID]
			if !ok {
				return fmt.Errorf("advanced checkpoint reception has no actual transition event")
			}
			event := journal.Events[index]
			from, _ := event["from"].(map[string]any)
			if (event["transition_id"] != "TR-003" && event["transition_id"] != "TR-004") || from["state"] != r.OriginState {
				return fmt.Errorf("checkpoint followup transition binding differs")
			}
		} else if r.TransitionEventID != "" {
			return fmt.Errorf("unadvanced checkpoint reception has a transition event")
		}
		keys[r.Key] = true
		received[r.PacketID] = r
	}
	for _, p := range byPacket {
		if p.Binding.Attempt == 1 {
			continue
		}
		prior, ok, err := FindSubmissionKey(state, p.Binding.PreviousSubmissionKey)
		old, exists := byPacket[prior.PacketID]
		if err != nil || !ok || !exists || prior.RevisionPacketID != p.ID {
			return fmt.Errorf("checkpoint revision has no original previous reception")
		}
		b := old.Binding
		b.Attempt++
		b.PreviousSubmissionKey = prior.Key
		b.RevisionReason = p.Binding.RevisionReason
		b.ExecutionRequired = p.Binding.ExecutionRequired
		expected, err := workpacket.RebuildS5ForBinding(b, old)
		if err != nil || !reflect.DeepEqual(expected, p) {
			return fmt.Errorf("checkpoint revision frozen chain differs")
		}
	}
	for _, r := range received {
		if r.RevisionPacketID == "" {
			if r.SupersededBy != "" {
				return fmt.Errorf("checkpoint supersession has no issued revision")
			}
			continue
		}
		next, exists := byPacket[r.RevisionPacketID]
		if !exists || next.Binding.PreviousSubmissionKey != r.Key || evidence[r.EvidenceID]["status"] != "invalid" {
			return fmt.Errorf("checkpoint revision resurrects an old review")
		}
		successor, accepted := received[next.ID]
		expectedBy := "evt-document-revision-" + r.Key
		expectedRule := "document_review_revision_pending"
		if accepted {
			expectedBy = successor.EvidenceID
			expectedRule = "document_review_revision_superseded"
			if r.SupersededBy != successor.Key {
				return fmt.Errorf("checkpoint replacement is incomplete")
			}
		} else if r.SupersededBy != "" {
			return fmt.Errorf("unreceived checkpoint revision claims replacement")
		}
		e := evidence[r.EvidenceID]
		if e["invalidated_by"] != expectedBy || e["invalidation_rule"] != expectedRule {
			return fmt.Errorf("checkpoint old review invalidation differs")
		}
	}
	return nil
}
