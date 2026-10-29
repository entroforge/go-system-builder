package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/buildsurface"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/mainreceipt"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestCanonicalSurfaceEarlyInspectionFailureDoesNotBlockSuccessfulRevision(t *testing.T) {
	ctx := context.Background()
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, seedCanonicalS5AndApplyTR003)
	r1 := fixture.record
	// Fail the real clean-tree check before Inspect resolves source/target/base.
	// This is the original Writer's early failure path, not a hand-written checkpoint.
	dirtyPath := filepath.Join(fixture.worker, "src/pkg/uncommitted.txt")
	if err := os.WriteFile(dirtyPath, []byte("uncommitted fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inspection := fixture.inspect(t, "")
	if err := os.Remove(dirtyPath); err != nil {
		t.Fatal(err)
	}
	if inspection.Ready || inspection.CanonicalResult == nil || inspection.SourceHead != "" || inspection.TargetHead != "" || inspection.MergeBase != "" || len(inspection.Blockers) != 1 {
		t.Fatalf("not the original early inspection failure: %+v", inspection)
	}
	failed, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig())
	if err != nil || failed.Checkpoint.State != integration.StatePreserved || failed.Checkpoint.CanonicalMainFacts != nil || failed.Checkpoint.MergeCommit != "" {
		t.Fatalf("early failure invented a Main contribution: %+v err=%v", failed.Checkpoint, err)
	}
	r1Path := integration.CheckpointPathForRecord(fixture.root, r1.Reference())
	r1Before, err := os.ReadFile(r1Path)
	if err != nil {
		t.Fatal(err)
	}
	before := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	if _, err := buildsurface.Read(fixture.root, before.State); !errors.Is(err, mainreceipt.ErrNotVerified) {
		t.Fatalf("unfinished current r1 must still block: %v", err)
	}
	r2 := registerCanonicalRevisionTwo(t, fixture, r1, "agent-s6-early-failure-r2", canonicalSourceFile, []byte("main-check-marker\n"), "early-failure-r2")
	ready := fixture.inspect(t, "")
	if !ready.Ready {
		t.Fatalf("legitimate successor not ready: %v", ready.Blockers)
	}
	integrated, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: ready}, fixture.integrateConfig())
	if err != nil || integrated.Checkpoint.State != integration.StateVerified {
		t.Fatalf("successor integration failed: %+v err=%v", integrated.Checkpoint, err)
	}
	current := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	surface, err := buildsurface.Read(fixture.root, current.State)
	if err != nil || len(surface.Contributions) != 1 || surface.Contributions[0].AssignmentRecord != r2.Reference() {
		t.Fatalf("early r1 blocked verified r2 or became successful history: %+v err=%v", surface, err)
	}
	catalog, err := transition.LoadCatalog(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := fileview.New(fixture.root, "refs/heads/main", catalog.Definition.FileSources)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := qualitygate.NewEvaluator(registry).Evaluate(ctx, qualitygate.Input{
		Root: fixture.root, Snapshot: current, Files: files, GateID: "GATE-BUILDER-BATCH-READY", TransitionID: "TR-006",
	})
	if err != nil || gate.Status != qualitygate.StatusSatisfied {
		t.Fatalf("original S6 exit after early r1 and verified r2: %+v err=%v", gate, err)
	}
	started, err := transition.Apply(fixture.root, fixture.statePath, fixture.journalPath, transition.Request{
		Files: files, TransitionID: "TR-006", ExpectedRevision: current.Revision,
		ExpectedRuntimeID: canonicalRuntimeID, Actor: "orchestrator",
		Evidence: map[string]string{"builder_report_record": canonicalEvidenceIDForPath(t, current.State, integrated.Checkpoint.CanonicalMainFacts.Binding.ResultPath)},
		GateID:   gate.GateID, GateFingerprint: gate.Fingerprint, ProducerResponsibility: "delivery_orchestrator",
	})
	if err != nil {
		t.Fatalf("original S6 exit Writer: %v", err)
	}
	draft, notes := review.DraftPlanForRoot(fixture.root, started.State, 1)
	if draft == nil || draft.ProductBaseline == nil || !reflect.DeepEqual(*draft.ProductBaseline, surface) {
		t.Fatalf("S7 draft lost verified successor surface: %+v notes=%v", draft, notes)
	}
	if after, err := os.ReadFile(r1Path); err != nil || !reflect.DeepEqual(after, r1Before) {
		t.Fatalf("early historical failure was rewritten: %v", err)
	}
}

func TestCanonicalSurfaceSkipsOnlyPreservedFailedHistoricalContribution(t *testing.T) {
	ctx := context.Background()
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, seedCanonicalS5AndApplyTR003)
	r1 := fixture.record
	r1Result := fixture.result

	// The first accepted contribution merges an actual failing implementation.
	// Keep one additional in-scope path so the final source-backed S7 surface
	// must continue reporting product bytes outside the successful r2 facts.
	if err := os.WriteFile(filepath.Join(fixture.worker, filepath.FromSlash(canonicalSourceFile)), []byte("the Main check marker is missing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const r1RetainedPath = "src/pkg/r1-retained.txt"
	if err := os.WriteFile(filepath.Join(fixture.worker, filepath.FromSlash(r1RetainedPath)), []byte("preserved product output from failed r1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, fixture.worker, "add", "--", canonicalSourceFile, r1RetainedPath)
	gitFixture(t, fixture.worker, "commit", "-m", "S6 r1 implementation fails the real Main check")
	r1Inspection := fixture.inspect(t, "")
	if !r1Inspection.Ready {
		t.Fatalf("r1 Inspect not ready: %v", r1Inspection.Blockers)
	}
	r1Integrated, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: r1Inspection}, fixture.integrateConfig())
	if !errors.Is(err, integration.ErrCheckFailed) || r1Integrated.Checkpoint.State != integration.StatePreserved || r1Integrated.Checkpoint.ResumeState != integration.StateMerged || r1Integrated.Checkpoint.MergeCommit == "" {
		t.Fatalf("r1 failed Main check was not durably preserved after merge: state=%q resume=%q merge=%q err=%v", r1Integrated.Checkpoint.State, r1Integrated.Checkpoint.ResumeState, r1Integrated.Checkpoint.MergeCommit, err)
	}
	r1Path := integration.CheckpointPathForRecord(fixture.root, r1.Reference())
	r1Before, err := os.ReadFile(r1Path)
	if err != nil {
		t.Fatalf("read preserved r1 checkpoint: %v", err)
	}
	var r1Checkpoint integration.Checkpoint
	if err := json.Unmarshal(r1Before, &r1Checkpoint); err != nil {
		t.Fatal(err)
	}
	if r1Checkpoint.CanonicalMainFacts == nil || r1Checkpoint.CanonicalMainFacts.Binding.AssignmentRecord != r1.Reference() || r1Checkpoint.CanonicalMainFacts.Binding.ResultRevision != r1Result.ResultRevision || r1Checkpoint.CanonicalMainFacts.Binding.AnswerDigest != r1Result.AnswerDigest || len(r1Checkpoint.CheckReceipts) != 1 {
		t.Fatalf("failed r1 checkpoint lost its exact accepted Result/Main receipt binding: %+v", r1Checkpoint)
	}
	var failedCheck struct {
		Status string `json:"status"`
		Exit   *int   `json:"exit_code"`
	}
	failedReceipt, err := os.ReadFile(r1Checkpoint.CheckReceipts[0])
	if err != nil || json.Unmarshal(failedReceipt, &failedCheck) != nil || failedCheck.Status != "fail" || failedCheck.Exit == nil || *failedCheck.Exit == 0 {
		t.Fatalf("r1 checkpoint does not retain the actual failed Main check receipt: result=%+v err=%v bytes=%s", failedCheck, err, failedReceipt)
	}
	r1Snapshot := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	if _, err := buildsurface.Read(fixture.root, r1Snapshot.State); err == nil {
		t.Fatal("product surface accepted an unverified current r1 contribution")
	}

	r2 := registerCanonicalRevisionTwo(t, fixture, r1, "agent-s6-main-fixture-failed-r1-r2", canonicalSourceFile, []byte("main-check-marker\n"), "failed-r1-successful-r2")
	r2Inspection := fixture.inspect(t, "")
	if !r2Inspection.Ready || !reflect.DeepEqual(r2Inspection.ChangedPaths, []string{canonicalSourceFile}) {
		t.Fatalf("fresh r2 Inspect ready=%t changed paths=%v blockers=%v", r2Inspection.Ready, r2Inspection.ChangedPaths, r2Inspection.Blockers)
	}
	r2Integrated, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: r2Inspection}, fixture.integrateConfig())
	if err != nil || r2Integrated.Checkpoint.State != integration.StateVerified || r2Integrated.Checkpoint.CanonicalMainFacts == nil || r2Integrated.Checkpoint.CanonicalMainFacts.Binding.AssignmentRecord != r2.Reference() {
		t.Fatalf("r2 fresh Main integration = state=%q facts=%+v err=%v", r2Integrated.Checkpoint.State, r2Integrated.Checkpoint.CanonicalMainFacts, err)
	}
	if fileSize(t, fixture.counterPath) != 1 {
		t.Fatalf("r2 current Main check executions=%d, want one successful run after r1 failure", fileSize(t, fixture.counterPath))
	}
	if after, err := os.ReadFile(r1Path); err != nil || !reflect.DeepEqual(after, r1Before) {
		t.Fatalf("r2 changed immutable failed r1 checkpoint bytes: err=%v", err)
	}

	current := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	surface, err := buildsurface.Read(fixture.root, current.State)
	if err != nil {
		t.Fatalf("source-backed product surface after successful r2: %v", err)
	}
	if surface.StartCommit == "" || surface.FinalCommit != r2Integrated.Checkpoint.MergeCommit || len(surface.Tasks) != 1 || surface.Tasks[0].ID != canonicalTaskID || len(surface.Contributions) != 1 {
		t.Fatalf("product surface did not retain only the verified current contribution: %+v", surface)
	}
	contribution := surface.Contributions[0]
	if contribution.AssignmentRecord != r2.Reference() || contribution.ResultRef != r2Integrated.Checkpoint.CanonicalMainFacts.Binding.ResultPath || contribution.MergeCommit != r2Integrated.Checkpoint.MergeCommit {
		t.Fatalf("surface contribution is not the exact r2 Main proof: %+v", contribution)
	}
	if len(surface.Paths) != 2 || !surfacePathPresent(surface.Paths, canonicalSourceFile) || !surfacePathPresent(surface.Paths, r1RetainedPath) || !containsString(surface.UnattributedPaths, r1RetainedPath) {
		t.Fatalf("S7 product surface lost the repaired source or preserved residual path: paths=%+v unattributed=%v", surface.Paths, surface.UnattributedPaths)
	}

	catalog, err := transition.LoadCatalog(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := fileview.New(fixture.root, "refs/heads/main", catalog.Definition.FileSources)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := qualitygate.NewEvaluator(registry).Evaluate(ctx, qualitygate.Input{
		Root: fixture.root, Snapshot: current, Files: files,
		GateID: "GATE-BUILDER-BATCH-READY", TransitionID: "TR-006",
	})
	if err != nil || gate.Status != qualitygate.StatusSatisfied {
		t.Fatalf("original TR-006 gate after r2 = status=%q missing=%v conflicts=%v err=%v", gate.Status, gate.Missing, gate.Conflicts, err)
	}
	started, err := transition.Apply(fixture.root, fixture.statePath, fixture.journalPath, transition.Request{
		Files: files, TransitionID: "TR-006", ExpectedRevision: current.Revision,
		ExpectedRuntimeID: canonicalRuntimeID, Actor: "orchestrator",
		Evidence: map[string]string{"builder_report_record": canonicalEvidenceIDForPath(t, current.State, r2Integrated.Checkpoint.CanonicalMainFacts.Binding.ResultPath)},
		GateID:   gate.GateID, GateFingerprint: gate.Fingerprint,
		ProducerResponsibility: "delivery_orchestrator",
	})
	if err != nil {
		t.Fatalf("original TR-006 Writer after r2: %v", err)
	}
	draft, notes := review.DraftPlanForRoot(fixture.root, started.State, 1)
	if draft == nil || draft.ProductBaseline == nil || !reflect.DeepEqual(*draft.ProductBaseline, surface) {
		t.Fatalf("original S7 Draft lost the exact source-backed product surface: draft=%+v notes=%v", draft, notes)
	}
	if !surfacePathPresent(draft.ProductBaseline.Paths, r1RetainedPath) || len(draft.ProductBaseline.Contributions) != 1 || draft.ProductBaseline.Contributions[0].AssignmentRecord != r2.Reference() {
		t.Fatalf("S7 Draft omitted residual review scope or reintroduced failed r1 as a verified contribution: %+v", draft.ProductBaseline)
	}
}

func TestCanonicalSurfaceRetainsVerifiedHistoricalContribution(t *testing.T) {
	ctx := context.Background()
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, seedCanonicalS5AndApplyTR003)
	r1 := fixture.record
	r1Inspection := fixture.inspect(t, "")
	if !r1Inspection.Ready {
		t.Fatalf("r1 Inspect not ready: %v", r1Inspection.Blockers)
	}
	r1Integrated, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: r1Inspection}, fixture.integrateConfig())
	if err != nil || r1Integrated.Checkpoint.State != integration.StateVerified {
		t.Fatalf("verified r1 integration = state=%q err=%v", r1Integrated.Checkpoint.State, err)
	}
	r2 := registerCanonicalRevisionTwo(t, fixture, r1, "agent-s6-main-fixture-verified-r2", "src/pkg/revision-two.txt", []byte("verified historical test r2 output\n"), "verified-history-r2")
	r2Inspection := fixture.inspect(t, "")
	if !r2Inspection.Ready || !reflect.DeepEqual(r2Inspection.ChangedPaths, []string{"src/pkg/revision-two.txt"}) {
		t.Fatalf("r2 Inspect ready=%t changed paths=%v blockers=%v", r2Inspection.Ready, r2Inspection.ChangedPaths, r2Inspection.Blockers)
	}
	r2Integrated, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: r2Inspection}, fixture.integrateConfig())
	if err != nil || r2Integrated.Checkpoint.State != integration.StateVerified {
		t.Fatalf("r2 integration with verified r1 history = state=%q err=%v", r2Integrated.Checkpoint.State, err)
	}
	current := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	surface, err := buildsurface.Read(fixture.root, current.State)
	if err != nil {
		t.Fatalf("product surface with two verified revisions: %v", err)
	}
	if len(surface.Contributions) != 2 || surface.Contributions[0].AssignmentRecord != r1.Reference() || surface.Contributions[0].MergeCommit != r1Integrated.Checkpoint.MergeCommit || surface.Contributions[1].AssignmentRecord != r2.Reference() || surface.Contributions[1].MergeCommit != r2Integrated.Checkpoint.MergeCommit {
		t.Fatalf("verified historical Main contribution was not retained in original order: %+v", surface.Contributions)
	}
}

func registerCanonicalRevisionTwo(t *testing.T, fixture *canonicalMainFixture, previous assignmentrecord.Record, agentID, changePath string, content []byte, intent string) assignmentrecord.Record {
	t.Helper()
	manifestPath := filepath.Join(fixture.root, filepath.FromSlash(canonicalManifest))
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["manifest_id"] = "team-manifest-s6-main-fixture-" + intent
	manifest["version"] = "v1.0.2"
	manifest["workgroup_id"] = "workgroup-s6-main-fixture-" + intent
	manifest["platform_team_id"] = "platform-team-s6-main-fixture-" + intent
	assignments, ok := manifest["assignments"].([]any)
	if !ok || len(assignments) != 1 {
		t.Fatalf("manifest assignments = %#v", manifest["assignments"])
	}
	assignmentManifest, ok := assignments[0].(map[string]any)
	if !ok {
		t.Fatalf("manifest assignment = %#v", assignments[0])
	}
	assignmentManifest["agent_id"] = agentID
	updatedManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	updatedManifest = append(updatedManifest, '\n')
	if err := os.WriteFile(manifestPath, updatedManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, fixture.root, "add", canonicalManifest)
	gitFixture(t, fixture.root, "commit", "-m", "prepare fresh canonical S6 revision two manifest")

	gitFixture(t, fixture.root, "worktree", "remove", fixture.worker)
	gitFixture(t, fixture.root, "branch", "-D", canonicalBranch)
	gitFixture(t, fixture.root, "worktree", "add", "-b", canonicalBranch, fixture.worker, "main")
	if err := os.WriteFile(filepath.Join(fixture.worker, filepath.FromSlash(changePath)), content, 0o600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, fixture.worker, "add", "--", changePath)
	gitFixture(t, fixture.worker, "commit", "-m", "S6 fresh revision two source")

	before := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	previousSHA, err := previous.Digest()
	if err != nil {
		t.Fatal(err)
	}
	issued, err := assignment.Register(fixture.root, fixture.statePath, fixture.journalPath, assignment.Request{
		ExpectedRevision: before.Revision,
		ManifestPath:     manifestPath,
		TaskID:           previous.Task.ID,
		TaskPath:         filepath.Join(fixture.root, filepath.FromSlash(canonicalTask)),
		Revision: &assignment.RevisionRequest{
			AssignmentID: previous.AssignmentID, ExpectedRecord: previous.Reference(),
			ExpectedRecordSHA256: previousSHA, IntentKey: intent,
		},
		OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("original Register revision Writer: %v", err)
	}
	r2, found, err := assignmentrecord.ForOwner(issued.State, agentID)
	if err != nil || !found || r2.Revision != 2 || r2.AssignmentID != previous.AssignmentID {
		t.Fatalf("resolve current r2 Record: found=%t record=%+v err=%v", found, r2.Reference(), err)
	}
	planPath := writeCanonicalRevisionPlan(t, fixture.root, r2)
	var plan map[string]any
	planBytes, err := os.ReadFile(planPath)
	if err != nil || json.Unmarshal(planBytes, &plan) != nil {
		t.Fatalf("read canonical r2 PLAN for exact changed path: err=%v", err)
	}
	plan["planned_paths"] = []string{changePath}
	if r2.ClosingContract != nil {
		assertions := plan["assertion_checks"].([]any)
		assertions[0].(map[string]any)["done_when_indexes"] = []int{0}
	}
	writeJSONFixture(t, planPath, plan)
	if _, outcome, err := assignment.AgentBegin(fixture.root, fixture.statePath, fixture.journalPath, assignment.AgentBeginRequest{
		ExpectedRevision: -1, AgentID: r2.OwnerAgentID, PlanPath: planPath, OccurredAt: time.Now().UTC(),
	}); err != nil || !outcome.Chained {
		t.Fatalf("r2 original AgentBegin: outcome=%+v err=%v", outcome, err)
	}
	fixture.record = r2
	fixture.planPath = planPath
	fixture.result = fixture.acceptResult(t, "msg-s6-main-"+intent+"-result", "The new scoped revision was implemented and assessed.")
	return r2
}

func surfacePathPresent(paths []buildsurface.Path, want string) bool {
	for _, path := range paths {
		if path.Path == want {
			return true
		}
	}
	return false
}
