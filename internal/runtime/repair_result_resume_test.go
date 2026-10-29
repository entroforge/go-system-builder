package runtime

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func issueRepairResultResumeSuccessorFixture(t *testing.T) (repairResultBlockerReaderFixture, workpacket.RepairResultPacket) {
	t.Helper()
	f := newRepairResultBlockerReaderFixture(t)
	attachRepairResultResolutionFixture(t, &f, nil)
	base := f.runtime.packet.Binding
	resume, err := RepairResultResumeBinding(f.runtime.plan.root, f.runtime.plan.state, base)
	if err != nil || resume == nil {
		t.Fatalf("derive Resume from accepted resolution: resume=%+v err=%v", resume, err)
	}
	if resume.PredecessorPacketID != f.runtime.packet.ID || resume.BlockerRef != f.artifactRef {
		t.Fatalf("derived Resume is not bound to original blocker: %+v", resume)
	}
	base.Resume = resume
	successor, err := workpacket.BuildRepairResult(base, f.runtime.packet.Inputs)
	if err != nil {
		t.Fatalf("build deterministic S9 Resume successor: %v", err)
	}
	registerRepairResultResumePacket(t, &f, successor, 11, 12, 12)
	return f, successor
}

func registerRepairResultResumePacket(t *testing.T, f *repairResultBlockerReaderFixture, packet workpacket.RepairResultPacket, before, after, sequence int) {
	t.Helper()
	rowBytes, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(rowBytes, &row); err != nil {
		t.Fatal(err)
	}
	entities := f.runtime.plan.state["entities"].(map[string]any)
	entities["work_packets"] = append(entities["work_packets"].([]any), row)
	b := packet.Binding.Sources
	event := buildJournalEvent(Mutation{
		EventID: packet.IssuanceEventID, TransitionID: "PACKET-ISSUE", Event: "transition_committed",
		Actor: "orchestrator", IdempotencyKey: "packet:" + packet.ID, RuntimeID: b.RuntimeID,
		BaselineGeneration: b.BaselineGeneration,
		From:               map[string]any{"state": "bug_resolution", "phase": "fixing"},
		To:                 map[string]any{"state": "bug_resolution", "phase": "fixing"},
	}, b.RuntimeID, before, after, sequence, time.Date(2026, 10, 14, 12, 12, 0, 0, time.UTC))
	f.runtime.events = append(f.runtime.events, event)
	writeBlockerReaderJournal(t, f)
}

func TestRepairResultResumeLineageSelectsOneCurrentSuccessor(t *testing.T) {
	f, successor := issueRepairResultResumeSuccessorFixture(t)
	owner := successor.Binding.Sources.OwnerAgentID
	if err := VerifyRepairResultResume(f.runtime.plan.root, f.runtime.plan.state, successor); err != nil {
		t.Fatalf("verify accepted blocker → Main resolution → successor chain: %v", err)
	}
	selected, err := CurrentRepairResultPackets(f.runtime.plan.root, f.runtime.plan.state, owner)
	if err != nil || len(selected) != 1 || selected[0].ID != successor.ID {
		t.Fatalf("current selector did not retire predecessor and select successor: selected=%+v err=%v", selected, err)
	}
	if err := VerifyRepairResultPacketCurrent(f.runtime.plan.root, f.runtime.plan.state, successor); err != nil {
		t.Fatalf("verified Resume successor did not pass current packet check: %v", err)
	}
	if err := VerifyRepairResultPacketCurrent(f.runtime.plan.root, f.runtime.plan.state, f.runtime.packet); err == nil {
		t.Fatal("retired Resume predecessor still passed the current packet check")
	}
	pending, err := RepairResultResumePending(f.runtime.plan.root, f.runtime.plan.state, owner, successor.Binding.Sources.RepairAssignmentID)
	if err != nil || pending {
		t.Fatalf("unique current successor did not release ResumePending: pending=%t err=%v", pending, err)
	}
	old, found, err := FindRepairResultPacket(f.runtime.plan.state, f.runtime.packet.ID)
	if err != nil || !found || old.ID != f.runtime.packet.ID {
		t.Fatalf("historical predecessor packet was not retained: packet=%+v err=%v", old, err)
	}
}

func TestVerifyCurrentPreservesUnissuedCandidatePreparation(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	entities := f.plan.state["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	kept := make([]any, 0, len(rows))
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["packet_id"] != f.packet.ID {
			kept = append(kept, raw)
		}
	}
	entities["work_packets"] = kept
	if _, found, err := FindRepairResultPacket(f.plan.state, f.packet.ID); err != nil || found {
		t.Fatalf("fixture still contains an issued row for the build candidate: found=%t err=%v", found, err)
	}
	if len(f.events) < 2 || f.events[len(f.events)-1]["event_id"] != f.packet.IssuanceEventID {
		t.Fatal("fixture does not end with the packet issuance event")
	}
	f.events = f.events[:len(f.events)-1]
	f.plan.journal = s8CheckpointJournalBytes(t, f.events...)
	f.plan.state["revision"] = 8
	journalState := f.plan.state["journal"].(map[string]any)
	journalState["last_sequence"] = 8
	journalState["last_event_id"] = f.events[len(f.events)-1]["event_id"]
	writeRepairPlanReportJournal(t, &f.plan)
	if err := VerifyRepairResultPacketCurrent(f.plan.root, f.plan.state, f.packet); err != nil {
		t.Fatalf("unissued candidate failed its pre-issuance source/current check: %v", err)
	}
}

func TestCurrentRepairResultSelectorRejectsTwoIssuedPacketsForOneAssignment(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)

	// Make a second canonical packet for the same current Assignment slot. The
	// distinct PlanReport ref gives it a distinct packet identity; registering
	// both exact rows and issuance events must still fail the current selector.
	binding := f.packet.Binding
	binding.PlanReportRef += ".duplicate.json"
	var report map[string]any
	if err := json.Unmarshal(f.packet.Inputs.PlanReport, &report); err != nil {
		t.Fatal(err)
	}
	receipt, ok := report["receipt"].(map[string]any)
	if !ok {
		t.Fatal("source PlanReport fixture has no receipt object")
	}
	receipt["report_ref"] = binding.PlanReportRef
	reportBytes, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	binding.PlanReportSHA256 = workpacket.Digest(reportBytes)
	inputs := f.packet.Inputs
	inputs.PlanReport = reportBytes
	duplicate, err := workpacket.BuildRepairResult(binding, inputs)
	if err != nil {
		t.Fatalf("build second canonical packet for the same Assignment: %v", err)
	}
	if duplicate.ID == f.packet.ID || duplicate.Binding.Sources.RepairAssignmentID != f.packet.Binding.Sources.RepairAssignmentID {
		t.Fatalf("fixture did not create a distinct packet in the same Assignment slot: original=%s duplicate=%s", f.packet.ID, duplicate.ID)
	}
	rowBytes, err := json.Marshal(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(rowBytes, &row); err != nil {
		t.Fatal(err)
	}
	entities := f.plan.state["entities"].(map[string]any)
	entities["work_packets"] = append(entities["work_packets"].([]any), row)

	review := f.plan.state["review"].(map[string]any)
	pointer := review["repair"].(map[string]any)
	refs, _ := pointer["plan_report_refs"].([]any)
	pointer["plan_report_refs"] = append(refs, map[string]any{"path": duplicate.Binding.PlanReportRef, "sha256": duplicate.Binding.PlanReportSHA256})

	issue := buildJournalEvent(Mutation{
		EventID: duplicate.IssuanceEventID, TransitionID: "PACKET-ISSUE", Event: "transition_committed",
		Actor: "orchestrator", IdempotencyKey: "packet:" + duplicate.ID, RuntimeID: binding.Sources.RuntimeID,
		BaselineGeneration: binding.Sources.BaselineGeneration,
		From:               map[string]any{"state": "bug_resolution", "phase": "fixing"},
		To:                 map[string]any{"state": "bug_resolution", "phase": "fixing"},
	}, binding.Sources.RuntimeID, 9, 10, 10, time.Date(2026, 10, 14, 12, 10, 0, 0, time.UTC))
	events := append(append([]map[string]any{}, f.events...), issue)
	f.plan.journal = s8CheckpointJournalBytes(t, events...)
	f.plan.state["revision"] = 10
	journalState := f.plan.state["journal"].(map[string]any)
	journalState["last_sequence"] = 10
	journalState["last_event_id"] = duplicate.IssuanceEventID
	writeRepairPlanReportJournal(t, &f.plan)

	if err := VerifyRepairResultPacketCurrent(f.plan.root, f.plan.state, f.packet); err == nil {
		t.Fatal("current check accepted two issued packets for one RepairAssignment")
	}
}

func TestRepairResultResumeRejectsForgedTupleAndDuplicateRegistration(t *testing.T) {
	t.Run("different original resolution event", func(t *testing.T) {
		f, packet := issueRepairResultResumeSuccessorFixture(t)
		if err := VerifyRepairResultResume(f.runtime.plan.root, f.runtime.plan.state, packet); err != nil {
			t.Fatalf("original accepted recovery is not valid: %v", err)
		}
		packet.Binding.Resume.ResolutionEventID = "another-original-resolution-event"
		if err := VerifyRepairResultResume(f.runtime.plan.root, f.runtime.plan.state, packet); err == nil {
			t.Fatal("Resume borrowed an event absent from original acceptance")
		}
	})
	t.Run("forged resolution digest", func(t *testing.T) {
		f, packet := issueRepairResultResumeSuccessorFixture(t)
		packet.Binding.Resume.ResolutionMessageSHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		if err := VerifyRepairResultResume(f.runtime.plan.root, f.runtime.plan.state, packet); err == nil {
			t.Fatal("forged resolution digest was accepted")
		}
	})
	t.Run("duplicate registered successor", func(t *testing.T) {
		f, packet := issueRepairResultResumeSuccessorFixture(t)
		rowBytes, err := json.Marshal(packet)
		if err != nil {
			t.Fatal(err)
		}
		var row map[string]any
		if err := json.Unmarshal(rowBytes, &row); err != nil {
			t.Fatal(err)
		}
		entities := f.runtime.plan.state["entities"].(map[string]any)
		entities["work_packets"] = append(entities["work_packets"].([]any), row)
		if err := VerifyRepairResultResume(f.runtime.plan.root, f.runtime.plan.state, packet); err == nil {
			t.Fatal("duplicate successor registry identity was accepted")
		}
	})
}

func TestRepairResultResumeRejectsResolutionAfterSuccessorIssue(t *testing.T) {
	f, packet := issueRepairResultResumeSuccessorFixture(t)
	owner := packet.Binding.Sources.OwnerAgentID
	resolutionRef := packet.Binding.Resume.ResolutionMessageRef
	blockerRef := packet.Binding.Resume.BlockerRef
	resolutionID := len(f.runtime.events) - 2
	issueID := len(f.runtime.events) - 1
	if f.runtime.events[resolutionID]["event_id"] != packet.Binding.Resume.ResolutionEventID || f.runtime.events[issueID]["event_id"] != packet.IssuanceEventID {
		t.Fatal("fixture event order is not resolution then successor issue")
	}
	// Reorder the two accepted events while retaining a structurally valid,
	// contiguous journal and an otherwise exact successor tuple.
	packet.Binding.Resume.ResolutionEventID = fmt.Sprintf("evt-agent-%s-blocker_resolved-r12", owner)
	packet.Binding.Resume.ResolutionIdempotencyKey = fmt.Sprintf("runtime:agent:%s:blocker_resolved:11", owner)
	packet.Binding.Resume.ResolutionBeforeRevision = 11
	packet.Binding.Resume.ResolutionAfterRevision = 12
	packet.Binding.Resume.ResolutionSequence = 12
	rebuiltPacket, err := workpacket.BuildRepairResult(packet.Binding, packet.Inputs)
	if err != nil {
		t.Fatalf("rebuild successor with future resolution tuple: %v", err)
	}
	packet = rebuiltPacket
	entities := f.runtime.plan.state["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	rowBytes, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var successorRow map[string]any
	if err := json.Unmarshal(rowBytes, &successorRow); err != nil {
		t.Fatal(err)
	}
	oldPacketID := f.runtime.events[issueID]["idempotency_key"].(string)
	oldPacketID = oldPacketID[len("packet:"):]
	replaced := false
	for i, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["packet_id"] == oldPacketID {
			rows[i] = successorRow
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("fixture lacks the original successor row")
	}
	issueEvent := buildJournalEvent(Mutation{
		EventID: packet.IssuanceEventID, TransitionID: "PACKET-ISSUE", Event: "transition_committed",
		Actor: "orchestrator", IdempotencyKey: "packet:" + packet.ID, RuntimeID: packet.Binding.Sources.RuntimeID,
		BaselineGeneration: packet.Binding.Sources.BaselineGeneration,
		From:               map[string]any{"state": "bug_resolution", "phase": "fixing"},
		To:                 map[string]any{"state": "bug_resolution", "phase": "fixing"},
	}, packet.Binding.Sources.RuntimeID, 10, 11, 11, time.Date(2026, 10, 14, 12, 12, 0, 0, time.UTC))
	resolutionEvent := buildJournalEvent(Mutation{
		EventID:      fmt.Sprintf("evt-agent-%s-blocker_resolved-r12", owner),
		TransitionID: "AGENT-LIFECYCLE", Event: "blocker_resolved", Actor: "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:agent:%s:blocker_resolved:11", owner),
		RuntimeID:      packet.Binding.Sources.RuntimeID, BaselineGeneration: packet.Binding.Sources.BaselineGeneration,
		From:        map[string]any{"state": "bug_resolution", "phase": "fixing"},
		To:          map[string]any{"state": "bug_resolution", "phase": "fixing"},
		EvidenceIDs: []string{resolutionRef, blockerRef},
		Message:     "Recorded a schema-valid Agent lifecycle event (blocker_resolved)",
	}, packet.Binding.Sources.RuntimeID, 11, 12, 12, time.Date(2026, 10, 14, 12, 13, 0, 0, time.UTC))
	f.runtime.events[resolutionID], f.runtime.events[issueID] = issueEvent, resolutionEvent
	writeBlockerReaderJournal(t, &f)
	if err := VerifyRepairResultResume(f.runtime.plan.root, f.runtime.plan.state, packet); err == nil {
		t.Fatal("successor issued before its resolution was accepted")
	}
}

func TestRepairResultResumeSurvivesHistoricalResolutionInvalidation(t *testing.T) {
	f, successor := issueRepairResultResumeSuccessorFixture(t)
	for _, row := range objectRows(f.runtime.plan.state["evidence"]) {
		if row["path"] == successor.Binding.Resume.ResolutionMessageRef {
			row["status"] = "invalid"
			row["invalidated_by"] = "evt-later-invalidation"
			row["invalidation_rule"] = "historical-main-resolution"
			row["invalidation_reason"] = "Historical receipt remains accepted."
		}
	}
	if err := VerifyRepairResultResume(f.runtime.plan.root, f.runtime.plan.state, successor); err != nil {
		t.Fatalf("historical resolution invalidation erased Resume lineage: %v", err)
	}
	selected, err := CurrentRepairResultPackets(f.runtime.plan.root, f.runtime.plan.state, successor.Binding.Sources.OwnerAgentID)
	if err != nil || len(selected) != 1 || selected[0].ID != successor.ID {
		t.Fatalf("invalidating later-current resolution evidence revived predecessor: selected=%+v err=%v", selected, err)
	}
}

func TestRepairResultResumeTupleDigestIsExact(t *testing.T) {
	f, packet := issueRepairResultResumeSuccessorFixture(t)
	got, err := workpacket.RepairResultResumeSHA256(packet.Binding.Resume)
	if err != nil || got == "" {
		t.Fatalf("digest Resume tuple: digest=%q err=%v", got, err)
	}
	mutated := *packet.Binding.Resume
	mutated.ResolutionSequence++
	changed, err := workpacket.RepairResultResumeSHA256(&mutated)
	if err != nil || changed == got {
		t.Fatalf("Resume digest failed to bind sequence: got=%q changed=%q err=%v", got, changed, err)
	}
	if reflect.DeepEqual(&mutated, packet.Binding.Resume) {
		t.Fatal("test mutation did not change Resume tuple")
	}
	if err := VerifyRepairResultResume(f.runtime.plan.root, f.runtime.plan.state, packet); err != nil {
		t.Fatalf("original successor Resume digest fixture no longer verifies: %v", err)
	}
}

func TestRepairResultResumeVerifierMemoizesProofWithinOneSnapshot(t *testing.T) {
	f, successor := issueRepairResultResumeSuccessorFixture(t)
	journal, err := repairResultResumeJournal(f.runtime.plan.root, f.runtime.plan.state)
	if err != nil {
		t.Fatal(err)
	}
	packets, err := RepairResultPackets(f.runtime.plan.state)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := newRepairResultResumeVerifier(f.runtime.plan.root, f.runtime.plan.state, journal, packets)
	if err != nil {
		t.Fatal(err)
	}
	predecessor, found := verifier.byID[successor.Binding.Resume.PredecessorPacketID]
	if !found {
		t.Fatal("parsed snapshot registry lacks Resume predecessor")
	}
	if _, resolved, err := verifier.resolution(predecessor, predecessor.Binding.Sources.OwnerAgentID); err != nil || !resolved {
		t.Fatalf("verify original resolution: resolved=%t err=%v", resolved, err)
	}
	if err := verifier.verifyResume(successor); err != nil {
		t.Fatalf("verify successor lineage: %v", err)
	}
	issueCount, blockerCount, resolutionCount, digestCount, lineageCount := len(verifier.issues), len(verifier.blockers), len(verifier.resolutions), len(verifier.packetDigests), len(verifier.lineages)
	if _, resolved, err := verifier.resolution(predecessor, predecessor.Binding.Sources.OwnerAgentID); err != nil || !resolved {
		t.Fatalf("reuse original resolution proof: resolved=%t err=%v", resolved, err)
	}
	if err := verifier.verifyResume(successor); err != nil {
		t.Fatalf("reuse successor lineage proof: %v", err)
	}
	if len(verifier.issues) != issueCount || len(verifier.blockers) != blockerCount ||
		len(verifier.resolutions) != resolutionCount || len(verifier.packetDigests) != digestCount ||
		len(verifier.lineages) != lineageCount {
		t.Fatal("same-snapshot Resume verification repeated a completed historical proof")
	}
	fresh, err := newRepairResultResumeVerifier(f.runtime.plan.root, f.runtime.plan.state, journal, packets)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.issues)+len(fresh.blockers)+len(fresh.resolutions)+len(fresh.lineages) != 0 {
		t.Fatal("Resume proof cache escaped its request-local verifier")
	}
}

func TestRepairResultResumeReceiptAbsenceBeforeIssuance(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	journal, err := repairResultResumeJournal(f.plan.root, f.plan.state)
	if err != nil {
		t.Fatal(err)
	}
	packets, err := RepairResultPackets(f.plan.state)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := newRepairResultResumeVerifier(f.plan.root, f.plan.state, journal, packets)
	if err != nil {
		t.Fatal(err)
	}
	unissued := f.packet
	unissued.ID = "packet-ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if _, found, err := verifier.resolution(unissued, unissued.Binding.Sources.OwnerAgentID); err != nil || found {
		t.Fatalf("unissued packet invented resolution authority: found=%t err=%v", found, err)
	}
	if _, found, err := verifier.blocker(unissued, unissued.Binding.Sources.OwnerAgentID); err != nil || found {
		t.Fatalf("unissued packet invented blocker authority: found=%t err=%v", found, err)
	}
}
