package transition_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestTASK03901PlanningPhaseMachineIsFormal(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}

	machine, ok := catalog.Definition.PhaseMachines["planning"]
	if !ok {
		t.Fatal("planning phase machine must exist")
	}
	for _, phase := range []string{"design", "contracts", "tasks"} {
		if _, ok := machine.Phases[phase]; !ok {
			t.Fatalf("planning phase %q must be declared", phase)
		}
	}

	want := map[string]struct {
		from string
		to   string
	}{
		"PTR-PLAN-01": {from: "design", to: "contracts"},
		"PTR-PLAN-02": {from: "contracts", to: "tasks"},
	}
	for id, expected := range want {
		found := false
		for _, spec := range machine.Transitions {
			if spec.ID != id {
				continue
			}
			found = true
			if spec.From != expected.from || spec.To != expected.to {
				t.Fatalf("%s = %s -> %s, want %s -> %s", id, spec.From, spec.To, expected.from, expected.to)
			}
		}
		if !found {
			t.Fatalf("planning transition %s must be declared", id)
		}
	}
}

func TestTASK03901PlanningExitIsBoundToTasksPhase(t *testing.T) {
	data, err := os.ReadFile("../../docs/loop-definition.json")
	if err != nil {
		t.Fatal(err)
	}
	var definition struct {
		Transitions []map[string]any `json:"transitions"`
	}
	if err := json.Unmarshal(data, &definition); err != nil {
		t.Fatal(err)
	}
	for _, transition := range definition.Transitions {
		if transition["id"] != "TR-002" {
			continue
		}
		if transition["from_phase"] != "tasks" {
			t.Fatalf("TR-002.from_phase = %v, want tasks", transition["from_phase"])
		}
		return
	}
	t.Fatal("TR-002 must be declared")
}

func TestTASK03901TR002RejectsBeforeTasksPhase(t *testing.T) {
	root := filepath.Join("..", "..")
	statePath, journalPath := copyInactiveRuntime(t, root)
	startLockedREQ(t, root, statePath, journalPath)
	seedPlanningArtifacts(t, statePath)

	_, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID:     "TR-002",
		ExpectedRevision: 1,
		Actor:            "orchestrator",
	})
	if err == nil || !strings.Contains(err.Error(), "planning.tasks") {
		t.Fatalf("TR-002 must reject planning.design, got: %v", err)
	}
}

func TestTASK03901LoopDefinitionSchemaAcceptsCanonicalAutoTrigger(t *testing.T) {
	root := filepath.Join("..", "..")
	if err := schema.NewValidator(root).ValidateFile(
		"loop-definition.schema.json",
		"docs/loop-definition.json",
	); err != nil {
		t.Fatalf("canonical auto-trigger metadata must validate: %v", err)
	}
}

func TestTASK03901CatalogRejectsInvalidAutoTriggerMetadata(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{
			name: "unknown quality gate",
			mutate: func(auto map[string]any) {
				auto["quality_gate_id"] = "GATE-NOT-REGISTERED"
			},
			want: "unregistered quality gate",
		},
		{
			name: "actor is not declared",
			mutate: func(auto map[string]any) {
				auto["actors"] = []any{"orchestrator"}
			},
			want: "actor",
		},
		{
			name: "human transition is automatic",
			mutate: func(auto map[string]any) {
				auto["human_required"] = true
			},
			want: "human-required",
		},
		{
			name: "more than one trigger per event",
			mutate: func(auto map[string]any) {
				auto["max_per_event"] = float64(2)
			},
			want: "max_per_event",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeLoopDefinitionVariant(t, func(definition map[string]any) {
				planning := definition["phase_machines"].(map[string]any)["planning"].(map[string]any)
				planningTransitions := planning["transitions"].([]any)
				first := planningTransitions[0].(map[string]any)
				auto := first["auto_trigger"].(map[string]any)
				tc.mutate(auto)
				if actors, ok := auto["actors"]; ok {
					first["actors"] = actors
					delete(auto, "actors")
				}
			})

			_, err := transition.LoadCatalog(root)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadCatalog error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestTASK03901LoaderRejectsSemanticAutomationViolations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{
			name: "wrong but declared actor",
			mutate: func(definition map[string]any) {
				planning := definition["phase_machines"].(map[string]any)["planning"].(map[string]any)
				first := planning["transitions"].([]any)[0].(map[string]any)
				first["auto_trigger"].(map[string]any)["actor"] = "orchestrator"
			},
			want: "canonical hook_controller",
		},
		{
			name: "unknown actor list member",
			mutate: func(definition map[string]any) {
				planning := definition["phase_machines"].(map[string]any)["planning"].(map[string]any)
				first := planning["transitions"].([]any)[0].(map[string]any)
				first["actors"] = append(first["actors"].([]any), "unknown_actor")
			},
			want: "unregistered actor",
		},
		{
			name: "human-only transition is automatic",
			mutate: func(definition map[string]any) {
				for _, raw := range definition["transitions"].([]any) {
					candidate := raw.(map[string]any)
					if candidate["id"] != "TR-005" {
						continue
					}
					candidate["actors"] = append(candidate["actors"].([]any), "hook_controller")
					candidate["automation"] = map[string]any{"eligible": false, "human_boundary": true}
					candidate["auto_trigger"] = map[string]any{
						"enabled": true, "event": "PreToolUse", "actor": "hook_controller",
						"quality_gate_id": "GATE-DOCUMENT-PASS", "max_per_event": float64(1), "human_required": false,
					}
					return
				}
				t.Fatal("TR-005 not found")
			},
			want: "automation eligibility",
		},
		{
			name: "top-level and phase scopes are ambiguous",
			mutate: func(definition map[string]any) {
				for _, raw := range definition["transitions"].([]any) {
					candidate := raw.(map[string]any)
					if candidate["from"] == "verification" {
						candidate["selector"] = "SEL-CROSS-TOP"
					}
				}
				verification := definition["phase_machines"].(map[string]any)["verification"].(map[string]any)
				for _, raw := range verification["transitions"].([]any) {
					candidate := raw.(map[string]any)
					candidate["selector"] = "SEL-CROSS-TOP"
					if candidate["id"] == "PTR-VERIFY-01" {
						candidate["selector"] = "SEL-CROSS-PHASE"
					}
				}
			},
			want: "verification.delivery",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeLoopDefinitionVariant(t, tc.mutate)
			_, err := transition.LoadCatalog(root)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadCatalog error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestTASK03901CatalogRejectsMultipleAutomaticCandidatesWithoutSelector(t *testing.T) {
	root := writeLoopDefinitionVariant(t, func(definition map[string]any) {
		planning := definition["phase_machines"].(map[string]any)["planning"].(map[string]any)
		transitions := planning["transitions"].([]any)
		clone := make(map[string]any)
		for key, value := range transitions[0].(map[string]any) {
			clone[key] = value
		}
		clone["id"] = "PTR-PLAN-03"
		clone["event"] = "planning_design_rework"
		clone["to"] = "design"
		delete(clone, "selector")
		transitions = append(transitions, clone)
		planning["transitions"] = transitions
	})

	_, err := transition.LoadCatalog(root)
	if err == nil || !strings.Contains(err.Error(), "selector") {
		t.Fatalf("LoadCatalog error = %v, want selector rejection", err)
	}
}

func TestTASK03901CatalogCoversAutomaticMainlineAndRecovery(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}

	wantAuto := []string{
		"PTR-PLAN-01", "PTR-PLAN-02", "TR-002", "TR-003", "TR-004", "TR-006", "TR-007",
		"PTR-VERIFY-01", "PTR-VERIFY-02", "PTR-VERIFY-03", "PTR-VERIFY-04", "PTR-VERIFY-05",
		"TR-008", "TR-009", "TR-010", "TR-011", "TR-012", "TR-013", "TR-014", "TR-015",
		"TR-016", "TR-017", "TR-018", "PTR-BUG-01", "PTR-BUG-02", "PTR-BUG-03", "PTR-BUG-04",
		"PTR-BUG-05", "PTR-BUG-06", "PTR-BUG-07", "TR-022", "TR-023", "TR-024",
	}
	for _, id := range wantAuto {
		spec, ok := catalog.Transitions[id]
		if !ok {
			spec, ok = catalog.PhaseTransitionSpec[id]
		}
		if !ok {
			t.Fatalf("automatic transition %s is not declared", id)
		}
		if spec.AutoTrigger == nil || !spec.AutoTrigger.Enabled {
			t.Fatalf("automatic transition %s must be enabled in the catalog", id)
		}
		if spec.AutoTrigger.Event != "PreToolUse" || spec.AutoTrigger.MaxPerEvent != 1 {
			t.Fatalf("automatic transition %s has invalid trigger: %#v", id, spec.AutoTrigger)
		}
		if spec.AutoTrigger.Actor != "hook_controller" {
			t.Fatalf("automatic transition %s actor = %q, want hook_controller", id, spec.AutoTrigger.Actor)
		}
		if spec.Automation == nil || !spec.Automation.Eligible || spec.Automation.HumanBoundary {
			t.Fatalf("automatic transition %s has invalid automation policy: %#v", id, spec.Automation)
		}
	}

	for _, id := range []string{"TR-001", "TR-005", "TR-019", "TR-020", "TR-021"} {
		if spec, ok := catalog.Transitions[id]; ok && spec.AutoTrigger != nil && spec.AutoTrigger.Enabled {
			t.Fatalf("human-only transition %s must not be automatic", id)
		}
	}
}

func TestTASK03901SelectorResolverReturnsZeroForUnmatchedFacts(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}

	resolution, err := catalog.ResolveAutomaticTransition(
		transition.Cursor{State: "planning", Phase: "design"},
		transition.TriggerFacts{RequestedEvents: []string{"unrelated_event"}},
	)
	if err != nil {
		t.Fatalf("unmatched facts must not conflict: %v", err)
	}
	if resolution.Transition != nil {
		t.Fatalf("unmatched facts selected %s", resolution.Transition.ID)
	}
}

func TestTASK03901SelectorResolverReturnsOneCandidate(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}

	resolution, err := catalog.ResolveAutomaticTransition(
		transition.Cursor{State: "planning", Phase: "design"},
		transition.TriggerFacts{GateOutcomes: []transition.GateOutcome{{
			GateID: "GATE-PLANNING-DESIGN-COMPLETE",
			Status: "satisfied",
		}}},
	)
	if err != nil {
		t.Fatalf("one satisfied gate must resolve: %v", err)
	}
	if resolution.Transition == nil || resolution.Transition.ID != "PTR-PLAN-01" {
		t.Fatalf("resolved transition = %#v, want PTR-PLAN-01", resolution.Transition)
	}
}

func TestTASK03901SelectorResolverReturnsConflictForMutuallyExclusiveEvents(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}

	resolution, err := catalog.ResolveAutomaticTransition(
		transition.Cursor{State: "verification", Phase: "clean_round_evaluation"},
		transition.TriggerFacts{
			RequestedEvents: []string{"clean_round_valid", "clean_round_incomplete"},
			GateOutcomes: []transition.GateOutcome{
				{GateID: "GATE-VERIFY-CLEAN-ROUND-VALID", Status: "satisfied"},
				{GateID: "GATE-CLEAN-ROUND-INCOMPLETE", Status: "satisfied"},
			},
		},
	)
	if resolution.Transition != nil {
		t.Fatalf("conflicting facts selected %s", resolution.Transition.ID)
	}
	if err == nil || !strings.Contains(err.Error(), transition.TriggerConflictCode) {
		t.Fatalf("conflicting facts error = %v, want %s", err, transition.TriggerConflictCode)
	}
}

func TestTASK03901CatalogReadsLegacyPluralAutoTriggerEvent(t *testing.T) {
	root := writeLoopDefinitionVariant(t, func(definition map[string]any) {
		planning := definition["phase_machines"].(map[string]any)["planning"].(map[string]any)
		first := planning["transitions"].([]any)[0].(map[string]any)
		auto := first["auto_trigger"].(map[string]any)
		delete(auto, "event")
		auto["events"] = []any{"PreToolUse"}
	})

	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatalf("legacy auto-trigger event must be readable: %v", err)
	}
	auto := catalog.PhaseTransitionSpec["PTR-PLAN-01"].AutoTrigger
	if got := auto.Event; got != "PreToolUse" {
		t.Fatalf("legacy auto-trigger event = %q, want PreToolUse", got)
	}
	if len(auto.Events) != 0 {
		t.Fatalf("legacy events must be cleared after canonicalization: %#v", auto.Events)
	}
	encoded, err := json.Marshal(auto)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "events") {
		t.Fatalf("canonical auto-trigger round-trip must not emit events: %s", encoded)
	}
	var roundTrip transition.AutoTriggerSpec
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip.Event != "PreToolUse" || len(roundTrip.Events) != 0 {
		t.Fatalf("canonical round-trip = %#v", roundTrip)
	}
}

func writeLoopDefinitionVariant(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	data, err := os.ReadFile("../../docs/loop-definition.json")
	if err != nil {
		t.Fatal(err)
	}
	var definition map[string]any
	if err := json.Unmarshal(data, &definition); err != nil {
		t.Fatal(err)
	}
	mutate(definition)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err = json.MarshalIndent(definition, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "loop-definition.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
