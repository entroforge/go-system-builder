package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

var (
	moduleTruthHeader = regexp.MustCompile(`(?m)^>\s*Module current truth:\s*(.*)$`)
	moduleName        = regexp.MustCompile("`([a-z0-9]+(?:-[a-z0-9]+)*)`")
	moduleLink        = regexp.MustCompile(`\[[^\]]*\]\(([^\s)]+)\)`)
)

// knownE2ECases projects a bounded lower bound, not a semantic impact graph.
// Only the bound REQ and the current frozen TASKs' explicit module declarations
// select modules. Reading a document for background never assigns its CASEs.
func knownE2ECases(ctx context.Context, root string, state map[string]any, plan *Plan) ([]e2eScenario, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if root == "" {
		return nil, nil
	}
	read, err := planAssetReader(ctx, root, plan, state)
	if err != nil {
		return nil, err
	}
	modules := map[string]bool{}
	bound, _ := state["bound_req"].(map[string]any)
	if p := stringField(bound["path"]); p != "" {
		body, err := read(p)
		// Historical disk-mode runtimes did not require the REQ to be present
		// at registration. Preserve that recovery boundary, without interpreting
		// absent input as permission to scan the entire project.
		if err != nil && !(plan.ProductBaseline == nil && os.IsNotExist(err)) {
			return nil, fmt.Errorf("S7_CASE_SOURCE: Main must restore bound REQ %s: %w", p, err)
		}
		addModulePaths(modules, p, string(body))
	}
	var tasks []taskDoc
	if plan.ProductBaseline != nil {
		for _, t := range plan.ProductBaseline.Tasks {
			tasks = append(tasks, taskDoc{id: t.ID, path: t.Path, sha256: t.SHA256})
		}
	} else {
		for _, raw := range stateDocuments(state) {
			doc, _ := raw.(map[string]any)
			if doc["kind"] == "task" && intField(doc["generation"]) == baselineGeneration(state) {
				tasks = append(tasks, taskDoc{id: stringField(doc["id"]), path: stringField(doc["path"]), sha256: stringField(doc["sha256"])})
			}
		}
	}
	for _, task := range tasks {
		body, err := read(task.path)
		if err != nil {
			return nil, fmt.Errorf("S7_CASE_SOURCE: Main must restore frozen TASK %s: %w", task.path, err)
		}
		if task.sha256 == "" || sha256Of(body) != task.sha256 {
			return nil, fmt.Errorf("S7_CASE_SOURCE: frozen TASK %s changed; Main must restore its original source", task.path)
		}
		text := string(body)
		if h := moduleTruthHeader.FindStringSubmatch(text); len(h) == 2 {
			addModulePaths(modules, task.path, h[1])
		}
		// Read the existing Module Impact section only. Manifest and other
		// prose may link unrelated modules without declaring them impacted.
		active := false
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "#") {
				active = strings.HasSuffix(strings.TrimSpace(line), " Module Impact")
				continue
			}
			if active && strings.HasPrefix(strings.TrimSpace(line), "Modules touched:") {
				addModulePaths(modules, task.path, line)
				for _, match := range moduleName.FindAllStringSubmatch(line, -1) {
					modules[match[1]] = true
				}
			}
		}
	}
	var names []string
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	var cases []e2eScenario
	for _, name := range names {
		p := "docs/design/prototypes/" + name + "/cases.json"
		body, err := read(p)
		if err != nil {
			return nil, fmt.Errorf("S7_CASE_SOURCE: Main must restore declared module CASE source %s: %w", p, err)
		}
		var doc e2eCasesDocument
		if err := json.Unmarshal(body, &doc); err != nil {
			return nil, fmt.Errorf("S7_CASE_SOURCE: decode %s: %w", p, err)
		}
		if len(doc.Cases) == 0 {
			return nil, fmt.Errorf("S7_CASE_SOURCE: declared module %s has no CASE list; Main must restore its original scenario source", name)
		}
		seen := map[string]bool{}
		for _, c := range doc.Cases {
			if c.ID == "" || seen[c.ID] {
				return nil, fmt.Errorf("S7_CASE_SOURCE: empty or duplicate CASE %q in %s; Main must correct the original scenario source", c.ID, p)
			}
			seen[c.ID] = true
			if c.Required && c.BrowserRequired {
				cases = append(cases, e2eScenario{CasesPath: p, Module: name, ID: c.ID, Title: c.Title, Polarity: c.Polarity, FlowRefs: c.FlowRefs, Oracle: c.Oracle, Required: true, BrowserRequired: true})
			}
		}
	}
	return cases, nil
}

func addModulePaths(modules map[string]bool, source, text string) {
	for _, match := range e2eModuleRefPattern.FindAllStringSubmatch(text, -1) {
		modules[match[1]] = true
	}
	for _, link := range moduleLink.FindAllStringSubmatch(text, -1) {
		p, _, _ := strings.Cut(link[1], "#")
		// Root-relative source names are already handled above. Relative
		// Markdown links resolve against their original authoring document.
		if strings.HasPrefix(p, ".") {
			for _, match := range e2eModuleRefPattern.FindAllStringSubmatch(path.Join(path.Dir(source), p)+"/", -1) {
				modules[match[1]] = true
			}
		}
	}
}

func validateKnownE2ECaseCoverage(ctx context.Context, root string, state map[string]any, plan *Plan) error {
	cases, err := knownE2ECases(ctx, root, state, plan)
	if err != nil {
		return err
	}
	// A declared required browser CASE cannot be routed to a non-browser
	// responsibility. Missing historical fields remain unchanged on load.
	browserRefs := map[string]bool{}
	for _, c := range cases {
		browserRefs[scenarioRef(c)] = true
	}
	claimMap := planClaimMap(plan)
	for _, assignment := range plan.Assignments {
		if assignment.BehaviorSurface != "non_browser" {
			continue
		}
		for _, id := range assignment.ClaimIDs {
			for _, ref := range claimMap[id].SourceRefs {
				if browserRefs[ref] {
					return &PlanAnswerError{fmt.Errorf("S7_CASE_COVERAGE: assignment %s declares non_browser but owns required browser CASE %s; restore browser applicability in the original Plan", assignment.AssignmentID, ref)}
				}
			}
		}
	}
	covered := map[string]bool{}
	for _, claim := range plan.Claims {
		if claim.Lens == "e2e" {
			for _, ref := range claim.SourceRefs {
				covered[ref] = true
			}
		}
	}
	var missing []string
	for _, c := range cases {
		if !covered[scenarioRef(c)] {
			missing = append(missing, scenarioRef(c))
		}
	}
	if len(missing) > 0 {
		return &PlanAnswerError{fmt.Errorf("S7_CASE_COVERAGE: known required browser CASEs have no explicit E2E disposition: %s; Main must assign required Claims or justify each CASE with the original N/A rationale/checklist in this ReviewPlan", strings.Join(missing, ", "))}
	}
	return nil
}
