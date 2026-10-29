package investigation_test

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestS8RegisteredWorkerAnswerUsesOriginalConsumerWithoutHookAuthorization(t *testing.T) {
	fixture, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	initial := currentS8Snapshot(t, fixture)
	workerRoot := registerS8WorkerExecution(t, fixture, initial, owner)

	issued := currentS8Snapshot(t, fixture)
	packets, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, issued.Revision, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue S8 packet for registered Worker: packets=%d err=%v", len(packets), err)
	}
	packet := packets[0]
	answerPath := writeS8ProfessionalAnswer(t, workerRoot, packet, "The registered Worker inspected the frozen Finding evidence.")
	observedAnswerPath := filepath.Join(workerRoot, filepath.FromSlash(answerPath))
	request := investigation.IssuedHypothesisResultRequest{
		ExpectedRevision: issued.Revision,
		PacketID:         packet.ID,
		AnswerPath:       answerPath,
		OwnerAgentID:     owner.ownerID,
		// SourceRoot and AuthorizeSource are intentionally omitted. The original
		// packet and workspace registry determine the exact Worker checkout.
	}

	stateBeforeRejects, journalBeforeRejects := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	wrongOwner := request
	wrongOwner.OwnerAgentID = "agent-not-the-registered-investigator"
	if _, err := investigation.SubmitIssuedHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, wrongOwner); err == nil {
		t.Fatal("S8 packet accepted a different owner tuple")
	}
	wrongRoot := request
	wrongRoot.SourceRoot = fixture.root
	if _, err := investigation.SubmitIssuedHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, wrongRoot); err == nil {
		t.Fatal("S8 packet accepted Main as a substitute for its registered Worker source")
	}
	if !bytes.Equal(stateBeforeRejects, mustRead(t, fixture.statePath)) || !bytes.Equal(journalBeforeRejects, mustRead(t, fixture.journalPath)) {
		t.Fatal("wrong owner/source tuple changed the original Runtime state or journal")
	}

	service := &submission.Service{
		Root: fixture.root, StatePath: fixture.statePath, JournalPath: fixture.journalPath,
		Validator: semantic.RuntimeCandidateValidator{},
	}
	selectedPacket, selectedRoot, found, err := service.HypothesisResultSourceForPath(observedAnswerPath)
	if err != nil || !found || selectedPacket.ID != packet.ID || selectedRoot != workerRoot {
		t.Fatalf("resolve S8 tool-saved absolute Worker path: packet=%s root=%q found=%v err=%v", selectedPacket.ID, selectedRoot, found, err)
	}
	acceptedReceipt, err := service.SaveHypothesisResultFrom(context.Background(), owner.ownerID, observedAnswerPath, "", nil)
	if err != nil || !acceptedReceipt.Accepted {
		t.Fatalf("S8 service/original consumer required Hook source authorization for a registered Worker answer: receipt=%+v err=%v", acceptedReceipt, err)
	}
	accepted := currentS8Snapshot(t, fixture)
	receipt, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, accepted.State, packet.ID, owner.ownerID)
	if err != nil || !found || receipt.ExecutionRoot != workerRoot {
		t.Fatalf("original Case receipt lost registered Worker source: found=%v root=%q want=%q err=%v", found, receipt.ExecutionRoot, workerRoot, err)
	}

	acceptedState, acceptedJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	replayed, err := investigation.SubmitIssuedHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, request)
	if err != nil || replayed.Revision != accepted.Revision {
		t.Fatalf("exact S8 answer replay failed: revision=%d want=%d err=%v", replayed.Revision, accepted.Revision, err)
	}
	if !bytes.Equal(acceptedState, mustRead(t, fixture.statePath)) || !bytes.Equal(acceptedJournal, mustRead(t, fixture.journalPath)) {
		t.Fatal("exact S8 answer replay wrote another Runtime or Case revision")
	}
}

func registerS8WorkerExecution(t *testing.T, fixture *intakeFixture, snapshot runtime.Snapshot, owner s8OwnerFixture) string {
	t.Helper()
	commitS8WorkerPlanInputs(t, fixture, owner)
	registry, err := workspace.New(context.Background(), fixture.root)
	if err != nil {
		t.Fatalf("read S8 Main workspace authority: %v", err)
	}
	execution, err := registry.Plan(context.Background(), snapshot.State, owner.assignmentID, owner.ownerID, nil, nil, nil)
	if err != nil {
		t.Fatalf("plan registered S8 Worker execution: %v", err)
	}
	if err := registry.Materialize(context.Background(), execution); err != nil {
		t.Fatalf("materialize registered S8 Worker execution: %v", err)
	}
	execution.Status = "ready"
	registry.Executions[execution.AssignmentID] = execution

	writer := runtime.NewWriter(fixture.statePath, fixture.journalPath, fixture.root, semantic.RuntimeCandidateValidator{})
	_, err = writer.UpdateCurrent(runtime.Mutation{
		EventID: "evt-test-s8-worker-registration-" + owner.assignmentID, TransitionID: "WORKSPACE", Event: "workspace_updated",
		Actor: "main", RuntimeID: execution.RuntimeID, BaselineGeneration: execution.BaselineGeneration,
		IdempotencyKey: "test:s8-worker-registration:" + owner.assignmentID, RetainLastTransition: true, OccurredAt: time.Now().UTC(),
		Apply: func(state map[string]any) error {
			state["workspace"] = workspace.Encode(registry)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("persist exact S8 Worker execution in original Runtime: %v", err)
	}
	return execution.Path
}

func commitS8WorkerPlanInputs(t *testing.T, fixture *intakeFixture, owner s8OwnerFixture) {
	t.Helper()
	inputs := []string{
		filepath.ToSlash(filepath.Join(".claude", "agents", "investigator.md")),
		filepath.ToSlash(filepath.Join(".claude", "tasks", owner.taskID+".json")),
		filepath.ToSlash(filepath.Join("docs", "control", "hook-policy.json")),
		filepath.ToSlash(filepath.Join("docs", "control", "loop-definition.json")),
	}
	add := exec.Command("git", append([]string{"-C", fixture.root, "add", "--"}, inputs...)...)
	if output, err := add.CombinedOutput(); err != nil {
		t.Fatalf("stage current S8 Worker workspace inputs %v: %v: %s", inputs, err, output)
	}
	commit := exec.Command("git", "-C", fixture.root, "commit", "-m", "fixture current S8 Worker plan inputs")
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("commit current S8 Worker workspace inputs %v: %v: %s", inputs, err, output)
	}
}
