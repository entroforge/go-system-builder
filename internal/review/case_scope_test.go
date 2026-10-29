package review

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
)

func putCaseScopeFile(t *testing.T, root, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, p), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func caseScopeREQ(t *testing.T, root, body string) map[string]any {
	t.Helper()
	p := "docs/requirements/REQ-TEST.md"
	putCaseScopeFile(t, root, p, body)
	return map[string]any{"path": p, "metadata": map[string]any{"ui_impact": "changed"}}
}

func knownCaseFixture(t *testing.T) (string, map[string]any, *Plan, string) {
	t.Helper()
	root := t.TempDir()
	state := baseVerificationState()
	for k, v := range caseScopeREQ(t, root, "[Shareholder](../design/prototypes/shareholder/README.md)\n") {
		state["bound_req"].(map[string]any)[k] = v
	}
	task := "docs/dev/tasks/TASK-101.md"
	body := "# Task: TASK-101\n> Status: complete\n> Module current truth: `docs/design/prototypes/shareholder/`\n\n## 2. Document Manifest\n\n| 1 | scenario | unrelated | docs/design/prototypes/unrelated/cases.json | CASE-001 | background | optional |\n\n### 3.1 Module Impact\n\nModules touched: `shareholder`, `certificate`. Certificate generation consumes changed shareholder data.\n\nUnrelated background: [manual](../../design/prototypes/unrelated/cases.json). This is not a touched module.\n\n## 4. Scope\n"
	putCaseScopeFile(t, root, task, body)
	state["documents"] = []any{map[string]any{"id": "TASK-101", "kind": "task", "path": task, "version": "v1.0.0", "sha256": sha256Of([]byte(body)), "status": "locked", "generation": 1}}
	for _, m := range []string{"shareholder", "certificate", "unrelated"} {
		putCaseScopeFile(t, root, "docs/design/prototypes/"+m+"/cases.json", `{"module":"`+m+`","cases":[{"id":"CASE-001","title":"flow","required":true,"browser_required":true}]}`)
	}
	p := writePlanFile(t, root)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var plan Plan
	if err = json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	plan.Claims[0].SourceRefs = append(plan.Claims[0].SourceRefs, "TASK-101", task)
	return root, state, &plan, p
}

func writeCaseScopePlan(t *testing.T, p string, plan *Plan) {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func addCaseDispositions(plan *Plan) {
	for _, m := range []string{"shareholder", "certificate"} {
		c := plan.Claims[len(plan.Claims)-1]
		c.ClaimID = "claim-e2e-na-" + m
		c.Target = "docs/design/prototypes/" + m + "/cases.json#CASE-001"
		c.SourceRefs = []string{c.Target}
		c.NARationale = "This specific entry is unchanged by the delivery; original contract evidence limits the change to internal fields."
		c.NAChecklistID = "na-checklist-template-1#" + m
		plan.Claims = append(plan.Claims, c)
	}
}

func TestKnownCASECoverageRejectsOmissionAtOriginalRegistration(t *testing.T) {
	root, state, plan, p := knownCaseFixture(t)
	sp, jp := writeState(t, root, state)
	before, _ := os.ReadFile(sp)
	writeCaseScopePlan(t, p, plan)
	_, err := RegisterPlan(root, sp, jp, PlanRequest{ExpectedRevision: 1, PlanPath: p})
	if err == nil || !strings.Contains(err.Error(), "S7_CASE_COVERAGE") || !strings.Contains(err.Error(), "certificate/cases.json#CASE-001") {
		t.Fatalf("known consumer omitted: %v", err)
	}
	after, _ := os.ReadFile(sp)
	if !bytes.Equal(before, after) {
		t.Fatal("rejected answer changed Runtime")
	}
	if data, err := os.ReadFile(jp); err == nil && len(bytes.TrimSpace(data)) > 0 {
		t.Fatal("rejected answer committed journal")
	}
	// Same answer, existing per-CASE N/A fields: no new operation or report.
	addCaseDispositions(plan)
	writeCaseScopePlan(t, p, plan)
	if _, err = RegisterPlan(root, sp, jp, PlanRequest{ExpectedRevision: 1, PlanPath: p}); err != nil {
		t.Fatalf("explicit dispositions should register: %v", err)
	}
}

func TestKnownCASECoverageExactIdentityAndDisposition(t *testing.T) {
	for _, mode := range []string{"bare", "one-module-only", "asset-only", "qa-only", "required", "not-applicable"} {
		t.Run(mode, func(t *testing.T) {
			root, state, plan, _ := knownCaseFixture(t)
			addCaseDispositions(plan)
			switch mode {
			case "bare":
				for i := 4; i < len(plan.Claims); i++ {
					plan.Claims[i].SourceRefs = []string{"CASE-001"}
				}
			case "one-module-only":
				plan.Claims = plan.Claims[:len(plan.Claims)-1]
			case "asset-only":
				plan.E2EAssets = []E2EAsset{{CaseRef: plan.Claims[len(plan.Claims)-1].Target}}
				plan.Claims = plan.Claims[:len(plan.Claims)-1]
			case "qa-only":
				plan.Claims[len(plan.Claims)-1].Lens = "qa"
			case "required":
				for i := 4; i < len(plan.Claims); i++ {
					plan.Claims[i].Applicability = "required"
				}
			}
			err := validateKnownE2ECaseCoverage(context.Background(), root, state, plan)
			wantPass := mode == "required" || mode == "not-applicable"
			if (err == nil) != wantPass {
				t.Fatalf("mode=%s: %v", mode, err)
			}
		})
	}
}

func TestKnownCASEScopeStaysWithinExplicitImpact(t *testing.T) {
	root, state, plan, _ := knownCaseFixture(t)
	// Even a missing historical TASK must not enlarge or block current scope.
	state["documents"] = append(state["documents"].([]any), map[string]any{"kind": "task", "generation": 0, "path": "docs/dev/tasks/TASK-OLD.md"})
	inventory, notes := discoverE2EInventory(root, state, plan)
	if len(notes) != 0 || len(inventory.Cases) != 2 {
		t.Fatalf("wrong source scope: %+v %v", inventory, notes)
	}
	for _, c := range inventory.Cases {
		if c.Module == "unrelated" {
			t.Fatal("optional Manifest reading expanded execution scope")
		}
	}
	// No declared impact is unknown, not a request to sweep all modules.
	inventory, notes = discoverE2EInventory(root, map[string]any{})
	if len(notes) != 0 || len(inventory.Cases) != 0 {
		t.Fatalf("unbounded fallback: %+v %v", inventory, notes)
	}
	state["bound_req"].(map[string]any)["metadata"].(map[string]any)["ui_impact"] = "none"
	draft, _ := DraftPlanForRoot(root, state, 1)
	if draft == nil || countLensAssignments(draft, "e2e") != 2 || draft.E2ECoverageState == "not_applicable" {
		t.Fatal("generic ui_impact=none erased known CASEs")
	}
}

func TestKnownCASEScopeRejectsBrokenSources(t *testing.T) {
	for _, mode := range []string{"task-drift", "task-missing", "cases-missing", "duplicate", "decode", "empty", "missing-list"} {
		t.Run(mode, func(t *testing.T) {
			root, state, plan, _ := knownCaseFixture(t)
			task := "docs/dev/tasks/TASK-101.md"
			cases := "docs/design/prototypes/certificate/cases.json"
			switch mode {
			case "task-drift":
				putCaseScopeFile(t, root, task, "changed impact")
			case "task-missing":
				if err := os.Remove(filepath.Join(root, task)); err != nil {
					t.Fatal(err)
				}
			case "cases-missing":
				if err := os.Remove(filepath.Join(root, cases)); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				putCaseScopeFile(t, root, cases, `{"cases":[{"id":"CASE-001"},{"id":"CASE-001"}]}`)
			case "empty":
				putCaseScopeFile(t, root, cases, `{"cases":[]}`)
			case "missing-list":
				putCaseScopeFile(t, root, cases, `{}`)
			case "decode":
				putCaseScopeFile(t, root, cases, "{")
			}
			if _, err := knownE2ECases(nil, root, state, plan); err == nil || !strings.Contains(err.Error(), "S7_CASE_SOURCE") {
				t.Fatalf("broken source silently became empty scope: %v", err)
			}
		})
	}
}

func TestKnownCASEScopeUsesOriginalPinnedSources(t *testing.T) {
	root, state, plan, _ := knownCaseFixture(t)
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "original source")
	declaration := `{"file_sources":[{"path":".","source":"git_tree"}]}`
	putCaseScopeFile(t, root, "definition.json", declaration)
	state["definition"] = map[string]any{"path": "definition.json", "sha256": sha256Of([]byte(declaration))}
	task := state["documents"].([]any)[0].(map[string]any)
	plan.ProductBaseline = &buildsurface.Snapshot{FinalCommit: git("rev-parse", "HEAD"), Tasks: []buildsurface.Task{{ID: "TASK-101", Path: stringField(task["path"]), SHA256: stringField(task["sha256"])}}}
	putCaseScopeFile(t, root, stringField(task["path"]), "unapproved changed scope")
	putCaseScopeFile(t, root, "docs/design/prototypes/certificate/cases.json", `{"cases":[]}`)
	cases, err := knownE2ECases(nil, root, state, plan)
	if err != nil || len(cases) != 2 {
		t.Fatalf("read worktree instead of original approved inputs: %+v %v", cases, err)
	}
	putCaseScopeFile(t, root, "definition.json", `{}`)
	if _, err := knownE2ECases(nil, root, state, plan); err == nil {
		t.Fatal("drifted source declaration accepted")
	}
}

func TestKnownCASECoverageCannotBeDroppedByOriginalRevision(t *testing.T) {
	root, state, plan, p := knownCaseFixture(t)
	source := revisionSourceState()
	state["evidence"] = source["evidence"]
	addCaseDispositions(plan)
	last := len(plan.Claims) - 1
	plan.Claims[last].SourceRefs = append(plan.Claims[last].SourceRefs, "review-result-qa-1")
	consumer := plan.Claims[last]
	writeCaseScopePlan(t, p, plan)
	sp, jp := writeState(t, root, state)
	snap, err := RegisterPlan(root, sp, jp, PlanRequest{ExpectedRevision: 1, PlanPath: p})
	if err != nil {
		t.Fatal(err)
	}
	original, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}
	plan.Claims = plan.Claims[:last]
	// This removal satisfies the existing controlled-revision source/surface
	// rules; the new source denominator, not an unrelated gate, must reject it.
	if _, err := diffClaims(original, plan, "review-result-qa-1", "docs/design/prototypes/certificate"); err != nil {
		t.Fatal(err)
	}
	writeCaseScopePlan(t, p, plan)
	before, _ := os.ReadFile(sp)
	request := ReviseRequest{ExpectedRevision: snap.Revision, PlanPath: p, SourceRef: "review-result-qa-1", AffectedSurface: "docs/design/prototypes/certificate"}
	_, err = RevisePlan(root, sp, jp, request)
	if err == nil || !strings.Contains(err.Error(), "S7_CASE_COVERAGE") {
		t.Fatalf("revision dropped consumer CASE: %v", err)
	}
	after, _ := os.ReadFile(sp)
	if !bytes.Equal(before, after) {
		t.Fatal("rejected revision changed Runtime")
	}
	// Restore the explicit disposition, then revise its actual oracle on the
	// same original revision path. No new command or revision slot is needed.
	consumer.Oracle = "current observed contract evidence proves this exact entry unchanged"
	plan.Claims = append(plan.Claims, consumer)
	writeCaseScopePlan(t, p, plan)
	revised, err := RevisePlan(root, sp, jp, request)
	if err != nil {
		t.Fatal(err)
	}
	if ptr := PlanPointerFromState(revised.State); ptr == nil || ptr.Revision != 2 {
		t.Fatal("valid original revision was not committed")
	}
}

func TestKnownCASECoveragePreservesParallelAssignmentsAndExistingAssets(t *testing.T) {
	root, state, plan, p := knownCaseFixture(t)
	addCaseDispositions(plan)
	plan.E2ECoverageState = "regression_available"
	script := "web/e2e/existing.spec.ts"
	putCaseScopeFile(t, root, script, "// existing test implementation\n")
	for i := 4; i < len(plan.Claims); i++ {
		c := &plan.Claims[i]
		c.Applicability = "required"
		c.NARationale = ""
		c.NAChecklistID = ""
		c.FocusKey = c.Target
		plan.Assignments = append(plan.Assignments, PlanAssignment{AssignmentID: "assignment-" + c.ClaimID, Lens: "e2e", BehaviorSurface: "browser", ClaimIDs: []string{c.ClaimID}, FocusKeys: []string{c.Target}, ExecutionWave: "behavior", NonOverlapBoundary: "owns exactly " + c.Target})
		plan.E2EAssets = append(plan.E2EAssets, E2EAsset{CaseRef: c.Target, Path: script, SelectorRef: "existing selector contract", RouteRef: "/existing-entry", Environment: "original test environment"})
	}
	writeCaseScopePlan(t, p, plan)
	sp, jp := writeState(t, root, state)
	snap, err := RegisterPlan(root, sp, jp, PlanRequest{ExpectedRevision: 1, PlanPath: p})
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}
	if len(accepted.E2EAssets) != 2 || countLensAssignments(accepted, "e2e") != 2 {
		t.Fatal("scope check collapsed independent assignments or required rebuilding existing assets")
	}
	for _, asset := range accepted.E2EAssets {
		if asset.SHA256 == "" || asset.AssetID == "" {
			t.Fatal("original machine-field preparation was lost")
		}
	}
}
