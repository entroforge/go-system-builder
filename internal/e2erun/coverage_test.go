package e2erun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

func writeCases(t *testing.T, root, module string) string {
	t.Helper()
	path := "docs/design/prototypes/" + module + "/cases.json"
	doc := map[string]any{"module": module, "cases": []any{map[string]any{
		"id": "CASE-001", "rule_id": "RULE-001", "branch_id": "valid", "title": "save settings", "polarity": "positive", "required": true, "browser_required": true,
		"witness": map[string]string{"input": "valid settings"}, "fixture_id": "settings-default", "story_refs": []string{"STORY-001"}, "flow_refs": []string{"PATH-001"},
		"oracle": map[string]any{"visible": []string{"saved"}, "terminal_state": "saved", "persisted_effects": []string{"stored value"}, "forbidden_side_effects": []string{"other users unchanged"}},
	}}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, path), data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCASEOracleDenominatorIsModuleQualifiedAndExact(t *testing.T) {
	root := t.TempDir()
	modules := []string{writeCases(t, root, "settings"), writeCases(t, root, "contacts")}
	oracles, err := ReadOracles(fileview.Disk{Root: root}, modules)
	if err != nil {
		t.Fatal(err)
	}
	if len(oracles) != 8 {
		t.Fatalf("same-named CASEs collapsed: %+v", oracles)
	}
	var required []Requirement
	for _, oracle := range oracles {
		required = append(required, Requirement{ClaimID: "claim-owner", CaseRef: CaseRef{ModuleRef: oracle.ModuleRef, CaseID: oracle.CaseID, OracleRef: oracle.OracleRef, Persona: "owner", DataProfile: "default"}, Project: "desktop", Browser: "chromium"})
	}
	if err := ValidateRequirements(required, oracles); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRequirements(required[:len(required)-1], oracles); err == nil {
		t.Fatal("missing oracle accepted")
	}
	duplicated := append(append([]Requirement(nil), required[:len(required)-1]...), required[0])
	if err := ValidateRequirements(duplicated, oracles); err == nil {
		t.Fatal("same count concealed duplicate/missing tuple")
	}
	foreign := append([]Requirement(nil), required...)
	foreign[0].CaseRef.ModuleRef = "docs/design/prototypes/foreign/cases.json"
	if err := ValidateRequirements(foreign, oracles); err == nil {
		t.Fatal("bare CASE match admitted foreign module")
	}
	if _, err := ReadOracles(fileview.Disk{Root: root}, []string{modules[0], modules[0]}); err == nil {
		t.Fatal("duplicate module denominator accepted")
	}
}

func TestExecutionCoverageDoesNotCountLogsOrDuplicateAttempts(t *testing.T) {
	test := fixtureTest()
	test.CaseRefs = append(test.CaseRefs, CaseRef{ModuleRef: test.CaseRefs[0].ModuleRef, CaseID: "CASE-001", OracleRef: "/oracle/persisted_effects/0", Persona: "owner", DataProfile: "default"})
	required := []Requirement{}
	for _, ref := range test.CaseRefs {
		required = append(required, Requirement{ClaimID: "claim-one", CaseRef: ref, Project: test.Project, Browser: test.Browser})
	}
	r := Receipt{Mode: "execution", RunID: "run-one", Complete: true, Outcome: "pass", Tests: []Test{test}, Attempts: []Attempt{{ExecutionID: "run-one:" + test.ID + ":0", TestID: test.ID, Status: "passed", BrowserPageObserved: true}}, Binding: Binding{RuntimeID: "runtime-one", Generation: 1, Round: 1, SourceCommit: "commit", SubjectDigest: "subject"}}
	if err := ValidateExecutionCoverage(required, []Receipt{r}); err != nil {
		t.Fatalf("one shared execution may cover several explicit oracles: %v", err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Receipt)
	}{
		{"collection_only", func(r *Receipt) { r.Mode = "collection"; r.Attempts = nil }},
		{"log_only", func(r *Receipt) { r.Attempts = nil; r.Output = "2 passed" }},
		{"wrong_browser", func(r *Receipt) { r.Tests[0].Browser = "webkit" }},
		{"wrong_project", func(r *Receipt) { r.Tests[0].Project = "other" }},
		{"wrong_persona", func(r *Receipt) { r.Tests[0].CaseRefs[0].Persona = "guest" }},
		{"missing_oracle", func(r *Receipt) { r.Tests[0].CaseRefs = r.Tests[0].CaseRefs[:1] }},
		{"repeated_execution", func(r *Receipt) { r.Attempts = append(r.Attempts, r.Attempts[0]) }},
		{"forged_execution_id", func(r *Receipt) { r.Attempts[0].ExecutionID = "self-reported-pass" }},
		{"no_browser", func(r *Receipt) { r.Attempts[0].BrowserPageObserved = false }},
		{"skip", func(r *Receipt) { r.Attempts[0].Status = "skipped" }},
		{"retry", func(r *Receipt) { r.Attempts[0].Retry = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, _ := json.Marshal(r)
			var next Receipt
			if err := json.Unmarshal(data, &next); err != nil {
				t.Fatal(err)
			}
			tc.change(&next)
			if err := ValidateExecutionCoverage(required, []Receipt{next}); err == nil {
				t.Fatal("insufficient execution coverage accepted")
			}
		})
	}
	if err := ValidateExecutionCoverage(required, []Receipt{r, r}); err == nil {
		t.Fatal("duplicate run double counted")
	}
	other := r
	other.RunID = "run-two"
	other.Binding.Round = 2
	if err := ValidateExecutionCoverage(required, []Receipt{r, other}); err == nil {
		t.Fatal("incompatible rounds merged")
	}
}
