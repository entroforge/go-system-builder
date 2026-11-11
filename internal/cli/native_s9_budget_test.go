package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestNativeS9CanceledPreparationDoesNotPublishBrief(t *testing.T) {
	root := t.TempDir()
	packet := workpacket.RepairPlanReportPacket{ID: "packet-budget", ViewPath: ".claude/submissions/repair/view.md"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prepareNativeS9Brief(ctx, root, packet); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request started preparation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, nativeS9BriefPath(packet))); !os.IsNotExist(err) {
		t.Fatalf("canceled request published a brief: %v", err)
	}
}
