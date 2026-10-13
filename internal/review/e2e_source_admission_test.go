package review

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2ESourceMentionsCannotQualifyRegressionRegistration(t *testing.T) {
	for _, tc := range []struct{ name, path, content string }{
		{"foreign_module_browser", "web/test/e2e/foreign/person.spec.ts", `// CASE-001 describes foreign person preference, not settings save`},
		{"foreign_module_unit", "web/test/unit/foreign/person.spec.ts", `// CASE-001 is only a unit-test comment; there is no browser test`},
		{"same_module_comment_only", "web/test/e2e/settings/comment.spec.ts", `// CASE-001
export const noTests = true;`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			fixtureEvidenceRoot = root
			defer func() { fixtureEvidenceRoot = "" }()
			statePath, journalPath := writeState(t, root, coldStartState())
			if err := os.WriteFile(journalPath, nil, 0644); err != nil {
				t.Fatal(err)
			}
			casesPath := filepath.Join(root, "docs/design/prototypes/settings/cases.json")
			if err := os.MkdirAll(filepath.Dir(casesPath), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(casesPath, []byte(`{"module":"settings","cases":[{"id":"CASE-001","title":"save settings","required":true,"browser_required":true,"flow_refs":["PATH-settings-save"]}]}`), 0644); err != nil {
				t.Fatal(err)
			}
			assetPath := filepath.Join(root, tc.path)
			if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(assetPath, []byte(tc.content), 0644); err != nil {
				t.Fatal(err)
			}
			inventory, notes := discoverE2EInventory(root, map[string]any{})
			if len(notes) != 0 || len(inventory.Candidates) != 1 || len(inventory.Assets) != 0 {
				t.Fatalf("discovery=%+v notes=%v", inventory, notes)
			}
			planPath := writeColdStartPlan(t, root, "unused-workspace")
			data, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			var raw map[string]any
			if err = json.Unmarshal(data, &raw); err != nil {
				t.Fatal(err)
			}
			raw["e2e_coverage_state"] = "regression_available"
			raw["verification_artifact_workspace"] = nil
			raw["e2e_assets"] = inventory.Assets
			data, err = json.MarshalIndent(raw, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(planPath, data, 0644); err != nil {
				t.Fatal(err)
			}
			beforeState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			beforeJournal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			_, err = RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: 1, PlanPath: planPath})
			if err == nil || !strings.Contains(err.Error(), "regression_available requires at least one fingerprinted reusable E2E asset") {
				t.Fatalf("uncollected source mention must be rejected: %v", err)
			}
			afterState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			afterJournal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
				t.Fatal("rejected regression registration mutated Runtime")
			}

		})
	}
}
