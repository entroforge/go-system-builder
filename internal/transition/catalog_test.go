// Catalog-specific tests for TASK-013: fail-closed startup, the 3 task-entity
// guards, the start_new_review_round action, the single-call-per-pause-event
// invariant, and the TR-019 sentinel migration evidence.
package transition_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/transition"
)

// TestCatalogFailsClosedOnMissingGuard verifies LoadCatalog returns an error
// when a declared guard name is not registered in guardRegistry. We simulate
// the failure by passing a stub Loop Definition with a sentinel guard name
// that we know is unregistered.
func TestCatalogFailsClosedOnMissingGuard(t *testing.T) {
	root := t.TempDir()
	defDir := filepath.Join(root, "docs")
	os.MkdirAll(defDir, 0o755)
	def := map[string]any{
		"schema_version":    "1.0.0",
		"states":            map[string]any{},
		"phase_machines":    map[string]any{},
		"entity_lifecycles": map[string]any{},
		"transitions": []any{
			map[string]any{
				"id":                "TR-XXX",
				"from":              "inactive",
				"event":             "x",
				"to":                "planning",
				"actors":            []string{"user"},
				"guards":            []string{"this_guard_is_definitely_not_registered"},
				"actions":           []string{},
				"required_evidence": []string{},
				"description":       "test",
			},
		},
		"global_transitions": []any{},
		"forbidden_events":   []any{},
		"invariants":         []any{},
	}
	data, _ := json.MarshalIndent(def, "", "  ")
	os.WriteFile(filepath.Join(defDir, "loop-definition.json"), data, 0o644)
	_, err := transition.LoadCatalog(root)
	if err == nil {
		t.Fatal("LoadCatalog must fail closed on missing guard")
	}
	if !strings.Contains(err.Error(), "unregistered guard") {
		t.Fatalf("expected unregistered guard error, got: %v", err)
	}
}

// TestCatalogFailsClosedOnMissingAction mirrors the guard test for actions.
func TestCatalogFailsClosedOnMissingAction(t *testing.T) {
	root := t.TempDir()
	defDir := filepath.Join(root, "docs")
	os.MkdirAll(defDir, 0o755)
	def := map[string]any{
		"schema_version":    "1.0.0",
		"states":            map[string]any{},
		"phase_machines":    map[string]any{},
		"entity_lifecycles": map[string]any{},
		"transitions": []any{
			map[string]any{
				"id":                "TR-YYY",
				"from":              "inactive",
				"event":             "x",
				"to":                "planning",
				"actors":            []string{"user"},
				"guards":            []string{},
				"actions":           []string{"this_action_is_definitely_not_registered"},
				"required_evidence": []string{},
				"description":       "test",
			},
		},
		"global_transitions": []any{},
		"forbidden_events":   []any{},
		"invariants":         []any{},
	}
	data, _ := json.MarshalIndent(def, "", "  ")
	os.WriteFile(filepath.Join(defDir, "loop-definition.json"), data, 0o644)
	_, err := transition.LoadCatalog(root)
	if err == nil {
		t.Fatal("LoadCatalog must fail closed on missing action")
	}
	if !strings.Contains(err.Error(), "unregistered action") {
		t.Fatalf("expected unregistered action error, got: %v", err)
	}
}

func TestCatalogFailsClosedOnUnknownRequiredEvidence(t *testing.T) {
	root := t.TempDir()
	defDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(defDir, 0o755); err != nil {
		t.Fatal(err)
	}
	def := map[string]any{
		"schema_version":    "1.0.0",
		"states":            map[string]any{},
		"phase_machines":    map[string]any{},
		"entity_lifecycles": map[string]any{},
		"transitions": []any{
			map[string]any{
				"id":                "TR-EVIDENCE-UNKNOWN",
				"from":              "inactive",
				"event":             "evidence_unknown",
				"to":                "planning",
				"actors":            []string{"orchestrator"},
				"guards":            []string{},
				"actions":           []string{},
				"required_evidence": []string{"unknown_evidence_slot"},
				"description":       "test",
			},
		},
		"global_transitions": []any{},
		"forbidden_events":   []any{},
		"invariants":         []any{},
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defDir, "loop-definition.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = transition.LoadCatalog(root)
	if err == nil || !strings.Contains(err.Error(), "required evidence") {
		t.Fatalf("expected unknown required evidence to fail closed, got %v", err)
	}
}

// TestTaskEntityGuardsPresentInGuardRegistry is the explicit assertion for
// the 3 task-entity guards resolved by DV round 1 F-CORE-001/002/003.
func TestTaskEntityGuardsPresentInGuardRegistry(t *testing.T) {
	for _, name := range []string{
		"required_verification_evidence_present",
		"builder_activation_recorded",
		"builder_report_complete",
	} {
		if _, ok := transition.LookupGuard(name); !ok {
			t.Fatalf("guard %s must be present in guardRegistry", name)
		}
	}
}

// TestStartNewReviewRoundPresentInActionRegistry is the explicit assertion
// for PTR-VERIFY-05 resolved by DV round 1 F-CORE-004.
func TestStartNewReviewRoundPresentInActionRegistry(t *testing.T) {
	if _, ok := transition.LookupAction("start_new_review_round"); !ok {
		t.Fatal("action start_new_review_round must be present in actionRegistry")
	}
}

// TestCapturePauseCheckpointSingleCallPerPauseEvent verifies the action
// rejects a second capture when state["pause"] already holds a non-nil
// checkpoint. This is the runtime invariant that makes
// single_call_per_pause_event provable.
func TestCapturePauseCheckpointSingleCallPerPauseEvent(t *testing.T) {
	state := map[string]any{
		"lifecycle": map[string]any{
			"state":          "verification",
			"phase":          "delivery",
			"phase_revision": float64(1),
		},
		"baseline": map[string]any{"generation": float64(1), "captured_at": "2026-01-01T00:00:00Z"},
		"review":   map[string]any{"round": float64(1), "clean_round": nil},
		"pause":    nil,
	}
	ctx := transition.ActionContext{
		Spec:       transition.TransitionSpec{ID: "TR-011", Description: "verification release blocked"},
		OccurredAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if _, err := transition.CallActionCapturePauseCheckpointForTest(state, ctx); err != nil {
		t.Fatalf("first capture should succeed, got: %v", err)
	}
	if state["pause"] == nil {
		t.Fatal("first capture should set state.pause")
	}
	// Second capture must be rejected.
	if _, err := transition.CallActionCapturePauseCheckpointForTest(state, ctx); err == nil {
		t.Fatal("second capture must be rejected to preserve single_call_per_pause_event")
	}
}

// TestTR019SentinelInLoopDefinition asserts the post-migration loop-
// definition.json contains the RESUME_FROM_PAUSE sentinel and no
// $resume_state literal.
func TestTR019SentinelInLoopDefinition(t *testing.T) {
	defPath := filepath.Join("..", "..", "docs", "loop-definition.json")
	data, err := os.ReadFile(defPath)
	if err != nil {
		t.Fatalf("read loop-definition.json: %v", err)
	}
	if strings.Contains(string(data), "$resume_state") {
		t.Fatal("loop-definition.json must not contain the $resume_state literal")
	}
	if !strings.Contains(string(data), "RESUME_FROM_PAUSE") {
		t.Fatal("loop-definition.json must contain RESUME_FROM_PAUSE")
	}
	var def struct {
		Transitions []struct {
			ID string `json:"id"`
			To string `json:"to"`
		} `json:"transitions"`
	}
	if err := json.Unmarshal(data, &def); err != nil {
		t.Fatalf("decode loop-definition.json: %v", err)
	}
	var tr019To string
	for _, tr := range def.Transitions {
		if tr.ID == "TR-019" {
			tr019To = tr.To
			break
		}
	}
	if tr019To == "" {
		t.Fatal("TR-019 transition missing from loop-definition.json")
	}
	if tr019To != "RESUME_FROM_PAUSE" {
		t.Fatalf("TR-019.to = %q; want RESUME_FROM_PAUSE", tr019To)
	}
}
