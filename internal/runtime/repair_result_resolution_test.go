package runtime

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func attachRepairResultResolutionFixture(t *testing.T, f *repairResultBlockerReaderFixture, edit func(map[string]any)) string {
	t.Helper()
	p := f.runtime.packet
	b := p.Binding.Sources
	blocker, found, err := VerifiedRepairResultBlockerReceipt(f.runtime.plan.root, f.runtime.plan.state, p.ID, b.OwnerAgentID)
	if err != nil || !found {
		t.Fatalf("fixture needs original blocker receipt: found=%t err=%v", found, err)
	}
	packetSHA, err := workpacket.RepairResultPacketDigest(p)
	if err != nil {
		t.Fatal(err)
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
			"evidence_refs": []string{"evidence://main-review"},
			"packet_id":     p.ID, "packet_sha256": packetSHA,
			"blocker_ref": blocker.ArtifactRef, "blocker_sha256": blocker.ArtifactSHA256,
			"execution_assignment_id": blocker.Artifact.ExecutionAssignmentID,
			"execution_generation":    blocker.Artifact.ExecutionGeneration,
			"execution_sha256":        p.Binding.ExecutionSHA256,
		},
	}
	if edit != nil {
		edit(message["s9_resolution"].(map[string]any))
	}
	messageBytes, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	messageSHA := workpacket.Digest(messageBytes)
	messageDir := path.Join(".claude/evidence", b.RuntimeID, fmt.Sprintf("g%d", b.BaselineGeneration), "S9", "blocker-messages", p.ID)
	messageRef := path.Join(messageDir, messageSHA+".json")
	writeBlockerReaderFile(t, f.runtime.plan.root, messageRef, messageBytes)

	row := map[string]any{
		"id": messageRef, "kind": "repair_result_resolution", "path": messageRef, "sha256": messageSHA,
		"status": "valid", "baseline_generation": b.BaselineGeneration, "review_round": b.ReviewRound,
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

func TestVerifiedRepairResultResolutionReceiptHistoricalAndRetiresOldPacket(t *testing.T) {
	f := newRepairResultBlockerReaderFixture(t)
	messageRef := attachRepairResultResolutionFixture(t, &f, nil)

	got, found, err := VerifiedRepairResultResolutionReceipt(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet.ID, f.runtime.packet.Binding.Sources.OwnerAgentID)
	if err != nil || !found {
		t.Fatalf("accepted historical resolution not found: found=%t err=%v", found, err)
	}
	if got.PacketID != f.runtime.packet.ID || got.OwnerAgentID != f.runtime.packet.Binding.Sources.OwnerAgentID || got.BlockerRef != f.artifactRef || got.MessageRef != messageRef || got.AcceptedIdempotencyKey != fmt.Sprintf("runtime:agent:%s:blocker_resolved:10", got.OwnerAgentID) || got.AcceptedSequence <= 10 {
		t.Fatalf("resolution receipt identity differs: %+v", got)
	}
	if selected, err := CurrentRepairResultPackets(f.runtime.plan.root, f.runtime.plan.state, got.OwnerAgentID); err != nil || len(selected) != 0 {
		t.Fatalf("resolved original packet remains current: selected=%+v err=%v", selected, err)
	}
	if err := VerifyRepairResultPacketCurrent(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet); err == nil {
		t.Fatal("resolved original packet passed current verification")
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

func TestVerifiedRepairResultResolutionReceiptRejectsForgedMessage(t *testing.T) {
	f := newRepairResultBlockerReaderFixture(t)
	attachRepairResultResolutionFixture(t, &f, func(resolution map[string]any) {
		resolution["packet_sha256"] = strings.Repeat("f", 64)
	})
	if _, found, err := VerifiedRepairResultResolutionReceipt(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet.ID, f.runtime.packet.Binding.Sources.OwnerAgentID); err == nil || found {
		t.Fatalf("resolution message with forged packet digest was accepted: found=%t", found)
	}
}

func TestVerifiedRepairResultResolutionReceiptRejectsWrongChronologyAndDuplicates(t *testing.T) {
	for _, variant := range []string{"before_blocker", "duplicate_event"} {
		t.Run(variant, func(t *testing.T) {
			f := newRepairResultBlockerReaderFixture(t)
			messageRef := attachRepairResultResolutionFixture(t, &f, nil)
			switch variant {
			case "before_blocker":
				last := len(f.runtime.events) - 1
				f.runtime.events[last] = buildJournalEvent(Mutation{
					EventID:      fmt.Sprintf("evt-agent-%s-blocker_resolved-r10", f.runtime.packet.Binding.Sources.OwnerAgentID),
					TransitionID: "AGENT-LIFECYCLE", Event: "blocker_resolved", Actor: "orchestrator",
					IdempotencyKey: fmt.Sprintf("runtime:agent:%s:blocker_resolved:9", f.runtime.packet.Binding.Sources.OwnerAgentID),
					RuntimeID:      f.runtime.packet.Binding.Sources.RuntimeID, BaselineGeneration: f.runtime.packet.Binding.Sources.BaselineGeneration,
					From: map[string]any{"state": "bug_resolution", "phase": "fixing"}, To: map[string]any{"state": "bug_resolution", "phase": "fixing"},
					EvidenceIDs: []string{messageRef, f.artifactRef},
					Message:     "Recorded a schema-valid Agent lifecycle event (blocker_resolved)",
				}, f.runtime.packet.Binding.Sources.RuntimeID, 9, 10, 11, time.Date(2026, 10, 14, 12, 11, 0, 0, time.UTC))
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
	if selected, err := CurrentRepairResultPackets(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet.Binding.Sources.OwnerAgentID); err != nil || len(selected) != 0 {
		t.Fatalf("invalidated resolution revived old packet scope: selected=%+v err=%v", selected, err)
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
	if _, found, err := VerifiedRepairResultResolutionReceipt(f.runtime.plan.root, f.runtime.plan.state, p.ID, owner); err == nil || found {
		t.Fatalf("Agent resolution pointer without evidence/journal proof was accepted: found=%t", found)
	}
	if _, err := CurrentRepairResultPackets(f.runtime.plan.root, f.runtime.plan.state, owner); err == nil {
		t.Fatal("current selector did not fail closed on an orphan resolution pointer")
	}
}
