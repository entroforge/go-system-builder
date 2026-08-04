package transition_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

// stateAtVerification returns a runtime state positioned in verification with
// one round of valid evidence, so pause/resume transitions have something to
// checkpoint.
func stateAtVerification(t *testing.T, root string) {
	t.Helper()
	// Copy the real Loop Definition so the transition engine can resolve.
	defSrc := filepath.Join("..", "..", "docs", "loop-definition.json")
	defData, err := os.ReadFile(defSrc)
	if err != nil {
		t.Fatalf("read loop-definition.json: %v", err)
	}
	defDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(defDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defDir, "loop-definition.json"), defData, 0o644); err != nil {
		t.Fatal(err)
	}
	// Use the post-migration Loop Definition SHA + a REQ-099 ID (matches the
	// REQ-NNN pattern required by the loop-state schema) so the pre-commit
	// validator accepts the fixture.
	state := map[string]any{
		"schema_version": "1.1.0",
		"runtime_id":     "loop-test",
		"definition": map[string]any{
			"path":    "docs/loop-definition.json",
			"version": "1.2.0",
			"sha256":  "31c2f880dea1aeff73354c6e4a1dc45c234739a861ddcded79efe59cfbb69c86",
		},
		"revision": 5,
		"lifecycle": map[string]any{
			"state":          "verification",
			"phase":          "delivery",
			"phase_revision": 1,
		},
		"authorization": map[string]any{
			"mode":        "loop",
			"command":     "/loop REQ-099",
			"actor":       "tester",
			"occurred_at": "2026-01-01T00:00:00Z",
		},
		"bound_req": map[string]any{
			"path":        "docs/requirements/REQ-099.md",
			"version":     "1.0.0",
			"sha256":      "31c2f880dea1aeff73354c6e4a1dc45c234739a861ddcded79efe59cfbb69c86",
			"id":          "REQ-099",
			"status":      "locked",
			"approved_by": "tester",
			"approved_at": "2026-01-01T00:00:00Z",
		},
		"baseline": map[string]any{
			"generation":  1,
			"captured_at": "2026-01-01T00:00:00Z",
		},
		"configuration": map[string]any{
			"repair": map[string]any{
				"max_attempts_per_bug":       3,
				"max_same_contract_failures": 2,
				"max_full_review_rounds":     5,
			},
		},
		"hook_control": map[string]any{
			"policy_ref": map[string]any{
				"path":    "docs/hook-policy.json",
				"version": "v1.0.0",
				"sha256":  "31c2f880dea1aeff73354c6e4a1dc45c234739a861ddcded79efe59cfbb69c86",
			},
			"mode":                 "audit",
			"health":               "healthy",
			"consecutive_failures": 0,
			"last_checked_at":      nil,
		},
		"review": map[string]any{
			"round":       1,
			"clean_round": nil,
		},
		"documents": []any{
			map[string]any{
				"id":         "REQ-099",
				"kind":       "req",
				"path":       "docs/requirements/REQ-099.md",
				"version":    "1.0.0",
				"sha256":     "31c2f880dea1aeff73354c6e4a1dc45c234739a861ddcded79efe59cfbb69c86",
				"status":     "locked",
				"generation": 1,
			},
		},
		"entities": map[string]any{
			"agents": []any{},
			"tasks":  []any{},
			"bugs":   []any{},
			"teams":  []any{},
		},
		"blockers": []any{},
		"evidence": []any{
			map[string]any{
				"id":                  "ev-1",
				"kind":                "delivery_review",
				"path":                "docs/reports/review/REV-1.md",
				"sha256":              "31c2f880dea1aeff73354c6e4a1dc45c234739a861ddcded79efe59cfbb69c86",
				"status":              "valid",
				"baseline_generation": 1,
				"review_round":        1,
				"produced_by":         []any{"agent-x"},
				"invalidated_by":      nil,
				"invalidation_rule":   nil,
				"invalidation_reason": nil,
				"responsibility_id":   "VER-REQ",
				"scope_refs":          []any{},
			},
		},
		"pause":           nil,
		"journal":         map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": float64(0), "last_event_id": nil},
		"last_transition": nil,
		"updated_at":      "2026-01-01T00:00:00Z",
	}
	writeState(t, root, state)
}

func writeState(t *testing.T, root string, state map[string]any) {
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
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readState(t *testing.T, root string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestTR010CapturePauseCheckpoint(t *testing.T) {
	root := t.TempDir()
	stateAtVerification(t, root)
	err := applyTransition(t, root, "TR-011", 5, map[string]string{
		"human_decision_record": "docs/reports/human/decision.md",
		"review_result_record":  "docs/reports/qa/QA-1.md",
		"pause_record":          "docs/reports/human/pause-record.md",
	})
	if err != nil {
		t.Fatalf("TR-011 failed: %v", err)
	}
	state := readState(t, root)
	pause, ok := state["pause"].(map[string]any)
	if !ok {
		t.Fatal("expected pause checkpoint to be captured")
	}
	requiredFields := []string{
		"from_state", "from_phase", "phase_revision", "baseline_generation",
		"review_round", "entity_snapshot_revision", "reason",
		"required_human_action", "document_fingerprints",
		"committed_idempotency_keys", "paused_at",
	}
	for _, field := range requiredFields {
		if _, present := pause[field]; !present {
			t.Errorf("pause checkpoint missing field %s", field)
		}
	}
	if pause["from_state"] != "verification" {
		t.Errorf("expected from_state=verification, got %v", pause["from_state"])
	}
	if pause["baseline_generation"] != float64(1) {
		t.Errorf("expected baseline_generation=1, got %v", pause["baseline_generation"])
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "paused" {
		t.Errorf("expected state=paused, got %v", lifecycle["state"])
	}
}

func TestTR020IncrementsBaselineAndInvalidatesEvidence(t *testing.T) {
	root := t.TempDir()
	stateAtVerification(t, root)
	// First pause.
	if err := applyTransition(t, root, "TR-011", 5, map[string]string{
		"human_decision_record": "docs/reports/human/decision.md",
		"review_result_record":  "docs/reports/qa/QA-1.md",
		"pause_record":          "docs/reports/human/pause-record.md",
	}); err != nil {
		t.Fatal(err)
	}
	// Then reinit with a new locked REQ.
	err := applyTransition(t, root, "TR-020", 6, map[string]string{
		"human_decision_record": "docs/reports/human/decision.md",
		"req_lock_record":       "docs/reports/human/req-lock.md",
	})
	if err != nil {
		t.Fatalf("TR-020 failed: %v", err)
	}
	state := readState(t, root)
	baseline, _ := state["baseline"].(map[string]any)
	if baseline["generation"] != float64(2) {
		t.Errorf("expected baseline generation 2, got %v", baseline["generation"])
	}
	evidence := state["evidence"].([]any)
	for _, raw := range evidence {
		entry := raw.(map[string]any)
		if entry["status"] != "invalid" {
			t.Errorf("expected evidence %s to be invalid after TR-020", entry["id"])
		}
		if entry["invalidated_by"] != "TR-020" {
			t.Errorf("expected invalidated_by TR-020, got %v", entry["invalidated_by"])
		}
	}
}

// applyTransition is a helper that invokes the transition engine.
func applyTransition(t *testing.T, root, transitionID string, expectedRevision int, evidence map[string]string) error {
	t.Helper()
	state := readState(t, root)
	registerFixtureEvidence(t, root, state, evidence)
	writeState(t, root, state)
	req := transition.Request{
		TransitionID:     transitionID,
		ExpectedRevision: expectedRevision,
		Actor:            "orchestrator",
		Evidence:         evidence,
	}
	_, err := transition.Apply(root,
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
		req)
	return err
}

func registerFixtureEvidence(t *testing.T, root string, state map[string]any, requested map[string]string) {
	t.Helper()
	baseline, _ := state["baseline"].(map[string]any)
	review, _ := state["review"].(map[string]any)
	generation := fixtureInt(baseline["generation"])
	round := fixtureInt(review["round"])
	items, _ := state["evidence"].([]any)
	for requirement, ref := range requested {
		found := false
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item != nil && (item["id"] == ref || item["path"] == ref) {
				found = true
				break
			}
		}
		if found {
			continue
		}
		full := filepath.Join(root, filepath.Clean(ref))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		content := []byte("fixture evidence: " + requirement + "\n")
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
		var reviewRound any
		if round > 0 {
			reviewRound = round
		}
		kind := "human_decision"
		if requirement == "review_result_record" {
			kind = "qa_review"
		}
		items = append(items, map[string]any{
			"id": "fixture-" + requirement, "kind": kind, "path": ref,
			"sha256": transition.SHA256(content), "status": "valid", "baseline_generation": generation,
			"review_round": reviewRound, "produced_by": []any{"fixture"}, "invalidated_by": nil,
			"invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": nil, "scope_refs": []any{},
		})
	}
	state["evidence"] = items
}

func fixtureInt(value any) int {
	switch number := value.(type) {
	case float64:
		return int(number)
	case int:
		return number
	default:
		return 0
	}
}
