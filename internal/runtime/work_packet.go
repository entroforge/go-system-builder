package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

var ErrSubmissionSuperseded = errors.New("submission has a named revision; use its current packet")

var ErrSubmissionConflict = errors.New("submission identity already accepted different content")
var errSubmissionAccepted = errors.New("submission already accepted")

type SubmissionRecord struct {
	Key               string `json:"submission_key"`
	PacketID          string `json:"packet_id"`
	Owner             string `json:"owner"`
	ContentHash       string `json:"content_sha256"`
	EvidenceID        string `json:"evidence_id"`
	FollowupID        string `json:"followup_event_id"`
	ControlStatus     string `json:"control_status"`
	OriginState       string `json:"origin_state"`
	OriginPhase       string `json:"origin_phase"`
	OriginEntry       int    `json:"origin_entry"`
	TransitionEventID string `json:"transition_event_id,omitempty"`
	RevisionPacketID  string `json:"revision_packet_id,omitempty"`
	SupersededBy      string `json:"superseded_by,omitempty"`
}

type SubmissionRequest struct {
	PacketID   string
	Owner      string
	AnswerJSON []byte
	// ContentHash, when provided by an internal adapter, is checked against
	// the canonical validated answer. It never supplies the stored digest.
	ContentHash string
	Files       fileview.Reader
	// Authorize is the trusted adapter's original source/assignment validator.
	// It runs again inside the domain Writer, against the candidate's actual
	// state. Platform payloads and authoring JSON cannot supply this capability.
	Authorize func(root string, state map[string]any) error
}

func RecordWorkPacket(root, statePath, journalPath string, packet workpacket.Packet, files fileview.Reader, validator CandidateValidator) (Snapshot, error) {
	return RecordWorkPacketContext(context.Background(), root, statePath, journalPath, packet, files, validator)
}

func RecordWorkPacketContext(ctx context.Context, root, statePath, journalPath string, packet workpacket.Packet, files fileview.Reader, validator CandidateValidator) (Snapshot, error) {
	store := NewWriter(statePath, journalPath, root, validator).WithContext(ctx)
	snapshot, err := store.Snapshot()
	if err != nil {
		return Snapshot{}, fmt.Errorf("read packet runtime: %w", err)
	}
	if err := ValidateWorkPacket(root, snapshot.State, packet, files); err != nil {
		return Snapshot{}, err
	}
	if prior, ok, err := FindWorkPacket(snapshot.State, packet.ID); err != nil {
		return Snapshot{}, err
	} else if ok {
		if !reflect.DeepEqual(prior, packet) {
			return Snapshot{}, fmt.Errorf("packet identity conflict")
		}
		return snapshot, nil
	}
	if packet.Binding.Attempt != 1 {
		return Snapshot{}, fmt.Errorf("new packet attempt requires a trusted document review revision")
	}
	row, err := packetValue(packet)
	if err != nil {
		return Snapshot{}, err
	}
	current := snapshot.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": current["state"], "phase": current["phase"]}
	result, err := store.UpdateCurrent(Mutation{EventID: fmt.Sprintf("evt-packet-%s-r%d", packet.ID, snapshot.Revision+1), TransitionID: "PACKET-ISSUE", Event: "work_packet_issued", Actor: "orchestrator", IdempotencyKey: "packet:" + packet.ID, RuntimeID: packet.Binding.RuntimeID, BaselineGeneration: packet.Binding.Generation, From: cursor, To: cursor, OccurredAt: time.Now().UTC(), RetainLastTransition: true, Apply: func(state map[string]any) error {
		if err := ValidateWorkPacket(root, state, packet, files); err != nil {
			return err
		}
		if _, ok, err := FindWorkPacket(state, packet.ID); err != nil {
			return err
		} else if ok {
			return errSubmissionAccepted
		}
		entities := state["entities"].(map[string]any)
		rows, _ := entities["work_packets"].([]any)
		if err := validateDocumentReviewPacketSlot(rows, packet); err != nil {
			return err
		}
		entities["work_packets"] = append(rows, row)
		return nil
	}})
	if err != nil {
		// A concurrent identical issuer may encounter the first event before
		// Apply. Only an exact durable row proves that issuance succeeded.
		latest, readErr := store.Snapshot()
		if readErr == nil {
			prior, ok, findErr := FindWorkPacket(latest.State, packet.ID)
			if findErr == nil && ok && reflect.DeepEqual(prior, packet) {
				return latest, nil
			}
		}
		return Snapshot{}, fmt.Errorf("record work packet: %w", err)
	}
	return result, nil
}

// validateDocumentReviewPacketSlot compares only the original S5 subtype.
// Other registered domains have distinct bindings and no S5 stage_entry.
func validateDocumentReviewPacketSlot(rows []any, packet workpacket.Packet) error {
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("Runtime work packet is not an object")
		}
		if row["template_id"] != "s5-document-review-v1" {
			continue
		}
		binding, _ := row["binding"].(map[string]any)
		entry, err := integerField(binding, "stage_entry")
		if err != nil {
			return err
		}
		if binding["owner"] == packet.Binding.Owner && binding["responsibility"] == packet.Binding.Responsibility && entry == packet.Binding.StageEntry && binding["runtime_id"] == packet.Binding.RuntimeID && numberBindingGeneration(binding) == packet.Binding.Generation {
			return fmt.Errorf("responsibility already has an issued packet; use a trusted revision")
		}
	}
	return nil
}

func PacketEnabled(state map[string]any) bool {
	configuration, _ := state["configuration"].(map[string]any)
	optimization, _ := configuration["optimization"].(map[string]any)
	return workstate.SupportsAssignments(state["schema_version"]) && optimization["packet"] == true
}

func FindWorkPacket(state map[string]any, id string) (workpacket.Packet, bool, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	var found workpacket.Packet
	seen := false
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["packet_id"] != id {
			continue
		}
		if seen {
			return workpacket.Packet{}, false, fmt.Errorf("duplicate Runtime work packet ID %q", id)
		}
		seen = true
		if row["template_id"] != "s5-document-review-v1" {
			return workpacket.Packet{}, false, fmt.Errorf("work packet %q has subtype %q; use its typed lookup", id, row["template_id"])
		}
		data, err := json.Marshal(row)
		if err != nil {
			return workpacket.Packet{}, false, fmt.Errorf("encode packet row: %w", err)
		}
		var packet workpacket.Packet
		if err := json.Unmarshal(data, &packet); err != nil {
			return packet, false, fmt.Errorf("decode packet row: %w", err)
		}
		found = packet
	}
	if !seen {
		return workpacket.Packet{}, false, nil
	}
	return found, true, nil
}

func FindSubmission(state map[string]any, packetID string) (SubmissionRecord, bool, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["packet_id"] != packetID {
			continue
		}
		data, err := json.Marshal(row)
		if err != nil {
			return SubmissionRecord{}, false, fmt.Errorf("encode submission row: %w", err)
		}
		var record SubmissionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return record, false, fmt.Errorf("decode submission row: %w", err)
		}
		return record, true, nil
	}
	return SubmissionRecord{}, false, nil
}

func prepareSubmission(root string, state map[string]any, req EvidenceRequest, envelopeData []byte) (SubmissionRecord, error) {
	s := req.Submission
	if _, taskPlanning, err := FindTaskPlanningPacket(state, s.PacketID); err != nil {
		return SubmissionRecord{}, err
	} else if taskPlanning {
		return prepareTaskPlanningSubmission(root, state, req, envelopeData)
	}
	if _, contract, err := FindContractPacket(state, s.PacketID); err != nil {
		return SubmissionRecord{}, err
	} else if contract {
		return prepareContractSubmission(root, state, req, envelopeData)
	}
	if s == nil {
		return SubmissionRecord{}, nil
	}
	if _, design, err := FindDesignPacket(state, s.PacketID); err != nil {
		return SubmissionRecord{}, err
	} else if design {
		return prepareDesignSubmission(root, state, req, envelopeData)
	}
	if req.Kind != "document_review" || len(req.ProducedBy) != 1 || req.ProducedBy[0] != s.Owner || req.ReviewRound != nil {
		return SubmissionRecord{}, fmt.Errorf("invalid document submission binding")
	}
	p, ok, err := FindWorkPacket(state, s.PacketID)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if !ok || p.Binding.Owner != s.Owner || p.Binding.Responsibility != req.ResponsibilityID {
		return SubmissionRecord{}, fmt.Errorf("submission owner or responsibility does not match issued packet")
	}
	answer, issues := workpacket.ValidateAnswer(p, s.AnswerJSON)
	if len(issues) > 0 {
		return SubmissionRecord{}, fmt.Errorf("submission answer %s: %s (%s)", issues[0].Pointer, issues[0].Expected, issues[0].Observed)
	}
	contentHash, err := workpacket.AnswerDigest(answer)
	if err != nil {
		return SubmissionRecord{}, fmt.Errorf("hash validated answer: %w", err)
	}
	if s.ContentHash != "" && s.ContentHash != contentHash {
		return SubmissionRecord{}, fmt.Errorf("submission content hash does not match validated answer")
	}
	prior, accepted, err := FindSubmission(state, s.PacketID)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if accepted {
		if prior.RevisionPacketID != "" {
			return prior, ErrSubmissionSuperseded
		}
		if prior.ContentHash != contentHash {
			return prior, ErrSubmissionConflict
		}
		if prior.Owner != s.Owner || prior.EvidenceID != req.ID {
			return prior, fmt.Errorf("submission replay differs from original domain reference")
		}
		if err := workpacket.ValidateDocumentReview(p, req.ID, answer, envelopeData); err != nil {
			return prior, err
		}
		items, _ := state["evidence"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item["id"] == prior.EvidenceID && item["kind"] == req.Kind && item["path"] == req.Path && item["sha256"] == workpacket.Digest(envelopeData) {
				return prior, errSubmissionAccepted
			}
		}
		return prior, fmt.Errorf("submission replay differs from original evidence artifact")
	}
	if err := workpacket.ValidateDocumentReview(p, req.ID, answer, envelopeData); err != nil {
		return SubmissionRecord{}, err
	}
	if err := validateRevisionReception(state, p); err != nil {
		return SubmissionRecord{}, err
	}
	if err := ValidateWorkPacket(root, state, p, s.Files); err != nil {
		return SubmissionRecord{}, err
	}
	if s.Authorize == nil {
		return SubmissionRecord{}, fmt.Errorf("submission requires the original S5 source and assignment validator")
	}
	if err := s.Authorize(root, state); err != nil {
		return SubmissionRecord{}, fmt.Errorf("submission authorization: %w", err)
	}
	key := workpacket.Digest([]byte("submission:" + p.ID))
	return SubmissionRecord{Key: key, PacketID: p.ID, Owner: s.Owner, ContentHash: contentHash, EvidenceID: req.ID, FollowupID: workpacket.Digest([]byte(key + ":after-domain")), ControlStatus: "pending", OriginState: "document_verification", OriginPhase: "", OriginEntry: p.Binding.StageEntry}, nil
}

func appendSubmission(state map[string]any, record SubmissionRecord) error {
	if err := supersedePreviousSubmission(state, record); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode accepted submission: %w", err)
	}
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		return fmt.Errorf("decode accepted submission: %w", err)
	}
	entities := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	entities["submissions"] = append(rows, row)
	return nil
}

func packetValue(packet workpacket.Packet) (map[string]any, error) {
	data, err := json.Marshal(packet)
	if err != nil {
		return nil, fmt.Errorf("encode issued packet: %w", err)
	}
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		return nil, fmt.Errorf("decode issued packet: %w", err)
	}
	return row, nil
}
