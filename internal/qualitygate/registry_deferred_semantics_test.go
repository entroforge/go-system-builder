package qualitygate_test

import (
	"context"
	"testing"

	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestRegistryDefersUnimplementedUnrelatedGateToItsEvaluation(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatal(err)
	}
	// A future S9 gate has no implementation yet. The implemented S7
	// Finding exit must remain usable and must not need batch evidence.
	spec := catalog.Transitions["TR-012"]
	trigger := *spec.AutoTrigger
	trigger.QualityGateID = "GATE-FUTURE-UNIMPLEMENTED"
	spec.AutoTrigger = &trigger
	catalog.Transitions["TR-012"] = spec
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatalf("unrelated missing semantics blocked registry: %v", err)
	}
	engine := qualitygate.NewEvaluator(registry)
	state := map[string]any{
		"lifecycle": map[string]any{"state": "verification", "phase": "observation_sealed"},
		"review":    map[string]any{"round_entry": map[string]any{"plan_id": "plan-current"}},
		"entities":  map[string]any{"findings": []any{map[string]any{"finding_id": "F-1", "review_plan_id": "plan-current"}}},
	}
	result, err := engine.Evaluate(context.Background(), qualitygate.Input{
		Snapshot: runtime.Snapshot{State: state}, GateID: "GATE-VERIFY-BLOCKING-FINDING", TransitionID: "TR-008",
	})
	if err != nil || result.Status != qualitygate.StatusSatisfied {
		t.Fatalf("accepted Finding exit = %#v, %v", result, err)
	}
	state["entities"] = map[string]any{"findings": []any{}}
	result, err = engine.Evaluate(context.Background(), qualitygate.Input{
		Snapshot: runtime.Snapshot{State: state}, GateID: "GATE-VERIFY-BLOCKING-FINDING", TransitionID: "TR-008",
	})
	if err != nil || result.Status != qualitygate.StatusNotReady {
		t.Fatalf("Finding exit without a Finding = %#v, %v", result, err)
	}

	state["lifecycle"] = map[string]any{"state": spec.From, "phase": spec.FromPhase}
	result, err = engine.Evaluate(context.Background(), qualitygate.Input{
		Snapshot: runtime.Snapshot{State: state}, GateID: trigger.QualityGateID, TransitionID: "TR-012",
	})
	if err != nil || result.Status != qualitygate.StatusUnknown || result.ErrorCode != qualitygate.ErrorGateUnknown {
		t.Fatalf("current unimplemented gate = %#v, %v", result, err)
	}
}
