package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
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
	// EvidenceJSON is the original consumer's prepared professional report.
	// Its actual path is chosen exclusively by the existing Evidence Writer.
	EvidenceJSON []byte
	Files        fileview.Reader
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
	if prior, ok, err := FindWorkPacket(snapshot.State, packet.ID); err != nil {
		return Snapshot{}, err
	} else if ok {
		if !sameS5IssuedResponsibility(prior, packet) {
			return Snapshot{}, fmt.Errorf("packet identity conflict")
		}
		return snapshot, nil
	}
	if err := ValidateWorkPacket(root, snapshot.State, packet, files); err != nil {
		return Snapshot{}, err
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
	// G18: the packet id is the packet's own identity, and the idempotency key
	// already names it. The event id follows the same shape as the other
	// packet issuances (`evt-contract-packet-<id>`): no Runtime write count.
	// The preparation attempt is the questionnaire's own domain index, so a
	// responsibility that is prepared again keeps its own identity.
	attempt, err := packetPreparationAttempt(ctx, journalPath, packet.ID)
	if err != nil {
		return Snapshot{}, err
	}
	result, err := store.UpdateCurrent(Mutation{EventID: fmt.Sprintf("evt-packet-%s-%d", packet.ID, attempt), TransitionID: "PACKET-ISSUE", Event: "work_packet_issued", Actor: "orchestrator", IdempotencyKey: fmt.Sprintf("packet:%s:%d", packet.ID, attempt), RuntimeID: packet.Binding.RuntimeID, BaselineGeneration: packet.Binding.Generation, From: cursor, To: cursor, OccurredAt: time.Now().UTC(), RetainLastTransition: true, Apply: func(state map[string]any) error {
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
			if findErr == nil && ok && sameS5IssuedResponsibility(prior, packet) {
				return latest, nil
			}
		}
		return Snapshot{}, fmt.Errorf("record work packet: %w", err)
	}
	return result, nil
}

// packetPreparationAttempt counts the preparations this responsibility's
// questionnaire already has inside this responsibility, as the Runtime journal
// records them.
//
// The Runtime state row is only the projection of that fact. A partially
// issued responsibility can lose its row while the journal keeps the
// issuance, and the domain then still proves that the questionnaire was
// prepared before. The new preparation is therefore the next attempt of that
// domain thing rather than a duplicate of the first one, so it needs its own
// identity. The attempt number is the questionnaire's own domain index; it is
// never the Runtime write count, a digest or a wall-clock value.
//
// The count is taken from the same journal the store validates the Runtime
// against, archived segments included, so a rotated journal still counts its
// preparations. It is taken before the writer takes its lock, so an issuance
// that was interrupted before it reached the journal is not counted yet; the
// store's own recovery then reconciles that pending write and the journal's
// uniqueness check refuses the duplicate, which keeps the domain's outcome
// instead of a second issuance.
func packetPreparationAttempt(ctx context.Context, journalPath, packetID string) (int, error) {
	if journalPath == "" {
		return 1, nil
	}
	inspection, err := inspectJournalContext(ctx, journalPath)
	if err != nil {
		return 0, fmt.Errorf("read runtime journal for the packet preparation attempt: %w", err)
	}
	prefix := "evt-packet-" + packetID + "-"
	prepared := 0
	for _, event := range inspection.Events {
		if event["transition_id"] != "PACKET-ISSUE" {
			continue
		}
		eventID, _ := event["event_id"].(string)
		if !strings.HasPrefix(eventID, prefix) {
			continue
		}
		prepared++
	}
	return prepared + 1, nil
}

// Replays use the Writer's recorded paths and original assignment; presentation
// or an independently prepared candidate path is not another authority test.
func sameS5IssuedResponsibility(a, b workpacket.Packet) bool {
	x, y := a.Binding, b.Binding
	return a.ID == b.ID && a.TemplateID == b.TemplateID &&
		x.RuntimeID == y.RuntimeID && x.Generation == y.Generation &&
		x.StageEntry == y.StageEntry && x.Owner == y.Owner &&
		x.AssignmentID == y.AssignmentID && x.Responsibility == y.Responsibility &&
		x.Attempt == y.Attempt && x.PreviousSubmissionKey == y.PreviousSubmissionKey
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
	if req.Kind != "document_review" || len(req.ProducedBy) != 1 || req.ProducedBy[0] != s.Owner {
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
	prior, accepted, err := FindSubmission(state, s.PacketID)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if accepted {
		if prior.RevisionPacketID != "" {
			return prior, ErrSubmissionSuperseded
		}
		if prior.Owner != s.Owner || prior.EvidenceID != req.ID {
			return prior, ErrSubmissionConflict
		}
		items, _ := state["evidence"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item["id"] != prior.EvidenceID || item["kind"] != req.Kind {
				continue
			}
			original, err := ReadRecordedEvidenceReport(root, stringValue(item["path"]))
			if err != nil {
				return prior, fmt.Errorf("accepted document review is unavailable: %w", err)
			}
			if err := validateS5AcceptedAnswer(p, prior.EvidenceID, answer, original); err != nil {
				return prior, err
			}
			return prior, errSubmissionAccepted
		}
		return prior, fmt.Errorf("accepted document review reference is unavailable")
	}
	if err := validateS5AcceptedAnswer(p, req.ID, answer, envelopeData); err != nil {
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
	key := "submission:" + p.ID
	return SubmissionRecord{Key: key, PacketID: p.ID, Owner: s.Owner, EvidenceID: req.ID, FollowupID: key + ":after-domain", ControlStatus: "pending", OriginState: "document_verification", OriginPhase: "", OriginEntry: p.Binding.StageEntry}, nil
}

// validateS5AcceptedAnswer compares the actual professional result, not
// historical subject digests, when a live responsibility is observed again.
func validateS5AcceptedAnswer(p workpacket.Packet, id string, answer workpacket.Answer, data []byte) error {
	var received workpacket.DocumentReviewEnvelope
	if err := json.Unmarshal(data, &received); err != nil {
		return fmt.Errorf("decode document review: %w", err)
	}
	if received.EvidenceID != id || received.Kind != "document_review" || received.RuntimeID != p.Binding.RuntimeID || received.ProducerAgentID != p.Binding.Owner || received.ProducerResponsibility != p.Binding.Responsibility {
		return fmt.Errorf("document review does not belong to the original responsibility")
	}
	if answer.Conclusion == nil || received.Conclusion != *answer.Conclusion || !slices.Equal(received.Findings, answer.Findings) {
		return ErrSubmissionConflict
	}
	return nil
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
