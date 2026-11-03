package runtime

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// RepairResultResolutionReceipt is a read-only projection of the immutable
// Main resolution message and its accepted blocker_resolved lifecycle CAS.
// It is a historical fact, not a current work packet or Resume authorization.
type RepairResultResolutionReceipt struct {
	Contract     string `json:"contract"`
	PacketID     string `json:"packet_id"`
	OwnerAgentID string `json:"owner_agent_id"`
	BlockerRef   string `json:"blocker_ref"`

	MessageRef string `json:"message_ref"`

	AcceptedEventID              string `json:"accepted_event_id"`
	AcceptedIdempotencyKey       string `json:"accepted_idempotency_key"`
	AcceptedAfterRuntimeRevision int    `json:"accepted_after_runtime_revision"`
	AcceptedSequence             int    `json:"accepted_sequence"`
}

// UnmarshalJSON retains the original receipt coordinates while allowing
// unrelated extensions. The receiver associates those coordinates with the
// Runtime, message and journal for the issued responsibility.
func (receipt *RepairResultResolutionReceipt) UnmarshalJSON(data []byte) error {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return fmt.Errorf("S9 resolution receipt JSON: %w", err)
	}
	var decoded repairResultResolutionReceiptWire
	if err := decodeRepairResultBlockerJSON(data, &decoded); err != nil {
		return fmt.Errorf("decode S9 resolution receipt: %w", err)
	}
	if decoded.Contract != "s9-repair-result-resolution-receipt-v1" || decoded.PacketID == "" || decoded.OwnerAgentID == "" || decoded.BlockerRef == "" || decoded.MessageRef == "" || decoded.AcceptedEventID == "" || decoded.AcceptedIdempotencyKey == "" || decoded.AcceptedSequence < 1 {
		return fmt.Errorf("S9 resolution receipt has incomplete acceptance coordinates")
	}
	*receipt = RepairResultResolutionReceipt(decoded)
	return nil
}

type repairResultResolutionReceiptWire RepairResultResolutionReceipt

// VerifiedRepairResultResolutionReceipt reads an accepted indexed Main decision.
// Its acceptance coordinates come from the original Agent Writer journal.
func VerifiedRepairResultResolutionReceipt(root string, state map[string]any, packetID, owner string) (RepairResultResolutionReceipt, bool, error) {
	packet, found, err := FindRepairResultPacketContext(repairPacketContext(root), state, packetID)
	if err != nil || !found {
		return RepairResultResolutionReceipt{}, false, err
	}
	if owner != "" && owner != packet.Binding.Sources.OwnerAgentID {
		return RepairResultResolutionReceipt{}, false, nil
	}
	if owner == "" {
		owner = packet.Binding.Sources.OwnerAgentID
	}
	journal, err := repairResultResumeJournal(root, state)
	if err != nil {
		return RepairResultResolutionReceipt{}, false, err
	}
	return verifiedRepairResultResolutionReceiptWithJournal(root, state, packet, owner, journal)
}

func verifiedRepairResultResolutionReceiptWithJournal(root string, state map[string]any, p workpacket.RepairResultPacket, owner string, journal controlproof.JournalInspection) (RepairResultResolutionReceipt, bool, error) {
	packets, err := repairResultPacketsForBindingContext(repairPacketContext(root), state, p.Binding)
	if err != nil {
		return RepairResultResolutionReceipt{}, false, err
	}
	verifier, err := newRepairResultResumeVerifier(root, state, journal, packets)
	if err != nil {
		return RepairResultResolutionReceipt{}, false, err
	}
	return verifier.resolution(p, owner)
}

func verifiedRepairResultResolutionReceiptWithVerifier(verifier *repairResultResumeVerifier, p workpacket.RepairResultPacket, owner string) (RepairResultResolutionReceipt, bool, error) {
	root, state, journal := verifier.root, verifier.state, verifier.journal
	b := p.Binding.Sources
	if owner == "" || owner != b.OwnerAgentID {
		return RepairResultResolutionReceipt{}, false, fmt.Errorf("S9 resolution receipt owner differs from the originally issued packet")
	}
	evidenceRows, pointerSeen, err := repairResultResolutionEvidenceRows(state, p, owner)
	if err != nil {
		return RepairResultResolutionReceipt{}, false, err
	}
	events, err := repairResultResolutionEvents(journal, owner, evidenceRows)
	if err != nil {
		return RepairResultResolutionReceipt{}, false, err
	}
	if len(evidenceRows) == 0 && len(events) == 0 && !pointerSeen {
		return RepairResultResolutionReceipt{}, false, nil
	}
	blocker, blockerFound, blockerErr := verifier.blocker(p, owner)
	if blockerErr != nil {
		return RepairResultResolutionReceipt{}, false, fmt.Errorf("verify S9 predecessor blocker receipt: %w", blockerErr)
	}
	if !blockerFound {
		return RepairResultResolutionReceipt{}, false, fmt.Errorf("S9 resolution lineage has no verified predecessor blocker receipt")
	}
	if len(evidenceRows) != 1 || len(events) != 1 {
		return RepairResultResolutionReceipt{}, false, fmt.Errorf("S9 resolution evidence and lifecycle event are missing, duplicated or orphaned")
	}

	evidence := evidenceRows[0]
	messageRef := stringValue(evidence["id"])
	messagePath := stringValue(evidence["path"])
	if messageRef == "" || messageRef != messagePath {
		return RepairResultResolutionReceipt{}, false, fmt.Errorf("S9 resolution original reference unavailable")
	}
	if err := verifyUniqueResolutionEvidencePath(state, messagePath, evidence); err != nil {
		return RepairResultResolutionReceipt{}, false, err
	}
	if err := verifyRepairResultResolutionEvidence(evidence, p, blocker); err != nil {
		return RepairResultResolutionReceipt{}, false, err
	}
	messageBytes, err := workpacket.ReadFile(root, messageRef, 1<<20)
	if err != nil {
		return RepairResultResolutionReceipt{}, false, fmt.Errorf("read immutable S9 resolution message: %w", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", messageBytes); err != nil {
		return RepairResultResolutionReceipt{}, false, fmt.Errorf("validate S9 resolution message: %w", err)
	}
	if err := verifyRepairResultResolutionMessage(messageBytes, p, blocker); err != nil {
		return RepairResultResolutionReceipt{}, false, err
	}

	event := events[0]
	eventID, key, after, sequence, err := verifyRepairResultResolutionEvent(event, p, owner, messageRef, blocker, state)
	if err != nil {
		return RepairResultResolutionReceipt{}, false, err
	}
	if err := verifyUniqueRepairResultBlockerCommittedKey(journal, key, eventID); err != nil {
		return RepairResultResolutionReceipt{}, false, fmt.Errorf("S9 resolution idempotency key: %w", err)
	}
	return RepairResultResolutionReceipt{
		Contract:                     "s9-repair-result-resolution-receipt-v1",
		PacketID:                     p.ID,
		OwnerAgentID:                 owner,
		BlockerRef:                   blocker.ArtifactRef,
		MessageRef:                   messageRef,
		AcceptedEventID:              eventID,
		AcceptedIdempotencyKey:       key,
		AcceptedAfterRuntimeRevision: after,
		AcceptedSequence:             sequence,
	}, true, nil
}

func repairResultResolutionEvidenceRows(state map[string]any, p workpacket.RepairResultPacket, owner string) ([]map[string]any, bool, error) {
	rows, ok := state["evidence"].([]any)
	if !ok {
		return nil, false, fmt.Errorf("Runtime evidence index is not an array")
	}
	entities, _ := state["entities"].(map[string]any)
	agents, ok := entities["agents"].([]any)
	if !ok {
		return nil, false, fmt.Errorf("Runtime Agent registry is not an array")
	}
	pointerSeen := false
	ownerCount := 0
	for i, raw := range agents {
		agent, ok := raw.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("Runtime agents[%d] is not an object", i)
		}
		if agent["id"] != owner {
			continue
		}
		ownerCount++
		_ = agent // the selected packet-scoped evidence below supplies the reference

	}
	if ownerCount > 1 {
		return nil, false, fmt.Errorf("S9 resolution owner Agent is duplicated")
	}

	matching := make([]map[string]any, 0, 1)
	seenIDs := map[string]bool{}
	for i, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("Runtime evidence[%d] is not an object", i)
		}
		id := stringValue(row["id"])
		if id != "" {
			if seenIDs[id] {
				return nil, false, fmt.Errorf("Runtime evidence index duplicates id %q", id)
			}
			seenIDs[id] = true
		}
		refs, refsOK := checkpointStringSlice(row["scope_refs"])
		packetScoped := row["kind"] == "repair_result_resolution" && refsOK && containsStringRuntime(refs, p.ID)

		if packetScoped {
			matching = append(matching, row)
		}
	}
	return matching, pointerSeen, nil
}

func repairResultResolutionEvents(journal controlproof.JournalInspection, owner string, rows []map[string]any) ([]map[string]any, error) {
	paths := map[string]bool{}
	for _, row := range rows {
		paths[stringValue(row["path"])] = true
	}
	matching := []map[string]any{}
	prefix := "evt-agent-" + owner + "-blocker_resolved-r"
	for _, event := range journal.Events {
		if !strings.HasPrefix(stringValue(event["event_id"]), prefix) {
			continue
		}
		refs, ok := checkpointStringSlice(event["evidence_ids"])
		if !ok {
			return nil, fmt.Errorf("S9 blocker_resolved journal evidence_ids is not a string array")
		}
		for _, ref := range refs {
			if paths[ref] && ref != "" {
				matching = append(matching, event)
				break
			}
		}
	}
	return matching, nil
}

func verifyRepairResultResolutionEvidence(row map[string]any, p workpacket.RepairResultPacket, blocker RepairResultBlockerReceipt) error {
	b := p.Binding.Sources
	for _, required := range []string{"id", "kind", "path", "status", "baseline_generation", "produced_by", "invalidated_by", "invalidation_rule", "invalidation_reason", "responsibility_id", "scope_refs"} {
		if _, exists := row[required]; !exists {
			return fmt.Errorf("S9 resolution evidence omits required field %q", required)
		}
	}
	status, statusOK := row["status"].(string)
	if row["id"] != row["path"] || row["kind"] != "repair_result_resolution" || row["responsibility_id"] != nil || !statusOK || (status != "valid" && status != "invalid" && status != "superseded") {
		return fmt.Errorf("S9 resolution evidence identity, kind or responsibility differs")
	}
	generation, genErr := integerField(row, "baseline_generation")
	if genErr != nil || generation != b.BaselineGeneration {
		return fmt.Errorf("S9 resolution evidence generation differs")
	}
	producers, ok := checkpointStringSlice(row["produced_by"])
	if !ok || len(producers) != 1 || producers[0] != "main" {
		return fmt.Errorf("S9 resolution evidence must be produced only by Main")
	}
	refs, ok := checkpointStringSlice(row["scope_refs"])
	if !ok || !sameRepairResultResolutionScopeRefs(refs, p.ID, b.RepairAssignmentID, blocker.ArtifactRef) {
		return fmt.Errorf("S9 resolution evidence scope must contain exactly packet, RepairAssignment and predecessor blocker refs")
	}
	// status and invalidation columns describe present-day evidence freshness;
	// they do not erase the accepted lifecycle CAS reconstructed below.
	return nil
}

func verifyUniqueResolutionEvidencePath(state map[string]any, messagePath string, selected map[string]any) error {
	rows, ok := state["evidence"].([]any)
	if !ok {
		return fmt.Errorf("Runtime evidence index is not an array")
	}
	count := 0
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil || row["path"] != messagePath {
			continue
		}
		count++
		if row["id"] != selected["id"] {
			return fmt.Errorf("S9 resolution message path is aliased by a different evidence ID")
		}
	}
	if count != 1 {
		return fmt.Errorf("S9 resolution evidence path is duplicated or missing")
	}
	return nil
}

func verifyRepairResultResolutionMessage(data []byte, p workpacket.RepairResultPacket, blocker RepairResultBlockerReceipt, _ ...string) error {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return err
	}
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil || message == nil {
		return fmt.Errorf("S9 resolution message is not an object")
	}
	b := p.Binding.Sources
	var manifestAssignment map[string]any
	if err := json.Unmarshal(p.Inputs.Sources.ManifestAssignment, &manifestAssignment); err != nil || manifestAssignment == nil {
		return fmt.Errorf("S9 resolution frozen manifest Assignment is invalid")
	}
	if message["message_type"] != "blocker_resolution" || message["runtime_id"] != b.RuntimeID || message["agent_id"] != b.OwnerAgentID || message["task_id"] != b.TaskID || message["team_id"] != b.TeamID || message["agent_definition_ref"] != manifestAssignment["agent_definition_ref"] {
		return fmt.Errorf("S9 resolution message lifecycle envelope differs from the issued owner/TASK/team")
	}
	resolution, ok := message["s9_resolution"].(map[string]any)
	if !ok {
		return fmt.Errorf("S9 resolution message lacks its closed S9 subtype")
	}
	if resolution["decision"] != "retry" || resolution["packet_id"] != p.ID || resolution["blocker_ref"] != blocker.ArtifactRef {
		return fmt.Errorf("S9 resolution does not match the original responsibility")
	}

	return nil
}

func verifyRepairResultResolutionEvent(event map[string]any, p workpacket.RepairResultPacket, owner, messageRef string, blocker RepairResultBlockerReceipt, state map[string]any) (eventID, key string, after, sequence int, err error) {
	before, be := integerField(event, "before_revision")
	after, ae := integerField(event, "after_revision")
	sequence, se := integerField(event, "sequence")
	generation, ge := integerField(event, "baseline_generation")
	actor, _ := event["actor"].(map[string]any)
	from, _ := event["from"].(map[string]any)
	to, _ := event["to"].(map[string]any)
	refs, refsOK := checkpointStringSlice(event["evidence_ids"])
	wantID := fmt.Sprintf("evt-agent-%s-blocker_resolved-r%d", owner, after)
	wantKey := fmt.Sprintf("runtime:agent:%s:blocker_resolved:%d", owner, before)
	// The Runtime's write counter is a recorded field, never a qualifier. It is
	// read on its original path and carried into the receipt for audit and
	// history restoration; the required-field checks below only mirror the
	// journal event schema, and no continuity, freshness or ordering is
	// derived from the counter. The committed event coordinate (sequence) is
	// what orders this event after the original blocker.
	if be != nil || ae != nil || se != nil || ge != nil || !refsOK || sequence <= blocker.AcceptedSequence || generation != p.Binding.Sources.BaselineGeneration || event["event_id"] != wantID || event["event"] != "transition_committed" || event["outcome"] != "committed" || event["transition_id"] != "AGENT-LIFECYCLE" || event["runtime_id"] != p.Binding.Sources.RuntimeID || event["idempotency_key"] != wantKey || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" || event["message"] != "Recorded a schema-valid Agent lifecycle event (blocker_resolved)" || from["state"] != "bug_resolution" || from["phase"] != "fixing" || !reflect.DeepEqual(from, to) || !sameBlockerEvidenceRefs(refs, messageRef, blocker.ArtifactRef) {
		return "", "", 0, 0, fmt.Errorf("S9 blocker_resolved lifecycle event tuple or evidence IDs differ")
	}
	if sequence <= blocker.AcceptedSequence {
		return "", "", 0, 0, fmt.Errorf("S9 blocker_resolved event does not follow its original blocker")
	}
	return wantID, wantKey, after, sequence, nil
}

func sameRepairResultResolutionScopeRefs(refs []string, packetID, repairAssignmentID, blockerRef string) bool {
	if len(refs) != 3 || packetID == repairAssignmentID || packetID == blockerRef || repairAssignmentID == blockerRef {
		return false
	}
	want := []string{packetID, repairAssignmentID, blockerRef}
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if !containsStringRuntime(want, ref) || seen[ref] {
			return false
		}
		seen[ref] = true
	}
	return true
}
