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

// VerifiedRepairResultBlockerReceipt reads the blocker already registered by
// the Agent Writer, including its original journal acceptance coordinates.
func VerifiedRepairResultBlockerReceipt(root string, state map[string]any, packetID, owner string) (RepairResultBlockerReceipt, bool, error) {
	packet, found, err := FindRepairResultPacketContext(repairPacketContext(root), state, packetID)
	if err != nil || !found {
		return RepairResultBlockerReceipt{}, false, err
	}
	if owner != "" && owner != packet.Binding.Sources.OwnerAgentID {
		return RepairResultBlockerReceipt{}, false, nil
	}
	// A receipt query may select the original owner from its exact packet;
	// it does not grant that owner permission to execute or complete work.
	if owner == "" {
		owner = packet.Binding.Sources.OwnerAgentID
	}
	journal, err := repairResultResumeJournal(root, state)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	return verifiedRepairResultBlockerReceiptWithJournal(root, state, packet, owner, journal)
}

func verifiedRepairResultBlockerReceiptWithJournal(root string, state map[string]any, p workpacket.RepairResultPacket, owner string, journal controlproof.JournalInspection) (RepairResultBlockerReceipt, bool, error) {
	packets, err := repairResultPacketsForBindingContext(repairPacketContext(root), state, p.Binding)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	verifier, err := newRepairResultResumeVerifier(root, state, journal, packets)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	return verifier.blocker(p, owner)
}

func verifiedRepairResultBlockerReceiptWithVerifier(verifier *repairResultResumeVerifier, p workpacket.RepairResultPacket, owner string) (RepairResultBlockerReceipt, bool, error) {
	root, state, journal := verifier.root, verifier.state, verifier.journal
	b := p.Binding.Sources
	if owner == "" || owner != b.OwnerAgentID {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("S9 blocker receipt owner differs from the originally issued packet")
	}
	if err := controlproof.ValidateStateJournalPair(state, journal); err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	issueSequence, _, err := verifier.issue(p)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}

	evidenceRows, err := repairResultBlockerEvidenceRows(state, p)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	lifecycleEvents, err := repairResultBlockerEvents(journal, p, owner, evidenceRows)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	hasBlocker, err := repairResultBlockerPresence(len(evidenceRows), len(lifecycleEvents))
	if err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	if !hasBlocker {
		return RepairResultBlockerReceipt{}, false, nil
	}
	// Candidate and final Result events are conflicts only if this packet also
	// has a blocker fact. A packet that simply completed successfully has no
	// blocker receipt to return and must project (false, nil), not a conflict.
	if err := rejectRepairResultBlockerOutcomeConflict(
		hasBlocker,
		eventIndexHas(journal.EventIndex, "evt-s9-issued-repair-candidate-"+p.ID),
		eventIndexHas(journal.EventIndex, "evt-s9-issued-repair-result-"+p.ID),
	); err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}

	evidence := evidenceRows[0]
	artifactRef := stringValue(evidence["id"])
	artifactPath := stringValue(evidence["path"])
	if artifactRef == "" || artifactRef != artifactPath {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("S9 blocker artifact reference unavailable")
	}
	if err := verifyUniqueBlockerEvidencePath(state, artifactPath, evidence); err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	artifactBytes, err := workpacket.ReadFile(root, artifactPath, 20<<20)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("read S9 blocker artifact: %w", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-result-blocker.schema.json", artifactBytes); err != nil {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("validate S9 blocker artifact: %w", err)
	}
	var artifact RepairResultBlockerArtifact
	if err := json.Unmarshal(artifactBytes, &artifact); err != nil {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("decode S9 blocker artifact: %w", err)
	}
	if err := verifyRepairResultBlockerArtifact(p, artifact); err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}

	messageBytes, err := workpacket.ReadFile(root, artifact.MessageRef, 1<<20)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("read S9 blocker lifecycle message: %w", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", messageBytes); err != nil {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("validate S9 blocker lifecycle message: %w", err)
	}
	if err := verifyRepairResultBlockerMessage(messageBytes, p, artifact); err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	if err := verifyRepairResultBlockerEvidence(evidence, p, artifactRef); err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}

	event := lifecycleEvents[0]
	acceptedEventID, acceptedKey, acceptedAfter, acceptedSequence, err := verifyRepairResultBlockerEvent(event, p, owner, artifactRef, artifact.MessageRef, issueSequence)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	if err := verifyUniqueRepairResultBlockerCommittedKey(journal, acceptedKey, acceptedEventID); err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	return RepairResultBlockerReceipt{
		Contract:                     "s9-repair-result-blocker-receipt-v1",
		ArtifactRef:                  artifactRef,
		Artifact:                     artifact,
		AcceptedEventID:              acceptedEventID,
		AcceptedIdempotencyKey:       acceptedKey,
		AcceptedAfterRuntimeRevision: acceptedAfter,
		AcceptedSequence:             acceptedSequence,
	}, true, nil
}

// verifyHistoricalRepairResultIssueWithPackets validates the original packet
// issue event against the registry already parsed for this request.
func verifyHistoricalRepairResultIssueWithPackets(state map[string]any, p workpacket.RepairResultPacket, journal controlproof.JournalInspection, packets map[string]workpacket.RepairResultPacket, _ ...string) (sequence, after int, err error) {
	stored, found := packets[p.ID]
	if !found || !reflect.DeepEqual(stored, p) {
		return 0, 0, fmt.Errorf("S9 blocker packet is not the exact retained Runtime work_packet")
	}
	i, found := journal.EventIndex[p.IssuanceEventID]
	if !found {
		return 0, 0, fmt.Errorf("S9 blocker original packet issuance event is missing")
	}
	event := journal.Events[i]
	actor, _ := event["actor"].(map[string]any)
	from, _ := event["from"].(map[string]any)
	to, _ := event["to"].(map[string]any)
	after, ae := integerField(event, "after_revision")
	sequence, se := integerField(event, "sequence")
	generation, ge := integerField(event, "baseline_generation")
	b := p.Binding.Sources
	if event["event"] != "transition_committed" || event["outcome"] != "committed" || event["transition_id"] != "PACKET-ISSUE" || event["event_id"] != p.IssuanceEventID || event["idempotency_key"] != "packet:"+p.ID || event["runtime_id"] != b.RuntimeID || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" || ae != nil || se != nil || ge != nil || sequence < 1 || generation < 1 || !reflect.DeepEqual(from, to) {
		return 0, 0, fmt.Errorf("S9 blocker original packet issue tuple differs")
	}
	if err := verifyUniqueRepairResultBlockerCommittedKey(journal, "packet:"+p.ID, p.IssuanceEventID); err != nil {
		return 0, 0, err
	}
	return sequence, after, nil
}

// Failed audit records do not establish acceptance. Among committed events,
// the original key must identify exactly the retained acceptance event, even
// when a conflicting event points outside this packet's artifact directory.
func verifyUniqueRepairResultBlockerCommittedKey(journal controlproof.JournalInspection, key, eventID string) error {
	count := 0
	for _, event := range journal.Events {
		if event["idempotency_key"] != key || event["outcome"] != "committed" {
			continue
		}
		count++
		if event["event_id"] != eventID {
			return fmt.Errorf("S9 blocker historical key %q has a conflicting committed event", key)
		}
	}
	if count != 1 {
		return fmt.Errorf("S9 blocker historical key %q does not have exactly one committed event", key)
	}
	return nil
}

func repairResultBlockerEvidenceRows(state map[string]any, p workpacket.RepairResultPacket) ([]map[string]any, error) {
	rows, ok := state["evidence"].([]any)
	if !ok {
		return nil, fmt.Errorf("Runtime evidence index is not an array")
	}
	matching := make([]map[string]any, 0, 1)
	seenIDs := map[string]bool{}
	for i, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Runtime evidence[%d] is not an object", i)
		}
		id := stringValue(row["id"])
		if id != "" {
			if seenIDs[id] {
				return nil, fmt.Errorf("Runtime evidence index duplicates id %q", id)
			}
			seenIDs[id] = true
		}
		refs, refsOK := checkpointStringSlice(row["scope_refs"])
		isPacketScopedBlocker := row["kind"] == "repair_result_blocker" && refsOK && containsStringRuntime(refs, p.ID)
		if isPacketScopedBlocker {
			matching = append(matching, row)
		}
	}
	return matching, nil
}

func repairResultBlockerPresence(evidenceRows, lifecycleEvents int) (bool, error) {
	if evidenceRows < 0 || lifecycleEvents < 0 {
		return false, fmt.Errorf("S9 blocker evidence/event count is invalid")
	}
	if evidenceRows == 0 && lifecycleEvents == 0 {
		return false, nil
	}
	if evidenceRows != 1 || lifecycleEvents != 1 {
		return false, fmt.Errorf("S9 blocker evidence and original lifecycle event are missing, duplicated or orphaned")
	}
	return true, nil
}

func rejectRepairResultBlockerOutcomeConflict(hasBlocker, candidateAccepted, finalResultAccepted bool) error {
	if !hasBlocker {
		return nil
	}
	if candidateAccepted {
		return fmt.Errorf("S9 packet already has an original candidate acceptance")
	}
	if finalResultAccepted {
		return fmt.Errorf("S9 packet already has an original final Result acceptance")
	}
	return nil
}

func eventIndexHas(index map[string]int, eventID string) bool {
	_, found := index[eventID]
	return found
}

// agentLifecycleIdentity reports whether a saved Agent lifecycle event id and
// idempotency key belong to one owner and one event kind. The historical shape
// ended in "-r<revision>" and the semantic shape ends in "-<intent key>"; both
// start with the same prefix, so an already saved identity is accepted in
// either form and never recomputed from the Runtime write counter (G18 §1.4).
func agentLifecycleIdentity(eventID, key, owner, kind string) bool {
	eventPrefix := "evt-agent-" + owner + "-" + kind + "-"
	keyPrefix := "runtime:agent:" + owner + ":" + kind + ":"
	return len(eventID) > len(eventPrefix) && strings.HasPrefix(eventID, eventPrefix) &&
		len(key) > len(keyPrefix) && strings.HasPrefix(key, keyPrefix)
}

func repairResultBlockerEvents(journal controlproof.JournalInspection, p workpacket.RepairResultPacket, owner string, rows []map[string]any) ([]map[string]any, error) {
	paths := map[string]bool{}
	for _, row := range rows {
		paths[stringValue(row["path"])] = true
	}
	matching := []map[string]any{}
	for _, event := range journal.Events {
		if event["transition_id"] != "AGENT-LIFECYCLE" || event["runtime_id"] != p.Binding.Sources.RuntimeID || !strings.HasPrefix(stringValue(event["event_id"]), "evt-agent-"+owner+"-work_blocked-") {
			continue
		}
		refs, ok := checkpointStringSlice(event["evidence_ids"])
		if !ok {
			return nil, fmt.Errorf("S9 work_blocked journal evidence_ids is not a string array")
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

func verifyRepairResultBlockerEvidence(row map[string]any, p workpacket.RepairResultPacket, artifactRef string) error {
	if row["kind"] != "repair_result_blocker" || row["path"] != artifactRef {
		return fmt.Errorf("S9 blocker original reference differs")
	}
	return nil
}

func verifyUniqueBlockerEvidencePath(state map[string]any, artifactPath string, selected map[string]any) error {
	rows, ok := state["evidence"].([]any)
	if !ok {
		return fmt.Errorf("Runtime evidence index is not an array")
	}
	count := 0
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["path"] == artifactPath {
			count++
			if row["id"] != selected["id"] {
				return fmt.Errorf("S9 blocker artifact path is aliased by a different evidence ID")
			}
		}
	}
	if count != 1 {
		return fmt.Errorf("S9 blocker artifact evidence path is duplicated or missing")
	}
	return nil
}

func verifyRepairResultBlockerArtifact(p workpacket.RepairResultPacket, artifact RepairResultBlockerArtifact) error {
	if artifact.PacketID != p.ID || artifact.Binding.Sources.OwnerAgentID != p.Binding.Sources.OwnerAgentID || artifact.Binding.Sources.RepairAssignmentID != p.Binding.Sources.RepairAssignmentID {
		return fmt.Errorf("S9 blocker responsibility differs")
	}
	answer, problems := workpacket.ValidateRepairResultAnswer(p, artifact.SubmittedAnswer)
	if len(problems) != 0 {
		return fmt.Errorf("S9 blocker answer invalid: %v", problems)
	}
	var professional workpacket.RepairResultProfessionalAnswer
	if err := json.Unmarshal(answer, &professional); err != nil {
		return err
	}
	if professional.Verdict != "blocked" && professional.Verdict != "contract_conflict" && professional.Verdict != "stale" {
		return fmt.Errorf("S9 blocker verdict differs")
	}
	return nil
}

func repairResultBlockerDraft(p workpacket.RepairResultPacket, answer json.RawMessage) ([]byte, error) {
	draft := p.Draft
	draft.Answer = append(json.RawMessage(nil), answer...)
	return json.Marshal(draft)
}

func verifyRepairResultBlockerMessage(data []byte, p workpacket.RepairResultPacket, artifact RepairResultBlockerArtifact) error {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return err
	}
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil || message == nil {
		return fmt.Errorf("S9 blocker message is not an object")
	}
	b := p.Binding.Sources
	var manifestAssignment map[string]any
	if err := json.Unmarshal(p.Inputs.Sources.ManifestAssignment, &manifestAssignment); err != nil || manifestAssignment == nil {
		return fmt.Errorf("S9 blocker frozen manifest Assignment is invalid")
	}
	if message["message_type"] != "blocker_report" || message["runtime_id"] != b.RuntimeID || message["agent_id"] != b.OwnerAgentID || message["task_id"] != b.TaskID || message["team_id"] != b.TeamID || message["agent_definition_ref"] != manifestAssignment["agent_definition_ref"] {
		return fmt.Errorf("S9 blocker message lifecycle envelope differs from the issued owner/TASK/team")
	}
	// The frozen L4 message extension names the exact issued packet and answer,
	// never the not-yet-written blocker artifact. Keep this check separate from
	// schema validation so the indexed message cannot be rebound to another
	// packet after it has been copied to its immutable content-addressed path.
	extension, ok := message["s9_blocker"].(map[string]any)
	if !ok || extension["packet_id"] != p.ID || extension["answer_path"] != artifact.AnswerPath {
		return fmt.Errorf("S9 blocker message does not bind the exact packet and answer ref/SHA")
	}
	if _, has := extension["artifact_ref"]; has {
		return fmt.Errorf("S9 blocker lifecycle message must not contain a forward artifact ref")
	}
	return nil
}

func verifyRepairResultBlockerEvent(event map[string]any, p workpacket.RepairResultPacket, owner, artifactRef, messageRef string, issueSequence int) (eventID, key string, after, sequence int, err error) {
	_, be := integerField(event, "before_revision")
	after, ae := integerField(event, "after_revision")
	sequence, se := integerField(event, "sequence")
	generation, ge := integerField(event, "baseline_generation")
	actor, _ := event["actor"].(map[string]any)
	from, _ := event["from"].(map[string]any)
	to, _ := event["to"].(map[string]any)
	refs, refsOK := checkpointStringSlice(event["evidence_ids"])
	// The saved identity is compared, never recomputed from the Runtime write
	// counter (G18 §1.4). Both the historical "-r<revision>" shape and the
	// semantic "-<intent key>" shape are accepted.
	eventID = stringValue(event["event_id"])
	key = stringValue(event["idempotency_key"])
	// The Runtime's write counter is a recorded field, never a qualifier. It is
	// read on its original path and carried into the receipt for audit and
	// history restoration; the required-field checks below only mirror the
	// journal event schema, and no continuity, freshness or ordering is
	// derived from the counter. The committed event coordinate (sequence) and
	// the evidence references are what bind this event to the original blocker.
	if be != nil || ae != nil || se != nil || ge != nil || !refsOK || sequence < 1 || generation != p.Binding.Sources.BaselineGeneration || !agentLifecycleIdentity(eventID, key, owner, "work_blocked") || event["event"] != "transition_committed" || event["outcome"] != "committed" || event["transition_id"] != "AGENT-LIFECYCLE" || event["runtime_id"] != p.Binding.Sources.RuntimeID || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" || event["message"] != "Recorded a schema-valid Agent lifecycle event (work_blocked)" || !reflect.DeepEqual(from, to) || !sameBlockerEvidenceRefs(refs, messageRef, artifactRef) {
		return "", "", 0, 0, fmt.Errorf("S9 original work_blocked lifecycle event tuple or evidence IDs differ")
	}
	if sequence <= issueSequence {
		return "", "", 0, 0, fmt.Errorf("S9 original blocker event does not follow packet issuance")
	}
	return eventID, key, after, sequence, nil
}

func containsStringRuntime(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sameBlockerEvidenceRefs(refs []string, messageRef, artifactRef string) bool {
	if len(refs) != 2 || refs[0] == refs[1] {
		return false
	}
	return (refs[0] == messageRef && refs[1] == artifactRef) || (refs[0] == artifactRef && refs[1] == messageRef)
}

func sameBlockerScopeRefs(refs []string, packetID, repairAssignmentID string) bool {
	if len(refs) != 2 || packetID == repairAssignmentID || refs[0] == refs[1] {
		return false
	}
	return (refs[0] == packetID && refs[1] == repairAssignmentID) || (refs[0] == repairAssignmentID && refs[1] == packetID)
}

func validBlockerExecutionAssignmentID(frozenID, repairAssignmentID, manifestAssignmentID, artifactID string) bool {
	return (frozenID == repairAssignmentID || frozenID == manifestAssignmentID) && artifactID == frozenID
}

func validBlockerCommitID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !('0' <= c && c <= '9') && !('a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}

// Project acceptance metadata from the original Writer journal. This does not
// reauthorize the Worker or reconstruct historical execution permissions.
func recordedRepairResultLifecycleEvent(journal controlproof.JournalInspection, packet workpacket.RepairResultPacket, messageRef, action string) (map[string]any, error) {
	var selected map[string]any
	prefix := "evt-agent-" + packet.Binding.Sources.OwnerAgentID + "-" + action + "-"
	for _, event := range journal.Events {
		refs, _ := checkpointStringSlice(event["evidence_ids"])
		if !containsStringRuntime(refs, messageRef) || !strings.HasPrefix(stringValue(event["event_id"]), prefix) || event["outcome"] != "committed" {
			continue
		}
		// The Runtime's write counter is a recorded field, never a qualifier.
		// The required-field checks below only mirror the journal event schema;
		// no continuity or freshness is derived from the counter. The committed
		// event coordinate (sequence) and the evidence references are what
		// identify the original acceptance event.
		_, be := integerField(event, "before_revision")
		_, ae := integerField(event, "after_revision")
		sequence, se := integerField(event, "sequence")
		generation, ge := integerField(event, "baseline_generation")
		if selected != nil || event["transition_id"] != "AGENT-LIFECYCLE" || event["runtime_id"] != packet.Binding.Sources.RuntimeID || ge != nil || generation < 1 || (action != "work_blocked" && generation != packet.Binding.Sources.BaselineGeneration) || be != nil || ae != nil || se != nil || sequence < 1 || stringValue(event["idempotency_key"]) == "" {
			return nil, fmt.Errorf("S9 recorded %s has ambiguous or invalid original acceptance coordinates", action)
		}
		selected = event
	}
	if selected == nil {
		return nil, fmt.Errorf("S9 recorded %s has no original acceptance event", action)
	}
	if err := verifyUniqueRepairResultBlockerCommittedKey(journal, stringValue(selected["idempotency_key"]), stringValue(selected["event_id"])); err != nil {
		return nil, err
	}
	return selected, nil
}
