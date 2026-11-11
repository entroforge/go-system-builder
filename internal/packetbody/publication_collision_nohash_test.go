package packetbody

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPublishPreservesUnknownCollisionWithoutAuthenticatingBytes(t *testing.T) {
	for _, unknown := range []string{`{"packet_id":"same","template_id":"x"}`, `{"unrelated":true}`, `broken`} {
		t.Run(unknown, func(t *testing.T) {
			root := t.TempDir()
			body := map[string]any{"packet_id": "same", "template_id": "x"}
			original := Directory + "p/" + base64.RawURLEncoding.EncodeToString([]byte("same")) + ".json"
			path := filepath.Join(root, original)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(unknown), 0600); err != nil {
				t.Fatal(err)
			}
			index, err := Publish(context.Background(), root, body)
			if err != nil {
				t.Fatal(err)
			}
			ref, ok, err := ReferenceFromIndex(index)
			if err != nil || !ok || ref.Path == original {
				t.Fatalf("new original ref: %#v %v %v", ref, ok, err)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != unknown {
				t.Fatalf("unknown was changed: %q %v", got, err)
			}
			got, err := Read(WithRoot(context.Background(), root), index)
			if err != nil || !reflect.DeepEqual(got, body) {
				t.Fatalf("issued body: %#v %v", got, err)
			}
		})
	}
}

func TestPublishHandlesOccupiedDefaultParentAndAvoidsUnsafeRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Directory), 0700); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, Directory, "p")
	if err := os.WriteFile(parent, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := Publish(context.Background(), root, map[string]any{"packet_id": "same"})
	if err != nil {
		t.Fatal(err)
	}
	ref, _, _ := ReferenceFromIndex(index)
	if ref.Path == "" {
		t.Fatalf("ref: %#v", ref)
	}
	if got, err := os.ReadFile(parent); err != nil || string(got) != "private" {
		t.Fatalf("occupied parent changed: %q %v", got, err)
	}
	unsafe := t.TempDir()
	if err := os.MkdirAll(filepath.Join(unsafe, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := os.Symlink(out, filepath.Join(unsafe, ".claude", "packet-bodies")); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(context.Background(), unsafe, map[string]any{"packet_id": "same"}); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(out); err != nil || len(entries) != 0 {
		t.Fatal("followed symlink storage root")
	}
}
