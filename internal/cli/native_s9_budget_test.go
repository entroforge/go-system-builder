package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestNativeS9CanceledPreparationPreservesOriginalSource(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		root := t.TempDir()
		packet := workpacket.RepairPlanReportPacket{ID: "packet-budget", ViewPath: ".claude/submissions/repair/view.md"}
		brief, source, _ := nativeS9Paths(packet)
		const original = "{\"base_commit\":\"original-source\"}"
		if pinned {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, source)), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, source), []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := prepareNativeS9Brief(ctx, root, packet); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled request started new preparation: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, brief)); !os.IsNotExist(err) {
			t.Fatalf("canceled request published a brief: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(root, source))
		if pinned {
			if err != nil || string(data) != original {
				t.Fatalf("canceled preparation changed the original source: %v", err)
			}
		} else if !os.IsNotExist(err) {
			t.Fatalf("canceled request published a new source pin: %v", err)
		}
	}
}
