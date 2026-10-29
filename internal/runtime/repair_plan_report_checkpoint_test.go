package runtime

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// This adds a mechanically assembled accepted receipt to the source-proof
// fixture. It validates Runtime's retained evidence checks, not the original
// repair-domain Writer or a real human approval process.
func acceptRepairPlanReportFixture(t *testing.T, f *repairPlanReportRuntimeFixture) RepairPlanReportReceipt {
	t.Helper()
	answer := json.RawMessage(`{"plan":"Reproduce the approved invariant failure, then make the smallest scoped repair.","red_checks":[{"name":"original failure","command":"go test ./internal/api -run TestPayloadRoundTrip","result":"fail","evidence_refs":["test://s9/before-fix"]}],"proposed_paths":["internal/api/payload.go"]}`)
	draft := f.packet.Draft
	draft.Answer = answer
	draftBytes, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	accepted, fields := workpacket.ValidateRepairPlanReportAnswer(f.packet, draftBytes)
	if len(fields) != 0 {
		t.Fatalf("synthetic complete PlanReport answer rejected: %#v", fields)
	}
	packetSHA, err := workpacket.RepairPlanReportPacketDigest(f.packet)
	if err != nil {
		t.Fatal(err)
	}
	issuedAt := "2026-10-14T12:05:00Z"
	acceptedEventID := "evt-s9-issued-plan-report-" + f.packet.ID
	acceptedKey := "runtime:s9:issued-plan-report:" + f.packet.ID
	const (
		reportRef = ".claude/review/repair/plan-reports/"
	)
	fullReportRef := reportRef + f.packet.ReportID + ".json"
	receipt := RepairPlanReportReceipt{
		Contract: "s9-plan-report-receipt-v1", PacketID: f.packet.ID, PacketSHA256: packetSHA,
		AnswerSHA256: workpacket.Digest(accepted), SubmittedAnswer: accepted,
		OwnerAgentID: f.packet.Binding.OwnerAgentID, RuntimeID: f.packet.Binding.RuntimeID,
		BaselineGeneration: f.packet.Binding.BaselineGeneration, ReviewRound: f.packet.Binding.ReviewRound,
		CaseID: f.packet.Binding.CaseID, CaseRef: f.packet.Binding.CaseRef, CaseSHA256: f.packet.Binding.CaseSHA256,
		ContractID: f.packet.Binding.ContractID, ContractRef: f.packet.Binding.ContractRef, ContractSHA256: f.packet.Binding.ContractSHA256,
		SessionID: f.packet.Binding.SessionID, SessionRef: f.packet.Binding.SessionRef, SessionSHA256: f.packet.Binding.SessionSHA256,
		PlanID: f.packet.Binding.PlanID, PlanRef: f.packet.Binding.PlanRef, PlanSHA256: f.packet.Binding.PlanSHA256,
		RepairAssignmentID: f.packet.Binding.RepairAssignmentID, RepairAssignmentSHA256: f.packet.Binding.RepairAssignmentSHA256,
		ManifestAssignmentID: f.packet.Binding.ManifestAssignmentID, ManifestAssignmentSHA256: f.packet.Binding.ManifestAssignmentSHA256,
		TeamID: f.packet.Binding.TeamID, TaskID: f.packet.Binding.TaskID, TaskRef: f.packet.Binding.TaskRef, TaskSHA256: f.packet.Binding.TaskSHA256,
		ManifestRef: f.packet.Binding.ManifestRef, ManifestSHA256: f.packet.Binding.ManifestSHA256,
		ExecutionRoot: f.root, ReportID: f.packet.ReportID, ReportRef: fullReportRef, WorkComplete: false,
		IssuanceEventID: f.packet.IssuanceEventID, AcceptedEventID: acceptedEventID, AcceptedIdempotencyKey: acceptedKey,
		AcceptedAfterRuntimeRevision: 6, AcceptedSequence: 6,
	}
	var answerFields map[string]any
	if err := json.Unmarshal(accepted, &answerFields); err != nil {
		t.Fatal(err)
	}
	report := map[string]any{
		"schema_version": "1.0.0", "record_type": "repair_plan_report", "report_id": f.packet.ReportID,
		"session_id": f.packet.Binding.SessionID, "plan_id": f.packet.Binding.PlanID,
		"assignment_id": f.packet.Binding.RepairAssignmentID, "assertion_ids": []string{"root-1"},
		"agent_id": f.packet.Binding.OwnerAgentID,
		"plan":     answerFields["plan"], "red_checks": answerFields["red_checks"], "proposed_paths": answerFields["proposed_paths"],
		"status": "reported", "reported_at": issuedAt, "receipt": receipt,
	}
	reportBytes := writeRepairPlanReportJSON(t, f.root, fullReportRef, report)
	repair := f.state["review"].(map[string]any)["repair"].(map[string]any)
	indexedRef := map[string]any{"path": fullReportRef, "sha256": workpacket.Digest(reportBytes)}
	repair["plan_report_refs"] = []any{indexedRef}
	repair["plan_report_ref"] = fullReportRef
	repair["plan_report_sha256"] = workpacket.Digest(reportBytes)
	repair["status"] = "reproducing"
	f.state["lifecycle"].(map[string]any)["phase"] = "reproducing"
	acceptedEvent := buildJournalEvent(Mutation{
		EventID: acceptedEventID, TransitionID: "PTR-BUG-10", Event: "transition_committed",
		Actor: f.packet.Binding.OwnerAgentID, IdempotencyKey: acceptedKey, RuntimeID: f.packet.Binding.RuntimeID,
		BaselineGeneration: f.packet.Binding.BaselineGeneration,
		From:               map[string]any{"state": "bug_resolution", "phase": "planning"},
		To:                 map[string]any{"state": "bug_resolution", "phase": "reproducing"}, EvidenceIDs: []string{fullReportRef},
	}, f.packet.Binding.RuntimeID, 5, 6, 6, time.Date(2026, 10, 14, 12, 6, 0, 0, time.UTC))
	f.journal = s8CheckpointJournalBytes(t, f.approvalEvent, f.sessionEvent, f.planEvent, f.registerEvent, f.issueEvent, acceptedEvent)
	f.state["revision"] = 6
	f.state["journal"].(map[string]any)["last_sequence"] = 6
	f.state["journal"].(map[string]any)["last_event_id"] = acceptedEventID
	writeRepairPlanReportJournal(t, f)
	return receipt
}

func writeRepairPlanReportJSON(t *testing.T, root, ref string, value any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return data
}

func readRepairPlanReportJSON(t *testing.T, root, ref string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref)))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func updateRepairPlanReportArtifact(t *testing.T, f *repairPlanReportRuntimeFixture, mutate func(map[string]any)) {
	t.Helper()
	repair := f.state["review"].(map[string]any)["repair"].(map[string]any)
	ref := repair["plan_report_ref"].(string)
	doc := readRepairPlanReportJSON(t, f.root, ref)
	mutate(doc)
	data := writeRepairPlanReportJSON(t, f.root, ref, doc)
	sha := workpacket.Digest(data)
	repair["plan_report_sha256"] = sha
	repair["plan_report_refs"].([]any)[0].(map[string]any)["sha256"] = sha
}

func TestRepairPlanReportReceiptAndCheckpointValidateOriginalAcceptedReport(t *testing.T) {
	f := newRepairPlanReportRuntimeFixture(t)
	receipt := acceptRepairPlanReportFixture(t, &f)
	before, err := json.Marshal(f.state)
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := VerifiedRepairPlanReportReceipt(f.root, f.state, f.packet.ID, f.packet.Binding.OwnerAgentID)
	if err != nil || !found || got.PacketID != f.packet.ID || got.AcceptedEventID != receipt.AcceptedEventID {
		t.Fatalf("verified S9 receipt found=%t receipt=%+v err=%v", found, got, err)
	}
	if got.WorkComplete {
		t.Fatal("pre-execution PlanReport receipt must not mark work complete")
	}
	filtered, err := validateRepairPlanReportCheckpointIndices(f.root, f.state, f.journal)
	if err != nil {
		t.Fatalf("S9 accepted packet/report checkpoint rejected: %v", err)
	}
	rows := filtered["entities"].(map[string]any)["work_packets"].([]any)
	if len(rows) != 0 {
		t.Fatalf("S9 typed packet was not hidden from the legacy checkpoint decoder: %#v", rows)
	}
	if err := ValidateCheckpointIndices(f.root, f.state, f.journal); err != nil {
		t.Fatalf("full Runtime checkpoint validator rejected S9 accepted report: %v", err)
	}
	after, _ := json.Marshal(f.state)
	if string(before) != string(after) {
		t.Fatal("receipt lookup/checkpoint validation mutated Runtime state")
	}
}

func TestRepairPlanReportReceiptIndentRoundTripCanonicalizesFullAnswerAndRejectsDuplicateKeys(t *testing.T) {
	f := newRepairPlanReportRuntimeFixture(t)
	receipt := acceptRepairPlanReportFixture(t, &f)
	originalAnswer := append([]byte(nil), receipt.SubmittedAnswer...)
	originalAnswerSHA := workpacket.Digest(originalAnswer)
	if originalAnswerSHA != receipt.AnswerSHA256 {
		t.Fatalf("fixture full-answer SHA = %s, receipt SHA = %s", originalAnswerSHA, receipt.AnswerSHA256)
	}

	// The original PlanReport writer indents its immutable artifact. The typed
	// receipt must normalize the embedded RawMessage back to the same canonical
	// full answer before receipt deep-equality and digest verification.
	indented, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var decoded RepairPlanReportReceipt
	if err := json.Unmarshal(indented, &decoded); err != nil {
		t.Fatalf("unmarshal indented original S9 receipt: %v", err)
	}
	if !reflect.DeepEqual(decoded, receipt) || !bytes.Equal(decoded.SubmittedAnswer, originalAnswer) || workpacket.Digest(decoded.SubmittedAnswer) != receipt.AnswerSHA256 {
		t.Fatalf("S9 full-answer receipt changed across JSON indentation round-trip: equal=%v answerSHA=%s receiptSHA=%s", reflect.DeepEqual(decoded, receipt), workpacket.Digest(decoded.SubmittedAnswer), receipt.AnswerSHA256)
	}

	// Duplicate fields inside the answer remain rejected even though the outer
	// receipt has a custom canonicalizing UnmarshalJSON path.
	duplicate := receipt
	duplicate.SubmittedAnswer = json.RawMessage(`{"plan":"first","plan":"second"}`)
	duplicateBytes, err := json.MarshalIndent(duplicate, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(duplicateBytes, &RepairPlanReportReceipt{}); err == nil {
		t.Fatal("S9 receipt accepted duplicate professional answer keys")
	}
}

func TestRepairPlanReportReceiptRejectsAnswerSourceIndexAndEventDrift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, *repairPlanReportRuntimeFixture)
	}{
		{"professional answer differs from receipt", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			updateRepairPlanReportArtifact(t, f, func(doc map[string]any) { doc["plan"] = "silently changed accepted answer" })
		}},
		{"receipt answer digest differs", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			updateRepairPlanReportArtifact(t, f, func(doc map[string]any) {
				doc["receipt"].(map[string]any)["answer_sha256"] = strings.Repeat("0", 64)
			})
		}},
		{"source drift", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			mutateRepairPlanReportSource(t, f, f.packet.Binding.ContractRef, func(doc map[string]any) { doc["root_cause_statement"] = "changed after issue" })
		}},
		{"accepted event identity", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			f.journal = s8CheckpointChangeJournalEvent(t, f.journal, 5, func(e map[string]any) { e["idempotency_key"] = "wrong" })
			writeRepairPlanReportJournal(t, f)
		}},
		{"accepted event generation", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			f.journal = s8CheckpointChangeJournalEvent(t, f.journal, 5, func(e map[string]any) { e["baseline_generation"] = 3 })
			writeRepairPlanReportJournal(t, f)
		}},
		{"accepted event runtime revision", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			f.journal = s8CheckpointChangeJournalEvent(t, f.journal, 5, func(e map[string]any) { e["after_revision"] = 7 })
			writeRepairPlanReportJournal(t, f)
		}},
		{"receipt owner tuple", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			updateRepairPlanReportArtifact(t, f, func(doc map[string]any) {
				doc["receipt"].(map[string]any)["owner_agent_id"] = "builder-other"
			})
		}},
		{"missing indexed report ref", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			repair := f.state["review"].(map[string]any)["repair"].(map[string]any)
			repair["plan_report_refs"] = []any{}
			delete(repair, "plan_report_ref")
			delete(repair, "plan_report_sha256")
		}},
		{"duplicate receipt path", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			repair := f.state["review"].(map[string]any)["repair"].(map[string]any)
			original := repair["plan_report_refs"].([]any)[0].(map[string]any)
			doc := readRepairPlanReportJSON(t, f.root, original["path"].(string))
			duplicateRef := ".claude/review/repair/plan-reports/duplicate.json"
			data := writeRepairPlanReportJSON(t, f.root, duplicateRef, doc)
			repair["plan_report_refs"] = append(repair["plan_report_refs"].([]any), map[string]any{"path": duplicateRef, "sha256": workpacket.Digest(data)})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepairPlanReportRuntimeFixture(t)
			acceptRepairPlanReportFixture(t, &f)
			tc.mutate(t, &f)
			if _, _, err := VerifiedRepairPlanReportReceipt(f.root, f.state, f.packet.ID, f.packet.Binding.OwnerAgentID); err == nil {
				t.Fatal("tampered or incomplete original S9 receipt was accepted")
			}
			if _, err := validateRepairPlanReportCheckpointIndices(f.root, f.state, f.journal); err == nil {
				t.Fatal("checkpoint index validator accepted tampered or incomplete original receipt")
			}
		})
	}
}
