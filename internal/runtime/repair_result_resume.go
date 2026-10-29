package runtime

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// RepairResultResumeBinding reads the current owner's recorded resolution.
// The successor retains the original resolution acceptance coordinates.
func RepairResultResumeBinding(root string, state map[string]any, base workpacket.RepairResultBinding) (*workpacket.RepairResultResume, error) {
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	if state["runtime_id"] != base.Sources.RuntimeID || !hypothesisJSONEqual(baseline["generation"], base.Sources.BaselineGeneration) ||
		pointer["session_id"] != base.Sources.SessionID || pointer["plan_ref"] != base.Sources.PlanRef {
		return nil, nil
	}
	entities, _ := state["entities"].(map[string]any)
	messageRef := ""
	for _, agent := range objectRows(entities["agents"]) {
		if agent["id"] == base.Sources.OwnerAgentID {
			messageRef = stringValue(agent["blocker_resolved_ref"])
		}
	}
	if messageRef == "" {
		return nil, nil
	}
	var index map[string]any
	for _, row := range objectRows(state["evidence"]) {
		if row["kind"] == "repair_result_resolution" && row["path"] == messageRef {
			if index != nil {
				return nil, fmt.Errorf("S9 current resolution is indexed more than once")
			}
			index = row
		}
	}
	if index == nil {
		return nil, fmt.Errorf("S9 current resolution message is not recorded")
	}
	data, err := workpacket.ReadFile(root, messageRef, 1<<20)
	if err != nil {
		return nil, err
	}
	var message struct {
		Resolution struct {
			PacketID   string `json:"packet_id"`
			PacketSHA  string `json:"packet_sha256"`
			BlockerRef string `json:"blocker_ref"`
			BlockerSHA string `json:"blocker_sha256"`
		} `json:"s9_resolution"`
	}
	if err := json.Unmarshal(data, &message); err != nil {
		return nil, err
	}
	refs, _ := checkpointStringSlice(index["scope_refs"])
	if !containsStringRuntime(refs, message.Resolution.PacketID) || !containsStringRuntime(refs, message.Resolution.BlockerRef) {
		return nil, fmt.Errorf("S9 recorded resolution refers to another predecessor or blocker")
	}
	prior, found, err := FindRepairResultPacketContext(repairPacketContext(root), state, message.Resolution.PacketID)
	if err != nil || !found {
		return nil, fmt.Errorf("S9 resolution predecessor questionnaire is unavailable: %v", err)
	}
	b := prior.Binding
	if b.Sources.RuntimeID != base.Sources.RuntimeID || b.Sources.BaselineGeneration != base.Sources.BaselineGeneration ||
		b.Sources.SessionID != base.Sources.SessionID || b.Sources.RepairAssignmentID != base.Sources.RepairAssignmentID ||
		b.Sources.OwnerAgentID != base.Sources.OwnerAgentID || b.ReworkRef != base.ReworkRef {
		return nil, nil
	}
	var issued map[string]any
	if err := json.Unmarshal(prior.Inputs.Execution, &issued); err != nil {
		return nil, err
	}
	workspace, _ := state["workspace"].(map[string]any)
	executions, _ := workspace["executions"].(map[string]any)
	execution, _ := executions[stringValue(issued["assignment_id"])].(map[string]any)
	if execution == nil || !hypothesisJSONEqual(execution["execution_generation"], issued["execution_generation"]) || execution["worktree_path"] != issued["worktree_path"] {
		return nil, nil
	}
	receipt, found, err := VerifiedRepairResultResolutionReceipt(root, state, prior.ID, base.Sources.OwnerAgentID)
	if err != nil || !found {
		return nil, fmt.Errorf("S9 current resolution receipt unavailable: %v", err)
	}
	journal, err := repairResultResumeJournal(root, state)
	if err != nil {
		return nil, err
	}
	return repairResultResumeFromReceipt(prior, receipt, journal, message.Resolution.PacketSHA)
}

// VerifyRepairResultResume verifies a packet's full predecessor chain against
// the retained Runtime packet registry, immutable evidence and one parsed
// journal. A new unissued packet is accepted only when its tuple exactly equals
// the tuple derivable from that same authority.
func VerifyRepairResultResume(root string, state map[string]any, packet workpacket.RepairResultPacket) error {
	journal, err := repairResultResumeJournal(root, state)
	if err != nil {
		return err
	}
	packets, err := repairResultPacketsForBindingContext(repairPacketContext(root), state, packet.Binding)
	if err != nil {
		return err
	}
	verifier, err := newRepairResultResumeVerifier(root, state, journal, packets)
	if err != nil {
		return err
	}
	return verifier.verifyResume(packet)
}

// repairResultResumeVerifier is scoped to one immutable state snapshot and one
// parsed journal. It reuses only verified structures within that call; there is
// no cross-request cache or authority outside the original readers.
type repairResultResumeVerifier struct {
	root    string
	state   map[string]any
	journal controlproof.JournalInspection
	packets []workpacket.RepairResultPacket
	byID    map[string]workpacket.RepairResultPacket

	issues        map[string]repairResultResumeIssueResult
	blockers      map[string]repairResultResumeBlockerResult
	resolutions   map[string]repairResultResumeResolutionResult
	packetDigests map[string]string
	lineages      map[string]error
}

type repairResultResumeIssueResult struct {
	sequence int
	after    int
	err      error
}
type repairResultResumeBlockerResult struct {
	receipt RepairResultBlockerReceipt
	found   bool
	err     error
}
type repairResultResumeResolutionResult struct {
	receipt RepairResultResolutionReceipt
	found   bool
	err     error
}

// The caller must supply packets returned by RepairResultPackets, which has
// already applied the closed schema and canonical rebuild checks. Keeping that
// parsed registry here prevents repeat rebuilds while preserving those checks.
func newRepairResultResumeVerifier(root string, state map[string]any, journal controlproof.JournalInspection, packets []workpacket.RepairResultPacket) (*repairResultResumeVerifier, error) {
	if err := controlproof.ValidateStateJournalPair(state, journal); err != nil {
		return nil, err
	}
	verifier := &repairResultResumeVerifier{
		root: root, state: state, journal: journal, packets: packets,
		byID:          make(map[string]workpacket.RepairResultPacket, len(packets)),
		issues:        make(map[string]repairResultResumeIssueResult),
		blockers:      make(map[string]repairResultResumeBlockerResult),
		resolutions:   make(map[string]repairResultResumeResolutionResult),
		packetDigests: make(map[string]string),
		lineages:      make(map[string]error),
	}
	for _, packet := range packets {
		if prior, exists := verifier.byID[packet.ID]; exists {
			if !reflect.DeepEqual(prior, packet) {
				return nil, fmt.Errorf("S9 Runtime packet registry duplicates identity %s", packet.ID)
			}
			return nil, fmt.Errorf("S9 Runtime packet registry duplicates identity %s", packet.ID)
		}
		verifier.byID[packet.ID] = packet
	}
	return verifier, nil
}

func (v *repairResultResumeVerifier) registered(packet workpacket.RepairResultPacket) (bool, error) {
	stored, found := v.byID[packet.ID]
	if found && !reflect.DeepEqual(stored, packet) {
		return false, fmt.Errorf("S9 Resume packet differs from its exact Runtime registry row")
	}
	return found, nil
}

func (v *repairResultResumeVerifier) packetDigest(packet workpacket.RepairResultPacket) (string, error) {
	if found, err := v.registered(packet); err != nil {
		return "", err
	} else if !found {
		return workpacket.RepairResultPacketContentDigest(packet)
	}
	if digest, exists := v.packetDigests[packet.ID]; exists {
		return digest, nil
	}
	digest, err := workpacket.RepairResultPacketContentDigest(packet)
	if err != nil {
		return "", err
	}
	v.packetDigests[packet.ID] = digest
	return digest, nil
}

func (v *repairResultResumeVerifier) issue(packet workpacket.RepairResultPacket) (int, int, error) {
	if found, err := v.registered(packet); err != nil || !found {
		if err == nil {
			err = fmt.Errorf("S9 issue packet is not an exact registered row")
		}
		return 0, 0, err
	}
	if result, exists := v.issues[packet.ID]; exists {
		return result.sequence, result.after, result.err
	}
	packetSHA, err := v.packetDigest(packet)
	if err != nil {
		return 0, 0, err
	}
	sequence, after, err := verifyHistoricalRepairResultIssueWithPackets(v.state, packet, v.journal, v.byID, packetSHA)
	v.issues[packet.ID] = repairResultResumeIssueResult{sequence: sequence, after: after, err: err}
	return sequence, after, err
}

func (v *repairResultResumeVerifier) blocker(packet workpacket.RepairResultPacket, owner string) (RepairResultBlockerReceipt, bool, error) {
	if owner == "" || owner != packet.Binding.Sources.OwnerAgentID {
		return RepairResultBlockerReceipt{}, false, fmt.Errorf("S9 blocker receipt owner differs from the originally issued packet")
	}
	if found, err := v.registered(packet); err != nil || !found {
		// Before PACKET-ISSUE there can be no accepted receipt. Match the
		// original public reader's absent-packet result; this grants no issue,
		// source authority or permission to the unregistered questionnaire.
		return RepairResultBlockerReceipt{}, false, err
	}
	if result, exists := v.blockers[packet.ID]; exists {
		return result.receipt, result.found, result.err
	}
	receipt, found, err := verifiedRepairResultBlockerReceiptWithVerifier(v, packet, owner)
	v.blockers[packet.ID] = repairResultResumeBlockerResult{receipt: receipt, found: found, err: err}
	return receipt, found, err
}

func (v *repairResultResumeVerifier) resolution(packet workpacket.RepairResultPacket, owner string) (RepairResultResolutionReceipt, bool, error) {
	if owner == "" || owner != packet.Binding.Sources.OwnerAgentID {
		return RepairResultResolutionReceipt{}, false, fmt.Errorf("S9 resolution receipt owner differs from the originally issued packet")
	}
	if found, err := v.registered(packet); err != nil || !found {
		// Before PACKET-ISSUE there can be no accepted receipt. Match the
		// original public reader's absent-packet result; this grants no issue,
		// source authority or permission to the unregistered questionnaire.
		return RepairResultResolutionReceipt{}, false, err
	}
	if result, exists := v.resolutions[packet.ID]; exists {
		return result.receipt, result.found, result.err
	}
	receipt, found, err := verifiedRepairResultResolutionReceiptWithVerifier(v, packet, owner)
	v.resolutions[packet.ID] = repairResultResumeResolutionResult{receipt: receipt, found: found, err: err}
	return receipt, found, err
}

func (v *repairResultResumeVerifier) resumeBinding(base workpacket.RepairResultBinding) (*workpacket.RepairResultResume, error) {
	if base.Resume != nil {
		return nil, fmt.Errorf("S9 Resume derivation requires a base binding without Resume")
	}
	var selected *struct {
		packet  workpacket.RepairResultPacket
		receipt RepairResultResolutionReceipt
	}
	for _, packet := range v.packets {
		if !sameRepairResultResumeSlot(packet.Binding, base) {
			continue
		}
		if err := v.verifyResume(packet); err != nil {
			return nil, fmt.Errorf("verify existing S9 packet %s Resume lineage: %w", packet.ID, err)
		}
	}
	for _, packet := range v.packets {
		if !sameRepairResultResumeSlot(packet.Binding, base) {
			continue
		}
		receipt, found, err := v.resolution(packet, base.Sources.OwnerAgentID)
		if err != nil {
			return nil, fmt.Errorf("read S9 predecessor resolution for %s: %w", packet.ID, err)
		}
		if !found || !sameRepairResultResumeBase(packet.Binding, base) {
			// A changed frozen source/rework follows its existing independent
			// dispatch path and cannot inherit this predecessor's resolution.
			continue
		}
		if selected == nil || receipt.AcceptedSequence > selected.receipt.AcceptedSequence {
			selected = &struct {
				packet  workpacket.RepairResultPacket
				receipt RepairResultResolutionReceipt
			}{packet: packet, receipt: receipt}
		} else if receipt.AcceptedSequence == selected.receipt.AcceptedSequence {
			return nil, fmt.Errorf("S9 slot has multiple resolutions at one journal sequence")
		}
	}
	if selected == nil {
		return nil, nil
	}
	packetSHA, err := v.packetDigest(selected.packet)
	if err != nil {
		return nil, err
	}
	resume, err := repairResultResumeFromReceipt(selected.packet, selected.receipt, v.journal, packetSHA)
	if err != nil {
		return nil, err
	}
	if err := workpacket.ValidateRepairResultResume(base.Sources.OwnerAgentID, resume); err != nil {
		return nil, err
	}
	children := 0
	for _, packet := range v.packets {
		if packet.Binding.Resume != nil && packet.Binding.Resume.PredecessorPacketID == selected.packet.ID {
			children++
			if !sameRepairResultResumeBase(packet.Binding, base) || !reflect.DeepEqual(packet.Binding.Resume, resume) {
				return nil, fmt.Errorf("S9 Resume predecessor already has a changed or forked successor")
			}
		}
	}
	if children > 1 {
		return nil, fmt.Errorf("S9 Resume predecessor has multiple successor packets")
	}
	return resume, nil
}

func (v *repairResultResumeVerifier) verifyResume(packet workpacket.RepairResultPacket) error {
	digest, err := v.packetDigest(packet)
	if err != nil {
		return err
	}
	key := packet.ID + ":" + digest
	if result, exists := v.lineages[key]; exists {
		return result
	}
	err = v.verifyResumeUncached(packet)
	v.lineages[key] = err
	return err
}

func (v *repairResultResumeVerifier) verifyResumeUncached(packet workpacket.RepairResultPacket) error {
	registered, err := v.registered(packet)
	if err != nil {
		return err
	}
	if !registered {
		rebuilt, rebuildErr := workpacket.RebuildRepairResult(packet)
		if rebuildErr != nil || !reflect.DeepEqual(packet, rebuilt) {
			return fmt.Errorf("S9 Resume packet differs from its canonical rebuild")
		}
		base := packet.Binding
		base.Resume = nil
		expected, err := v.resumeBinding(base)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(expected, packet.Binding.Resume) {
			return fmt.Errorf("unissued S9 packet Resume differs from the unique accepted predecessor")
		}
		return nil
	}
	if err := workpacket.ValidateRepairResultResume(packet.Binding.Sources.OwnerAgentID, packet.Binding.Resume); err != nil {
		return err
	}
	visited := make(map[string]bool, len(v.packets))
	current := packet
	for steps := 0; steps <= len(v.packets); steps++ {
		if visited[current.ID] {
			return fmt.Errorf("S9 Resume lineage contains a packet cycle")
		}
		visited[current.ID] = true
		issueSequence, issueAfter, err := v.issue(current)
		if err != nil {
			return fmt.Errorf("verify S9 Resume packet issuance %s: %w", current.ID, err)
		}
		resume := current.Binding.Resume
		if resume == nil {
			for _, other := range v.packets {
				if other.ID == current.ID || !sameRepairResultResumeBase(other.Binding, current.Binding) {
					continue
				}
				receipt, resolved, err := v.resolution(other, current.Binding.Sources.OwnerAgentID)
				if err != nil {
					return fmt.Errorf("verify prior S9 resolution before packet %s: %w", current.ID, err)
				}
				if !resolved || receipt.AcceptedSequence >= issueSequence {
					continue
				}
				return fmt.Errorf("S9 packet %s omits required Resume after predecessor resolution %s", current.ID, other.ID)
			}
			return nil
		}
		parent, exists := v.byID[resume.PredecessorPacketID]
		if !exists || visited[parent.ID] || !sameRepairResultResumeBase(current.Binding, parent.Binding) {
			return fmt.Errorf("S9 Resume predecessor is missing, cyclic or has changed frozen sources")
		}
		receipt, resolved, err := v.resolution(parent, current.Binding.Sources.OwnerAgentID)
		if err != nil {
			return fmt.Errorf("verify S9 Resume predecessor resolution %s: %w", parent.ID, err)
		}
		if !resolved {
			return fmt.Errorf("S9 Resume predecessor lacks an accepted blocker resolution")
		}
		parentSHA, err := v.packetDigest(parent)
		if err != nil {
			return err
		}
		expected, err := repairResultResumeFromReceipt(parent, receipt, v.journal, parentSHA)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(resume, expected) {
			return fmt.Errorf("S9 Resume tuple differs from the exact predecessor resolution")
		}
		if receipt.AcceptedSequence >= issueSequence || receipt.AcceptedAfterRuntimeRevision >= issueAfter {
			return fmt.Errorf("S9 successor packet issuance does not follow its Main resolution")
		}
		children := 0
		for _, child := range v.packets {
			if child.Binding.Resume != nil && child.Binding.Resume.PredecessorPacketID == parent.ID {
				children++
				if child.ID != current.ID {
					return fmt.Errorf("S9 Resume predecessor has a forked successor packet")
				}
			}
		}
		if children != 1 {
			return fmt.Errorf("S9 Resume predecessor does not have exactly one registered successor")
		}
		current = parent
	}
	return fmt.Errorf("S9 Resume lineage exceeds its packet registry bound")
}

func repairResultResumeFromReceipt(packet workpacket.RepairResultPacket, receipt RepairResultResolutionReceipt, journal controlproof.JournalInspection, packetSHA string) (*workpacket.RepairResultResume, error) {
	if receipt.PacketID != packet.ID || receipt.OwnerAgentID != packet.Binding.Sources.OwnerAgentID {
		return nil, fmt.Errorf("S9 resolution receipt owner/packet differs from Resume predecessor")
	}
	if !isSHA256Hex(packetSHA) {
		return nil, fmt.Errorf("S9 Resume predecessor packet digest is invalid")
	}
	index, exists := journal.EventIndex[receipt.AcceptedEventID]
	if !exists || index < 0 || index >= len(journal.Events) {
		return nil, fmt.Errorf("S9 Resume resolution event is missing from the original journal")
	}
	event := journal.Events[index]
	before, be := integerField(event, "before_revision")
	after, ae := integerField(event, "after_revision")
	sequence, se := integerField(event, "sequence")
	if be != nil || ae != nil || se != nil || before < 0 || after != before+1 || after != receipt.AcceptedAfterRuntimeRevision || sequence != receipt.AcceptedSequence || event["event_id"] != receipt.AcceptedEventID || event["idempotency_key"] != receipt.AcceptedIdempotencyKey {
		return nil, fmt.Errorf("S9 Resume resolution acceptance coordinates differ from the verified event")
	}
	return &workpacket.RepairResultResume{
		PredecessorPacketID: packet.ID, PredecessorPacketSHA256: packetSHA,
		BlockerRef: receipt.BlockerRef, BlockerSHA256: receipt.BlockerSHA256,
		ResolutionMessageRef: receipt.MessageRef, ResolutionMessageSHA256: receipt.MessageSHA256,
		ResolutionEventID: receipt.AcceptedEventID, ResolutionIdempotencyKey: receipt.AcceptedIdempotencyKey,
		ResolutionBeforeRevision: before, ResolutionAfterRevision: after, ResolutionSequence: sequence,
	}, nil
}

func sameRepairResultResumeSlot(left, right workpacket.RepairResultBinding) bool {
	a, b := left.Sources, right.Sources
	return a.RuntimeID == b.RuntimeID && a.BaselineGeneration == b.BaselineGeneration && a.ReviewRound == b.ReviewRound &&
		a.RepairAssignmentID == b.RepairAssignmentID && a.OwnerAgentID == b.OwnerAgentID
}

func sameRepairResultResumeBase(left, right workpacket.RepairResultBinding) bool {
	return reflect.DeepEqual(left.Sources, right.Sources) && left.PlanReportRef == right.PlanReportRef &&
		left.PlanReportSHA256 == right.PlanReportSHA256 && left.ExecutionSHA256 == right.ExecutionSHA256 &&
		left.ReworkRef == right.ReworkRef && left.ReworkSHA256 == right.ReworkSHA256
}
