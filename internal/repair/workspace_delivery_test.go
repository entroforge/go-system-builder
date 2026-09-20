package repair_test

import (
	"context"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workspace"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceCandidateFreezesSourceAndRejectsDirtyOrStaleAuthority(t *testing.T) {
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
	contractRef, contractSHA := writeRuntimeContract(t, root)
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
	_, planReportRef, err := repair.CreatePlanReport(root, repair.PlanReportRequest{Session: sessionRef, Plan: planRef, AssignmentID: "repair-assignment-unit-1", AgentID: "builder-1", ReportID: "repair-plan-report-1", PlanText: "restore the payload authority", RedChecks: []repair.RepairCheck{{Name: "original failure", Command: "test -f internal/api/payload.go", Result: "fail", EvidenceRefs: []string{"test://red"}}}, ProposedPaths: []string{"internal/api/payload.go"}})
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
	e, err := b.Plan(ctx, started.State, "assignment-s9-unit-1", "builder-1", authority.Assignment.Scope, authority.Checks, authority.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	b.Executions[e.AssignmentID] = e
	if err = b.Materialize(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.Status = "ready"
	b.Executions[e.AssignmentID] = e
	writeFile(t, e.Path, "internal/api/payload.go", "package api\n")
	proposal := repair.RepairResultRequest{ResultID: "repair-result-worker", ProducerAgentID: e.AgentID, AssignmentID: e.AssignmentID, UnitResults: []repair.RepairUnitResult{{UnitID: "unit-1", Status: "pass", EvidenceRefs: []string{"test://unit"}}}, Checks: []repair.RepairCheck{{Name: "post-fix", Command: authority.Checks[0], Result: "pass", EvidenceRefs: []string{"test://green"}}}, Result: "pass"}
	if _, _, err = repair.CreateWorkspaceCandidate(ctx, root, started.State, e, proposal); err == nil {
		t.Fatal("untracked source was silently omitted")
	}
	committed, err := b.Commit(ctx, e, workspace.CommitRequest{ExpectedHead: e.BaseCommit, Message: "repair", Paths: []string{"internal/api/payload.go"}})
	if err != nil {
		t.Fatal(err)
	}
	badProposal := proposal
	badProposal.Checks = nil
	if _, _, err := repair.CreateWorkspaceCandidate(ctx, root, started.State, e, badProposal); err == nil {
		t.Fatal("invalid final result shape accepted before merge")
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
	if _, exists, err := repair.RegisteredWorkspaceResult(root, finalized.State, c); err != nil || !exists {
		t.Fatalf("verified result cannot resume: %v", err)
	}
	if finalized.State["lifecycle"].(map[string]any)["state"] != "bug_resolution" {
		t.Fatal("candidate skipped independent verification/fresh S7")
	}
	e.Generation++
	if _, err = repair.ValidateWorkspaceCandidate(root, started.State, e, ref); err == nil {
		t.Fatal("stale execution accepted")
	}
	e.Generation--
	pointer := started.State["review"].(map[string]any)["repair"].(map[string]any)
	pointer["status"] = "planning"
	if _, err = repair.ValidateWorkspaceCandidate(root, started.State, e, ref); err == nil {
		t.Fatal("revoked execution authority accepted")
	}
}
