package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// This exercises the original native entry through current Record/PLAN/context,
// frozen manifest worktree, the real isolated runner and the original Runtime
// Writer. Its outcome is the host's actual isolation result; the fixture never
// manufactures a passing receipt.
func TestNativeAssignmentContextCheckUsesOriginalIsolatedRunnerAndWriter(t *testing.T) {
	root, taskID, taskPath := newS6RecordContextFixture(t)
	const (
		manifestRef = ".claude/workgroups/manifest-s6-native-context.json"
		owner       = "agent-s6-native-context"
		assignment  = "assignment-s6-native-context"
		worktreeRel = ".worktrees/native-owner-s6-check"
		branch      = "work/native-owner-s6-check"
	)
	worktree := filepath.Join(root, filepath.FromSlash(worktreeRel))
	if err := os.MkdirAll(filepath.Dir(worktree), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), root, "worktree", "add", "-b", branch, worktree, "HEAD"); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, filepath.FromSlash(manifestRef))
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	row := manifest["assignments"].([]any)[0].(map[string]any)
	row["worktree_path"], row["branch"], row["target_branch"] = worktreeRel, branch, "test-development"
	mainProfile := row["check_profiles"].([]any)[0].(map[string]any)
	mainProfile["location"] = "main"
	workerProfile := map[string]any{
		"id": "profile-s6-native-owned-check", "version": "v1.0.0", "check_index": 0,
		"command": "go test ./src/pkg", "location": "worker", "timeout_seconds": 20,
		"environment_policy": "declared_only", "environment": []any{},
		"dependencies":  []any{map[string]any{"path": "src/pkg/input.go", "sha256": workpacket.Digest([]byte("package pkg\n"))}},
		"fixtures":      []any{map[string]any{"path": "testdata/input.txt", "sha256": workpacket.Digest([]byte("registered profile input\n"))}},
		"input_closure": "whole_tree", "side_effects": "none", "reuse_policy": "never",
	}
	row["closing_contract"] = map[string]any{"contract": assignmentrecord.ClosingContractVersion, "worker_checks": []any{map[string]any{
		"index": 0, "command": "go test ./src/pkg", "profile": workerProfile,
	}}}
	row["integration_check_mode"] = "post_merge"
	stageJSON(t, root, manifestRef, manifest)
	stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", manifestRef, "--task-id", taskID, "--task", taskPath)
	snapshot := stageSnapshot(t, root)
	record, found, err := assignmentrecord.ForOwner(snapshot.State, owner)
	if err != nil || !found || record.ClosingContract == nil || len(record.ClosingContract.WorkerChecks) != 1 {
		t.Fatalf("Register did not freeze one native WorkerCheck: found=%t record=%+v err=%v", found, record, err)
	}
	contextOutput := stageCLI(t, "runtime", "assignment-context", "prepare", "--root", root, "--agent-id", owner)
	var nativeBinding struct {
		OwnedCheckCommands []string `json:"owned_check_commands"`
		Context            struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"context_ref"`
	}
	if err := json.Unmarshal([]byte(contextOutput), &nativeBinding); err != nil || nativeBinding.Context.Path == "" || nativeBinding.Context.SHA256 == "" {
		t.Fatalf("prepare original fixed context: %+v err=%v", nativeBinding, err)
	}
	wantOwnedCheckCommands := []string{policy.NativeOwnedCheckInvocation(root, owner, 0)}
	if !reflect.DeepEqual(nativeBinding.OwnedCheckCommands, wantOwnedCheckCommands) {
		t.Fatalf("prepare did not return the exact frozen owner-check command: got=%q want=%q", nativeBinding.OwnedCheckCommands, wantOwnedCheckCommands)
	}
	contextBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(nativeBinding.Context.Path)))
	if err != nil || workpacket.Digest(contextBytes) != nativeBinding.Context.SHA256 {
		t.Fatalf("fixed context source mismatch: %v", err)
	}
	var context struct {
		PlanPath           string         `json:"plan_draft_path"`
		Plan               map[string]any `json:"plan_draft"`
		OwnedCheckCommands []string       `json:"owned_check_commands"`
	}
	if err := json.Unmarshal(contextBytes, &context); err != nil || context.PlanPath == "" || context.Plan == nil {
		t.Fatalf("decode fixed context plan draft: %+v %v", context, err)
	}
	if !reflect.DeepEqual(context.OwnedCheckCommands, wantOwnedCheckCommands) {
		t.Fatalf("fixed native context does not retain its original check command: got=%q want=%q", context.OwnedCheckCommands, wantOwnedCheckCommands)
	}
	loaded, err := hookctx.LoadFull(root, owner)
	if err != nil || loaded == nil || len(loaded.PolicyContext.NativeOwnedAssignments) != 1 {
		t.Fatalf("trusted hook loader did not project the fixed native WorkerCheck: loaded=%+v err=%v", loaded, err)
	}
	projected := loaded.PolicyContext.NativeOwnedAssignments[0]
	if projected.AssignmentID != assignment || projected.OwnerAgentID != owner || projected.AssignmentRecordSHA256 == "" || !projected.ContextBound || projected.PlanActivated {
		t.Fatalf("pre-activation native policy projection is incorrect: %+v", projected)
	}
	assertion := "The registered package change passes the frozen WorkerCheck."
	oracle := "Run the frozen package command in the original isolated Worker runner."
	context.Plan["objective"] = "Implement the reviewed package TASK within the frozen scope."
	context.Plan["planned_paths"] = []any{"src/pkg/input.go"}
	context.Plan["assertion_checks"] = []any{map[string]any{"assertion": assertion, "oracle": oracle, "done_when_indexes": []any{0}}}
	context.Plan["dependencies"], context.Plan["risks_blockers"] = []any{}, []any{}
	stageJSON(t, root, context.PlanPath, context.Plan)
	stageCLI(t, "runtime", "agent-begin", "--root", root, "--agent-id", owner, "--plan", context.PlanPath)
	loaded, err = hookctx.LoadFull(root, owner)
	if err != nil || loaded == nil || len(loaded.PolicyContext.NativeOwnedAssignments) != 1 || !loaded.PolicyContext.NativeOwnedAssignments[0].PlanActivated {
		t.Fatalf("trusted hook loader did not project the current activated native WorkerCheck: loaded=%+v err=%v", loaded, err)
	}

	var out, errout bytes.Buffer
	code := cli.Run([]string{"runtime", "assignment-context", "check", "--root", root, "--agent-id", owner, "--check-index", "0"}, nil, &out, &errout)
	current := stageSnapshot(t, root)
	var registered []struct {
		CheckIndex          int                              `json:"check_index"`
		Command             string                           `json:"command"`
		ProfileDigest       string                           `json:"profile_digest"`
		Record              assignmentrecord.RecordReference `json:"record"`
		RecordSHA256        string                           `json:"record_sha256"`
		RuntimeID           string                           `json:"runtime_id"`
		BaselineGeneration  int                              `json:"baseline_generation"`
		AssignmentID        string                           `json:"assignment_id"`
		OwnerAgentID        string                           `json:"owner_agent_id"`
		ExecutionGeneration int                              `json:"execution_generation"`
		ContextGeneration   int                              `json:"native_context_generation"`
		ContextRef          string                           `json:"native_context_ref"`
		ContextSHA256       string                           `json:"native_context_sha256"`
		ActivationRef       string                           `json:"activation_ref"`
		ActivationSHA256    string                           `json:"activation_sha256"`
		Checkout            string                           `json:"checkout"`
		SourceBranch        string                           `json:"source_branch"`
		TargetBranch        string                           `json:"target_branch"`
		SourceHead          string                           `json:"source_head"`
		SourceSnapshotSHA   string                           `json:"source_snapshot_sha256"`
		ToolReceipt         checkexec.ToolReceipt            `json:"tool_receipt"`
	}
	for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] != owner {
			continue
		}
		encoded, _ := json.Marshal(row["owned_check_receipts"])
		if err := json.Unmarshal(encoded, &registered); err != nil {
			t.Fatal(err)
		}
	}
	if len(registered) != 1 {
		t.Fatalf("original Runtime Writer did not persist one captured native receipt: rows=%d code=%d stderr=%s stdout=%s", len(registered), code, errout.String(), out.String())
	}
	receipt := registered[0]
	if receipt.CheckIndex != 0 || receipt.Command != "go test ./src/pkg" || receipt.ProfileDigest != record.ClosingContract.WorkerChecks[0].Profile.Digest() || receipt.Record != record.Reference() || receipt.RecordSHA256 == "" || receipt.RuntimeID != record.RuntimeID || receipt.BaselineGeneration != record.BaselineGeneration || receipt.AssignmentID != record.AssignmentID || receipt.OwnerAgentID != owner || receipt.ExecutionGeneration != 0 || receipt.ContextGeneration != 1 || receipt.ContextRef != nativeBinding.Context.Path || receipt.ContextSHA256 != nativeBinding.Context.SHA256 || receipt.ActivationRef == "" || receipt.ActivationSHA256 == "" || receipt.Checkout != worktree || receipt.SourceBranch != branch || receipt.TargetBranch != "test-development" || receipt.SourceHead == "" || receipt.SourceSnapshotSHA == "" {
		t.Fatalf("native receipt lost its original identity/context/activation/source tuple: %+v", receipt)
	}
	verified, err := checkexec.VerifyToolReceipt(filepath.Dir(receipt.ToolReceipt.Path), receipt.ToolReceipt)
	if err != nil {
		t.Fatalf("registered process-captured receipt did not verify: %v", err)
	}
	if verified.Passed != (code == 0) {
		t.Fatalf("CLI exit %d disagrees with actual sealed host result pass=%t: %s", code, verified.Passed, errout.String())
	}
	if !verified.Passed {
		if errout.Len() == 0 || !strings.Contains(errout.String(), "WorkerCheck") && !strings.Contains(errout.String(), "capability") && !strings.Contains(errout.String(), "sandbox") {
			t.Fatalf("host rejection has no original runner/capability explanation: %s", errout.String())
		}
		if current.Revision <= snapshot.Revision {
			t.Fatal("actual failed runner receipt was not recorded by the original Writer")
		}
	}
}

func TestNativeAssignmentContextCheckRejectsCallerSuppliedAuthority(t *testing.T) {
	root, _, _ := newS6RecordContextFixture(t)
	for _, args := range [][]string{
		{"runtime", "assignment-context", "check", "--root", root, "--agent-id", "agent-s6-native-context", "--check-index", "0", "--source-root", root},
		{"runtime", "assignment-context", "check", "--root", root, "--agent-id", "agent-s6-native-context", "--check-index", "0", "--command", "true"},
	} {
		var out, errout bytes.Buffer
		if code := cli.Run(args, nil, &out, &errout); code == 0 {
			t.Fatalf("native WorkerCheck accepted a caller-supplied execution authority: args=%q", args)
		}
	}
}
