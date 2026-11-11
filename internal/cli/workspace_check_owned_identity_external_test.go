package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// This is a synthetic captured-receipt fixture for the real original Writer,
// not a claim that an isolated runner executed. The end-to-end S6 test covers
// the real runner separately on hosts with isolation capability.
func TestWorkspaceOwnedCheckWriterAcceptsDistinctSameIndexAndReplays(t *testing.T) {
	root, task, taskPath := newS6RecordContextFixture(t)
	const manifestRef = ".claude/workgroups/manifest-s6-native-context.json"
	const owner = "agent-s6-native-context"
	var manifest map[string]any
	body, err := os.ReadFile(filepath.Join(root, manifestRef))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	row := manifest["assignments"].([]any)[0].(map[string]any)
	row["integration_check_mode"] = "post_merge"
	row["check_profiles"].([]any)[0].(map[string]any)["location"] = "main"
	profile := checkexec.Profile{ID: "CP-OWN-IDENTITY", Version: "v1", CheckIndex: 0, Command: "true", Location: "worker", TimeoutSeconds: 1, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never"}
	row["closing_contract"] = map[string]any{"contract": assignmentrecord.ClosingContractVersion, "worker_checks": []any{map[string]any{"index": 0, "command": profile.Command, "profile": profile}}}
	stageJSON(t, root, manifestRef, manifest)
	stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", manifestRef, "--task-id", task, "--task", taskPath)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	initial := stageSnapshot(t, root)
	registry, err := workspace.Decode(initial.State)
	if err != nil {
		t.Fatal(err)
	}
	record, found, err := assignmentrecord.ForOwner(initial.State, owner)
	if err != nil || !found {
		t.Fatalf("original record: %t %v", found, err)
	}
	e, err := registry.Plan(context.Background(), initial.State, record.AssignmentID, record.OwnerAgentID, record.EffectiveWritePaths, record.RequiredChecks, []string{record.Task.Ref})
	if err != nil {
		t.Fatal(err)
	}
	e, err = registry.FreezeCheckProfiles(e, record.CheckProfiles)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Materialize(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Bootstrap(e, executable); err != nil {
		t.Fatal(err)
	}
	e.Status = "ready"
	registry.Executions[e.AssignmentID] = e
	initial.State["workspace"] = workspace.Encode(registry)
	// Explicitly construct a valid historical ready/working snapshot. This is
	// test setup only: no approval, check receipt, result or PASS is fabricated.
	for _, raw := range initial.State["entities"].(map[string]any)["agents"].([]any) {
		agent, _ := raw.(map[string]any)
		if agent["id"] == owner {
			if err := workstate.RecordAgentActivity(initial.State, agent, "working", "work_started"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := registry.ValidateAuthority(initial.State); err != nil {
		t.Fatalf("synthetic historical fixture invalid: %v", err)
	}
	stageJSON(t, root, ".claude/loop-state.json", initial.State)
	observation, err := checkexec.ObserveProfile(context.Background(), e.Path, profile)
	if err != nil {
		t.Fatal(err)
	}
	capture := func(name, status string) checkexec.ToolReceipt {
		t.Helper()
		dir := filepath.Join(root, ".claude/evidence/workspace-checks", name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		log := filepath.Join(dir, "output.log")
		if err := os.WriteFile(log, []byte("synthetic owned receipt"), 0600); err != nil {
			t.Fatal(err)
		}
		data, err := checkexec.SealReceipt(map[string]any{"version": 2, "command": profile.Command, "status": status, "output": log, "check_binding": workspace.CheckBinding(e), "profile_observation": observation, "check_index": 0, "worker_check_index": 0, "assignment_record": e.AssignmentRecord, "worker_check_snapshot": map[string]any{"runtime_id": e.RuntimeID, "baseline_generation": e.BaselineGeneration, "assignment_id": e.AssignmentID, "owner_agent_id": e.AgentID, "execution_generation": e.Generation}})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "receipt.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		var ref checkexec.ToolReceipt
		ctx := checkexec.WithReceiptCapture(checkexec.WithBinding(checkexec.WithProfile(context.Background(), profile), workspace.CheckBinding(e)), func(value checkexec.ToolReceipt) { ref = value })
		if err := checkexec.CaptureReceipt(ctx, path, e.Path, data); err != nil {
			t.Fatal(err)
		}
		return ref
	}
	one, two, three := capture("owned-one", "fail"), capture("owned-two", "pass"), capture("owned-three", "fail")
	four, five := capture("owned-four", "pass"), capture("owned-five", "fail")
	before := stageSnapshot(t, root)
	// Simulate a genuinely registered pre-upgrade event and receipt. Replaying
	// that old fact must not depend on recomputing its old event identity.
	coordinate := fmt.Sprintf("owned-check-%s-%s-g%d-check0", e.AssignmentID, e.AgentID, e.Generation)
	writer := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{})
	_, err = writer.Update(before.Revision, runtime.Mutation{EventID: "evt-workspace-" + coordinate, TransitionID: "WORKSPACE-OWNED-CHECK", Event: "workspace_owned_check_received", Actor: owner, RuntimeID: e.RuntimeID, IdempotencyKey: "runtime:workspace:" + e.RuntimeID + ":" + coordinate, RetainLastTransition: true, ProducerResponsibility: "owned_check", Apply: func(state map[string]any) error {
		b, err := workspace.Decode(state)
		if err != nil {
			return err
		}
		current := b.Executions[e.AssignmentID]
		if err := b.AcceptOwnedCheckReceipt(&current, 0, one); err != nil {
			return err
		}
		b.Executions[e.AssignmentID] = current
		state["workspace"] = workspace.Encode(b)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.ExportTestWorkspaceOwnedReceipt(root, e, 0, one); err != nil {
		t.Fatalf("old owned receipt replay: %v", err)
	}
	if err := cli.ExportTestWorkspaceOwnedReceipt(root, e, 0, two); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, ref := range []checkexec.ToolReceipt{three, three} {
		wg.Add(1)
		go func(ref checkexec.ToolReceipt) {
			defer wg.Done()
			errs <- cli.ExportTestWorkspaceOwnedReceipt(root, e, 0, ref)
		}(ref)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	errs = make(chan error, 2)
	for _, ref := range []checkexec.ToolReceipt{four, five} {
		wg.Add(1)
		go func(ref checkexec.ToolReceipt) {
			defer wg.Done()
			errs <- cli.ExportTestWorkspaceOwnedReceipt(root, e, 0, ref)
		}(ref)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	after := stageSnapshot(t, root)
	registry, err = workspace.Decode(after.State)
	if err != nil {
		t.Fatal(err)
	}
	got := registry.Executions[e.AssignmentID].OwnedCheckReceipts
	if len(got) != 5 || after.Revision != before.Revision+5 {
		t.Fatalf("same-index distinct receipts lost: %d revision %d/%d", len(got), after.Revision, before.Revision)
	}
	for _, ref := range []checkexec.ToolReceipt{one, two, three, four, five} {
		if err := cli.ExportTestWorkspaceOwnedReceipt(root, e, 0, ref); err != nil {
			t.Fatal(err)
		}
	}
	replay := stageSnapshot(t, root)
	if replay.Revision != after.Revision {
		t.Fatal("owned receipt replay created new journal event")
	}
	journal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ValidateCheckpointIndices(root, replay.State, journal); err != nil {
		t.Fatalf("checkpoint pair: %v", err)
	}
}
