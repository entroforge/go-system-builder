package policy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestNativeOwnedCheckDecisionRequiresExactFrozenCommandAndCallerBinding(t *testing.T) {
	root := t.TempDir()
	worktree := filepath.Join(root, ".worktrees", "registered-owner")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	const (
		owner       = "agent-owned-check"
		assignment  = "assignment-owned-check"
		recordSHA   = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		checkIndex  = 2
		checkScript = "go test ./src/pkg"
	)
	runtimeContext := policy.RuntimeContext{
		ProjectRoot: root, RuntimeID: "loop-owned-check", CurrentState: "building",
		CurrentStage: "S6", CurrentBaselineGeneration: 4,
		NativeOwnedAssignments: []policy.NativeOwnedAssignment{{
			AssignmentID: assignment, OwnerAgentID: owner, WorktreePath: worktree,
			AssignmentRecordSHA256: recordSHA, OwnerState: "working", ContextBound: true,
			PlanActivated: true, Checks: []policy.NativeOwnedCheck{{Index: checkIndex, Command: checkScript}},
		}},
	}
	command := policy.NativeOwnedCheckInvocation(root, owner, checkIndex)

	main := policy.Input{
		Event: "PreToolUse", SessionID: "platform-session-main", CWD: root,
		ToolName: "Bash", ToolInput: map[string]any{"command": command}, Runtime: runtimeContext,
	}
	if decision, blocked := policy.NativeOwnedCheckDecision(main); blocked {
		t.Fatalf("trusted Main command should pass to the exact runner adapter: %+v", decision)
	}

	worker := main
	worker.AgentID = owner
	worker.CWD = worktree
	worker.Runtime.Agent = &policy.AgentContext{
		ID: owner, State: "working", AssignmentID: assignment,
		DispatchMode: "plan_checkpoint", PlanReportedRef: ".claude/evidence/plan-activated.json",
	}
	if decision, blocked := policy.NativeOwnedCheckDecision(worker); blocked {
		t.Fatalf("registered native owner command should pass: %+v", decision)
	}

	cases := []struct {
		name   string
		mutate func(*policy.Input)
	}{
		{"forged root flag", func(in *policy.Input) { in.ToolInput["command"] = replaceCommandArg(command, "--root", "forged-root") }},
		{"forged owner flag", func(in *policy.Input) {
			in.ToolInput["command"] = replaceCommandArg(command, "--agent-id", "other-agent")
		}},
		{"forged check index", func(in *policy.Input) { in.ToolInput["command"] = replaceCommandArg(command, "--check-index", "3") }},
		{"shell suffix", func(in *policy.Input) { in.ToolInput["command"] = command + " && true" }},
		{"missing original session", func(in *policy.Input) { in.SessionID = "" }},
		{"worker hook owner mismatch", func(in *policy.Input) { in.AgentID = "other-agent" }},
		{"worker loaded owner mismatch", func(in *policy.Input) { in.Runtime.Agent.ID = "other-agent" }},
		{"worker CWD mismatch", func(in *policy.Input) { in.CWD = root }},
		{"unbound context", func(in *policy.Input) { in.Runtime.NativeOwnedAssignments[0].ContextBound = false }},
		{"unactivated plan", func(in *policy.Input) { in.Runtime.NativeOwnedAssignments[0].PlanActivated = false }},
		{"wrong loaded dispatch", func(in *policy.Input) { in.Runtime.Agent.DispatchMode = "readback_submitted" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := worker
			tc.mutate(&input)
			decision, blocked := policy.NativeOwnedCheckDecision(input)
			if !blocked || decision.Decision != "deny" || decision.RuleID != policy.RuleNativeOwnedCheck {
				t.Fatalf("invalid native check binding was not denied: blocked=%t decision=%+v", blocked, decision)
			}
		})
	}
}

func TestNativeOwnedCheckDecisionOnlyTreatsTheRunnerAdapterAsSpecial(t *testing.T) {
	root := t.TempDir()
	command := policy.NativeOwnedCheckInvocation(root, "agent-owned-check", 0)
	input := policy.Input{Event: "PreToolUse", ToolName: "Bash", ToolInput: map[string]any{"command": command}}
	decision, blocked := policy.NativeOwnedCheckDecision(input)
	if !blocked || decision.RuleID != policy.RuleNativeOwnedCheck {
		t.Fatalf("runner intent without trusted Runtime facts must fail closed: blocked=%t decision=%+v", blocked, decision)
	}
	input.ToolInput["command"] = "go test ./src/pkg"
	if decision, blocked := policy.NativeOwnedCheckDecision(input); blocked {
		t.Fatalf("unrelated Bash command was captured by the owned-check adapter rule: %+v", decision)
	}
}

func replaceCommandArg(command, flag, replacement string) string {
	for i := 0; i < len(command); i++ {
		if command[i] != ' ' {
			continue
		}
		start := i + 1
		if start+len(flag) <= len(command) && command[start:start+len(flag)] == flag {
			valueStart := start + len(flag) + 1
			for valueStart < len(command) && command[valueStart] == '\'' {
				valueStart++
			}
			valueEnd := valueStart
			for valueEnd < len(command) && command[valueEnd] != '\'' && command[valueEnd] != ' ' {
				valueEnd++
			}
			return command[:valueStart] + replacement + command[valueEnd:]
		}
	}
	return command + " " + flag + " " + replacement
}
