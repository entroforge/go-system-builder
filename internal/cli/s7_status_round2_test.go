package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// TestS7StatusReportsRoundBudget proves the round counter / budget projection
// the S7 round-2 Main agent uses to orient itself (L3-S7 complexity review
// round-5 gap: "no documented way to confirm this is round N of M").
func TestS7StatusReportsRoundBudget(t *testing.T) {
	root := t.TempDir()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "schema", "assets", "loop-state.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["revision"] = 1
	state["runtime_id"] = "loop-REQ-S7ROUND"
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["lifecycle"] = map[string]any{"state": "verification", "phase": "planned", "phase_revision": 0}
	state["baseline"] = map[string]any{"generation": 1}
	state["configuration"] = map[string]any{
		"repair": map[string]any{
			"max_attempts_per_bug":       3,
			"max_same_contract_failures": 2,
			"max_full_review_rounds":     5,
		},
	}
	state["review"] = map[string]any{
		"round": 2, "clean_round": nil,
		"round_entry": map[string]any{
			"transition_id": "TR-012", "repair_handoff_ref": ".claude/review/repair/handoff.json",
			"review_plan_seed_ref": ".claude/review/repair/s7-seeds/review-plan-s9-round-2.json",
			"change_impact_ref":    ".claude/review/repair/change-impact.json",
		},
		"plan": map[string]any{
			"plan_id": "review-plan-r2", "path": ".claude/review/plans/r2.json",
			"sha256": "deadbeef", "revision": 1, "review_round": 2,
			"status": "running", "e2e_coverage_state": "not_applicable",
			"submitted_at": "2026-08-24T07:00:00Z",
		},
		"claims": map[string]any{}, "assignments": map[string]any{}, "observation_batch": nil,
	}
	state["entities"] = map[string]any{
		"agents": []any{
			map[string]any{
				"id": "agent-qa-r2", "role": "qa", "state": "blocked",
				"task_ids": []any{"TASK-1"}, "team_id": "workgroup-r2",
				"definition_ref": "agents/qa.md",
				"readback_ref":   nil, "activation_ref": nil, "activation_revision": nil,
				"blocker_resolved_ref": nil,
				"updated_at":           "2026-08-24T07:00:00Z",
			},
		},
		"tasks": []any{}, "bugs": []any{}, "teams": []any{},
	}
	state["evidence"] = []any{}
	state["documents"] = []any{}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	marshalled, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(marshalled, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	// seed the plan pointer + a blocked assignment with blocker_ref so the
	// board prints both projections.
	stateBytes, _ := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	var loaded map[string]any
	_ = json.Unmarshal(stateBytes, &loaded)
	reviewMap, _ := loaded["review"].(map[string]any)
	planMap := reviewMap["plan"].(map[string]any)
	planRel := planMap["path"].(string)
	planBytes := []byte(`{"schema_version":"1.0.0","review_plan_id":"review-plan-r2","review_round":2,"baseline_generation":1,"frozen_subjects":[],"claims":[{"claim_id":"claim-qa-1","lens":"qa","focus_key":"logic-state-error","target":"internal/example","assertion":"errors propagate","oracle":"no dropped error","method":"code review","applicability":"required","source_refs":["REQ-002"]}],"assignments":[{"assignment_id":"assignment-qa-r2","lens":"qa","claim_ids":["claim-qa-1"],"focus_keys":["logic-state-error"],"non_overlap_boundary":"owns static quality","execution_wave":"static"}]}`)
	planDir := filepath.Dir(filepath.Join(root, planRel))
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, planRel), planBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	planMap["sha256"] = mustSha256Hex(t, planBytes)
	reviewMap["assignments"] = map[string]any{
		"assignment-qa-r2": map[string]any{
			"agent_id": "agent-qa-r2", "claim_ids": []string{"claim-qa-1"},
			"lens": "qa", "status": "blocked",
			"blocker_ref": ".claude/evidence/loop-REQ-S7ROUND/g1/review-blockers/review-result-qa-r2-sitelost-site-lost.json",
			"blocked_at":  "2026-08-24T07:00:00Z",
		},
	}
	marshalled, _ = json.MarshalIndent(loaded, "", "  ")
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(marshalled, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := cli.Run([]string{"s7", "status", "--root", root}, strings.NewReader(""), &out, &out); code != 0 {
		t.Fatalf("s7 status exit=%d output=%s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"S7 review board (round 2 of 5)",
		"status=blocked",
		"blocker_ref=.claude/evidence/loop-REQ-S7ROUND/g1/review-blockers/review-result-qa-r2-sitelost-site-lost.json",
		"blocker_resolved",
		"round_entry: TR-012 (S9 handoff seed)",
		"seed_projection: present",
		"focus=logic-state-error target=internal/example",
		"pending required claims: 1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("s7 status missing %q:\n%s", want, got)
		}
	}
}

func TestS7StatusReportsSeedProjectionGap(t *testing.T) {
	root := t.TempDir()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "schema", "assets", "loop-state.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["review"] = map[string]any{
		"round": 2,
		"round_entry": map[string]any{
			"transition_id":        "TR-012",
			"review_plan_seed_ref": ".claude/review/repair/s7-seeds/review-plan-s9-round-2.json",
		},
		"plan":   nil,
		"claims": map[string]any{}, "assignments": map[string]any{}, "observation_batch": nil,
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	stateBytes, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(stateBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := cli.Run([]string{"s7", "status", "--root", root}, strings.NewReader(""), &out, &out); code != 0 {
		t.Fatalf("s7 status exit=%d output=%s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"seed_projection: missing",
		"review.plan is absent",
		"reconcile the S9 handoff projection",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("s7 status missing %q:\n%s", want, got)
		}
	}
}

func mustSha256Hex(t *testing.T, data []byte) string {
	t.Helper()
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
