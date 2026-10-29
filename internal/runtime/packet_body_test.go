package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestNewPacketStorageUsesOriginalBodyAndLeavesLegacyRowsInline(t *testing.T) {
	root := t.TempDir()
	p := checkpointTestReviewPacket(t, root)
	state, _ := checkpointTestReviewState(t, p, nil)
	original, _ := json.Marshal(state)
	store := &Store{root: root}
	changed, err := store.externalizeNewPacketBodies(map[string]any{}, state)
	if err != nil || !changed {
		t.Fatalf("new packet externalization: %v %v", changed, err)
	}
	row := state["entities"].(map[string]any)["work_packets"].([]any)[0].(map[string]any)
	if row["draft"] != nil || row["inputs"] != nil {
		t.Fatal("body retained in main state")
	}
	data, _ := json.Marshal(row)
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-result-packet-index.schema.json", data); err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-result-work-packet.schema.json", data); err == nil {
		t.Fatal("legacy schema silently accepted new representation")
	}
	ctx := WithReviewResultReadCache(WithPacketBodyRoot(context.Background(), root))
	got, found, err := FindReviewResultPacketContext(ctx, state, p.ID)
	if err != nil || !found || !reflect.DeepEqual(got, p) {
		t.Fatalf("stored original: %v %v", found, err)
	}
	var legacy map[string]any
	if err := json.Unmarshal(original, &legacy); err != nil {
		t.Fatal(err)
	}
	changed, err = store.externalizeNewPacketBodies(legacy, legacy)
	if err != nil || changed {
		t.Fatalf("legacy row implicitly migrated: %v %v", changed, err)
	}
	legacyAfter, _ := json.Marshal(legacy)
	if string(original) != string(legacyAfter) {
		t.Fatal("legacy physical payload changed")
	}
}

func TestPacketBodyCurrentSelectionDoesNotReadHistoricalBody(t *testing.T) {
	root := t.TempDir()
	p := checkpointTestReviewPacket(t, root)
	state, _ := checkpointTestReviewState(t, p, nil)
	entities := state["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	index, err := packetbody.Publish(context.Background(), root, rows[0].(map[string]any))
	if err != nil {
		t.Fatal(err)
	}
	binding := p.Binding
	binding.ReviewPlanRevision++
	historical, err := workpacket.BuildReviewResult(binding, p.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(historical)
	var history map[string]any
	if err := json.Unmarshal(raw, &history); err != nil {
		t.Fatal(err)
	}
	historyIndex, err := packetbody.Publish(context.Background(), root, history)
	if err != nil {
		t.Fatal(err)
	}
	ref, _, _ := packetbody.ReferenceFromIndex(historyIndex)
	if err := os.Remove(filepath.Join(root, ref.Path)); err != nil {
		t.Fatal(err)
	}
	entities["work_packets"] = []any{historyIndex, index}
	ctx, timings := metrics.WithCallTimings(WithReviewResultReadCache(WithPacketBodyRoot(context.Background(), root)))
	current, err := CurrentReviewResultPacketsContext(ctx, state, p.Binding.OwnerAgentID)
	if err != nil || len(current) != 1 || !reflect.DeepEqual(current[0], p) {
		t.Fatalf("selected current: %v %v", current, err)
	}
	if timings.Snapshot()["runtime/review_packet_selected_build"].Count != 1 {
		t.Fatal(timings.Snapshot())
	}
	if _, err := ReviewResultPacketsContext(ctx, state); err == nil {
		t.Fatal("full diagnostic ignored missing historical body")
	}
	historyIndex["storage_format"] = "future-format"
	if _, err := CurrentReviewResultPacketsContext(ctx, state, p.Binding.OwnerAgentID); err == nil {
		t.Fatal("unselected unknown index format ignored")
	}
}

func TestCheckpointRetainsIndependentPacketBodyAndRestoresExactBytes(t *testing.T) {
	root := t.TempDir()
	p := checkpointTestReviewPacket(t, root)
	state, _ := checkpointTestReviewState(t, p, nil)
	store := &Store{root: root}
	if _, err := store.externalizeNewPacketBodies(map[string]any{}, state); err != nil {
		t.Fatal(err)
	}
	row := state["entities"].(map[string]any)["work_packets"].([]any)[0].(map[string]any)
	ref, _, _ := packetbody.ReferenceFromIndex(row)
	dir := ".claude/runtime-checkpoints/fixture"
	if err := store.publishCheckpointPacketBodies(state, dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ref.Path)); err != nil {
		t.Fatal(err)
	}
	bodies, err := readCheckpointPacketBodies(root, dir, state)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withCheckpointPacketBodies(WithPacketBodyRoot(context.Background(), root), bodies)
	got, found, err := FindReviewResultPacketContext(ctx, state, p.ID)
	if err != nil || !found || !reflect.DeepEqual(got, p) {
		t.Fatalf("checkpoint original: %v %v", found, err)
	}
	if err := RestoreCheckpointPacketBodies(context.Background(), root, CheckpointPair{State: state, PacketBodies: bodies}); err != nil {
		t.Fatal(err)
	}
	if err := verifyReferencedPacketBodies(context.Background(), root, state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ref.Path), []byte("conflict"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RestoreCheckpointPacketBodies(context.Background(), root, CheckpointPair{State: state, PacketBodies: bodies}); err == nil {
		t.Fatal("restore overwrote conflicting original body")
	}
}

func TestPacketStorageClassifiesCanceledOrphanWithoutGrantingNeighbors(t *testing.T) {
	root := t.TempDir()
	p := checkpointTestReviewPacket(t, root)
	state, _ := checkpointTestReviewState(t, p, nil)
	row := state["entities"].(map[string]any)["work_packets"].([]any)[0].(map[string]any)
	ctx, cancel := context.WithCancel(context.Background())
	index, err := packetbody.Publish(ctx, root, row)
	if err != nil {
		t.Fatal(err)
	}
	cancel() // Publication survived, but no Runtime reference was committed.
	ref, _, _ := packetbody.ReferenceFromIndex(index)
	neighbor := packetbody.Directory + "unknown.json"
	if err := os.WriteFile(filepath.Join(root, neighbor), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	controls, err := ClassifyPacketBodyStoragePathsContext(context.Background(), root, []string{ref.Path, neighbor})
	if err != nil || !reflect.DeepEqual(controls, []string{ref.Path}) {
		t.Fatalf("orphan storage classification: %v %v", controls, err)
	}
	if _, err := ClassifyPacketBodyStoragePathsContext(ctx, root, []string{ref.Path}); err != context.Canceled {
		t.Fatalf("canceled classification: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ref.Path), []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ClassifyPacketBodyStoragePathsContext(context.Background(), root, []string{ref.Path}); err == nil {
		t.Fatal("corrupt storage was exempted")
	}
}
