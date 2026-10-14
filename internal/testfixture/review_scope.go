package testfixture

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ReviewPlan upgrades a fixture through the production scope resolver supplied
// by its owning package. The generic callback avoids a review/testfixture cycle.
func ReviewPlan[T any](t *testing.T, root string, state, plan map[string]any, resolve func(string, map[string]any, int, string) (T, error)) {
	t.Helper()
	ReviewProject(t, root, state, plan)
	round := 0
	switch value := plan["review_round"].(type) {
	case int:
		round = value
	case float64:
		round = int(value)
	}
	workspace, _ := plan["verification_artifact_workspace"].(string)
	scope, err := resolve(root, state, round, workspace)
	if err != nil {
		t.Fatal(err)
	}
	ScopePlan(t, plan, scope)
	Write(t, root, ".claude/loop-state.json", state)
}

// BindingPlan provides real scope provenance in a separate disposable project
// for tests of the dispatch binding alone, without editing their source root.
func BindingPlan[T any](t *testing.T, state, plan map[string]any, resolve func(string, map[string]any, int, string) (T, error)) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var isolated map[string]any
	if err := json.Unmarshal(data, &isolated); err != nil {
		t.Fatal(err)
	}
	bound, _ := isolated["bound_req"].(map[string]any)
	delete(bound, "workspace")
	ReviewPlan(t, t.TempDir(), isolated, plan, resolve)
}

func ReviewPlanFile[T any](t *testing.T, root string, plan map[string]any, resolve func(string, map[string]any, int, string) (T, error)) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	ReviewPlan(t, root, state, plan, resolve)
}

// HistoricalPlan fills schema-required authoring fields in read-only fixtures;
// it keeps 1.0 so the board tests continue exercising historical reads.
func HistoricalPlan(t *testing.T, data []byte) []byte {
	t.Helper()
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]any{"baseline_generation": 1, "frozen_subjects": []any{}, "claims": []any{}, "assignments": []any{}, "e2e_coverage_state": "not_applicable", "dispatch_capacity_policy": "coverage_complete", "created_by": "fixture", "created_at": "2026-10-14T00:00:00Z"} {
		if plan[key] == nil {
			plan[key] = value
		}
	}
	if len(AnySlice(plan["frozen_subjects"])) == 0 {
		plan["frozen_subjects"] = []any{map[string]any{"path": "internal/example/service.go", "sha256": Digest([]byte("historical fixture"))}}
	}
	if len(AnySlice(plan["claims"])) == 0 {
		plan["claims"] = []any{map[string]any{"claim_id": "claim-historical-qa", "lens": "qa", "target": "internal/example", "assertion": "holds", "oracle": "inspection", "method": "review", "applicability": "required", "source_refs": []string{"REQ-FIXTURE"}}}
	}
	if len(AnySlice(plan["assignments"])) == 0 {
		plan["assignments"] = []any{map[string]any{"assignment_id": "assignment-historical-qa", "lens": "qa", "claim_ids": []string{"claim-historical-qa"}, "non_overlap_boundary": "historical QA", "execution_wave": "static"}}
	}
	completed, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return completed
}

// ReviewProject gives an existing disposable review fixture real Git, REQ,
// relation and scenario provenance. It preserves the fixture's subjects and
// runtime identities; it does not relax any production scope validator.
func ReviewProject(t *testing.T, root string, state, plan map[string]any) {
	t.Helper()
	_, fixtureSource, _, _ := runtime.Caller(0)
	sourceRoot, err := filepath.Abs(filepath.Join(filepath.Dir(fixtureSource), "../.."))
	if err != nil {
		t.Fatal(err)
	}
	fixtureRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if fixtureRoot == sourceRoot {
		t.Fatal("refusing to mutate the development repository as a fixture")
	}

	if _, err := os.Stat(filepath.Join(root, "docs/control/loop-definition.json")); os.IsNotExist(err) {
		_, file, _, _ := runtime.Caller(0)
		data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../docs/control/loop-definition.json"))
		if err != nil {
			t.Fatal(err)
		}
		Write(t, root, "docs/control/loop-definition.json", data)
	}
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil {
		bound = map[string]any{"id": "REQ-FIXTURE", "path": "docs/requirements/REQ-FIXTURE.md", "version": "1.0.0", "status": "locked", "approved_by": "fixture", "approved_at": "2026-10-14T00:00:00Z"}
		state["bound_req"] = bound
	}
	reqPath, _ := bound["path"].(string)
	if reqPath == "" {
		reqPath = "docs/requirements/REQ-FIXTURE.md"
		bound["path"] = reqPath
	}
	req, err := os.ReadFile(filepath.Join(root, reqPath))
	if os.IsNotExist(err) {
		req = Write(t, root, reqPath, "# Disposable review fixture\n")
	} else if err != nil {
		t.Fatal(err)
	}
	bound["sha256"] = Digest(req)
	if bound["id"] == nil {
		bound["id"] = "REQ-FIXTURE"
	}
	modules := []any{}
	cases, err := filepath.Glob(filepath.Join(root, "docs/design/prototypes/*/cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	browser := false
	for _, raw := range plan["claims"].([]any) {
		c := raw.(map[string]any)
		browser = browser || (c["lens"] == "e2e" && c["applicability"] == "required")
	}
	if browser && len(cases) == 0 {
		p := "docs/design/prototypes/fixture/cases.json"
		Write(t, root, p, map[string]any{"module": "fixture", "cases": []any{map[string]any{"id": "CASE-001", "title": "fixture flow", "required": true, "browser_required": true, "flow_refs": []string{"PATH-001"}}}})
		cases = []string{filepath.Join(root, p)}
	}
	paths := []string{"internal/", "server/", "web/", "docs/", "tests/", "src/", "e2e/", "evidence/", "s10/", "main.go", "go.mod", "go.sum"}
	generation := state["baseline"].(map[string]any)["generation"]
	documents, _ := state["documents"].([]any)
	lock := func(p string, data []byte) {
		for _, raw := range documents {
			d := raw.(map[string]any)
			if d["path"] == p {
				d["sha256"], d["status"], d["generation"] = Digest(data), "locked", generation
				return
			}
		}
		documents = append(documents, map[string]any{"id": "SCOPE-" + Digest([]byte(p))[:12], "kind": "design", "path": p, "version": "1.0.0", "sha256": Digest(data), "status": "locked", "generation": generation})
	}
	for i, absolute := range cases {
		data, err := os.ReadFile(absolute)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		module, _ := doc["module"].(string)
		if module == "" {
			module = filepath.Base(filepath.Dir(absolute))
		}
		rel, _ := filepath.Rel(root, absolute)
		rel = filepath.ToSlash(rel)
		owned := []string{"docs/design/prototypes/" + module + "/"}
		if i == 0 {
			owned = paths
		}
		modules = append(modules, map[string]any{"id": module, "owned_paths": owned, "cases_path": rel, "contract_refs": []any{}, "browser_applicability": "required"})
		lock(rel, data)
	}
	if len(modules) == 0 {
		modules = append(modules, map[string]any{"id": "fixture", "owned_paths": paths, "contract_refs": []any{}, "browser_applicability": "not_applicable", "applicability_refs": []string{reqPath}})
	}
	for _, raw := range modules {
		module := raw.(map[string]any)["id"].(string)
		ref := "docs/design/prototypes/" + module + "/"
		if !strings.Contains(string(req), ref) {
			req = append(req, []byte("\nFixture module: "+ref+"\n")...)
		}
	}
	Write(t, root, reqPath, req)
	bound["sha256"] = Digest(req)
	lock(reqPath, req)
	graphPath := "docs/architecture/module-relations.json"
	graph := Write(t, root, graphPath, map[string]any{"schema_version": "1.0.0", "modules": modules, "resources": []any{}})
	lock(graphPath, graph)
	state["documents"] = documents
	if err := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Run(); err != nil {
		Git(t, root, "init", "-b", "fixture-dev")
	}
	exclude := Git(t, root, "rev-parse", "--git-path", "info/exclude")
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(root, exclude)
	}
	if err := os.MkdirAll(filepath.Dir(exclude), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\n# Disposable review authoring/control artifacts\n/.claude/\n/plan*.json\n/review-plan*.json\n/review-result*.json\n/result*.json\n/manifest*.json\n/e2e-workspace/\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Only fixture product/spec inputs are staged; Runtime and authoring plans
	// must not become incidental product changes in subsequent scope checks.
	fixtureInputs := []string{"docs", "internal", "server", "web", "tests", "src", "e2e", "evidence", "s10", "main.go", "go.mod", "go.sum", reqPath}
	for _, raw := range documents {
		if p, ok := raw.(map[string]any)["path"].(string); ok && !strings.HasPrefix(p, ".claude/") {
			fixtureInputs = append(fixtureInputs, p)
		}
	}
	for _, p := range uniqueFixtureStrings(fixtureInputs) {
		if _, err := os.Stat(filepath.Join(root, p)); err == nil {
			Git(t, root, "add", "--", p)
		}
	}
	if err := exec.Command("git", "-C", root, "diff", "--cached", "--quiet").Run(); err != nil {
		Git(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "review fixture provenance")
	}
	head := Git(t, root, "rev-parse", "HEAD")
	branch := Git(t, root, "symbolic-ref", "--short", "HEAD")
	binding, _ := bound["workspace"].(map[string]any)
	if binding == nil {
		binding = map[string]any{}
		bound["workspace"] = binding
	}
	binding["project_root"], binding["dev_branch"], binding["release_upstream"] = root, branch, branch
	if value, _ := binding["bound_commit"].(string); value == "" {
		binding["bound_commit"] = head
	}
}

// ScopePlan retains the behavior under test while attaching scope calculated
// by the real resolver. Non-browser fixtures have an independent QA assessment;
// browser fixtures retain the full module-qualified CASE/PATH denominator.
func ScopePlan(t *testing.T, plan map[string]any, resolved any) {
	t.Helper()
	data, err := json.Marshal(resolved)
	if err != nil {
		t.Fatal(err)
	}
	var scope map[string]any
	if err := json.Unmarshal(data, &scope); err != nil {
		t.Fatal(err)
	}
	if unresolved, _ := scope["unresolved_inputs"].([]any); len(unresolved) != 0 {
		t.Fatalf("review fixture has unresolved scope: %v", unresolved)
	}
	plan["schema_version"], plan["module_scope"] = "1.1.0", scope
	if plan["created_by"] == nil {
		plan["created_by"] = "fixture"
	}
	if plan["created_at"] == nil {
		plan["created_at"] = "2026-10-14T00:00:00Z"
	}
	claims := plan["claims"].([]any)
	assignments := plan["assignments"].([]any)
	for _, raw := range claims {
		c := raw.(map[string]any)
		if c["applicability"] == "not_applicable" && c["na_checklist_id"] == nil {
			c["na_checklist_id"] = "fixture-applicability"
		}
	}
	owned := func(id string, lens, wave string) bool {
		for _, raw := range assignments {
			a := raw.(map[string]any)
			if a["lens"] == lens && a["execution_wave"] == wave {
				for _, claim := range Strings(a["claim_ids"]) {
					if claim == id {
						return true
					}
				}
			}
		}
		return false
	}
	dispositions := []any{}
	for _, raw := range scope["candidates"].([]any) {
		candidate := raw.(map[string]any)
		module := candidate["module_id"].(string)
		d := map[string]any{"module_id": module, "rationale": "independently reviewed disposable fixture scope", "evidence_refs": []any{}, "verification_claim_ids": []string{}, "assessment_claim_ids": []string{}}
		refs, _ := candidate["requirement_refs"].([]any)
		if len(refs) > 0 {
			d["action"] = "browser_regression"
			ids := []string{}
			for _, raw := range claims {
				c := raw.(map[string]any)
				id := c["claim_id"].(string)
				if c["lens"] == "e2e" && c["applicability"] == "required" && owned(id, "e2e", "behavior") {
					c["source_refs"] = uniqueFixtureStrings(append(Strings(c["source_refs"]), "module:"+module))
					c["scenario_refs"] = uniqueFixtureRefs(append(AnySlice(c["scenario_refs"]), refs...))
					ids = append(ids, id)
				}
			}
			d["verification_claim_ids"] = ids
			for _, raw := range AnySlice(plan["e2e_assets"]) {
				a := raw.(map[string]any)
				for _, rawRef := range refs {
					r := rawRef.(map[string]any)
					if a["case_ref"] == r["case_id"] {
						a["module_id"], a["cases_path"] = module, r["cases_path"]
					}
				}
			}
		} else {
			d["action"] = "no_behavior_impact"
			for _, rawRef := range scope["input_refs"].([]any) {
				r := rawRef.(map[string]any)
				if r["path"] == "docs/architecture/module-relations.json" && r["source_kind"] == "git_tree" && r["present"] == true {
					d["evidence_refs"] = []any{r}
					break
				}
			}
			var assessment map[string]any
			for _, raw := range claims {
				c := raw.(map[string]any)
				if c["lens"] == "qa" && c["applicability"] == "required" && (c["focus_key"] == nil || c["focus_key"] == "" || c["focus_key"] == "module-scope:"+module) && owned(c["claim_id"].(string), "qa", "static") {
					assessment = c
					break
				}
			}
			if assessment == nil {
				id := "claim-fixture-scope-" + module
				assessment = map[string]any{"claim_id": id, "lens": "qa", "target": "module:" + module, "assertion": "fixture changes have no browser behavior", "oracle": "pinned module declaration contains only non-browser fixture inputs", "method": "independent scope assessment", "applicability": "required", "source_refs": []string{}}
				claims = append(claims, assessment)
				assignments = append(assignments, map[string]any{"assignment_id": "assignment-fixture-scope-" + module, "lens": "qa", "claim_ids": []string{id}, "non_overlap_boundary": "independent fixture scope assessment", "execution_wave": "static"})
			}
			assessment["focus_key"] = "module-scope:" + module
			assessment["source_refs"] = uniqueFixtureStrings(append(Strings(assessment["source_refs"]), "module:"+module))
			d["assessment_claim_ids"] = []string{assessment["claim_id"].(string)}
		}
		dispositions = append(dispositions, d)
	}
	scope["dispositions"] = dispositions
	plan["claims"], plan["assignments"] = claims, assignments
}

func Strings(value any) []string {
	if values, ok := value.([]string); ok {
		return values
	}
	var values []string
	for _, raw := range AnySlice(value) {
		if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
			values = append(values, s)
		}
	}
	return values
}

func AnySlice(value any) []any {
	values, _ := value.([]any)
	return values
}

func uniqueFixtureRefs(refs []any) []any {
	out := []any{}
	seen := map[string]bool{}
	for _, ref := range refs {
		data, err := json.Marshal(ref)
		if err != nil {
			panic(err)
		}
		if !seen[string(data)] {
			out = append(out, ref)
			seen[string(data)] = true
		}
	}
	return out
}

func uniqueFixtureStrings(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		if !seen[value] {
			out = append(out, value)
			seen[value] = true
		}
	}
	return out
}
