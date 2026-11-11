package repair

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/integration"
)

func TestIntegratedChangesObserveRecordedPathsWithoutOID(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "modified.txt"), []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	cp := integration.Checkpoint{ChangedPaths: []string{"modified.txt", "deleted.txt"}}
	got, err := IntegratedRepairChanges(context.Background(), root, cp)
	if err != nil || len(got) != 2 || got[0].Path != "modified.txt" || got[0].Status != "modified" || got[1].Path != "deleted.txt" || got[1].Status != "deleted" {
		t.Fatalf("actual paths: %+v %v", got, err)
	}
	// The helper describes the current observation and never asserts historical
	// bytes when someone changes the checkout after the merge.
	if err := os.Remove(filepath.Join(root, "modified.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deleted.txt"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = IntegratedRepairChanges(context.Background(), root, cp)
	if err != nil || got[0].Status != "deleted" || got[1].Status != "modified" {
		t.Fatalf("current observation: %+v %v", got, err)
	}
	if _, err := IntegratedRepairChanges(context.Background(), root, integration.Checkpoint{ChangedPaths: []string{"../escape"}}); err == nil {
		t.Fatal("unsafe changed path accepted")
	}
	if _, err := IntegratedRepairChanges(context.Background(), filepath.Join(root, "deleted.txt"), integration.Checkpoint{ChangedPaths: []string{"child"}}); err == nil {
		t.Fatal("actual observation failure hidden")
	}
}
