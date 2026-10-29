package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// This tests original Writer/CAS acceptance with captured unit-fixture bytes.
// The separate integration tests prove actual Main execution. It makes no
// claim of a successful isolated Worker process on this restricted host.
func TestWorkspaceToolReceiptOriginalWriterAndCheckpointProvenance(t *testing.T) {
	f := newRuntimeFixture(t)
	f.state["schema_version"] = "1.2.0"
	f.state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": false}
	f.addAgent("builder", "builder", "working", "workgroup", nil)
	for _, args := range [][]string{{"init", "-b", "test2"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
		if _, err := runGit(t, f.root, args...); err != nil {
			t.Fatal(err)
		}
	}
	bindFixtureWorkspace(t, f)
	b, err := workspace.New(context.Background(), f.root)
	if err != nil {
		t.Fatal(err)
	}
	p := checkexec.Profile{ID: "CP-OWN", Version: "v1", CheckIndex: 0, Command: "true", Location: "worker", TimeoutSeconds: 1, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never"}
	e := workspace.Execution{AssignmentID: "assignment", RuntimeID: workspace.RuntimeID(f.state), BaselineGeneration: workspace.Generation(f.state), Generation: 1, AgentID: "builder", Path: filepath.Join(f.root, ".worktrees/worker"), Branch: "codex/worker", TargetBranch: b.Branch, BaseCommit: b.BoundHead, Status: "preparing", CheckLocation: "worker", WritePaths: []string{}, Checks: []string{p.Command}, Inputs: map[string]string{}, CheckProfiles: []checkexec.Profile{p}}
	if err := b.Materialize(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{".claude/agents", ".claude/skills"} {
		if err := os.MkdirAll(filepath.Join(f.root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Bootstrap(e, executable); err != nil {
		t.Fatal(err)
	}
	e.BootstrapSHA256, err = workspace.BootstrapDigest(e)
	if err != nil {
		t.Fatal(err)
	}
	e.Status = "ready"
	b.Executions[e.AssignmentID] = e
	f.state["workspace"] = workspace.Encode(b)
	f.persist(t)
	observation, err := checkexec.ObserveProfile(context.Background(), e.Path, p)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.root, ".claude/evidence/workspace-checks/unit-fixture")
	os.MkdirAll(dir, 0700)
	log := filepath.Join(dir, "output.log")
	os.WriteFile(log, []byte("unit fixture output"), 0600)
	data, err := checkexec.SealReceipt(map[string]any{"version": 2, "command": p.Command, "status": "pass", "output": log, "profile_observation": observation, "check_binding": workspace.CheckBinding(e)}, log)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "receipt.json")
	os.WriteFile(path, data, 0600)
	var ref checkexec.ToolReceipt
	ctx := checkexec.WithReceiptCapture(checkexec.WithBinding(checkexec.WithProfile(context.Background(), p), workspace.CheckBinding(e)), func(r checkexec.ToolReceipt) { ref = r })
	if err := checkexec.CaptureReceipt(ctx, path, e.Path, data); err != nil {
		t.Fatal(err)
	}
	store := runtime.NewStore(f.statePath, f.journalPath)
	before, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := acceptWorkspaceToolReceipt(f.root, e, ref); err != nil {
		t.Fatal(err)
	}
	after, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision+1 {
		t.Fatalf("receipt did not use original CAS: before=%d after=%d", before.Revision, after.Revision)
	}
	registered, err := workspace.Decode(after.State)
	if err != nil || len(registered.Executions[e.AssignmentID].ToolCheckReceipts) != 1 {
		t.Fatal("receipt not registered", err)
	}
	journal, err := os.ReadFile(f.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ValidateCheckpointIndices(f.root, after.State, journal); err != nil {
		t.Fatal("original receipt could not survive checkpoint", err)
	}
	if err := runtime.ValidateCheckpointIndices(f.root, after.State, nil); err == nil {
		t.Fatal("receipt became accepted without original journal")
	}
	if err := acceptWorkspaceToolReceipt(f.root, e, ref); err != nil {
		t.Fatal("receipt replay not idempotent", err)
	}
	replayed, err := store.Snapshot()
	if err != nil || replayed.Revision != after.Revision {
		t.Fatal("receipt replay changed revision", err)
	}
	// Change an invocation identity without changing registered authority.
	stale := e
	stale.Generation++
	if err := acceptWorkspaceToolReceipt(f.root, stale, ref); err == nil {
		t.Fatal("stale execution was registered")
	}
	data, _ = json.Marshal(after.State["lifecycle"])
	original, _ := json.Marshal(before.State["lifecycle"])
	if string(data) != string(original) {
		t.Fatal("tool facts advanced lifecycle")
	}
}
