package packetbody

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBodyPublicationRetainsOriginalAndRequiresExactIndex(t *testing.T) {
	root := t.TempDir()
	body := map[string]any{"packet_id": "original", "template_id": "historical", "binding": map[string]any{"owner": "worker"}, "inputs": map[string]any{"original": "frozen"}, "draft": map[string]any{"questions": "historical"}}
	ctx := WithRoot(context.Background(), root)
	index, err := Publish(ctx, root, body)
	if err != nil {
		t.Fatal(err)
	}
	if index["inputs"] != nil || index["draft"] != nil {
		t.Fatal("full body remained in index")
	}
	got, err := Read(ctx, index)
	if err != nil || !reflect.DeepEqual(got, body) {
		t.Fatalf("original body: %v %v", got, err)
	}
	if _, err := Publish(ctx, root, body); err != nil {
		t.Fatal(err)
	}
	index["binding"].(map[string]any)["owner"] = "other"
	if _, err := Read(ctx, index); err == nil {
		t.Fatal("changed owner accepted against original body")
	}
}

func TestBodyMissingCorruptUnknownFormatAndCancellationFailClosed(t *testing.T) {
	root := t.TempDir()
	ctx := WithRoot(context.Background(), root)
	index, err := Publish(ctx, root, map[string]any{"packet_id": "original", "inputs": strings.Repeat("x", 4096), "draft": "original"})
	if err != nil {
		t.Fatal(err)
	}
	ref, _, err := ReferenceFromIndex(index)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Read(context.Background(), index); err == nil {
		t.Fatal("unrooted reader accepted reference")
	}
	if err := os.WriteFile(filepath.Join(root, ref.Path), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(ctx, index); err == nil {
		t.Fatal("corrupt body accepted")
	}
	if err := os.Remove(filepath.Join(root, ref.Path)); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(ctx, index); err == nil {
		t.Fatal("missing body accepted")
	}
	index["storage_format"] = "future-format"
	if _, _, err := ReferenceFromIndex(index); err == nil {
		t.Fatal("unknown format accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Publish(canceled, root, map[string]any{"packet_id": "new"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled publish: %v", err)
	}
}
