package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestReviewPlanGuidancePreservesOriginalResponsibility(t *testing.T) {
	for _, name := range []string{"same_plan_historical_number", "different_plan_same_number", "legacy", "unknown", "seed", "no_plan"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			facts := map[string]any{
				"round":       7,
				"round_entry": map[string]any{"plan_id": "plan-current"},
				"plan":        map[string]any{"plan_id": "plan-current", "path": "accepted.json", "revision": 2},
			}
			state := map[string]any{"runtime_id": "runtime-original", "lifecycle": map[string]any{"state": "verification", "phase": "planning"}, "baseline": map[string]any{"generation": 1}, "review": facts}
			registered := true
			switch name {
			case "different_plan_same_number":
				facts["round_entry"] = map[string]any{"plan_id": "plan-next", "round": 7}
				facts["plan"].(map[string]any)["review_round"] = 7
				registered = false
			case "legacy":
				facts["round_entry"] = map[string]any{"round": 7}
				facts["plan"].(map[string]any)["review_round"] = 7
			case "unknown":
				facts["round_entry"] = map[string]any{}
			case "seed":
				facts["round_entry"] = map[string]any{"plan_id": "plan-current", "transition_id": "TR-012", "baseline_generation": 1, "review_plan_seed_ref": "seed.json", "repair_handoff_ref": "handoff.json"}
				facts["repair"] = map[string]any{"review_plan_seed_ref": "seed.json", "review_plan_seed_sha256": "seed-digest", "handoff_ref": "handoff.json"}
				facts["plan"] = map[string]any{"plan_id": "plan-current", "path": "seed.json", "sha256": "seed-digest", "revision": 1}
				registered = false
			case "no_plan":
				delete(facts, "plan")
				registered = false
			}
			snapshot := runtime.Snapshot{State: state}
			request := policy.Input{Event: "PreToolUse", AgentID: "Main", Context: context.WithValue(context.Background(), hookReaderKey{}, &hookReader{Kind: "Main", snapshot: snapshot})}
			// Missing display material never reopens an accepted responsibility.
			if registered {
				if message, err := prepareReviewPlanQuestionnaireContext(root, request, snapshot); message != "" || err != nil {
					t.Fatalf("registered Plan reissued preparation: %s %v", message, err)
				}
				if result := receiveReviewPlanQuestionnaire(root, request, snapshot, "missing-original.json"); result.ReplaySame || result.Changed || !strings.Contains(result.Context, "missing-original.json") {
					t.Fatalf("missing original answer was misreported as a replay: %+v", result)
				}
			}
			observed := observableQuestionnaires(root, snapshot)
			planObserved := false
			for _, item := range observed {
				planObserved = planObserved || item.s7Plan
			}
			if planObserved == registered {
				t.Fatalf("passive observation planned=%v registered=%v", planObserved, registered)
			}
			// Pending and absent drafts are naturally silent; only preparation
			// owns missing material. Repeated ordinary Pre does not create files.
			path := reviewPlanQuestionnairePath(root, state)
			for i := 0; i < 2; i++ {
				if result := observeMainS7ReviewPlan(root, request); result.Attempted || result.Context != "" {
					t.Fatalf("missing material triggered reception: %+v", result)
				}
			}
			if path == "" {
				return // Unknown coordinates preserve the accepted pointer above.
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("listener created missing draft: %v", err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"claims":[{"target":"TODO(planner)"}]}`), 0600); err != nil {
				t.Fatal(err)
			}
			if result := observeMainS7ReviewPlan(root, request); result.Attempted || strings.Contains(result.Context, "registered") {
				t.Fatalf("draft was mistaken for professional answer: %+v", result)
			}
		})
	}
}

func TestS7ObservationRecognizesAnyRealClaimBesidePlaceholders(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want bool
	}{
		{"empty", `{"claims":[]}`, false},
		{"placeholder", `{"claims":[{"lens":"qa","target":"TODO(planner): select target","oracle":"TODO(planner): decide"}]}`, false},
		{"e2e professional oracle", `{"claims":[{"lens":"qa","oracle":"TODO(planner): decide"},{"lens":"e2e","oracle":"The route refuses the unauthenticated request"}]}`, true},
		{"claim beside seed placeholder", `{"claims":[{"lens":"qa","oracle":"PLANNER-REFINE: decide"},{"lens":"e2e","target":"entry","assertion":"visible refusal"}]}`, true},
		{"unclassified target and method", `{"claims":[{"target":"service/entry","method":"inspect the TODO(planner) token as product data"}]}`, true},
		{"placeholder text within sentence", `{"claims":[{"assertion":"the response may contain PLANNER-REFINE as user data"}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := s7ObservedPlanHasProfessionalContent([]byte(tc.data)); got != tc.want {
				t.Fatalf("professional content = %v, want %v", got, tc.want)
			}
		})
	}
}
