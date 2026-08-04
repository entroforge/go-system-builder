package e2ecoverage_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/e2ecoverage"
)

const fixtureJSON = `{
  "version": "test",
  "req": "REQ-039",
  "weights": {"L0": 0.0, "L1": 0.15, "L2": 0.40, "L3": 0.70, "L4": 1.0},
  "scenarios": [
    {"id": "CT-039-01", "set": "CT", "fidelity": "L3", "test_refs": ["a_test.go:TestA"]},
    {"id": "CT-039-02", "set": "CT", "fidelity": "L1", "test_refs": ["b_test.go:TestB"]},
    {"id": "AC-001", "set": "AC", "fidelity": "L4", "test_refs": ["c_test.go:TestC"]},
    {"id": "AC-002", "set": "AC", "fidelity": "L2", "test_refs": []},
    {"id": "SPINE-S2-S11", "set": "TASK-CLOSING", "fidelity": "L3", "test_refs": ["spine_test.go"]},
    {"id": "HOOK-PreToolUse", "set": "HOOK", "fidelity": "L3", "test_refs": ["hook_test.go"]},
    {"id": "HOOK-SessionStart", "set": "HOOK", "fidelity": "L2", "test_refs": ["hook_test.go"]},
    {"id": "HOOK-PostCompact", "set": "HOOK", "fidelity": "L0", "test_refs": []}
  ]
}`

func TestScoreFromFixture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.json")
	if err := os.WriteFile(path, []byte(fixtureJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	inv, err := e2ecoverage.LoadInventory(path)
	if err != nil {
		t.Fatal(err)
	}
	report := e2ecoverage.Score(inv)

	// CT∪AC: 4 scenarios; 3 have test_refs → 0.75
	if report.IDPresenceNumer != 3 || report.IDPresenceDenom != 4 {
		t.Fatalf("ID presence numer/denom: got %d/%d want 3/4", report.IDPresenceNumer, report.IDPresenceDenom)
	}
	if report.IDPresence != 0.75 {
		t.Fatalf("ID_Presence: got %.3f want 0.750", report.IDPresence)
	}

	// Fidelity: (0.70 + 0.15 + 1.0 + 0.40) / 4 = 0.5625
	wantFidelity := (0.70 + 0.15 + 1.0 + 0.40) / 4.0
	if report.FidelityScore != wantFidelity {
		t.Fatalf("FidelityScore: got %.4f want %.4f", report.FidelityScore, wantFidelity)
	}

	// Hook: 3 events, 1 at L3+
	if report.HookSurface != "1/3" {
		t.Fatalf("HookSurface: got %q want 1/3", report.HookSurface)
	}

	if report.OrganicSpine != 0 {
		t.Fatalf("OrganicSpine: got %d want 0 (L3 spine)", report.OrganicSpine)
	}

	if len(report.BelowL3) != 2 {
		t.Fatalf("BelowL3 count: got %d want 2", len(report.BelowL3))
	}

	if report.GatePassed {
		t.Fatal("gate should fail on fixture")
	}
}

func TestOrganicSpineL4(t *testing.T) {
	inv := e2ecoverage.Inventory{
		Weights: map[string]float64{"L0": 0, "L1": 0.15, "L2": 0.40, "L3": 0.70, "L4": 1.0},
		Scenarios: []e2ecoverage.Scenario{
			{ID: "CT-039-01", Set: "CT", Fidelity: "L3", TestRefs: []string{"x"}},
			{ID: "AC-001", Set: "AC", Fidelity: "L3", TestRefs: []string{"x"}},
			{ID: "SPINE-S2-S11", Set: "TASK-CLOSING", Fidelity: "L4", TestRefs: []string{"x"}},
			{ID: "HOOK-PreToolUse", Set: "HOOK", Fidelity: "L3", TestRefs: []string{"x"}},
			{ID: "HOOK-SessionStart", Set: "HOOK", Fidelity: "L3", TestRefs: []string{"x"}},
			{ID: "HOOK-SubagentStop", Set: "HOOK", Fidelity: "L3", TestRefs: []string{"x"}},
			{ID: "HOOK-PreCompact", Set: "HOOK", Fidelity: "L3", TestRefs: []string{"x"}},
			{ID: "HOOK-PostCompact", Set: "HOOK", Fidelity: "L3", TestRefs: []string{"x"}},
			{ID: "HOOK-SubagentStart", Set: "HOOK", Fidelity: "L3", TestRefs: []string{"x"}},
			{ID: "HOOK-TeammateIdle", Set: "HOOK", Fidelity: "L3", TestRefs: []string{"x"}},
		},
	}
	report := e2ecoverage.Score(inv)
	if report.OrganicSpine != 1 {
		t.Fatalf("OrganicSpine: got %d want 1", report.OrganicSpine)
	}
}

func TestGatePassesAtDecimalPointEightFive(t *testing.T) {
	// 16×L4 + 16×L3 = decimal 0.85; IEEE float of 0.7*16/32 is slightly
	// under 0.85 raw, but the gate must PASS at milli-precision.
	weights := map[string]float64{"L0": 0, "L1": 0.15, "L2": 0.40, "L3": 0.70, "L4": 1.0}
	var scenarios []e2ecoverage.Scenario
	for i := 0; i < 16; i++ {
		scenarios = append(scenarios, e2ecoverage.Scenario{
			ID: "CT-L4-" + string(rune('A'+i)), Set: "CT", Fidelity: "L4", TestRefs: []string{"t"},
		})
	}
	for i := 0; i < 16; i++ {
		scenarios = append(scenarios, e2ecoverage.Scenario{
			ID: "CT-L3-" + string(rune('A'+i)), Set: "CT", Fidelity: "L3", TestRefs: []string{"t"},
		})
	}
	scenarios = append(scenarios,
		e2ecoverage.Scenario{ID: "SPINE-S2-S11", Set: "TASK-CLOSING", Fidelity: "L4", TestRefs: []string{"t"}},
	)
	for _, h := range []string{"PreToolUse", "SessionStart", "PreCompact", "PostCompact", "SubagentStart", "SubagentStop", "TeammateIdle"} {
		scenarios = append(scenarios, e2ecoverage.Scenario{
			ID: "HOOK-" + h, Set: "HOOK", Fidelity: "L3", TestRefs: []string{"t"},
		})
	}
	report := e2ecoverage.Score(e2ecoverage.Inventory{Weights: weights, Scenarios: scenarios})
	if !report.GatePassed {
		t.Fatalf("gate should PASS at FidelityScore≈0.85, got %.17f failures=%v", report.FidelityScore, report.GateFailures)
	}
}
