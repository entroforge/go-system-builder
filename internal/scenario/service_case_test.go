package scenario_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/scenario"
)

func serviceREQBody() string {
	return strings.Replace(bridgeReqBody, "> UI impact：changed", "> UI impact：none", 1)
}

func TestServiceScenarioWithoutUIAssetsRetainsCasesAndBridge(t *testing.T) {
	root := bridgeFixture(t, serviceREQBody())
	report, err := scenario.GenerateModule(root, "investor-workbench")
	if err != nil {
		t.Fatalf("GenerateModule() rejected service-only package: %v", err)
	}
	if report.Counts.Cases != 2 || report.Counts.BrowserRequiredCases != 0 {
		t.Fatalf("service coverage counts = cases %d, browser %d; want 2, 0", report.Counts.Cases, report.Counts.BrowserRequiredCases)
	}

	moduleDir := filepath.Join(root, "docs/design/prototypes/investor-workbench")
	for _, name := range []string{"index.html", "service.html"} {
		if _, err := os.Stat(filepath.Join(moduleDir, name)); !os.IsNotExist(err) {
			t.Fatalf("service-only package unexpectedly requires %s (stat error %v)", name, err)
		}
	}
	var generated scenario.CasesOutput
	caseBytes, err := os.ReadFile(filepath.Join(moduleDir, "cases.json"))
	if err != nil {
		t.Fatalf("read generated cases: %v", err)
	}
	if err := json.Unmarshal(caseBytes, &generated); err != nil {
		t.Fatalf("decode generated cases: %v", err)
	}
	if len(generated.Cases) != 2 {
		t.Fatalf("generated CASE count = %d, want 2", len(generated.Cases))
	}
	for _, current := range generated.Cases {
		if current.BrowserRequired || len(current.FlowRefs) != 1 || current.FlowRefs[0] != "F-001" {
			t.Fatalf("service CASE %s must retain an F-only non-browser route: %+v", current.ID, current)
		}
	}

	if _, err := scenario.ValidateModule(root, "investor-workbench", scenario.ValidateOptions{}); err != nil {
		t.Fatalf("ValidateModule() rejected generated service package: %v", err)
	}
	bridge, err := scenario.RunBridge(root, true)
	if err != nil {
		t.Fatalf("full AC→CASE bridge rejected service package: %v", err)
	}
	if bridge.TotalAC != 3 || bridge.ReachedCases != 1 || bridge.EndorsedNA != 2 {
		t.Fatalf("bridge counts = total %d reached %d N/A %d, want 3/1/2", bridge.TotalAC, bridge.ReachedCases, bridge.EndorsedNA)
	}
	if err := scenario.GuardBridgeChecked(root); err != nil {
		t.Fatalf("PTR-PLAN-02 bridge guard rejected service package: %v", err)
	}
}

func TestServiceScenarioStillRequiresFRRuleReference(t *testing.T) {
	root := bridgeFixture(t, serviceREQBody())
	modelPath := filepath.Join(root, "docs/design/prototypes/investor-workbench/scenario-model.json")
	data, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	var model map[string]any
	if err := json.Unmarshal(data, &model); err != nil {
		t.Fatal(err)
	}
	rules := model["rules"].([]any)
	rules[0].(map[string]any)["source_refs"] = []any{"REQ-001/FR-002"}
	encoded, err := json.MarshalIndent(model, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelPath, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := scenario.GuardBridgeChecked(root); err == nil || !strings.Contains(err.Error(), "FR-001") {
		t.Fatalf("bridge must reject a service package that dropped the FR reference, got %v", err)
	}
}

func TestServiceScenarioRequiresStoryAndFlowSources(t *testing.T) {
	for _, missing := range []string{"stories.md", "flows.md"} {
		t.Run(missing, func(t *testing.T) {
			root := bridgeFixture(t, serviceREQBody())
			path := filepath.Join(root, "docs/design/prototypes/investor-workbench", missing)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil {
				t.Fatalf("GenerateModule() accepted service package without %s", missing)
			}
		})
	}
}
