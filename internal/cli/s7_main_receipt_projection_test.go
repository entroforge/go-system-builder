package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
)

func TestS7LegacySaveProjectionDistinguishesCommittedReceiptFailure(t *testing.T) {
	gap := &submission.ReviewResultReceiptError{PacketID: "packet-own", OwnerAgentID: "reviewer-own", AnswerPath: "original/answer.json", SourceRoot: "/original/source", Revision: 42, Cause: errors.New("immutable Result unavailable")}
	var out, diagnostic bytes.Buffer
	renderReviewResultSaveHook(&out, &diagnostic, gap.AnswerPath, submission.Receipt{PacketID: gap.PacketID}, fmt.Errorf("wrapped: %w", gap))
	for _, want := range []string{"original submission succeeded", "receipt verification remains incomplete", gap.PacketID, gap.OwnerAgentID, gap.AnswerPath, gap.SourceRoot, "42", "immutable Result unavailable", "runtime review-continue", "runtime audit-prepare", "No author correction"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing committed submission fact %q: %s", want, &out)
		}
	}
	if strings.Contains(out.String()+diagnostic.String(), "not_accepted") || strings.Contains(diagnostic.String(), "not accepted") || strings.Contains(out.String(), `"accepted":true`) || strings.Contains(out.String(), `"work_complete":true`) {
		t.Fatalf("unverified receipt was presented as rejection/completion: %s %s", &out, &diagnostic)
	}
	out.Reset()
	diagnostic.Reset()
	renderReviewResultSaveHook(&out, &diagnostic, gap.AnswerPath, submission.Receipt{}, errors.New("original source unavailable before submission"))
	if strings.Contains(out.String()+diagnostic.String(), "submission succeeded") || !strings.Contains(diagnostic.String(), "submission is unconfirmed") {
		t.Fatalf("pre-submit error manufactured commit: %s %s", &out, &diagnostic)
	}
}

func TestS7MainReceiptReadFailurePreservesOriginalSubmissionDiagnostic(t *testing.T) {
	root := t.TempDir()
	state := s7RecoveryFixtureState("running", "running")
	subject := nativeReturnSubject{kind: "s7-review-result-v1", id: "packet-own", ref: "original/view.md"}
	for _, prior := range []string{"", "original submission succeeded at Runtime revision 42; receipt verification failed for reviewer-own at original/answer.json"} {
		submission.RecordPlanningDiagnostic(root, state, nativeS7DiagnosticKind(subject.id), prior)
		text := nativeS7MainReceiptGap(context.Background(), root, runtime.Snapshot{State: state}, subject, "", errors.New("current immutable Result read failed"))
		for _, want := range []string{prior, "current receipt is unknown", "current immutable Result read failed", subject.id, subject.ref, "runtime review-continue", "runtime audit-prepare", "No author correction"} {
			if !strings.Contains(text, want) {
				t.Fatalf("lost original fact/current failure %q: %s", want, text)
			}
		}
		if prior == "" && strings.Contains(text, "submission succeeded") {
			t.Fatalf("receipt read failure manufactured commit: %s", text)
		}
		if strings.Contains(text, "not accepted") || strings.Contains(text, `"accepted":true`) || strings.Contains(text, "historical or replaced") {
			t.Fatalf("unknown receipt state was replaced by a conclusion: %s", text)
		}
	}
}
