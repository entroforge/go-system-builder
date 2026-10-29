package scenario_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/scenario"
)

// Both requirements are real fixture files. The module keeps one current
// model/catalog; the bound requirement supplies only this round's AC scope.
func multiREQModule(t *testing.T) string {
	t.Helper()
	root := bridgeFixture(t, serviceREQBody())
	dir := filepath.Join(root, "docs/design/prototypes/investor-workbench")
	data, err := os.ReadFile(filepath.Join(dir, "scenario-model.json"))
	if err != nil {
		t.Fatal(err)
	}
	var model map[string]any
	if err := json.Unmarshal(data, &model); err != nil {
		t.Fatal(err)
	}
	old := model["rules"].([]any)[0].(map[string]any)
	old["source_refs"] = []any{"REQ-OLD-001/FR-009"}
	copyBytes, _ := json.Marshal(old)
	var current map[string]any
	if err := json.Unmarshal(copyBytes, &current); err != nil {
		t.Fatal(err)
	}
	current["id"], current["source_refs"] = "rule-current", []any{"REQ-001/FR-001"}
	for i, raw := range current["branches"].([]any) {
		branch := raw.(map[string]any)
		branch["id"] = []string{"b-current-pos", "b-current-neg"}[i]
		branch["case_id"] = []string{"CASE-CURRENT-001", "CASE-CURRENT-002"}[i]
		branch["title"] = "Current requirement discriminator " + branch["id"].(string)
	}
	model["rules"] = []any{old, current}
	writeJSON(t, filepath.Join(dir, "scenario-model.json"), model)
	writeJSON(t, filepath.Join(dir, "cross-matrix.json"), map[string]any{"module": "investor-workbench", "entries": []any{
		map[string]any{"fact": "fact-x", "story": "S-001", "req_ref": "REQ-OLD-001/FR-009", "branch": "b-pos"},
		map[string]any{"fact": "fact-x", "story": "S-001", "req_ref": "REQ-OLD-001/FR-009", "branch": "b-neg"},
		map[string]any{"fact": "fact-x", "story": "S-001", "req_ref": "REQ-001/FR-001", "branch": "b-current-pos"},
		map[string]any{"fact": "fact-x", "story": "S-001", "req_ref": "REQ-001/FR-001", "branch": "b-current-neg"},
	}})
	if err := os.WriteFile(filepath.Join(root, "docs/requirements/REQ-OLD-001.md"), []byte("# REQ-OLD-001\n\n| ID | 描述 | 指向 |\n|---|---|---|\n| FR-009 | Original behavior | S-001 |\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func readModuleJSON(t *testing.T, root, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "docs/design/prototypes/investor-workbench", name))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestModuleKeepsCompleteMultiREQBehaviorAndBoundACBridge(t *testing.T) {
	root := multiREQModule(t)
	dir := filepath.Join(root, "docs/design/prototypes/investor-workbench")
	before, err := os.ReadFile(filepath.Join(dir, "scenario-model.json"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := scenario.GenerateModule(root, "investor-workbench")
	if err != nil {
		t.Fatalf("complete module rejected its original historical source: %v", err)
	}
	if report.Counts.Cases != 4 {
		t.Fatalf("current catalog lost behaviors: %+v", report.Counts)
	}
	if _, err := scenario.ValidateAll(root, scenario.ValidateOptions{}); err != nil {
		t.Fatalf("S3 consumer rejected complete current module: %v", err)
	}
	if err := scenario.GuardBridgeChecked(root); err != nil {
		t.Fatal(err)
	}
	bridge, err := scenario.RunBridge(root, true)
	if err != nil || bridge.REQ != "REQ-001" || bridge.TotalAC != 3 || bridge.ReachedCases != 1 {
		t.Fatalf("bound AC scope: %+v %v", bridge, err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "scenario-model.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("generation rewrote original model or historical source")
	}
	var cases scenario.CasesOutput
	data, _ := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, item := range cases.Cases {
		ids[item.ID] = true
		wantRule := "rule-current"
		if strings.HasPrefix(item.ID, "CASE-X-") {
			wantRule = "rule-x"
		}
		if item.RuleID != wantRule {
			t.Fatalf("CASE %s was rebound from its original rule to %s", item.ID, item.RuleID)
		}
	}
	for _, id := range []string{"CASE-X-001", "CASE-X-002", "CASE-CURRENT-001", "CASE-CURRENT-002"} {
		if !ids[id] {
			t.Fatalf("original case %s was dropped/renumbered", id)
		}
	}
}

func TestMultiREQModuleRetainsBranchAndCurrentSourceGuards(t *testing.T) {
	for _, fault := range []string{"missing_old_required", "wrong_branch_source", "unknown_current_FR", "unknown_FR_under_REQ_cell", "neighbor_REQ_name"} {
		t.Run(fault, func(t *testing.T) {
			root := multiREQModule(t)
			model := readModuleJSON(t, root, "scenario-model.json")
			matrix := readModuleJSON(t, root, "cross-matrix.json")
			entries := matrix["entries"].([]any)
			current := model["rules"].([]any)[1].(map[string]any)
			want := ""
			switch fault {
			case "missing_old_required":
				matrix["entries"] = entries[1:]
				want = "b-pos"
			case "wrong_branch_source":
				entries[0].(map[string]any)["req_ref"] = "REQ-OTHER-001/FR-009"
				want = "never cites"
			case "unknown_current_FR":
				entries[2].(map[string]any)["req_ref"] = "REQ-001/FR-999"
				want = "does not declare"
			case "unknown_FR_under_REQ_cell":
				current["source_refs"] = []any{"REQ-001/FR-999"}
				for _, raw := range entries[2:] {
					raw.(map[string]any)["req_ref"] = "REQ-001"
				}
				want = "does not declare"
			case "neighbor_REQ_name":
				entries[2].(map[string]any)["req_ref"] = "REQ-0010"
				want = "never cites"
			}
			dir := filepath.Join(root, "docs/design/prototypes/investor-workbench")
			writeJSON(t, filepath.Join(dir, "scenario-model.json"), model)
			writeJSON(t, filepath.Join(dir, "cross-matrix.json"), matrix)
			if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s lost original source/coverage guard: %v", fault, err)
			}
		})
	}
}

func TestHistoricalSameFRNumberCannotReplaceCurrentAC(t *testing.T) {
	root := multiREQModule(t)
	dir := filepath.Join(root, "docs/design/prototypes/investor-workbench")
	model := readModuleJSON(t, root, "scenario-model.json")
	old := model["rules"].([]any)[0].(map[string]any)
	old["source_refs"] = []any{"REQ-OLD-001/FR-001"}
	model["rules"] = []any{old}
	writeJSON(t, filepath.Join(dir, "scenario-model.json"), model)
	matrix := readModuleJSON(t, root, "cross-matrix.json")
	entries := matrix["entries"].([]any)[:2]
	for _, raw := range entries {
		raw.(map[string]any)["req_ref"] = "REQ-OLD-001/FR-001"
	}
	matrix["entries"] = entries
	writeJSON(t, filepath.Join(dir, "cross-matrix.json"), matrix)
	if err := os.WriteFile(filepath.Join(root, "docs/requirements/REQ-OLD-001.md"), []byte("# REQ-OLD-001\n| FR-001 | Historical behavior | S-001 |\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	if _, err := scenario.RunBridge(root, true); err == nil || !strings.Contains(err.Error(), "REQ-001/FR-001") {
		t.Fatalf("historical FR replaced current obligation: %v", err)
	}
}

func TestREQLevelMatrixJoinsOriginalFRSourceWithoutRewritingIt(t *testing.T) {
	root := multiREQModule(t)
	matrix := readModuleJSON(t, root, "cross-matrix.json")
	for _, raw := range matrix["entries"].([]any) {
		entry := raw.(map[string]any)
		if strings.HasPrefix(entry["req_ref"].(string), "REQ-001/") {
			entry["req_ref"] = "REQ-001"
		}
	}
	writeCrossMatrix(t, root, "investor-workbench", matrix)
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
		t.Fatalf("declared REQ-level source join failed: %v", err)
	}
}

func TestHistoricalModuleDoesNotDemandCurrentFRForNFROnlyRequirement(t *testing.T) {
	root := multiREQModule(t)
	dir := filepath.Join(root, "docs/design/prototypes/investor-workbench")
	model := readModuleJSON(t, root, "scenario-model.json")
	model["rules"] = model["rules"].([]any)[:1]
	writeJSON(t, filepath.Join(dir, "scenario-model.json"), model)
	matrix := readModuleJSON(t, root, "cross-matrix.json")
	matrix["entries"] = matrix["entries"].([]any)[:2]
	writeJSON(t, filepath.Join(dir, "cross-matrix.json"), matrix)
	req := "# REQ-001\n\n## §C 具体需求\n| ID | 描述 | 指向 |\n|---|---|---|\n| NFR-001 | Existing service latency requirement | latency |\n| AC-001 | Verify latency | NFR-001 |\n"
	if err := os.WriteFile(filepath.Join(root, "docs/requirements/REQ-001.md"), []byte(req), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
		t.Fatalf("historical module borrowed current FR obligations: %v", err)
	}
	if _, err := scenario.ValidateAll(root, scenario.ValidateOptions{}); err != nil {
		t.Fatalf("unrelated NFR scope poisoned complete module: %v", err)
	}
}

func TestREQLevelMatrixDoesNotTreatArbitrarySuffixAsFRSource(t *testing.T) {
	root := multiREQModule(t)
	model := readModuleJSON(t, root, "scenario-model.json")
	model["rules"].([]any)[0].(map[string]any)["source_refs"] = []any{"REQ-OLD-001/not-an-FR"}
	writeModel(t, root, model)
	matrix := readModuleJSON(t, root, "cross-matrix.json")
	for _, raw := range matrix["entries"].([]any)[:2] {
		raw.(map[string]any)["req_ref"] = "REQ-OLD-001"
	}
	writeCrossMatrix(t, root, "investor-workbench", matrix)
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil || !strings.Contains(err.Error(), "never cites") {
		t.Fatalf("arbitrary source suffix became a valid FR join: %v", err)
	}
}
