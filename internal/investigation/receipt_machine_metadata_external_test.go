package investigation_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestOriginalHypothesisReceiptIgnoresHistoricalMachineFields(t *testing.T) {
	fixture, owners := prepareS8GenerationTwoFixture(t, false)
	current, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	packets, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, current.Revision, owners[0].ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue original: %d %v", len(packets), err)
	}
	p := packets[0]
	answer := writeS8ProfessionalAnswer(t, fixture.root, p, "The original Finding describes the actual boundary.")
	current, err = runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err := investigation.ReceiveIssuedHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, investigation.IssuedHypothesisResultRequest{ExpectedRevision: -1, PacketID: p.ID, OwnerAgentID: owners[0].ownerID, AnswerPath: answer}, true)
	if err != nil {
		t.Fatal(err)
	}
	ptr := accepted.State["review"].(map[string]any)["investigation"].(map[string]any)
	casePath := filepath.Join(fixture.root, filepath.FromSlash(ptr["path"].(string)))
	for _, old := range []any{nil, "old", float64(7), map[string]any{"old": true}} {
		data, err := os.ReadFile(casePath)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		rows := doc["hypothesis_results"].([]any)
		receipt := rows[len(rows)-1].(map[string]any)["receipt"].(map[string]any)
		for _, key := range []string{"packet_sha256", "answer_sha256", "assignment_sha256", "task_sha256", "manifest_sha256"} {
			receipt[key] = old
		}
		writeInvestigationJSON(t, casePath, doc)
		before, err := os.ReadFile(casePath)
		if err != nil {
			t.Fatal(err)
		}
		got, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, accepted.State, p.ID, owners[0].ownerID)
		if err != nil || !found || got.PacketID != p.ID || got.OwnerAgentID != owners[0].ownerID {
			t.Fatalf("original receipt old=%v found=%v err=%v", old, found, err)
		}
		after, err := os.ReadFile(casePath)
		if err != nil || string(before) != string(after) {
			t.Fatal("receipt read rewrote original Case")
		}
	}
	if _, _, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, accepted.State, p.ID, "other-owner"); err == nil {
		t.Fatal("different owner acquired original receipt")
	}
}
