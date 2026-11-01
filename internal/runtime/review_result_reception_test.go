package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestReviewResultReceptionPreservesOriginalBodyAndCheckpoint(t *testing.T) {
	root := t.TempDir()
	packet := checkpointTestReviewPacket(t, root)
	state, _ := checkpointTestReviewState(t, packet, nil)
	rows := state["entities"].(map[string]any)["work_packets"].([]any)
	inline := rows[0].(map[string]any)
	reception := map[string]any{"execution_root": root, "work_complete": false}
	inline["review_result_reception"] = reception
	data, _ := json.Marshal(inline)
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-result-work-packet.schema.json", data); err != nil {
		t.Fatal(err)
	}
	ctx := WithPacketBodyRoot(context.Background(), root)
	got, found, err := FindReviewResultPacketContext(ctx, state, packet.ID)
	if err != nil || !found || !reflect.DeepEqual(got, packet) {
		t.Fatalf("inline original changed: %v %v", found, err)
	}
	store := &Store{root: root}
	changed, err := store.externalizeNewPacketBodies(map[string]any{}, state)
	if err != nil || !changed {
		t.Fatalf("externalize original: %v %v", changed, err)
	}
	index := rows[0].(map[string]any)
	if !reflect.DeepEqual(index["review_result_reception"], reception) {
		t.Fatal("externalization lost reception")
	}
	ref, _, err := packetbody.ReferenceFromIndex(index)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(root, ref.Path))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.publishCheckpointPacketBodies(state, ".claude/checkpoints/late-test"); err != nil {
		t.Fatal(err)
	}
	bodies, err := readCheckpointPacketBodies(root, ".claude/checkpoints/late-test", state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ref.Path)); err != nil {
		t.Fatal(err)
	}
	checkpointCtx := withCheckpointPacketBodies(ctx, bodies)
	rebuilt, found, err := FindReviewResultPacketContext(checkpointCtx, state, packet.ID)
	if err != nil || !found || !reflect.DeepEqual(rebuilt, packet) {
		t.Fatalf("checkpoint original changed: %v %v", found, err)
	}
	if !reflect.DeepEqual(ReviewResultReception(state, packet.ID), reception) {
		t.Fatal("checkpoint reconstruction lost reception")
	}
	if !reflect.DeepEqual(bodies[ref.Path], original) {
		t.Fatal("checkpoint body changed immutable bytes")
	}
	// Old Runtime schema compatibility: the optional field is absent historically.
	delete(index, "review_result_reception")
	data, _ = json.Marshal(index)
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-result-packet-index.schema.json", data); err != nil {
		t.Fatal(err)
	}
}
