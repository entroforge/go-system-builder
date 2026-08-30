package qualitygate_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestRegistryCoversEveryAutomaticGate(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	want := make(map[string]string)
	for id, spec := range catalog.Transitions {
		if spec.AutoTrigger != nil && spec.AutoTrigger.Enabled {
			want[spec.AutoTrigger.QualityGateID] = id
		}
	}
	for id, spec := range catalog.PhaseTransitionSpec {
		if spec.AutoTrigger != nil && spec.AutoTrigger.Enabled {
			want[spec.AutoTrigger.QualityGateID] = id
		}
	}

	if got := len(registry.IDs()); got != len(want) {
		t.Fatalf("registry size = %d, want %d", got, len(want))
	}
	for gateID, transitionID := range want {
		spec, ok := registry.Lookup(gateID)
		if !ok {
			t.Errorf("automatic gate %s is not registered", gateID)
			continue
		}
		if spec.TransitionID != transitionID {
			t.Errorf("gate %s transition = %s, want %s", gateID, spec.TransitionID, transitionID)
		}
	}
}

func TestRegistryDefinesSemanticsForEveryAutomaticGate(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	for _, gateID := range registry.IDs() {
		spec, _ := registry.Lookup(gateID)
		if spec.SemanticVersion == "" {
			t.Errorf("gate %s has no semantic version", gateID)
		}
		if len(spec.EvidenceRequirements) == 0 {
			t.Errorf("gate %s has no qualified evidence requirements", gateID)
		}
		for _, requirement := range spec.EvidenceRequirements {
			if requirement.Kind == "" || len(requirement.Responsibilities) == 0 || len(requirement.Conclusions) == 0 {
				t.Errorf("gate %s has incomplete evidence requirement: %#v", gateID, requirement)
			}
		}
	}
}

func TestRegistryLookupReturnsDefensiveCopy(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	first, ok := registry.Lookup("GATE-VERIFY-CLEAN-ROUND-PASSED")
	if !ok {
		t.Fatal("clean-round gate is not registered")
	}
	first.EvidenceRequirements[0].Responsibilities[0] = "mutated"
	first.EvidenceRequirements[0].Conclusions[0] = "mutated"

	second, _ := registry.Lookup("GATE-VERIFY-CLEAN-ROUND-PASSED")
	if second.EvidenceRequirements[0].Responsibilities[0] != "Clean Round Evaluator" {
		t.Fatalf("registry responsibility was mutated: %#v", second.EvidenceRequirements[0])
	}
	if second.EvidenceRequirements[0].Conclusions[0] != "pass" {
		t.Fatalf("registry conclusion was mutated: %#v", second.EvidenceRequirements[0])
	}
}
