package repair

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/metrics"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestRepairPlanReportIssueTimeSharesRequestParserAndKeepsLivePairChecks(t *testing.T) {
	// A published schema example supplies a valid journal row. This test targets
	// only timestamp lookup/parser plumbing, not S9 issuance or approval authority.
	data, err := schema.ReadAsset("loop-event.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func() {
		t.Helper()
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(journalPath, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	state := map[string]any{"runtime_id": event["runtime_id"], "journal": map[string]any{"last_sequence": 1, "last_event_id": event["event_id"]}}
	packet := workpacket.RepairPlanReportPacket{IssuanceEventID: event["event_id"].(string)}
	ctx, timings := metrics.WithCallTimings(controlproof.WithJournalReadCache(t.Context()))
	for i := 0; i < 2; i++ {
		got, err := repairPlanReportIssueTime(root, state, packet, ctx)
		if err != nil || got.Format(time.RFC3339Nano) != event["occurred_at"] {
			t.Fatalf("original issue timestamp changed: %s %v", got, err)
		}
	}
	if timings.Snapshot()["runtime/journal_decode_build"].Count != 1 || timings.Snapshot()["runtime/journal_decode_hit"].Count < 2 {
		t.Fatalf("issue-time lookup reparsed the same request journal: %+v", timings.Snapshot())
	}
	state["journal"].(map[string]any)["last_sequence"] = 2
	if _, err := repairPlanReportIssueTime(root, state, packet, ctx); err == nil {
		t.Fatal("parser cache bypassed the original current state/journal cursor check")
	}
	state["journal"].(map[string]any)["last_sequence"] = 1
	event["occurred_at"] = "2026-06-19T00:00:00Z"
	write()
	got, err := repairPlanReportIssueTime(root, state, packet, ctx)
	if err != nil || got.Format(time.RFC3339Nano) != event["occurred_at"] || timings.Snapshot()["runtime/journal_decode_build"].Count != 2 {
		t.Fatalf("changed journal bytes reused an old issue timestamp: %s %v %+v", got, err, timings.Snapshot())
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repairPlanReportIssueTime(root, state, packet, canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached issue timestamp concealed cancellation: %v", err)
	}
}

// s9WriteCountRoot creates a minimal original-Writer workspace with the
// control sources the Runtime validator reads.
func s9WriteCountRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range []string{"docs/control/loop-definition.json", "docs/control/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		target := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// s9WriteCountState is a schema-valid Runtime state with one current repair
// responsibility and no Runtime write count in any identity.
func s9WriteCountState(t *testing.T, root string, pointer map[string]any) map[string]any {
	t.Helper()
	state := map[string]any{
		"schema_version": "1.1.0",
		"runtime_id":     "loop-req039-ct",
		"definition":     map[string]any{"path": "docs/control/loop-definition.json", "version": "1.1.0"},
		"revision":       float64(0),
		"lifecycle":      map[string]any{"state": "bug_resolution", "phase": "repair_readback", "phase_revision": float64(0)},
		"authorization":  map[string]any{"mode": "binding", "command": "loop-harness req bind", "actor": "user", "occurred_at": "2026-07-30T00:00:00Z"},
		"bound_req": map[string]any{"id": "REQ-039", "path": "docs/requirements/REQ-039-loop-control-plane.md",
			"version": "v2.0.0", "status": "locked", "approved_by": "user", "approved_at": "2026-07-30T00:00:00Z",
			"metadata": map[string]any{"ui_impact": "none"}},
		"baseline":        map[string]any{"generation": float64(1), "captured_at": "2026-07-30T00:00:00Z"},
		"review":          map[string]any{"round": float64(1), "clean_round": nil, "repair": pointer},
		"configuration":   map[string]any{"repair": map[string]any{"max_attempts_per_bug": float64(3), "max_same_contract_failures": float64(2)}},
		"entities":        map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}},
		"documents":       []any{},
		"evidence":        []any{},
		"blockers":        []any{},
		"pause":           nil,
		"last_transition": nil,
		"journal":         map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": float64(0), "last_event_id": nil},
		"hook_control": map[string]any{"policy_ref": map[string]any{"path": "docs/control/hook-policy.json", "version": "v2.0.0"},
			"mode": "enforce", "health": "healthy", "consecutive_failures": float64(0), "last_checked_at": nil},
		"updated_at": "2026-07-30T00:00:00Z",
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return state
}

// s9JournalEventIDs counts the journal events per event id.
func s9JournalEventIDs(t *testing.T, journalPath string) map[string]int {
	t.Helper()
	raw, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if id, ok := event["event_id"].(string); ok {
			ids[id]++
		}
	}
	return ids
}

// G18 §3.2: the S9 PlanReport answer recording does not assert a Runtime write
// count. The omission marker (-1) takes the current-value path, so one
// legitimate professional answer is recorded even when the Runtime moved for an
// unrelated reason. The protection stays in the three domain facts: the
// received row (the recorded plan report reference of this assignment), the
// journal-level dedup (the issued receipt's accepted event id and idempotency
// key) and the current repair responsibility (revalidated inside Apply). The
// Runtime revision is an audit coordinate only
// (L4-revision-usage.md:141).
func TestS9PlanReportAnswerRecordingTakesCurrentValueAndKeepsDomainProtection(t *testing.T) {
	root := s9WriteCountRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	pointer := map[string]any{
		"session_id":   "repair-session-write-count",
		"case_id":      "investigation-case-write-count",
		"contract_id":  "repair-contract-write-count",
		"contract_ref": ".claude/review/investigation/contracts/repair-contract-write-count.json",
		"path":         ".claude/review/repair/sessions/repair-session-write-count.json",
		"plan_id":      "repair-plan-write-count",
		"plan_ref":     ".claude/review/repair/plans/repair-plan-write-count.json",
		"status":       "planning",
		"revision":     float64(1),
		"updated_at":   "2026-08-25T00:00:00Z",
		"next_action":  "record the Builder's own PlanReport",
		"assignment_owners": map[string]any{
			"repair-assignment-unit-1": "builder-1",
		},
	}
	s9WriteCountState(t, root, pointer)

	packet := workpacket.RepairPlanReportPacket{
		ID:         "packet-s9-plan-report-write-count",
		TemplateID: "s9-plan-report-v1",
		Binding: workpacket.RepairPlanReportBinding{
			RuntimeID: "loop-req039-ct", BaselineGeneration: 1,
			CaseID: "investigation-case-write-count", CaseRef: ".claude/review/investigation/cases/investigation-case-write-count.json",
			ContractID: "repair-contract-write-count", ContractRef: ".claude/review/investigation/contracts/repair-contract-write-count.json",
			SessionID: "repair-session-write-count", SessionRef: ".claude/review/repair/sessions/repair-session-write-count.json",
			PlanID: "repair-plan-write-count", PlanRef: ".claude/review/repair/plans/repair-plan-write-count.json",
			RepairAssignmentID:   "repair-assignment-unit-1",
			ManifestAssignmentID: "assignment-s9-unit-1",
			TeamID:               "team-s9", TaskID: "TASK-s9-unit-1", TaskRef: ".claude/workgroup/s9/TASK-s9-unit-1.json",
			ManifestRef:  ".claude/workgroup/s9/team-manifest.json",
			OwnerAgentID: "builder-1",
		},
		ReportID:        "repair-plan-report-write-count",
		IssuanceEventID: "evt-repair-plan-report-packet-packet-s9-plan-report-write-count",
		ViewPath:        ".claude/work-packets/packet-s9-plan-report-write-count.json",
		AnswerPath:      ".claude/submissions/packet-s9-plan-report-write-count/builder-1/answer.json",
	}
	reportPath := responsibilityArtifactPath("plan-reports", packet.ReportID)
	report := PlanReport{
		SchemaVersion: "1.0.0", RecordType: "repair_plan_report", ReportID: packet.ReportID,
		SessionID: packet.Binding.SessionID, PlanID: packet.Binding.PlanID,
		AssignmentID: packet.Binding.RepairAssignmentID, AssertionIDs: []string{"symptom-1"},
		AgentID: packet.Binding.OwnerAgentID, Plan: "restore the payload authority",
		RedChecks:     []RepairCheck{{Name: "original failure", Command: "go test ./internal/api", Result: "fail", EvidenceRefs: []string{"test://red"}}},
		ProposedPaths: []string{"internal/api/payload.go"}, Status: "reported",
		ReportedAt: "2026-08-25T00:00:00Z",
	}
	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, filepath.FromSlash(reportPath))), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(reportPath)), reportBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	receipt := runtimepkg.RepairPlanReportReceipt{
		Contract: repairPlanReportReceiptContract, PacketID: packet.ID,
		SubmittedAnswer: json.RawMessage(`{"plan":"restore the payload authority"}`),
		OwnerAgentID:    packet.Binding.OwnerAgentID, RuntimeID: packet.Binding.RuntimeID, BaselineGeneration: packet.Binding.BaselineGeneration,
		CaseID: packet.Binding.CaseID, CaseRef: packet.Binding.CaseRef,
		ContractID: packet.Binding.ContractID, ContractRef: packet.Binding.ContractRef,
		SessionID: packet.Binding.SessionID, SessionRef: packet.Binding.SessionRef,
		PlanID: packet.Binding.PlanID, PlanRef: packet.Binding.PlanRef,
		RepairAssignmentID: packet.Binding.RepairAssignmentID, ManifestAssignmentID: packet.Binding.ManifestAssignmentID,
		TeamID: packet.Binding.TeamID, TaskID: packet.Binding.TaskID, TaskRef: packet.Binding.TaskRef,
		ManifestRef: packet.Binding.ManifestRef, ExecutionRoot: root,
		ReportID: packet.ReportID, ReportRef: reportPath,
		WorkComplete: false, IssuanceEventID: packet.IssuanceEventID,
		AcceptedEventID: "evt-s9-issued-plan-report-" + packet.ID, AcceptedIdempotencyKey: "runtime:s9:issued-plan-report:" + packet.ID,
	}
	capability := &issuedPlanReportCapability{packet: packet, receipt: receipt, answer: receipt.SubmittedAnswer, source: root}
	submit := func(expectedRevision int) (runtimepkg.Snapshot, PlanReport, error) {
		return submitIssuedRepairPlanReportToRuntime(root, statePath, journalPath, submitIssuedPlanReportRequest{
			RuntimeRequest: RuntimeRequest{ExpectedRevision: expectedRevision, Actor: packet.Binding.OwnerAgentID, OccurredAt: time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)},
			Report:         ArtifactRef{Path: reportPath}, issued: capability,
		})
	}

	// An explicit Runtime write count assertion is still honoured: the KEEP
	// mechanism of the Runtime Writer is unchanged.
	if _, _, err := submit(4); !errors.Is(err, runtimepkg.ErrStaleRevision) {
		t.Fatalf("explicit stale Runtime revision error = %v, want a stale revision refusal", err)
	}

	// The omission marker takes the current-value path: the professional answer
	// is recorded even though the caller named a Runtime revision it read long
	// ago.
	next, committed, err := submit(-1)
	if err != nil {
		t.Fatalf("S9 PlanReport answer refused because the Runtime moved: %v", err)
	}
	if committed.ReportID != packet.ReportID || committed.AgentID != packet.Binding.OwnerAgentID {
		t.Fatalf("committed S9 PlanReport = %+v", committed)
	}
	committedPointer, _ := next.State["review"].(map[string]any)["repair"].(map[string]any)
	if committedPointer["plan_report_ref"] != reportPath || committedPointer["status"] != "repairing" {
		t.Fatalf("current repair responsibility = %v", committedPointer)
	}
	events := s9JournalEventIDs(t, journalPath)
	if events[receipt.AcceptedEventID] != 1 {
		t.Fatalf("issued PlanReport event identity = %v, want exactly one %s", events, receipt.AcceptedEventID)
	}
	raw, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), receipt.AcceptedIdempotencyKey) {
		t.Fatalf("issued PlanReport idempotency key %s is not in the journal: %s", receipt.AcceptedIdempotencyKey, raw)
	}

	// The same answer is still accepted exactly once: the second recording is
	// receipt-only and writes no new Runtime revision and no new journal event.
	replay, _, err := submit(-1)
	if err != nil {
		t.Fatalf("exact S9 PlanReport answer replay: %v", err)
	}
	if replay.Revision != next.Revision {
		t.Fatalf("exact replay revision = %d, want %d", replay.Revision, next.Revision)
	}
	if events := s9JournalEventIDs(t, journalPath); events[receipt.AcceptedEventID] != 1 {
		t.Fatalf("exact replay wrote another S9 PlanReport event: %v", events)
	}

	// The current repair responsibility stays the protection: an answer that no
	// longer belongs to the recorded Plan is refused by the domain check, not by
	// the Runtime write count.
	pointer["plan_ref"] = ".claude/review/repair/plans/repair-plan-other.json"
	s9WriteCountState(t, root, pointer)
	if _, _, err := submit(-1); err == nil || !strings.Contains(err.Error(), "differs from the current packet") {
		t.Fatalf("answer outside the current repair responsibility error = %v", err)
	}
}
