package review

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
)

func TestProductDriftPreservesInterruptedPacketPublicationTemporary(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, output)
	}
	staged := packetbody.Directory + ".packet-" + strings.Repeat("a", 32) + ".tmp"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, staged)), 0700); err != nil {
		t.Fatal(err)
	}
	partial := []byte(`{"binding":`)
	if err := os.WriteFile(filepath.Join(root, staged), partial, 0600); err != nil {
		t.Fatal(err)
	}
	neighbors := []string{packetbody.Directory + "neighbor.go", packetbody.Directory + "neighbor.tmp"}
	for _, path := range neighbors {
		if err := os.WriteFile(filepath.Join(root, path), []byte("ordinary file"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := detectUndeclaredProductDriftWithState(root, &Plan{}, nil, root, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled publication observation: %v", err)
	}
	drift, err := detectUndeclaredProductDriftWithState(root, &Plan{}, nil, root)
	if err != nil || !reflect.DeepEqual(drift, neighbors) {
		t.Fatalf("Git classified exact publication staging as product or exempted neighbors: %v %v", drift, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, staged)); err != nil || !reflect.DeepEqual(data, partial) {
		t.Fatalf("canceled/replayed drift scan changed staging: %v", err)
	}
	// Replacing the exact temporary with a symlink must fail its format
	// classification rather than exempting the linked file by name.
	if err := os.Remove(filepath.Join(root, staged)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, neighbors[0]), filepath.Join(root, staged)); err != nil {
		t.Fatal(err)
	}
	if _, err := detectUndeclaredProductDriftWithState(root, &Plan{}, nil, root); !errors.Is(err, errResultPacketDriftProof) {
		t.Fatalf("Git staging symlink gained exemption: %v", err)
	}
}
