package runtime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// HypothesisResultReceipt is the original immutable Case receipt wire.
// This wire is intentionally neutral: Runtime recovery must not import the
// investigation Writer in order to verify its original immutable artifacts.
type HypothesisResultReceipt struct {
	Contract                     string          `json:"contract"`
	PacketID                     string          `json:"packet_id"`
	PacketSHA256                 string          `json:"packet_sha256"`
	AnswerSHA256                 string          `json:"answer_sha256"`
	SubmittedAnswer              json.RawMessage `json:"submitted_answer"`
	OwnerAgentID                 string          `json:"owner_agent_id"`
	RuntimeID                    string          `json:"runtime_id"`
	BaselineGeneration           int             `json:"baseline_generation"`
	ReviewRound                  int             `json:"review_round"`
	AssignmentID                 string          `json:"assignment_id"`
	AssignmentSHA256             string          `json:"assignment_sha256"`
	TeamID                       string          `json:"team_id"`
	TaskID                       string          `json:"task_id"`
	TaskRef                      string          `json:"task_ref"`
	TaskSHA256                   string          `json:"task_sha256"`
	ManifestRef                  string          `json:"manifest_ref"`
	ManifestSHA256               string          `json:"manifest_sha256"`
	ExecutionRoot                string          `json:"execution_root"`
	IssuanceEventID              string          `json:"issuance_event_id"`
	AcceptedEventID              string          `json:"accepted_event_id"`
	AcceptedIdempotencyKey       string          `json:"accepted_idempotency_key"`
	AcceptedAfterRuntimeRevision int             `json:"accepted_after_runtime_revision"`
	AcceptedSequence             int             `json:"accepted_sequence"`
	SuccessorCaseRef             string          `json:"successor_case_ref"`
	SuccessorCaseRevision        int             `json:"successor_case_revision"`
}

// VerifiedHypothesisResultReceipt returns an original accepted fact without
// granting a new write or trusting a surviving authoring file. Historical
// Cases require an original pointer/history authority, never directory scans.
func VerifiedHypothesisResultReceipt(root string, state map[string]any, packetID, owner string) (HypothesisResultReceipt, bool, error) {
	p, found, err := FindHypothesisResultPacket(state, packetID)
	if err != nil || !found {
		return HypothesisResultReceipt{}, false, err
	}
	if owner == "" || p.Binding.OwnerAgentID != owner {
		return HypothesisResultReceipt{}, false, fmt.Errorf("S8 receipt owner differs from original issued owner")
	}
	data, err := controlproof.ReadJournalForState(root, state)
	if err != nil {
		return HypothesisResultReceipt{}, false, err
	}
	journal, err := controlproof.InspectJournalData(data)
	if err != nil {
		return HypothesisResultReceipt{}, false, err
	}
	if err := verifyHypothesisResultPacketIssue(root, state, p, journal); err != nil {
		return HypothesisResultReceipt{}, false, err
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	if pointer == nil || pointer["case_id"] != p.Binding.CaseID {
		return HypothesisResultReceipt{}, false, fmt.Errorf("S8 historical receipt has no original Case pointer/history authority")
	}
	document, err := readCheckpointHypothesisCase(root, stringValue(pointer["path"]), stringValue(pointer["sha256"]))
	if err != nil {
		return HypothesisResultReceipt{}, false, err
	}
	all, err := HypothesisResultPackets(state)
	if err != nil {
		return HypothesisResultReceipt{}, false, err
	}
	packets := map[string]workpacket.HypothesisResultPacket{}
	for _, issued := range all {
		packets[issued.ID] = issued
	}
	if err := validateHypothesisResultCaseReceipts(root, state, journal, packets); err != nil {
		return HypothesisResultReceipt{}, false, err
	}
	for _, row := range objectRows(document["hypothesis_results"]) {
		receipt, _ := row["receipt"].(map[string]any)
		if receipt["packet_id"] != packetID {
			continue
		}
		encoded, err := json.Marshal(receipt)
		if err != nil {
			return HypothesisResultReceipt{}, false, err
		}
		var result HypothesisResultReceipt
		if err := json.Unmarshal(encoded, &result); err != nil {
			return HypothesisResultReceipt{}, false, err
		}
		return result, true, nil
	}
	return HypothesisResultReceipt{}, false, nil
}

func validateHypothesisResultCheckpointIndices(root string, state map[string]any, data []byte) (map[string]any, error) {
	packets, err := HypothesisResultPackets(state)
	if err != nil {
		return nil, err
	}
	journal, err := controlproof.InspectJournalData(data)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]workpacket.HypothesisResultPacket, len(packets))
	for _, p := range packets {
		if err := verifyHypothesisResultPacketIssue(root, state, p, journal); err != nil {
			return nil, fmt.Errorf("S8 checkpoint packet %s: %w", p.ID, err)
		}
		byID[p.ID] = p
	}
	for _, event := range journal.Events {
		id := stringValue(event["event_id"])
		if !strings.HasPrefix(id, "evt-hypothesis-result-packet-") {
			continue
		}
		p, exists := byID[strings.TrimPrefix(id, "evt-hypothesis-result-packet-")]
		if !exists || p.IssuanceEventID != id {
			return nil, fmt.Errorf("S8 original issuance event has no durable packet row")
		}
	}
	if err := validateHypothesisResultCaseReceipts(root, state, journal, byID); err != nil {
		return nil, err
	}
	if len(packets) == 0 {
		return state, nil
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	filtered := make([]any, 0, len(rows)-len(packets))
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != hypothesisResultPacketTemplateID {
			filtered = append(filtered, raw)
		}
	}
	copyState := make(map[string]any, len(state))
	for key, value := range state {
		copyState[key] = value
	}
	copyEntities := make(map[string]any, len(entities))
	for key, value := range entities {
		copyEntities[key] = value
	}
	copyEntities["work_packets"] = filtered
	copyState["entities"] = copyEntities
	return copyState, nil
}

func readCheckpointHypothesisCase(root, ref, sha string) (map[string]any, error) {
	data, err := workpacket.ReadFile(root, ref, 20<<20)
	if err != nil || workpacket.Digest(data) != sha {
		return nil, fmt.Errorf("S8 Case %s bytes/SHA differ: %v", ref, err)
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return nil, err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", data); err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	return document, nil
}

func validateHypothesisResultCaseReceipts(root string, state map[string]any, journal controlproof.JournalInspection, packets map[string]workpacket.HypothesisResultPacket) error {
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	if pointer == nil {
		return nil
	}
	// Preserve old, unissued investigation behavior. Typed receipts are only
	// interpreted after this binary has issued S8 packets for this Case.
	currentCaseID := stringValue(pointer["case_id"])
	hasCurrentPacket := false
	for _, p := range packets {
		if p.Binding.CaseID == currentCaseID {
			hasCurrentPacket = true
			break
		}
	}
	if !hasCurrentPacket {
		return nil
	}
	document, err := readCheckpointHypothesisCase(root, stringValue(pointer["path"]), stringValue(pointer["sha256"]))
	if err != nil {
		return err
	}
	revision, err := integerField(document, "revision")
	pointerRevision, ptrErr := integerField(pointer, "revision")
	if err != nil || ptrErr != nil || revision != pointerRevision || document["case_id"] != currentCaseID {
		return fmt.Errorf("S8 current Case identity/revision differs from original pointer")
	}
	accepted := map[string]bool{}
	for _, row := range objectRows(document["hypothesis_results"]) {
		rawReceipt, exists := row["receipt"]
		if !exists {
			for _, p := range packets {
				if p.Binding.CaseID == currentCaseID && p.Binding.HypothesisID == row["hypothesis_id"] && p.Binding.AssignmentID == row["assignment_id"] {
					return fmt.Errorf("S8 issued hypothesis has a result without its original typed receipt")
				}
			}
			continue
		}
		encoded, err := json.Marshal(rawReceipt)
		if err != nil {
			return err
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("hypothesis-result-receipt.schema.json", encoded); err != nil {
			return err
		}
		var receipt HypothesisResultReceipt
		if err := json.Unmarshal(encoded, &receipt); err != nil {
			return err
		}
		p, found := packets[receipt.PacketID]
		if !found || accepted[receipt.PacketID] || p.Binding.CaseID != currentCaseID {
			return fmt.Errorf("S8 receipt is orphaned or duplicated")
		}
		accepted[receipt.PacketID] = true
		if err := verifyCheckpointHypothesisReceipt(root, state, journal, pointer, document, row, p, receipt); err != nil {
			return err
		}
	}
	return nil
}

func verifyCheckpointHypothesisReceipt(root string, state map[string]any, journal controlproof.JournalInspection, pointer, document, row map[string]any, p workpacket.HypothesisResultPacket, r HypothesisResultReceipt) error {
	b := p.Binding
	digest, err := workpacket.HypothesisResultPacketDigest(p)
	if err != nil || digest != r.PacketSHA256 || r.Contract != "s8-hypothesis-result-receipt-v1" || r.RuntimeID != b.RuntimeID ||
		r.BaselineGeneration != b.BaselineGeneration || r.ReviewRound != b.ReviewRound || r.OwnerAgentID != b.OwnerAgentID ||
		r.AssignmentID != b.AssignmentID || r.AssignmentSHA256 != b.AssignmentSHA256 || r.TeamID != b.TeamID ||
		r.TaskID != b.TaskID || r.TaskRef != b.TaskRef || r.TaskSHA256 != b.TaskSHA256 ||
		r.ManifestRef != b.ManifestRef || r.ManifestSHA256 != b.ManifestSHA256 || r.IssuanceEventID != p.IssuanceEventID ||
		row["hypothesis_id"] != b.HypothesisID || row["assignment_id"] != b.AssignmentID || r.SuccessorCaseRevision <= b.CaseRevision {
		return fmt.Errorf("S8 receipt machine tuple differs from original issuance")
	}
	draft := p.Draft
	draft.Answer = r.SubmittedAnswer
	authoring, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	answer, issues := workpacket.ValidateHypothesisResultAnswer(p, authoring)
	if len(issues) != 0 || workpacket.Digest(answer) != r.AnswerSHA256 {
		return fmt.Errorf("S8 receipt complete professional answer differs")
	}
	var fields map[string]any
	if err := json.Unmarshal(answer, &fields); err != nil {
		return err
	}
	for _, key := range []string{"method", "evidence_refs", "source_boundary_refs", "observed", "counterfactual_or_discriminator", "result", "explains_finding_ids", "does_not_explain"} {
		value := fields[key]
		if key == "explains_finding_ids" || key == "does_not_explain" {
			ids, ok := checkpointStringSlice(value)
			if !ok || !hypothesisExactStringSet(row[key], ids) {
				return fmt.Errorf("S8 result Finding set differs from complete answer: %s", key)
			}
			continue
		}
		if text, ok := value.(string); ok {
			value = strings.TrimSpace(text)
		}
		if key == "evidence_refs" && matchesS8EvidenceProjection(p.ID, value, row[key]) {
			continue
		}
		if !hypothesisJSONEqual(row[key], value) {
			return fmt.Errorf("S8 result projection differs from complete answer: %s", key)
		}
	}
	var professional workpacket.HypothesisResultAnswer
	if err := json.Unmarshal(answer, &professional); err != nil {
		return err
	}
	expectedNew := make([]any, 0, len(professional.NewHypotheses))
	for i, question := range professional.NewHypotheses {
		suffix := workpacket.Digest([]byte(p.ID + "\x00" + strconv.Itoa(i)))
		expectedNew = append(expectedNew, map[string]any{
			"hypothesis_id": "hypothesis-s8-" + suffix, "assignment_id": "assignment-s8-" + suffix,
			"statement": strings.TrimSpace(question.Statement), "invariant": strings.TrimSpace(question.Invariant),
			"discriminator":      strings.TrimSpace(question.Discriminator),
			"expected_outcomes":  map[string]any{"support": strings.TrimSpace(question.ExpectedOutcomes.Support), "refute": strings.TrimSpace(question.ExpectedOutcomes.Refute)},
			"source_finding_ids": question.SourceFindingIDs, "evidence_refs": question.EvidenceRefs, "status": "open",
		})
	}
	projectedQuestions, _ := row["new_hypotheses"].([]any)
	if len(projectedQuestions) == len(expectedNew) {
		for i, projected := range projectedQuestions {
			actual, _ := projected.(map[string]any)
			expected, _ := expectedNew[i].(map[string]any)
			if matchesS8EvidenceProjection(p.ID, expected["evidence_refs"], actual["evidence_refs"]) {
				expected["evidence_refs"] = actual["evidence_refs"]
			}
		}
	}
	if !hypothesisJSONEqual(row["new_hypotheses"], expectedNew) {
		return fmt.Errorf("S8 server-assigned new hypotheses differ from complete answer")
	}
	index, found := journal.EventIndex[r.AcceptedEventID]
	issueIndex, issueFound := journal.EventIndex[p.IssuanceEventID]
	if !found || !issueFound || index <= issueIndex {
		return fmt.Errorf("S8 receipt has no original acceptance after issuance")
	}
	event := journal.Events[index]
	after, afterErr := integerField(event, "after_revision")
	sequence, seqErr := integerField(event, "sequence")
	generation, genErr := integerField(event, "baseline_generation")
	actor, _ := event["actor"].(map[string]any)
	from, _ := event["from"].(map[string]any)
	to, _ := event["to"].(map[string]any)
	evidenceIDs, _ := checkpointStringSlice(event["evidence_ids"])
	if afterErr != nil || after != r.AcceptedAfterRuntimeRevision || seqErr != nil || sequence != r.AcceptedSequence ||
		genErr != nil || generation != b.BaselineGeneration || event["runtime_id"] != b.RuntimeID ||
		event["event"] != "transition_committed" || event["outcome"] != "committed" || len(evidenceIDs) != 1 || evidenceIDs[0] != b.CaseID ||
		event["transition_id"] != "INVESTIGATION-CASE-REVISION-UPDATED" || event["idempotency_key"] != r.AcceptedIdempotencyKey ||
		actor["type"] != "orchestrator" || actor["id"] != "orchestrator" || from["state"] != "bug_resolution" || from["phase"] != "investigation" || !hypothesisJSONEqual(from, to) {
		return fmt.Errorf("S8 receipt original acceptance coordinates differ")
	}
	if r.AcceptedEventID != fmt.Sprintf("evt-investigation-case-%s-r%d", b.CaseID, after) ||
		r.AcceptedIdempotencyKey != fmt.Sprintf("runtime:investigation-case:%s:%d", b.CaseID, after-1) {
		return fmt.Errorf("S8 receipt original acceptance identity differs")
	}
	expectedRef := fmt.Sprintf(".claude/review/investigation/cases/%s-r%d.json", b.CaseID, r.SuccessorCaseRevision)
	if r.SuccessorCaseRef != expectedRef {
		return fmt.Errorf("S8 receipt successor path differs")
	}
	sha := ""
	if pointer["path"] == r.SuccessorCaseRef && hypothesisJSONEqual(pointer["revision"], document["revision"]) {
		sha = stringValue(pointer["sha256"])
	}
	for _, historical := range objectRows(document["revision_history"]) {
		revision, err := integerField(historical, "revision")
		if err == nil && revision == r.SuccessorCaseRevision && historical["path"] == r.SuccessorCaseRef {
			if sha != "" {
				return fmt.Errorf("S8 receipt successor has ambiguous original history")
			}
			sha = stringValue(historical["sha256"])
		}
	}
	if sha == "" {
		return fmt.Errorf("S8 receipt successor SHA has no original pointer/history authority")
	}
	successor, err := readCheckpointHypothesisCase(root, r.SuccessorCaseRef, sha)
	if err != nil {
		return err
	}
	matching := 0
	for _, original := range objectRows(successor["hypothesis_results"]) {
		if hypothesisJSONEqual(original, row) {
			matching++
		}
	}
	if matching != 1 || successor["case_id"] != b.CaseID {
		return fmt.Errorf("S8 receipt is not the unique original successor result")
	}
	successorRevision, err := integerField(successor, "revision")
	if err != nil || successorRevision != r.SuccessorCaseRevision {
		return fmt.Errorf("S8 original successor Case revision differs")
	}
	originalHypothesis := 0
	for _, question := range objectRows(successor["hypotheses"]) {
		if question["hypothesis_id"] != b.HypothesisID || question["assignment_id"] != b.AssignmentID {
			continue
		}
		encoded, err := json.Marshal(question)
		if err != nil {
			return err
		}
		var original workpacket.HypothesisResultQuestion
		if err := json.Unmarshal(encoded, &original); err != nil {
			return err
		}
		digest, err := workpacket.HypothesisQuestionDigest(original)
		if err != nil || digest != b.HypothesisQuestionSHA256 || professional.Result == nil || original.Status != *professional.Result {
			return fmt.Errorf("S8 original successor hypothesis disposition differs from accepted answer")
		}
		originalHypothesis++
	}
	if originalHypothesis != 1 {
		return fmt.Errorf("S8 original successor has no unique accepted hypothesis")
	}
	for _, expected := range expectedNew {
		matches := 0
		for _, question := range objectRows(successor["hypotheses"]) {
			if hypothesisJSONEqual(question, expected) {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("S8 new hypothesis is missing or duplicated in original successor")
		}
	}
	return nil
}

func matchesS8EvidenceProjection(packetID string, authored, projected any) bool {
	a, aok := checkpointStringSlice(authored)
	b, bok := checkpointStringSlice(projected)
	if !aok || !bok || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] && !evidence.MatchesS8PathBinding(packetID, a[i], b[i]) {
			return false
		}
	}
	return true
}
