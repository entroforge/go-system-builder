package transition_test

import "testing"

// TestResumeUsesHumanDecision verifies that TR-019 resumes from the actual human decision.
func TestResumeUsesHumanDecision(t *testing.T) {
	root := t.TempDir()
	setupRepoWithDefinition(t, root)

	state := stateAtVerificationMap(5)
	state["lifecycle"] = map[string]any{"state": "paused", "phase": nil, "phase_revision": float64(2)}
	state["pause"] = map[string]any{
		"from_state":            "verification",
		"from_phase":            "running",
		"phase_revision":        float64(1),
		"baseline_generation":   float64(1),
		"reason":                "test",
		"required_human_action": "test",
		"paused_at":             "2026-01-01T00:00:00Z",
	}
	registerFixtureEvidence(t, root, state, map[string]string{
		"human_decision_record": "docs/reports/human/decision.md",
	})
	scopeFixtureEvidence(t, state, "docs/reports/human/decision.md", "runtime_resume:loop-test@5")
	writeFullState(t, root, state)

	err := applyT(t, root, "TR-019", 5, "user", map[string]string{
		"human_decision_record": "docs/reports/human/decision.md",
		"pause_record":          "generated:pause_checkpoint",
	})
	if err != nil {
		t.Fatalf("TR-019 should accept the real human decision: %v", err)
	}
}
