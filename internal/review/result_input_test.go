package review

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIssuedResultAcceptsHistoricalDigestWithoutReauthoring(t *testing.T) {
	root, sp, jp, _, packets, requests := issuedConcurrentResultsFixture(t)
	packet, request := packets[0], requests[0]
	path := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
	var answer map[string]any
	if err := json.Unmarshal(mustReadTestFile(t, path), &answer); err != nil {
		t.Fatal(err)
	}
	answer["answer"].(map[string]any)["verification_artifact_digest"] = "historical-value-not-a-sha"
	data, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	accepted, err := SubmitIssuedResult(root, sp, jp, request)
	if err != nil {
		t.Fatalf("original answer rejected due to historical digest: %v", err)
	}
	if _, ok, err := IssuedResultReceipt(root, accepted.State, packet.ID, request.OwnerAgentID); err != nil || !ok {
		t.Fatalf("original result not received: %v %v", ok, err)
	}
	if string(mustReadTestFile(t, path)) != string(data) {
		t.Fatal("original author answer was rewritten")
	}
}

func TestIssuedResultWriterContinuesPastHistoricalDigestFields(t *testing.T) {
	root, sp, jp, _, packets, requests := issuedConcurrentResultsFixture(t)
	first, err := SubmitIssuedResult(root, sp, jp, requests[0])
	if err != nil {
		t.Fatal(err)
	}
	rows := first.State["entities"].(map[string]any)["work_packets"].([]any)
	var row map[string]any
	for _, raw := range rows {
		candidate, _ := raw.(map[string]any)
		if candidate["packet_id"] == packets[0].ID {
			row = candidate
			break
		}
	}
	if row == nil {
		t.Fatal("original packet missing")
	}
	row["template_sha256"] = "old-template"
	binding := row["binding"].(map[string]any)
	binding["review_plan_sha256"] = "old-plan"
	binding["assignment_sha256"] = "old-assignment"
	oldReception := row["review_result_reception"].(map[string]any)
	oldReceipt := oldReception["receipt"].(map[string]any)
	oldReceipt["answer_sha256"] = "old-answer"
	oldReceipt["packet_sha256"] = "old-packet"
	data, err := json.Marshal(first.State)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sp, data, 0o600); err != nil {
		t.Fatal(err)
	}
	request := requests[1]
	request.ExpectedRevision = first.Revision
	continued, err := SubmitIssuedResult(root, sp, jp, request)
	if err != nil {
		t.Fatalf("historical packet/receipt blocked original Writer: %v", err)
	}
	if _, ok, err := IssuedResultReceipt(root, continued.State, packets[1].ID, request.OwnerAgentID); err != nil || !ok {
		t.Fatalf("next original responsibility was not received: %v %v", ok, err)
	}
	for _, raw := range continued.State["entities"].(map[string]any)["work_packets"].([]any) {
		candidate, _ := raw.(map[string]any)
		switch candidate["packet_id"] {
		case packets[0].ID:
			if candidate["template_sha256"] != "old-template" || candidate["review_result_reception"].(map[string]any)["receipt"].(map[string]any)["answer_sha256"] != "old-answer" {
				t.Fatal("historical packet/receipt was rewritten")
			}
		case packets[1].ID:
			receipt := candidate["review_result_reception"].(map[string]any)["receipt"].(map[string]any)
			for _, field := range []string{"review_plan_sha256", "assignment_sha256", "answer_sha256", "effective_answer_sha256", "packet_sha256", "result_sha256", "subject_digest", "verification_artifact_digest"} {
				if _, exists := receipt[field]; exists {
					t.Fatalf("new receipt registered digest field %s", field)
				}
			}
		}
	}
}

func TestIssuedResultInputSurvivesAuthorEditAndReceiptRemainsStable(t *testing.T) {
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
	// After acceptance the automatic observer returns the original receipt;
	// it does not reconsume the author's later edit.
	changed := strings.Replace(string(original), "observed claim-qa-1", "new observation for claim-qa-1", 1)
	if changed == string(original) {
		t.Fatal("fixture observation not found")
	}
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	request.Input, request.AuthorizeSource = nil, nil
	replayed, err := SubmitIssuedResult(root, sp, jp, request)
	if err != nil || replayed.Revision != accepted.Revision {
		t.Fatalf("automatic observer must retain the accepted receipt: revision=%d, err=%v", replayed.Revision, err)
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
	*deadlineAfterReviewPublication
	capture string
}

func (c *deadlineAfterFrozenCapture) Err() error {
	err := c.deadlineAfterReviewPublication.Err()
	if c.triggered {
		_ = os.WriteFile(c.capture, []byte("malformed subsequent capture line"), 0o600)
	}
	return err
}

func TestReviewDeadlineBeforeStagingDoesNotRestartSubmission(t *testing.T) {
	root, sp, jp, before, plan := packetModeFixture(t)
	const id = "review-result-no-new-deadline-work"
	input := writeResultFile(t, root, plan, "assignment-qa-1", id, "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := SubmitResult(root, sp, jp, SubmitRequest{Context: ctx, ExpectedRevision: before.Revision, AssignmentID: "assignment-qa-1", ResultPath: input})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline started a fresh consumption: %v", err)
	}
	if current := snapshotState(t, sp, jp); intField(current["revision"]) != before.Revision {
		t.Fatal("deadline committed new work")
	}
	artifact := filepath.Join(root, ".claude", "evidence", before.State["runtime_id"].(string), "g1", "reviews", "agent-qa-1", id+".json")
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("unstaged attempt published artifact: %v", err)
	}
}

func TestExpiredFrozenResultWithoutReceiptDoesNotStartReception(t *testing.T) {
	root, sp, jp, before, plan := packetModeFixture(t)
	input := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-expired-frozen-unreceived", "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	frozen := mustReadTestFile(t, input)
	state, journal := mustReadTestFile(t, sp), mustReadTestFile(t, jp)
	if err := os.Remove(input); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
	defer cancel()
	_, err := SubmitResult(root, sp, jp, SubmitRequest{Context: ctx, ExpectedRevision: before.Revision, AssignmentID: "assignment-qa-1", ResultPath: input, resultData: frozen})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired frozen answer started reception: %v", err)
	}
	if string(mustReadTestFile(t, sp)) != string(state) || string(mustReadTestFile(t, jp)) != string(journal) {
		t.Fatal("expired unreceived work changed Runtime")
	}
	if _, err := os.Stat(input); !os.IsNotExist(err) {
		t.Fatal("recovery recreated author input")
	}
}
