package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

func TestCanonicalRevisionReusesOnlyValidatedPredecessorSideParent(t *testing.T) {
	ctx := context.Background()
	fixture := newCanonicalMainFixture(t, canonicalSourceFile)
	r1 := fixture.record
	r1Inspection := fixture.inspect(t, "")
	if !r1Inspection.Ready {
		t.Fatalf("r1 Inspect not ready: %v", r1Inspection.Blockers)
	}
	r1Integrated, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: r1Inspection}, fixture.integrateConfig())
	if err != nil || r1Integrated.Checkpoint.State != integration.StateVerified {
		t.Fatalf("original r1 Main integration: state=%q err=%v", r1Integrated.Checkpoint.State, err)
	}
	r1Path := integration.CheckpointPathForRecord(fixture.root, r1.Reference())
	r1Before, err := os.ReadFile(r1Path)
	if err != nil || r1Integrated.Checkpoint.CanonicalMainFacts == nil {
		t.Fatalf("read verified r1 checkpoint/facts: err=%v facts=%+v", err, r1Integrated.Checkpoint.CanonicalMainFacts)
	}
	r1Facts := *r1Integrated.Checkpoint.CanonicalMainFacts
	r1Merge := r1Integrated.Checkpoint.MergeCommit
	r1Source := r1Facts.SourceHead
	if fileSize(t, fixture.counterPath) != 1 {
		t.Fatalf("r1 Main check count=%d, want 1", fileSize(t, fixture.counterPath))
	}

	// Revise the manifest on Main while keeping the worker branch at its
	// verified r1 SourceHead. The next source commit must have r1's source as
	// its merge base and must not contain r1's already-integrated merge commit.
	manifestPath := filepath.Join(fixture.root, filepath.FromSlash(canonicalManifest))
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["manifest_id"] = "team-manifest-s6-predecessor-fixture-r2"
	manifest["version"] = "v1.0.2"
	manifest["workgroup_id"] = "workgroup-s6-predecessor-fixture-r2"
	manifest["platform_team_id"] = "platform-team-s6-predecessor-fixture-r2"
	assignments, ok := manifest["assignments"].([]any)
	if !ok || len(assignments) != 1 {
		t.Fatalf("manifest assignments = %#v", manifest["assignments"])
	}
	assignmentManifest, ok := assignments[0].(map[string]any)
	if !ok {
		t.Fatalf("manifest assignment = %#v", assignments[0])
	}
	assignmentManifest["agent_id"] = "agent-s6-predecessor-fixture-r2"
	updatedManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	updatedManifest = append(updatedManifest, '\n')
	if err := os.WriteFile(manifestPath, updatedManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, fixture.root, "add", canonicalManifest)
	gitFixture(t, fixture.root, "commit", "-m", "prepare explicit S6 predecessor revision manifest")
	manifestMainHead := readGit(t, fixture.root, "rev-parse", "main")

	const r2ChangedPath = "src/pkg/predecessor-revision-two.txt"
	if err := os.WriteFile(filepath.Join(fixture.worker, filepath.FromSlash(r2ChangedPath)), []byte("revision two side-parent change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, fixture.worker, "add", r2ChangedPath)
	gitFixture(t, fixture.worker, "commit", "-m", "S6 revision two change on predecessor source branch")
	r2SourceHead := readGit(t, fixture.worker, "rev-parse", "HEAD")
	if got := readGit(t, fixture.worker, "rev-parse", "HEAD^"); got != r1Source {
		t.Fatalf("r2 source parent=%s, want exact r1 SourceHead %s", got, r1Source)
	}

	beforeRevision := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	previousSHA, err := r1.Digest()
	if err != nil {
		t.Fatal(err)
	}
	const revisionIntent = "s6-predecessor-fixture-revision-two"
	issued, err := assignment.Register(fixture.root, fixture.statePath, fixture.journalPath, assignment.Request{
		ExpectedRevision: beforeRevision.Revision,
		ManifestPath:     manifestPath,
		TaskID:           r1.Task.ID,
		TaskPath:         filepath.Join(fixture.root, filepath.FromSlash(canonicalTask)),
		Revision: &assignment.RevisionRequest{
			AssignmentID: r1.AssignmentID, ExpectedRecord: r1.Reference(),
			ExpectedRecordSHA256: previousSHA, IntentKey: revisionIntent,
		},
		OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("original Register revision Writer: %v", err)
	}
	r2, found, err := assignmentrecord.ForOwner(issued.State, "agent-s6-predecessor-fixture-r2")
	if err != nil || !found || r2.Revision != 2 || r2.AssignmentID != r1.AssignmentID {
		t.Fatalf("resolve issued r2 Record: found=%t Record=%+v err=%v", found, r2.Reference(), err)
	}
	assertStaleOwnerAckRejected(t, fixture, r1)
	r2PlanPath := writeCanonicalRevisionPlan(t, fixture.root, r2)
	if _, outcome, err := assignment.AgentBegin(fixture.root, fixture.statePath, fixture.journalPath, assignment.AgentBeginRequest{
		ExpectedRevision: -1, AgentID: r2.OwnerAgentID, PlanPath: r2PlanPath, OccurredAt: time.Now().UTC(),
	}); err != nil || !outcome.Chained {
		t.Fatalf("r2 original AgentBegin: outcome=%+v err=%v", outcome, err)
	}
	r2Result := acceptCanonicalRevisionResult(t, fixture, r2, "msg-s6-predecessor-r2-result", "The revised scoped change was implemented and assessed.")
	fixture.record = r2
	fixture.planPath = r2PlanPath
	fixture.result = r2Result

	r2Inspection := fixture.inspect(t, "")
	if !r2Inspection.Ready || !reflect.DeepEqual(r2Inspection.ChangedPaths, []string{r2ChangedPath}) {
		t.Fatalf("r2 Inspect ready=%t changed paths=%v blockers=%v", r2Inspection.Ready, r2Inspection.ChangedPaths, r2Inspection.Blockers)
	}
	if r2Inspection.SourceHead != r2SourceHead || r2Inspection.MergeBase != r1Source || r2Inspection.TargetHead != manifestMainHead {
		t.Fatalf("r2 inspection did not preserve the predecessor lineage: source=%s merge-base=%s target=%s; want source=%s base=%s target=%s", r2Inspection.SourceHead, r2Inspection.MergeBase, r2Inspection.TargetHead, r2SourceHead, r1Source, manifestMainHead)
	}
	// Removing the original revision event breaks the append-only journal
	// proof. Integrate must refuse before Main moves or any required check runs.
	restoreJournal := removeCanonicalJournalEvent(t, fixture.journalPath, loopruntime.AssignmentRevisionEventID(r1.Reference(), revisionIntent))
	missingJournal, missingJournalErr := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: r2Inspection}, fixture.integrateConfig())
	restoreJournal()
	if missingJournal.Checkpoint.State != integration.StatePreserved || missingJournal.Checkpoint.MergeCommit != "" || len(missingJournal.Checkpoint.Blockers) == 0 || readGit(t, fixture.root, "rev-parse", "main") != manifestMainHead || fileSize(t, fixture.counterPath) != 1 {
		t.Fatalf("missing original revision journal event was not preserved before Main/checks: state=%q merge=%s blockers=%v err=%v head=%s checks=%d", missingJournal.Checkpoint.State, missingJournal.Checkpoint.MergeCommit, missingJournal.Checkpoint.Blockers, missingJournalErr, readGit(t, fixture.root, "rev-parse", "main"), fileSize(t, fixture.counterPath))
	}
	// The current Record's mechanical scope still governs the complete
	// predecessor-based diff; an out-of-scope descendant cannot be merged.
	const outsidePath = "docs/dev/tasks/outside-s6-scope.txt"
	if err := os.WriteFile(filepath.Join(fixture.worker, filepath.FromSlash(outsidePath)), []byte("outside current Assignment scope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, fixture.worker, "add", outsidePath)
	gitFixture(t, fixture.worker, "commit", "-m", "attempt out-of-scope S6 revision change")
	outsideInspection := fixture.inspect(t, "")
	_, scopeErr := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: outsideInspection}, fixture.integrateConfig())
	if !errors.Is(scopeErr, integration.ErrScopeViolation) || readGit(t, fixture.root, "rev-parse", "main") != manifestMainHead || fileSize(t, fixture.counterPath) != 1 {
		t.Fatalf("out-of-scope r2 diff was not rejected before Main/checks: err=%v head=%s checks=%d blockers=%v", scopeErr, readGit(t, fixture.root, "rev-parse", "main"), fileSize(t, fixture.counterPath), outsideInspection.Blockers)
	}
	gitFixture(t, fixture.worker, "reset", "--hard", r2SourceHead)
	if len(r1Facts.Checks) != 1 || r1Facts.Checks[0].LogRef == "" {
		t.Fatalf("r1 predecessor checkpoint has no original Main log ref: %+v", r1Facts.Checks)
	}
	r1LogPath := r1Facts.Checks[0].LogRef
	r1LogBefore, err := os.ReadFile(r1LogPath)
	if err != nil {
		t.Fatalf("read predecessor Main log before tamper check: %v", err)
	}
	if err := os.WriteFile(r1LogPath, []byte("tampered predecessor Main log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	preserved, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: r2Inspection}, fixture.integrateConfig())
	if err != nil || preserved.Checkpoint.State != integration.StatePreserved || preserved.Checkpoint.MergeCommit != "" {
		t.Fatalf("r2 with tampered predecessor Main log should remain preserved before merge: state=%q merge=%s err=%v", preserved.Checkpoint.State, preserved.Checkpoint.MergeCommit, err)
	}
	if readGit(t, fixture.root, "rev-parse", "main") != manifestMainHead || fileSize(t, fixture.counterPath) != 1 {
		t.Fatalf("invalid predecessor evidence moved Main or ran current checks: head=%s checks=%d", readGit(t, fixture.root, "rev-parse", "main"), fileSize(t, fixture.counterPath))
	}
	if err := os.WriteFile(r1LogPath, r1LogBefore, 0o600); err != nil {
		t.Fatal(err)
	}
	retried, err := integration.Integrate(ctx, integration.IntegrateRequest{
		Inspection: r2Inspection, ExpectedRevision: preserved.Checkpoint.Revision, RetryPreserved: true,
	}, fixture.integrateConfig())
	if err != nil || retried.Checkpoint.State != integration.StateVerified {
		t.Fatalf("integrate r2 from validated r1 side-parent: %v", err)
	}
	r2Path := integration.CheckpointPathForRecord(fixture.root, r2.Reference())
	r2Checkpoint, found, err := integration.DefaultCheckpointStore().Load(r2Path)
	if err != nil || !found || r2Checkpoint.State != integration.StateVerified || r2Checkpoint.CanonicalMainFacts == nil {
		t.Fatalf("r2 current checkpoint: found=%t state=%q facts=%+v err=%v", found, r2Checkpoint.State, r2Checkpoint.CanonicalMainFacts, err)
	}
	facts := r2Checkpoint.CanonicalMainFacts
	if facts.Binding.AssignmentRecord != r2.Reference() || facts.Binding.ResultRevision != r2Result.ResultRevision || facts.Binding.AnswerDigest != r2Result.AnswerDigest || facts.MergeBase != r1Source || facts.SourceHead != r2SourceHead || facts.TestedHead != facts.MergeCommit || r2Checkpoint.TargetHead != manifestMainHead {
		t.Fatalf("r2 Main facts did not bind its own Result and historical source boundary: %+v", facts)
	}
	if facts.MergeCommit == r1Merge || facts.TargetBranch != "main" || facts.TestedHead == r1Facts.TestedHead || len(facts.ChangedPaths) != 1 || facts.ChangedPaths[0] != r2ChangedPath || len(facts.Checks) != len(r2.RequiredChecks) || len(facts.Checks) != 1 {
		t.Fatalf("r2 facts borrowed predecessor merge/diff/check coverage: %+v", facts)
	}
	if readGit(t, fixture.root, "rev-parse", facts.MergeCommit+"^") != manifestMainHead || readGit(t, fixture.root, "rev-parse", facts.MergeCommit+"^2") != r2SourceHead {
		t.Fatalf("r2 Main merge parents do not preserve actual Main and source heads: parents=%s", readGit(t, fixture.root, "show", "-s", "--format=%P", facts.MergeCommit))
	}
	if fileSize(t, fixture.counterPath) != 2 {
		t.Fatalf("r2 did not execute every fresh Main check; cumulative count=%d", fileSize(t, fixture.counterPath))
	}
	for index, check := range facts.Checks {
		if check.Status != "pass" || check.TestedHead != facts.MergeCommit || check.ReceiptRef == r1Facts.Checks[index].ReceiptRef || check.ReceiptSHA256 == r1Facts.Checks[index].ReceiptSHA256 {
			t.Fatalf("r2 Main check %d borrowed predecessor evidence: r1=%+v r2=%+v", index, r1Facts.Checks[index], check)
		}
		receiptBytes, err := os.ReadFile(check.ReceiptRef)
		if err != nil || digestHex(receiptBytes) != check.ReceiptSHA256 {
			t.Fatalf("r2 Main check %d durable receipt mismatch: err=%v", index, err)
		}
	}
	if after, err := os.ReadFile(r1Path); err != nil || !reflect.DeepEqual(after, r1Before) {
		t.Fatalf("r2 modified immutable predecessor checkpoint bytes: err=%v", err)
	}
	if stale, found, err := integration.DefaultCheckpointStore().Load(r1Path); err != nil || !found || stale.CanonicalMainFacts == nil || stale.CanonicalMainFacts.Binding.AssignmentRecord != r1.Reference() || stale.State != integration.StateVerified {
		t.Fatalf("predecessor checkpoint was not preserved as historical audit: found=%t state=%q facts=%+v err=%v", found, stale.State, stale.CanonicalMainFacts, err)
	}
	assertCanonicalQualitygateState(t, fixture, true, qualitygate.StatusSatisfied)
}

func removeCanonicalJournalEvent(t *testing.T, journalPath, eventID string) func() {
	t.Helper()
	paths, err := loopruntime.JournalSegmentPaths(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, journalPath)
	for _, path := range paths {
		original, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		var kept strings.Builder
		removed := 0
		for _, line := range strings.SplitAfter(string(original), "\n") {
			var event struct {
				ID string `json:"event_id"`
			}
			if json.Unmarshal([]byte(line), &event) == nil && event.ID == eventID {
				removed++
				continue
			}
			kept.WriteString(line)
		}
		if removed == 0 {
			continue
		}
		if removed != 1 {
			t.Fatalf("original Runtime revision event occurs %d times in journal segment %s", removed, path)
		}
		if err := os.WriteFile(path, []byte(kept.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return func() {
			t.Helper()
			if err := os.WriteFile(path, original, 0o644); err != nil {
				t.Fatalf("restore original Runtime journal segment: %v", err)
			}
		}
	}
	t.Fatalf("original Runtime revision event %s is missing from all journal segments", eventID)
	return func() {}
}
