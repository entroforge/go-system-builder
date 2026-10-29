package hook

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
)

func TestMainStopDecisionBlocksOnlyHardPendingReviewWork(t *testing.T) {
	cases := []struct {
		name         string
		assignments  map[string]any
		wantBlocked  bool
		wantRule     string
		wantRecovery string
	}{
		{
			name: "queued assignment must be dispatched",
			assignments: map[string]any{
				"assignment-e2e-1": map[string]any{"status": "planned", "agent_id": nil},
			},
			wantBlocked:  true,
			wantRule:     RuleMainStopPendingDispatch,
			wantRecovery: "dispatch",
		},
		{
			name: "submitted result must be consumed",
			assignments: map[string]any{
				"assignment-qa-1": map[string]any{"status": "result_submitted", "result_ref": "docs/review/result.json", "agent_id": "agent-qa-1"},
			},
			wantBlocked:  true,
			wantRule:     RuleMainStopUnconsumedResult,
			wantRecovery: "consume",
		},
		{
			name: "working assignment may finish asynchronously",
			assignments: map[string]any{
				"assignment-qa-1": map[string]any{"status": "dispatched", "agent_id": "agent-qa-1"},
			},
			wantBlocked: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := &review.Plan{}
			for id := range tc.assignments {
				plan.Assignments = append(plan.Assignments, review.PlanAssignment{AssignmentID: id})
			}
			decision, blocked := mainStopDecisionForState(map[string]any{
				"lifecycle": map[string]any{"state": "verification", "phase": "running"},
				"review":    map[string]any{"round": 1, "plan": map[string]any{"plan_id": "current", "path": "plan.json", "review_round": 1, "status": "running"}, "assignments": tc.assignments},
			}, policy.Input{Event: "Stop"}, plan)
			if blocked != tc.wantBlocked {
				t.Fatalf("blocked=%v, want %v; decision=%#v", blocked, tc.wantBlocked, decision)
			}
			if !tc.wantBlocked {
				return
			}
			if decision.Decision != "deny" || decision.RuleID != tc.wantRule {
				t.Fatalf("decision=%q rule=%q, want deny/%q", decision.Decision, decision.RuleID, tc.wantRule)
			}
			if !strings.Contains(strings.ToLower(strings.Join(decision.Recovery, "\n")), tc.wantRecovery) {
				t.Fatalf("recovery=%v, want %q", decision.Recovery, tc.wantRecovery)
			}
		})
	}
}

func TestMainStopDecisionHonorsPlatformLoopGuard(t *testing.T) {
	decision, blocked := mainStopDecisionForState(map[string]any{
		"review": map[string]any{
			"assignments": map[string]any{
				"assignment-e2e-1": map[string]any{"status": "planned"},
			},
		},
	}, policy.Input{Event: "Stop", StopHookActive: true})
	if blocked {
		t.Fatalf("stop_hook_active must allow, got %#v", decision)
	}
}

func TestMainStopDecisionIgnoresMalformedRuntime(t *testing.T) {
	if _, blocked := mainStopDecisionForState(nil, policy.Input{Event: "Stop"}); blocked {
		t.Fatal("missing runtime facts must fail open")
	}
}
