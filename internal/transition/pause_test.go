package transition_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
			"phase":          "running",
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
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
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

// seedPauseCheckpoint writes the authoritative pause checkpoint the way the
// S7 verdict transaction (runtime review-result submit) creates it — TR-010/
// TR-011 then only move the cursor (L3-S7 §9.2 single-carrier).
func seedPauseCheckpoint(t *testing.T, root string) {
	t.Helper()
	state := readState(t, root)
	lifecycle := state["lifecycle"].(map[string]any)
	state["pause"] = map[string]any{
		"from_state":            lifecycle["state"],
		"from_phase":            lifecycle["phase"],
		"phase_revision":        lifecycle["phase_revision"],
		"baseline_generation":   1,
		"review_round":          1,
		"reason":                "S7 review verdict: release_blocked",
		"required_human_action": "review the blocking finding or REQ change, then resume or re-lock the REQ",
		"document_fingerprints": []any{},
		"paused_at":             "2026-01-02T00:00:00Z",
	}
	writeState(t, root, state)
}

func TestTR010CapturePauseCheckpoint(t *testing.T) {
	root := t.TempDir()
	stateAtVerification(t, root)

	// L3-S7: TR-011 refuses to move without the verdict-created checkpoint.
	err := applyTransition(t, root, "TR-011", 5, map[string]string{
		"review_result_record": "docs/reports/qa/QA-1.md",
	})
	if err == nil || !strings.Contains(err.Error(), "pause checkpoint missing") {
		t.Fatalf("TR-011 without a checkpoint must fail, got %v", err)
	}

	// With the checkpoint in place, TR-011 moves the cursor and the
	// checkpoint survives untouched (single writer).
	seedPauseCheckpoint(t, root)
	err = applyTransition(t, root, "TR-011", 5, map[string]string{
		"review_result_record": "docs/reports/qa/QA-1.md",
	})
	if err != nil {
		t.Fatalf("TR-011 failed: %v", err)
	}
	state := readState(t, root)
	pause, ok := state["pause"].(map[string]any)
	if !ok {
		t.Fatal("expected pause checkpoint to survive the transition")
	}
	requiredFields := []string{
		"from_state", "from_phase", "phase_revision", "baseline_generation",
		"review_round", "reason",
		"required_human_action", "document_fingerprints",
		"paused_at",
	}
	for _, field := range requiredFields {
		if _, present := pause[field]; !present {
			t.Errorf("pause checkpoint missing field %s", field)
		}
	}
	if pause["from_state"] != "verification" {
		t.Errorf("expected from_state=verification, got %v", pause["from_state"])
	}
	if pause["paused_at"] != "2026-01-02T00:00:00Z" {
		t.Errorf("checkpoint was rewritten (paused_at=%v); the verdict transaction is the single writer", pause["paused_at"])
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "paused" {
		t.Errorf("expected state=paused, got %v", lifecycle["state"])
	}
}

func TestTR020IncrementsBaselineAndInvalidatesEvidence(t *testing.T) {
	root := t.TempDir()
	stateAtVerification(t, root)
	// First pause: the verdict transaction creates the checkpoint, TR-011
	// moves the cursor (L3-S7 single-carrier).
	seedPauseCheckpoint(t, root)
	if err := applyTransition(t, root, "TR-011", 5, map[string]string{
		"review_result_record": "docs/reports/qa/QA-1.md",
	}); err != nil {
		t.Fatal(err)
	}
	// Amend with the same REQ at a strictly higher version: write the file,
	// then apply TR-020 with its metadata.
	if err := os.MkdirAll(filepath.Join(root, "docs", "requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	amended := "# REQ-099\n\n> 状态：locked\n> 版本：1.1.0\n> UI impact：none\n"
	reqPath := filepath.Join(root, "docs", "requirements", "REQ-099.md")
	if err := os.WriteFile(reqPath, []byte(amended), 0o644); err != nil {
		t.Fatal(err)
	}
	shaHex := fmt.Sprintf("%x", sha256.Sum256([]byte(amended)))
	state := readState(t, root)
	registerFixtureEvidence(t, root, state, map[string]string{
		"human_decision_record": "docs/reports/human/decision.md",
	})
	scopeFixtureEvidence(t, state, "docs/reports/human/decision.md", "runtime_amend:loop-test@6")
	writeState(t, root, state)
	next, err := transition.Apply(root,
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
		transition.Request{
			TransitionID:     "TR-020",
			ExpectedRevision: 6,
			Actor:            "orchestrator",
			Evidence: map[string]string{
				"human_decision_record": "docs/reports/human/decision.md",
				"req_lock_record":       "docs/requirements/REQ-099.md@" + shaHex,
			},
			REQ: &transition.LockedREQ{
				ID: "REQ-099", Path: "docs/requirements/REQ-099.md",
				Version: "1.1.0", SHA256: shaHex,
				ApprovedBy: "tester", ApprovedAt: "2026-01-02T00:00:00Z",
			},
		})
	if err != nil {
		t.Fatalf("TR-020 failed: %v", err)
	}
	after := next.State
	generationInt := func(v any) int {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		default:
			return -1
		}
	}
	baseline, _ := after["baseline"].(map[string]any)
	if generationInt(baseline["generation"]) != 2 {
		t.Errorf("expected baseline generation 2, got %v", baseline["generation"])
	}
	evidence := after["evidence"].([]any)
	for _, raw := range evidence {
		entry := raw.(map[string]any)
		if entry["status"] != "invalid" {
			t.Errorf("expected evidence %s to be invalid after TR-020", entry["id"])
		}
		if entry["invalidated_by"] != "TR-020" {
			t.Errorf("expected invalidated_by TR-020, got %v", entry["invalidated_by"])
		}
	}
	// The amended REQ really enters the runtime (v4 P3 completion).
	bound, _ := after["bound_req"].(map[string]any)
	if bound["version"] != "1.1.0" || bound["sha256"] != shaHex {
		t.Errorf("bound_req not swapped to the amended REQ: %+v", bound)
	}
	generations := map[int]bool{}
	for _, raw := range after["documents"].([]any) {
		doc, _ := raw.(map[string]any)
		if doc["kind"] == "req" {
			if doc["status"] != "locked" {
				t.Errorf("req document entry must stay locked, got %v", doc["status"])
			}
			generations[generationInt(doc["generation"])] = true
		}
	}
	if !generations[1] || !generations[2] {
		t.Errorf("expected locked req documents for both generations, got %v", generations)
	}
	// Leaving paused clears the checkpoint (pause-residue fix).
	if after["pause"] != nil {
		t.Errorf("pause checkpoint must be cleared when leaving paused, got %v", after["pause"])
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

// scopeFixtureEvidence stamps a lifecycle-verb scope onto a registered
// human_decision fixture item, mirroring what the lifecycle CLI verbs
// record (`<scope>:<runtime_id>@<revision>`).
func scopeFixtureEvidence(t *testing.T, state map[string]any, ref, scope string) {
	t.Helper()
	items, _ := state["evidence"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil || (item["id"] != ref && item["path"] != ref) {
			continue
		}
		item["scope_refs"] = []any{scope}
		return
	}
	t.Fatalf("scopeFixtureEvidence: evidence %q not found", ref)
}

// TestTR004InvalidatesConsumedFixRecord verifies the rework retest (the
// batch-D ledger claimed but did not deliver): the fix_required record that
// triggers TR-004 is invalidated at commit — without it, a fix that changes
// no registered document re-selects TR-004 forever.
func TestTR004InvalidatesConsumedFixRecord(t *testing.T) {
	root := t.TempDir()
	setupRepoWithDefinition(t, root)
	state := inactiveState(5)
	state["lifecycle"] = map[string]any{"state": "document_verification", "phase": nil, "phase_revision": float64(1)}
	state["baseline"] = map[string]any{"generation": float64(1), "captured_at": "2026-08-18T00:00:00Z"}
	// req_baseline_unchanged is a real fingerprint guard now — seed a bound
	// REQ whose on-disk bytes match the registered sha256.
	reqData := []byte("# REQ\n> Status: locked\n")
	if err := os.MkdirAll(filepath.Join(root, "docs", "requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "requirements", "REQ-001.md"), reqData, 0o644); err != nil {
		t.Fatal(err)
	}
	state["bound_req"] = map[string]any{
		"id": "REQ-001", "path": "docs/requirements/REQ-001.md", "version": "v1",
		"sha256": transition.SHA256(reqData), "status": "locked",
		"approved_by": "pm-1", "approved_at": "2026-08-18T00:00:00Z",
	}
	writeFullState(t, root, state)
	// The consumed fix record: valid document_review with requested_event.
	fixEnvelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-dv-fix", "kind": "document_review",
		"runtime_id": "loop-inactive", "baseline_generation": 1,
		"producer_agent_id": "dv-spec-1", "producer_responsibility": "DV-SPEC-CONSISTENCY",
		"conclusion": "fix_required", "requested_event": "document_fix_required",
		"created_at": "2026-08-18T00:00:00Z",
	}
	fixData, _ := json.Marshal(fixEnvelope)
	fixPath := "evidence/dv-fix.json"
	if err := os.MkdirAll(filepath.Join(root, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, fixPath), fixData, 0o644); err != nil {
		t.Fatal(err)
	}
	st := readState(t, root)
	items, _ := st["evidence"].([]any)
	items = append(items, map[string]any{
		"id": "ev-dv-fix", "kind": "document_review", "path": fixPath,
		"sha256": transition.SHA256(fixData), "status": "valid", "baseline_generation": float64(1),
		"review_round": nil, "produced_by": []any{"dv-spec-1"}, "invalidated_by": nil,
		"responsibility_id": "DV-SPEC-CONSISTENCY", "scope_refs": []any{},
	})
	st["evidence"] = items
	writeFullState(t, root, st)

	if err := applyT(t, root, "TR-004", 5, "hook_controller", map[string]string{
		"document_review_record": "ev-dv-fix",
	}); err != nil {
		t.Fatalf("TR-004 failed: %v", err)
	}
	after := readState(t, root)
	for _, raw := range after["evidence"].([]any) {
		item := raw.(map[string]any)
		if item["id"] != "ev-dv-fix" {
			continue
		}
		if item["status"] != "invalid" || item["invalidation_rule"] != "consumed_fix_record" {
			t.Fatalf("consumed fix record must be invalid with consumed_fix_record rule, got status=%v rule=%v", item["status"], item["invalidation_rule"])
		}
		return
	}
	t.Fatal("ev-dv-fix not found after TR-004")
}
