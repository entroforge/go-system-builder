package policy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func boundaryFixture(t *testing.T) (*workspace.ExecutionRegistry, workspace.Execution) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if _, err := workspace.Git(ctx, root, args...); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-b", "feature/customer-a")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.invalid")
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".claude/\n.worktrees/\n"), 0600)
	git("add", ".gitignore")
	git("commit", "-m", "base")
	head, err := workspace.Git(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	bound := map[string]any{"workspace": workspace.Binding{ProjectRoot: root, DevBranch: "feature/customer-a", ReleaseUpstream: "main", BoundCommit: head}.Map()}
	state := map[string]any{"bound_req": bound, "runtime_id": "loop-boundary", "baseline": map[string]any{"generation": 1}}
	data, _ := json.Marshal(state)
	os.MkdirAll(filepath.Join(root, ".claude"), 0700)
	os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), data, 0600)
	b, err := workspace.New(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	e, err := b.Plan(ctx, state, "assignment", "builder", []string{"src/**", "docs/"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.Executions[e.AssignmentID] = e
	if err = b.Materialize(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.Status = "ready"
	b.Executions[e.AssignmentID] = e
	os.MkdirAll(filepath.Join(e.Path, "src"), 0700)
	os.MkdirAll(filepath.Join(e.Path, "docs/sub"), 0700)
	return b, e
}
func TestWorkspaceNativeDirectoryAndWriteBoundaries(t *testing.T) {
	b, e := boundaryFixture(t)
	for _, tc := range []struct {
		name, agent, cwd, path, tool string
		blocked                      bool
	}{
		{"main own file", "", b.MainRoot, "notes.md", "Write", false},
		{"main entered worker", "", e.Path, "src/a.go", "Write", true},
		{"main writes worker absolute", "", b.MainRoot, filepath.Join(e.Path, "src/a.go"), "Write", true},
		{"main reads worker", "", b.MainRoot, filepath.Join(e.Path, "src/a.go"), "Read", false},
		{"worker own scope", "builder", e.Path, "src/a.go", "Edit", false},
		{"worker subdirectory", "builder", filepath.Join(e.Path, "docs/sub"), "new.md", "Write", false},
		{"worker writes main", "builder", e.Path, filepath.Join(b.MainRoot, "src/a.go"), "Write", true},
		{"worker runs in main", "builder", b.MainRoot, "src/a.go", "Edit", true},
		{"worker escapes scope", "builder", e.Path, "other.txt", "Write", true},
		{"missing cwd", "", "", "src/a.go", "Write", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := Input{Event: "PreToolUse", AgentID: tc.agent, CWD: tc.cwd, ToolName: tc.tool, ToolInput: map[string]any{"file_path": tc.path}, Runtime: RuntimeContext{ProjectRoot: b.MainRoot, Workspace: b, RuntimeID: "loop-boundary", CurrentBaselineGeneration: 1}}
			decision, blocked := WorkspaceBoundaryDecision(input)
			if blocked != tc.blocked {
				t.Fatalf("blocked=%v decision=%+v", blocked, decision)
			}
		})
	}
}
func TestWorkspaceSymlinkCannotBypassOwnerOrScope(t *testing.T) {
	b, e := boundaryFixture(t)
	alias := filepath.Join(b.MainRoot, "alias")
	if err := os.Symlink(e.Path, alias); err != nil {
		t.Skip(err)
	}
	input := Input{Event: "PreToolUse", CWD: b.MainRoot, ToolName: "Write", ToolInput: map[string]any{"file_path": filepath.Join(alias, "src/new.go")}, Runtime: RuntimeContext{ProjectRoot: b.MainRoot, Workspace: b, RuntimeID: "loop-boundary", CurrentBaselineGeneration: 1}}
	if _, blocked := WorkspaceBoundaryDecision(input); !blocked {
		t.Fatal("Main symlink write reached Worker")
	}
	os.Mkdir(filepath.Join(e.Path, "outside"), 0700)
	if err := os.Symlink(filepath.Join(e.Path, "outside"), filepath.Join(e.Path, "src/alias")); err != nil {
		t.Fatal(err)
	}
	input.AgentID = "builder"
	input.CWD = e.Path
	input.ToolInput["file_path"] = "src/alias/new.go"
	if _, blocked := WorkspaceBoundaryDecision(input); !blocked {
		t.Fatal("Worker symlink escaped declared scope")
	}
}
func TestWorkspaceStaleWorkerCannotBecomeMain(t *testing.T) {
	b, e := boundaryFixture(t)
	e.Status = "retired"
	b.Executions[e.AssignmentID] = e
	input := Input{Event: "PreToolUse", AgentID: "builder", CWD: b.MainRoot, ToolName: "Write", ToolInput: map[string]any{"file_path": "src/a.go"}, Runtime: RuntimeContext{ProjectRoot: b.MainRoot, Workspace: b, RuntimeID: "loop-boundary", CurrentBaselineGeneration: 1}}
	if _, blocked := WorkspaceBoundaryDecision(input); !blocked {
		t.Fatal("stale Worker inherited Main authority")
	}
}
func TestWorkerAbsolutePathStillEnforcesLockedArtifact(t *testing.T) {
	b, e := boundaryFixture(t)
	input := Input{Event: "PreToolUse", AgentID: "builder", CWD: e.Path, ToolName: "Write", ToolInput: map[string]any{"file_path": filepath.Join(e.Path, "docs/contract.md")}, Runtime: RuntimeContext{ProjectRoot: b.MainRoot, Workspace: b, RuntimeID: "loop-boundary", CurrentBaselineGeneration: 1, CurrentStage: "S6", CurrentState: "building", LockedArtifacts: []LockedArtifact{{ID: "CT-001", Kind: "contract", Path: "docs/contract.md", Version: "1", SHA256: "hash", LockedFromStage: "S6", BaselineGeneration: 1}}}}
	decision, err := (&Engine{}).Evaluate(input)
	if err != nil || decision.RuleID != RuleLockedArtifactWrite {
		t.Fatalf("locked artifact bypass: %+v %v", decision, err)
	}
}

func TestWorkerSymlinkStillEnforcesAbsoluteMainLockedReference(t *testing.T) {
	b, e := boundaryFixture(t)
	os.WriteFile(filepath.Join(e.Path, "docs/contract.md"), []byte("locked"), 0600)
	if err := os.Symlink("contract.md", filepath.Join(e.Path, "docs/alias.md")); err != nil {
		t.Skip(err)
	}
	input := Input{Event: "PreToolUse", AgentID: "builder", CWD: e.Path, ToolName: "Write", ToolInput: map[string]any{"file_path": filepath.Join(e.Path, "docs/alias.md")}, Runtime: RuntimeContext{ProjectRoot: b.MainRoot, Workspace: b, RuntimeID: "loop-boundary", CurrentBaselineGeneration: 1, CurrentStage: "S6", CurrentState: "building", LockedArtifacts: []LockedArtifact{{ID: "CT-001", Kind: "contract", Path: filepath.Join(b.MainRoot, "docs/contract.md"), Version: "1", SHA256: "hash", LockedFromStage: "S6", BaselineGeneration: 1}}}}
	decision, err := (&Engine{}).Evaluate(input)
	if err != nil || decision.RuleID != RuleLockedArtifactWrite {
		t.Fatalf("locked symlink bypass: %+v %v", decision, err)
	}
}

func TestWorkerObservationAdapterAndPwd(t *testing.T) {
	b, e := boundaryFixture(t)
	os.MkdirAll(filepath.Join(b.MainRoot, ".claude/agents"), 0700)
	os.MkdirAll(filepath.Join(b.MainRoot, ".claude/skills"), 0700)
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
	b.Executions[e.AssignmentID] = e
	commands := []string{"pwd", workspace.ReceiptInvocation(e)}
	for _, c := range workspace.ObservationCommands(e) {
		commands = append(commands, c)
	}
	for _, command := range commands {
		in := Input{Event: "PreToolUse", AgentID: e.AgentID, CWD: e.Path, ToolName: "Bash", ToolInput: map[string]any{"command": command}, Runtime: RuntimeContext{ProjectRoot: b.MainRoot, Workspace: b, RuntimeID: e.RuntimeID, CurrentBaselineGeneration: e.BaselineGeneration}}
		if d, handled := WorkspaceBoundaryDecision(in); handled && d.Decision == "deny" {
			t.Fatalf("observation denied: %+v", d)
		}
		in.ToolInput["command"] = command + "; touch forbidden"
		if d, handled := WorkspaceBoundaryDecision(in); !handled || d.Decision != "deny" {
			t.Fatal("compound command accepted")
		}
	}
}

func TestWorkspacePolicyAcceptsOnlyFrozenOwnedCheckAdapter(t *testing.T) {
	_, e := boundaryFixture(t)
	e.BootstrapSHA256 = "frozen-bootstrap"
	profile := checkexec.Profile{ID: "owned-check", Version: "1.0.0", CheckIndex: 0, Command: "true", Location: "worker", TimeoutSeconds: 10, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never"}
	e.WorkerChecks = []assignmentrecord.WorkerCheck{{Index: 0, Command: profile.Command, Profile: profile}}
	base := Input{ToolName: "Bash", ToolInput: map[string]any{"command": workspace.OwnedCheckInvocation(e, 0)}}
	if !workspaceAdapterCommand(base, e) {
		t.Fatal("policy did not recognize the exact frozen owned-check command")
	}
	for _, command := range []string{
		workspace.OwnedCheckInvocation(e, 0) + " --check 0",
		workspace.OwnedCheckInvocation(e, 1),
		workspace.OwnedCheckInvocation(e, 0) + "; true",
	} {
		base.ToolInput["command"] = command
		if workspaceAdapterCommand(base, e) {
			t.Fatalf("policy accepted a changed owned-check invocation: %s", command)
		}
	}
}

func TestNativeOwnedCheckExceptionUsesExactAuthorityWithOtherWorkspaceExecution(t *testing.T) {
	b, e := boundaryFixture(t)
	owner := "native-builder"
	assignmentID := "native-assignment"
	checkCommand := "go test ./internal/example"
	native := NativeOwnedAssignment{
		AssignmentID: assignmentID, OwnerAgentID: owner, WorktreePath: e.Path,
		AssignmentRecordSHA256: "frozen-record-sha", OwnerState: "working",
		ContextBound: true, PlanActivated: true,
		Checks: []NativeOwnedCheck{{Index: 0, Command: checkCommand}},
	}
	agent := &AgentContext{ID: owner, State: "working", AssignmentID: assignmentID, DispatchMode: "plan_checkpoint", PlanReportedRef: "plan/report.json"}
	input := Input{
		Event: "PreToolUse", SessionID: "native-session", AgentID: owner, CWD: e.Path,
		ToolName: "Bash", ToolInput: map[string]any{"command": workspace.NativeOwnedCheckInvocation(b.MainRoot, owner, 0)},
		Runtime: RuntimeContext{
			ProjectRoot: b.MainRoot, Workspace: b, RuntimeID: e.RuntimeID,
			CurrentBaselineGeneration: e.BaselineGeneration, CurrentState: "building", CurrentStage: "S6",
			Agent: agent, NativeOwnedAssignments: []NativeOwnedAssignment{native},
		},
	}
	if decision, blocked := WorkspaceBoundaryDecision(input); blocked || decision.Decision == "deny" {
		t.Fatalf("exact native owner check was denied because another Workspace execution is registered: %+v", decision)
	}

	// The exception is command-specific. Ordinary shell commands from the same
	// owner CWD still hit the other-execution boundary.
	input.ToolInput["command"] = "pwd"
	if decision, blocked := WorkspaceBoundaryDecision(input); !blocked || decision.Decision != "deny" {
		t.Fatalf("native authority widened to an unrelated shell command: %+v", decision)
	}

	// A modified invocation cannot borrow the trusted native authority.
	input.ToolInput["command"] = workspace.NativeOwnedCheckInvocation(b.MainRoot, owner, 0) + " --check-index 1"
	if decision, blocked := WorkspaceBoundaryDecision(input); !blocked || decision.Decision != "deny" {
		t.Fatalf("modified native check invocation was accepted: %+v", decision)
	}

	// Nor can another platform Agent identity use the frozen owner's command.
	input.ToolInput["command"] = workspace.NativeOwnedCheckInvocation(b.MainRoot, owner, 0)
	input.AgentID = "different-agent"
	input.Runtime.Agent = &AgentContext{ID: "different-agent", State: "working", AssignmentID: assignmentID, DispatchMode: "plan_checkpoint", PlanReportedRef: "plan/report.json"}
	if decision, blocked := WorkspaceBoundaryDecision(input); !blocked || decision.Decision != "deny" {
		t.Fatalf("different Agent identity borrowed native check authority: %+v", decision)
	}
}
