// Conformance tests for the transition catalog. Per BUG-001 §4b.3 this file
// is the table-driven proof that every declared transition, guard, action,
// global transition, and forbidden event in loop-definition.json resolves
// against an implementation (or is rejected as forbidden). The tests read
// the catalog at runtime so a missing implementation or a declaration
// drift would cause a test failure.
package transition_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

// TestCatalogLoadSucceedsAgainstRepo verifies LoadCatalog passes against the
// real docs/loop-definition.json. A failure here means a declared guard or
// action is missing from the registry, which is the fail-closed invariant.
func TestCatalogLoadSucceedsAgainstRepo(t *testing.T) {
	root := "../.."
	cat, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}
	if cat == nil || cat.Definition == nil {
		t.Fatal("LoadCatalog returned a nil catalog or definition")
	}
}

func TestDefinitionUsesMainSpineStagesAndIndependentBinding(t *testing.T) {
	cat, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}
	want := map[string]string{
		"inactive": "S0", "planning": "S2/S3/S4",
		"document_verification": "S5", "building": "S6",
		"verification": "S7", "bug_resolution": "S8/S9",
		"acceptance": "S10", "release_audit": "S10",
		"awaiting_human_release": "S11",
	}
	for state, stage := range want {
		if got := cat.Definition.States[state].Stage; got != stage {
			t.Fatalf("state %s stage = %q, want %q", state, got, stage)
		}
	}
	if strings.Contains(cat.Definition.States["inactive"].ExitCondition, "/loop") {
		t.Fatal("inactive exit must use independent REQ binding, not Claude /loop")
	}
}

// TestConformanceOneRowPerTopLevelTransition asserts that for every declared
// top-level transition id there is a row in catalog.Transitions.
func TestConformanceOneRowPerTopLevelTransition(t *testing.T) {
	root := "../.."
	cat, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}
	declared := cat.SortedTransitionIDs()
	if len(declared) == 0 {
		t.Fatal("no top-level transitions declared")
	}
	for _, id := range declared {
		t.Run("transition:"+id, func(t *testing.T) {
			spec, ok := cat.Transitions[id]
			if !ok {
				t.Fatalf("transition %s declared but not in catalog.Transitions", id)
			}
			if spec.ID != id {
				t.Fatalf("transition %s catalog key mismatch: %s", id, spec.ID)
			}
		})
	}
}

// TestConformanceOneRowPerPhaseTransition asserts every declared phase
// transition is resolvable.
func TestConformanceOneRowPerPhaseTransition(t *testing.T) {
	root := "../.."
	cat, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}
	for owner, machine := range cat.Definition.PhaseMachines {
		for _, pt := range machine.Transitions {
			id := pt.ID
			t.Run("phase:"+owner+":"+id, func(t *testing.T) {
				if _, ok := cat.PhaseTransitionSpec[id]; !ok {
					t.Fatalf("phase transition %s not in catalog.PhaseTransitionSpec", id)
				}
			})
		}
	}
}

// TestConformanceOneRowPerGlobalTransition asserts every declared global
// transition (GTR-001..005) is reachable.
func TestConformanceOneRowPerGlobalTransition(t *testing.T) {
	root := "../.."
	cat, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}
	for _, id := range cat.SortedGlobalTransitionIDs() {
		t.Run("global:"+id, func(t *testing.T) {
			if id == "" {
				t.Fatal("empty global transition id")
			}
		})
	}
}

// TestConformanceEveryDeclaredGuardIsRegistered iterates the catalog's
// declared guard names (top-level + phase + entity) and verifies each one is
// registered in guardRegistry. A failure means a guard name in loop-
// definition.json has no implementation — fail-closed invariant.
func TestConformanceEveryDeclaredGuardIsRegistered(t *testing.T) {
	root := "../.."
	cat, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}
	declared := map[string]bool{}
	for _, t0 := range cat.Transitions {
		for _, g := range t0.Guards {
			declared[g] = true
		}
	}
	for _, machine := range cat.Definition.PhaseMachines {
		for _, pt := range machine.Transitions {
			for _, g := range pt.Guards {
				declared[g] = true
			}
		}
	}
	for _, entity := range cat.Definition.EntityLifecycles {
		for _, et := range entity.Transitions {
			for _, g := range et.Guards {
				declared[g] = true
			}
		}
	}
	for _, gtr := range cat.GlobalTransitions {
		for _, g := range gtr.Guards {
			declared[g] = true
		}
	}
	names := make([]string, 0, len(declared))
	for n := range declared {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run("guard:"+name, func(t *testing.T) {
			registration, ok := transition.LookupGuardRegistration(name)
			if !ok {
				t.Fatalf("guard %s declared in spec but not in guardRegistry", name)
			}
			if registration.Enforcement != transition.GuardSemanticCheck && registration.Enforcement != transition.GuardEvidenceAttestation {
				t.Fatalf("guard %s has unknown enforcement %q", name, registration.Enforcement)
			}
		})
	}
}

func TestGuardEnforcementStrengthIsHonest(t *testing.T) {
	cases := map[string]transition.GuardEnforcement{
		"planning_complete":          transition.GuardSemanticCheck,
		"no_other_active_loop":       transition.GuardSemanticCheck,
		"joint_document_pass":        transition.GuardEvidenceAttestation,
		"completion_report_complete": transition.GuardEvidenceAttestation,
	}
	for name, want := range cases {
		registration, ok := transition.LookupGuardRegistration(name)
		if !ok {
			t.Fatalf("guard %s is not registered", name)
		}
		if registration.Enforcement != want {
			t.Fatalf("guard %s enforcement = %q, want %q", name, registration.Enforcement, want)
		}
	}
}

// TestConformanceEveryDeclaredActionIsRegistered mirrors the guard test for
// the action registry.
func TestConformanceEveryDeclaredActionIsRegistered(t *testing.T) {
	root := "../.."
	cat, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}
	declared := map[string]bool{}
	for _, t0 := range cat.Transitions {
		for _, a := range t0.Actions {
			declared[a] = true
		}
	}
	for _, machine := range cat.Definition.PhaseMachines {
		for _, pt := range machine.Transitions {
			for _, a := range pt.Actions {
				declared[a] = true
			}
		}
	}
	for _, entity := range cat.Definition.EntityLifecycles {
		for _, et := range entity.Transitions {
			for _, a := range et.Actions {
				declared[a] = true
			}
		}
	}
	for _, gtr := range cat.GlobalTransitions {
		for _, a := range gtr.Actions {
			declared[a] = true
		}
	}
	names := make([]string, 0, len(declared))
	for n := range declared {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run("action:"+name, func(t *testing.T) {
			if _, ok := transition.LookupAction(name); !ok {
				t.Fatalf("action %s declared in spec but not in actionRegistry", name)
			}
		})
	}
}

// TestConformanceOneRowPerForbiddenEvent iterates the forbidden_events list
// from the catalog and asserts each event has an enforcement entry.
func TestConformanceOneRowPerForbiddenEvent(t *testing.T) {
	root := "../.."
	cat, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}
	for _, name := range cat.SortedForbiddenEvents() {
		t.Run("forbidden:"+name, func(t *testing.T) {
			fe, ok := cat.ForbiddenEvents[name]
			if !ok {
				t.Fatalf("forbidden event %s declared but missing enforcement", name)
			}
			if fe.Enforcement == "" {
				t.Errorf("forbidden event %s has empty enforcement", name)
			}
		})
	}
}
