package investigation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS8LocalCaptureDoesNotArchiveCredentials(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, ".env"), []byte("TOKEN=fixture-only"), 0600); err != nil {
		t.Fatal(err)
	}
	answer := workpacket.HypothesisResultAnswer{EvidenceRefs: []string{"path:.env"}}
	err := bindS8LocalEvidence(root, source, workpacket.HypothesisResultPacket{}, &answer, false)
	if err == nil || !strings.Contains(err.Error(), "excludes") || strings.Contains(err.Error(), "TOKEN=") {
		t.Fatalf("sensitive observation copied or disclosed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("source guard wrote an archive: %v", err)
	}
}
