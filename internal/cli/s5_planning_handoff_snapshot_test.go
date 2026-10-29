package cli

import (
	"context"
	"github.com/entroforge/go-system-builder/internal/submission"
	"strings"
	"testing"
)

func TestS5HandoffUnreadableSnapshotPreservesUnprovenReceiptFacts(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := submission.Receipt{PacketID: "packet-s5-original", SubmissionKey: "original-key", Accepted: true, WorkComplete: true, CurrentStage: "S5", ConsumerOwner: "original-review-owner", ConsumerPending: false, NextAction: "original professional receipt"}
	prepareS5PlanningHandoff(ctx, root, &r)
	if !r.Accepted || !r.WorkComplete || r.SubmissionKey != "original-key" || r.ConsumerOwner != "original-review-owner" || r.ConsumerPending {
		t.Fatalf("snapshot failure rewrote receipt authority: %+v", r)
	}
	for _, want := range []string{"original professional receipt", "最终 Runtime 无法读取", "packet-s5-original"} {
		if !strings.Contains(r.NextAction, want) {
			t.Fatalf("missing %q: %s", want, r.NextAction)
		}
	}
	if strings.Contains(r.NextAction, "S4") || strings.Contains(r.NextAction, "stage-answer") {
		t.Fatalf("unproven packet was labeled as S4: %s", r.NextAction)
	}
}
