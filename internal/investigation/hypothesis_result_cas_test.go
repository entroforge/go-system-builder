package investigation_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestS8ConcurrentCommitPreservesAnswerAndOriginalReceiptRecovery(t *testing.T) {
	f, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	worker := registerS8WorkerExecution(t, f, currentS8Snapshot(t, f), owner)
	packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue original S8 responsibility: packets=%d err=%v", len(packets), err)
	}
	packet := packets[0]
	answer := writeS8ProfessionalAnswer(t, worker, packet, "The Investigator compared the original frozen evidence.")
	answerBytes := mustRead(t, filepath.Join(worker, answer))
	before := currentS8Snapshot(t, f)
	request := investigation.IssuedHypothesisResultRequest{
		ExpectedRevision: -1, PacketID: packet.ID, OwnerAgentID: owner.ownerID, AnswerPath: answer,
		AuthorizeSource: func(root string, state map[string]any) error {
			_, err := runtime.NewWriter(f.statePath, f.journalPath, root, semantic.RuntimeCandidateValidator{}).UpdateCurrent(runtime.Mutation{
				EventID: "evt-s8-unrelated-observation", TransitionID: "MILESTONE-REFRESH", Event: "milestone_refreshed",
				Actor: "Main", IdempotencyKey: "s8-unrelated-observation", RetainLastTransition: true,
			})
			return err
		},
	}
	_, err = investigation.SubmitIssuedHypothesisResult(f.root, f.statePath, f.journalPath, request)
	// G18 §3.1/§4.5: the Runtime write count is not a gate on the professional
	// reception. The concurrent Runtime update moved the counter for an
	// unrelated reason while the original responsibility, the pinned Case and
	// the hypothesis binding stayed unchanged, so the reception completes
	// instead of being refused on a counter.
	if err != nil {
		t.Fatalf("concurrent reception was refused on the Runtime write count: %v", err)
	}
	conflicted := currentS8Snapshot(t, f)
	if conflicted.Revision != before.Revision+2 {
		t.Fatalf("reception committed %d revisions, want exactly the unrelated update and the one reception commit", conflicted.Revision-before.Revision)
	}
	receipt, found, err := runtime.VerifiedHypothesisResultReceipt(f.root, conflicted.State, packet.ID, owner.ownerID)
	if err != nil || !found {
		t.Fatalf("accepted receipt is missing: found=%t err=%v", found, err)
	}
	// G18 §1: the accepted identity is the committed event identity. The
	// Runtime write count in the receipt is a diagnostic of where the
	// acceptance landed, never the identity, so the proof is the committed
	// event and not a counter.
	if receipt.AcceptedEventID == "" || receipt.AcceptedIdempotencyKey == "" {
		t.Fatalf("accepted receipt lost its committed identity: %+v", receipt)
	}
	// G18 §2 received-row clause: the same answer is received exactly once —
	// one result row on the Case and one committed journal event.
	assertS8SingleAcceptedResult(t, f, receipt, packet)
	if !bytes.Equal(answerBytes, mustRead(t, filepath.Join(worker, answer))) {
		t.Fatal("the reception rewrote the professional answer")
	}
	// Main has checked that the original Case/dispatch still holds. Reuse the
	// existing consumer and bytes; no new answer, publication or retry loop.
	request.AuthorizeSource = nil
	accepted, err := investigation.SubmitIssuedHypothesisResult(f.root, f.statePath, f.journalPath, request)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Revision != conflicted.Revision {
		t.Fatalf("the accepted receipt no longer proves its exact commit: revision=%d want=%d", accepted.Revision, conflicted.Revision)
	}
	assertS8SingleAcceptedResult(t, f, receipt, packet)
	if !bytes.Equal(answerBytes, mustRead(t, filepath.Join(worker, answer))) {
		t.Fatal("Main recovery required changing the accepted professional answer")
	}
	replayed, err := investigation.SubmitIssuedHypothesisResult(f.root, f.statePath, f.journalPath, request)
	if err != nil || replayed.Revision != accepted.Revision {
		t.Fatalf("recovered original receipt was not idempotent: revision=%d/%d err=%v", replayed.Revision, accepted.Revision, err)
	}
}
