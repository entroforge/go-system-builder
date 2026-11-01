package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
	reads := 0
	ctx := packetbody.WithResolver(WithReviewResultReadCache(WithPacketBodyRoot(context.Background(), root)), func(_ context.Context, ref packetbody.Reference) ([]byte, error) {
		reads++
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
	})
	current, err := CurrentReviewResultPacketsContext(ctx, state, p.Binding.OwnerAgentID)
	if err != nil || len(current) != 1 || !reflect.DeepEqual(current[0], p) {
		t.Fatalf("selected current: %v %v", current, err)
	}
	if reads != 1 {
		t.Fatalf("current selection read %d packet bodies, want the one selected original", reads)
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
	if err := os.WriteFile(filepath.Join(root, ref.Path), []byte("unknown private file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RestoreCheckpointPacketBodies(context.Background(), root, CheckpointPair{State: state, PacketBodies: bodies}); err != nil {
		t.Fatal(err)
	}
	actual, _, err := packetbody.ReferenceFromIndex(row)
	if err != nil || actual.Path == ref.Path {
		t.Fatalf("restore did not select an independent body: %v %v", actual, err)
	}
	if err := verifyReferencedPacketBodies(context.Background(), root, state); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(root, ref.Path)); err != nil || string(got) != "unknown private file" {
		t.Fatalf("unknown body was changed: %q %v", got, err)
	}
}

func TestPacketStorageDoesNotClaimCanceledOrphanOrNeighbors(t *testing.T) {
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
	if err != nil || len(controls) != 0 {
		t.Fatalf("orphan storage classification: %v %v", controls, err)
	}
	if _, err := ClassifyPacketBodyStoragePathsContext(ctx, root, []string{ref.Path}); err != context.Canceled {
		t.Fatalf("canceled classification: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ref.Path), []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	if paths, err := ClassifyPacketBodyStoragePathsContext(context.Background(), root, []string{ref.Path}); err != nil || len(paths) != 0 {
		t.Fatalf("unregistered storage acquired authority: %v %v", paths, err)
	}
}

func TestCheckpointRestoresWriterSelectedAlternativeBodyRoot(t *testing.T) {
	root := t.TempDir()
	p := checkpointTestReviewPacket(t, root)
	state, _ := checkpointTestReviewState(t, p, nil)
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(root, ".claude", "packet-bodies")
	if err := os.WriteFile(occupied, []byte("private occupant"), 0600); err != nil {
		t.Fatal(err)
	}
	store := &Store{root: root}
	if _, err := store.externalizeNewPacketBodies(map[string]any{}, state); err != nil {
		t.Fatal(err)
	}
	row := state["entities"].(map[string]any)["work_packets"].([]any)[0].(map[string]any)
	ref, external, err := packetbody.ReferenceFromIndex(row)
	if err != nil || !external || !strings.HasPrefix(ref.Path, ".claude/packet-bodies-alt-") {
		t.Fatalf("actual body reference: %+v %v", ref, err)
	}
	dir := ".claude/runtime-checkpoints/alternate-fixture"
	if err := store.publishCheckpointPacketBodies(state, dir); err != nil {
		t.Fatal(err)
	}
	bodies, err := readCheckpointPacketBodies(root, dir, state)
	if err != nil || len(bodies[ref.Path]) == 0 {
		t.Fatalf("checkpoint original body: %v", err)
	}
	if err := RestoreCheckpointPacketBodies(context.Background(), root, CheckpointPair{State: state, PacketBodies: bodies}); err != nil {
		t.Fatal(err)
	}
	if err := verifyReferencedPacketBodies(context.Background(), root, state); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(occupied); err != nil || string(got) != "private occupant" {
		t.Fatal("unknown storage root was changed")
	}
}
