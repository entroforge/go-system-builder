package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// These tests exercise the historical receipt reader with a mechanical
// Runtime fixture. They do not claim that an original private-capability
// lifecycle Writer accepted the blocker.
type repairResultBlockerReaderFixture struct {
	runtime     repairResultRuntimeFixture
	artifactRef string
	messageRef  string
}

func newRepairResultBlockerReaderFixture(t *testing.T) repairResultBlockerReaderFixture {
	t.Helper()
	f := newRepairResultRuntimeFixture(t)
	bindBlockerReaderPlatformSession(t, &f)
	p := f.packet
	b := p.Binding.Sources
	packetSHA, err := workpacket.RepairResultPacketDigest(p)
	if err != nil {
		t.Fatal(err)
	}

	answer := json.RawMessage("{\"verdict\":\"blocked\",\"before_fix_results\":[{\"assertion_id\":\"root-1\",\"observed_red\":\"the required check could not be completed\",\"evidence_refs\":[\"evidence://before-fix\"]}],\"unit_results\":[{\"unit_id\":\"unit-api\",\"status\":\"blocked\",\"evidence_refs\":[\"evidence://unit-api\"],\"implementation_summary\":\"the scoped unit remains blocked\",\"restored_invariant_refs\":[]}],\"detection_gap_artifacts\":[],\"evidence_refs\":[\"evidence://before-fix\"],\"scope_deviations\":[],\"residual_risks\":[],\"migration_ref\":\"\",\"rollback_ref\":\"\"}")
	draft := p.Draft
	draft.Answer = answer
	draftBytes, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	canonicalAnswer, problems := workpacket.ValidateRepairResultAnswer(p, draftBytes)
	if len(problems) != 0 {
		t.Fatalf("mechanical blocker answer is invalid: %+v", problems)
	}
	answerSHA := workpacket.Digest(canonicalAnswer)

	var manifestAssignment map[string]any
	if err := json.Unmarshal(p.Inputs.Sources.ManifestAssignment, &manifestAssignment); err != nil {
		t.Fatal(err)
	}
	message := map[string]any{
		"schema_version":       "1.0.0",
		"message_type":         "blocker_report",
		"message_id":           "msg-s9-blocker-reader",
		"correlation_id":       "corr-s9-blocker-reader",
		"runtime_id":           b.RuntimeID,
		"agent_id":             b.OwnerAgentID,
		"agent_definition_ref": manifestAssignment["agent_definition_ref"],
		"task_id":              b.TaskID,
		"bug_id":               nil,
		"team_id":              b.TeamID,
		"occurred_at":          "2026-10-14T12:10:00Z",
		"body":                 "Mechanical historical blocker fixture.",
		"s9_blocker": map[string]any{
			"packet_id": p.ID, "packet_sha256": packetSHA,
			"answer_path": p.AnswerPath, "answer_sha256": answerSHA,
		},
	}
	messageBytes, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	messageSHA := workpacket.Digest(messageBytes)
	messageRef := path.Join(".claude/evidence", b.RuntimeID, fmt.Sprintf("g%d", b.BaselineGeneration), "S9", "blocker-messages", p.ID, messageSHA+".json")
	writeBlockerReaderFile(t, f.plan.root, messageRef, messageBytes)

	var execution map[string]any
	if err := json.Unmarshal(p.Inputs.Execution, &execution); err != nil {
		t.Fatal(err)
	}
	artifact := RepairResultBlockerArtifact{
		Contract:              "s9-repair-result-blocker-v1",
		SchemaVersion:         "1.0.0",
		PacketID:              p.ID,
		PacketSHA256:          packetSHA,
		IssuanceEventID:       p.IssuanceEventID,
		Binding:               p.Binding,
		ExecutionAssignmentID: stringValue(execution["assignment_id"]),
		ExecutionGeneration:   intValueForBlockerFixture(execution["execution_generation"]),
		ExecutionRoot:         stringValue(execution["worktree_path"]),
		PlatformSessionID:     stringValue(execution["platform_session_id"]),
		BaseCommit:            stringValue(execution["base_commit"]),
		SourceHead:            strings.Repeat("c", 40),
		AnswerPath:            p.AnswerPath,
		SubmittedAnswer:       canonicalAnswer,
		AnswerSHA256:          answerSHA,
		MessageRef:            messageRef,
		MessageSHA256:         messageSHA,
	}
	artifactBytes, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	artifactSHA := workpacket.Digest(artifactBytes)
	artifactRef := path.Join(".claude/evidence", b.RuntimeID, fmt.Sprintf("g%d", b.BaselineGeneration), "S9", "blockers", p.ID, artifactSHA+".json")
	writeBlockerReaderFile(t, f.plan.root, artifactRef, artifactBytes)

	row := map[string]any{
		"id": artifactRef, "kind": "repair_result_blocker", "path": artifactRef, "sha256": artifactSHA,
		"status": "valid", "baseline_generation": b.BaselineGeneration, "review_round": b.ReviewRound,
		"produced_by": []string{b.OwnerAgentID}, "invalidated_by": nil, "invalidation_rule": nil,
		"invalidation_reason": nil, "responsibility_id": "BUILD-WORK-PACKAGE",
		"scope_refs": []string{p.ID, b.RepairAssignmentID},
	}
	f.plan.state["evidence"] = append(f.plan.state["evidence"].([]any), row)
	beforeRevision, afterRevision := 9, 10
	acceptedEvent := buildJournalEvent(Mutation{
		EventID:      fmt.Sprintf("evt-agent-%s-work_blocked-r%d", b.OwnerAgentID, afterRevision),
		TransitionID: "AGENT-LIFECYCLE", Event: "work_blocked", Actor: "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:agent:%s:work_blocked:%d", b.OwnerAgentID, beforeRevision),
		RuntimeID:      b.RuntimeID, BaselineGeneration: b.BaselineGeneration,
		From:        map[string]any{"state": "bug_resolution", "phase": "fixing"},
		To:          map[string]any{"state": "bug_resolution", "phase": "fixing"},
		EvidenceIDs: []string{messageRef, artifactRef},
		Message:     "Recorded a schema-valid Agent lifecycle event (work_blocked)",
	}, b.RuntimeID, beforeRevision, afterRevision, 10, time.Date(2026, 10, 14, 12, 10, 0, 0, time.UTC))
	f.events = append(f.events, acceptedEvent)
	f.plan.journal = s8CheckpointJournalBytes(t, f.events...)
	f.plan.state["revision"] = afterRevision
	f.plan.state["journal"].(map[string]any)["last_sequence"] = 10
	f.plan.state["journal"].(map[string]any)["last_event_id"] = acceptedEvent["event_id"]
	writeRepairPlanReportJournal(t, &f.plan)
	return repairResultBlockerReaderFixture{runtime: f, artifactRef: artifactRef, messageRef: messageRef}
}

func bindBlockerReaderPlatformSession(t *testing.T, f *repairResultRuntimeFixture) {
	t.Helper()
	old := f.packet
	f.execution["platform_session_id"] = "platform-s9-blocker-reader"
	definition, err := ProjectRepairResultExecution(f.execution)
	if err != nil {
		t.Fatal(err)
	}
	inputs := old.Inputs
	inputs.Execution = definition
	binding := old.Binding
	binding.ExecutionSHA256 = workpacket.Digest(definition)
	packet, err := workpacket.BuildRepairResult(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	rowBytes, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var replacement map[string]any
	if err := json.Unmarshal(rowBytes, &replacement); err != nil {
		t.Fatal(err)
	}
	entities := f.plan.state["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	replaced := false
	for i, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["packet_id"] == old.ID {
			rows[i] = replacement
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("mechanical fixture has no original S9 packet row")
	}
	issueReplaced := false
	for i, event := range f.events {
		if event["event_id"] != old.IssuanceEventID {
			continue
		}
		f.events[i] = buildJournalEvent(Mutation{
			EventID: packet.IssuanceEventID, TransitionID: "PACKET-ISSUE", Event: "transition_committed",
			Actor: "orchestrator", IdempotencyKey: "packet:" + packet.ID, RuntimeID: packet.Binding.Sources.RuntimeID,
			BaselineGeneration: packet.Binding.Sources.BaselineGeneration,
			From:               map[string]any{"state": "bug_resolution", "phase": "fixing"},
			To:                 map[string]any{"state": "bug_resolution", "phase": "fixing"},
		}, packet.Binding.Sources.RuntimeID, 8, 9, 9, time.Date(2026, 10, 14, 12, 9, 0, 0, time.UTC))
		issueReplaced = true
	}
	if !issueReplaced {
		t.Fatal("mechanical fixture has no original S9 issue event")
	}
	f.packet = packet
	f.plan.state["journal"].(map[string]any)["last_event_id"] = packet.IssuanceEventID
	f.plan.journal = s8CheckpointJournalBytes(t, f.events...)
	writeRepairPlanReportJournal(t, &f.plan)
}

func writeBlockerReaderFile(t *testing.T, root, ref string, data []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func intValueForBlockerFixture(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func writeBlockerReaderJournal(t *testing.T, f *repairResultBlockerReaderFixture) {
	t.Helper()
	f.runtime.plan.journal = s8CheckpointJournalBytes(t, f.runtime.events...)
	last := f.runtime.events[len(f.runtime.events)-1]
	f.runtime.plan.state["journal"].(map[string]any)["last_sequence"] = len(f.runtime.events)
	f.runtime.plan.state["journal"].(map[string]any)["last_event_id"] = last["event_id"]
	if after, err := integerField(last, "after_revision"); err == nil {
		f.runtime.plan.state["revision"] = after
	}
	writeRepairPlanReportJournal(t, &f.runtime.plan)
}

func TestVerifiedRepairResultBlockerReceiptHistoricalAndReadOnly(t *testing.T) {
	f := newRepairResultBlockerReaderFixture(t)
	// Historical acceptance remains readable after current Agent labels, live
	// source files and the present execution registry have changed or vanished.
	for _, agent := range objectRows(f.runtime.plan.state["entities"].(map[string]any)["agents"]) {
		if agent["id"] == f.runtime.packet.Binding.Sources.OwnerAgentID {
			agent["state"] = "done"
			agent["role"] = "retired-role-label"
		}
	}
	f.runtime.plan.state["workspace"].(map[string]any)["executions"] = map[string]any{}
	for _, row := range objectRows(f.runtime.plan.state["evidence"]) {
		if row["id"] == f.artifactRef {
			row["status"] = "invalid"
			row["invalidated_by"] = "evt-later-invalidation"
			row["invalidation_rule"] = "later-baseline-change"
			row["invalidation_reason"] = "Historical acceptance does not grant current gate eligibility."
		}
	}
	if err := os.WriteFile(filepath.Join(f.runtime.plan.root, filepath.FromSlash(f.runtime.packet.Binding.Sources.TaskRef)), []byte("{\"changed_after_acceptance\":true}"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateBefore, _ := json.Marshal(f.runtime.plan.state)
	journalBefore, err := os.ReadFile(filepath.Join(f.runtime.plan.root, ".claude", "loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	artifactBefore, err := os.ReadFile(filepath.Join(f.runtime.plan.root, filepath.FromSlash(f.artifactRef)))
	if err != nil {
		t.Fatal(err)
	}
	messageBefore, err := os.ReadFile(filepath.Join(f.runtime.plan.root, filepath.FromSlash(f.messageRef)))
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := VerifiedRepairResultBlockerReceipt(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet.ID, f.runtime.packet.Binding.Sources.OwnerAgentID)
	if err != nil || !found {
		t.Fatalf("historical receipt was lost after current-source/label drift: found=%t err=%v", found, err)
	}
	if got.ArtifactRef != f.artifactRef || got.ArtifactSHA256 != workpacket.Digest(artifactBefore) || got.AcceptedIdempotencyKey != fmt.Sprintf("runtime:agent:%s:work_blocked:9", f.runtime.packet.Binding.Sources.OwnerAgentID) {
		t.Fatalf("historical receipt identity differs: %+v", got)
	}
	stateAfter, _ := json.Marshal(f.runtime.plan.state)
	journalAfter, _ := os.ReadFile(filepath.Join(f.runtime.plan.root, ".claude", "loop-events.jsonl"))
	artifactAfter, _ := os.ReadFile(filepath.Join(f.runtime.plan.root, filepath.FromSlash(f.artifactRef)))
	messageAfter, _ := os.ReadFile(filepath.Join(f.runtime.plan.root, filepath.FromSlash(f.messageRef)))
	if !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(journalBefore, journalAfter) || !bytes.Equal(artifactBefore, artifactAfter) || !bytes.Equal(messageBefore, messageAfter) {
		t.Fatal("historical read changed Runtime or immutable evidence bytes")
	}
}

func TestVerifiedRepairResultBlockerReceiptReadsOriginalJournalRotation(t *testing.T) {
	f := newRepairResultBlockerReaderFixture(t)
	claude := filepath.Join(f.runtime.plan.root, ".claude")
	archive := filepath.Join(claude, "loop-events.jsonl.archive.9.jsonl")
	if err := os.WriteFile(archive, s8CheckpointJournalBytes(t, f.runtime.events[:9]...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claude, "loop-events.jsonl"), s8CheckpointJournalBytes(t, f.runtime.events[9:]...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := VerifiedRepairResultBlockerReceipt(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet.ID, f.runtime.packet.Binding.Sources.OwnerAgentID); err != nil || !found {
		t.Fatalf("full rotated journal history was not read: found=%t err=%v", found, err)
	}
}

func TestVerifiedRepairResultBlockerReceiptRejectsTamperingAndOrphans(t *testing.T) {
	for _, variant := range []string{"artifact_bytes", "message_bytes", "issue_key", "blocker_event_refs", "evidence_sha", "missing_event", "missing_row"} {
		t.Run(variant, func(t *testing.T) {
			f := newRepairResultBlockerReaderFixture(t)
			switch variant {
			case "artifact_bytes":
				writeBlockerReaderFile(t, f.runtime.plan.root, f.artifactRef, append(mustReadBlockerReaderFile(t, f.runtime.plan.root, f.artifactRef), ' '))
			case "message_bytes":
				writeBlockerReaderFile(t, f.runtime.plan.root, f.messageRef, append(mustReadBlockerReaderFile(t, f.runtime.plan.root, f.messageRef), ' '))
			case "issue_key":
				f.runtime.events[8]["idempotency_key"] = "packet:forged"
				writeBlockerReaderJournal(t, &f)
			case "blocker_event_refs":
				f.runtime.events[len(f.runtime.events)-1]["evidence_ids"] = []any{f.messageRef}
				writeBlockerReaderJournal(t, &f)
			case "evidence_sha":
				for _, row := range objectRows(f.runtime.plan.state["evidence"]) {
					if row["id"] == f.artifactRef {
						row["sha256"] = strings.Repeat("f", 64)
					}
				}
			case "missing_event":
				f.runtime.events = f.runtime.events[:len(f.runtime.events)-1]
				writeBlockerReaderJournal(t, &f)
			case "missing_row":
				rows := objectRows(f.runtime.plan.state["evidence"])
				filtered := make([]any, 0, len(rows)-1)
				for _, row := range rows {
					if row["id"] != f.artifactRef {
						filtered = append(filtered, row)
					}
				}
				f.runtime.plan.state["evidence"] = filtered
			}
			if _, found, err := VerifiedRepairResultBlockerReceipt(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet.ID, f.runtime.packet.Binding.Sources.OwnerAgentID); err == nil || found {
				t.Fatalf("tampered/orphan blocker history was accepted: found=%t", found)
			}
		})
	}
}

func TestVerifiedRepairResultBlockerReceiptRejectsDuplicateCommittedIdempotencyKeys(t *testing.T) {
	for _, variant := range []string{"packet_issue", "blocker_event_without_matching_ref"} {
		t.Run(variant, func(t *testing.T) {
			f := newRepairResultBlockerReaderFixture(t)
			p := f.runtime.packet
			b := p.Binding.Sources
			var duplicate Mutation
			switch variant {
			case "packet_issue":
				duplicate = Mutation{EventID: "evt-duplicate-packet-issue", TransitionID: "PACKET-ISSUE", Event: "transition_committed", Actor: "orchestrator", IdempotencyKey: "packet:" + p.ID, RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, From: map[string]any{"state": "bug_resolution", "phase": "fixing"}, To: map[string]any{"state": "bug_resolution", "phase": "fixing"}}
			case "blocker_event_without_matching_ref":
				duplicate = Mutation{EventID: fmt.Sprintf("evt-agent-%s-work_blocked-r10-duplicate", b.OwnerAgentID), TransitionID: "AGENT-LIFECYCLE", Event: "work_blocked", Actor: "orchestrator", IdempotencyKey: fmt.Sprintf("runtime:agent:%s:work_blocked:9", b.OwnerAgentID), RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, From: map[string]any{"state": "bug_resolution", "phase": "fixing"}, To: map[string]any{"state": "bug_resolution", "phase": "fixing"}, EvidenceIDs: []string{"unrelated-evidence"}, Message: "Recorded a schema-valid Agent lifecycle event (work_blocked)"}
			}
			duplicateEvent := buildJournalEvent(duplicate, b.RuntimeID, 9, 10, 11, time.Date(2026, 10, 14, 12, 11, 0, 0, time.UTC))
			f.runtime.events = append(f.runtime.events, duplicateEvent)
			writeBlockerReaderJournal(t, &f)
			if _, found, err := VerifiedRepairResultBlockerReceipt(f.runtime.plan.root, f.runtime.plan.state, p.ID, b.OwnerAgentID); err == nil || found {
				t.Fatalf("duplicate committed idempotency key was accepted: found=%t", found)
			}
		})
	}
}

func TestVerifiedRepairResultBlockerReceiptIgnoresNonCommittedSameKey(t *testing.T) {
	f := newRepairResultBlockerReaderFixture(t)
	p := f.runtime.packet
	b := p.Binding.Sources
	warning := buildJournalEvent(Mutation{
		EventID: "warning-reuses-uncommitted-key", JournalEvent: "warning_issued", JournalOutcome: "warning",
		Actor: "orchestrator", IdempotencyKey: "packet:" + p.ID, RuntimeID: b.RuntimeID,
		BaselineGeneration: b.BaselineGeneration,
		From:               map[string]any{"state": "bug_resolution", "phase": "fixing"},
		To:                 map[string]any{"state": "bug_resolution", "phase": "fixing"},
		Message:            "A noncommitted warning is not a second issue acceptance.",
	}, b.RuntimeID, 10, 10, 11, time.Date(2026, 10, 14, 12, 11, 0, 0, time.UTC))
	f.runtime.events = append(f.runtime.events, warning)
	writeBlockerReaderJournal(t, &f)
	if _, found, err := VerifiedRepairResultBlockerReceipt(f.runtime.plan.root, f.runtime.plan.state, p.ID, b.OwnerAgentID); err != nil || !found {
		t.Fatalf("noncommitted same-key warning invalidated historical receipt: found=%t err=%v", found, err)
	}
}

func mustReadBlockerReaderFile(t *testing.T, root, ref string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
