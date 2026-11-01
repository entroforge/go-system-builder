package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/transition"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

type s3IncompleteGate struct{}

func TestS3DirectTransitionCannotSkipCurrentQuestionnaire(t *testing.T) {
	root, _ := newContractAnswerFixture(t)
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, root)
	stateBytes, _ := os.ReadFile(s.StatePath)
	journalBytes, _ := os.ReadFile(s.JournalPath)
	_, err = transition.Apply(root, s.StatePath, s.JournalPath, transition.Request{
		TransitionID: "PTR-PLAN-02", Actor: "orchestrator", ExpectedRevision: before.Revision, Files: s.Files,
	})
	if err == nil || (!strings.Contains(err.Error(), "received current contract questionnaire") || !strings.Contains(err.Error(), "save its existing answer")) {
		t.Fatalf("direct transition bypassed original S3 reception, or failed for the wrong reason: %v", err)
	}
	if data, _ := os.ReadFile(s.StatePath); string(data) != string(stateBytes) {
		t.Fatal("rejected direct S3 transition changed Runtime")
	}
	if data, _ := os.ReadFile(s.JournalPath); string(data) != string(journalBytes) {
		t.Fatal("rejected direct S3 transition changed journal")
	}
}

func (s3IncompleteGate) Evaluate(_ context.Context, input qualitygate.Input) (qualitygate.Evaluation, error) {
	return qualitygate.Evaluation{Status: qualitygate.StatusNotReady, GateID: input.GateID, CandidateTransition: input.TransitionID, Missing: []string{"contract:docs/dev/contracts/BE-039.md:pending_check"}}, nil
}

func TestS3NotReadyRetainsAcceptedFollowupAndReadRetries(t *testing.T) {
	root, packet := newContractAnswerFixture(t)
	stageJSON(t, root, packet.AnswerPath, completedContractDraft(packet))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := pendingMainFollowupContext(s, packet.ID)
	receipt, err := s.SaveContracts(ctx, packet.AnswerPath)
	cancel()
	if err != nil || !receipt.Accepted {
		t.Fatalf("original S3 reception: %+v %v", receipt, err)
	}
	before := stageSnapshot(t, root)
	record, _, _ := runtime.FindSubmission(before.State, packet.ID)
	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{Root: root, CWD: root, Files: s.Files, Event: "PostToolUse", ToolName: "Write", SubmissionFollowupID: record.FollowupID, GateEvaluator: s3IncompleteGate{}})
	if err != nil || result.Error != "" || result.QualityGate.TransitionCommitted {
		t.Fatalf("incomplete S3 gate: %+v %v", result, err)
	}
	waiting := stageSnapshot(t, root)
	if waiting.Revision != before.Revision {
		t.Fatal("not-ready S3 consumed or rewrote the accepted followup")
	}
	if _, err := runtime.SubmissionFollowup(waiting.State, record.FollowupID); err != nil {
		t.Fatalf("S3 followup no longer retryable: %v", err)
	}
	advanced, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{Root: root, CWD: root, Files: s.Files, Event: "PreToolUse", ToolName: "Read"})
	if err != nil || advanced.Error != "" || !advanced.QualityGate.TransitionCommitted {
		t.Fatalf("ordinary listener did not resume original S3: %+v %v", advanced, err)
	}
	after, found, err := runtime.FindSubmission(advanced.Snapshot.State, packet.ID)
	if err != nil || !found || after.ControlStatus != "advanced" || after.Key != record.Key || after.EvidenceID != record.EvidenceID {
		t.Fatalf("resumption changed original reception: %+v %v", after, err)
	}
	assertPlanningTransitionJournalEvent(t, root, after.TransitionEventID, "PTR-PLAN-02", "contracts")
	assertDesignCheckpoint(t, root)
	stateBytes, _ := os.ReadFile(s.StatePath)
	journalBytes, _ := os.ReadFile(s.JournalPath)
	replay, err := s.SaveContracts(context.Background(), packet.AnswerPath)
	if err != nil || !replay.Accepted || len(replay.WaitingOn) != 0 || strings.Contains(replay.NextAction, "recover") {
		t.Fatalf("advanced S3 replay asked for system recovery: %+v %v", replay, err)
	}
	if data, _ := os.ReadFile(s.StatePath); string(data) != string(stateBytes) {
		t.Fatal("advanced S3 replay changed Runtime")
	}
	if data, _ := os.ReadFile(s.JournalPath); string(data) != string(journalBytes) {
		t.Fatal("advanced S3 replay changed journal")
	}
}

func TestS3RecoverResumesPendingWithoutAnotherAnswer(t *testing.T) {
	root, packet := newContractAnswerFixture(t)
	stageJSON(t, root, packet.AnswerPath, completedContractDraft(packet))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := pendingMainFollowupContext(s, packet.ID)
	_, err = s.SaveContracts(ctx, packet.AnswerPath)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, root)
	record, _, _ := runtime.FindSubmission(before.State, packet.ID)
	writeContractSource(t, root, packet.AnswerPath, "mutable authoring file is no longer required for original recovery\n")
	receipts, err := s.Recover(context.Background(), "Main", 2)
	if err != nil || len(receipts) != 1 || !receipts[0].Accepted {
		t.Fatalf("pending S3 recovery: %+v %v", receipts, err)
	}
	after := stageSnapshot(t, root)
	recovered, _, _ := runtime.FindSubmission(after.State, packet.ID)
	if recovered.ControlStatus != "advanced" || recovered.EvidenceID != record.EvidenceID {
		t.Fatalf("recovery replaced original professional work: %+v", recovered)
	}
	assertDesignCheckpoint(t, root)
}

func TestS3UncommittedOutputNamesCommitAndAcceptsSameAnswerAfterCommit(t *testing.T) {
	root, packet := newContractAnswerFixture(t)
	path := "docs/dev/contracts/BE-039.md"
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\nProfessional rework retained in this S3 entry.\n")...)
	writeContractSource(t, root, path, string(data))
	stageJSON(t, root, packet.AnswerPath, completedContractDraft(packet))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := s.SaveContracts(context.Background(), packet.AnswerPath)
	branch, branchErr := fileview.DevelopmentRef(stageSnapshot(t, root).State)
	if branchErr != nil {
		t.Fatal(branchErr)
	}
	if err == nil || rejected.Accepted || !strings.Contains(rejected.Error, path) || !strings.Contains(rejected.Error, branch) || !strings.Contains(rejected.Error, "commit") || !strings.Contains(rejected.NextAction, filepath.Join(root, packet.AnswerPath)) || strings.Contains(rejected.NextAction, "具名修订") {
		t.Fatalf("uncommitted professional output sent Main to a different route: %+v %v", rejected, err)
	}
	if _, found, err := runtime.FindSubmission(stageSnapshot(t, root).State, packet.ID); err != nil || found {
		t.Fatalf("rejected output was received: found=%t %v", found, err)
	}
	fixtures.CommitCurrentFixture(t, root)
	s, err = submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.SaveContracts(context.Background(), packet.AnswerPath)
	if err != nil || !r.Accepted || !r.WorkComplete || r.CurrentStage != "S4" {
		t.Fatalf("same current answer did not receive committed S3 rework: %+v %v", r, err)
	}
	if current, _ := os.ReadFile(filepath.Join(root, path)); string(current) != string(data) {
		t.Fatal("professional repair was rolled back")
	}
	record, found, err := runtime.FindSubmission(stageSnapshot(t, root).State, packet.ID)
	if err != nil || !found || record.ControlStatus != "advanced" {
		t.Fatalf("same packet did not advance: %+v %v", record, err)
	}
}
