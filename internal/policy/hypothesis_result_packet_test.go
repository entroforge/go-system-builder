package policy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func evaluateHypothesisResultPath(t *testing.T, root, agentID, assignmentID, path string, grant, assignment bool) policy.Decision {
	t.Helper()
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "control", "hook-policy.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var paths []string
	if grant {
		paths = []string{".claude/submissions/loop-test/g1/S8/r1/assignment-1/packet-1/investigator-1/answer.json"}
	}
	decision, err := engine.Evaluate(policy.Input{
		Event:    "PreToolUse",
		AgentID:  agentID,
		ToolName: "Write",
		ToolInput: map[string]any{
			"file_path": path,
		},
		Runtime: policy.RuntimeContext{
			PacketMode:                  true,
			HypothesisResultAnswerPaths: paths,
			RuntimeID:                   "loop-test",
			CurrentBaselineGeneration:   1,
			CurrentReviewRound:          1,
			ProjectRoot:                 root,
			CurrentState:                "bug_resolution",
			CurrentPhase:                "investigation",
			Agent: &policy.AgentContext{
				ID:                         "investigator-1",
				AssignmentID:               assignmentID,
				HypothesisResultAssignment: assignment,
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return decision
}

func TestHypothesisResultPacketWriteRequiresExactTrustedCurrentGrant(t *testing.T) {
	root := t.TempDir()
	issued := ".claude/submissions/loop-test/g1/S8/r1/assignment-1/packet-1/investigator-1/answer.json"
	if decision := evaluateHypothesisResultPath(t, root, "investigator-1", "assignment-1", issued, true, true); decision.Decision == "deny" || decision.Decision == "block" {
		t.Fatalf("exact current issued answer path should be allowed, got %+v", decision)
	}

	for _, tc := range []struct {
		name         string
		agentID      string
		assignmentID string
		path         string
		grant        bool
		assignment   bool
	}{
		{name: "unissued sibling path", agentID: "investigator-1", assignmentID: "assignment-1", path: ".claude/submissions/loop-test/g1/S8/r1/assignment-1/packet-1/investigator-1/other.json", grant: true, assignment: true},
		{name: "wrong payload owner", agentID: "investigator-2", assignmentID: "assignment-1", path: issued, grant: true, assignment: true},
		{name: "wrong assignment", agentID: "investigator-1", assignmentID: "assignment-2", path: issued, grant: true, assignment: true},
		{name: "missing exact path grant", agentID: "investigator-1", assignmentID: "assignment-1", path: issued, assignment: false},
		{name: "missing registered S8 assignment", agentID: "investigator-1", assignmentID: "assignment-1", path: issued, grant: true, assignment: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decision := evaluateHypothesisResultPath(t, root, tc.agentID, tc.assignmentID, tc.path, tc.grant, tc.assignment)
			if decision.Decision != "deny" {
				t.Fatalf("unissued or mismatched answer path must deny, got %+v", decision)
			}
		})
	}
}

func TestHypothesisResultPacketWriteRejectsSymlinkedIssuedPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	parent := filepath.Join(root, ".claude", "submissions", "loop-test", "g1", "S8", "r1", "assignment-1")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(parent, "packet-1")); err != nil {
		t.Fatal(err)
	}
	issued := ".claude/submissions/loop-test/g1/S8/r1/assignment-1/packet-1/investigator-1/answer.json"
	decision := evaluateHypothesisResultPath(t, root, "investigator-1", "assignment-1", issued, true, true)
	if decision.Decision != "deny" {
		t.Fatalf("symlinked answer path must deny, got %+v", decision)
	}
}

func TestHypothesisResultPacketWriteDoesNotUseTeammateNameAsIdentity(t *testing.T) {
	root := t.TempDir()
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "control", "hook-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	issued := ".claude/submissions/loop-test/g1/S8/r1/assignment-1/packet-1/investigator-1/answer.json"
	decision, err := engine.Evaluate(policy.Input{
		Event: "PreToolUse", TeammateName: "investigator-1", ToolName: "Write",
		ToolInput: map[string]any{"file_path": issued},
		Runtime: policy.RuntimeContext{
			PacketMode: true, HypothesisResultAnswerPaths: []string{issued}, RuntimeID: "loop-test",
			CurrentBaselineGeneration: 1, CurrentReviewRound: 1, ProjectRoot: root,
			CurrentState: "bug_resolution", CurrentPhase: "investigation",
			Agent: &policy.AgentContext{ID: "investigator-1", AssignmentID: "assignment-1", HypothesisResultAssignment: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "deny" {
		t.Fatalf("teammate_name alone must not grant an S8 write, got %+v", decision)
	}
}
