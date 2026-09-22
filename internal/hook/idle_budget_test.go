package hook

import (
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/policy"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdleBudgetIgnoresRevisionChurnAndPreservesSafety(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".claude"), 0700)
	state := map[string]any{"runtime_id": "loop-test", "baseline": map[string]any{"generation": 1}, "entities": map[string]any{"agents": []any{map[string]any{"id": "worker", "state": "working"}}}}
	input := policy.Input{Event: "TeammateIdle", SessionID: "session", AgentID: "worker"}
	decision := policy.Decision{RuleID: RuleTeammateIdleResumeAssignment, Reason: "missing result"}
	for i := 1; i <= 4; i++ {
		state["revision"] = i
		data, _ := json.Marshal(state)
		os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), data, 0600)
		exhausted, err := IdleRecoveryExhausted(root, input, decision)
		if err != nil || exhausted != (i > 2) {
			t.Fatalf("attempt %d exhausted=%v err=%v", i, exhausted, err)
		}
	}
	decision.RuleID = "phase_product_write"
	if exhausted, err := IdleRecoveryExhausted(root, input, decision); exhausted || err != nil {
		t.Fatalf("safety denial budgeted: %v %v", exhausted, err)
	}
	decision.RuleID = RuleTeammateIdleResumeAssignment
	decision.Reason = "new blocking fact"
	if exhausted, err := IdleRecoveryExhausted(root, input, decision); exhausted || err != nil {
		t.Fatalf("new recovery must have its own budget: %v %v", exhausted, err)
	}
	input.SessionID = "resumed-session"
	if exhausted, err := IdleRecoveryExhausted(root, input, decision); exhausted || err != nil {
		t.Fatalf("session isolation: %v %v", exhausted, err)
	}
}

func TestStopIdleRecoveryUsesAssignmentKind(t *testing.T) {
	for _, event := range []string{"TeammateIdle", "SubagentStop"} {
		agent := &policy.AgentContext{ID: "builder", AssignmentID: "assignment-builder", PlanReportedRef: "plan.json"}
		d := stopIdleBlock(event, agent)
		if strings.Contains(strings.Join(d.Recovery, " "), "review-result") {
			t.Fatal("builder directed to ReviewResult")
		}
		agent.ReviewAssignment = true
		d = stopIdleBlock(event, agent)
		if !strings.Contains(strings.Join(d.Recovery, " "), "review-result") {
			t.Fatal("review assignment missing canonical result path")
		}
	}
}
