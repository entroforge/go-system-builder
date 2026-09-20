package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

func boundaryFixture(t *testing.T) (*workspace.Binding, workspace.Execution) {
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
	b, err := workspace.New(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"runtime_id": "loop-boundary", "baseline": map[string]any{"generation": 1}}
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
