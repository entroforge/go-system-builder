package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
)

func TestPacketBodyPublicationStagingClassifiesOnlyExactRegularTemporary(t *testing.T) {
	root := t.TempDir()
	staged := packetbody.Directory + ".packet-" + strings.Repeat("a", 32) + ".tmp"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, staged)), 0700); err != nil {
		t.Fatal(err)
	}
	partial := []byte(`{"packet_id":"publication was interrupted`)
	if err := os.WriteFile(filepath.Join(root, staged), partial, 0600); err != nil {
		t.Fatal(err)
	}
	unknown := []string{
		packetbody.Directory + "neighbor.tmp",
		packetbody.Directory + "neighbor.go",
		packetbody.Directory + ".packet-" + strings.Repeat("a", 31) + ".tmp",
		packetbody.Directory + ".packet-" + strings.Repeat("A", 32) + ".tmp",
		staged + ".go",
		packetbody.Directory + "nested/.packet-" + strings.Repeat("b", 32) + ".tmp",
		"other/.packet-" + strings.Repeat("b", 32) + ".tmp",
	}
	paths := append([]string{staged}, unknown...)
	got, err := ClassifyPacketBodyStoragePathsContext(context.Background(), root, paths)
	if err != nil || !reflect.DeepEqual(got, []string{staged}) {
		t.Fatalf("publication staging classification: %v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ClassifyPacketBodyStoragePathsContext(ctx, root, []string{staged}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled staging observation: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, staged)); err != nil || !reflect.DeepEqual(data, partial) {
		t.Fatalf("observation removed/replaced staging evidence: %v", err)
	}
	// Complete publication may remove the temp between Git status and this
	// read-only classification. That race must not stale the product baseline.
	if err := os.Remove(filepath.Join(root, staged)); err != nil {
		t.Fatal(err)
	}
	if got, err := ClassifyPacketBodyStoragePathsContext(context.Background(), root, []string{staged}); err != nil || !reflect.DeepEqual(got, []string{staged}) {
		t.Fatalf("vanished publication temporary: %v %v", got, err)
	}
}

func TestPacketBodyPublicationStagingRejectsSymlinksAndDirectories(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "parent-symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			staged := packetbody.Directory + ".packet-" + strings.Repeat("a", 32) + ".tmp"
			path := filepath.Join(root, staged)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(root, "target")
				if err := os.WriteFile(target, []byte("ordinary bytes"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			default:
				target := t.TempDir()
				if err := os.Remove(filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := ClassifyPacketBodyStoragePathsContext(context.Background(), root, []string{staged}); err == nil || len(got) != 0 {
				t.Fatalf("%s gained staging exemption: %v %v", kind, got, err)
			}
		})
	}
}
