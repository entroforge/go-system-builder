package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverE2EInventoryMapsRequiredCasesToSpecFingerprints(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "docs", "design", "prototypes", "settings")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := `{
  "module": "settings",
  "cases": [
    {"id":"CASE-001","title":"保存设置","polarity":"positive","required":true,"browser_required":true,"flow_refs":["PATH-001"],"oracle":{"visible":["saved"],"terminal_state":"saved"}},
    {"id":"CASE-002","title":"拒绝非法设置","polarity":"negative","required":true,"browser_required":true,"flow_refs":["PATH-002"],"oracle":{"visible":["error"],"terminal_state":"unchanged","rejection":"invalid"}},
    {"id":"CASE-003","title":"内部分支","polarity":"positive","required":true,"browser_required":false,"flow_refs":["PATH-003"],"oracle":{"visible":["n/a"],"terminal_state":"done"}}
  ]
}` + "\n"
	if err := os.WriteFile(filepath.Join(moduleDir, "cases.json"), []byte(cases), 0o644); err != nil {
		t.Fatal(err)
	}
	specRel := "web/e2e/settings/settings.spec.ts"
	specPath := filepath.Join(root, filepath.FromSlash(specRel))
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := "test('CASE-001 PATH-001', async () => {});\ntest('CASE-002 PATH-002', async () => {});\n"
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	inventory, diagnostics := discoverE2EInventory(root, map[string]any{})
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected discovery diagnostics: %v", diagnostics)
	}
	if len(inventory.Cases) != 2 {
		t.Fatalf("cases=%d, want 2 required browser cases: %+v", len(inventory.Cases), inventory.Cases)
	}
	if len(inventory.Assets) != 2 {
		t.Fatalf("assets=%d, want one asset per mapped CASE: %+v", len(inventory.Assets), inventory.Assets)
	}
	for _, asset := range inventory.Assets {
		if asset.Path != specRel || len(asset.SHA256) != 64 || !strings.HasPrefix(asset.CaseRef, "CASE-") {
			t.Fatalf("invalid asset fingerprint: %+v", asset)
		}
	}
}

func TestDraftPlanSplitsE2EByCaseAndFallsBackToColdStart(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "docs", "design", "prototypes", "settings")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := `{"module":"settings","cases":[
  {"id":"CASE-001","title":"保存设置","polarity":"positive","required":true,"browser_required":true,"flow_refs":["PATH-001"],"oracle":{"visible":["saved"],"terminal_state":"saved"}},
  {"id":"CASE-002","title":"拒绝非法设置","polarity":"negative","required":true,"browser_required":true,"flow_refs":["PATH-002"],"oracle":{"visible":["error"],"terminal_state":"unchanged","rejection":"invalid"}}
]}` + "\n"
	if err := os.WriteFile(filepath.Join(moduleDir, "cases.json"), []byte(cases), 0o644); err != nil {
		t.Fatal(err)
	}
	reqPath := filepath.Join(root, "docs", "requirements", "REQ-TEST.md")
	if err := os.MkdirAll(filepath.Dir(reqPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reqPath, []byte("docs/design/prototypes/settings/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := baseDraftState(t)
	state["bound_req"] = map[string]any{"path": "docs/requirements/REQ-TEST.md", "metadata": map[string]any{"ui_impact": "changed"}}

	plan, _ := DraftPlanForRoot(root, state, 3)
	if plan.E2ECoverageState != "cold_start" {
		t.Fatalf("without matching specs E2E must be cold_start, got %q", plan.E2ECoverageState)
	}
	if plan.VerificationArtifactWorkspace == nil || len(plan.E2EAssets) != 0 {
		t.Fatalf("cold-start draft must expose a workspace and no reusable assets: workspace=%v assets=%+v", plan.VerificationArtifactWorkspace, plan.E2EAssets)
	}
	if got := countLensAssignments(plan, "e2e"); got != 2 {
		t.Fatalf("cold-start E2E assignments=%d, want one per required CASE", got)
	}
	for _, claim := range plan.Claims {
		if claim.Lens == "e2e" && strings.Contains(claim.Target, "TODO(planner)") {
			t.Fatalf("case-derived E2E target must be concrete: %+v", claim)
		}
	}

	specPath := filepath.Join(root, "web", "e2e", "settings", "settings.spec.ts")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, []byte("CASE-001 PATH-001\nCASE-002 PATH-002\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, _ = DraftPlanForRoot(root, state, 3)
	if plan.E2ECoverageState != "regression_available" || len(plan.E2EAssets) != 2 || plan.VerificationArtifactWorkspace != nil {
		t.Fatalf("complete CASE->spec mapping must be regression_available: state=%q assets=%d workspace=%v", plan.E2ECoverageState, len(plan.E2EAssets), plan.VerificationArtifactWorkspace)
	}
	if got := countLensAssignments(plan, "e2e"); got != 2 {
		t.Fatalf("regression E2E assignments=%d, want one per required CASE", got)
	}
}

func countLensAssignments(plan *Plan, lens string) int {
	count := 0
	for _, assignment := range plan.Assignments {
		if assignment.Lens == lens {
			count++
		}
	}
	return count
}
