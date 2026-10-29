package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/transition"
)

const (
	canonicalRuntimeID  = "loop-s6-main-fixture"
	canonicalTeamID     = "workgroup-s6-main-fixture"
	canonicalAgentID    = "agent-s6-main-fixture"
	canonicalAssignment = "assignment-s6-main-fixture"
	canonicalTaskID     = "TASK-001-01"
	canonicalManifest   = ".claude/workgroups/manifest-s6-main-fixture.json"
	canonicalTask       = "docs/dev/tasks/TASK-001-01.md"
	canonicalPlanRef    = ".claude/evidence/workgroup-s6-main-fixture-round-1/TASK-001-01/plan-report.json"
	canonicalProofRef   = "src/pkg/proof.txt"
	canonicalCheckFile  = "tools/main-check.sh"
	canonicalSourceFile = "src/pkg/behavior.txt"
	canonicalBranch     = "s6-main-fixture-worker"
)

type canonicalMainFixture struct {
	hookBinary  string // Set only for production-process Hook acceptance tests.
	root        string
	worker      string
	statePath   string
	journalPath string
	counterPath string
	failPath    string
	command     string
	record      assignmentrecord.Record
	planPath    string
	result      assignment.ResultAcceptanceReceipt
}

func TestCanonicalMainIntegrationUsesOriginalResultAndRealMainFacts(t *testing.T) {
	ctx := context.Background()
	fixture := newCanonicalMainFixture(t, canonicalSourceFile)
	assertCanonicalQualitygateState(t, fixture, false, qualitygate.StatusNotReady)
	inspection := fixture.inspect(t, "")
	if !inspection.Ready || len(inspection.ChangedPaths) != 1 || inspection.ChangedPaths[0] != canonicalSourceFile {
		t.Fatalf("initial canonical Inspect = ready %t paths %v blockers %v", inspection.Ready, inspection.ChangedPaths, inspection.Blockers)
	}

	// A stale Assignment revision projection cannot authorize the check or merge.
	staleRevision := inspection
	staleProjection := staleRevision.AssignmentProjection
	staleProjection.AssignmentRevision++
	staleRevision.AssignmentProjection = staleProjection
	if _, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: staleRevision}, fixture.integrateConfig()); err == nil {
		t.Fatal("stale Assignment revision projection was accepted")
	}
	if got := readGit(t, fixture.root, "rev-parse", "main"); got != inspection.TargetHead {
		t.Fatalf("stale revision moved Main from %s to %s", inspection.TargetHead, got)
	}
	if count := fileSize(t, fixture.counterPath); count != 0 {
		t.Fatalf("stale revision ran the Main check %d times", count)
	}

	// Caller-supplied PASS rows and a fake runner are ignored for v1.2. The
	// original Record's executable RequiredCheck must run after the Git merge.
	forged := inspection
	forged.RequiredChecks = []integration.CheckResult{{Command: fixture.command, Status: "pass"}}
	first, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: forged}, integration.IntegrateConfig{
		Root: fixture.root, GitRoot: fixture.root, RuntimeID: canonicalRuntimeID,
		RequiredChecks: []string{"caller-forged-pass"},
		CheckRunner:    func(context.Context, string, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("canonical Main Integrate: %v; Main status=%q", err, readGit(t, fixture.root, "status", "--porcelain", "-z", "--untracked-files=all"))
	}
	if first.Checkpoint.State != integration.StateVerified || first.Checkpoint.CanonicalMainFacts == nil {
		t.Fatalf("initial Main checkpoint = state %q facts %+v", first.Checkpoint.State, first.Checkpoint.CanonicalMainFacts)
	}
	if count := fileSize(t, fixture.counterPath); count != 1 {
		t.Fatalf("canonical Main RequiredCheck ran %d times, want once despite forged PASS", count)
	}
	assertActualCanonicalMainFacts(t, fixture, first.Checkpoint, 1, inspection.SourceHead, inspection.MergeBase, inspection.TargetHead)
	assertCanonicalQualitygateState(t, fixture, true, qualitygate.StatusSatisfied)
	merge := first.Checkpoint.MergeCommit
	assertTwoParentMerge(t, fixture.root, merge, inspection.TargetHead, inspection.SourceHead)

	// Repeating the exact accepted binding and inspection reuses its complete
	// checkpoint facts without another command or merge.
	repeated, err := integration.Integrate(ctx, integration.IntegrateRequest{
		Inspection: inspection, ExpectedRevision: first.Checkpoint.Revision,
	}, fixture.integrateConfig())
	if err != nil || !repeated.Reused {
		t.Fatalf("idempotent Main integration = reused %t err %v", repeated.Reused, err)
	}
	if count := fileSize(t, fixture.counterPath); count != 1 || repeated.Checkpoint.MergeCommit != merge || !reflect.DeepEqual(repeated.Checkpoint.CanonicalMainFacts, first.Checkpoint.CanonicalMainFacts) {
		t.Fatalf("idempotent call changed Main facts/merge or ran checks again: count=%d merge=%s facts=%+v", count, repeated.Checkpoint.MergeCommit, repeated.Checkpoint.CanonicalMainFacts)
	}

	// The original CompleteTask writer accepts a distinct professional Answer
	// under the same Record revision. An old inspection cannot bind it.
	secondResult := fixture.acceptResult(t, "msg-s6-main-result-2", "A later professional answer with distinct evidence.")
	if secondResult.ResultRevision != 2 || secondResult.AnswerDigest == fixture.result.AnswerDigest {
		t.Fatalf("second original Result receipt = %+v, first = %+v", secondResult, fixture.result)
	}
	assertCanonicalQualitygateState(t, fixture, false, qualitygate.StatusNotReady)
	if _, err := integration.Integrate(ctx, integration.IntegrateRequest{
		Inspection: inspection, ExpectedRevision: first.Checkpoint.Revision,
	}, fixture.integrateConfig()); err == nil {
		t.Fatal("inspection bound to the previous Result was accepted after a newer original Result")
	}
	if count := fileSize(t, fixture.counterPath); count != 1 {
		t.Fatalf("stale Result binding ran a Main check; count=%d", count)
	}

	// The regular post-merge Inspect observes no new commits. The Integrator
	// must revalidate the original merge receipt, retain its diff denominator,
	// and rerun every current Main RequiredCheck for the new Result.
	secondInspection := fixture.inspect(t, "")
	if secondInspection.Ready || !reflect.DeepEqual(secondInspection.Blockers, []string{integration.ErrMissingCommits.Error()}) {
		t.Fatalf("post-merge Inspect = ready %t blockers %v", secondInspection.Ready, secondInspection.Blockers)
	}
	second, err := integration.Integrate(ctx, integration.IntegrateRequest{
		Inspection: secondInspection, ExpectedRevision: first.Checkpoint.Revision,
	}, fixture.integrateConfig())
	if err != nil {
		t.Fatalf("reverify new original Result against recorded merge: %v", err)
	}
	if second.Checkpoint.State != integration.StateVerified || second.Checkpoint.MergeCommit != merge {
		t.Fatalf("new Result re-verification changed merge/state: %s / %s", second.Checkpoint.State, second.Checkpoint.MergeCommit)
	}
	if count := fileSize(t, fixture.counterPath); count != 2 {
		t.Fatalf("new Result Main checks ran %d times cumulatively, want exactly two", count)
	}
	assertActualCanonicalMainFacts(t, fixture, second.Checkpoint, 2, inspection.SourceHead, inspection.MergeBase, inspection.TargetHead)
	assertCanonicalQualitygateState(t, fixture, true, qualitygate.StatusSatisfied)
	assertTwoParentMerge(t, fixture.root, merge, inspection.TargetHead, inspection.SourceHead)
	if second.Checkpoint.CanonicalMainFacts.Binding.ResultRevision != 2 || second.Checkpoint.CanonicalMainFacts.Binding.AnswerDigest != secondResult.AnswerDigest {
		t.Fatalf("Main facts did not move to the new accepted Result binding: %+v", second.Checkpoint.CanonicalMainFacts.Binding)
	}

	// An explicit fresh recovery inspection makes exact post-merge retries
	// idempotent while preserving the original source and changed-path set.
	finalInspection := fixture.inspect(t, second.Checkpoint.MergeBase)
	if !finalInspection.Ready {
		t.Fatalf("fresh recovery Inspect not ready: %v", finalInspection.Blockers)
	}
	finalRetry, err := integration.Integrate(ctx, integration.IntegrateRequest{
		Inspection: finalInspection, ExpectedRevision: second.Checkpoint.Revision,
	}, fixture.integrateConfig())
	if err != nil || !finalRetry.Reused {
		t.Fatalf("fresh exact re-verification retry = reused %t err %v", finalRetry.Reused, err)
	}
	if count := fileSize(t, fixture.counterPath); count != 2 || finalRetry.Checkpoint.MergeCommit != merge || !reflect.DeepEqual(finalRetry.Checkpoint.CanonicalMainFacts, second.Checkpoint.CanonicalMainFacts) {
		t.Fatalf("exact Result retry changed verified facts or reran checks: count=%d facts=%+v", count, finalRetry.Checkpoint.CanonicalMainFacts)
	}
}

func TestCanonicalMainIntegrationRejectsMissingProofAndRequiresFreshSuccessfulCheck(t *testing.T) {
	ctx := context.Background()
	fixture := newCanonicalMainFixture(t, canonicalSourceFile)
	inspection := fixture.inspect(t, "")
	first, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig())
	if err != nil || first.Checkpoint.State != integration.StateVerified {
		t.Fatalf("initial integration = state %q err %v", first.Checkpoint.State, err)
	}
	if err := corruptCanonicalMainProof(t, fixture); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.failPath, []byte("fail Main check\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Missing physical receipt/log bytes and blank digests invalidate the old
	// verified facts. Integrate retries the original Main command; the induced
	// failure must preserve the merge without restoring StateVerified.
	recovery := fixture.inspect(t, first.Checkpoint.MergeBase)
	if !recovery.Ready {
		t.Fatalf("recovery Inspect = blockers %v", recovery.Blockers)
	}
	failed, err := integration.Integrate(ctx, integration.IntegrateRequest{
		Inspection: recovery, ExpectedRevision: first.Checkpoint.Revision,
	}, fixture.integrateConfig())
	if err == nil || failed.Checkpoint.State == integration.StateVerified {
		t.Fatalf("missing Main evidence was reused: state=%q err=%v", failed.Checkpoint.State, err)
	}
	stored, found, loadErr := integration.DefaultCheckpointStore().Load(integration.CheckpointPath(fixture.root, canonicalRuntimeID, 1, canonicalAssignment))
	if loadErr != nil || !found || stored.State != integration.StatePreserved || stored.MergeCommit != first.Checkpoint.MergeCommit || len(stored.CheckReceipts) != 1 {
		t.Fatalf("failed fresh Main recheck checkpoint = found %t state %q merge %q receipts %d err %v", found, stored.State, stored.MergeCommit, len(stored.CheckReceipts), loadErr)
	}
	var failedReceipt struct {
		Status  string `json:"status"`
		Exit    *int   `json:"exit_code"`
		Binding struct {
			Owner          string `json:"owner"`
			Responsibility string `json:"responsibility"`
		} `json:"check_binding"`
	}
	receiptBytes, readErr := os.ReadFile(stored.CheckReceipts[0])
	if readErr != nil || json.Unmarshal(receiptBytes, &failedReceipt) != nil || failedReceipt.Status != "fail" || failedReceipt.Exit == nil || *failedReceipt.Exit == 0 || failedReceipt.Binding.Owner != "Main" || failedReceipt.Binding.Responsibility != "main_required_integration" {
		t.Fatalf("fresh failing Main receipt = %+v read error %v bytes %s", failedReceipt, readErr, receiptBytes)
	}

	if err := os.Remove(fixture.failPath); err != nil {
		t.Fatal(err)
	}
	secondRecovery := fixture.inspect(t, stored.MergeBase)
	if !secondRecovery.Ready {
		t.Fatalf("successful retry Inspect = blockers %v", secondRecovery.Blockers)
	}
	verified, err := integration.Integrate(ctx, integration.IntegrateRequest{
		Inspection: secondRecovery, ExpectedRevision: stored.Revision, RetryPreserved: true,
	}, fixture.integrateConfig())
	if err != nil || verified.Checkpoint.State != integration.StateVerified || verified.Checkpoint.MergeCommit != first.Checkpoint.MergeCommit {
		t.Fatalf("fresh successful Main recheck = state %q merge %q err %v", verified.Checkpoint.State, verified.Checkpoint.MergeCommit, err)
	}
	if count := fileSize(t, fixture.counterPath); count != 2 {
		t.Fatalf("initial and retried successful Main commands should update the counter; got %d", count)
	}
	assertActualCanonicalMainFacts(t, fixture, verified.Checkpoint, 1, inspection.SourceHead, inspection.MergeBase, inspection.TargetHead)
	if verified.Checkpoint.CanonicalMainFacts.Checks[0].ReceiptRef == "" || verified.Checkpoint.CanonicalMainFacts.Checks[0].LogRef == "" {
		t.Fatal("fresh proof did not replace missing receipt/log references")
	}
}

func TestCanonicalMainIntegrationBlocksActualOutOfScopeGitDelta(t *testing.T) {
	ctx := context.Background()
	fixture := newCanonicalMainFixture(t, "docs/unowned.txt")
	inspection := fixture.inspect(t, "")
	if inspection.Ready || !reflect.DeepEqual(inspection.OutOfScopeDiff, []string{"docs/unowned.txt"}) {
		t.Fatalf("out-of-scope Inspect = ready %t diff %v blockers %v", inspection.Ready, inspection.OutOfScopeDiff, inspection.Blockers)
	}
	before := readGit(t, fixture.root, "rev-parse", "main")
	if _, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig()); err == nil {
		t.Fatal("out-of-scope actual Git delta reached Main integration")
	}
	if after := readGit(t, fixture.root, "rev-parse", "main"); after != before {
		t.Fatalf("out-of-scope delta moved target from %s to %s", before, after)
	}
	if count := fileSize(t, fixture.counterPath); count != 0 {
		t.Fatalf("out-of-scope delta ran Main checks %d times", count)
	}
}

func TestCanonicalClosingContractBlocksNewUnclosedResultAndPreservesMainHistory(t *testing.T) {
	ctx := context.Background()
	fixture := newCanonicalMainFixtureWithClosing(t, canonicalSourceFile, true, false)
	initialInspection := fixture.inspect(t, "")
	if !initialInspection.Ready {
		t.Fatalf("complete closing Result initial Inspect blockers %v", initialInspection.Blockers)
	}
	merged, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: initialInspection}, fixture.integrateConfig())
	if err != nil || merged.Checkpoint.State != integration.StateVerified || merged.Checkpoint.CanonicalMainFacts == nil {
		t.Fatalf("complete closing Result Main merge/checkpoint = state %q err %v", merged.Checkpoint.State, err)
	}
	merge := merged.Checkpoint.MergeCommit
	mainHead := readGit(t, fixture.root, "rev-parse", "main")
	if mainHead != merge || fileSize(t, fixture.counterPath) != 1 {
		t.Fatalf("initial closing Main verification head=%s merge=%s checks=%d", mainHead, merge, fileSize(t, fixture.counterPath))
	}
	assertTwoParentMerge(t, fixture.root, merge, initialInspection.TargetHead, initialInspection.SourceHead)
	assertActualCanonicalMainFacts(t, fixture, merged.Checkpoint, 1, initialInspection.SourceHead, initialInspection.MergeBase, initialInspection.TargetHead)
	completeReceipt, completeGate := currentCanonicalReceiptAndGate(t, fixture)
	if !completeReceipt.Accepted || !completeReceipt.Integrated || !completeReceipt.WorkComplete || completeReceipt.ProfessionalStatus != "complete" || completeGate.Status != qualitygate.StatusSatisfied {
		t.Fatalf("fully closed owner plus original Main facts receipt/gate = accepted/integrated/work_complete/professional %t/%t/%t/%q gate=%q missing=%v",
			completeReceipt.Accepted, completeReceipt.Integrated, completeReceipt.WorkComplete, completeReceipt.ProfessionalStatus, completeGate.Status, completeGate.Missing)
	}
	originalFacts := *merged.Checkpoint.CanonicalMainFacts

	for _, tc := range []struct {
		name         string
		disposition  string
		risks        []string
		deviations   []string
		wantBlockers []string
	}{
		{name: "incomplete assessment", disposition: "incomplete", wantBlockers: []string{`S6 closing pending: owner closing_assessment is "incomplete"`}},
		{name: "declared risk", disposition: "complete", risks: []string{"a required behavior remains uncertain"}, wantBlockers: []string{"S6 closing pending: owner Result declares risks or blockers"}},
		{name: "scope deviation", disposition: "complete", deviations: []string{"an unapproved path was changed"}, wantBlockers: []string{"S6 closing pending: owner Result declares scope deviations"}},
		{name: "multiple closure blockers", disposition: "complete", risks: []string{"a required behavior remains uncertain"}, deviations: []string{"an unapproved path was changed"}, wantBlockers: []string{"S6 closing pending: owner Result declares risks or blockers", "S6 closing pending: owner Result declares scope deviations"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accepted := fixture.acceptResultWithClosing(t, "msg-s6-closing-"+strings.ReplaceAll(tc.name, " ", "-"), "The owner records the current professional assessment.", tc.disposition, tc.risks, tc.deviations)
			if accepted.ResultRevision < 2 || accepted.AnswerDigest == "" {
				t.Fatalf("new owner Result receipt = %+v", accepted)
			}
			inspection := fixture.inspect(t, "")
			if inspection.Ready || !reflect.DeepEqual(inspection.Blockers, tc.wantBlockers) || inspection.CanonicalResult == nil || inspection.CanonicalResult.ResultRevision != accepted.ResultRevision {
				t.Fatalf("unclosed current Result Inspect ready=%t blockers=%v binding=%+v", inspection.Ready, inspection.Blockers, inspection.CanonicalResult)
			}

			// Simulate a stale caller trying to reuse the previously ready path and
			// acknowledge the old merge. Integrate must re-resolve the current
			// Result and block before checks, acknowledgement, or cleanup.
			inspection.Ready = true
			inspection.Blockers = nil
			blocked, integrateErr := integration.Integrate(ctx, integration.IntegrateRequest{
				Inspection: inspection, ExpectedRevision: merged.Checkpoint.Revision, Acknowledge: true, Cleanup: true,
			}, fixture.integrateConfig())
			if !errors.Is(integrateErr, integration.ErrClosingContractPending) || blocked.Checkpoint.State != integration.StatePreserved {
				t.Fatalf("unclosed Result reached Main/ACK/Cleanup: state=%q reused=%t err=%v", blocked.Checkpoint.State, blocked.Reused, integrateErr)
			}
			if blocked.Checkpoint.ResumeState != integration.StateMerged || blocked.Checkpoint.MergeCommit != merge || blocked.Checkpoint.CanonicalMainFacts == nil || !reflect.DeepEqual(*blocked.Checkpoint.CanonicalMainFacts, originalFacts) || !containsEveryString(blocked.Checkpoint.Blockers, tc.wantBlockers) {
				t.Fatalf("blocked Result did not preserve old Main history as history: state=%s resume=%s merge=%s facts=%+v blockers=%v", blocked.Checkpoint.State, blocked.Checkpoint.ResumeState, blocked.Checkpoint.MergeCommit, blocked.Checkpoint.CanonicalMainFacts, blocked.Checkpoint.Blockers)
			}
			if len(tc.wantBlockers) > 1 {
				repeated, repeatErr := integration.Integrate(ctx, integration.IntegrateRequest{
					Inspection: inspection, ExpectedRevision: blocked.Checkpoint.Revision, Acknowledge: true,
				}, fixture.integrateConfig())
				if !errors.Is(repeatErr, integration.ErrClosingContractPending) || !repeated.Reused || repeated.Checkpoint.Revision != blocked.Checkpoint.Revision {
					t.Fatalf("same multiple closure blockers were not idempotent: reused=%t revision=%d/%d err=%v", repeated.Reused, repeated.Checkpoint.Revision, blocked.Checkpoint.Revision, repeatErr)
				}
			}
			if readGit(t, fixture.root, "rev-parse", "main") != merge || fileSize(t, fixture.counterPath) != 1 {
				t.Fatalf("blocked Result moved Main or reran checks: head=%s count=%d", readGit(t, fixture.root, "rev-parse", "main"), fileSize(t, fixture.counterPath))
			}
			receipt, evaluation := currentCanonicalReceiptAndGate(t, fixture)
			if !receipt.Accepted || receipt.Integrated || receipt.WorkComplete || receipt.ProfessionalStatus != "incomplete" {
				t.Fatalf("unclosed Result builder receipt accepted/integrated/work_complete/professional=%t/%t/%t/%q pending=%v", receipt.Accepted, receipt.Integrated, receipt.WorkComplete, receipt.ProfessionalStatus, receipt.OwnPending)
			}
			if evaluation.Status != qualitygate.StatusNotReady || !containsString(evaluation.Missing, "integration_checkpoint:"+fixture.record.Task.ID+":canonical result/integration not verified") {
				t.Fatalf("unclosed Result TR-006 Gate = %q missing %v conflicts %v", evaluation.Status, evaluation.Missing, evaluation.Conflicts)
			}
		})
	}
}

func TestCanonicalClosingContractWithWorkerChecksCannotUseMainReceiptsAsOwnerProof(t *testing.T) {
	ctx := context.Background()
	fixture := newCanonicalMainFixtureWithClosing(t, canonicalSourceFile, true, true)
	if len(fixture.record.ClosingContract.WorkerChecks) != 1 || len(fixture.record.CheckProfiles) != 1 || fixture.record.CheckProfiles[0].Location != "main" {
		t.Fatalf("fixture did not separate owner checks from Main RequiredChecks: closing=%+v MainProfiles=%+v", fixture.record.ClosingContract, fixture.record.CheckProfiles)
	}
	mainBefore := readGit(t, fixture.root, "rev-parse", "main")
	inspection := fixture.inspect(t, "")
	want := "S6 closing pending: owned_check_evidence_unavailable"
	if inspection.Ready || !reflect.DeepEqual(inspection.Blockers, []string{want}) {
		t.Fatalf("worker-check closure Inspect ready=%t blockers=%v", inspection.Ready, inspection.Blockers)
	}
	inspection.Ready = true
	inspection.Blockers = nil
	blocked, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspection, Acknowledge: true}, fixture.integrateConfig())
	if !errors.Is(err, integration.ErrClosingContractPending) || blocked.Checkpoint.State != integration.StatePreserved || blocked.Checkpoint.MergeCommit != "" || !containsString(blocked.Checkpoint.Blockers, want) {
		t.Fatalf("Main receipts were substituted for missing owner-check evidence: state=%q merge=%q blockers=%v err=%v", blocked.Checkpoint.State, blocked.Checkpoint.MergeCommit, blocked.Checkpoint.Blockers, err)
	}
	if readGit(t, fixture.root, "rev-parse", "main") != mainBefore || fileSize(t, fixture.counterPath) != 0 {
		t.Fatalf("pending owner checks moved Main or ran Main RequiredChecks: head=%s count=%d", readGit(t, fixture.root, "rev-parse", "main"), fileSize(t, fixture.counterPath))
	}
	receipt, evaluation := currentCanonicalReceiptAndGate(t, fixture)
	if !receipt.Accepted || receipt.Integrated || receipt.WorkComplete || receipt.ProfessionalStatus == "complete" || !containsString(receipt.OwnPending, "owned_check_evidence_unavailable:0") {
		t.Fatalf("missing owner check proof builder receipt = accepted/integrated/work_complete/professional %t/%t/%t/%q pending=%v", receipt.Accepted, receipt.Integrated, receipt.WorkComplete, receipt.ProfessionalStatus, receipt.OwnPending)
	}
	if evaluation.Status != qualitygate.StatusNotReady {
		t.Fatalf("missing owner check proof TR-006 Gate status = %q", evaluation.Status)
	}
}

func newCanonicalMainFixture(t *testing.T, changedPath string) *canonicalMainFixture {
	return newCanonicalMainFixtureWithClosing(t, changedPath, false, false)
}

func newCanonicalMainFixtureWithClosing(t *testing.T, changedPath string, closing, workerCheck bool) *canonicalMainFixture {
	return newCanonicalMainFixtureWithSetup(t, changedPath, closing, workerCheck, nil)
}

func newCanonicalMainFixtureWithSetup(t *testing.T, changedPath string, closing, workerCheck bool, beforeRegister func(t *testing.T, root, statePath, journalPath string)) *canonicalMainFixture {
	t.Helper()
	return newCanonicalMainFixtureWithWorkerSetup(t, changedPath, closing, workerCheck, beforeRegister, nil)
}

func newCanonicalMainFixtureWithWorkerSetup(t *testing.T, changedPath string, closing, workerCheck bool, beforeRegister func(t *testing.T, root, statePath, journalPath string), workerSetup func(t *testing.T, worker string)) *canonicalMainFixture {
	t.Helper()
	root := t.TempDir()
	counterDir := t.TempDir()
	counterPath := filepath.Join(counterDir, "main-check-count")
	failPath := filepath.Join(counterDir, "fail-main-check")
	if err := os.WriteFile(counterPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	command := "sh " + shellQuote(canonicalCheckFile) + " " + shellQuote(counterPath) + " " + shellQuote(failPath)
	profile := checkexec.Profile{
		ID: "profile-s6-main-integration", Version: "v1.0.0", CheckIndex: 0, Command: command,
		Location: "main", TimeoutSeconds: 60, EnvironmentPolicy: "inherited", Environment: []checkexec.EnvFact{},
		Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree",
		SideEffects: "unknown", ReusePolicy: "never",
	}
	copyFixtureTree(t, filepath.Join(sourceRepositoryRoot(t), "docs", "control"), filepath.Join(root, "docs", "control"))
	copyFixtureFile(t, filepath.Join(sourceRepositoryRoot(t), "agents", "backend-builder.md"), filepath.Join(root, ".claude", "agents", "backend-builder.md"))
	for _, skill := range catalog.Skills {
		copyFixtureFile(t, filepath.Join(sourceRepositoryRoot(t), "skills", skill.Name, "SKILL.md"), filepath.Join(root, ".claude", "skills", skill.Name, "SKILL.md"))
	}

	taskBytes := []byte(fmt.Sprintf("# %s: Main integration fixture\n\n> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-001\n> Dispatch policy: waves-v1\n> Primary contract: BE-001\n\n## Scope\n\n| Type | Paths |\n| --- | --- |\n| prospective write paths | `src/pkg` |\n", canonicalTaskID))
	writeFixtureFile(t, root, canonicalTask, taskBytes)
	index := []byte(`# REQ-001 dispatch plan

> REQ: REQ-001
> Dispatch policy: waves-v1
> Revision: 1
> Status: complete

## W1 · one current builder assignment

- [ ] [current task](TASK-001-01.md)
`)
	writeFixtureFile(t, root, "docs/dev/tasks/index-REQ-001.md", index)
	writeFixtureFile(t, root, canonicalProofRef, []byte("original scoped evidence source\n"))
	writeFixtureFile(t, root, "src/pkg/input.go", []byte("package pkg\n"))
	writeFixtureFile(t, root, canonicalCheckFile, []byte(`#!/bin/sh
set -eu
counter="$1"
fail="$2"
if [ -f "$fail" ]; then
  echo "injected check failure" >&2
  exit 17
fi
test -f src/pkg/behavior.txt
grep -q 'main-check-marker' src/pkg/behavior.txt
printf x >> "$counter"
`))
	writeFixtureFile(t, root, ".gitignore", []byte("/.claude/loop-state.json\n/.claude/loop-state.json.lock.process\n/.claude/loop-events.jsonl\n/.claude/loop-metrics.json\n/.claude/integration.lock\n/.claude/runtime-checkpoints/\n/.claude/evidence/\n/.claude/hook-metrics/\n"))
	writeFixtureFile(t, root, ".claude/loop-state.json", []byte(`{"bound_req":{"id":"REQ-001"}}`))

	assignmentManifest := map[string]any{
		"assignment_id": canonicalAssignment, "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": "backend-builder",
		"agent_id": canonicalAgentID, "agent_definition_ref": ".claude/agents/backend-builder.md",
		"skill_refs": []string{"backend-engineering"}, "read_paths": []string{"docs/dev/tasks/"},
		"scope": []string{"src/pkg"}, "write_paths": []string{"src/pkg"}, "output_paths": []string{},
		"depends_on": []string{}, "required_checks": []string{command}, "done_when": []string{"the real Main required check passes"},
		"dispatch_mode": "plan_checkpoint", "check_profiles": []checkexec.Profile{profile},
		"reuse_decision": "create", "grouping_rationale": "one complete S6 Main integration slice", "status": "planned",
	}
	if workerCheck {
		workerProfile := profile
		workerProfile.ID = "profile-s6-worker-owned"
		workerProfile.Command = "go test ./src/pkg"
		workerProfile.Location = "worker"
		assignmentManifest["integration_check_mode"] = "post_merge"
		assignmentManifest["closing_contract"] = map[string]any{"contract": assignmentrecord.ClosingContractVersion, "worker_checks": []any{
			map[string]any{"index": 0, "command": workerProfile.Command, "profile": workerProfile},
		}}
	} else if closing {
		// Empty owner-check sets still require the explicit, frozen contract.
		assignmentManifest["integration_check_mode"] = "post_merge"
		assignmentManifest["closing_contract"] = map[string]any{"contract": assignmentrecord.ClosingContractVersion, "worker_checks": []any{}}
	}
	manifest := map[string]any{
		"schema_version": "1.0.0", "assignment_record_contract": assignmentrecord.AssignmentRecordContract,
		"manifest_id": "team-manifest-s6-main-fixture", "version": "v1.0.0", "runtime_id": canonicalRuntimeID,
		"req_id": "REQ-001", "baseline_generation": 1, "review_round": 1,
		"platform_team_id": "platform-s6-main-fixture", "workgroup_id": canonicalTeamID,
		"workgroup_kind": "builder", "status": "planned",
		"documents": []any{map[string]any{"id": canonicalTaskID, "path": canonicalTask, "version": "v1.0.0", "sha256": digestHex(taskBytes)}},
		"risk_tags": []any{},
		"responsibility_dispositions": []any{map[string]any{
			"responsibility_id": "BUILD-WORK-PACKAGE", "disposition": "assigned", "trigger": "one scoped builder TASK",
			"assignment_ids": []string{canonicalAssignment}, "na_rationale": nil, "evidence_ref": nil,
		}},
		"assignments":      []any{assignmentManifest},
		"separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1,
		"quantity_rationale": "one owner for the bounded Main integration fixture",
		"validation": map[string]any{
			"result": "pass", "missing_responsibilities": []string{}, "unresolved_conflicts": []string{},
			"warnings": []string{}, "validated_at": "2026-10-14T00:00:00Z",
		},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, root, canonicalManifest, manifestBytes)
	gitFixture(t, root, "init", "-b", "main")
	gitFixture(t, root, "config", "user.name", "S6 integration test")
	gitFixture(t, root, "config", "user.email", "s6-integration@example.invalid")
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "commit", "-m", "S6 canonical Main fixture baseline")

	statePath, journalPath := prepareCanonicalRuntime(t, root, taskBytes, index)
	if beforeRegister != nil {
		beforeRegister(t, root, statePath, journalPath)
	}

	worker := filepath.Join(t.TempDir(), "worker")
	gitFixture(t, root, "worktree", "add", "-b", canonicalBranch, worker, "main")
	if changedPath == canonicalSourceFile {
		writeFixtureFile(t, worker, canonicalSourceFile, []byte("main-check-marker\n"))
	} else {
		writeFixtureFile(t, worker, changedPath, []byte("outside the original Assignment scope\n"))
	}
	if workerSetup != nil {
		workerSetup(t, worker)
	}
	gitFixture(t, worker, "add", ".")
	gitFixture(t, worker, "commit", "-m", "S6 worker source commit")

	preRegister := readRuntimeSnapshot(t, statePath, journalPath)
	if _, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: preRegister.Revision, ManifestPath: filepath.Join(root, filepath.FromSlash(canonicalManifest)),
		TaskID: canonicalTaskID, TaskPath: filepath.Join(root, filepath.FromSlash(canonicalTask)),
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("original Register: %v", err)
	}
	snapshot := readRuntimeSnapshot(t, statePath, journalPath)
	record, found, err := assignmentrecord.ForOwner(snapshot.State, canonicalAgentID)
	if err != nil || !found {
		t.Fatalf("read registered Assignment Record: found %t err %v", found, err)
	}
	planPath := writeCanonicalPlan(t, root, record)
	if _, outcome, err := assignment.AgentBegin(root, statePath, journalPath, assignment.AgentBeginRequest{
		ExpectedRevision: -1, AgentID: canonicalAgentID, PlanPath: planPath,
		OccurredAt: time.Now().UTC(),
	}); err != nil || !outcome.Chained {
		t.Fatalf("original AgentBegin: outcome %+v err %v", outcome, err)
	}
	fixture := &canonicalMainFixture{
		root: root, worker: worker, statePath: statePath, journalPath: journalPath,
		counterPath: counterPath, failPath: failPath, command: command, record: record, planPath: planPath,
	}
	fixture.result = fixture.acceptResult(t, "msg-s6-main-result-1", "The scoped behavior was implemented and evidence was recorded.")
	return fixture
}

func (f *canonicalMainFixture) acceptResult(t *testing.T, messageID, summary string) assignment.ResultAcceptanceReceipt {
	return f.acceptResultWithClosing(t, messageID, summary, "complete", nil, nil)
}

func (f *canonicalMainFixture) acceptResultWithClosing(t *testing.T, messageID, summary, disposition string, risks, deviations []string) assignment.ResultAcceptanceReceipt {
	t.Helper()
	occurredAt := time.Now().UTC()
	digest, err := f.record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "completion_report", "message_id": messageID,
		"correlation_id": "corr-" + messageID, "runtime_id": f.record.RuntimeID,
		"baseline_generation": f.record.BaselineGeneration, "agent_id": f.record.OwnerAgentID,
		"agent_definition_ref": f.record.AgentDefinition.Ref, "task_id": f.record.Task.ID, "bug_id": nil,
		"team_id": f.record.TeamID, "occurred_at": occurredAt.Format(time.RFC3339Nano),
		"assignment_id": f.record.AssignmentID, "assignment_revision": f.record.Revision,
		"assignment_record_contract": f.record.Contract, "assignment_record_sha256": digest,
		"summary": summary,
		"assertion_evidence": []any{map[string]any{
			"assertion": "TASK behavior is implemented", "oracle": "the real Main required check passes",
			"evidence_refs": []string{canonicalProofRef},
		}},
		"risks_blockers": nonNilStrings(risks), "scope_deviations": nonNilStrings(deviations),
	}
	if f.record.ClosingContract != nil {
		judgment := "satisfied"
		if disposition == "incomplete" || disposition == "blocked" {
			judgment = disposition
		}
		message["closing_assessment"] = map[string]any{
			"disposition": disposition,
			"conditions": []any{map[string]any{
				"done_when_index": 0, "judgment": judgment,
				"plan_assertion_indexes": []int{0}, "rationale": "The owner assessed the frozen DoneWhen against its accepted PLAN assertion and evidence.",
			}},
		}
	}
	messagePath := filepath.Join(f.root, ".claude", "evidence", canonicalTeamID, canonicalTaskID, messageID+".json")
	writeJSONFixture(t, messagePath, message)
	_, receipt, err := assignment.CompleteTaskWithReceipt(f.root, f.statePath, f.journalPath, assignment.CompletionRequest{
		ExpectedRevision: -1, AgentID: f.record.OwnerAgentID, MessagePath: messagePath,
		OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatalf("original CompleteTask Result writer: %v", err)
	}
	if !receipt.Current || receipt.ResultRevision == 0 || receipt.ResultRef == "" || receipt.ResultSHA256 == "" {
		t.Fatalf("original Result receipt = %+v", receipt)
	}
	return receipt
}

func currentCanonicalReceiptAndGate(t *testing.T, fixture *canonicalMainFixture) (qualitygate.BuilderReceipt, qualitygate.Evaluation) {
	t.Helper()
	snapshot := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	files := fileview.Disk{Root: fixture.root}
	currentRef := fixture.currentResultRef(t)
	receipt := qualitygate.CurrentBuilderReceipt(qualitygate.Input{Root: fixture.root, Snapshot: snapshot, Files: files}, qualitygate.BuilderReceiptRequest{
		Owner: fixture.record.OwnerAgentID, AssignmentID: fixture.record.AssignmentID, TaskID: fixture.record.Task.ID,
		CompletionRef: currentRef, RequiredChecks: append([]string{}, fixture.record.RequiredChecks...), DoneWhen: append([]string{}, fixture.record.DoneWhen...),
	})
	catalog, err := transition.LoadCatalog(fixture.root)
	if err != nil {
		t.Fatalf("load original Loop Definition for TR-006 Gate: %v", err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatalf("build original Quality Gate registry: %v", err)
	}
	evaluation, err := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), qualitygate.Input{
		Root: fixture.root, Snapshot: snapshot, Files: files, GateID: "GATE-BUILDER-BATCH-READY", TransitionID: "TR-006",
	})
	if err != nil {
		t.Fatalf("evaluate original TR-006 Quality Gate: %v", err)
	}
	return receipt, evaluation
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func (f *canonicalMainFixture) inspect(t *testing.T, recoveryBase string) integration.Inspection {
	t.Helper()
	snapshot := readRuntimeSnapshot(t, f.statePath, f.journalPath)
	record, found, err := assignmentrecord.ForOwner(snapshot.State, f.record.OwnerAgentID)
	if err != nil || !found || record.Reference() != f.record.Reference() {
		t.Fatalf("current original Record = found %t record %+v err %v", found, record.Reference(), err)
	}
	ref := record.Reference()
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	projection := hookctx.AssignmentContext{
		AssignmentID: record.AssignmentID, AssignmentRecord: &ref, AssignmentRecordSHA256: digest,
		AssignmentRevision: record.Revision, BaselineGeneration: record.BaselineGeneration,
		TaskID: record.Task.ID, OwnerAgentID: record.OwnerAgentID, RoleFamily: record.RoleFamily,
		AgentDefinitionRef: record.AgentDefinition.Ref, WorktreePath: f.worker, Branch: canonicalBranch,
		TargetBranch: "main", ManifestRef: record.Manifest.Ref, State: "reported",
		CompletionRef: f.currentResultRef(t), WritePaths: append([]string{}, record.EffectiveWritePaths...),
		ReadPaths: append([]string{}, record.ReadPaths...), OutputPaths: append([]string{}, record.ScopeBasis.ManifestOutputPaths...),
		SkillRefs: append([]string{}, record.SkillRefs...), DependsOn: append([]string{}, record.DependsOn...),
		RequiredChecks: append([]string{}, record.RequiredChecks...), CheckProfiles: append([]checkexec.Profile{}, record.CheckProfiles...),
		DoneWhen: append([]string{}, record.DoneWhen...), ResponsibilityIDs: []string{record.ResponsibilityID},
	}
	return func() integration.Inspection {
		inspection, err := integration.Inspect(context.Background(), integration.InspectRequest{
			Root: f.root, Assignment: projection, TargetBranch: "main", BaselineGeneration: record.BaselineGeneration,
			RuntimeID: record.RuntimeID,
		}, integration.InspectConfig{RecoveryBase: recoveryBase})
		if err != nil {
			t.Fatalf("original Inspect: %v", err)
		}
		return inspection
	}()
}

func (f *canonicalMainFixture) currentResultRef(t *testing.T) string {
	t.Helper()
	snapshot := readRuntimeSnapshot(t, f.statePath, f.journalPath)
	entities := snapshot.State["entities"].(map[string]any)
	for _, raw := range entities["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == f.record.OwnerAgentID {
			ref, _ := row["completion_reported_ref"].(string)
			if ref == "" {
				t.Fatal("original Runtime Agent has no current Result ref")
			}
			return ref
		}
	}
	t.Fatal("original Runtime Agent is missing")
	return ""
}

func (f *canonicalMainFixture) integrateConfig() integration.IntegrateConfig {
	return integration.IntegrateConfig{Root: f.root, GitRoot: f.root, RuntimeID: canonicalRuntimeID}
}

func assertActualCanonicalMainFacts(t *testing.T, f *canonicalMainFixture, cp integration.Checkpoint, resultRevision int, sourceHead, mergeBase, targetHead string) {
	t.Helper()
	facts := cp.CanonicalMainFacts
	if cp.State != integration.StateVerified || facts == nil {
		t.Fatalf("checkpoint has no verified canonical Main facts: state=%q facts=%+v", cp.State, facts)
	}
	if cp.AssignmentRecord != nil || cp.AssignmentRecordSHA256 != "" {
		t.Fatal("verified checkpoint retained a duplicate early Record binding beside authoritative Main facts")
	}
	if facts.Binding.AssignmentRecord != f.record.Reference() || facts.Binding.AssignmentRecordSHA256 != mustRecordDigest(t, f.record) || facts.Binding.ResultRevision != resultRevision || facts.Binding.AnswerDigest == "" {
		t.Fatalf("Main facts Result/Record binding = %+v", facts.Binding)
	}
	if cp.TargetHead != targetHead || facts.MainRoot != f.root || facts.SourceHead != sourceHead || facts.MergeBase != mergeBase || facts.TargetBranch != "main" || facts.MergeCommit != cp.MergeCommit || facts.TestedHead != cp.TestedHead || facts.TestedHead != cp.MergeCommit || facts.TestedHead == cp.TargetHead || facts.ExecutionGeneration != 0 {
		t.Fatalf("Main Git/runner coordinates = %+v", facts)
	}
	wantPaths := []string{canonicalSourceFile}
	if len(facts.ChangedPaths) != 1 || facts.ChangedPaths[0] != wantPaths[0] {
		t.Fatalf("MainFacts changed paths = %v, want actual Git path %v", facts.ChangedPaths, wantPaths)
	}
	wantDigest, err := assignmentresult.ChangedPathsDigest(wantPaths)
	if err != nil || facts.ChangedPathsSHA256 != wantDigest {
		t.Fatalf("MainFacts changed path digest = %q, want %q (%v)", facts.ChangedPathsSHA256, wantDigest, err)
	}
	if len(facts.Checks) != len(f.record.RequiredChecks) || len(facts.Checks) != 1 {
		t.Fatalf("Main check coverage = %d, original RequiredChecks = %v", len(facts.Checks), f.record.RequiredChecks)
	}
	fact := facts.Checks[0]
	if fact.CheckIndex != 0 || fact.Command != f.command || fact.Status != "pass" || fact.TestedHead != cp.TestedHead || fact.ReceiptRef == "" || fact.LogRef == "" || fact.ReceiptSHA256 == "" || fact.LogSHA256 == "" {
		t.Fatalf("Main check fact = %+v", fact)
	}
	if len(cp.CheckReceipts) != 1 || filepath.Clean(cp.CheckReceipts[0]) != filepath.Clean(fact.ReceiptRef) {
		t.Fatalf("checkpoint Main receipts = %v, fact receipt %s", cp.CheckReceipts, fact.ReceiptRef)
	}
	receiptBytes, err := os.ReadFile(fact.ReceiptRef)
	if err != nil || digestHex(receiptBytes) != fact.ReceiptSHA256 {
		t.Fatalf("Main receipt bytes/digest mismatch: err %v SHA %q", err, fact.ReceiptSHA256)
	}
	logBytes, err := os.ReadFile(fact.LogRef)
	if err != nil || digestHex(logBytes) != fact.LogSHA256 {
		t.Fatalf("Main log bytes/digest mismatch: err %v SHA %q", err, fact.LogSHA256)
	}
	var receipt struct {
		Command    string `json:"command"`
		CWD        string `json:"cwd"`
		Head       string `json:"head"`
		HeadBefore string `json:"head_before"`
		HeadAfter  string `json:"head_after"`
		Status     string `json:"status"`
		Exit       *int   `json:"exit_code"`
		Output     string `json:"output"`
		Binding    struct {
			RuntimeID           string `json:"runtime_id"`
			Generation          int    `json:"baseline_generation"`
			AssignmentID        string `json:"assignment_id"`
			Owner               string `json:"owner"`
			ExecutionGeneration int    `json:"execution_generation"`
			Responsibility      string `json:"responsibility"`
		} `json:"check_binding"`
	}
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Command != fact.Command || filepath.Clean(receipt.CWD) != filepath.Clean(f.root) || receipt.Head != cp.TestedHead || receipt.HeadBefore != cp.TestedHead || receipt.HeadAfter != cp.TestedHead || receipt.Status != "pass" || receipt.Exit == nil || *receipt.Exit != 0 || receipt.Output != fact.LogRef || receipt.Binding.RuntimeID != canonicalRuntimeID || receipt.Binding.Generation != 1 || receipt.Binding.AssignmentID != canonicalAssignment || receipt.Binding.Owner != "Main" || receipt.Binding.Responsibility != "main_required_integration" || receipt.Binding.ExecutionGeneration != 0 {
		t.Fatalf("raw Main receipt does not prove current original check/head/binding: %+v", receipt)
	}
	if len(f.record.CheckProfiles) != 1 || fact.ProfileDigest != f.record.CheckProfiles[0].Digest() || len(cp.ToolCheckReceipts) != 1 {
		t.Fatalf("typed Main Profile/receipt chain missing: fact=%+v profiles=%d tools=%d", fact, len(f.record.CheckProfiles), len(cp.ToolCheckReceipts))
	}
	if _, err := checkexec.VerifyToolReceipt(filepath.Join(filepath.Dir(integration.CheckpointPath(f.root, canonicalRuntimeID, 1, canonicalAssignment)), "checks"), cp.ToolCheckReceipts[0]); err != nil {
		t.Fatalf("original typed Main ToolReceipt verification: %v", err)
	}
	resultBytes, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(facts.Binding.ResultPath)))
	if err != nil || digestHex(resultBytes) != facts.Binding.ResultSHA256 {
		t.Fatalf("Main facts accepted Result bytes/digest mismatch: %v", err)
	}
	var accepted assignmentresult.AcceptedResult
	if err := json.Unmarshal(resultBytes, &accepted); err != nil || accepted.Conclusion != "submitted" || accepted.ResultRevision != resultRevision || accepted.AnswerDigest != facts.Binding.AnswerDigest {
		t.Fatalf("Main machine verification changed professional Result conclusion: result=%+v err=%v", accepted, err)
	}
	if cp.CompletionReportPath != facts.Binding.ResultPath || cp.CompletionReportSHA256 != facts.Binding.ResultSHA256 {
		t.Fatalf("original completion report cross-check differs from canonical binding: %s %s", cp.CompletionReportPath, cp.CompletionReportSHA256)
	}
}

func assertCanonicalQualitygateState(t *testing.T, f *canonicalMainFixture, wantIntegrated bool, wantGateStatus qualitygate.Status) {
	t.Helper()
	snapshot := readRuntimeSnapshot(t, f.statePath, f.journalPath)
	files := fileview.Disk{Root: f.root}
	input := qualitygate.Input{Root: f.root, Snapshot: snapshot, Files: files}
	receipt := qualitygate.CurrentBuilderReceipt(input, qualitygate.BuilderReceiptRequest{
		Owner: f.record.OwnerAgentID, AssignmentID: f.record.AssignmentID, TaskID: f.record.Task.ID,
		CompletionRef: f.currentResultRef(t), RequiredChecks: append([]string{}, f.record.RequiredChecks...),
		DoneWhen: append([]string{}, f.record.DoneWhen...),
	})
	if !receipt.Accepted || receipt.Integrated != wantIntegrated || receipt.WorkComplete || receipt.ProfessionalStatus != "accepted_unassessed" {
		if wantIntegrated && !receipt.Integrated {
			diagnosticCanonicalQualitygateMismatch(t, f, snapshot)
		}
		t.Fatalf("CurrentBuilderReceipt accepted/integrated/work_complete/professional = %t/%t/%t/%q, want true/%t/false/accepted_unassessed; pending=%v waiting=%v",
			receipt.Accepted, receipt.Integrated, receipt.WorkComplete, receipt.ProfessionalStatus, wantIntegrated, receipt.OwnPending, receipt.WaitingOn)
	}
	if receipt.ResponsibilityStatus != "accepted_incomplete" {
		t.Fatalf("CurrentBuilderReceipt responsibility status = %q, want accepted_incomplete", receipt.ResponsibilityStatus)
	}

	catalog, err := transition.LoadCatalog(f.root)
	if err != nil {
		t.Fatalf("load original Loop Definition for TR-006 Gate: %v", err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatalf("build original Quality Gate registry: %v", err)
	}
	evaluation, err := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), qualitygate.Input{
		Root: f.root, Snapshot: snapshot, Files: files,
		GateID: "GATE-BUILDER-BATCH-READY", TransitionID: "TR-006",
	})
	if err != nil || evaluation.Status != wantGateStatus {
		t.Fatalf("actual TR-006 Quality Gate = status %q missing %v conflicts %v err %v, want %q",
			evaluation.Status, evaluation.Missing, evaluation.Conflicts, err, wantGateStatus)
	}
	if evaluation.Status == qualitygate.StatusSatisfied && len(evaluation.Missing) != 0 {
		t.Fatalf("satisfied TR-006 Gate retained missing facts: %v", evaluation.Missing)
	}
	if evaluation.Status == qualitygate.StatusNotReady && !containsString(evaluation.Missing, "integration_checkpoint:"+f.record.Task.ID+":canonical result/integration not verified") {
		t.Fatalf("TR-006 Gate did not retain the canonical Main integration gap: %v", evaluation.Missing)
	}
}

func diagnosticCanonicalQualitygateMismatch(t *testing.T, f *canonicalMainFixture, snapshot loopruntime.Snapshot) {
	t.Helper()
	path := integration.CheckpointPath(f.root, canonicalRuntimeID, 1, canonicalAssignment)
	cp, found, err := integration.DefaultCheckpointStore().Load(path)
	if err != nil || !found || cp.CanonicalMainFacts == nil {
		t.Logf("qualitygate debug checkpoint found=%t err=%v", found, err)
		return
	}
	read := fileview.Disk{Root: f.root}
	resultRef := f.currentResultRef(t)
	accepted, acceptedErr := assignmentresult.LoadAccepted(snapshot.State, f.record, resultRef, func(ref string) ([]byte, error) {
		return read.ReadFile(ref)
	})
	resultBytes, resultErr := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(resultRef)))
	var mainErr error
	if acceptedErr == nil && resultErr == nil {
		mainErr = assignmentresult.ValidateMainFacts(f.record, accepted, resultRef, digestHex(resultBytes), *cp.CanonicalMainFacts, func(ref string) ([]byte, error) {
			return os.ReadFile(ref)
		})
	}
	mergeBase := ""
	if cp.TargetHead != "" && cp.SourceHead != "" {
		mergeBase = readGit(t, f.root, "merge-base", cp.TargetHead, cp.SourceHead)
	}
	targetHasMerge := exec.Command("git", "-C", f.root, "merge-base", "--is-ancestor", cp.MergeCommit, cp.TargetBranch).Run() == nil
	t.Logf("qualitygate debug: accepted_err=%v result_err=%v validate_main_facts=%v cp_state=%s task=%s assignment=%s generation=%d report=%s result=%s mergebase=%s/%s target=%s source=%s merge=%s tested=%s mainroot=%s targetBranch=%s boundBranch=%s targetHasMerge=%t cp_changed=%v/%s facts=%+v verifiedAt=%s",
		acceptedErr, resultErr, mainErr, cp.State, cp.TaskID, cp.AssignmentID, cp.BaselineGeneration,
		cp.CompletionReportPath, resultRef, mergeBase, cp.MergeBase, cp.TargetHead, cp.SourceHead, cp.MergeCommit, cp.TestedHead,
		cp.CanonicalMainFacts.MainRoot, cp.TargetBranch, "main", targetHasMerge, cp.ChangedPaths, cp.ChangedPathsSHA256, *cp.CanonicalMainFacts, cp.VerifiedAt)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsEveryString(values, want []string) bool {
	for _, item := range want {
		if !containsString(values, item) {
			return false
		}
	}
	return true
}

func corruptCanonicalMainProof(t *testing.T, f *canonicalMainFixture) error {
	t.Helper()
	path := integration.CheckpointPath(f.root, canonicalRuntimeID, 1, canonicalAssignment)
	cp, found, err := integration.DefaultCheckpointStore().Load(path)
	if err != nil || !found || cp.CanonicalMainFacts == nil || len(cp.CanonicalMainFacts.Checks) != 1 {
		return fmt.Errorf("load verified canonical Main checkpoint: found %t err %v", found, err)
	}
	fact := cp.CanonicalMainFacts.Checks[0]
	for _, ref := range []string{fact.ReceiptRef, fact.LogRef} {
		if err := os.Remove(ref); err != nil {
			return fmt.Errorf("remove original Main evidence %s: %w", ref, err)
		}
	}
	cp.CanonicalMainFacts.Checks[0].ReceiptSHA256 = ""
	cp.CanonicalMainFacts.Checks[0].LogSHA256 = ""
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func assertTwoParentMerge(t *testing.T, root, merge, targetParent, sourceParent string) {
	t.Helper()
	line := readGit(t, root, "rev-list", "--parents", "-n", "1", merge)
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[0] != merge || fields[1] != targetParent || fields[2] != sourceParent {
		t.Fatalf("merge %s parents = %v, want non-squash target/source parents %s / %s", merge, fields, targetParent, sourceParent)
	}
	if head := readGit(t, root, "rev-parse", "main"); head != merge {
		t.Fatalf("target branch head = %s, want recorded merge %s", head, merge)
	}
}

func prepareCanonicalRuntime(t *testing.T, root string, taskBytes, indexBytes []byte) (string, string) {
	t.Helper()
	raw, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = "1.2.0"
	state["runtime_id"] = canonicalRuntimeID
	state["revision"] = float64(8)
	delete(state, "milestone")
	state["last_transition"] = nil
	state["definition"] = fixtureFingerprint(t, root, "docs/control/loop-definition.json", "1.3.0")
	state["hook_control"].(map[string]any)["policy_ref"] = fixtureFingerprint(t, root, "docs/control/hook-policy.json", "v2.0.0")
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": float64(0)}
	state["bound_req"] = map[string]any{
		"path": "docs/requirements/REQ-001.md", "version": "v1.0.0", "sha256": strings.Repeat("1", 64),
		"id": "REQ-001", "status": "locked", "approved_by": "user", "approved_at": "2026-10-14T00:00:00Z",
		"workspace": map[string]any{"project_root": root, "dev_branch": "main", "release_upstream": "main", "bound_commit": "baseline"},
	}
	state["documents"] = []any{
		map[string]any{"id": canonicalTaskID, "kind": "task", "path": canonicalTask, "version": "v1.0.0", "sha256": digestHex(taskBytes), "status": "locked", "generation": float64(1)},
		map[string]any{"id": "dispatch-plan:REQ-001", "kind": "dispatch_plan", "path": "docs/dev/tasks/index-REQ-001.md", "version": "1", "sha256": digestHex(indexBytes), "status": "locked", "generation": float64(1)},
	}
	state["baseline"] = map[string]any{"generation": float64(1), "captured_at": "2026-10-14T00:00:00Z"}
	state["entities"] = map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}, "assignments": []any{}}
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": float64(0), "last_event_id": nil}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	writeJSONFixture(t, statePath, state)
	return statePath, journalPath
}

func writeCanonicalPlan(t *testing.T, root string, record assignmentrecord.Record) string {
	t.Helper()
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	assertion := map[string]any{"assertion": "TASK behavior is implemented", "oracle": "the real Main required check passes"}
	if record.ClosingContract != nil {
		assertion["done_when_indexes"] = []int{0}
	}
	plan := map[string]any{
		"schema_version": "1.0.0", "message_type": "plan_report", "message_id": "msg-s6-main-plan",
		"correlation_id": "corr-s6-main-plan", "runtime_id": record.RuntimeID,
		"agent_id": record.OwnerAgentID, "agent_definition_ref": record.AgentDefinition.Ref,
		"task_id": record.Task.ID, "bug_id": nil, "team_id": record.TeamID,
		"occurred_at": time.Now().UTC().Format(time.RFC3339Nano), "assignment_id": record.AssignmentID,
		"assignment_revision": record.Revision, "assignment_record_contract": record.Contract,
		"assignment_record_sha256": digest, "objective": "Implement the reviewed builder responsibility.",
		"planned_paths":    []string{canonicalSourceFile},
		"assertion_checks": []any{assertion},
		"dependencies":     []string{}, "risks_blockers": []string{},
	}
	path := filepath.Join(root, filepath.FromSlash(canonicalPlanRef))
	writeJSONFixture(t, path, plan)
	return path
}

func fixtureFingerprint(t *testing.T, root, ref, version string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref)))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"path": ref, "version": version, "sha256": digestHex(data)}
}

func sourceRepositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func copyFixtureTree(t *testing.T, source, target string) {
	t.Helper()
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		output := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(output, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			return err
		}
		return os.WriteFile(output, data, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
}

func copyFixtureFile(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFixtureFile(t *testing.T, root, ref string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readRuntimeSnapshot(t *testing.T, statePath, journalPath string) loopruntime.Snapshot {
	t.Helper()
	snapshot, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatalf("read original Runtime snapshot: %v", err)
	}
	return snapshot
}

func gitFixture(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func readGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	return gitFixture(t, root, args...)
}

func digestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func mustRecordDigest(t *testing.T, record assignmentrecord.Record) string {
	t.Helper()
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func fileSize(t *testing.T, path string) int {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return int(info.Size())
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
