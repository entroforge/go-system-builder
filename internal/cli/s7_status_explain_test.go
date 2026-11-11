package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The status board explains natural answer reception without reviving a wave gate or manual submit.
func TestS7StatusExplain(t *testing.T) {
	root := t.TempDir()
	state := minimalS7ExplainState(t, root)
	writeS7ExplainRuntime(t, root, state)
	var output bytes.Buffer
	if code := runS7Status(root, &output, true); code != 0 {
		t.Fatalf("status exit=%d: %s", code, output.String())
	}
	for _, forbidden := range []string{"wave readiness", "A-completeness", "B-admission", "review-result submit", "workspace-digest"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("obsolete gate %q appears: %s", forbidden, output.String())
		}
	}
	if !strings.Contains(output.String(), "natural events receive each answer") {
		t.Fatalf("missing original answer guidance: %s", output.String())
	}
}

// minimalS7ExplainState builds a round-1 plan pointer with one static-wave
// QA Assignment (claim still planned) and one behavior-wave E2E Assignment,
// so the wave gate is active and RemainingStaticClaims counts exactly 1.
func minimalS7ExplainState(t *testing.T, root string) map[string]any {
	t.Helper()
	state := map[string]any{
		"runtime_id": "runtime-explain-1",
		"revision":   1,
		"journal":    map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil},
		"lifecycle":  map[string]any{"state": "verification", "phase": "review"},
		"entities":   map[string]any{"tasks": []any{}},
		"review": map[string]any{
			"round":       1,
			"clean_round": nil,

			"claims": map[string]any{},
			"assignments": map[string]any{
				"assignment-static-1":   map[string]any{"lens": "qa", "status": "planned", "agent_id": nil, "queued_agent_id": nil},
				"assignment-behavior-1": map[string]any{"lens": "e2e", "status": "planned", "agent_id": nil, "queued_agent_id": nil},
			},
		},
	}
	planBytes, err := json.MarshalIndent(map[string]any{
		"schema_version": "1.0.0",
		"review_plan_id": "review-plan-explain-1",
		"review_round":   1,
		"claims": []any{
			map[string]any{
				"claim_id": "claim-static-1", "lens": "qa", "target": "internal/example",
				"assertion": "errors propagate", "oracle": "no dropped error",
				"method": "code review", "applicability": "required", "source_refs": []string{"REQ-001"},
			},
			map[string]any{
				"claim_id": "claim-behavior-1", "lens": "e2e", "target": "login flow",
				"assertion": "login works", "oracle": "dashboard renders",
				"method": "e2e", "applicability": "required", "source_refs": []string{"REQ-001"},
			},
		},
		"assignments": []any{
			map[string]any{
				"assignment_id": "assignment-static-1", "lens": "qa",
				"claim_ids": []string{"claim-static-1"}, "execution_wave": "static",
				"non_overlap_boundary": "owns the qa claim only",
			},
			map[string]any{
				"assignment_id": "assignment-behavior-1", "lens": "e2e",
				"claim_ids": []string{"claim-behavior-1"}, "execution_wave": "behavior",
				"non_overlap_boundary": "owns the e2e claim only",
			},
		},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	planBytes = append(planBytes, '\n')
	planRel := filepath.Join(".claude", "review", "plans", "review-plan-explain-1.json")
	state["review"].(map[string]any)["plan"] = map[string]any{
		"plan_id":            "review-plan-explain-1",
		"path":               filepath.ToSlash(planRel),
		"revision":           1,
		"review_round":       1,
		"status":             "running",
		"e2e_coverage_state": "regression_available",
		"submitted_at":       "2026-08-27T00:00:00Z",
	}
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(planRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, planRel), planBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return state
}

// writeS7ExplainRuntime persists the state map the way the runtime verbs do.
func writeS7ExplainRuntime(t *testing.T, root string, state map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
