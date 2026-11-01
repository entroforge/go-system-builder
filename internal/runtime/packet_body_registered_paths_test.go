package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
)

func writePacketClassificationJSON(t *testing.T, root, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPacketBodyClassificationUsesOnlyRegisteredFacts(t *testing.T) {
	root := t.TempDir()
	registered := packetbody.Directory + "alternate-1/body.json"
	orphan := packetbody.Directory + "p/orphan.json"
	temporary := packetbody.Directory + ".packet-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.tmp"
	row := map[string]any{"packet_id": "p1", "template_id": reviewResultPacketTemplateID, "storage_format": packetbody.StorageFormat, "body_ref": map[string]any{"format": packetbody.BodyFormat, "path": registered}}
	state := map[string]any{"entities": map[string]any{"work_packets": []any{row}}}
	writePacketClassificationJSON(t, root, ".claude/loop-state.json", state)
	for _, path := range []string{registered, orphan, temporary} {
		abs := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("unrelated or damaged"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ClassifyPacketBodyStoragePathsContext(context.Background(), root, []string{registered, orphan, temporary, "internal/product.go", "../escape"})
	if err != nil || !reflect.DeepEqual(got, []string{registered}) {
		t.Fatalf("registered paths: %v %v", got, err)
	}
	for _, path := range []string{orphan, temporary} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatalf("unknown path was removed: %s: %v", path, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ClassifyPacketBodyStoragePathsContext(ctx, root, []string{registered}); err != context.Canceled {
		t.Fatalf("canceled classification: %v", err)
	}
}

func TestPacketBodyClassificationIncludesOnlyCoherentPendingAndIndexedCheckpoint(t *testing.T) {
	root := t.TempDir()
	refPath := packetbody.Directory + "alternate-1/body.json"
	row := map[string]any{"packet_id": "p1", "template_id": reviewResultPacketTemplateID, "storage_format": packetbody.StorageFormat, "body_ref": map[string]any{"format": packetbody.BodyFormat, "path": refPath}}
	state := map[string]any{"runtime_id": "loop-test", "revision": 1, "journal": map[string]any{"last_sequence": 1, "last_event_id": "event-1"}, "entities": map[string]any{"work_packets": []any{row}}}
	event := map[string]any{"event_id": "event-1", "runtime_id": "loop-test", "before_revision": 0, "after_revision": 1, "sequence": 1, "request_id": "request", "idempotency_key": "key"}
	marker := map[string]any{"schema_version": "1.0.0", "state": state, "journal_event": event, "request_id": "request", "idempotency_key": "key", "retain_last_transition": true}
	writePacketClassificationJSON(t, root, ".claude/loop-state.json.commit-pending.json", marker)
	dir := ".claude/runtime-checkpoints/cp/runtime/r1-s1"
	manifest := dir + "/manifest.json"
	checkpoint := checkpointBodyPath(dir, packetbody.Reference{Path: refPath})
	writePacketClassificationJSON(t, root, checkpointIndexPath, checkpointIndex{SchemaVersion: "1.0.0", Manifests: []string{manifest}})
	writePacketClassificationJSON(t, root, manifest, RuntimeCheckpoint{SchemaVersion: "1.0.0", StatePath: dir + "/state.json"})
	writePacketClassificationJSON(t, root, dir+"/state.json", state)
	got, err := ClassifyPacketBodyStoragePathsContext(context.Background(), root, []string{refPath, checkpoint})
	if err != nil || !reflect.DeepEqual(got, []string{refPath, checkpoint}) {
		t.Fatalf("pending/checkpoint refs: %v %v", got, err)
	}
	event["request_id"] = "different"
	writePacketClassificationJSON(t, root, ".claude/loop-state.json.commit-pending.json", marker)
	got, err = ClassifyPacketBodyStoragePathsContext(context.Background(), root, []string{refPath, checkpoint})
	if err != nil || !reflect.DeepEqual(got, []string{checkpoint}) {
		t.Fatalf("incoherent pending was trusted: %v %v", got, err)
	}
}
