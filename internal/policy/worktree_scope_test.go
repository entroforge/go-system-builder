package policy_test

import (
	"github.com/entroforge/go-system-builder/internal/policy"
	"path/filepath"
	"testing"
)

func TestWorktreeScopeUsesTargetAndPreservesRootSafety(t *testing.T) {
	root := t.TempDir()
	engine := loadRepositoryPolicy(t)
	input := policy.Input{Event: "PreToolUse", ToolName: "Edit", CWD: filepath.Join(root, ".worktrees", "worker"),
		Runtime: policy.RuntimeContext{RuntimeID: "loop-test", ProjectRoot: root, CurrentState: "building", CurrentStage: "S6", CurrentBaselineGeneration: 1,
			LockedArtifacts: []policy.LockedArtifact{{ID: "BE-001", Kind: "contract", Path: "docs/dev/contracts/BE-001.md", Version: "v1", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", LockedFromStage: "S6", BaselineGeneration: 1}}}}
	input.ToolInput = map[string]any{"file_path": "docs/dev/contracts/BE-001.md"}
	if got, _ := engine.Evaluate(input); got.Decision == "deny" {
		t.Fatalf("temporary file treated as root artifact: %#v", got)
	}
	input.ToolInput = map[string]any{"file_path": filepath.Join(root, "docs/dev/contracts/BE-001.md")}
	if got, _ := engine.Evaluate(input); got.Decision != "deny" {
		t.Fatalf("worker cwd bypassed root protection: %#v", got)
	}
	input.ToolName = "Bash"
	input.ToolInput = map[string]any{"command": "git merge --squash worker"}
	if got, _ := engine.Evaluate(input); got.Decision != "deny" {
		t.Fatalf("worktree bypassed squash protection: %#v", got)
	}
}
