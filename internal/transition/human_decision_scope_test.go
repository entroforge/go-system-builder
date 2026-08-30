package transition_test

import (
	"testing"
)

// TestHumanDecisionScopeBinding pins the transition-layer human gate: a
// human_decision approval authorizes exactly one verb on one runtime at one
// revision (the transition-layer counterpart of validateLifecycleApproval).
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
	})

	t.Run("exact scope passes the gate", func(t *testing.T) {
		root, state := build(t)
		scopeFixtureEvidence(t, state, ref, "runtime_resume:loop-test@5")
		writeFullState(t, root, state)
		if err := applyT(t, root, "TR-019", 5, "user", map[string]string{
			"human_decision_record": ref, "pause_record": "generated:pause_checkpoint",
		}); err != nil {
			t.Fatalf("correctly scoped approval must pass: %v", err)
		}
	})
}
