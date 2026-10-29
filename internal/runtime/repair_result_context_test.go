package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Every original Final query boundary must honor cancellation before starting
// new source reads; deliberately absent sources must never mask that budget.
func TestRepairResultFinalProofContextStopsBeforeNewSources(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	packet := workpacket.RepairResultPacket{}
	state := map[string]any{}
	if _, _, err := VerifiedRepairResultReceiptContext(ctx, root, state, "original-packet", "owner"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Final query lost caller cancellation: %v", err)
	}
	if _, err := repairResultJournal(root, state, packet, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("journal query lost caller cancellation: %v", err)
	}
	if _, _, err := findRepairResultReceipt(root, state, packet, controlproof.JournalInspection{}, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("receipt read lost caller cancellation: %v", err)
	}
	if _, _, err := findRepairResultCandidateReceipt(root, state, packet, controlproof.JournalInspection{}, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("candidate proof lost caller cancellation: %v", err)
	}
	if err := verifyRepairResultMainProof(root, packet, RepairResultReceipt{}, nil, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("checkpoint proof lost caller cancellation: %v", err)
	}
}
