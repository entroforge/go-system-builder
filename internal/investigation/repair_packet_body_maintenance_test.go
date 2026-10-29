package investigation_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// This starts with original approval, dispatch and candidate Writers, then
// models an installed legacy inline state at that same accepted journal pair.
// Maintenance and checkpoint recovery use their public original mechanisms.
func TestS9LegacyInlineMaintenanceAndCheckpointRestorePreserveOriginalHistory(t *testing.T) {
	accepted := prepareAcceptedS9RepairResultCandidate(t)
	fixture := accepted.fixture
	ctx := runtime.WithPacketBodyRoot(t.Context(), fixture.root)
	writer := runtime.NewWriter(fixture.statePath, fixture.journalPath, fixture.root, semantic.RuntimeCandidateValidator{}).WithContext(t.Context())
	before, err := writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	originalPlans, err := runtime.RepairPlanReportPacketsContext(ctx, before.State)
	if err != nil {
		t.Fatal(err)
	}
	originalResults, err := runtime.RepairResultPacketsContext(ctx, before.State)
	if err != nil || len(originalPlans) != 2 || len(originalResults) != 1 {
		t.Fatalf("original issued history: plans=%d results=%d err=%v", len(originalPlans), len(originalResults), err)
	}
	originalReceipt, found, err := runtime.VerifiedRepairResultCandidateReceipt(fixture.root, before.State, accepted.packet.ID, accepted.owner.agentID)
	if err != nil || !found {
		t.Fatalf("original candidate receipt: %t %v", found, err)
	}
	originalAnswer := mustRead(t, filepath.Join(accepted.workerRoot, filepath.FromSlash(accepted.answerPath)))
	originalJournal := mustRead(t, fixture.journalPath)
	rows := before.State["entities"].(map[string]any)["work_packets"].([]any)
	originalBodies := map[string][]byte{}
	for i, raw := range rows {
		row := raw.(map[string]any)
		if row["template_id"] != "s9-plan-report-v1" && row["template_id"] != "s9-repair-result-v1" {
			continue
		}
		ref, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil {
			t.Fatal(err)
		}
		body, err := packetbody.Read(ctx, row)
		if err != nil {
			t.Fatal(err)
		}
		if external {
			originalBodies[ref.Path] = mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(ref.Path)))
		}
		rows[i] = body
	}
	if len(originalBodies) != 3 {
		t.Fatalf("original Store did not publish both S9 packet kinds: bodies=%d", len(originalBodies))
	}
	// Only the representation changes in this legacy installation fixture; no
	// event, ownership, question, receipt or acceptance coordinate is invented.
	legacy, err := json.MarshalIndent(before.State, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.statePath, append(legacy, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	legacyResults, err := runtime.RepairResultPackets(before.State)
	if err != nil || !reflect.DeepEqual(legacyResults, originalResults) {
		t.Fatalf("legacy inline compatibility: %v", err)
	}
	for path := range originalBodies {
		if err := os.Remove(filepath.Join(fixture.root, filepath.FromSlash(path))); err != nil {
			t.Fatal(err)
		}
	}
	maintained, err := writer.ExternalizePacketBodies()
	if err != nil {
		t.Fatalf("explicit original-body maintenance: %v", err)
	}
	if maintained.Revision != before.Revision+1 {
		t.Fatalf("maintenance revision=%d want %d", maintained.Revision, before.Revision+1)
	}
	for _, raw := range maintained.State["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["template_id"] != "s9-plan-report-v1" && row["template_id"] != "s9-repair-result-v1" {
			continue
		}
		ref, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil || !external || row["inputs"] != nil || row["draft"] != nil {
			t.Fatalf("maintenance left a non-index S9 row: %v", err)
		}
		data := mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(ref.Path)))
		if original, ok := originalBodies[ref.Path]; ok && !bytes.Equal(data, original) {
			t.Fatal("maintenance changed the original packet body bytes")
		}
		originalBodies[ref.Path] = data
	}
	if !bytes.Equal(originalAnswer, mustRead(t, filepath.Join(accepted.workerRoot, filepath.FromSlash(accepted.answerPath)))) {
		t.Fatal("maintenance overwrote the private accepted answer")
	}
	afterJournal := mustRead(t, fixture.journalPath)
	if !bytes.HasPrefix(afterJournal, originalJournal) || bytes.Count(afterJournal, []byte{'\n'}) != bytes.Count(originalJournal, []byte{'\n'})+1 {
		t.Fatal("maintenance rewrote history or created multiple business events")
	}
	replay, err := writer.ExternalizePacketBodies()
	if err != nil || replay.Revision != maintained.Revision || !bytes.Equal(afterJournal, mustRead(t, fixture.journalPath)) {
		t.Fatalf("maintenance replay was not read-only: %v", err)
	}
	checkpoint, err := runtime.LatestRuntimeCheckpoint(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	for path := range originalBodies {
		if err := os.Remove(filepath.Join(fixture.root, filepath.FromSlash(path))); err != nil {
			t.Fatal(err)
		}
	}
	pair, err := runtime.ReadRuntimeCheckpoint(fixture.root, checkpoint, semantic.RuntimeCandidateValidator{})
	if err != nil || pair.Manifest.Revision != maintained.Revision {
		t.Fatalf("independent checkpoint original-body read: revision=%d err=%v", pair.Manifest.Revision, err)
	}
	if err := runtime.RestoreCheckpointPacketBodies(t.Context(), fixture.root, pair); err != nil {
		t.Fatalf("restore original S9 body copies: %v", err)
	}
	restoredPlans, err := runtime.RepairPlanReportPacketsContext(ctx, pair.State)
	if err != nil || !reflect.DeepEqual(restoredPlans, originalPlans) {
		t.Fatalf("restored original Plan questions changed: %v", err)
	}
	restoredResults, err := runtime.RepairResultPacketsContext(ctx, pair.State)
	if err != nil || !reflect.DeepEqual(restoredResults, originalResults) {
		t.Fatalf("restored original Result questions changed: %v", err)
	}
	for path, original := range originalBodies {
		if !bytes.Equal(original, mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(path)))) {
			t.Fatal("checkpoint restoration changed original body bytes")
		}
	}
	// Receipt-only history survives independently of the private authoring file.
	if err := os.Remove(filepath.Join(accepted.workerRoot, filepath.FromSlash(accepted.answerPath))); err != nil {
		t.Fatal(err)
	}
	restoredReceipt, found, err := runtime.VerifiedRepairResultCandidateReceipt(fixture.root, pair.State, accepted.packet.ID, accepted.owner.agentID)
	if err != nil || !found || !reflect.DeepEqual(restoredReceipt, originalReceipt) {
		t.Fatalf("restored original candidate receipt changed: found=%t err=%v", found, err)
	}
	if !bytes.Equal(afterJournal, mustRead(t, fixture.journalPath)) {
		t.Fatal("body restoration/receipt lookup created new Runtime events")
	}
}
