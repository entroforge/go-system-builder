package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIssuedResultFrozenInputSurvivesAuthorEditAndNewSaveConflicts(t *testing.T) {
	root, sp, jp, _, packets, requests := issuedConcurrentResultsFixture(t)
	packet, request := packets[0], requests[0]
	path := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
	original := mustReadTestFile(t, path)
	input, issues, err := ReadIssuedResultInput(context.Background(), root, packet, request.OwnerAgentID)
	if err != nil || len(issues) != 0 {
		t.Fatalf("freeze answer: %v %v", issues, err)
	}
	// This compatibility callback runs at the original consumer boundary, after
	// the service captured the answer, and simulates a subsequent editor save.
	request.Input = input
	request.AuthorizeSource = func(_ string, _ map[string]any) error {
		return os.WriteFile(path, []byte(`{"changed":"after original read"}`), 0o600)
	}
	accepted, err := SubmitIssuedResult(root, sp, jp, request)
	if err != nil {
		t.Fatalf("fixed answer not consumed: %v", err)
	}
	if _, ok, err := IssuedResultReceipt(root, accepted.State, packet.ID, request.OwnerAgentID); err != nil || !ok {
		t.Fatalf("receipt: %v %v", ok, err)
	}
	// An explicit new Save reads new bytes. A changed professional observation
	// must not be hidden by the prior receipt or a stale per-packet input cache.
	changed := strings.Replace(string(original), "observed claim-qa-1", "new observation for claim-qa-1", 1)
	if changed == string(original) {
		t.Fatal("fixture observation not found")
	}
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	request.Input, request.AuthorizeSource = nil, nil
	if _, err := SubmitIssuedResult(root, sp, jp, request); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("new explicit save must conflict: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := IssuedResultReceipt(root, accepted.State, packet.ID, request.OwnerAgentID); err != nil || !ok {
		t.Fatalf("receipt depends on private answer: %v %v", ok, err)
	}
}

func TestIssuedResultFrozenInputCannotCrossPacketOwnerOrSource(t *testing.T) {
	for _, change := range []string{"packet", "owner", "source"} {
		t.Run(change, func(t *testing.T) {
			root, sp, jp, _, packets, requests := issuedConcurrentResultsFixture(t)
			input, issues, err := ReadIssuedResultInput(context.Background(), root, packets[0], requests[0].OwnerAgentID)
			if err != nil || len(issues) != 0 {
				t.Fatalf("freeze answer: %v %v", issues, err)
			}
			request := requests[0]
			switch change {
			case "packet":
				request = requests[1]
			case "owner":
				input.owner = requests[1].OwnerAgentID
			case "source":
				input.sourceRoot = t.TempDir()
			}
			request.Input = input
			if _, err := SubmitIssuedResult(root, sp, jp, request); err == nil || !strings.Contains(err.Error(), "frozen answer differs") {
				t.Fatalf("mismatched %s accepted: %v", change, err)
			}
		})
	}
}

func TestIssuedResultFrozenInputRetainsFieldErrorsAndCancellation(t *testing.T) {
	root, _, _, _, packets, _ := issuedConcurrentResultsFixture(t)
	packet := packets[0]
	path := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	input, issues, err := ReadIssuedResultInput(context.Background(), root, packet, packet.Binding.OwnerAgentID)
	if err != nil || len(issues) == 0 {
		t.Fatalf("missing original field issues: %v %v", issues, err)
	}
	pointer := input.issues[0].Pointer
	issues[0].Pointer = "/caller-mutated"
	if input.issues[0].Pointer != pointer {
		t.Fatal("caller changed captured validation")
	}
	if _, err := input.validated(input.sourceRoot, packet.AnswerPath, packet.Binding.OwnerAgentID, packet); err == nil {
		t.Fatal("invalid answer gained validation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ReadIssuedResultInput(ctx, root, packet, packet.Binding.OwnerAgentID); err != context.Canceled {
		t.Fatalf("canceled read: %v", err)
	}
}

type deadlineAfterFrozenCapture struct {
	*deadlineAfterReviewStaging
	capture string
}

func (c *deadlineAfterFrozenCapture) Err() error {
	err := c.deadlineAfterReviewStaging.Err()
	if c.triggered {
		_ = os.WriteFile(c.capture, []byte("malformed subsequent capture line"), 0o600)
	}
	return err
}

func TestReviewDeadlineContinuationUsesOriginalCapturePreparation(t *testing.T) {
	root, sp, jp, snapshot, plan := packetModeFixture(t)
	const id = "review-result-frozen-capture"
	input := writeResultFile(t, root, plan, "assignment-qa-1", id, "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	artifact := filepath.Join(root, ".claude", "evidence", snapshot.State["runtime_id"].(string), "g1", "reviews", "agent-qa-1", id+".json")
	capture := filepath.Join(root, "docs", "reports", "original-capture.jsonl")
	ctx := &deadlineAfterFrozenCapture{deadlineAfterReviewStaging: &deadlineAfterReviewStaging{Context: context.Background(), artifact: artifact}, capture: capture}
	accepted, err := SubmitResult(root, sp, jp, SubmitRequest{Context: ctx, ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: input, CaptureDir: capture})
	if err != nil {
		t.Fatalf("continuation reread changed capture: %v", err)
	}
	if accepted.Revision != snapshot.Revision+1 {
		t.Fatal("continuation did not accept once")
	}
	if _, err := LoadCaptureStepsStrict(capture); err == nil {
		t.Fatal("fixture capture did not change")
	}
}

func TestReviewDeadlineBeforeStagingDoesNotRestartSubmission(t *testing.T) {
	root, sp, jp, before, plan := packetModeFixture(t)
	const id = "review-result-no-new-deadline-work"
	input := writeResultFile(t, root, plan, "assignment-qa-1", id, "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	current, err := SubmitResult(root, sp, jp, SubmitRequest{Context: ctx, ExpectedRevision: before.Revision, AssignmentID: "assignment-qa-1", ResultPath: input})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline started a fresh consumption: %v", err)
	}
	if current.Revision != before.Revision {
		t.Fatal("deadline committed unstaged work")
	}
	artifact := filepath.Join(root, ".claude", "evidence", before.State["runtime_id"].(string), "g1", "reviews", "agent-qa-1", id+".json")
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("unstaged attempt published artifact: %v", err)
	}
}
