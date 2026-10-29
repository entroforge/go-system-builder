package investigation_test

import (
	"bytes"
	"path/filepath"
	"strings"
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
	// This exact interleaving reaches the original frozen-journal check before
	// Case CAS. It must remain a rejection, not be disguised as a harmless CAS
	// or an instruction to reconcile potentially damaged persistence.
	if err == nil || !strings.Contains(err.Error(), "S8 original Case continuity") || !strings.Contains(err.Error(), "Main") || !strings.Contains(err.Error(), packet.ID) || !strings.Contains(err.Error(), packet.AnswerPath) {
		t.Fatalf("concurrent reception lost its cause, original object or recovery owner: %v", err)
	}
	conflicted := currentS8Snapshot(t, f)
	if conflicted.Revision != before.Revision+1 {
		t.Fatalf("failed reception committed beyond the unrelated update: %d/%d", conflicted.Revision, before.Revision)
	}
	if _, found, err := runtime.VerifiedHypothesisResultReceipt(f.root, conflicted.State, packet.ID, owner.ownerID); err != nil || found {
		t.Fatalf("failed internal CAS fabricated acceptance: found=%t err=%v", found, err)
	}
	if !bytes.Equal(answerBytes, mustRead(t, filepath.Join(worker, answer))) {
		t.Fatal("CAS conflict rewrote the professional answer")
	}
	// Main has checked that the original Case/dispatch still holds. Reuse the
	// existing consumer and bytes; no new answer, publication or retry loop.
	request.AuthorizeSource = nil
	accepted, err := investigation.SubmitIssuedHypothesisResult(f.root, f.statePath, f.journalPath, request)
	if err != nil {
		t.Fatal(err)
	}
	receipt, found, err := runtime.VerifiedHypothesisResultReceipt(f.root, accepted.State, packet.ID, owner.ownerID)
	if err != nil || !found || receipt.AcceptedAfterRuntimeRevision != accepted.Revision || accepted.Revision != conflicted.Revision+1 {
		t.Fatalf("original receipt no longer proves its exact commit: receipt=%+v revision=%d err=%v", receipt, accepted.Revision, err)
	}
	if !bytes.Equal(answerBytes, mustRead(t, filepath.Join(worker, answer))) {
		t.Fatal("Main recovery required changing the accepted professional answer")
	}
	replayed, err := investigation.SubmitIssuedHypothesisResult(f.root, f.statePath, f.journalPath, request)
	if err != nil || replayed.Revision != accepted.Revision {
		t.Fatalf("recovered original receipt was not idempotent: revision=%d/%d err=%v", replayed.Revision, accepted.Revision, err)
	}
}
