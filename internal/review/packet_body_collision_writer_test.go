package review

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

func TestOriginalReviewerWriterPreservesUnknownPacketStorage(t *testing.T) {
	root, statePath, journalPath, before, _ := packetModeFixture(t)
	unknown := filepath.Join(root, ".claude", "packet-bodies", "p")
	if err := os.MkdirAll(filepath.Dir(unknown), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unknown, []byte("private unregistered file"), 0600); err != nil {
		t.Fatal(err)
	}
	packets, err := IssueResultPackets(root, statePath, journalPath, before.Revision, "agent-qa-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue original packet: %d %v", len(packets), err)
	}
	if data, err := os.ReadFile(unknown); err != nil || string(data) != "private unregistered file" {
		t.Fatalf("unknown file changed: %q %v", data, err)
	}
	state, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	rows := state.State["entities"].(map[string]any)["work_packets"].([]any)
	var ref packetbody.Reference
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["packet_id"] == packets[0].ID {
			ref, _, err = packetbody.ReferenceFromIndex(row)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if ref.Path == "" {
		t.Fatalf("missing original registered ref: %#v", ref)
	}
	controls, err := loopruntime.ClassifyPacketBodyStoragePathsContext(context.Background(), root, []string{ref.Path, ".claude/packet-bodies/p"})
	if err != nil || len(controls) != 1 || controls[0] != ref.Path {
		t.Fatalf("registered classification: %v %v", controls, err)
	}
	again, err := IssueResultPackets(root, statePath, journalPath, state.Revision, "agent-qa-1")
	if err != nil || len(again) != 1 || again[0].ID != packets[0].ID {
		t.Fatalf("original responsibility replay: %v %v", again, err)
	}
	after, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil || after.Revision != state.Revision {
		t.Fatalf("replay changed writer state: %#v %v", after, err)
	}
}
