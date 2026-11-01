package policy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func evaluateRepairPlanReportPath(t *testing.T, root, agentID, assignmentID, path string, grant bool) policy.Decision {
	t.Helper()
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "control", "hook-policy.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var paths []string
	// hookctx only projects an issued path for its actual assignment.
	if grant && assignmentID == "repair-assignment-1" {
		paths = []string{".claude/submissions/loop-test/g1/S9-plan/r1/repair-assignment-1/packet-abc/builder-1/answer.json"}
	}
	decision, err := engine.Evaluate(policy.Input{
		Event: "PreToolUse", AgentID: agentID, ToolName: "Write",
		ToolInput: map[string]any{"file_path": path},
		Runtime: policy.RuntimeContext{
			PacketMode: true, RepairPlanReportAnswerPaths: paths,
			RuntimeID: "loop-test", CurrentBaselineGeneration: 1,
			CurrentState: "bug_resolution", CurrentPhase: "planning", ProjectRoot: root,
			Agent: &policy.AgentContext{ID: "builder-1", RepairAssignmentID: assignmentID, State: "working"},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return decision
}

func TestRepairPlanReportWriteRequiresExactCurrentTrustedGrant(t *testing.T) {
	root := t.TempDir()
	issued := ".claude/submissions/loop-test/g1/S9-plan/r1/repair-assignment-1/packet-abc/builder-1/answer.json"
	if decision := evaluateRepairPlanReportPath(t, root, "builder-1", "repair-assignment-1", issued, true); decision.Decision == "deny" || decision.Decision == "block" {
		t.Fatalf("exact current answer path should be allowed, got %+v", decision)
	}
	for _, tc := range []struct {
		name         string
		agentID      string
		assignmentID string
		path         string
		grant        bool
	}{
		{"unissued sibling path", "builder-1", "repair-assignment-1", ".claude/submissions/loop-test/g1/S9-plan/r1/repair-assignment-1/packet-abc/builder-1/other.json", true},
		{"wrong owner", "builder-2", "repair-assignment-1", issued, true},
		{"wrong assignment", "builder-1", "repair-assignment-2", issued, true},
		{"missing path grant", "builder-1", "repair-assignment-1", issued, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decision := evaluateRepairPlanReportPath(t, root, tc.agentID, tc.assignmentID, tc.path, tc.grant)
			if decision.Decision != "deny" {
				t.Fatalf("unissued or mismatched path must deny, got %+v", decision)
			}
		})
	}
}

func TestRepairPlanReportWriteNeedsPreExecutionPhase(t *testing.T) {
	root := t.TempDir()
	issued := ".claude/submissions/loop-test/g1/S9-plan/r1/repair-assignment-1/packet-abc/builder-1/answer.json"
	in := policy.Input{Event: "PreToolUse", AgentID: "builder-1", ToolName: "Write", ToolInput: map[string]any{"file_path": issued}, Runtime: policy.RuntimeContext{PacketMode: true, RepairPlanReportAnswerPaths: []string{issued}, CurrentState: "bug_resolution", CurrentPhase: "fixing", ProjectRoot: root, Agent: &policy.AgentContext{ID: "builder-1", RepairAssignmentID: "repair-assignment-1"}}}
	decision, _ := policy.EvaluateAgentScoped(in)
	if decision.Decision != "deny" {
		t.Fatalf("invalid S9 phase must deny, got %+v", decision)
	}
}

func TestRepairPlanReportWriteRejectsSymlinkedPacketPath(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	parent := filepath.Join(root, ".claude", "submissions", "loop-test", "g1", "S9-plan", "r1", "repair-assignment-1")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(parent, "packet-abc")); err != nil {
		t.Skip(err)
	}
	issued := ".claude/submissions/loop-test/g1/S9-plan/r1/repair-assignment-1/packet-abc/builder-1/answer.json"
	decision := evaluateRepairPlanReportPath(t, root, "builder-1", "repair-assignment-1", issued, true)
	if decision.Decision != "deny" {
		t.Fatalf("symlinked packet path must deny, got %+v", decision)
	}
}
