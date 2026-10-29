package workpacket

import (
	"bytes"
	"os"
	"testing"
)

func TestPinnedDirectoryPublicationPreservesOriginalBytes(t *testing.T) {
	root := t.TempDir()
	const path = ".claude/runtime-checkpoints/source/state.json"
	original := []byte(`{"runtime_id":"original"}`)
	if got, err := PublishBounded(root, path, original, 1024); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("initial durable publication: bytes=%q err=%v", got, err)
	}
	if got, err := PublishBounded(root, path, []byte(`{"runtime_id":"other"}`), 1024); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("immutable publication replaced original bytes: bytes=%q err=%v", got, err)
	}
	pinned, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := pinned.Close(); err != nil {
		t.Fatal(err)
	}
	if err := SyncDirectory(pinned); err == nil {
		t.Fatal("directory sync accepted a closed pinned root")
	}
}
