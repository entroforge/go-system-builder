package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

var ErrRevisionConflict = errors.New("previous submission already has a revision with another reason")

// DocumentReviewRevisionRequest is a trusted Main application capability.
// Authorize validates the actual source and retires a stopped independent
// execution inside this same Writer mutation. Answer JSON cannot supply it.
type DocumentReviewRevisionRequest struct {
	PreviousSubmissionKey string
	Reason                string
	ExecutionRequired     bool
	Files                 fileview.Reader
	Validator             CandidateValidator
	Authorize             func(state map[string]any, previous, successor workpacket.Packet) error
}

func FindSubmissionKey(state map[string]any, key string) (SubmissionRecord, bool, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["submission_key"] == key {
			return FindSubmission(state, stringValue(row["packet_id"]))
		}
	}
	return SubmissionRecord{}, false, nil
}

func revisionPacket(state map[string]any, req DocumentReviewRevisionRequest) (workpacket.Packet, workpacket.Packet, bool, error) {
	r, ok, err := FindSubmissionKey(state, req.PreviousSubmissionKey)
	if err != nil || !ok {
		return workpacket.Packet{}, workpacket.Packet{}, false, fmt.Errorf("revision requires an original received submission: %v", err)
	}
	old, ok, err := FindWorkPacket(state, r.PacketID)
	if err != nil || !ok || r.Key != workpacket.Digest([]byte("submission:"+old.ID)) || r.Owner != old.Binding.Owner {
		return old, workpacket.Packet{}, false, fmt.Errorf("previous reception binding is invalid")
	}
	if r.RevisionPacketID != "" {
		next, ok, err := FindWorkPacket(state, r.RevisionPacketID)
		if err != nil || !ok {
			return old, next, false, fmt.Errorf("previous revision has no issued packet")
		}
		b := old.Binding
		b.Attempt++
		b.PreviousSubmissionKey = r.Key
		b.RevisionReason = req.Reason
		b.ExecutionRequired = next.Binding.ExecutionRequired
		expected, err := workpacket.RebuildS5ForBinding(b, old)
		if err != nil || !reflect.DeepEqual(expected, next) {
			return old, next, false, ErrRevisionConflict
		}
		return old, next, true, nil
	}
	b := old.Binding
	b.Attempt++
	b.PreviousSubmissionKey = r.Key
	b.RevisionReason = req.Reason
	b.ExecutionRequired = req.ExecutionRequired || old.Binding.ExecutionRequired
	next, err := workpacket.RebuildS5ForBinding(b, old)
	return old, next, false, err
}

func validateRevisionPrior(root string, state map[string]any, req DocumentReviewRevisionRequest, old workpacket.Packet) error {
	if err := ValidateWorkPacket(root, state, old, req.Files); err != nil {
		return err
	}
	if req.Authorize == nil {
		return fmt.Errorf("revision requires the trusted Main source validator")
	}
	r, ok, err := FindSubmission(state, old.ID)
	if err != nil || !ok || r.RevisionPacketID != "" || r.SupersededBy != "" {
		return fmt.Errorf("revision previous reception is no longer current")
	}
	if err := validateRevisionReception(state, old); err != nil {
		return err
	}
	// Do not require generic PLAN activation here. Ordinary S5
	// plan_checkpoint reviews are consumed directly from their issued domain
	// packet; the trusted Main Authorize callback still applies the original
	// approval-mode check and validates the exact source under the Writer.
	items, _ := state["evidence"].([]any)
	for _, raw := range items {
		e, _ := raw.(map[string]any)
		if e["id"] != r.EvidenceID {
			continue
		}
		if e["kind"] != "document_review" || e["status"] != "valid" {
			return fmt.Errorf("previous document review is not currently qualified")
		}
		data, err := workpacket.ReadFile(root, stringValue(e["path"]), 1<<20)
		if err != nil || workpacket.Digest(data) != e["sha256"] {
			return fmt.Errorf("previous document review artifact is unverifiable")
		}
		var envelope workpacket.DocumentReviewEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return err
		}
		answer := workpacket.Answer{Conclusion: &envelope.Conclusion, Findings: envelope.Findings}
		if err := workpacket.ValidateDocumentReview(old, r.EvidenceID, answer, data); err != nil {
			return err
		}
		hash, err := workpacket.AnswerDigest(answer)
		if err != nil || hash != r.ContentHash {
			return fmt.Errorf("previous review does not match the received content")
		}
		return nil
	}
	return fmt.Errorf("previous reception has no domain evidence")
}

// RecordDocumentReviewRevision suspends the previous evidence at issuance.
// The original gates therefore cannot consume its PASS while a new answer is
// pending. Issuance, lineage, old followup retirement and source retirement
// commit together through the original Writer.
func RecordDocumentReviewRevision(root, statePath, journalPath string, req DocumentReviewRevisionRequest) (Snapshot, workpacket.Packet, error) {
	if strings.TrimSpace(req.Reason) == "" || len(req.Reason) > 1024 {
		return Snapshot{}, workpacket.Packet{}, fmt.Errorf("revision reason must be concrete and <=1024 bytes")
	}
	store := NewWriter(statePath, journalPath, root, req.Validator)
	snap, err := store.Snapshot()
	if err != nil {
		return Snapshot{}, workpacket.Packet{}, err
	}
	old, next, replay, err := revisionPacket(snap.State, req)
	if err != nil {
		return Snapshot{}, workpacket.Packet{}, err
	}
	if replay {
		return snap, next, nil
	}
	if err := validateRevisionPrior(root, snap.State, req, old); err != nil {
		return Snapshot{}, workpacket.Packet{}, err
	}
	event := "evt-document-revision-" + req.PreviousSubmissionKey
	result, err := store.UpdateCurrent(Mutation{EventID: event, TransitionID: "DOCUMENT-REVIEW-REVISION", Event: "document_review_revision_issued", Actor: "orchestrator", IdempotencyKey: "document-review-revision:" + req.PreviousSubmissionKey, RuntimeID: old.Binding.RuntimeID, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		previous, successor, replay, err := revisionPacket(state, req)
		if err != nil {
			return err
		}
		if replay {
			return errSubmissionAccepted
		}
		if !reflect.DeepEqual(previous, old) || !reflect.DeepEqual(successor, next) {
			return fmt.Errorf("revision origin changed")
		}
		if err := validateRevisionPrior(root, state, req, previous); err != nil {
			return err
		}
		if err := req.Authorize(state, previous, successor); err != nil {
			return err
		}
		row, err := packetValue(successor)
		if err != nil {
			return err
		}
		entities := state["entities"].(map[string]any)
		packets, _ := entities["work_packets"].([]any)
		entities["work_packets"] = append(packets, row)
		priorRow := submissionRow(state, req.PreviousSubmissionKey)
		priorRow["revision_packet_id"] = successor.ID
		if priorRow["control_status"] == "pending" {
			priorRow["control_status"] = "replaced"
		}
		return invalidateRevisionEvidence(state, stringValue(priorRow["evidence_id"]), event, "document_review_revision_pending", req.Reason)
	}})
	if err != nil {
		latest, readErr := store.Snapshot()
		if readErr == nil {
			_, successor, replay, checkErr := revisionPacket(latest.State, req)
			if checkErr != nil {
				return Snapshot{}, workpacket.Packet{}, checkErr
			}
			if replay {
				return latest, successor, nil
			}
		}
		return Snapshot{}, workpacket.Packet{}, err
	}
	return result, next, nil
}

func submissionRow(state map[string]any, key string) map[string]any {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["submission_key"] == key {
			return row
		}
	}
	return nil
}

func validateRevisionReception(state map[string]any, p workpacket.Packet) error {
	if r, ok, err := FindSubmission(state, p.ID); err != nil {
		return err
	} else if ok && r.RevisionPacketID != "" {
		return ErrSubmissionSuperseded
	}
	if p.Binding.Attempt == 1 {
		return nil
	}
	prior, ok, err := FindSubmissionKey(state, p.Binding.PreviousSubmissionKey)
	if err != nil || !ok || prior.RevisionPacketID != p.ID {
		return fmt.Errorf("revision packet has no original successor binding")
	}
	old, ok, err := FindWorkPacket(state, prior.PacketID)
	if err != nil || !ok {
		return fmt.Errorf("revision packet has no previous packet")
	}
	b := old.Binding
	b.Attempt++
	b.PreviousSubmissionKey = prior.Key
	b.RevisionReason = p.Binding.RevisionReason
	b.ExecutionRequired = p.Binding.ExecutionRequired
	expected, err := workpacket.RebuildS5ForBinding(b, old)
	if err != nil || !reflect.DeepEqual(expected, p) {
		return fmt.Errorf("revision frozen chain differs from the original reception")
	}
	if prior.SupersededBy != "" && prior.SupersededBy != workpacket.Digest([]byte("submission:"+p.ID)) {
		return fmt.Errorf("revision previous reception was superseded by another submission")
	}
	return nil
}

func supersedePreviousSubmission(state map[string]any, record SubmissionRecord) error {
	if _, taskPlanning, err := FindTaskPlanningPacket(state, record.PacketID); err != nil {
		return err
	} else if taskPlanning {
		return nil
	}
	if _, contract, err := FindContractPacket(state, record.PacketID); err != nil {
		return err
	} else if contract {
		return nil
	}
	if _, design, err := FindDesignPacket(state, record.PacketID); err != nil {
		return err
	} else if design {
		return nil
	}
	p, ok, err := FindWorkPacket(state, record.PacketID)
	if err != nil || !ok {
		return fmt.Errorf("received submission has no packet")
	}
	if err := validateRevisionReception(state, p); err != nil {
		return err
	}
	if p.Binding.Attempt == 1 {
		return nil
	}
	row := submissionRow(state, p.Binding.PreviousSubmissionKey)
	if row == nil {
		return fmt.Errorf("revision previous reception is missing")
	}
	row["superseded_by"] = record.Key
	return invalidateRevisionEvidence(state, stringValue(row["evidence_id"]), record.EvidenceID, "document_review_revision_superseded", p.Binding.RevisionReason)
}

func invalidateRevisionEvidence(state map[string]any, id, by, rule, reason string) error {
	items, _ := state["evidence"].([]any)
	for _, raw := range items {
		e, _ := raw.(map[string]any)
		if e["id"] == id {
			e["status"] = "invalid"
			e["invalidated_by"] = by
			e["invalidation_rule"] = rule
			e["invalidation_reason"] = reason
			return nil
		}
	}
	return fmt.Errorf("revision previous evidence is missing")
}

// CurrentDocumentReviewEvidence reports whether a document-review evidence
// row belongs to the current typed S5 responsibility and its accepted
// revision tip. Runtimes without any typed S5 issuance retain their original
// evidence behavior.
func CurrentDocumentReviewEvidence(state map[string]any, evidenceID string) bool {
	if evidenceID == "" {
		return false
	}
	var evidence map[string]any
	items, _ := state["evidence"].([]any)
	for _, raw := range items {
		row, _ := raw.(map[string]any)
		if row != nil && row["id"] == evidenceID {
			if evidence != nil {
				return false
			}
			evidence = row
		}
	}
	if evidence == nil || evidence["kind"] != "document_review" || evidence["status"] != "valid" {
		return false
	}
	entities, _ := state["entities"].(map[string]any)
	packets, _ := entities["work_packets"].([]any)
	hasTypedS5 := false
	for _, raw := range packets {
		row, _ := raw.(map[string]any)
		if row != nil && row["template_id"] == "s5-document-review-v1" {
			hasTypedS5 = true
			break
		}
	}
	var record SubmissionRecord
	recordFound := false
	submissions, _ := entities["submissions"].([]any)
	for _, raw := range submissions {
		row, _ := raw.(map[string]any)
		if row == nil || row["evidence_id"] != evidenceID {
			continue
		}
		data, err := json.Marshal(row)
		if err != nil || json.Unmarshal(data, &record) != nil || recordFound {
			return false
		}
		recordFound = true
	}
	if !recordFound {
		return !hasTypedS5 && !hasTaskPlanningPacket(state)
	}
	p, found, err := FindWorkPacket(state, record.PacketID)
	if err != nil || !found {
		return false
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation, generationErr := integerField(baseline, "generation")
	b := p.Binding
	if generationErr != nil || record.PacketID != p.ID || record.Owner != b.Owner || record.Key != workpacket.Digest([]byte("submission:"+p.ID)) || record.EvidenceID != evidenceID || b.RuntimeID != stringValue(state["runtime_id"]) || b.Generation != generation || record.RevisionPacketID != "" || record.SupersededBy != "" {
		return false
	}
	life, _ := state["lifecycle"].(map[string]any)
	entry, entryErr := integerField(life, "phase_revision")
	if entryErr != nil || life["state"] != "document_verification" || entry != b.StageEntry || record.OriginState != "document_verification" || record.OriginPhase != "" || record.OriginEntry != b.StageEntry || record.ControlStatus == "replaced" {
		return false
	}
	evidenceGeneration, evidenceGenerationErr := integerField(evidence, "baseline_generation")
	if evidenceGenerationErr != nil || evidenceGeneration != generation {
		return false
	}
	if err := validateRevisionReception(state, p); err != nil {
		return false
	}
	teams, _ := entities["teams"].([]any)
	currentTeam := false
	latestEntry := -1
	for _, raw := range teams {
		team, _ := raw.(map[string]any)
		teamEntry, entryErr := integerField(team, "packet_stage_entry")
		teamGeneration, generationErr := integerField(team, "packet_generation")
		if team == nil || team["kind"] != "document_verifier" || (team["status"] != "planned" && team["status"] != "active") || generationErr != nil || teamGeneration != b.Generation || entryErr != nil {
			continue
		}
		if teamEntry > latestEntry {
			latestEntry = teamEntry
		}
		if teamEntry == b.StageEntry && containsString(team["agent_ids"], b.Owner) && containsString(team["responsibility_ids"], b.Responsibility) {
			currentTeam = true
		}
	}
	return currentTeam && b.StageEntry == latestEntry
}

// ValidateCurrentDocumentReviewRevisions closes the manual TR-003 boundary
// too: a pending or non-PASS current revision cannot be replaced by citing the
// other reviewer's PASS. Initial legacy attestations retain their contract.
func ValidateCurrentDocumentReviewRevisions(root string, state map[string]any, files fileview.Reader) error {
	if !PacketEnabled(state) {
		return nil
	}
	if hasTaskPlanningPacket(state) {
		return validateCurrentTypedS5Passes(root, state, files)
	}
	life, _ := state["lifecycle"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	entry, err := integerField(life, "phase_revision")
	if err != nil {
		return err
	}
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return err
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("revision packet registry contains a non-object row")
		}
		// The registry also retains typed planning and execution packets.
		// Only S5 packets can carry document-review revision authority.
		if row["template_id"] != "s5-document-review-v1" {
			continue
		}
		p, ok, err := FindWorkPacket(state, stringValue(row["packet_id"]))
		if err != nil || !ok {
			return fmt.Errorf("revision packet cannot be resolved")
		}
		if p.Binding.Attempt == 1 || p.Binding.RuntimeID != state["runtime_id"] || p.Binding.Generation != generation || p.Binding.StageEntry != entry {
			continue
		}
		record, received, err := FindSubmission(state, p.ID)
		if err != nil {
			return err
		}
		if received && record.RevisionPacketID != "" {
			continue
		}
		if !received {
			return fmt.Errorf("current named revision has not been received")
		}
		if err := validateRevisionReception(state, p); err != nil {
			return err
		}
		if err := ValidateWorkPacket(root, state, p, files); err != nil {
			return err
		}
		items, _ := state["evidence"].([]any)
		qualified := false
		for _, raw := range items {
			e, _ := raw.(map[string]any)
			if e["id"] != record.EvidenceID {
				continue
			}
			if e["status"] != "valid" || e["kind"] != "document_review" {
				return fmt.Errorf("current revision evidence is not valid")
			}
			data, err := workpacket.ReadFile(root, stringValue(e["path"]), 1<<20)
			if err != nil || workpacket.Digest(data) != e["sha256"] {
				return fmt.Errorf("current revision artifact is unverifiable")
			}
			var envelope workpacket.DocumentReviewEnvelope
			if err := json.Unmarshal(data, &envelope); err != nil {
				return err
			}
			answer := workpacket.Answer{Conclusion: &envelope.Conclusion, Findings: envelope.Findings}
			if err := workpacket.ValidateDocumentReview(p, record.EvidenceID, answer, data); err != nil {
				return err
			}
			hash, err := workpacket.AnswerDigest(answer)
			if err != nil || hash != record.ContentHash || envelope.Conclusion != "pass" {
				return fmt.Errorf("current revision has no received PASS")
			}
			qualified = true
			break
		}
		if !qualified {
			return fmt.Errorf("current revision has no domain evidence")
		}
	}
	return nil
}

func validateCurrentTypedS5Passes(root string, state map[string]any, files fileview.Reader) error {
	if files == nil {
		files = fileview.Disk{Root: root}
	}
	packets, err := S5Packets(root, state, "")
	if err != nil {
		return fmt.Errorf("resolve current S5 reviewer responsibilities: %w", err)
	}
	if len(packets) != 2 {
		return fmt.Errorf("current S4 batch requires both independent S5 review responsibilities; found %d", len(packets))
	}
	required := map[string]bool{"DV-SPEC-CONSISTENCY": false, "DV-TASK-EXECUTABILITY": false}
	owners := map[string]bool{}
	for _, p := range packets {
		responsibility := p.Binding.Responsibility
		if _, ok := required[responsibility]; !ok || required[responsibility] {
			return fmt.Errorf("current S5 batch has a missing or duplicate review responsibility %q", responsibility)
		}
		if p.Binding.Owner == "" || owners[p.Binding.Owner] {
			return fmt.Errorf("current S5 review responsibilities must have different owners")
		}
		required[responsibility] = true
		owners[p.Binding.Owner] = true
		if err := validateCurrentTypedS5Pass(root, state, p, files); err != nil {
			return fmt.Errorf("current S5 %s: %w", responsibility, err)
		}
	}
	for responsibility, found := range required {
		if !found {
			return fmt.Errorf("current S5 batch is missing %s", responsibility)
		}
	}
	return nil
}

func validateCurrentTypedS5Pass(root string, state map[string]any, p workpacket.Packet, files fileview.Reader) error {
	if err := ValidateWorkPacket(root, state, p, files); err != nil {
		return err
	}
	record, received, err := FindSubmission(state, p.ID)
	if err != nil {
		return err
	}
	if !received || record.Owner != p.Binding.Owner || record.RevisionPacketID != "" || record.SupersededBy != "" {
		return fmt.Errorf("current issued answer has not been received as the current tip")
	}
	if err := validateRevisionReception(state, p); err != nil {
		return err
	}
	items, _ := state["evidence"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil || item["id"] != record.EvidenceID {
			continue
		}
		if item["kind"] != "document_review" || item["status"] != "valid" {
			return fmt.Errorf("current review evidence is not valid")
		}
		data, err := workpacket.ReadFile(root, stringValue(item["path"]), 1<<20)
		if err != nil || workpacket.Digest(data) != item["sha256"] {
			return fmt.Errorf("current review artifact is unverifiable")
		}
		var envelope workpacket.DocumentReviewEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return err
		}
		answer := workpacket.Answer{Conclusion: &envelope.Conclusion, Findings: envelope.Findings}
		if err := workpacket.ValidateDocumentReview(p, record.EvidenceID, answer, data); err != nil {
			return err
		}
		digest, err := workpacket.AnswerDigest(answer)
		if err != nil || digest != record.ContentHash || envelope.Conclusion != "pass" {
			return fmt.Errorf("current review answer is not a received PASS")
		}
		return nil
	}
	return fmt.Errorf("current accepted submission has no domain evidence")
}
