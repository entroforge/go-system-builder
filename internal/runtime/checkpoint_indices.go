package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// ValidateCheckpointIndices verifies the saved Runtime and journal form a
// coherent pair. Already received responsibilities remain original facts;
// recovery does not authenticate their sources or recompute their decisions.
func ValidateCheckpointIndices(root string, state map[string]any, journalBytes []byte) error {
	return ValidateCheckpointIndicesContext(context.Background(), root, state, journalBytes)
}

func ValidateCheckpointIndicesContext(ctx context.Context, root string, state map[string]any, journalBytes []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateRecoveryPair(state, journalBytes); err != nil {
		return err
	}
	return validateReceptionPairing(state, journalBytes)
}

// validateReceptionPairing restores the recorded pairing between accepted
// document-review receptions and the packet index.
//
// The blueprint requires both halves in one sentence: "checkpoint 恢复和具名历史
// 继续按故障模型检查实际保存材料、版本和事务配对;Hook 不对任何阶段计算或认证
// Git OID、来源、答卷或证据散列" (blueprint/l4-mechanisms/L4-runtime-control-plane.md:72),
// and "普通观察先读取状态与索引,检查索引结构和身份唯一性" (same file:70).
//
// Every fact checked here is a recorded identity or a recorded transaction
// pairing:
//
//   - a reception names one indexed packet (orphan reception),
//   - one packet and one reception identity are accepted once (duplicate key),
//   - an advanced reception names the real journal transition that advanced it
//     from its recorded origin state (forged transition),
//   - a revised reception names the indexed revision packet bound to it, and
//     its old review stays invalidated (old review resurrection).
//
// No content digest, checksum, file hash or artifact body is recomputed, read
// or compared here: the submission key is the recorded reception identity
// ("submission:" + packet ID, internal/runtime/work_packet.go:343), not a
// digest of the saved bytes, and comparing it with the revision packet's
// recorded previous_submission_key is a transaction pairing, not a hash gate.
func validateReceptionPairing(state map[string]any, journalBytes []byte) error {
	if !hasIssuedIndices(state) {
		return nil
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	if len(rows) == 0 {
		return nil
	}
	packets, err := documentReviewPackets(state)
	if err != nil {
		return err
	}
	if len(packets) == 0 {
		return nil
	}
	journal, err := inspectJournalData(journalBytes)
	if err != nil {
		return err
	}
	evidence := checkpointEvidenceRows(state)
	received := map[string]SubmissionRecord{}
	keys := map[string]string{}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("checkpoint reception row is unreadable")
		}
		record, err := decodeSubmissionRow(row)
		if err != nil {
			return err
		}
		if !isDocumentReviewReception(packets, record) {
			continue
		}
		if _, exists := packets[record.PacketID]; !exists {
			return fmt.Errorf("orphan checkpoint reception %q has no indexed packet", record.PacketID)
		}
		if previous, duplicate := received[record.PacketID]; duplicate {
			return fmt.Errorf("duplicate checkpoint reception for packet %q (%q and %q)", record.PacketID, previous.Key, record.Key)
		}
		if owner, duplicate := keys[record.Key]; duplicate {
			return fmt.Errorf("duplicate checkpoint reception identity %q (%q and %q)", record.Key, owner, record.PacketID)
		}
		if record.ControlStatus == "advanced" {
			if err := checkAdvancedReception(journal, record); err != nil {
				return err
			}
		}
		keys[record.Key] = record.PacketID
		received[record.PacketID] = record
	}
	for _, record := range received {
		if record.RevisionPacketID == "" {
			continue
		}
		next, exists := packets[record.RevisionPacketID]
		if !exists {
			return fmt.Errorf("checkpoint revision %q has no indexed packet for the original reception %q", record.RevisionPacketID, record.Key)
		}
		if next.Binding.PreviousSubmissionKey != record.Key {
			return fmt.Errorf("checkpoint revision %q is not bound to the original reception %q", next.ID, record.Key)
		}
		row, exists := evidence[record.EvidenceID]
		if !exists {
			return fmt.Errorf("checkpoint reception %q has no recorded evidence %q", record.Key, record.EvidenceID)
		}
		if row["status"] != "invalid" {
			return fmt.Errorf("checkpoint revision resurrects the old review %q", record.EvidenceID)
		}
	}
	return nil
}

// documentReviewPackets indexes the document-review packets of this Runtime.
// Other packet families keep their own typed lookups; this index only carries
// the S5 document-review packets whose receptions are checked here.
func documentReviewPackets(state map[string]any) (map[string]workpacket.Packet, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	packets := map[string]workpacket.Packet{}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if row["template_id"] != "s5-document-review-v1" {
			continue
		}
		id := stringValue(row["packet_id"])
		if id == "" {
			continue
		}
		packet, found, err := FindWorkPacket(state, id)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		packets[id] = packet
	}
	return packets, nil
}

// isDocumentReviewReception reports whether the recorded reception belongs to
// the document-review family. The family is recognised by the indexed packet
// type, and by the origin state that only this family records.
func isDocumentReviewReception(packets map[string]workpacket.Packet, record SubmissionRecord) bool {
	if _, ours := packets[record.PacketID]; ours {
		return true
	}
	return record.OriginState == "document_verification"
}

// checkAdvancedReception requires an advanced reception to name the real
// journal transition that advanced it, and to name the state that transition
// started from. A forged transition event id, or a real event that belongs to
// another transition, cannot satisfy this pairing.
func checkAdvancedReception(journal journalInspection, record SubmissionRecord) error {
	if record.TransitionEventID == "" {
		return fmt.Errorf("advanced checkpoint reception %q names no transition event", record.Key)
	}
	index, ok := journal.EventIndex[record.TransitionEventID]
	if !ok {
		return fmt.Errorf("advanced checkpoint reception %q has no actual transition event %q", record.Key, record.TransitionEventID)
	}
	event := journal.Events[index]
	from, _ := event["from"].(map[string]any)
	if record.OriginState != "" && from["state"] != record.OriginState {
		return fmt.Errorf("advanced checkpoint reception %q is not bound to its recorded origin state %q", record.Key, record.OriginState)
	}
	return nil
}

// checkpointEvidenceRows indexes the recorded evidence rows by their identity.
func checkpointEvidenceRows(state map[string]any) map[string]map[string]any {
	rows, _ := state["evidence"].([]any)
	evidence := map[string]map[string]any{}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := stringValue(row["id"])
		if id == "" {
			continue
		}
		evidence[id] = row
	}
	return evidence
}

// decodeSubmissionRow decodes one recorded reception row with the same shape
// the production typed lookup uses.
func decodeSubmissionRow(row map[string]any) (SubmissionRecord, error) {
	data, err := json.Marshal(row)
	if err != nil {
		return SubmissionRecord{}, fmt.Errorf("encode checkpoint reception row: %w", err)
	}
	var record SubmissionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return record, fmt.Errorf("decode checkpoint reception row: %w", err)
	}
	return record, nil
}
