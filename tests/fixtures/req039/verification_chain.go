package req039fixtures

import (
	"os"
	"path/filepath"
	"testing"
)

// Post L3-S7 the verification fixtures live in review_round.go (ReviewPlan,
// Canonical ReviewResults, sealed ObservationBatch). This file keeps the two
// helpers that predate the plan model but are still consumed: the review
// workgroup registration used by stage fixtures, and the REQ doc writer.

// EnsureVerificationWorkgroups registers the three review workgroup kinds
// (delivery_verifier / qa / e2e_browser) in entities.teams. Stage-level
// fixtures (SeedVerificationDelivery and friends) use it to make the
// verification state look dispatched.
func EnsureVerificationWorkgroups(state map[string]any) {
	entities, _ := state["entities"].(map[string]any)
	if entities == nil {
		entities = map[string]any{}
		state["entities"] = entities
	}
	teams, _ := entities["teams"].([]any)
	present := map[string]bool{}
	for _, raw := range teams {
		team, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := team["kind"].(string); kind != "" {
			present[kind] = true
		}
	}
	round := reviewRoundFromState(state)
	for _, spec := range []struct {
		dimension, workgroupKind, responsibility, agent string
	}{
		{"delivery", "delivery_verifier", "Delivery Verifier", "delivery-1"},
		{"qa", "qa", "QA", "qa-1"},
		{"e2e_browser", "e2e_browser", "E2E Browser", "e2e-1"},
	} {
		if present[spec.workgroupKind] {
			continue
		}
		teams = append(teams, map[string]any{
			"id":                 "team-" + spec.dimension,
			"platform_team_id":   "platform-" + spec.dimension,
			"kind":               spec.workgroupKind,
			"status":             "complete",
			"manifest_ref":       filepath.Join("evidence", "ev-manifest-"+spec.dimension+".json"),
			"responsibility_ids": []any{spec.responsibility},
			"agent_ids":          []any{spec.agent},
			"review_round":       round,
		})
	}
	entities["teams"] = teams
}

// EnsureREQDoc writes the bound REQ file when fixtures need ui_impact metadata.
func EnsureREQDoc(t *testing.T, root string, state map[string]any, uiImpact string) {
	t.Helper()
	reqPath := "docs/requirements/REQ-039-loop-control-plane.md"
	if br, ok := state["bound_req"].(map[string]any); ok {
		if p, _ := br["path"].(string); p != "" {
			reqPath = p
		}
	}
	content := []byte("# REQ-039\n> UI impact: " + uiImpact + "\n")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, reqPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, reqPath), content, 0o644); err != nil {
		t.Fatal(err)
	}
}
