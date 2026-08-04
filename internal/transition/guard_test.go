package transition_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

// TestGuardNoOpenBlockingBugsBlocksTR009 verifies that TR-009 (clean_round_passed)
// is rejected when a blocking BUG is still open.
func TestGuardNoOpenBlockingBugsBlocksTR009(t *testing.T) {
	root := t.TempDir()
	setupRepoWithDefinition(t, root)
	state := stateAtVerificationMap(5)
	// Add an open blocking BUG.
	entities := state["entities"].(map[string]any)
	entities["bugs"] = []any{
		map[string]any{"id": "BUG-001", "state": "accepted", "severity": "P0"},
	}
	writeFullState(t, root, state)

	err := applyT(t, root, "TR-009", 5, "orchestrator", map[string]string{
		"clean_round_evidence": "docs/reports/review/clean-round.md",
	})
	if err == nil {
		t.Fatal("TR-009 should be rejected when a blocking BUG is open")
	}
}

// TestGuardNoInvalidatedEvidenceBlocksTR009 verifies that TR-009 is rejected
// when current-round evidence is marked invalid.
func TestGuardNoInvalidatedEvidenceBlocksTR009(t *testing.T) {
	root := t.TempDir()
	setupRepoWithDefinition(t, root)
	state := stateAtVerificationMap(5)
	// Add invalidated evidence in the current round.
	evidence := state["evidence"].([]any)
	evidence = append(evidence, map[string]any{
		"id": "ev-bad", "status": "invalid", "review_round": float64(1),
		"baseline_generation": float64(1), "scope_refs": []any{},
		"responsibility_id": "VER-1", "kind": "delivery_review", "path": "x",
		"sha256": "x", "produced_by": []any{"a"},
		"invalidated_by": "TR-007", "invalidation_rule": "test", "invalidation_reason": "test",
	})
	state["evidence"] = evidence
	writeFullState(t, root, state)

	err := applyT(t, root, "TR-009", 5, "orchestrator", map[string]string{
		"clean_round_evidence": "docs/reports/review/clean-round.md",
	})
	if err == nil {
		t.Fatal("TR-009 should be rejected when current-round evidence is invalid")
	}
}

// TestGuardPassesWhenNoBlockingBugs verifies TR-009 passes when guards are clean.
func TestGuardPassesWhenNoBlockingBugs(t *testing.T) {
	root := t.TempDir()
	setupRepoWithDefinition(t, root)
	state := stateAtVerificationMap(5)
	// Ensure no bugs and no invalid evidence.
	writeFullState(t, root, state)

	// TR-009 requires clean_round_evidence. With no blocking bugs and no invalid
	// evidence in round 1, the guard should pass (the transition itself may still
	// need other evidence fields, but the guard should not be the blocker).
	err := applyT(t, root, "TR-009", 5, "orchestrator", map[string]string{
		"clean_round_evidence": "docs/reports/review/clean-round.md",
	})
	// We only assert that the error (if any) is NOT about guards.
	if err != nil {
		if containsStr(err.Error(), "blocking BUG") || containsStr(err.Error(), "invalid") {
			t.Fatalf("guard should not block when clean: %v", err)
		}
	}
}

// TestGuardUIIImpactResolvedBlocksUnknown verifies the SM-003 guard
// (LOOP-STATE-MACHINE.md §15): once `req bind` registers a REQ with
// `ui_impact = unknown`, the planning phase cannot advance until PM
// clarifies the value in §11. The guard is state-derived (reads
// bound_req.metadata.ui_impact) and is wired into the registry as
// `ui_impact_resolved`.
func TestGuardUIIImpactResolvedBlocksUnknown(t *testing.T) {
	fn, ok := transition.LookupGuard("ui_impact_resolved")
	if !ok {
		t.Fatal("ui_impact_resolved guard must be registered")
	}
	cases := []struct {
		name      string
		state     map[string]any
		wantError bool
	}{
		{
			name: "ui_impact=unknown rejects planning",
			state: map[string]any{
				"bound_req": map[string]any{
					"id":       "REQ-SM003",
					"metadata": map[string]any{"ui_impact": "unknown"},
				},
			},
			wantError: true,
		},
		{
			name: "ui_impact=none passes planning",
			state: map[string]any{
				"bound_req": map[string]any{
					"id":       "REQ-SM003-NONE",
					"metadata": map[string]any{"ui_impact": "none"},
				},
			},
			wantError: false,
		},
		{
			name: "ui_impact=changed passes planning",
			state: map[string]any{
				"bound_req": map[string]any{
					"id":       "REQ-SM003-CHANGED",
					"metadata": map[string]any{"ui_impact": "changed"},
				},
			},
			wantError: false,
		},
		{
			name:      "no bound_req passes (pre-bind)",
			state:     map[string]any{},
			wantError: false,
		},
		{
			name: "bound_req without metadata passes",
			state: map[string]any{
				"bound_req": map[string]any{"id": "REQ-LEGACY"},
			},
			wantError: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := fn(tc.state, nil)
			if tc.wantError && err == nil {
				t.Fatal("guard should reject unknown ui_impact")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("guard should accept %q: %v", tc.name, err)
			}
			if tc.wantError && err != nil && !containsStr(err.Error(), "ui_impact=unknown") {
				t.Fatalf("error should mention ui_impact=unknown, got: %v", err)
			}
		})
	}
}

// TestBindREQAcceptsUnknownUIIImpact locks the spec-aligned ENUM contract
// that the SM-003 design depends on: `unknown` is a valid planning-phase
// value (LOOP-STATE-MACHINE.md §15 + ui-prototype.md §3), and bindREQ must
// accept it. The guard `ui_impact_resolved` (registered separately) is what
// blocks planning from advancing until the value is clarified.
func TestBindREQAcceptsUnknownUIIImpact(t *testing.T) {
	root := filepath.Join("..", "..")
	statePath, journalPath := copyInactiveRuntime(t, root)
	// The REQ path must be under root so filepath.Rel works; place it under
	// internal/transition/testdata so the path lives in the test sandbox but
	// is still reachable from the repo root.
	reqPath := filepath.Join(root, "internal", "transition", "testdata", "ui-unknown-req.md")
	content := "# 需求：REQ-099\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：unknown\n"
	if err := os.WriteFile(reqPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(reqPath) })
	relPath := "internal/transition/testdata/ui-unknown-req.md"
	hash := fileHash(t, reqPath)
	next, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID:     "TR-001",
		ExpectedRevision: 0,
		Actor:            "user",
		Evidence: map[string]string{
			"req_lock_record":           "REQ-099#lock",
			"loop_authorization_record": "user:/loop REQ-099",
		},
		REQ: &transition.LockedREQ{
			ID:         "REQ-099",
			Path:       relPath,
			Version:    "v1.0.0",
			SHA256:     hash,
			ApprovedBy: "user",
			ApprovedAt: "2026-07-05T00:00:00Z",
		},
	})
	if err != nil {
		t.Fatalf("bindREQ must accept ui_impact=unknown: %v", err)
	}
	bound, _ := next.State["bound_req"].(map[string]any)
	metadata, _ := bound["metadata"].(map[string]any)
	if got := metadata["ui_impact"]; got != "unknown" {
		t.Fatalf("ui_impact round-trip: got %v want unknown", got)
	}

	// Now the SM-003 guard must reject any further planning transition.
	fn, ok := transition.LookupGuard("ui_impact_resolved")
	if !ok {
		t.Fatal("ui_impact_resolved guard must be registered")
	}
	if err := fn(next.State, nil); err == nil {
		t.Fatal("guard must reject ui_impact=unknown")
	}
}

// stateAtVerificationMap returns a minimal verification-phase state for guard tests.
func stateAtVerificationMap(rev int) map[string]any {
	return map[string]any{
		"schema_version": "1.1.0",
		"runtime_id":     "loop-test",
		"definition":     map[string]any{"path": "docs/loop-definition.json", "version": "1.2.0", "sha256": "31c2f880dea1aeff73354c6e4a1dc45c234739a861ddcded79efe59cfbb69c86"},
		"revision":       float64(rev),
		"lifecycle":      map[string]any{"state": "verification", "phase": "clean_round_evaluation", "phase_revision": float64(1)},
		"authorization":  map[string]any{"mode": "loop", "command": "/loop", "actor": "x", "occurred_at": "2026-01-01T00:00:00Z"},
		"bound_req":      map[string]any{"id": "REQ-099", "path": "docs/requirements/REQ-099.md", "version": "1.0.0", "sha256": "31c2f880dea1aeff73354c6e4a1dc45c234739a861ddcded79efe59cfbb69c86", "status": "locked", "approved_by": "x", "approved_at": "2026-01-01T00:00:00Z"},
		"baseline":       map[string]any{"generation": float64(1), "captured_at": "2026-01-01T00:00:00Z"},
		"configuration": map[string]any{
			"repair": map[string]any{"max_attempts_per_bug": float64(3), "max_same_contract_failures": float64(2), "max_full_review_rounds": float64(5)},
		},
		"hook_control": map[string]any{
			"policy_ref":           map[string]any{"path": "docs/hook-policy.json", "version": "v1.0.0", "sha256": "31c2f880dea1aeff73354c6e4a1dc45c234739a861ddcded79efe59cfbb69c86"},
			"mode":                 "audit",
			"health":               "healthy",
			"consecutive_failures": float64(0),
			"last_checked_at":      nil,
		},
		"review":          map[string]any{"round": float64(1), "clean_round": nil},
		"documents":       []any{},
		"entities":        map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}},
		"evidence":        []any{},
		"blockers":        []any{},
		"pause":           nil,
		"journal":         map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": float64(0), "last_event_id": nil},
		"last_transition": nil,
		"updated_at":      "2026-01-01T00:00:00Z",
	}
}

// Suppress unused import if json/os/filepath are not directly used in this file
// (they're used by helpers in scenarios_test.go which is in the same package).
var _ = json.Marshal
var _ = os.ReadFile
var _ = filepath.Join
var _ = transition.Request{}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
