package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverE2EInventoryOffersCandidatesWithoutAssetDeclarations(t *testing.T) {
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

	inventory, diagnostics := discoverE2EInventory(root, map[string]any{"bound_req": caseScopeREQ(t, root, "docs/design/prototypes/settings/\n")})
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected discovery diagnostics: %v", diagnostics)
	}
	if len(inventory.Cases) != 2 {
		t.Fatalf("cases=%d, want 2 required browser cases: %+v", len(inventory.Cases), inventory.Cases)
	}
	if len(inventory.Candidates) != 1 {
		t.Fatalf("candidates=%d, want one script hint: %+v", len(inventory.Candidates), inventory.Candidates)
	}
	for _, asset := range inventory.Candidates {
		if asset.Path != specRel || len(asset.SHA256) != 64 || len(asset.PossibleCases) != 2 || !strings.Contains(asset.PossibleCases[0], "/settings/cases.json#CASE-") {
			t.Fatalf("invalid asset fingerprint: %+v", asset)
		}
	}
}

func TestDraftPlanKeepsAssetReuseUndecidedUntilMainChooses(t *testing.T) {
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
	if plan.E2ECoverageState != "" {
		t.Fatalf("without a professional choice E2E must remain undecided, got %q", plan.E2ECoverageState)
	}
	if plan.VerificationArtifactWorkspace != nil || len(plan.E2EAssets) != 0 {
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
	if plan.E2ECoverageState != "" || len(plan.E2EAssets) != 0 || plan.VerificationArtifactWorkspace != nil {
		t.Fatalf("CASE text matches must not decide regression availability: state=%q assets=%d workspace=%v", plan.E2ECoverageState, len(plan.E2EAssets), plan.VerificationArtifactWorkspace)
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

func TestE2ECandidatesKeepCollidingModuleCASEsAndExactTokenBoundaries(t *testing.T) {
	root := t.TempDir()
	for _, m := range []string{"alpha", "beta"} {
		dir := filepath.Join(root, "docs/design/prototypes", m)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cases.json"), []byte(`{"cases":[{"id":"CASE-001","required":true,"browser_required":true,"flow_refs":["PATH-001"]}]}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "web"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"alpha.spec.ts": "// CASE-001 is only a comment", "other.spec.ts": "CASE-0010"} {
		if err := os.WriteFile(filepath.Join(root, "web", name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	inventory, diagnostics := discoverE2EInventory(root, map[string]any{"bound_req": caseScopeREQ(t, root, "docs/design/prototypes/alpha/\ndocs/design/prototypes/beta/\n")})
	if len(diagnostics) != 0 || len(inventory.Cases) != 2 || len(inventory.Candidates) != 1 {
		t.Fatalf("lost or conflated source: %+v %v", inventory, diagnostics)
	}
	refs := inventory.Candidates[0].PossibleCases
	if len(refs) != 2 || refs[0] != "docs/design/prototypes/alpha/cases.json#CASE-001" || refs[1] != "docs/design/prototypes/beta/cases.json#CASE-001" {
		t.Fatalf("wrong identity hints: %v", refs)
	}
}
