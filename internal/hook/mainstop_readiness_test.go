package hook

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
)

func TestMainStopRequiresCurrentExecutableReviewWork(t *testing.T) {
	for _, scenario := range []string{"ready", "static-wait", "dependency-wait", "resource-wait", "queued-owner", "sealed", "sealed-cursor", "s8", "pause", "stale", "old-round", "missing-plan"} {
		t.Run(scenario, func(t *testing.T) {
			plan := &review.Plan{
				Claims:      []review.Claim{{ClaimID: "static", Applicability: "required"}, {ClaimID: "next", Applicability: "required"}},
				Assignments: []review.PlanAssignment{{AssignmentID: "static", ExecutionWave: "static", ClaimIDs: []string{"static"}}, {AssignmentID: "next", ExecutionWave: "static", ClaimIDs: []string{"next"}}},
			}
			candidate := map[string]any{"status": "planned", "agent_id": nil, "result_ref": nil}
			holder := map[string]any{"status": "dispatched", "agent_id": "qa", "result_ref": nil}
			pointer := map[string]any{"plan_id": "current", "path": "plan.json", "review_round": 1, "status": "running"}
			state := map[string]any{"schema_version": "1.2.0", "lifecycle": map[string]any{"state": "verification", "phase": "running"}, "review": map[string]any{
				"round": 1, "plan": pointer, "assignments": map[string]any{"next": candidate, "static": holder},
				"claims": map[string]any{"static": map[string]any{"disposition": "running"}, "next": map[string]any{"disposition": "planned"}},
			}}
			switch scenario {
			case "static-wait":
				plan.Assignments[1].ExecutionWave = "behavior"
			case "dependency-wait":
				plan.Claims[1].DependsOn = []string{"static"}
			case "resource-wait":
				candidate["resource_locks"], holder["resource_locks"] = []any{"port:8080"}, []any{"port:8080"}
			case "queued-owner":
				candidate["queued_agent_id"] = "registered-reviewer"
			case "sealed":
				pointer["status"] = "observation_sealed"
			case "sealed-cursor":
				state["lifecycle"] = map[string]any{"state": "verification", "phase": "observation_sealed"}
			case "s8":
				state["lifecycle"] = map[string]any{"state": "bug_resolution", "phase": "investigation"}
			case "pause":
				state["pause"] = map[string]any{"reason": "actual decision pending"}
			case "stale":
				pointer["status"] = "stale"
			case "old-round":
				pointer["review_round"] = 2
			case "missing-plan":
				plan = nil
			}
			decision, blocked := mainStopDecisionForState(state, policy.Input{Event: "Stop"}, plan)
			if blocked != (scenario == "ready") {
				t.Fatalf("blocked=%v: %#v", blocked, decision)
			}
		})
	}
}
