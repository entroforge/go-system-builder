package policy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func evaluateReviewResultPath(t *testing.T, root, agentID, path string, grant bool) policy.Decision {
	t.Helper()
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "control", "hook-policy.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var paths []string
	if grant {
		paths = []string{".claude/submissions/s7/loop-test/g1/r1/assignment-1/reviewer-1/answer.json"}
	}
	decision, err := engine.Evaluate(policy.Input{
		Event:    "PreToolUse",
		AgentID:  agentID,
		ToolName: "Write",
		ToolInput: map[string]any{
			"file_path": path,
		},
		Runtime: policy.RuntimeContext{
			PacketMode:              true,
			ReviewResultAnswerPaths: paths,
			ProjectRoot:             root,
			CurrentState:            "verification",
			Agent: &policy.AgentContext{
				ID:               "reviewer-1",
				AssignmentID:     "review-assignment-1",
				ReviewAssignment: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return decision
}

func TestReviewResultPacketWriteRequiresExactTrustedCurrentGrant(t *testing.T) {
	root := t.TempDir()
	issued := ".claude/submissions/s7/loop-test/g1/r1/assignment-1/reviewer-1/answer.json"
	if decision := evaluateReviewResultPath(t, root, "reviewer-1", issued, true); decision.Decision == "deny" || decision.Decision == "block" {
		t.Fatalf("exact current issued answer path should be allowed, got %+v", decision)
	}

	for _, tc := range []struct {
		name    string
		agentID string
		path    string
		grant   bool
	}{
		{name: "unissued sibling path", agentID: "reviewer-1", path: ".claude/submissions/s7/loop-test/g1/r1/assignment-1/reviewer-1/other.json", grant: true},
		{name: "wrong payload owner", agentID: "reviewer-2", path: issued, grant: true},
		{name: "missing current packet grant", agentID: "reviewer-1", path: issued, grant: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decision := evaluateReviewResultPath(t, root, tc.agentID, tc.path, tc.grant)
			if decision.Decision != "deny" {
				t.Fatalf("unissued or mismatched answer path must deny, got %+v", decision)
			}
		})
	}
}

func TestReviewResultPacketWriteRejectsSymlinkedIssuedPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	parent := filepath.Join(root, ".claude", "submissions", "s7")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(parent, "loop-test")); err != nil {
		t.Fatal(err)
	}
	issued := ".claude/submissions/s7/loop-test/g1/r1/assignment-1/reviewer-1/answer.json"
	decision := evaluateReviewResultPath(t, root, "reviewer-1", issued, true)
	if decision.Decision != "deny" {
		t.Fatalf("symlinked answer path must deny, got %+v", decision)
	}
}
