package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS7StalePlanRecoveryPrecedesQueuedDispatchAndAnswers(t *testing.T) {
	root := t.TempDir()
	plan := map[string]any{"plan_id": "review-plan-stale", "path": ".claude/review/plans/stale.json", "status": "stale", "review_round": 1}
	state := map[string]any{
		"runtime_id": "loop-S7-stale", "baseline": map[string]any{"generation": 1},
		"lifecycle":       map[string]any{"state": "verification", "phase": "running"},
		"journal":         map[string]any{"path": ".claude/loop-events.jsonl"},
		"last_transition": map[string]any{"event_id": "evt-review-plan-stale-original"},
		"review": map[string]any{"round": 1, "plan": plan, "assignments": map[string]any{
			"assignment-e2e":    map[string]any{"status": "planned", "lens": "e2e"},
			"assignment-static": map[string]any{"status": "dispatched", "agent_id": "reviewer-static", "lens": "qa"},
		}},
	}
	guidance := policy.Guidance{Missing: []string{"claim_results", "claim:static"}}
	applyS7RecoveryProjection(root, state, &guidance)
	if !strings.Contains(guidance.Action, "Main must resolve") || len(guidance.Missing) != 1 || !strings.Contains(guidance.Missing[0], "stale ReviewPlan") || strings.Contains(strings.Join(guidance.Automation, " "), "assignments queued") {
		t.Fatalf("stale plan did not take recovery priority: %+v", guidance)
	}
	// No Plan file, issuer journal or service exists. A stale reminder uses
	// only existing facts and exact paths, never starts packet preparation.
	message := stageReviewResultLifecycleContext(context.Background(), root, policy.Input{Event: "PreToolUse", CWD: root}, nil, runtime.Snapshot{State: state}, "Main")
	for _, want := range []string{filepath.Join(root, ".claude/review/plans/stale.json"), review.PlanQuestionnairePath(root, state), filepath.Join(root, ".claude/loop-events.jsonl"), "evt-review-plan-stale-original", "status=stale"} {
		if !strings.Contains(message, want) {
			t.Fatalf("missing original stale fact/path %q: %s", want, message)
		}
	}
	for _, forbidden := range []string{"manifest-draft", "register-workgroup", "fill the professional ReviewResult", "questionnaire preparation deferred"} {
		if strings.Contains(message, forbidden) {
			t.Fatalf("stale reminder requested old work: %s", message)
		}
	}
	for _, status := range []string{"running", "cannot_clean", "discovery_draining"} {
		plan["status"] = status
		if action := s7RecoveryNextAction(root, state, status, 1, nil, []string{"assignment-e2e"}, nil, nil, nil); !strings.Contains(action, "original ReviewPlan source") {
			t.Fatalf("existing %s behavior changed: %s", status, action)
		}
	}
}
