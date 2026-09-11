package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncDirPlatformContract(t *testing.T) {
	dir := t.TempDir()
	if err := syncDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := syncDir(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing directory accepted")
	}
	path := filepath.Join(dir, "record")
	if err := writeDurableFile(path, []byte("checkpoint")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "checkpoint" {
		t.Fatalf("data=%q error=%v", data, err)
	}
}
