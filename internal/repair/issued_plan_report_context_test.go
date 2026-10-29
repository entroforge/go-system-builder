package repair

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/metrics"
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
