package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestS7StatusExplain proves the RC-12 Step A contract on `s7 status`:
//
//  1. The default board stays compact — the wave gate renders as the
//     single "wave readiness" one-liner, never the explain expansion.
//  2. `--explain` expands the gate into the three named lines
//     (A-completeness, B-admission, next-verb) and projects the exact
//     CLI verb for the blocked case.
//  3. --explain is read-only: it must not add state or consume the batch.
func TestS7StatusExplain(t *testing.T) {
	root := t.TempDir()
	state := minimalS7ExplainState(t, root)
	writeS7ExplainRuntime(t, root, state)

	var compact bytes.Buffer
	if code := runS7Status(root, &compact, false); code != 0 {
		t.Fatalf("s7 status compact exit=%d output:\n%s", code, compact.String())
	}
	if !strings.Contains(compact.String(), "wave readiness: behavior dispatch is blocked — 1 static-wave claim(s) still awaiting a disposition") {
		t.Fatalf("compact board must keep the one-liner gate:\n%s", compact.String())
	}
	for _, forbidden := range []string{"explain A-completeness", "explain B-admission", "explain next-verb"} {
		if strings.Contains(compact.String(), forbidden) {
			t.Fatalf("compact board must not render %q without --explain:\n%s", forbidden, compact.String())
		}
	}

	var explained bytes.Buffer
	if code := runS7Status(root, &explained, true); code != 0 {
		t.Fatalf("s7 status --explain exit=%d output:\n%s", code, explained.String())
	}
	out := explained.String()
	for _, want := range []string{
		"explain A-completeness: 1 required static-wave claim(s) still awaiting a pass/finding/blocked disposition (static settled=false)",
		"explain B-admission: behavior-wave dispatch is BLOCKED",
		"explain next-verb: `loop-harness runtime review-result submit --assignment-id <static-assignment-id> --result <result.json>`",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("explain board lacks %q:\n%s", want, out)
		}
	}

	// Read-only: the explain run must not consume the plan pointer or
	// mutate the runtime projection.
	after, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), `"plan_id": "review-plan-explain-1"`) {
		t.Fatalf("explain must not mutate the registered plan pointer:\n%s", string(after))
	}
}

// TestS7StatusExplainAdmittedAfterStaticSettles drives the B-admission
// flip: once the static claim reaches a final disposition, --explain must
// admit the behavior wave and project the manifest-draft → register-workgroup
// verb pair instead of the blocked submit verb.
func TestS7StatusExplainAdmittedAfterStaticSettles(t *testing.T) {
	root := t.TempDir()
	state := minimalS7ExplainState(t, root)
	claims, _ := state["review"].(map[string]any)["claims"].(map[string]any)
	claims["claim-static-1"] = map[string]any{
		"claim_id": "claim-static-1", "lens": "qa", "disposition": "pass",
		"applicability": "required", "assignment_id": "assignment-static-1",
	}
	writeS7ExplainRuntime(t, root, state)

	var explained bytes.Buffer
	if code := runS7Status(root, &explained, true); code != 0 {
		t.Fatalf("s7 status --explain exit=%d output:\n%s", code, explained.String())
	}
	out := explained.String()
	for _, want := range []string{
		"explain A-completeness: 0 required static-wave claim(s) still awaiting a pass/finding/blocked disposition (static settled=true)",
		"explain B-admission: behavior-wave dispatch is ADMITTED",
		"explain next-verb: `loop-harness s7 manifest-draft --assignment <behavior-assignment-id> --out <manifest.json>`",
		"`loop-harness runtime register-workgroup --manifest <manifest.json> --task-id <TASK> --task <task.md>`",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("explain board lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "BLOCKED") {
		t.Fatalf("settled static set must not render BLOCKED:\n%s", out)
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
			"round":             1,
			"clean_round":       nil,
			"observation_batch": nil,
			"claims":            map[string]any{},
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
		"sha256":             s7ExplainSHA(planBytes),
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

// s7ExplainSHA mirrors the runtime's artifact digest convention.
func s7ExplainSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
