package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
)

func TestS7NaturalReceiptPreservesAcceptedAnswerAndMainOwnedContinuation(t *testing.T) {
	for _, test := range []struct{ name, state, phase, status, cause, want string }{
		{"clean_exit_pending", "verification", "clean", "clean", "original Gate missing release evidence", "runtime review-continue"},
		{"s10_material_pending", "acceptance", "acceptance", "clean", "S10 questionnaire preparation failed: source unavailable", "S10 acceptance"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			state := map[string]any{"runtime_id": "receipt-runtime", "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": test.state, "phase": test.phase, "phase_revision": 3}, "review": map[string]any{"plan": map[string]any{"status": test.status}}}
			submission.RecordPlanningDiagnostic(root, state, "S7 acceptance continuation", test.cause)
			var out bytes.Buffer
			nativeS7NaturalReceipt(root, policy.Input{Event: "PreToolUse"}, runtime.Snapshot{State: state}, "reviewer-one", "packet-one", "original-answer", submission.Receipt{Accepted: true, WorkComplete: true, ResponsibilityStatus: "done"}, "", &out)
			for _, want := range []string{"accepted=true", "work_complete=true", "Main owns", "Reviewer does not inherit", test.cause, test.want} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("lost accepted receipt / concrete outlet %q: %s", want, &out)
				}
			}
		})
	}
}

func TestS7NaturalReceiptKeepsAcceptedTruthWhenCurrentSnapshotFails(t *testing.T) {
	var out bytes.Buffer
	nativeS7NaturalReceipt(t.TempDir(), policy.Input{Event: "PostToolUse"}, runtime.Snapshot{}, "reviewer-one", "packet-one", "original-answer", submission.Receipt{Accepted: true, WorkComplete: true, ResponsibilityStatus: "done"}, "original Runtime read failed; Main restores it", &out)
	for _, want := range []string{"accepted=true", "work_complete=true", "stage is unavailable", "Main owns recovery", "original Runtime read failed"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("lost accepted recovery fact %q: %s", want, &out)
		}
	}
}

func TestS7SubmittedReceiptFailureKeepsCommitAndMainRecovery(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		t.Run(event, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			gap := &submission.ReviewResultReceiptError{PacketID: "original-packet", OwnerAgentID: "original-reviewer", AnswerPath: "original-answer.json", SourceRoot: "/original/source", Revision: 42, Cause: errors.New("original receipt source unavailable")}
			root := t.TempDir()
			state := map[string]any{"runtime_id": "receipt-runtime", "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "verification", "phase": "running", "phase_revision": 3}}
			if code := nativeS7SubmittedReceiptFailure(root, policy.Input{Event: event}, state, gap, &out, &diagnostic); code != 0 {
				t.Fatalf("receipt failure blocked event: %d", code)
			}
			saved := submission.PlanningDiagnostic(root, state, nativeS7DiagnosticKind(gap.PacketID))
			if !strings.Contains(saved, "original submission succeeded") || !strings.Contains(saved, gap.Cause.Error()) || !strings.Contains(saved, gap.PacketID) {
				t.Fatalf("Main display diagnostic lost committed fact: %s", saved)
			}
			for _, message := range []string{out.String(), diagnostic.String()} {
				for _, want := range []string{"original submission succeeded", "receipt verification remains incomplete", gap.PacketID, gap.OwnerAgentID, gap.AnswerPath, gap.SourceRoot, "revision 42", "Main must inspect the original receipt", "runtime review-continue", "original receipt source unavailable", "best effort and may not have reached Main"} {
					if !strings.Contains(message, want) {
						t.Fatalf("lost committed submission fact %q: %s", want, message)
					}
				}
				for _, forbidden := range []string{"unreceived", "accepted=true", "work_complete=true", "Correct professional fields", "resubmit", "save again"} {
					if strings.Contains(message, forbidden) {
						t.Fatalf("receipt failure claimed incorrect fact %q: %s", forbidden, message)
					}
				}
			}
		})
	}
}

func TestS7SubmittedReceiptFailureReportsDiagnosticDeliveryLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/diagnostics"), []byte("blocked diagnostic directory"), 0600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"runtime_id": "receipt-runtime", "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "verification", "phase": "running"}}
	gap := &submission.ReviewResultReceiptError{PacketID: "original-packet", OwnerAgentID: "original-reviewer", AnswerPath: "original-answer.json", SourceRoot: root, Revision: 42, Cause: errors.New("receipt unavailable")}
	var out, diagnostic bytes.Buffer
	if code := nativeS7SubmittedReceiptFailure(root, policy.Input{Event: "PostToolUse"}, state, gap, &out, &diagnostic); code != 0 {
		t.Fatalf("display storage changed committed submission: %d", code)
	}
	for _, message := range []string{out.String(), diagnostic.String()} {
		for _, want := range []string{"original submission succeeded", "receipt verification remains incomplete", "best effort and may not have reached Main", "independently verify the original authoritative receipt"} {
			if !strings.Contains(message, want) {
				t.Fatalf("lost diagnostic delivery limit %q: %s", want, message)
			}
		}
	}
}
