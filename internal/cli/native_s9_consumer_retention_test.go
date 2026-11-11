package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/diagnosticstorage"
	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS9ConsumerCauseSurvivesPrerequisiteAndConcurrentPublication(t *testing.T) {
	root := t.TempDir()
	snap := attemptDeliverySnapshot()
	publish := func(consumer bool, message string) error {
		a := newNativeS9AttemptCapture(t.Context(), root, snap, "plan-original", "builder-original", "Builder")
		a.SetInput(filepath.Join(root, "lost-private-result.json"))
		if consumer {
			a.EnterConsumer("typed_result_consumer")
		}
		return a.Publish(t.Context(), errors.New(message))
	}
	if err := publish(true, "before_fix_results got 3 want 6"); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() { defer group.Done(); failures <- publish(false, "source path unavailable") }()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, "plan-original")
	if !ok || got.ConsumerFailure == nil || !strings.Contains(got.ConsumerFailure.Message, "got 3 want 6") || !strings.Contains(got.Message, "source path unavailable") {
		t.Fatalf("lost consumer cause: %+v", got)
	}
	if !diagnosticstorage.Recognizes(t.Context(), root, nativeS9DiagnosticPath("plan-original")) || !diagnosticstorage.Recognizes(t.Context(), root, ".claude/diagnostics/native-s9/.publish.lock") {
		t.Fatal("bounded producer artifacts not recognized")
	}
	if err := publish(true, "new original consumer rejection"); err != nil {
		t.Fatal(err)
	}
	got, ok = nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, "plan-original")
	if !ok || got.ConsumerFailure != nil || strings.Contains(got.Message, "got 3 want 6") {
		t.Fatalf("new consumer duplicated old cause: %+v", got)
	}
}

func TestNativeS9PublishLockHonorsOriginalDeadline(t *testing.T) {
	root := t.TempDir()
	release, err := filelock.Acquire(t.Context(), filepath.Join(root, ".claude/diagnostics/native-s9/.publish.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	a := newNativeS9AttemptCapture(t.Context(), root, attemptDeliverySnapshot(), "plan-original", "builder-original", "Builder")
	if err := a.Publish(ctx, errors.New("actual error")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock escaped deadline: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, nativeS9DiagnosticPath("plan-original"))); !os.IsNotExist(err) {
		t.Fatalf("published after deadline: %v", err)
	}
}

// A real professional rejection must survive platform cleanup and a failed
// technical retry, and reach the next Main model through all existing outlets.
func TestNativeS9OriginalStopCauseReachesMainAfterSourceCleanup(t *testing.T) {
	root, worker, plan, input, answer := nativeS9WorkerFixture(t, 2)
	if len(answer.BeforeFixResults) != 6 {
		t.Fatalf("original contract has %d assertions, want 6", len(answer.BeforeFixResults))
	}
	// L4-hook-acceptance-checklist.md:53/:59 and L3-S9-repair.md:489 removed the
	// per-assertion coverage machine gate, so three of six assertions are no
	// longer a rejection reason. The surviving necessary check is the direct
	// structure of the author's own fields, so the professional rejection this
	// test follows now comes from an unusable assertion ID.
	answer.BeforeFixResults = answer.BeforeFixResults[:3]
	answer.BeforeFixResults[0].AssertionID = ""
	flat := nativeS9TestResultPath(t, root, plan)
	path := filepath.Join(worker, flat)
	if err := persistRepairLeadQuestionnaire(path, answer); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	var diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); !handled || code != 0 {
		t.Fatalf("original Stop: %v %d %s", handled, code, &diagnostic)
	}
	if !strings.Contains(diagnostic.String(), "before_fix_results") || !strings.Contains(diagnostic.String(), "assertion_id") {
		t.Fatalf("original consumer did not reject the unusable assertion: %s", &diagnostic)
	}
	// Keep the actual rejected responsibility and live source, but publish a
	// later prerequisite failure through the original diagnostic producer.
	snap := repairDispatchSnapshot(t, root)
	a := newNativeS9AttemptCapture(t.Context(), root, snap, plan.ID, plan.Binding.OwnerAgentID, "Builder")
	a.SetInput(path)
	if err := a.Publish(t.Context(), errors.New("later source prerequisite unavailable")); err != nil {
		t.Fatal(err)
	}
	handoff := s9HandoffForBinding(t.Context(), root, snap, plan.Binding)
	if handoff.Plan.id != plan.ID || !handoff.PlanAccepted || handoff.ConsumerFailure == nil || handoff.Attempt == nil || handoff.Attempt.FailureKind == "author" {
		t.Fatalf("lost original accepted Plan/rejected Result coordinates: %+v", handoff)
	}
	if handoff.ResultFact.kind == "candidate" || handoff.ResultFact.kind == "result" {
		t.Fatal("professional rejection manufactured delivery")
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/loop-state.json.commit-pending.json")); !os.IsNotExist(err) {
		t.Fatalf("author correction is not a pending transaction: %v", err)
	}
	packets, err := runtime.CurrentRepairResultPacketsContext(t.Context(), root, snap.State, plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 || packets[0].Binding.Sources.RepairAssignmentID != plan.Binding.RepairAssignmentID {
		t.Fatalf("missing original Result responsibility: %+v %v", packets, err)
	}
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil {
		t.Fatalf("missing original execution: %v", err)
	}
	execution := registry.Executions[plan.Binding.ManifestAssignmentID]
	if execution.DeliveryRef != "" {
		t.Fatal("rejected Result acquired Candidate")
	}

	action := s9UnreceivedActionContext(t.Context(), root, handoff)
	if !strings.Contains(action, "original author may correct") || !strings.Contains(action, path) || strings.Contains(action, "stage-answer consume --root") {
		t.Fatalf("weak prerequisite erased original author direction: %s", action)
	}
	if err := os.RemoveAll(worker); err != nil {
		t.Fatal(err)
	}
	service, err := submission.NewReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, handled, err := consumeNativeS9Original(t.Context(), service, path); !handled || err == nil {
		t.Fatalf("lost source retry: handled=%v err=%v", handled, err)
	}
	snap = repairDispatchSnapshot(t, root)
	got, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, plan.ID)
	if !ok || got.ConsumerFailure == nil || !strings.Contains(got.ConsumerFailure.Message, "before_fix_results") {
		t.Fatalf("weak retry erased original consumer rejection: %+v", got)
	}
	brief := nativeS9BriefPath(plan)
	texts := []string{
		nativeExecutionMainOutput(t, root, filepath.Join(root, brief), map[string]any{"status": "completed"}),
		currentQuestionnaireLifecycleContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root}),
	}
	var out, integrate bytes.Buffer
	if code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", plan.Binding.ManifestAssignmentID, "--agent", plan.Binding.OwnerAgentID}, &out, &integrate); code == 0 {
		t.Fatal("missing candidate integrated")
	}
	texts = append(texts, integrate.String())
	for _, text := range texts {
		for _, want := range []string{"before_fix_results", "assertion_id", plan.Binding.OwnerAgentID, "PlanReport receipt is accepted", "observed then", "unavailable"} {
			if !strings.Contains(text, want) {
				t.Errorf("Main outlet missing %q: %s", want, text)
			}
		}
		if strings.Contains(text, "stage-answer consume --root") {
			t.Errorf("unavailable source offered invalid consume: %s", text)
		}
	}
}

func TestNativeS9AvailableFlatInputDoesNotHideMissingGitSource(t *testing.T) {
	root, worker, plan, _, _ := nativeS9WorkerFixture(t)
	input := filepath.Join(worker, nativeS9TestResultPath(t, root, plan))
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	snap := repairDispatchSnapshot(t, root)
	a := newNativeS9AttemptCapture(t.Context(), root, snap, plan.ID, plan.Binding.OwnerAgentID, "Builder")
	a.SetInput(input)
	if err := a.Publish(t.Context(), errors.New("original source gap")); err != nil {
		t.Fatal(err)
	}
	handoff := s9HandoffForBinding(t.Context(), root, snap, plan.Binding)
	if handoff.Plan.id != plan.ID || handoff.Attempt == nil || handoff.Attempt.InputRef != input {
		t.Fatalf("missing original producer coordinates: %+v", handoff)
	}
	gitPath := filepath.Join(worker, ".git")
	backup := filepath.Join(t.TempDir(), "original-git")
	if err := os.Rename(gitPath, backup); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Rename(backup, gitPath); err != nil {
			t.Error(err)
		}
	})
	before := repairDispatchRuntimeBytes(t, root)
	got := s9UnreceivedActionContext(t.Context(), root, handoff)
	if !strings.Contains(got, "Git source is unavailable at "+worker) || strings.Contains(got, s9OriginalConsumeCommand(root, input)) {
		t.Fatalf("offered recovery after required source disappeared: %s", got)
	}
	repairDispatchAssertUnchanged(t, root, before)
	if raw, err := os.ReadFile(input); err != nil || !bytes.Equal(raw, original) {
		t.Fatalf("original readable input changed: %q %v", raw, err)
	}
}

func TestNativeS9SuccessfulConsumerDoesNotPromoteLaterPrerequisiteFailure(t *testing.T) {
	root := t.TempDir()
	snap := attemptDeliverySnapshot()
	a := newNativeS9AttemptCapture(t.Context(), root, snap, "plan-original", "builder-original", "Builder")
	a.EnterConsumer("flat_result_consumer")
	a.ConsumerSucceeded()
	if err := a.Publish(t.Context(), errors.New("later receipt read unavailable")); err != nil {
		t.Fatal(err)
	}
	next := newNativeS9AttemptCapture(t.Context(), root, snap, "plan-original", "builder-original", "Builder")
	if err := next.Publish(t.Context(), errors.New("later source unavailable")); err != nil {
		t.Fatal(err)
	}
	got, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, "plan-original")
	if !ok || got.ConsumerFailure != nil {
		t.Fatalf("post-consumer prerequisite promoted to consumer cause: %+v", got)
	}
}
