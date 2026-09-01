package transition_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

// TestHumanDecisionScopeBinding pins the transition-layer human gate: a
// human_decision approval authorizes exactly one semantic verb on one runtime;
// Runtime revision is not part of the human handoff.
func TestHumanDecisionScopeBinding(t *testing.T) {
	build := func(t *testing.T) (string, map[string]any) {
		t.Helper()
		root := t.TempDir()
		setupRepoWithDefinition(t, root)
		state := stateAtVerificationMap(5)
		state["lifecycle"] = map[string]any{"state": "paused", "phase": nil, "phase_revision": float64(2)}
		state["pause"] = map[string]any{
			"from_state": "verification", "from_phase": "running", "phase_revision": float64(1),
			"baseline_generation": float64(1), "review_round": nil, "reason": "fixture", "required_human_action": "fixture",
			"document_fingerprints": []any{}, "paused_at": "2026-01-01T00:00:00Z",
		}
		registerFixtureEvidence(t, root, state, map[string]string{
			"human_decision_record": "docs/reports/human/decision.md",
		})
		return root, state
	}
	const ref = "docs/reports/human/decision.md"

	t.Run("wrong verb scope is rejected", func(t *testing.T) {
		root, state := build(t)
		scopeFixtureEvidence(t, state, ref, "runtime_pause:loop-test@5")
		writeFullState(t, root, state)
		err := applyT(t, root, "TR-019", 5, "user", map[string]string{
			"human_decision_record": ref, "pause_record": "generated:pause_checkpoint",
		})
		if err == nil {
			t.Fatal("a pause approval must not authorize a resume (TR-019)")
		}
	})

	t.Run("stale revision scope is rejected", func(t *testing.T) {
		root, state := build(t)
		scopeFixtureEvidence(t, state, ref, "runtime_resume:loop-test@4")
		writeFullState(t, root, state)
		err := applyT(t, root, "TR-019", 5, "user", map[string]string{
			"human_decision_record": ref, "pause_record": "generated:pause_checkpoint",
		})
		if err == nil {
			t.Fatal("a revision-4 approval must not authorize the revision-5 resume")
		}
	})

	t.Run("unscoped evidence is rejected", func(t *testing.T) {
		root, state := build(t)
		writeFullState(t, root, state)
		err := applyT(t, root, "TR-019", 5, "user", map[string]string{
			"human_decision_record": ref, "pause_record": "generated:pause_checkpoint",
		})
		if err == nil {
			t.Fatal("unscoped human_decision evidence must not authorize TR-019")
		}
		if !strings.Contains(err.Error(), "scope_refs") || !strings.Contains(err.Error(), "semantic scope") {
			t.Fatalf("scope rejection must explain semantic evidence registration: %v", err)
		}
	})

	t.Run("exact scope passes the gate", func(t *testing.T) {
		root, state := build(t)
		scopeFixtureEvidence(t, state, ref, "runtime_resume:loop-test")
		writeFullState(t, root, state)
		if err := applyT(t, root, "TR-019", 5, "user", map[string]string{
			"human_decision_record": ref, "pause_record": "generated:pause_checkpoint",
		}); err != nil {
			t.Fatalf("correctly scoped approval must pass: %v", err)
		}
	})
}

func TestModernHumanDecisionBindsTargetCursorAndCannotReplay(t *testing.T) {
	build := func(t *testing.T, targetState string) string {
		t.Helper()
		root := t.TempDir()
		setupRepoWithDefinition(t, root)
		state := stateAtVerificationMap(5)
		state["lifecycle"] = map[string]any{"state": "awaiting_human_release", "phase": nil, "phase_revision": float64(1)}
		path := "docs/reports/human/decision.json"
		artifact, err := json.Marshal(map[string]any{
			"decision": "approve", "decision_id": "decision-modern", "runtime_id": "loop-test",
			"disposition": "approve", "target_cursor": map[string]any{"state": targetState, "phase": nil},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), artifact, 0o644); err != nil {
			t.Fatal(err)
		}
		state["evidence"] = []any{map[string]any{
			"id": "decision-modern", "kind": "human_decision", "path": path,
			"sha256": transition.SHA256(artifact), "status": "valid", "baseline_generation": 1,
			"review_round": 1, "produced_by": []any{"user"}, "scope_refs": []any{"runtime_release:loop-test"},
			"invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil,
			"responsibility_id": nil,
		}}
		writeFullState(t, root, state)
		return root
	}

	t.Run("target cursor is enforced", func(t *testing.T) {
		root := build(t, "paused")
		err := applyT(t, root, "TR-025", 5, "user", map[string]string{"human_decision_record": "decision-modern"})
		if err == nil || !strings.Contains(err.Error(), "targets state") {
			t.Fatalf("wrong target cursor error = %v", err)
		}
	})

	t.Run("successful decision cannot be replayed", func(t *testing.T) {
		root := build(t, "awaiting_human_release")
		if err := applyT(t, root, "TR-025", 5, "user", map[string]string{"human_decision_record": "decision-modern"}); err != nil {
			t.Fatalf("first decision failed: %v", err)
		}
		state := readState(t, root)
		item := state["evidence"].([]any)[0].(map[string]any)
		if item["consumed_by"] != "TR-025" {
			t.Fatalf("decision consumption metadata = %#v", item)
		}
		state["lifecycle"] = map[string]any{"state": "awaiting_human_release", "phase": nil, "phase_revision": float64(2)}
		writeFullState(t, root, state)
		err := applyT(t, root, "TR-025", 6, "user", map[string]string{"human_decision_record": "decision-modern"})
		if err == nil || !strings.Contains(err.Error(), "already consumed") {
			t.Fatalf("replay error = %v, want already-consumed rejection", err)
		}
	})
}
