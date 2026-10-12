package review

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/testfixture"
)

func scopeTestGraph() *ModuleRelations {
	return &ModuleRelations{SchemaVersion: "1.0.0", Modules: []ModuleDeclaration{{ID: "customers", OwnedPaths: []string{"server/customers/", "web/customers/"}, CasesPath: "docs/design/prototypes/customers/cases.json", ContractRefs: []string{}, BrowserApplicability: "required"}, {ID: "orders", OwnedPaths: []string{"server/orders/"}, CasesPath: "docs/design/prototypes/orders/cases.json", ContractRefs: []string{}, BrowserApplicability: "required"}}, Resources: []ModuleResource{{ID: "customer-status", Paths: []string{"server/customers/status.go"}, ProviderModuleIDs: []string{"customers"}, ConsumerModuleIDs: []string{"orders"}, SourceRefs: []string{"docs/dev/contracts/SYNC-001.md"}}}}
}
func scopeWrite(t *testing.T, root, p string, v any) []byte {
	t.Helper()
	var data []byte
	switch value := v.(type) {
	case []byte:
		data = value
	case string:
		data = []byte(value)
	default:
		var e error
		data, e = json.MarshalIndent(v, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
	}
	full := filepath.Join(root, p)
	if e := os.MkdirAll(filepath.Dir(full), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(full, data, 0644); e != nil {
		t.Fatal(e)
	}
	return data
}
func scopeRunGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	b, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %s %v", args, b, e)
	}
	return strings.TrimSpace(string(b))
}
func scopeFixture(t *testing.T) (string, map[string]any, *Plan, string, string) {
	t.Helper()
	root := t.TempDir()
	state := baseVerificationState()
	sp, jp := writeState(t, root, state)
	graphBytes, _ := json.Marshal(scopeTestGraph())
	sp, jp = testfixture.RelatedModules(t, root, state, graphBytes)
	plan, notes := DraftPlanForRoot(root, state, 1)
	if plan == nil {
		t.Fatalf("draft: %v", notes)
	}
	if len(plan.ModuleScope.UnresolvedInputs) > 0 {
		t.Fatalf("fixture scope unresolved: %v", plan.ModuleScope.UnresolvedInputs)
	}
	// Complete authoring, with one explicit DV Claim as no TASK is installed.
	for i := range plan.Claims {
		if len(plan.Claims[i].SourceRefs) == 0 {
			plan.Claims[i].SourceRefs = []string{"server/customers/status.go"}
		}
		plan.Claims[i].Oracle = strings.ReplaceAll(plan.Claims[i].Oracle, "TODO(planner):", "verified oracle:")
	}
	plan.Claims = append(plan.Claims, Claim{ClaimID: "claim-dv-1", Lens: "delivery", Target: "server/customers/status.go", Assertion: "REQ acceptance remains delivered", Oracle: "REQ obligations map to status API behavior", Method: "traceability", Applicability: "required", SourceRefs: []string{"REQ-001", "server/customers/status.go"}})
	plan.Assignments = append(plan.Assignments, PlanAssignment{AssignmentID: "assignment-dv-1", Lens: "delivery", ClaimIDs: []string{"claim-dv-1"}, NonOverlapBoundary: "REQ acceptance traceability", ExecutionWave: "static"})
	return root, state, plan, sp, jp
}
func TestModuleScopeTransitiveCycleAndDeletionUnion(t *testing.T) {
	g := scopeTestGraph()
	g.Modules = append(g.Modules, ModuleDeclaration{ID: "invoices", OwnedPaths: []string{"server/invoices/"}, BrowserApplicability: "not_applicable", ApplicabilityRefs: []string{"docs/dev/contracts/SYNC-001.md"}})
	g.Resources = append(g.Resources, ModuleResource{ID: "order-created", Paths: []string{"server/orders/event.go"}, ProviderModuleIDs: []string{"orders"}, ConsumerModuleIDs: []string{"invoices", "customers"}, SourceRefs: []string{"docs/dev/contracts/SYNC-001.md"}})
	current := *g
	current.Resources = nil
	in := ScopeInputs{Graph: &current, PreviousGraph: g, ChangedPaths: []string{"server/customers/status.go"}, KnownModules: []string{"customers", "orders", "invoices"}, Requirements: map[string][]ScenarioRef{"customers": {{CasesPath: "docs/design/prototypes/customers/cases.json", CaseID: "CASE-001", PathIDs: []string{"PATH-001"}}}, "orders": {{CasesPath: "docs/design/prototypes/orders/cases.json", CaseID: "CASE-001", PathIDs: []string{"PATH-001"}}}}}
	s, e := ResolveModuleScope(in)
	if e != nil || len(s.Candidates) != 3 {
		t.Fatalf("closure: %+v %v", s, e)
	}
	in.ChangedPaths = []string{"server/orders-old/file.go"}
	s, e = ResolveModuleScope(in)
	if e != nil || !strings.Contains(strings.Join(s.UnresolvedInputs, " "), "no module/resource ownership") {
		t.Fatalf("ownership boundary: %+v %v", s, e)
	}
	in.Graph = nil
	in.PreviousGraph = nil
	in.KnownModules = nil
	s, e = ResolveModuleScope(in)
	if e != nil || len(s.UnresolvedInputs) == 0 {
		t.Fatalf("empty inventory succeeded: %+v %v", s, e)
	}
}
func TestModuleScopeDraftRejectsRelatedOmissionAndCASECollision(t *testing.T) {
	root, state, plan, _, _ := scopeFixture(t)
	if len(plan.ModuleScope.Candidates) != 2 || plan.ModuleScope.Candidates[1].ModuleID != "orders" {
		t.Fatalf("related order omitted: %+v", plan.ModuleScope)
	}
	if e := ValidateModuleScope(root, state, plan); e != nil {
		t.Fatal(e)
	}
	data, _ := json.Marshal(plan)
	if e := schema.NewEmbeddedValidator().ValidateBytes("review-plan.schema.json", data); e != nil {
		t.Fatal(e)
	}
	tests := []struct {
		name string
		edit func(*Plan)
	}{
		{"candidate omission", func(p *Plan) {
			p.ModuleScope.Candidates = p.ModuleScope.Candidates[:1]
			p.ModuleScope.Dispositions = p.ModuleScope.Dispositions[:1]
		}},
		{"disposition omission", func(p *Plan) { p.ModuleScope.Dispositions = p.ModuleScope.Dispositions[:1] }},
		{"CASE collision", func(p *Plan) {
			for i := range p.Claims {
				if p.Claims[i].Lens == "e2e" && strings.Contains(p.Claims[i].ClaimID, "orders") {
					p.Claims[i].ScenarioRefs[0].CasesPath = "docs/design/prototypes/customers/cases.json"
				}
			}
		}},
		{"PATH omission", func(p *Plan) {
			for i := range p.Claims {
				if p.Claims[i].Lens == "e2e" {
					p.Claims[i].ScenarioRefs = nil
				}
			}
		}},
		{"digest", func(p *Plan) { p.ModuleScope.InputDigest = strings.Repeat("a", 64) }},
		{"downgrade", func(p *Plan) { p.SchemaVersion = "1.0.0"; p.ModuleScope = nil }},
		{"UI downgrade", func(p *Plan) { p.ModuleScope.Dispositions[0].Action = "no_behavior_impact" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var copy Plan
			json.Unmarshal(data, &copy)
			tc.edit(&copy)
			if e := ValidateModuleScope(root, state, &copy); e == nil {
				t.Fatal("invalid scope accepted")
			}
		})
	}
}
func TestModuleScopeFreshnessCASAndUnrelatedWorkspace(t *testing.T) {
	root, state, plan, _, _ := scopeFixture(t)
	prepared, e := prepareModuleScope(root, state, 1, workspaceValue(plan.VerificationArtifactWorkspace))
	if e != nil {
		t.Fatal(e)
	}
	scopeWrite(t, root, *plan.VerificationArtifactWorkspace+"/new.spec.ts", "// verification work\n")
	scopeRunGit(t, root, "add", *plan.VerificationArtifactWorkspace)
	scopeRunGit(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "verification workspace")
	if e := prepared.Verify(state); e == nil {
		t.Fatal("CAS accepted moved view")
	}
	if e := ValidateModuleScope(root, state, plan); e != nil {
		t.Fatalf("unrelated workspace invalidated scope: %v", e)
	}
	g := scopeTestGraph()
	g.Modules = append(g.Modules, ModuleDeclaration{ID: "invoices", OwnedPaths: []string{"server/invoices/"}, BrowserApplicability: "not_applicable", ApplicabilityRefs: []string{"docs/dev/contracts/SYNC-001.md"}})
	g.Resources[0].ConsumerModuleIDs = append(g.Resources[0].ConsumerModuleIDs, "invoices")
	b := scopeWrite(t, root, ModuleRelationsPath, g)
	stateDocuments(state)[0].(map[string]any)["sha256"] = sha256Of(b)
	scopeRunGit(t, root, "add", ModuleRelationsPath)
	scopeRunGit(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "new consumer")
	if e := ValidateModuleScope(root, state, plan); e == nil {
		t.Fatal("new consumer retained old PASS scope")
	}
}
func TestModuleScopeRegisterRetryRejectsTamperingWithoutWrites(t *testing.T) {
	root, _, plan, sp, jp := scopeFixture(t)
	path := ".claude/review/author-plan.json"
	scopeWrite(t, root, path, plan)
	first, e := RegisterPlan(root, sp, jp, PlanRequest{PlanPath: filepath.Join(root, path), ExpectedRevision: -1, OperationID: "scope-register"})
	if e != nil {
		t.Fatal(e)
	}
	second, e := RegisterPlan(root, sp, jp, PlanRequest{PlanPath: filepath.Join(root, path), ExpectedRevision: -1, OperationID: "scope-register"})
	if e != nil || first.Revision != second.Revision || !second.OperationReplayed {
		t.Fatalf("retry: %+v %v", second, e)
	}
	before, _ := os.ReadFile(sp)
	journal, _ := os.ReadFile(jp)
	plan.ModuleScope.Candidates = plan.ModuleScope.Candidates[:1]
	scopeWrite(t, root, path, plan)
	if _, e = RegisterPlan(root, sp, jp, PlanRequest{PlanPath: filepath.Join(root, path), ExpectedRevision: -1, OperationID: "scope-register"}); e == nil {
		t.Fatal("conflicting operation accepted")
	}
	after, _ := os.ReadFile(sp)
	later, _ := os.ReadFile(jp)
	if !bytes.Equal(before, after) || !bytes.Equal(journal, later) {
		t.Fatal("rejection wrote success")
	}
}
func TestModuleScopeDispositionChangeInvalidatesClaims(t *testing.T) {
	_, _, p, _, _ := scopeFixture(t)
	data, _ := json.Marshal(p)
	var next Plan
	json.Unmarshal(data, &next)
	next.ModuleScope.Dispositions[1].Rationale = "new scope exclusion rationale"
	changed := changedModuleScopeClaims(p, &next)
	if len(changed) != 2 {
		t.Fatalf("top-level scope change ignored: %v", changed)
	}
}
func TestModuleScopeVersionsAndAuthorOnly(t *testing.T) {
	root, _, p, _, _ := scopeFixture(t)
	b, _ := json.Marshal(p)
	before, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	report := PreflightPlan(root, b, nil)
	if report.Mode != "author_only" || !report.ArtifactValid || report.TransitionReady {
		t.Fatalf("author-only: %+v", report)
	}
	after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("lint wrote state")
	}
	if e := ValidateProtocolBinding(map[string]any{"schema_version": "1.1.0"}, "1.1.0"); e == nil {
		t.Fatal("Result version was widened")
	}
	p.SchemaVersion = "1.0.0"
	b, _ = json.Marshal(p)
	if e := schema.NewEmbeddedValidator().ValidateBytes("review-plan.schema.json", b); e == nil {
		t.Fatal("old schema accepted new fields")
	}
}

func consumeScopedFixture(t *testing.T, complete bool) (string, map[string]any, *Plan, string, string) {
	return consumeScopedFixtureWithExclusion(t, complete, false)
}
func consumeScopedFixtureWithExclusion(t *testing.T, complete, exclude bool) (string, map[string]any, *Plan, string, string) {
	t.Helper()
	root, state, plan, sp, jp := scopeFixture(t)
	sourceBytes := scopeWrite(t, root, ".claude/evidence/scope-review.txt", "independent scope decision evidence")
	source := "path:.claude/evidence/scope-review.txt#sha256=" + sha256Of(sourceBytes)
	for i := range plan.Claims {
		if plan.Claims[i].Lens == "e2e" {
			plan.Claims[i].SourceRefs = append(plan.Claims[i].SourceRefs, source)
		}
	}
	if exclude {
		d := &plan.ModuleScope.Dispositions[1]
		d.Action, d.Rationale = "no_behavior_impact", "pinned API contract preserves the consumer behavior; independent assessor must verify"
		removed := map[string]bool{}
		for _, id := range d.VerificationClaimIDs {
			removed[id] = true
		}
		d.VerificationClaimIDs = []string{}
		d.AssessmentClaimIDs = []string{"claim-scope-orders"}
		for _, r := range plan.ModuleScope.InputRefs {
			if r.Path == ModuleRelationsPath && r.SourceKind == "git_tree" {
				d.EvidenceRefs = []ScopeInputRef{r}
			}
		}
		claims := []Claim{}
		for _, c := range plan.Claims {
			if !removed[c.ClaimID] {
				claims = append(claims, c)
			}
		}
		plan.Claims = append(claims, Claim{ClaimID: "claim-scope-orders", Lens: "qa", Target: "module:orders", Assertion: "consumer behavior is unchanged", Oracle: "independently verify unchanged consumer contract against pinned scope sources", Method: "static impact assessment", Applicability: "required", SourceRefs: []string{"module:orders", ModuleRelationsPath}, FocusKey: "module-scope:orders"})
		assignments := []PlanAssignment{}
		for _, a := range plan.Assignments {
			if a.Lens == "e2e" && removed[a.ClaimIDs[0]] {
				continue
			}
			if a.AssignmentID == "assignment-qa-design-boundary" {
				a.ClaimIDs = append(a.ClaimIDs, "claim-scope-orders")
			}
			assignments = append(assignments, a)
		}
		plan.Assignments = assignments
	}
	// Prepare independent synthetic reviewers; no browser or business process is run.
	agents := []any{}
	for _, a := range plan.Assignments {
		role := "qa"
		if a.Lens == "delivery" {
			role = "delivery-verifier"
		}
		if a.Lens == "e2e" {
			role = "e2e-tester"
		}
		agents = append(agents, map[string]any{"id": "agent-" + a.AssignmentID, "role": role, "state": "working", "task_ids": []any{}, "team_id": "team-review-1", "definition_ref": "agents/qa.md", "prompt_ref": ".claude/workgroups/REQ-001/m.json#" + a.AssignmentID, "readback_ref": nil, "activation_ref": nil, "activation_revision": nil, "updated_at": "2026-10-12T00:00:00Z"})
	}
	state["entities"].(map[string]any)["agents"] = agents
	scopeWrite(t, root, ".claude/loop-state.json", state)
	author := scopeWrite(t, root, ".claude/review/author-plan.json", plan)
	_ = author
	snap, e := RegisterPlan(root, sp, jp, PlanRequest{PlanPath: filepath.Join(root, ".claude/review/author-plan.json"), ExpectedRevision: -1})
	if e != nil {
		t.Fatal(e)
	}
	ordered := []PlanAssignment{}
	for _, wave := range []string{"static", "behavior"} {
		for _, a := range plan.Assignments {
			if a.ExecutionWave == wave {
				ordered = append(ordered, a)
			}
		}
	}
	for i, a := range ordered {
		if !complete && i == len(ordered)-1 {
			break
		}
		agent := "agent-" + a.AssignmentID
		snap = markDispatched(t, root, sp, jp, snap, a.AssignmentID, agent)
		conclusions := map[string]string{}
		for _, id := range a.ClaimIDs {
			conclusions[id] = "pass"
		}
		resultPath := writeResultFile(t, root, plan, a.AssignmentID, "review-result-"+a.AssignmentID, agent, "pass", conclusions, nil)
		movedPath := filepath.Join(root, ".claude/review", filepath.Base(resultPath))
		if e := os.Rename(resultPath, movedPath); e != nil {
			t.Fatal(e)
		}
		resultPath = movedPath
		if a.Lens == "e2e" {
			b, _ := os.ReadFile(resultPath)
			var result map[string]any
			json.Unmarshal(b, &result)
			digest, err := WorkspaceDigest(root, *plan.VerificationArtifactWorkspace)
			if err != nil {
				t.Fatal(err)
			}
			result["verification_artifact_digest"] = digest
			scopeWrite(t, root, ".claude/review/"+filepath.Base(resultPath), result)
		}
		snap, e = SubmitResult(root, sp, jp, SubmitRequest{ResultPath: resultPath, AssignmentID: a.AssignmentID, ExpectedRevision: -1})
		if e != nil {
			t.Fatalf("submit %s: %v", a.AssignmentID, e)
		}
	}
	return root, snap.State, plan, sp, jp
}

func TestModuleScopeConsumedResultClosureAndEvidenceTampering(t *testing.T) {
	root, finalState, plan, _, _ := consumeScopedFixture(t, true)
	if p := PlanPointerFromState(finalState); p.Status != "clean" {
		t.Fatalf("not clean: %+v", p)
	}
	if e := ValidateModuleScopeResults(root, finalState, plan); e != nil {
		t.Fatal(e)
	}
	evidence := finalState["evidence"].([]any)
	for _, raw := range evidence {
		row := raw.(map[string]any)
		if row["kind"] == "review_result" {
			scopeWrite(t, root, stringField(row["path"]), "tampered result")
			break
		}
	}
	if e := ValidateModuleScopeResults(root, finalState, plan); e == nil {
		t.Fatal("tampered consumed result accepted")
	}
}

func TestModuleScopeIndependentExclusionAndMissingRelations(t *testing.T) {
	root, state, plan, _, _ := scopeFixture(t)
	d := &plan.ModuleScope.Dispositions[1]
	d.Action = "no_behavior_impact"
	d.Rationale = "the preserved status contract has no order behavior effect"
	d.VerificationClaimIDs = []string{}
	id := "claim-scope-orders"
	d.AssessmentClaimIDs = []string{id}
	for _, ref := range plan.ModuleScope.InputRefs {
		if ref.Path == "docs/dev/contracts/SYNC-001.md" && ref.SourceKind == "git_tree" {
			d.EvidenceRefs = append(d.EvidenceRefs, ref)
		}
	}
	plan.Claims = append(plan.Claims, Claim{ClaimID: id, Lens: "qa", Target: "module:orders", Assertion: "orders is unaffected", Oracle: "status contract compatibility independently proved", Method: "static impact assessment", Applicability: "required", SourceRefs: []string{"module:orders", "docs/dev/contracts/SYNC-001.md"}, FocusKey: "module-scope:orders"})
	plan.Assignments = append(plan.Assignments, PlanAssignment{AssignmentID: "assignment-scope-orders", Lens: "qa", ClaimIDs: []string{id}, ExecutionWave: "static", NonOverlapBoundary: "orders impact assessment"})
	if e := ValidateModuleScope(root, state, plan); e != nil {
		t.Fatal(e)
	}
	r := &Result{ProducerAgentID: "contract-author", ClaimResults: []ClaimResult{{ClaimID: id, Conclusion: "pass"}}}
	if e := validateScopeProducerIndependence(state, plan, r); e == nil {
		t.Fatal("author self-certified exclusion")
	}
	r.ProducerAgentID = "independent-qa"
	if e := validateScopeProducerIndependence(state, plan, r); e != nil {
		t.Fatal(e)
	}
	d.AssessmentClaimIDs = nil
	if e := ValidateModuleScope(root, state, plan); e == nil {
		t.Fatal("reason-only exclusion accepted")
	}
	scopeRunGit(t, root, "rm", ModuleRelationsPath)
	scopeRunGit(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "missing relations")
	s, e := ResolveModuleScopeForRoot(root, state, 1, *plan.VerificationArtifactWorkspace)
	if e != nil {
		t.Fatal(e)
	}
	if s.Resolution != "conservative_fallback" || len(s.Candidates) != 2 {
		t.Fatalf("missing graph vanished candidates: %+v", s)
	}
}

func TestModuleScopeS9CumulativeAndVerifiedSubjects(t *testing.T) {
	root, state, plan, _, _ := scopeFixture(t)
	planBytes := scopeWrite(t, root, ".claude/review/plans/prior.json", plan)
	state["review"].(map[string]any)["plan"] = map[string]any{"plan_id": plan.ReviewPlanID, "path": ".claude/review/plans/prior.json", "sha256": sha256Of(planBytes), "revision": 1, "review_round": 1, "status": "observation_sealed", "e2e_coverage_state": plan.E2ECoverageState}
	state["review"].(map[string]any)["round"] = 2
	// Confirm introduces no patch but still has a fingerprinted verification subject.
	code, _ := os.ReadFile(filepath.Join(root, "server/customers/status.go"))
	impact := scopeWrite(t, root, ".claude/evidence/confirm-impact.json", map[string]any{"record_type": "change_impact", "runtime_id": state["runtime_id"], "baseline_generation": 1, "changed_artifacts": []any{}, "verified_subjects": []any{map[string]any{"path": "server/customers/status.go", "sha256": sha256Of(code)}}})
	state["evidence"] = append(state["evidence"].([]any), map[string]any{"id": "confirm-impact", "kind": "change_impact", "path": ".claude/evidence/confirm-impact.json", "sha256": sha256Of(impact), "baseline_generation": 1, "status": "valid", "review_round": 2})
	s, e := ResolveModuleScopeForRoot(root, state, 2, "e2e-workspace/round-2")
	if e != nil || len(s.UnresolvedInputs) != 0 || len(s.Candidates) != 2 {
		t.Fatalf("confirm narrowed original scope: %+v %v", s, e)
	}
	found := false
	for _, ref := range s.InputRefs {
		if ref.SourceKind == "prior_plan" {
			found = true
		}
	}
	if !found {
		t.Fatal("S9 prior-plan provenance lost")
	}
	// The handoff seed freezes that provenance; replacing the active plan with
	// the new seed must leave the resolver's denominator unchanged.
	seed := *plan
	seed.ReviewRound = 2
	seed.ModuleScope = &s
	seedBytes := scopeWrite(t, root, ".claude/review/seed.json", seed)
	review := state["review"].(map[string]any)
	review["plan"] = nil
	review["round_entry"] = map[string]any{"transition_id": "TR-012", "round": 2, "baseline_generation": 1, "change_impact_ref": "confirm-impact", "review_plan_seed_ref": ".claude/review/seed.json", "review_plan_seed_sha256": sha256Of(seedBytes)}
	again, e := ResolveModuleScopeForRoot(root, state, 2, "e2e-workspace/round-2")
	if e != nil || s.InputDigest != again.InputDigest {
		t.Fatalf("seed scope changed: %+v %v", again, e)
	}
	scopeWrite(t, root, ".claude/evidence/confirm-impact.json", "tampered confirmation")
	invalid, e := ResolveModuleScopeForRoot(root, state, 2, "e2e-workspace/round-2")
	if e != nil || len(invalid.UnresolvedInputs) == 0 {
		t.Fatal("tampered confirm scope accepted")
	}
}

func TestModuleScopeConcurrentIdenticalOperations(t *testing.T) {
	root, _, plan, sp, jp := scopeFixture(t)
	path := filepath.Join(root, ".claude/review/concurrent.json")
	scopeWrite(t, root, ".claude/review/concurrent.json", plan)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := RegisterPlan(root, sp, jp, PlanRequest{PlanPath: path, ExpectedRevision: -1, OperationID: "related-concurrent"})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	journal, _ := os.ReadFile(jp)
	if bytes.Count(journal, []byte("review_plan_registered")) != 1 {
		t.Fatal("concurrent operation wrote duplicate success")
	}
}

// Exercise the real revision transaction: scope wording alone must invalidate
// consumed browser results even though every Claim remains byte-identical.
func TestModuleScopeRevisionInvalidatesConsumedResults(t *testing.T) {
	root, state, plan, sp, jp := consumeScopedFixture(t, false)
	oldDispositions := Dispositions(state)
	var consumed []string
	for _, d := range plan.ModuleScope.Dispositions {
		for _, id := range d.VerificationClaimIDs {
			if oldDispositions[id].Disposition == "pass" {
				consumed = append(consumed, oldDispositions[id].ResultID)
			}
		}
	}
	if len(consumed) == 0 {
		t.Fatal("fixture consumed no scoped result")
	}
	beforeClaims, _ := json.Marshal(plan.Claims)
	plan.ModuleScope.Dispositions[0].Rationale += "; independent scope review confirmed this disposition"
	scopeWrite(t, root, ".claude/review/revised.json", plan)
	sourceBytes, err := os.ReadFile(filepath.Join(root, ".claude/evidence/scope-review.txt"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := RevisePlan(root, sp, jp, ReviseRequest{PlanPath: filepath.Join(root, ".claude/review/revised.json"), SourceRef: "path:.claude/evidence/scope-review.txt#sha256=" + sha256Of(sourceBytes), AffectedSurface: "docs/design/prototypes", ExpectedRevision: -1})
	if err != nil {
		t.Fatal(err)
	}
	afterClaims, _ := json.Marshal(plan.Claims)
	if !bytes.Equal(beforeClaims, afterClaims) {
		t.Fatal("fixture changed Claim wording")
	}
	for _, d := range plan.ModuleScope.Dispositions {
		for _, id := range d.VerificationClaimIDs {
			if got := Dispositions(snapshot.State)[id]; got.Disposition != "planned" || got.ResultID != "" {
				t.Fatalf("old scope pass survived revision: %+v", got)
			}
		}
	}
	for _, resultID := range consumed {
		found := false
		for _, raw := range evidenceEntries(snapshot.State) {
			row := mapValue(raw)
			if row["id"] == resultID {
				found = row["status"] == "invalid" && row["invalidation_reason"] == "plan_revision"
			}
		}
		if !found {
			t.Fatalf("old scoped Result %s remains valid", resultID)
		}
	}
	if err := ValidateModuleScopeResults(root, snapshot.State, plan); err == nil {
		t.Fatal("revision passed without fresh scope Results")
	}
}

func TestModuleScopeLockedPlanningDocumentsAndFlowChanges(t *testing.T) {
	g := scopeTestGraph()
	in := ScopeInputs{Graph: g, PreviousGraph: g, ChangedPaths: []string{"docs/dev/tasks/TASK-001.md"}, LockedDocumentPaths: []string{"docs/dev/tasks/TASK-001.md"}, KnownModules: []string{"customers", "orders"}, Requirements: map[string][]ScenarioRef{"customers": {{CasesPath: g.Modules[0].CasesPath, CaseID: "CASE-001", PathIDs: []string{"PATH-001"}}}, "orders": {{CasesPath: g.Modules[1].CasesPath, CaseID: "CASE-001", PathIDs: []string{"PATH-001"}}}}}
	out, err := ResolveModuleScope(in)
	if err != nil || len(out.UnresolvedInputs) != 0 || len(out.Candidates) != 2 || out.Resolution != "conservative_fallback" {
		t.Fatalf("locked planning input lost scope: %+v %v", out, err)
	}
	in.LockedDocumentPaths = nil
	out, err = ResolveModuleScope(in)
	if err != nil || len(out.UnresolvedInputs) == 0 {
		t.Fatal("unapproved Markdown was treated as harmless")
	}
	in.ChangedPaths = []string{"docs/design/prototypes/orders/flows.md"}
	out, err = ResolveModuleScope(in)
	if err != nil || len(out.UnresolvedInputs) != 0 || len(out.Candidates) != 1 || !out.Candidates[0].DirectUI {
		t.Fatalf("changed flow oracle allowed browser waiver: %+v %v", out, err)
	}
}

func TestModuleScopeIndependentExclusionConsumption(t *testing.T) {
	root, state, plan, _, _ := consumeScopedFixtureWithExclusion(t, true, true)
	if PlanPointerFromState(state).Status != "clean" {
		t.Fatal("independently assessed exclusion cannot complete")
	}
	if err := ValidateModuleScopeResults(root, state, plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.ModuleScope.Candidates) != 2 || plan.ModuleScope.Dispositions[1].Action != "no_behavior_impact" {
		t.Fatal("exclusion erased related candidate")
	}
}
