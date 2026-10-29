package workspace

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestCanonicalWorkerChecksStaySeparateFromMainCheckProjection(t *testing.T) {
	f := newOwnedCheckAssignmentWorkspaceFixture(t)
	e, err := f.binding.Plan(f.ctx, f.state, f.record.AssignmentID, f.record.OwnerAgentID, f.record.EffectiveWritePaths, f.record.RequiredChecks, []string{f.record.Task.Ref})
	if err != nil {
		t.Fatal(err)
	}
	e, err = f.binding.FreezeCheckProfiles(e, f.record.CheckProfiles)
	if err != nil {
		t.Fatal(err)
	}
	if e.CheckLocation != "main" || len(e.Checks) != 1 || len(e.WorkerChecks) != 1 || e.Checks[0] == e.WorkerChecks[0].Command {
		t.Fatalf("Main checks and owner-run checks were merged: %+v", e)
	}
	if err := ValidateAssignmentRecord(f.state, e); err != nil {
		t.Fatalf("frozen execution differs from the original Assignment Record: %v", err)
	}
	if err := f.binding.ValidateInputs(e); err != nil {
		t.Fatalf("frozen owner profile inputs: %v", err)
	}

	dispatch, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, f.record)
	if err != nil {
		t.Fatal(err)
	}
	e, err = f.binding.FreezeDispatchContext(e, dispatch, nil)
	if err != nil {
		t.Fatal(err)
	}
	contextProjection, err := f.binding.ReadDispatchContext(e)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(contextProjection.WorkerChecks, e.WorkerChecks) || !reflect.DeepEqual(contextProjection.Dispatch.ClosingContract.WorkerChecks, e.WorkerChecks) {
		t.Fatal("frozen context lost or changed the Record-owned WorkerChecks")
	}
	if commands := workspaceCheckCommands(e); len(commands) != 0 {
		t.Fatalf("Main execution exposed Worker commands for Main-owned RequiredChecks: %v", commands)
	}
	commands := ownedCheckCommands(e)
	if len(commands) != 1 || !strings.Contains(commands[0], " --owned-check 0") || strings.Contains(commands[0], " --check ") {
		t.Fatalf("prepare/launch did not expose the separate frozen WorkerCheck adapter: %v", commands)
	}
	changed := dispatch
	changed.ClosingContract = copyWorkerContract(dispatch.ClosingContract)
	changed.ClosingContract.WorkerChecks[0].Command = "false"
	if _, err := f.binding.FreezeDispatchContext(e, changed, nil); err == nil {
		t.Fatal("Dispatch context accepted a changed Record-owned check")
	}
}

func TestOwnedReceiptAcceptsCapturedFailedFactAndKeepsOnlyLatestForReuse(t *testing.T) {
	f := newOwnedCheckAssignmentWorkspaceFixture(t)
	e, err := f.binding.Plan(f.ctx, f.state, f.record.AssignmentID, f.record.OwnerAgentID, f.record.EffectiveWritePaths, f.record.RequiredChecks, []string{f.record.Task.Ref})
	if err != nil {
		t.Fatal(err)
	}
	e, err = f.binding.FreezeCheckProfiles(e, f.record.CheckProfiles)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.binding.Materialize(f.ctx, e); err != nil {
		t.Fatalf("materialize frozen owner check checkout: %v", err)
	}
	e.BootstrapSHA256 = "fixture-bootstrap"
	first := captureOwnedReceiptFixture(t, f.binding, e, 0, "pass", false)
	if err := f.binding.AcceptOwnedCheckReceipt(&e, 0, first); err != nil {
		t.Fatalf("accept process-captured WorkerCheck receipt: %v", err)
	}
	if err := f.binding.AcceptOwnedCheckReceipt(&e, 0, first); err != nil || len(e.OwnedCheckReceipts) != 1 {
		t.Fatalf("idempotent receipt receive changed history: %v rows=%d", err, len(e.OwnedCheckReceipts))
	}
	second := captureOwnedReceiptFixture(t, f.binding, e, 0, "fail", false)
	if err := f.binding.AcceptOwnedCheckReceipt(&e, 0, second); err != nil {
		t.Fatalf("accept captured failed check as a machine fact: %v", err)
	}
	if len(e.OwnedCheckReceipts) != 2 || e.OwnedCheckReceipts[0].Receipt.SHA256 != first.SHA256 || e.OwnedCheckReceipts[1].Receipt.SHA256 != second.SHA256 {
		t.Fatal("owned receipt history was rewritten or evicted")
	}
	latest := latestOwnedCheckReceipts(e.OwnedCheckReceipts, 0)
	if len(latest) != 1 || latest[0] != second {
		t.Fatalf("reuse did not select the latest registered result: %+v", latest)
	}
	duplicate := e
	duplicate.OwnedCheckReceipts = append(append([]OwnedCheckReceipt(nil), e.OwnedCheckReceipts...), e.OwnedCheckReceipts[0])
	candidate := *f.binding
	candidate.Executions = map[string]Execution{e.AssignmentID: duplicate}
	if err := candidate.ValidateAuthority(f.state); err == nil {
		t.Fatal("Runtime projection accepted a duplicate owned receipt reference")
	}

	bad := captureOwnedReceiptFixture(t, f.binding, e, 0, "fail", true)
	if err := f.binding.AcceptOwnedCheckReceipt(&e, 0, bad); err == nil {
		t.Fatal("receipt with an owner tuple inconsistent with the current execution was accepted")
	}

	full := e
	full.OwnedCheckReceipts = make([]OwnedCheckReceipt, 1024)
	for i := range full.OwnedCheckReceipts {
		full.OwnedCheckReceipts[i] = OwnedCheckReceipt{WorkerCheckIndex: 0, Receipt: first}
	}
	third := captureOwnedReceiptFixture(t, f.binding, e, 0, "fail", false)
	if err := f.binding.AcceptOwnedCheckReceipt(&full, 0, third); err == nil || !strings.Contains(err.Error(), "full") {
		t.Fatalf("owned receipt index overflow was silently accepted/evicted: %v", err)
	}
	if len(full.OwnedCheckReceipts) != 1024 {
		t.Fatal("overflow handling rewrote or evicted receipt history")
	}
}

func TestOwnedCheckObservationUsesTheRunnerDisposableCopy(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "owned-copy-test"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}} {
		if _, err := Git(context.Background(), root, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "input.md"), []byte("source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "excluded"), []byte("excluded\n"), 0600); err != nil {
		t.Fatal(err)
	}
	profile := checkexec.Profile{ID: "owned-copy-profile", Version: "1.0.0", CheckIndex: 0, Command: "true", Location: "worker", TimeoutSeconds: 10, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never"}
	canonical, err := Canonical(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, observation, err := ObserveOwnedCheckSource(context.Background(), canonical, profile)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := CheckSnapshotDigest(context.Background(), canonical)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot != hash || len(snapshot) != 64 || observation.ProfileDigest != profile.Digest() || observation.SubjectDigest == snapshot {
		t.Fatalf("collector and runner copy observations diverged: snapshot=%s digest=%s observation=%+v", snapshot, hash, observation)
	}
}

func TestRunOwnedCheckCapturesTheOriginalIsolatedRunnerReceipt(t *testing.T) {
	f := newOwnedCheckAssignmentWorkspaceFixture(t)
	e, err := f.binding.Plan(f.ctx, f.state, f.record.AssignmentID, f.record.OwnerAgentID, f.record.EffectiveWritePaths, f.record.RequiredChecks, []string{f.record.Task.Ref})
	if err != nil {
		t.Fatal(err)
	}
	e, err = f.binding.FreezeCheckProfiles(e, f.record.CheckProfiles)
	if err != nil {
		t.Fatal(err)
	}
	e.Status = "ready"
	if err := f.binding.Materialize(f.ctx, e); err != nil {
		t.Fatalf("materialize owner check checkout: %v", err)
	}
	for path, body := range map[string]string{
		".claude/agents/backend-builder.md":           "---\nname: backend-builder\ndescription: fixture\n---\nBuild the registered TASK.\n",
		".claude/skills/agent-dispatch/SKILL.md":      "# dispatch fixture\n",
		".claude/skills/backend-engineering/SKILL.md": "# backend fixture\n",
		".claude/skills/code-quality/SKILL.md":        "# quality fixture\n",
		".claude/skills/testing-strategy/SKILL.md":    "# testing fixture\n",
	} {
		full := filepath.Join(f.binding.MainRoot, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.binding.Bootstrap(e, executable); err != nil {
		t.Fatalf("bootstrap owner check execution: %v", err)
	}
	e.BootstrapSHA256, err = BootstrapDigest(e)
	if err != nil {
		t.Fatal(err)
	}
	f.binding.Executions[e.AssignmentID] = e
	f.state["workspace"] = Encode(f.binding)
	saveControlState(t, f.binding.MainRoot, f.state)

	result, runErr := f.binding.RunOwnedCheck(context.Background(), e, 0, io.Discard)
	if result.Attempt != nil {
		defer result.Attempt.Close()
	}
	if result.ReceiptPath == "" || result.ToolReceipt.Path != result.ReceiptPath || result.Attempt == nil {
		t.Fatalf("original isolated runner did not return its process-captured receipt: result=%+v err=%v", result, runErr)
	}
	verified, err := checkexec.VerifyToolReceipt(filepath.Join(f.binding.MainRoot, ".claude/evidence/workspace-checks"), result.ToolReceipt)
	if err != nil || verified.Passed != result.Passed {
		t.Fatalf("captured owner receipt does not verify its actual result: passed=%t runerr=%v verify=%v", result.Passed, runErr, err)
	}
	if verified.Observation.Permissions == nil || verified.Observation.ReuseEligible {
		t.Fatal("original owner runner omitted permission facts or enabled unproved reuse")
	}
	if verified.Observation.Permissions.Available && runErr == nil && !verified.Observation.Permissions.Known() {
		t.Fatal("available isolation was not recorded as known")
	}
	if !verified.Observation.Permissions.Available && (runErr == nil || result.Passed) {
		t.Fatal("unavailable original isolation used a fallback or became PASS")
	}
	if !verified.Observation.Permissions.Available {
		t.Logf("host isolation denial remained a failed captured fact: %s", verified.Observation.Permissions.Diagnostic)
	}
	if err := f.binding.AcceptOwnedCheckReceipt(&e, 0, result.ToolReceipt); err != nil {
		t.Fatalf("original Workspace Writer helper rejected the captured owner receipt: %v", err)
	}
	if len(e.OwnedCheckReceipts) != 1 || e.OwnedCheckReceipts[0].Receipt.SHA256 != result.ToolReceipt.SHA256 {
		t.Fatal("original Workspace receipt helper did not append the captured fact")
	}
	if err := result.Attempt.Finish(result.ReceiptPath, runErr); err != nil {
		t.Fatalf("finish original owned-check diagnostic: %v", err)
	}
}

func captureOwnedReceiptFixture(t *testing.T, binding *ExecutionRegistry, e Execution, index int, status string, corruptOwner bool) checkexec.ToolReceipt {
	t.Helper()
	check := e.WorkerChecks[index]
	observation, err := checkexec.ObserveProfile(checkexec.WithProfile(context.Background(), check.Profile), e.Path, check.Profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(binding.MainRoot, ".claude/evidence/workspace-checks"), 0700); err != nil {
		t.Fatal(err)
	}
	// Receipts are captured under the exact evidence root accepted by the
	// original Workspace Writer.
	dir, err := os.MkdirTemp(filepath.Join(binding.MainRoot, ".claude/evidence/workspace-checks"), "owned-check-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "output.log")
	if err := os.WriteFile(logPath, []byte("fixture output\n"), 0600); err != nil {
		t.Fatal(err)
	}
	owner := e.AgentID
	if corruptOwner {
		owner = "agent-other"
	}
	snapshot := checkexec.Hash([]byte("actual copied source fixture"))
	snapshotFacts := map[string]any{"runtime_id": e.RuntimeID, "baseline_generation": e.BaselineGeneration, "assignment_id": e.AssignmentID, "owner_agent_id": owner, "execution_generation": e.Generation, "bootstrap_sha256": "fixture-bootstrap"}
	raw := map[string]any{
		"version": 2, "runtime_id": e.RuntimeID, "baseline_generation": e.BaselineGeneration,
		"assignment_id": e.AssignmentID, "execution_generation": e.Generation,
		"check_index": index, "command": check.Command, "snapshot_sha256": snapshot,
		"started_at": "2026-10-14T00:00:00Z", "elapsed_ms": 1, "status": status,
		"error": "", "output": logPath, "disposable_copy": true,
		"check_binding": CheckBinding(e), "profile_observation": observation,
		"assignment_record": e.AssignmentRecord, "assignment_record_sha256": e.AssignmentRecordSHA256,
		"worker_check_index": index, "worker_check_contract": "s6-closing-v1",
		"worker_check_snapshot": snapshotFacts,
	}
	data, err := checkexec.SealReceipt(raw, logPath)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "receipt.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var ref checkexec.ToolReceipt
	ctx := checkexec.WithReceiptCapture(checkexec.WithBinding(checkexec.WithProfile(context.Background(), check.Profile), CheckBinding(e)), func(value checkexec.ToolReceipt) { ref = value })
	if err := checkexec.CaptureReceipt(ctx, path, e.Path, data); err != nil {
		t.Fatal(err)
	}
	if ref.Path == "" {
		t.Fatal("fixture did not create a process-captured tool reference")
	}
	return ref
}

func copyWorkerContract(source *assignmentrecord.ClosingContract) *assignmentrecord.ClosingContract {
	data, _ := json.Marshal(source)
	var copied assignmentrecord.ClosingContract
	_ = json.Unmarshal(data, &copied)
	return &copied
}
