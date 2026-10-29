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
	"github.com/entroforge/go-system-builder/internal/submission"
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
	answer.BeforeFixResults = answer.BeforeFixResults[:3]
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
	if !strings.Contains(diagnostic.String(), "before_fix_results") || !strings.Contains(diagnostic.String(), "got 3 want 6") {
		t.Fatalf("original consumer did not reject incomplete assertions: %s", &diagnostic)
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
	snap := repairDispatchSnapshot(t, root)
	got, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, plan.ID)
	if !ok || got.ConsumerFailure == nil || !strings.Contains(got.ConsumerFailure.Message, "before_fix_results") {
		t.Fatalf("weak retry erased original consumer rejection: %+v", got)
	}
	brief, _, _ := nativeS9Paths(plan)
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
		for _, want := range []string{"before_fix_results", "got 3 want 6", plan.Binding.OwnerAgentID, "PlanReport receipt is accepted", "observed then", "unavailable"} {
			if !strings.Contains(text, want) {
				t.Errorf("Main outlet missing %q: %s", want, text)
			}
		}
		if strings.Contains(text, "stage-answer consume --root") {
			t.Errorf("unavailable source offered invalid consume: %s", text)
		}
	}
}

func TestNativeS9RetainedProfessionalCauseDoesNotBecomeTechnicalReplay(t *testing.T) {
	root := t.TempDir()
	worker := t.TempDir()
	if err := os.WriteFile(filepath.Join(worker, ".git"), []byte("gitdir: original"), 0600); err != nil {
		t.Fatal(err)
	}
	relative := filepath.Join(".claude", "submissions", "native-s9", "original-plan", "result.json")
	input := filepath.Join(worker, relative)
	if err := os.MkdirAll(filepath.Dir(input), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte("original professional input"), 0600); err != nil {
		t.Fatal(err)
	}
	handoff := s9HandoffProjection{Native: true, Plan: s9ListenerQuestionnaire{id: "original-plan"}, Diagnostic: "preserved count mismatch", Attempt: &nativeS9Attempt{InputRef: input, FailureKind: "unknown"}, ConsumerFailure: &nativeS9ConsumerFailure{Attempt: &nativeS9Attempt{InputRef: input, FailureKind: "author"}}}
	got := s9UnreceivedActionContext(t.Context(), root, handoff)
	if !strings.Contains(got, "original author may correct") || strings.Contains(got, s9OriginalConsumeCommand(root, input)) {
		t.Fatalf("prerequisite attempt erased author direction: %s", got)
	}
}

func TestNativeS9AvailableFlatInputDoesNotHideMissingGitSource(t *testing.T) {
	root := t.TempDir()
	worker := t.TempDir()
	relative := filepath.Join(".claude", "submissions", "native-s9", "original-plan", "result.json")
	input := filepath.Join(worker, relative)
	if err := os.MkdirAll(filepath.Dir(input), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte("original input still exists"), 0600); err != nil {
		t.Fatal(err)
	}
	handoff := s9HandoffProjection{Native: true, Plan: s9ListenerQuestionnaire{id: "original-plan"}, Diagnostic: "original source gap", Attempt: &nativeS9Attempt{InputRef: input}}
	got := s9UnreceivedActionContext(t.Context(), root, handoff)
	if !strings.Contains(got, "Git source is unavailable") || strings.Contains(got, s9OriginalConsumeCommand(root, input)) {
		t.Fatalf("offered recovery after required source disappeared: %s", got)
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
