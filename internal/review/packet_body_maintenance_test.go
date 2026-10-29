package review

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestPacketBodyMaintenanceMigratesLegacyWithOriginalViewAndPrivateAnswer(t *testing.T) {
	root, statePath, journalPath, snapshot, _ := packetModeFixture(t)
	packets, err := IssueResultPackets(root, statePath, journalPath, snapshot.Revision, "agent-qa-1")
	if err != nil {
		t.Fatal(err)
	}
	p := packets[0]
	issued, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the supported pre-externalization on-disk representation.
	data, _ := json.Marshal(p)
	var original map[string]any
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	issued.State["entities"].(map[string]any)["work_packets"] = []any{original}
	legacyBytes, _ := json.MarshalIndent(issued.State, "", "  ")
	if err := os.WriteFile(statePath, legacyBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, p.ViewPath)); err != nil {
		t.Fatal(err)
	}
	private := []byte("private professional draft remains exact\n")
	if err := os.WriteFile(filepath.Join(root, p.AnswerPath), private, 0600); err != nil {
		t.Fatal(err)
	}
	writer := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(context.Background())
	maintained, err := writer.ExternalizePacketBodies()
	if err != nil {
		t.Fatal(err)
	}
	if maintained.Revision != issued.Revision+1 {
		t.Fatalf("maintenance revision %d", maintained.Revision)
	}
	row := maintained.State["entities"].(map[string]any)["work_packets"].([]any)[0].(map[string]any)
	if _, external, err := packetbody.ReferenceFromIndex(row); err != nil || !external {
		t.Fatalf("maintained row: %v %v", external, err)
	}
	ctx := loopruntime.WithPacketBodyRoot(context.Background(), root)
	got, found, err := loopruntime.FindReviewResultPacketContext(ctx, maintained.State, p.ID)
	if err != nil || !found || !reflect.DeepEqual(got, p) {
		t.Fatalf("original packet changed: %v %v", found, err)
	}
	view, err := os.ReadFile(filepath.Join(root, p.ViewPath))
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := json.MarshalIndent(p, "", "  ")
	if !reflect.DeepEqual(view, append(expected, '\n')) {
		t.Fatal("missing view not restored from original")
	}
	if answer, err := os.ReadFile(filepath.Join(root, p.AnswerPath)); err != nil || !reflect.DeepEqual(answer, private) {
		t.Fatalf("private answer changed: %v", err)
	}
	stateBefore, journalBefore := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	replay, err := writer.ExternalizePacketBodies()
	if err != nil {
		t.Fatal(err)
	}
	if replay.Revision != maintained.Revision || !reflect.DeepEqual(stateBefore, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(journalBefore, mustReadTestFile(t, journalPath)) {
		t.Fatal("maintenance replay created new facts")
	}
}
