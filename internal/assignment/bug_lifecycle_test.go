package assignment_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
)

func setupBugRuntime(t *testing.T, root string, bugState string) {
	t.Helper()
	// Copy loop-definition.json.
	defSrc := filepath.Join("..", "..", "docs", "loop-definition.json")
	defData, err := os.ReadFile(defSrc)
	if err != nil {
		t.Fatal(err)
	}
	defDir := filepath.Join(root, "docs")
	os.MkdirAll(defDir, 0o755)
	os.WriteFile(filepath.Join(defDir, "loop-definition.json"), defData, 0o644)

	bug := map[string]any{
		"id":                          "BUG-001",
		"state":                       bugState,
		"path":                        "docs/reports/bugs/BUG-001.md",
		"severity":                    "blocking",
		"attempt_count":               float64(0),
		"same_contract_failure_count": float64(0),
		"original_finder_agent_ids":   []any{"agent-finder"},
	}
	state := map[string]any{
		"schema_version": "1.0.0",
		"runtime_id":     "loop-test",
		"definition":     map[string]any{"path": "x", "version": "1.0.0", "sha256": "x"},
		"revision":       3,
		"lifecycle":      map[string]any{"state": "bug_resolution", "phase": "investigation", "phase_revision": float64(1)},
		"authorization":  map[string]any{"mode": "loop", "command": "/loop", "actor": "x", "occurred_at": "2026-01-01T00:00:00Z"},
		"bound_req":      map[string]any{"id": "REQ-X", "path": "x", "version": "1.0.0", "sha256": "x", "status": "locked"},
		"baseline":       map[string]any{"generation": float64(1), "captured_at": "2026-01-01T00:00:00Z"},
		"review":         map[string]any{"round": float64(0), "clean_round": nil},
		"documents":      []any{},
		"entities": map[string]any{
			"agents": []any{},
			"tasks":  []any{},
			"bugs":   []any{bug},
			"teams":  []any{},
		},
		"evidence":        []any{},
		"blockers":        []any{},
		"pause":           nil,
		"journal":         map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": float64(0), "last_event_id": nil},
		"last_transition": nil,
		"updated_at":      "2026-01-01T00:00:00Z",
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.MkdirAll(filepath.Join(root, ".claude"), 0o755)
	os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(data, '\n'), 0o644)
	os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), []byte{}, 0o644)
}

func TestBugInvestigationStarted(t *testing.T) {
	root := t.TempDir()
	setupBugRuntime(t, root, "draft")
	_, err := assignment.AdvanceBug(root,
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
		assignment.BugEventRequest{
			ExpectedRevision: 3,
			BugID:            "BUG-001",
			Event:            "investigation_started",
		})
	if err != nil {
		t.Fatalf("investigation_started failed: %v", err)
	}
	assertBugState(t, root, "BUG-001", "investigating")
}

func TestBugReportSubmittedRequiresGuards(t *testing.T) {
	root := t.TempDir()
	setupBugRuntime(t, root, "investigating")
	_, err := assignment.AdvanceBug(root,
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
		assignment.BugEventRequest{
			ExpectedRevision: 3,
			BugID:            "BUG-001",
			Event:            "bug_report_submitted",
		})
	if err == nil {
		t.Fatal("expected bug_report_submitted to fail without root_cause_evidence param")
	}
}

func TestBugAcceptedSucceedsWithGuards(t *testing.T) {
	root := t.TempDir()
	setupBugRuntime(t, root, "pending_approval")
	_, err := assignment.AdvanceBug(root,
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
		assignment.BugEventRequest{
			ExpectedRevision: 3,
			BugID:            "BUG-001",
			Event:            "bug_accepted",
			Params: map[string]any{
				"root_cause_evidence": "docs/reports/bugs/BUG-001.md#root-cause",
				"closing_contract":    "docs/reports/bugs/BUG-001.md#closing-contract",
			},
		})
	if err != nil {
		t.Fatalf("bug_accepted failed: %v", err)
	}
	assertBugState(t, root, "BUG-001", "accepted")
}

func TestBugClosingContractFailedLoopsToInvestigating(t *testing.T) {
	root := t.TempDir()
	setupBugRuntime(t, root, "retesting")
	_, err := assignment.AdvanceBug(root,
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
		assignment.BugEventRequest{
			ExpectedRevision: 3,
			BugID:            "BUG-001",
			Event:            "closing_contract_failed",
			Params: map[string]any{
				"failure_evidence": "docs/reports/bugs/BUG-001.md#failure",
			},
		})
	if err != nil {
		t.Fatalf("closing_contract_failed failed: %v", err)
	}
	state := readBugRuntimeState(t, root)
	bugs := state["entities"].(map[string]any)["bugs"].([]any)
	bug := bugs[0].(map[string]any)
	if bug["state"] != "investigating" {
		t.Errorf("expected investigating, got %v", bug["state"])
	}
	if bug["attempt_count"] != float64(1) {
		t.Errorf("expected attempt_count=1 after retry, got %v", bug["attempt_count"])
	}
}

func TestBugRetryLimitExceededRejects(t *testing.T) {
	root := t.TempDir()
	setupBugRuntime(t, root, "retesting")
	// Set configuration with a low limit and bump the BUG's attempt count.
	state := readBugRuntimeState(t, root)
	state["configuration"] = map[string]any{
		"repair": map[string]any{
			"max_attempts_per_bug": float64(2),
		},
	}
	bugs := state["entities"].(map[string]any)["bugs"].([]any)
	bug := bugs[0].(map[string]any)
	bug["attempt_count"] = float64(2) // already at limit
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(data, '\n'), 0o644)

	_, err := assignment.AdvanceBug(root,
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
		assignment.BugEventRequest{
			ExpectedRevision: 3,
			BugID:            "BUG-001",
			Event:            "closing_contract_failed",
			Params: map[string]any{
				"failure_evidence": "docs/reports/bugs/BUG-001.md#failure",
			},
		})
	if err == nil {
		t.Fatal("expected closing_contract_failed to fail when max_attempts_per_bug exceeded")
	}
}

func TestBugUnknownEventRejected(t *testing.T) {
	root := t.TempDir()
	setupBugRuntime(t, root, "draft")
	_, err := assignment.AdvanceBug(root,
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
		assignment.BugEventRequest{
			ExpectedRevision: 3,
			BugID:            "BUG-001",
			Event:            "nonexistent_event",
		})
	if err == nil {
		t.Fatal("expected error for unknown event")
	}
}

func TestBugWrongStateRejected(t *testing.T) {
	root := t.TempDir()
	setupBugRuntime(t, root, "closed")
	_, err := assignment.AdvanceBug(root,
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
		assignment.BugEventRequest{
			ExpectedRevision: 3,
			BugID:            "BUG-001",
			Event:            "investigation_started",
		})
	if err == nil {
		t.Fatal("expected error: BUG in closed state cannot start investigation")
	}
}

// TestOriginalFinderCannotClose is the structural identity-check test
// required by BUG-003 §4b.2(g) and TASK-015 §6. It exercises three
// scenarios:
//
//  1. Self-close attempt: Builder A registered the BUG (so A is in
//     original_finder_agent_ids) and the same Builder A issues
//     closing_contract_passed. The runtime must reject with the typed
//     BUG_CLOSE_BY_FINDER_FORBIDDEN error.
//  2. Other-Builder attempt: a different Builder B (not in
//     original_finder_agent_ids) issues closing_contract_passed. The
//     runtime must allow the transition (it succeeds; the assertion is
//     that no error is returned).
//  3. Same-actor id collision across repair attempts: the BUG has
//     multiple original finders (e.g. DV + QA) and the actor is one of
//     them. The runtime must reject.
func TestOriginalFinderCannotClose(t *testing.T) {
	t.Run("self_close_rejected", func(t *testing.T) {
		root := t.TempDir()
		setupBugRuntime(t, root, "retesting")
		_, err := assignment.AdvanceBug(root,
			filepath.Join(root, ".claude", "loop-state.json"),
			filepath.Join(root, ".claude", "loop-events.jsonl"),
			assignment.BugEventRequest{
				ExpectedRevision: 3,
				BugID:            "BUG-001",
				Event:            "closing_contract_passed",
				Params: map[string]any{
					"actor_agent_id":          "agent-finder", // IS in original_finder_agent_ids
					"reverification_evidence": "docs/reports/bugs/BUG-001.md#reverify",
				},
			})
		if err == nil {
			t.Fatal("expected closing_contract_passed to be rejected for Builder self-close")
		}
		if !strings.Contains(err.Error(), "BUG_CLOSE_BY_FINDER_FORBIDDEN") {
			t.Fatalf("expected typed BUG_CLOSE_BY_FINDER_FORBIDDEN error, got: %v", err)
		}
	})

	t.Run("other_builder_allowed", func(t *testing.T) {
		root := t.TempDir()
		setupBugRuntime(t, root, "retesting")
		_, err := assignment.AdvanceBug(root,
			filepath.Join(root, ".claude", "loop-state.json"),
			filepath.Join(root, ".claude", "loop-events.jsonl"),
			assignment.BugEventRequest{
				ExpectedRevision: 3,
				BugID:            "BUG-001",
				Event:            "closing_contract_passed",
				Params: map[string]any{
					"actor_agent_id":          "agent-other-builder", // NOT in finders
					"reverification_evidence": "docs/reports/bugs/BUG-001.md#reverify",
				},
			})
		if err != nil {
			t.Fatalf("expected other-Builder close to succeed, got: %v", err)
		}
		assertBugState(t, root, "BUG-001", "closed")
	})

	t.Run("same_actor_id_collision_rejected", func(t *testing.T) {
		root := t.TempDir()
		setupBugRuntime(t, root, "retesting")
		// Augment finders with a second id.
		state := readBugRuntimeState(t, root)
		bugs := state["entities"].(map[string]any)["bugs"].([]any)
		bug := bugs[0].(map[string]any)
		bug["original_finder_agent_ids"] = []any{"agent-finder", "agent-qa-finder"}
		data, _ := json.MarshalIndent(state, "", "  ")
		os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(data, '\n'), 0o644)
		// Actor is the QA finder — must still be rejected.
		_, err := assignment.AdvanceBug(root,
			filepath.Join(root, ".claude", "loop-state.json"),
			filepath.Join(root, ".claude", "loop-events.jsonl"),
			assignment.BugEventRequest{
				ExpectedRevision: 3,
				BugID:            "BUG-001",
				Event:            "closing_contract_passed",
				Params: map[string]any{
					"actor_agent_id":          "agent-qa-finder",
					"reverification_evidence": "docs/reports/bugs/BUG-001.md#reverify",
				},
			})
		if err == nil {
			t.Fatal("expected closing_contract_passed to be rejected for QA-finder self-close")
		}
		if !strings.Contains(err.Error(), "BUG_CLOSE_BY_FINDER_FORBIDDEN") {
			t.Fatalf("expected typed BUG_CLOSE_BY_FINDER_FORBIDDEN error, got: %v", err)
		}
	})
}

// TestOriginalFinderAssignedHelper is a direct unit test for the exported
// identity-check function. It covers presence vs identity semantics: the
// array must be NON-empty (presence precondition) AND the actor must be IN
// the array (identity check).
func TestOriginalFinderAssignedHelper(t *testing.T) {
	cases := []struct {
		name   string
		bug    map[string]any
		actor  string
		expect bool
	}{
		{"nil_bug", nil, "agent-a", false},
		{"missing_field", map[string]any{}, "agent-a", false},
		{"empty_find_array", map[string]any{"original_finder_agent_ids": []any{}}, "agent-a", false},
		{"actor_in_array", map[string]any{"original_finder_agent_ids": []any{"agent-a"}}, "agent-a", true},
		{"actor_not_in_array", map[string]any{"original_finder_agent_ids": []any{"agent-b"}}, "agent-a", false},
		{"actor_among_many", map[string]any{"original_finder_agent_ids": []any{"agent-b", "agent-a", "agent-c"}}, "agent-a", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := assignment.OriginalFinderAssigned(c.bug, c.actor)
			if got != c.expect {
				t.Errorf("OriginalFinderAssigned(%v, %q) = %v; want %v", c.bug, c.actor, got, c.expect)
			}
		})
	}
}

func assertBugState(t *testing.T, root, bugID, expected string) {
	t.Helper()
	state := readBugRuntimeState(t, root)
	bugs := state["entities"].(map[string]any)["bugs"].([]any)
	for _, raw := range bugs {
		bug := raw.(map[string]any)
		if bug["id"] == bugID {
			if bug["state"] != expected {
				t.Errorf("BUG %s state = %s, want %s", bugID, bug["state"], expected)
			}
			return
		}
	}
	t.Errorf("BUG %s not found", bugID)
}

func readBugRuntimeState(t *testing.T, root string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	json.Unmarshal(data, &state)
	return state
}
