package repair_test

import (
	"context"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceCandidateFreezesAttemptAndPreservesHistoricalReads(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	ctx := context.Background()
	for _, args := range [][]string{{"init", "-b", "test2"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}} {
		if _, err := workspace.Git(ctx, root, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".claude/\n.worktrees/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(ctx, root, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(ctx, root, "commit", "-m", "baseline"); err != nil {
		t.Fatal(err)
	}
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	if _, err := workspace.Git(ctx, root, "add", "docs/requirements"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(ctx, root, "commit", "-m", "requirement input"); err != nil {
		t.Fatal(err)
	}
	contractRef, contractSHA := writeRuntimeMigrationContract(t, root)
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRef.Path, "repair_contract_sha256": contractSHA}
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	_, _, sessionRef, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "main"}, SessionID: "repair-session-1", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, planRef, err := repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 1, Actor: "main"}, PlanID: "repair-plan-1", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if inside, err := workspace.Git(ctx, root, "rev-parse", "--is-inside-work-tree"); err != nil || inside != "true" {
		t.Fatalf("actual passing precheck: %q %v", inside, err)
	}
	_, planReportRef, err := repair.CreatePlanReport(root, repair.PlanReportRequest{Session: sessionRef, Plan: planRef, AssignmentID: "repair-assignment-unit-1", AgentID: "builder-1", ReportID: "repair-plan-report-1", PlanText: "restore the payload authority", RedChecks: []repair.RepairCheck{{Name: "actual passing precheck", Command: "git rev-parse --is-inside-work-tree", Result: "pass", EvidenceRefs: []string{"test://actual-precheck"}}}, ProposedPaths: []string{"internal/api/payload.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repair.SubmitRepairPlanReportToRuntime(root, statePath, journalPath, repair.SubmitPlanReportRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 2, Actor: "builder-1"}, Report: planReportRef}); err != nil {
		t.Fatal(err)
	}
	started, err := repair.BeginRepairExecution(root, statePath, journalPath, repair.BeginRepairExecutionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 3, Actor: "main"}})
	if err != nil {
		t.Fatal(err)
	}

	authority, err := repair.ResolveWorkspaceAuthority(root, started.State, "assignment-s9-unit-1", "builder-1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := workspace.New(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"agents", "skills"} {
		if err := os.MkdirAll(filepath.Join(root, ".claude", dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	e, err := b.Plan(ctx, started.State, "assignment-s9-unit-1", "builder-1", authority.Assignment.Scope, []string{"test -f internal/api/payload.go && test -d internal/api"}, authority.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	b.Executions[e.AssignmentID] = e
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	persistWorkspace := func(event string) (loopruntime.Snapshot, error) {
		current, err := store.Snapshot()
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		key := fmt.Sprintf("fixture-workspace-%s-r%d", event, current.Revision+1)
		return store.Update(current.Revision, loopruntime.Mutation{
			EventID: key, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "main",
			RuntimeID: e.RuntimeID, BaselineGeneration: e.BaselineGeneration,
			IdempotencyKey: key, RetainLastTransition: true, OccurredAt: time.Now().UTC(),
			Message: "persist original S9 Worker execution registry",
			Apply: func(state map[string]any) error {
				state["workspace"] = workspace.Encode(b)
				return nil
			},
		})
	}
	if _, err := persistWorkspace("preparing"); err != nil {
		t.Fatal(err)
	}
	if err = b.Materialize(ctx, e); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Bootstrap(e, executable); err != nil {
		t.Fatal(err)
	}
	e.BootstrapSHA256, err = workspace.BootstrapDigest(e)
	if err != nil {
		t.Fatal(err)
	}
	e.Status = "ready"
	b.Executions[e.AssignmentID] = e
	started, err = persistWorkspace("ready")
	if err != nil {
		t.Fatal(err)
	}
	proposal := repair.RepairResultRequest{ResultID: "repair-result-worker", ProducerAgentID: e.AgentID, AssignmentID: e.AssignmentID, UnitResults: []repair.RepairUnitResult{{UnitID: "unit-1", Status: "pass", EvidenceRefs: []string{"test://unit"}}}, Checks: []repair.RepairCheck{{Name: "post-fix", Command: e.Checks[0], Result: "pass", EvidenceRefs: []string{"test://green"}}}, Result: "pass"}
	proposal.ScopeDeviations = []string{"The precheck assessed repository availability; payload behavior was not checked before implementation."}
	noOp, noOpRef, err := repair.CreateWorkspaceCandidate(ctx, root, started.State, e, proposal)
	if err != nil {
		t.Fatalf("clean no-op candidate without migration/rollback references: %v", err)
	}
	if noOp.SourceHead != e.BaseCommit || len(noOp.Proposal.ChangedArtifacts) != 0 || noOp.Proposal.MigrationRef != "" || noOp.Proposal.RollbackRef != "" {
		t.Fatalf("no-op candidate lost its actual HEAD/empty diff: %+v", noOp)
	}
	if _, err := repair.ValidateWorkspaceCandidate(root, started.State, e, noOpRef); err != nil {
		t.Fatalf("no-op candidate does not match its actual Worker: %v", err)
	}
	// This assertion stops at the candidate boundary. No-op integration is
	// deliberately not exercised by this original changed-source flow below.
	writeFile(t, e.Path, "internal/api/payload.go", "package api\n")
	if _, _, err = repair.CreateWorkspaceCandidate(ctx, root, started.State, e, proposal); err == nil {
		t.Fatal("untracked source was silently omitted")
	}
	committed, err := b.Commit(ctx, e, workspace.CommitRequest{ExpectedHead: e.BaseCommit, Message: "repair", Paths: []string{"internal/api/payload.go"}})
	if err != nil {
		t.Fatal(err)
	}
	badProposal := proposal
	badProposal.Checks = nil
	// Missing Main check facts do not make the Builder's candidate unreadable.
	// Recording this draft must not promote it to an accepted Final.
	pending, _, err := repair.CreateWorkspaceCandidate(ctx, root, started.State, e, badProposal)
	if err != nil || len(pending.Proposal.Checks) != 0 {
		t.Fatalf("candidate invented Main checks or rejected truthful missing work: %+v %v", pending, err)
	}
	if _, exists, err := repair.RegisteredWorkspaceResult(root, started.State, pending); err != nil || exists {
		t.Fatalf("candidate without Main checks promoted itself to Final: %t %v", exists, err)
	}
	c, ref, err := repair.CreateWorkspaceCandidate(ctx, root, started.State, e, proposal)
	if err != nil {
		t.Fatal(err)
	}
	if c.SourceHead != committed.Commit || c.Proposal.ChangedArtifacts[0].SHA256 != fileHash([]byte("package api\n")) {
		t.Fatalf("candidate not bound to Git blob: %+v", c)
	}
	_, again, err := repair.CreateWorkspaceCandidate(ctx, root, started.State, e, proposal)
	if err != nil || again != ref {
		t.Fatalf("candidate retry changed identity: %+v %v", again, err)
	}
	if _, err = repair.ValidateWorkspaceCandidate(root, started.State, e, ref); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := repair.RegisteredWorkspaceResult(root, started.State, c); err != nil || exists {
		t.Fatalf("candidate promoted itself to result: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, "internal/api/payload.go")); !os.IsNotExist(err) {
		t.Fatal("candidate wrote Main product")
	}
	inspect, err := integration.Inspect(ctx, integration.InspectRequest{Root: root, RuntimeID: e.RuntimeID, BaselineGeneration: e.BaselineGeneration, TargetBranch: e.TargetBranch, Assignment: hookctx.AssignmentContext{AssignmentID: e.AssignmentID, OwnerAgentID: e.AgentID, WorktreePath: e.Path, Branch: e.Branch, TargetBranch: e.TargetBranch, WritePaths: e.WritePaths}}, integration.InspectConfig{ValidateDelivery: func(_ context.Context, in integration.Inspection) error {
		if in.SourceHead != c.SourceHead {
			return fmt.Errorf("source drift")
		}
		_, err := repair.ValidateWorkspaceCandidate(root, started.State, e, ref)
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	integrated, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspect}, integration.IntegrateConfig{Root: root, GitRoot: root, RuntimeID: e.RuntimeID, RequiredChecks: e.Checks, CheckRunner: integration.CommandCheckRunner})
	if err != nil || integrated.Checkpoint.State != integration.StateVerified {
		t.Fatalf("integration failed: %+v %v", integrated, err)
	}
	finalized, result, _, err := repair.SubmitRepairResultToRuntime(root, statePath, journalPath, repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: started.Revision, Actor: e.AgentID}, Result: c.Proposal})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result != "pass" {
		t.Fatalf("domain result=%s", result.Result)
	}
	if len(result.ScopeDeviations) != 1 || result.ScopeDeviations[0] != proposal.ScopeDeviations[0] {
		t.Fatalf("domain Result lost the actual reported deviation: %+v", result.ScopeDeviations)
	}
	if len(result.BeforeFixChecks) != 1 || result.BeforeFixChecks[0].Result != "pass" {
		t.Fatalf("domain Result did not retain the actual passing precheck: %+v", result.BeforeFixChecks)
	}
	if _, exists, err := repair.RegisteredWorkspaceResult(root, finalized.State, c); err != nil || !exists {
		t.Fatalf("verified result cannot resume: %v", err)
	}
	if finalized.State["lifecycle"].(map[string]any)["state"] != "bug_resolution" {
		t.Fatal("candidate skipped independent verification/fresh S7")
	}
	// Recording the actual diff must also work when Main needs to assess a
	// change outside the proposed scope. This is not integration approval.
	writeFile(t, e.Path, "outside-assignment.txt", "actual deviation\n")
	if _, err := workspace.Git(ctx, e.Path, "add", "outside-assignment.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(ctx, e.Path, "commit", "-m", "record actual deviation"); err != nil {
		t.Fatal(err)
	}
	deviationProposal := proposal
	deviationProposal.ScopeDeviations = []string{"Main must assess outside-assignment.txt"}
	deviation, _, err := repair.CreateWorkspaceCandidate(ctx, root, started.State, e, deviationProposal)
	if err != nil {
		t.Fatalf("actual out-of-scope diff could not be recorded: %v", err)
	}
	foundDeviation := false
	for _, artifact := range deviation.Proposal.ChangedArtifacts {
		foundDeviation = foundDeviation || artifact.Path == "outside-assignment.txt"
	}
	if !foundDeviation {
		t.Fatal("candidate omitted actual out-of-scope diff")
	}
	e.Generation++
	if _, err = repair.ValidateWorkspaceCandidate(root, started.State, e, ref); err == nil {
		t.Fatal("stale execution accepted")
	}
	e.Generation--
	pointer := started.State["review"].(map[string]any)["repair"].(map[string]any)
	pointer["status"] = "planning"
	if _, err = repair.ValidateWorkspaceCandidate(root, started.State, e, ref); err != nil {
		t.Fatalf("a phase label erased the existing candidate history: %v", err)
	}
	// Actual replacement of the recorded execution attempt retires this
	// candidate's current association. A phase word alone grants no work ACL.
	replacement := e
	replacement.Generation++
	b.Executions[e.AssignmentID] = replacement
	started.State["workspace"] = workspace.Encode(b)
	if _, err = repair.ValidateWorkspaceCandidate(root, started.State, e, ref); err == nil {
		t.Fatal("candidate borrowed a replacement execution attempt")
	}
}
