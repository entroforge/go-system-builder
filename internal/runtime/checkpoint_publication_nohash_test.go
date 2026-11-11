package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpointDirectoryClaimDoesNotReuseUnknownAndRejectsUnsafeStorage(t *testing.T) {
	root := t.TempDir()
	base := ".claude/runtime-checkpoints/cp/cnVudGltZQ/r7-s9"
	original := filepath.Join(root, filepath.FromSlash(base))
	if err := os.MkdirAll(original, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, "private.txt"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	dir, err := claimCheckpointDirectory(context.Background(), root, base)
	if err != nil || !strings.Contains(dir, "/alternate-1/") {
		t.Fatalf("new checkpoint directory %q: %v", dir, err)
	}
	if got, err := os.ReadFile(filepath.Join(original, "private.txt")); err != nil || string(got) != "private" {
		t.Fatalf("unknown directory changed: %q %v", got, err)
	}
	if !validCheckpointManifestPath(dir+"/manifest.json") || !checkpointBelongsToEvent(dir+"/manifest.json", base) {
		t.Fatal("new checkpoint identity is not usable")
	}
	for _, invalid := range []string{dir + "/../../other/manifest.json", ".claude/runtime-checkpoints/alternate-x/cp/cnVudGltZQ/r7-s9/manifest.json", ".claude/runtime-checkpoints/alternate-2/cp/cnVudGltZQ/r8-s9/manifest.json"} {
		if checkpointBelongsToEvent(invalid, base) {
			t.Fatalf("different event or unsafe path accepted: %s", invalid)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := claimCheckpointDirectory(ctx, root, base); err != context.Canceled {
		t.Fatalf("canceled claim: %v", err)
	}
	unsafe := t.TempDir()
	if err := os.MkdirAll(filepath.Join(unsafe, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(unsafe, ".claude", "runtime-checkpoints")); err != nil {
		t.Fatal(err)
	}
	if _, err := claimCheckpointDirectory(context.Background(), unsafe, base); err == nil {
		t.Fatal("checkpoint storage followed symlink")
	}
}
