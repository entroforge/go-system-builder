package runtime

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"testing"
	"time"
)

func attachRepairResultResolutionFixture(t *testing.T, f *repairResultBlockerReaderFixture, edit func(map[string]any)) string {
	t.Helper()
	p := f.runtime.packet
	b := p.Binding.Sources
	blocker, found, err := VerifiedRepairResultBlockerReceipt(f.runtime.plan.root, f.runtime.plan.state, p.ID, b.OwnerAgentID)
	if err != nil || !found {
		t.Fatalf("fixture needs original blocker receipt: found=%t err=%v", found, err)
	}
	var manifestAssignment map[string]any
	if err := json.Unmarshal(p.Inputs.Sources.ManifestAssignment, &manifestAssignment); err != nil {
		t.Fatal(err)
	}
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "blocker_resolution",
		"message_id": "msg-s9-resolution-reader", "correlation_id": "corr-s9-resolution-reader",
		"runtime_id": b.RuntimeID, "agent_id": b.OwnerAgentID,
		"agent_definition_ref": manifestAssignment["agent_definition_ref"],
		"task_id":              b.TaskID, "bug_id": nil, "team_id": b.TeamID,
		"occurred_at": "2026-10-14T12:11:00Z", "body": "Main reviewed the blocker and authorized retry.",
		"s9_resolution": map[string]any{
			"decision": "retry", "rationale": "The existing contract remains valid; retry the blocked scope.",
			"evidence_refs":           []string{"evidence://main-review"},
			"packet_id":               p.ID,
			"blocker_ref":             blocker.ArtifactRef,
			"execution_assignment_id": blocker.Artifact.ExecutionAssignmentID,
			"execution_generation":    blocker.Artifact.ExecutionGeneration,
		},
	}
	if edit != nil {
		edit(message["s9_resolution"].(map[string]any))
	}
	messageBytes, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	messageDir := path.Join(".claude/evidence", b.RuntimeID, fmt.Sprintf("g%d", b.BaselineGeneration), "S9", "blocker-messages", p.ID)
	messageRef := path.Join(messageDir, "resolution.json")
	writeBlockerReaderFile(t, f.runtime.plan.root, messageRef, messageBytes)

	row := map[string]any{
		"id": messageRef, "kind": "repair_result_resolution", "path": messageRef,
		"status": "valid", "baseline_generation": b.BaselineGeneration, "review_round": "",
		"produced_by": []string{"main"}, "invalidated_by": nil, "invalidation_rule": nil,
		"invalidation_reason": nil, "responsibility_id": nil,
		"scope_refs": []string{p.ID, b.RepairAssignmentID, blocker.ArtifactRef},
	}
	f.runtime.plan.state["evidence"] = append(f.runtime.plan.state["evidence"].([]any), row)
	for _, agent := range objectRows(f.runtime.plan.state["entities"].(map[string]any)["agents"]) {
		if agent["id"] == b.OwnerAgentID {
			agent["blocker_resolved_ref"] = messageRef
			agent["state"] = "done" // Historical resolution reader must not require blocked state.
		}
	}
	event := buildJournalEvent(Mutation{
		EventID:      fmt.Sprintf("evt-agent-%s-blocker_resolved-r11", b.OwnerAgentID),
		TransitionID: "AGENT-LIFECYCLE", Event: "blocker_resolved", Actor: "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:agent:%s:blocker_resolved:10", b.OwnerAgentID),
		RuntimeID:      b.RuntimeID, BaselineGeneration: b.BaselineGeneration,
		From:        map[string]any{"state": "bug_resolution", "phase": "fixing"},
		To:          map[string]any{"state": "bug_resolution", "phase": "fixing"},
		EvidenceIDs: []string{messageRef, blocker.ArtifactRef},
		Message:     "Recorded a schema-valid Agent lifecycle event (blocker_resolved)",
	}, b.RuntimeID, 10, 11, 11, time.Date(2026, 10, 14, 12, 11, 0, 0, time.UTC))
	f.runtime.events = append(f.runtime.events, event)
	writeBlockerReaderJournal(t, f)
	return messageRef
}

func TestVerifiedRepairResultResolutionReceiptKeepsOriginalUntilSuccessor(t *testing.T) {
	f := newRepairResultBlockerReaderFixture(t)
	messageRef := attachRepairResultResolutionFixture(t, &f, nil)

	got, found, err := VerifiedRepairResultResolutionReceipt(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet.ID, f.runtime.packet.Binding.Sources.OwnerAgentID)
	if err != nil || !found {
		t.Fatalf("accepted historical resolution not found: found=%t err=%v", found, err)
	}
	if got.PacketID != f.runtime.packet.ID || got.OwnerAgentID != f.runtime.packet.Binding.Sources.OwnerAgentID || got.BlockerRef != f.artifactRef || got.MessageRef != messageRef || got.AcceptedIdempotencyKey != fmt.Sprintf("runtime:agent:%s:blocker_resolved:10", got.OwnerAgentID) || got.AcceptedSequence <= 10 {
		t.Fatalf("resolution receipt identity differs: %+v", got)
	}
	if selected, err := CurrentRepairResultPackets(f.runtime.plan.root, f.runtime.plan.state, got.OwnerAgentID); err != nil || len(selected) != 1 || selected[0].ID != f.runtime.packet.ID {
		t.Fatalf("original responsibility disappeared before successor issue: selected=%+v err=%v", selected, err)
	}
	if pending, err := RepairResultResumePending(f.runtime.plan.root, f.runtime.plan.state, got.OwnerAgentID, f.runtime.packet.Binding.Sources.RepairAssignmentID); err != nil || !pending {
		t.Fatalf("actual accepted resolution lost its pending continuation: pending=%v err=%v", pending, err)
	}
}

// G18 §1.4: a journal written with the semantic Agent lifecycle identity is
// read exactly like the historical "-r<revision>" identity, and the receipt
// carries the saved identity instead of a recomputed one.
func TestRepairResultResolutionReaderAcceptsSemanticLifecycleIdentity(t *testing.T) {
	f := newRepairResultBlockerReaderFixture(t)
	attachRepairResultResolutionFixture(t, &f, nil)
	owner := f.runtime.packet.Binding.Sources.OwnerAgentID
	semanticEventID := "evt-agent-" + owner + "-blocker_resolved-msg-s9-resolution-reader"
	semanticKey := "runtime:agent:" + owner + ":blocker_resolved:msg-s9-resolution-reader"
	for _, event := range f.runtime.events {
		if event["transition_id"] != "AGENT-LIFECYCLE" {
			continue
		}
		if !strings.Contains(fmt.Sprint(event["event_id"]), "-blocker_resolved-") {
			continue
		}
		event["event_id"] = semanticEventID
		event["idempotency_key"] = semanticKey
	}
	writeBlockerReaderJournal(t, &f)

	got, found, err := VerifiedRepairResultResolutionReceipt(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet.ID, owner)
	if err != nil || !found {
		t.Fatalf("semantic resolution identity not found: found=%t err=%v", found, err)
	}
	if got.AcceptedEventID != semanticEventID || got.AcceptedIdempotencyKey != semanticKey {
		t.Fatalf("resolution receipt did not carry the saved semantic identity: %+v", got)
	}
}

func TestRepairResultResolutionReaderLeavesBlockerOnlyPacketCurrent(t *testing.T) {
	f := newRepairResultBlockerReaderFixture(t)
	owner := f.runtime.packet.Binding.Sources.OwnerAgentID

	if _, found, err := VerifiedRepairResultResolutionReceipt(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet.ID, owner); err != nil || found {
		t.Fatalf("original work_blocked event was mistaken for a resolution: found=%t err=%v", found, err)
	}
	selected, err := CurrentRepairResultPackets(f.runtime.plan.root, f.runtime.plan.state, owner)
	if err != nil || len(selected) != 1 || selected[0].ID != f.runtime.packet.ID {
		t.Fatalf("blocker-only current packet was retired: selected=%+v err=%v", selected, err)
	}
}

func TestVerifiedRepairResultResolutionReceiptRejectsWrongChronologyAndDuplicates(t *testing.T) {
	for _, variant := range []string{"before_blocker", "duplicate_event"} {
		t.Run(variant, func(t *testing.T) {
			f := newRepairResultBlockerReaderFixture(t)
			messageRef := attachRepairResultResolutionFixture(t, &f, nil)
			switch variant {
			case "before_blocker":
				// The resolution must be a committed event that follows its own
				// blocker. The Runtime write counter is a recorded field, not an
				// ordering authority, so the wrong chronology is expressed on
				// the committed event coordinate: the resolution is committed at
				// the blocker's own sequence and the blocker after it.
				owner := f.runtime.packet.Binding.Sources.OwnerAgentID
				binding := f.runtime.packet.Binding.Sources
				last := len(f.runtime.events) - 1
				f.runtime.events[last-1] = buildJournalEvent(Mutation{
					EventID:      fmt.Sprintf("evt-agent-%s-work_blocked-r11", owner),
					TransitionID: "AGENT-LIFECYCLE", Event: "work_blocked", Actor: "orchestrator",
					IdempotencyKey: fmt.Sprintf("runtime:agent:%s:work_blocked:10", owner),
					RuntimeID:      binding.RuntimeID, BaselineGeneration: binding.BaselineGeneration,
					From: map[string]any{"state": "bug_resolution", "phase": "fixing"}, To: map[string]any{"state": "bug_resolution", "phase": "fixing"},
					EvidenceIDs: []string{messageRef, f.artifactRef},
					Message:     "Recorded a schema-valid Agent lifecycle event (work_blocked)",
				}, binding.RuntimeID, 10, 11, 11, time.Date(2026, 10, 14, 12, 11, 0, 0, time.UTC))
				f.runtime.events[last] = buildJournalEvent(Mutation{
					EventID:      fmt.Sprintf("evt-agent-%s-blocker_resolved-r10", owner),
					TransitionID: "AGENT-LIFECYCLE", Event: "blocker_resolved", Actor: "orchestrator",
					IdempotencyKey: fmt.Sprintf("runtime:agent:%s:blocker_resolved:9", owner),
					RuntimeID:      binding.RuntimeID, BaselineGeneration: binding.BaselineGeneration,
					From: map[string]any{"state": "bug_resolution", "phase": "fixing"}, To: map[string]any{"state": "bug_resolution", "phase": "fixing"},
					EvidenceIDs: []string{messageRef, f.artifactRef},
					Message:     "Recorded a schema-valid Agent lifecycle event (blocker_resolved)",
				}, binding.RuntimeID, 9, 10, 10, time.Date(2026, 10, 14, 12, 10, 0, 0, time.UTC))
			case "duplicate_event":
				original := f.runtime.events[len(f.runtime.events)-1]
				duplicate := map[string]any{}
				for key, value := range original {
					duplicate[key] = value
				}
				duplicate["event_id"] = fmt.Sprintf("evt-agent-%s-blocker_resolved-r12", f.runtime.packet.Binding.Sources.OwnerAgentID)
				duplicate["idempotency_key"] = fmt.Sprintf("runtime:agent:%s:blocker_resolved:11", f.runtime.packet.Binding.Sources.OwnerAgentID)
				duplicate["before_revision"] = 11
				duplicate["after_revision"] = 12
				duplicate["sequence"] = 12
				f.runtime.events = append(f.runtime.events, duplicate)
			}
			writeBlockerReaderJournal(t, &f)
			if _, found, err := VerifiedRepairResultResolutionReceipt(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet.ID, f.runtime.packet.Binding.Sources.OwnerAgentID); err == nil || found {
				t.Fatalf("invalid resolution chronology/duplicate was accepted: found=%t", found)
			}
		})
	}
}

func TestVerifiedRepairResultResolutionReceiptSurvivesEvidenceInvalidation(t *testing.T) {
	f := newRepairResultBlockerReaderFixture(t)
	attachRepairResultResolutionFixture(t, &f, nil)
	for _, row := range objectRows(f.runtime.plan.state["evidence"]) {
		if row["kind"] == "repair_result_resolution" {
			row["status"] = "invalid"
			row["invalidated_by"] = "evt-later-evidence-invalidation"
			row["invalidation_rule"] = "later-current-evidence-change"
			row["invalidation_reason"] = "Historical lifecycle acceptance remains true."
		}
	}
	if _, found, err := VerifiedRepairResultResolutionReceipt(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet.ID, f.runtime.packet.Binding.Sources.OwnerAgentID); err != nil || !found {
		t.Fatalf("evidence invalidation erased historical resolution: found=%t err=%v", found, err)
	}
	if selected, err := CurrentRepairResultPackets(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet.Binding.Sources.OwnerAgentID); err != nil || len(selected) != 1 || selected[0].ID != f.runtime.packet.ID {
		t.Fatalf("original responsibility was not retained while continuation is pending: selected=%+v err=%v", selected, err)
	}
}

func TestVerifiedRepairResultResolutionReceiptRejectsPointerWithoutProof(t *testing.T) {
	f := newRepairResultBlockerReaderFixture(t)
	owner := f.runtime.packet.Binding.Sources.OwnerAgentID
	p := f.runtime.packet
	fakeRef := path.Join(".claude/evidence", p.Binding.Sources.RuntimeID, fmt.Sprintf("g%d", p.Binding.Sources.BaselineGeneration), "S9", "blocker-messages", p.ID, strings.Repeat("d", 64)+".json")
	for _, agent := range objectRows(f.runtime.plan.state["entities"].(map[string]any)["agents"]) {
		if agent["id"] == owner {
			agent["blocker_resolved_ref"] = fakeRef
		}
	}
	if _, found, err := VerifiedRepairResultResolutionReceipt(f.runtime.plan.root, f.runtime.plan.state, p.ID, owner); err != nil || found {
		t.Fatalf("an unindexed pointer was mistaken for accepted resolution: found=%t err=%v", found, err)
	}
	if selected, err := CurrentRepairResultPackets(f.runtime.plan.root, f.runtime.plan.state, owner); err != nil || len(selected) != 1 || selected[0].ID != p.ID {
		t.Fatalf("unindexed pointer blocked the original responsibility: %v %v", selected, err)
	}
}
