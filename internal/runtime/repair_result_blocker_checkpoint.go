package runtime

import (
	"encoding/json"
	"fmt"
	"path"
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
	issueSequence, issueAfter, err := verifier.issue(p)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}

	evidenceRows, err := repairResultBlockerEvidenceRows(state, p)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	lifecycleEvents, err := repairResultBlockerEvents(journal, p, owner)
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
	artifactSHA := stringValue(evidence["sha256"])
	if artifactRef == "" || artifactRef != artifactPath || !isSHA256Hex(artifactSHA) {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("S9 blocker evidence row lacks its content-addressed artifact ref/SHA")
	}
	expectedArtifactDir := path.Join(".claude/evidence", b.RuntimeID, fmt.Sprintf("g%d", b.BaselineGeneration), "S9", "blockers", p.ID)
	if artifactPath != path.Join(expectedArtifactDir, artifactSHA+".json") {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("S9 blocker artifact path is not content addressed under its issued packet")
	}
	if err := verifyUniqueBlockerEvidencePath(state, artifactPath, evidence); err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	artifactBytes, err := workpacket.ReadFile(root, artifactPath, 20<<20)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("read S9 blocker artifact: %w", err)
	}
	if workpacket.Digest(artifactBytes) != artifactSHA {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("S9 blocker artifact bytes differ from original evidence SHA")
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-result-blocker.schema.json", artifactBytes); err != nil {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("validate S9 blocker artifact: %w", err)
	}
	var artifact RepairResultBlockerArtifact
	if err := json.Unmarshal(artifactBytes, &artifact); err != nil {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("decode S9 blocker artifact: %w", err)
	}
	packetSHA, err := verifier.packetDigest(p)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	if err := verifyRepairResultBlockerArtifact(p, artifact, packetSHA); err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}

	messageBytes, err := workpacket.ReadFile(root, artifact.MessageRef, 1<<20)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("read S9 blocker lifecycle message: %w", err)
	}
	if workpacket.Digest(messageBytes) != artifact.MessageSHA256 {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("S9 blocker message bytes differ from its immutable SHA")
	}
	expectedMessageDir := path.Join(".claude/evidence", b.RuntimeID, fmt.Sprintf("g%d", b.BaselineGeneration), "S9", "blocker-messages", p.ID)
	if artifact.MessageRef != path.Join(expectedMessageDir, artifact.MessageSHA256+".json") {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("S9 blocker message ref is not content addressed under its issued packet")
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", messageBytes); err != nil {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("validate S9 blocker lifecycle message: %w", err)
	}
	if err := verifyRepairResultBlockerMessage(messageBytes, p, artifact); err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	if err := verifyRepairResultBlockerEvidence(evidence, p, artifactRef, artifactSHA); err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}

	event := lifecycleEvents[0]
	acceptedEventID, acceptedKey, acceptedAfter, acceptedSequence, err := verifyRepairResultBlockerEvent(event, p, owner, artifactRef, artifact.MessageRef, issueSequence, issueAfter, state)
	if err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	if err := verifyUniqueRepairResultBlockerCommittedKey(journal, acceptedKey, acceptedEventID); err != nil {
		return RepairResultBlockerReceipt{}, false, err
	}
	return RepairResultBlockerReceipt{
		Contract:                     "s9-repair-result-blocker-receipt-v1",
		ArtifactRef:                  artifactRef,
		ArtifactSHA256:               artifactSHA,
		Artifact:                     artifact,
		AcceptedEventID:              acceptedEventID,
		AcceptedIdempotencyKey:       acceptedKey,
		AcceptedAfterRuntimeRevision: acceptedAfter,
		AcceptedSequence:             acceptedSequence,
	}, true, nil
}

// verifyHistoricalRepairResultIssueWithPackets validates the original packet
// issue event against the registry already parsed for this request.
func verifyHistoricalRepairResultIssueWithPackets(state map[string]any, p workpacket.RepairResultPacket, journal controlproof.JournalInspection, packets map[string]workpacket.RepairResultPacket, digest string) (sequence, after int, err error) {
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
	before, be := integerField(event, "before_revision")
	after, ae := integerField(event, "after_revision")
	sequence, se := integerField(event, "sequence")
	generation, ge := integerField(event, "baseline_generation")
	stateRevision, re := integerField(state, "revision")
	b := p.Binding.Sources
	if !isSHA256Hex(digest) || event["event"] != "transition_committed" || event["outcome"] != "committed" || event["transition_id"] != "PACKET-ISSUE" || event["event_id"] != p.IssuanceEventID || event["idempotency_key"] != "packet:"+p.ID || event["runtime_id"] != b.RuntimeID || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" || be != nil || ae != nil || se != nil || ge != nil || re != nil || before < 0 || after != before+1 || after > stateRevision || sequence < 1 || generation != b.BaselineGeneration || from["state"] != "bug_resolution" || from["phase"] != "fixing" || !reflect.DeepEqual(from, to) {
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
	artifactDirPrefix := path.Join(".claude/evidence", p.Binding.Sources.RuntimeID, fmt.Sprintf("g%d", p.Binding.Sources.BaselineGeneration), "S9", "blockers", p.ID) + "/"
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
		isInExactBlockerDir := strings.HasPrefix(stringValue(row["path"]), artifactDirPrefix)
		if isPacketScopedBlocker || isInExactBlockerDir {
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

func repairResultBlockerEvents(journal controlproof.JournalInspection, p workpacket.RepairResultPacket, owner string) ([]map[string]any, error) {
	base := path.Join(".claude/evidence", p.Binding.Sources.RuntimeID, fmt.Sprintf("g%d", p.Binding.Sources.BaselineGeneration), "S9")
	artifactPrefix := path.Join(base, "blockers", p.ID) + "/"
	messagePrefix := path.Join(base, "blocker-messages", p.ID) + "/"
	matching := make([]map[string]any, 0, 1)
	for _, event := range journal.Events {
		if event["transition_id"] != "AGENT-LIFECYCLE" || event["runtime_id"] != p.Binding.Sources.RuntimeID {
			continue
		}
		generation, err := integerField(event, "baseline_generation")
		if err != nil || generation != p.Binding.Sources.BaselineGeneration {
			continue
		}
		eventID := stringValue(event["event_id"])
		if !strings.HasPrefix(eventID, "evt-agent-"+owner+"-work_blocked-r") {
			continue
		}
		refs, ok := checkpointStringSlice(event["evidence_ids"])
		if !ok {
			return nil, fmt.Errorf("S9 work_blocked journal evidence_ids is not a string array")
		}
		for _, ref := range refs {
			if strings.HasPrefix(ref, artifactPrefix) || strings.HasPrefix(ref, messagePrefix) {
				matching = append(matching, event)
				break
			}
		}
	}
	return matching, nil
}

func verifyRepairResultBlockerEvidence(row map[string]any, p workpacket.RepairResultPacket, artifactRef, artifactSHA string) error {
	b := p.Binding.Sources
	if row["id"] != artifactRef || row["kind"] != "repair_result_blocker" || row["path"] != artifactRef || row["sha256"] != artifactSHA || row["responsibility_id"] != "BUILD-WORK-PACKAGE" {
		return fmt.Errorf("S9 blocker evidence identity, kind, SHA or responsibility differs")
	}
	generation, genErr := integerField(row, "baseline_generation")
	round, roundErr := integerField(row, "review_round")
	if genErr != nil || roundErr != nil || generation != b.BaselineGeneration || round != b.ReviewRound {
		return fmt.Errorf("S9 blocker evidence generation/review round differs")
	}
	producers, ok := checkpointStringSlice(row["produced_by"])
	if !ok || len(producers) != 1 || producers[0] != b.OwnerAgentID {
		return fmt.Errorf("S9 blocker evidence must be produced only by the original owner")
	}
	refs, ok := checkpointStringSlice(row["scope_refs"])
	if !ok || !sameBlockerScopeRefs(refs, p.ID, b.RepairAssignmentID) {
		return fmt.Errorf("S9 blocker evidence scope must contain exactly the packet and RepairAssignment, without duplicates")
	}
	// status and invalidation columns are mutable current-evidence metadata.
	// This historical receipt proves the original CAS and never grants current
	// responsibility; a separate current selector must evaluate freshness.
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

func verifyRepairResultBlockerArtifact(p workpacket.RepairResultPacket, artifact RepairResultBlockerArtifact, packetSHA string) error {
	b := p.Binding.Sources
	if !isSHA256Hex(packetSHA) {
		return fmt.Errorf("S9 blocker packet digest is invalid")
	}
	if artifact.Contract != "s9-repair-result-blocker-v1" || artifact.SchemaVersion != "1.0.0" || artifact.PacketID != p.ID || artifact.PacketSHA256 != packetSHA || artifact.IssuanceEventID != p.IssuanceEventID || !reflect.DeepEqual(artifact.Binding, p.Binding) || artifact.AnswerPath != p.AnswerPath {
		return fmt.Errorf("S9 blocker artifact packet/binding/answer-path identity differs")
	}
	var execution map[string]any
	if err := json.Unmarshal(p.Inputs.Execution, &execution); err != nil || execution == nil {
		return fmt.Errorf("S9 blocker frozen Execution is invalid")
	}
	projection, err := ProjectRepairResultExecution(execution)
	if err != nil || workpacket.Digest(projection) != p.Binding.ExecutionSHA256 {
		return fmt.Errorf("S9 blocker frozen execution digest differs: %v", err)
	}
	frozenAssignmentID := stringValue(execution["assignment_id"])
	if execution["runtime_id"] != b.RuntimeID || !hypothesisJSONEqual(execution["baseline_generation"], b.BaselineGeneration) || execution["agent_id"] != b.OwnerAgentID || !validBlockerExecutionAssignmentID(frozenAssignmentID, b.RepairAssignmentID, b.ManifestAssignmentID, artifact.ExecutionAssignmentID) || artifact.ExecutionGeneration < 1 || !hypothesisJSONEqual(execution["execution_generation"], artifact.ExecutionGeneration) || artifact.ExecutionRoot != stringValue(execution["worktree_path"]) || artifact.BaseCommit != stringValue(execution["base_commit"]) || !validBlockerCommitID(artifact.SourceHead) {
		return fmt.Errorf("S9 blocker artifact execution/owner/base tuple differs from frozen packet")
	}
	draft, err := repairResultBlockerDraft(p, artifact.SubmittedAnswer)
	if err != nil {
		return fmt.Errorf("encode S9 issued questionnaire for answer validation: %w", err)
	}
	answer, problems := workpacket.ValidateRepairResultAnswer(p, draft)
	if len(problems) != 0 || string(answer) != string(artifact.SubmittedAnswer) || workpacket.Digest(answer) != artifact.AnswerSHA256 {
		return fmt.Errorf("S9 blocker professional answer is not the exact full answer for this packet: %v", problems)
	}
	var professional workpacket.RepairResultProfessionalAnswer
	if err := json.Unmarshal(answer, &professional); err != nil {
		return fmt.Errorf("decode S9 blocker verdict: %w", err)
	}
	if professional.Verdict != "blocked" && professional.Verdict != "contract_conflict" && professional.Verdict != "stale" {
		return fmt.Errorf("S9 blocker receipt cannot accept a completed verdict")
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
	if !ok || extension["packet_id"] != p.ID || extension["packet_sha256"] != artifact.PacketSHA256 || extension["answer_path"] != artifact.AnswerPath || extension["answer_sha256"] != artifact.AnswerSHA256 {
		return fmt.Errorf("S9 blocker message does not bind the exact packet and answer ref/SHA")
	}
	if _, has := extension["artifact_ref"]; has {
		return fmt.Errorf("S9 blocker lifecycle message must not contain a forward artifact ref")
	}
	return nil
}

func verifyRepairResultBlockerEvent(event map[string]any, p workpacket.RepairResultPacket, owner, artifactRef, messageRef string, issueSequence, issueAfter int, state map[string]any) (eventID, key string, after, sequence int, err error) {
	before, be := integerField(event, "before_revision")
	after, ae := integerField(event, "after_revision")
	sequence, se := integerField(event, "sequence")
	generation, ge := integerField(event, "baseline_generation")
	stateRevision, re := integerField(state, "revision")
	actor, _ := event["actor"].(map[string]any)
	from, _ := event["from"].(map[string]any)
	to, _ := event["to"].(map[string]any)
	refs, refsOK := checkpointStringSlice(event["evidence_ids"])
	wantID := fmt.Sprintf("evt-agent-%s-work_blocked-r%d", owner, after)
	wantKey := fmt.Sprintf("runtime:agent:%s:work_blocked:%d", owner, before)
	if be != nil || ae != nil || se != nil || ge != nil || re != nil || !refsOK || before < 0 || after != before+1 || after > stateRevision || sequence < 1 || generation != p.Binding.Sources.BaselineGeneration || event["event_id"] != wantID || event["event"] != "transition_committed" || event["outcome"] != "committed" || event["transition_id"] != "AGENT-LIFECYCLE" || event["runtime_id"] != p.Binding.Sources.RuntimeID || event["idempotency_key"] != wantKey || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" || event["message"] != "Recorded a schema-valid Agent lifecycle event (work_blocked)" || from["state"] != "bug_resolution" || from["phase"] != "fixing" || !reflect.DeepEqual(from, to) || !sameBlockerEvidenceRefs(refs, messageRef, artifactRef) {
		return "", "", 0, 0, fmt.Errorf("S9 original work_blocked lifecycle event tuple or evidence IDs differ")
	}
	if sequence <= issueSequence || after <= issueAfter {
		return "", "", 0, 0, fmt.Errorf("S9 original blocker event does not follow packet issuance")
	}
	return wantID, wantKey, after, sequence, nil
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
	prefix := "evt-agent-" + packet.Binding.Sources.OwnerAgentID + "-" + action + "-r"
	for _, event := range journal.Events {
		refs, _ := checkpointStringSlice(event["evidence_ids"])
		if !containsStringRuntime(refs, messageRef) || !strings.HasPrefix(stringValue(event["event_id"]), prefix) || event["outcome"] != "committed" {
			continue
		}
		before, be := integerField(event, "before_revision")
		after, ae := integerField(event, "after_revision")
		sequence, se := integerField(event, "sequence")
		generation, ge := integerField(event, "baseline_generation")
		if selected != nil || event["transition_id"] != "AGENT-LIFECYCLE" || event["runtime_id"] != packet.Binding.Sources.RuntimeID || ge != nil || generation != packet.Binding.Sources.BaselineGeneration || be != nil || ae != nil || se != nil || before < 0 || after != before+1 || sequence < 1 || stringValue(event["idempotency_key"]) == "" {
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
