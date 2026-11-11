package packetbody

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestPublishOriginalSelectsSafeRootWhenStorageRootIsOccupied(t *testing.T) {
	for _, kind := range []string{"normal", "file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
				t.Fatal(err)
			}
			originalRoot := filepath.Join(root, ".claude", "packet-bodies")
			outside := t.TempDir()
			if kind == "file" {
				if err := os.WriteFile(originalRoot, []byte("unknown"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "symlink" {
				if err := os.Symlink(outside, originalRoot); err != nil {
					t.Fatal(err)
				}
			}
			data := []byte(`{"packet_id":"packet-1","professional":"original"}`)
			ref, err := PublishOriginal(context.Background(), root, "packet-1", data)
			if err != nil {
				t.Fatal(err)
			}
			if kind != "normal" && !strings.HasPrefix(ref.Path, ".claude/packet-bodies-alt-") {
				t.Fatalf("unsafe storage root selected: %s", ref.Path)
			}
			row := map[string]any{"storage_format": StorageFormat, "body_ref": map[string]any{"format": ref.Format, "path": ref.Path}}
			registered, external, err := ReferenceFromIndex(row)
			if err != nil || !external || registered != ref {
				t.Fatalf("registered reference: %+v %v", registered, err)
			}
			if got, err := workpacket.ReadFile(root, ref.Path, Limit); err != nil || string(got) != string(data) {
				t.Fatalf("original body: %q %v", got, err)
			}
			if kind == "file" {
				if got, err := os.ReadFile(originalRoot); err != nil || string(got) != "unknown" {
					t.Fatal("unknown root changed")
				}
			}
			if kind == "symlink" {
				entries, err := os.ReadDir(outside)
				if err != nil || len(entries) != 0 {
					t.Fatal("symlink target was touched")
				}
			}
		})
	}
}
