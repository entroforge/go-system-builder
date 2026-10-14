package e2erun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

func exerciseFormalFailures(t *testing.T, ctx context.Context, root string, profile Profile, config, spec string, formal FormalRequest) {
	t.Helper()
	// Valid annotations, a PASS, a user-authored step title and stdout text
	// together still cannot manufacture a browser API observation.
	unit := `const {test,expect}=require('playwright/test');
test('unit masquerading as browser', {annotation:{type:'gsb.case',description:JSON.stringify({module_ref:'docs/design/prototypes/settings/cases.json',case_id:'CASE-001',oracle_ref:'/oracle/visible/0',persona:'owner',data_profile:'default'})}}, async()=>{
 await test.step('Create page', async()=>{expect(true).toBeTruthy()});
 console.log(JSON.stringify({type:'api_step',step:{category:'pw:api',title:'Create page'}}));
});`
	for _, candidate := range []struct {
		name, code, outcome string
		page                bool
	}{
		{"unit_without_browser", unit, "pass", false},
		{"browser_failure", strings.Replace(spec, "toHaveText('Saved')", "toHaveText('Wrong')", 1), "fail", true},
	} {
		for path, data := range map[string]string{"settings.spec.cjs": candidate.code, "playwright.config.cjs": config} {
			if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
		profileData, _ := json.Marshal(profile)
		if err := os.WriteFile(filepath.Join(root, ProfilePath), profileData, 0600); err != nil {
			t.Fatal(err)
		}
		testGit(t, root, "add", ".")
		testGit(t, root, "commit", "-qm", candidate.name)
		candidateView, err := fileview.New(root, "refs/heads/development", []fileview.Rule{{Path: ".", Source: "git_tree"}})
		if err != nil {
			t.Fatal(err)
		}
		formal.Files, formal.Binding.SourceCommit = candidateView, candidateView.Commit
		formal.Mode, formal.Collection, formal.SelectedTestIDs = "collection", nil, nil
		observed, err := RunFormal(ctx, formal)
		if err != nil {
			t.Fatalf("collect %s: %v\n%s", candidate.name, err, observed.Bytes())
		}
		collected, _ := observed.Receipt()
		formal.Mode, formal.Collection, formal.SelectedTestIDs = "execution", &collected, []string{collected.Tests[0].ID}
		observed, err = RunFormal(ctx, formal)
		if err != nil {
			t.Fatalf("execute %s: %v\n%s", candidate.name, err, observed.Bytes())
		}
		r, ok := observed.Receipt()
		if !ok || !r.Complete || r.Outcome != candidate.outcome || len(r.Attempts) != 1 || r.Attempts[0].BrowserPageObserved != candidate.page {
			t.Fatalf("%s observation: %s", candidate.name, observed.Bytes())
		}
		required := []Requirement{{ClaimID: "claim-browser", CaseRef: r.Tests[0].CaseRefs[0], Project: r.Tests[0].Project, Browser: r.Tests[0].Browser}}
		if err := ValidateExecutionCoverage(required, []Receipt{r}); err == nil {
			t.Fatalf("%s incorrectly satisfied browser PASS", candidate.name)
		}
		if directory := os.Getenv("GSB_E2E_RECEIPT_DIR"); directory != "" {
			if err := os.WriteFile(filepath.Join(directory, candidate.name+".json"), observed.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, change := range []func(*Receipt){
			func(r *Receipt) { r.Attempts[0].BrowserPageObserved = !r.Attempts[0].BrowserPageObserved },
			func(r *Receipt) { r.Tests[0].CaseRefs[0].CaseID = "CASE-FORGED" },
		} {
			var forged Receipt
			if err := json.Unmarshal(observed.Bytes(), &forged); err != nil {
				t.Fatal(err)
			}
			change(&forged)
			data, _ := json.Marshal(forged)
			if _, err := DecodeReceipt(data); err == nil {
				t.Fatal("receipt summary tampering was not detected against raw events")
			}
		}
	}
}

func TestCollectionCoverageRequiresExactEligibleTupleSet(t *testing.T) {
	test := fixtureTest()
	required := []Requirement{{ClaimID: "claim", CaseRef: test.CaseRefs[0], Project: test.Project, Browser: test.Browser}}
	collection := Receipt{Mode: "collection", Complete: true, Outcome: "pass", Tests: []Test{test}}
	if err := ValidateCollectionCoverage(required, collection, []string{test.ID}); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{nil, {test.ID, test.ID}, {"missing"}} {
		if err := ValidateCollectionCoverage(required, collection, ids); err == nil {
			t.Fatalf("bad selection accepted: %v", ids)
		}
	}
	for _, mutate := range []func(*Receipt){
		func(r *Receipt) { r.Tests[0].Browser = "webkit" },
		func(r *Receipt) { r.Tests[0].CaseRefs[0].ModuleRef = "docs/design/prototypes/contacts/cases.json" },
		func(r *Receipt) { r.Tests[0].CaseRefs[0].OracleRef = "/oracle/visible/1" },
		func(r *Receipt) { r.Tests[0].CaseRefs = append(r.Tests[0].CaseRefs, r.Tests[0].CaseRefs[0]) },
		func(r *Receipt) { r.Tests[0].ExpectedStatus = "skipped" },
	} {
		data, _ := json.Marshal(collection)
		var next Receipt
		if err := json.Unmarshal(data, &next); err != nil {
			t.Fatal(err)
		}
		mutate(&next)
		if err := ValidateCollectionCoverage(required, next, []string{test.ID}); err == nil {
			t.Fatal("incompatible/ambiguous selection covered required browser tuple")
		}
	}
}
