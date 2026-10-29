package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/transition"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

type s4UnreadyGate struct{}

func (s4UnreadyGate) Evaluate(_ context.Context, input qualitygate.Input) (qualitygate.Evaluation, error) {
	return qualitygate.Evaluation{Status: qualitygate.StatusNotReady, GateID: input.GateID, CandidateTransition: input.TransitionID, Missing: []string{"task:pending_machine_check"}}, nil
}

func TestS4DirectTransitionCannotSkipAnswer(t *testing.T) {
	root, packet := newTaskPlanningAnswerFixture(t)
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, root)
	_, err = transition.Apply(root, s.StatePath, s.JournalPath, transition.Request{TransitionID: "TR-002", Actor: "orchestrator", ExpectedRevision: before.Revision, Files: s.Files})
	if err == nil {
		t.Fatalf("TR-002 bypassed unreceived S4 questionnaire %s: lifecycle=%v", packet.ID, stageSnapshot(t, root).State["lifecycle"])
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("rejected direct transition mutated Runtime")
	}
}

func TestS4DirectApplyRejectsAcceptedTaskDriftAtomically(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := pendingMainFollowupContext(s, p.ID)
	receipt, err := s.SaveTaskPlanning(ctx, p.AnswerPath)
	cancel()
	if err != nil || !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("S4 reception did not leave an accepted followup: %+v %v", receipt, err)
	}
	accepted := stageSnapshot(t, root)
	record, found, err := runtime.FindSubmission(accepted.State, p.ID)
	if err != nil || !found || record.ControlStatus != "pending" || record.FollowupID == "" {
		t.Fatalf("S4 followup is not pending: %+v found=%t err=%v", record, found, err)
	}

	path := "docs/dev/tasks/TASK-039-01.md"
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	writeContractSource(t, root, path, string(data)+"\nCommitted after the accepted S4 answer.\n")
	fixtures.CommitCurrentFixture(t, root)
	s, err = submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, root)
	statePath, journalPath := s.StatePath, s.JournalPath
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	taskBefore, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	_, err = transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: "TR-002", Actor: "orchestrator", ExpectedRevision: before.Revision,
		SubmissionFollowupID: record.FollowupID, Files: s.Files,
	})
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("TR-002 accepted the committed post-reception TASK change: %v", err)
	}
	after := stageSnapshot(t, root)
	stateAfter, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	journalAfter, readErr := os.ReadFile(journalPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	taskAfter, readErr := os.ReadFile(filepath.Join(root, path))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if after.Revision != before.Revision || string(stateAfter) != string(stateBefore) || string(journalAfter) != string(journalBefore) || string(taskAfter) != string(taskBefore) {
		t.Fatal("rejected TR-002 changed the Runtime pair or committed TASK bytes")
	}
}

func TestS4TR002RegistersOnlyAcceptedREQBatch(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t, func(root string) {
		writeContractSource(t, root, "docs/dev/contracts/CONTRACTS-777.md", "# CONTRACTS-777\n> REQ: REQ-777\n> Status: locked\n> Version: v1.0.0\n\n## 合同清单\n| Contract ID | Type | Owner | Status | Version | Notes |\n|:---|:---|:---|:---|:---|:---|\n| BE-777 | backend | Main | locked | v1.0.0 | foreign batch |\n\n## 需求覆盖矩阵\n| Requirement | Clause |\n|:---|:---|\n| REQ-777 | BE-777 §1 |\n")
		writeContractSource(t, root, "docs/dev/contracts/BE-777.md", "# BE-777\n> Source REQ refs: REQ-777\n> Status: locked\n> Version: v1.0.0\n\n## 1. Foreign behavior\nClause §1 belongs to the other REQ.\n")
	})
	writeContractSource(t, root, "docs/dev/tasks/TASK-777-01.md", "# Foreign task\n> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-777\n")
	fixtures.CommitCurrentFixture(t, root)
	// A TASK entity may survive a retired S5 review batch and return to S4.
	// Its lifecycle and owner facts survive that rework; TR-002 refreshes only
	// the source document projection so the later original S6 registration sees
	// the accepted bytes.
	prior := stageSnapshot(t, root)
	entities := prior.State["entities"].(map[string]any)
	entities["tasks"] = []any{map[string]any{
		"id":              "TASK-039-01",
		"state":           "in_progress",
		"path":            "docs/dev/tasks/TASK-039-01.md",
		"sha256":          strings.Repeat("c", 64),
		"owner_agent_ids": []any{"reviewer-spec", "reviewer-task"},
	}}
	stageJSON(t, root, ".claude/loop-state.json", prior.State)
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := pendingMainFollowupContext(s, p.ID)
	receipt, err := s.SaveTaskPlanning(ctx, p.AnswerPath)
	cancel()
	if err != nil || !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("S4 reception with foreign sibling files failed: %+v %v", receipt, err)
	}
	snapshot := stageSnapshot(t, root)
	accepted, found, err := runtime.FindSubmission(snapshot.State, p.ID)
	if err != nil || !found || accepted.ControlStatus != "pending" {
		t.Fatalf("S4 followup is not pending: %+v found=%t err=%v", accepted, found, err)
	}

	_, err = transition.Apply(root, s.StatePath, s.JournalPath, transition.Request{
		TransitionID: "TR-002", Actor: "orchestrator", ExpectedRevision: snapshot.Revision,
		SubmissionFollowupID: accepted.FollowupID, Files: s.Files,
	})
	if err != nil {
		t.Fatalf("TR-002 failed to register its accepted S4 batch: %v", err)
	}
	state := stageSnapshot(t, root).State
	registered := map[string]string{}
	registeredSHA := map[string]string{}
	for _, raw := range state["documents"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] == "task" || row["kind"] == "dispatch_plan" || row["kind"] == "contract" {
			registered[row["path"].(string)] = row["kind"].(string)
			if sha, ok := row["sha256"].(string); ok {
				registeredSHA[row["path"].(string)] = sha
			}
		}
	}
	for path, kind := range map[string]string{
		"docs/dev/contracts/CONTRACTS-039.md": "contract",
		"docs/dev/contracts/BE-039.md":        "contract",
		"docs/dev/tasks/index-REQ-039.md":     "dispatch_plan",
		"docs/dev/tasks/TASK-039-01.md":       "task",
	} {
		if registered[path] != kind {
			t.Errorf("accepted S4/S3 subject %s registered as %q, want %q", path, registered[path], kind)
		}
	}
	for _, path := range []string{"docs/dev/contracts/CONTRACTS-777.md", "docs/dev/contracts/BE-777.md", "docs/dev/tasks/TASK-777-01.md"} {
		if _, ok := registered[path]; ok {
			t.Errorf("TR-002 pulled foreign/unsubmitted sibling into registration: %s", path)
		}
	}
	if len(registered) != 4 {
		t.Errorf("TR-002 registered %d contract/task subjects, want exact accepted set of four: %#v", len(registered), registered)
	}
	var taskEntity map[string]any
	for _, raw := range state["entities"].(map[string]any)["tasks"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == "TASK-039-01" {
			taskEntity = row
			break
		}
	}
	if taskEntity == nil {
		t.Fatal("TR-002 removed the existing Runtime TASK entity")
	}
	if taskEntity["path"] != "docs/dev/tasks/TASK-039-01.md" || taskEntity["sha256"] != registeredSHA["docs/dev/tasks/TASK-039-01.md"] {
		t.Errorf("TR-002 did not refresh TASK source projection from accepted document: path=%v sha=%v registered=%v", taskEntity["path"], taskEntity["sha256"], registeredSHA)
	}
	if taskEntity["state"] != "in_progress" {
		t.Errorf("TR-002 changed existing TASK state: %v", taskEntity["state"])
	}
	owners, ok := taskEntity["owner_agent_ids"].([]any)
	if !ok || len(owners) != 2 || owners[0] != "reviewer-spec" || owners[1] != "reviewer-task" {
		t.Errorf("TR-002 changed existing TASK owners: %#v", taskEntity["owner_agent_ids"])
	}
}

func TestS4NotReadyRetainsOriginalFollowup(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := pendingMainFollowupContext(s, p.ID)
	receipt, err := s.SaveTaskPlanning(ctx, p.AnswerPath)
	cancel()
	if err != nil || !receipt.Accepted {
		t.Fatalf("original reception failed: %+v %v", receipt, err)
	}
	record, _, _ := runtime.FindSubmission(stageSnapshot(t, root).State, p.ID)
	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{Root: root, CWD: root, StatePath: s.StatePath, JournalPath: s.JournalPath, Files: s.Files, Event: "PostToolUse", ToolName: "Write", SubmissionFollowupID: record.FollowupID, GateEvaluator: s4UnreadyGate{}})
	if err != nil || result.Error != "" || result.QualityGate.TransitionCommitted {
		t.Fatalf("incomplete gate evaluation: %+v %v", result, err)
	}
	after := stageSnapshot(t, root)
	record, _, _ = runtime.FindSubmission(after.State, p.ID)
	if _, err := runtime.SubmissionFollowup(after.State, record.FollowupID); err != nil {
		t.Errorf("NotReady consumed original S4 followup: status=%s error=%v", record.ControlStatus, err)
	}
	resumed, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{Root: root, CWD: root, Files: s.Files, Event: "PreToolUse", ToolName: "Read"})
	if err != nil || resumed.Error != "" || !resumed.QualityGate.TransitionCommitted {
		t.Errorf("original Read failed to resume after gate became ready: %v; %s; gate=%+v", err, resumed.Error, resumed.QualityGate)
	}
	recovered, err := s.Recover(context.Background(), "Main", 2)
	if err != nil {
		t.Errorf("recover error: %v", err)
	}
	if stageSnapshot(t, root).State["lifecycle"].(map[string]any)["state"] != "document_verification" {
		t.Errorf("recover cannot resume accepted S4, receipts=%+v", recovered)
	}
}

func TestS4AdvancedReplayReturnsOriginalReceiptWithoutFalseFollowup(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.SaveTaskPlanning(context.Background(), p.AnswerPath)
	if err != nil || !first.Accepted || first.CurrentStage != "S5" {
		t.Fatalf("original S4 advancement: %+v %v", first, err)
	}
	paths := []string{s.StatePath, s.JournalPath, filepath.Join(root, p.AnswerPath)}
	bytes := map[string]string{}
	for _, path := range paths {
		b, _ := os.ReadFile(path)
		bytes[path] = string(b)
	}
	replay, err := s.SaveTaskPlanning(context.Background(), p.AnswerPath)
	if err != nil || !replay.Accepted || replay.SubmissionKey != first.SubmissionKey {
		t.Fatalf("lost original receipt: %+v %v", replay, err)
	}
	for _, path := range paths {
		b, _ := os.ReadFile(path)
		if string(b) != bytes[path] {
			t.Errorf("advanced replay mutated %s", path)
		}
	}
	if len(replay.WaitingOn) > 0 || strings.Contains(replay.NextAction, "recover") || strings.Contains(replay.NextAction, "接续") {
		t.Errorf("advanced replay invented followup: %+v", replay)
	}
}

func TestS4UncommittedDescriptionAcceptsUnchangedProfessionalAnswer(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	path := "docs/dev/tasks/TASK-039-01.md"
	b, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	writeContractSource(t, root, path, string(b)+"\nProfessional task repair retained.\n")
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	beforeAnswer, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.SaveTaskPlanning(context.Background(), p.AnswerPath)
	if err != nil || !r.Accepted || !r.WorkComplete {
		t.Fatalf("description-only uncommitted change rejected: %+v %v", r, err)
	}
	afterAnswer, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil || !bytes.Equal(beforeAnswer, afterAnswer) {
		t.Fatalf("professional answer changed on receipt: %v", err)
	}
	actual, err := os.ReadFile(filepath.Join(root, path))
	if err != nil || !bytes.Equal(actual, append(b, []byte("\nProfessional task repair retained.\n")...)) {
		t.Fatalf("actual professional task was rewritten: %v", err)
	}
}

func TestS4AcceptedDescriptionChangeKeepsCompletionButCannotRegisterChangedMaterials(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := pendingMainFollowupContext(s, p.ID)
	receipt, err := s.SaveTaskPlanning(ctx, p.AnswerPath)
	cancel()
	if err != nil || !receipt.Accepted {
		t.Fatalf("original reception: %+v %v", receipt, err)
	}
	path := "docs/dev/tasks/TASK-039-01.md"
	b, _ := os.ReadFile(filepath.Join(root, path))
	writeContractSource(t, root, path, string(b)+"\nChanged after reception.\n")
	fixtures.CommitCurrentFixture(t, root)
	s, err = submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	result, err := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), qualitygate.Input{Root: root, Snapshot: stageSnapshot(t, root), Files: s.Files, TransitionID: "TR-002", GateID: "GATE-PLANNING-TASKS-COMPLETE"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != qualitygate.StatusSatisfied || len(result.Conflicts) != 0 {
		t.Fatalf("description-only change retroactively invalidated completion: %+v", result)
	}
	before := stageSnapshot(t, root)
	original, found, err := runtime.FindSubmission(before.State, p.ID)
	if err != nil || !found || original.FollowupID == "" {
		t.Fatalf("original accepted followup is missing: %+v %v", original, err)
	}
	beforeAnswer, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := s.SaveTaskPlanning(context.Background(), p.AnswerPath)
	if err != nil || !replayed.Accepted || !replayed.WorkComplete || replayed.SubmissionKey != receipt.SubmissionKey || !replayed.ConsumerPending || replayed.ConsumerOwner != "main" || replayed.CurrentStage != "S4" {
		t.Fatalf("historical completion or pending Main registration was lost: %+v %v", replayed, err)
	}
	after := stageSnapshot(t, root)
	current, found, err := runtime.FindSubmission(after.State, p.ID)
	if err != nil || !found || current.FollowupID != original.FollowupID || current.ControlStatus == "advanced" {
		t.Fatalf("changed TASK replaced or advanced the original followup: %+v %v", current, err)
	}
	_, err = transition.Apply(root, s.StatePath, s.JournalPath, transition.Request{
		TransitionID: "TR-002", Actor: "orchestrator", ExpectedRevision: after.Revision,
		SubmissionFollowupID: current.FollowupID, Files: s.Files,
	})
	if err == nil || !strings.Contains(err.Error(), path) || stageSnapshot(t, root).Revision != after.Revision {
		t.Fatalf("original registration must reject the exact changed TASK without a write: %v", err)
	}
	beforeEntities := before.State["entities"].(map[string]any)
	afterEntities := after.State["entities"].(map[string]any)
	if len(beforeEntities["submissions"].([]any)) != len(afterEntities["submissions"].([]any)) || len(before.State["evidence"].([]any)) != len(after.State["evidence"].([]any)) {
		t.Fatal("same report was received again")
	}
	afterAnswer, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil || !bytes.Equal(beforeAnswer, afterAnswer) {
		t.Fatalf("professional answer changed: %v", err)
	}
}
