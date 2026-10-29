package review

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// taskDoc is one current-generation TASK document fact.
type taskDoc struct {
	id     string
	path   string
	sha256 string
}

// DraftPlan scaffolds a ReviewPlan from the current runtime facts
// (L3-S7 §4.2 claim generation order, planner assist): every
// current-generation TASK yields a delivery traceability claim; the union of
// Builder-changed paths yields one independently dispatchable QA Assignment
// per baseline focus; CASE/script discovery offers candidates while Main
// chooses actual reuse. UI impact and browser discovery do not decide general E2E applicability. The
// draft is still a scaffold when no stable CASE input exists —
// its remaining TODO markers are intentionally rejected by registration.
func DraftPlan(state map[string]any, round int) (*Plan, []string) {
	return draftPlanForRoot("", state, round)
}

// DraftPlanForRoot is the production S7 draft path. Canonical S6 generations
// are projected from the original machine snapshot and Main Git range;
// historical runtimes use the immutable completion envelope path.
func DraftPlanForRoot(root string, state map[string]any, round int) (*Plan, []string) {
	return draftPlanForRoot(root, state, round)
}

func DraftPlanForRootContext(ctx context.Context, root string, state map[string]any, round int) (*Plan, []string) {
	return draftPlanForRoot(root, state, round, ctx)
}

func draftPlanForRoot(root string, state map[string]any, round int, contexts ...context.Context) (*Plan, []string) {
	ctx := baselineContext(contexts)
	generation := baselineGeneration(state)
	var notes []string
	productBaseline, baselineErr := readCanonicalProductBaselineContext(ctx, root, state)
	if baselineErr != nil {
		return nil, []string{"canonical S6 product baseline cannot be verified: " + baselineErr.Error()}
	}

	var tasks []taskDoc
	if productBaseline != nil {
		for _, task := range productBaseline.Tasks {
			tasks = append(tasks, taskDoc{id: task.ID, path: task.Path, sha256: task.SHA256})
		}
	} else {
		for _, raw := range stateDocuments(state) {
			doc, _ := raw.(map[string]any)
			if doc == nil || doc["kind"] != "task" {
				continue
			}
			if intField(doc["generation"]) != generation {
				continue
			}
			id, _ := doc["id"].(string)
			path, _ := doc["path"].(string)
			sha, _ := doc["sha256"].(string)
			if id == "" || path == "" {
				continue
			}
			tasks = append(tasks, taskDoc{id: id, path: path, sha256: sha})
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].id < tasks[j].id })

	frozen := []FrozenSubject{}
	seenFrozen := map[string]bool{}
	for _, task := range tasks {
		if seenFrozen[task.path] {
			continue
		}
		seenFrozen[task.path] = true
		frozen = append(frozen, FrozenSubject{Path: task.path, SHA256: task.sha256, Kind: "task"})
	}

	projection := s7BaselineProjection{}
	if productBaseline != nil {
		var err error
		frozen, err = frozenSubjectsForProductBaseline(productBaseline)
		if err != nil {
			return nil, append(notes, "canonical S6 frozen subjects cannot be built: "+err.Error())
		}
		for _, path := range productBaseline.Paths {
			projection.ChangedPaths = append(projection.ChangedPaths, path.Path)
		}
		for _, file := range productBaseline.UnattributedPaths {
			notes = append(notes, "Main product change has no registered S6 contribution; it remains in full review coverage: "+file)
		}
	} else {
		projection = buildS7BaselineProjection(root, state, ctx)
		for _, diagnostic := range projection.Diagnostics {
			notes = append(notes, "S7 baseline projection: "+diagnostic+"; registration will reject an unverifiable completion artifact")
		}
		changedSubjects, subjectDiagnostics := changedSurfaceSubjects(root, projection.ChangedPaths)
		for _, diagnostic := range subjectDiagnostics {
			notes = append(notes, "S7 baseline projection: "+diagnostic+"; add a valid frozen subject before registration")
		}
		for _, subject := range changedSubjects {
			if seenFrozen[subject.Path] {
				continue
			}
			seenFrozen[subject.Path] = true
			frozen = append(frozen, subject)
		}
	}

	claims := []Claim{}
	assignments := []PlanAssignment{}
	claimSeq := 0
	nextClaim := func(lens, focus string) string {
		claimSeq++
		return fmt.Sprintf("claim-%s-%s-%d", lens, focus, claimSeq)
	}

	// Delivery traceability: one claim per TASK (L3-S7 §4.2 step 2).
	var dvClaimIDs []string
	for _, task := range tasks {
		id := nextClaim("dv", "traceability")
		claims = append(claims, Claim{
			ClaimID: id, Lens: "delivery", Target: task.path,
			Assertion:     task.id + " is delivered as locked: every acceptance obligation lands in the implementation",
			Oracle:        "TODO(planner): for " + task.id + ", read its Closing Contract and name the observable postcondition, forbidden side effect and source evidence that distinguish pass from fail",
			Method:        "requirement traceability review",
			Applicability: "required",
			SourceRefs:    []string{task.id, task.path},
			FocusKey:      "requirement-traceability",
		})
		dvClaimIDs = append(dvClaimIDs, id)
	}
	if len(dvClaimIDs) > 0 {
		assignments = append(assignments, PlanAssignment{
			AssignmentID: "assignment-dv-traceability", Lens: "delivery", ClaimIDs: dvClaimIDs,
			NonOverlapBoundary: "owns REQ/TASK traceability; QA owns implementation quality",
			ExecutionWave:      "static",
		})
	}

	// QA static baseline claims (L3-S7 §4.2 step 5): the standard focus set
	// over the changed surface.
	//
	// Target disclosure: the QA claim's `target` is the user-facing label
	// that names what the reviewer must look at. Without a real changed
	// surface the original "the current change surface" placeholder was
	// fiction — a QA reviewer reading it cannot tell what to review.
	// We project from the fingerprinted frozen subjects first (the same
	// authoritative surface the runtime validates against); if even that
	// is empty, emit an explicit TODO marker so registration can flag it.
	changedList := append([]string(nil), projection.ChangedPaths...)
	qaSurface, qaSurfaceIsPlaceholder := qaChangeSurface(changedList, frozen)
	if qaSurfaceIsPlaceholder {
		notes = append(notes, "QA claim `target` is a TODO marker (no current-generation completion envelopes and no frozen subjects); replace it with the real change surface before registration — the registration-time check rejects a fabricated target that names nothing")
	}
	// Each baseline focus is its own Assignment. This keeps the plan's
	// independent questions independently dispatchable: platform capacity may
	// queue them, but it must not collapse six quality perspectives into one
	// overloaded QA session.
	qaSourceRefs := taskIDs(tasks)
	if productBaseline != nil {
		qaSourceRefs = sortedUniqueRefs(append(qaSourceRefs, projection.ChangedPaths...))
	}
	for _, focus := range []struct{ key, assertion string }{
		{"design-boundary", "module boundaries, ownership and contracts are explicit; invalid crossings are rejected at the right layer"},
		{"pattern-idiom-fit", "the implementation uses the project's established design patterns and idioms where they reduce coupling or risk"},
		{"logic-state-error", "normal/edge/error paths are self-consistent; state transitions and error ownership are complete"},
		{"maintainability", "naming, abstraction level and cognitive complexity stay within the project's idiom"},
		{"testability-oracle", "behavior (not implementation detail) is asserted; negative/boundary paths carry valid oracles"},
		{"debt-operability", "the change does not introduce avoidable technical debt, opaque operation, or an unowned follow-up"},
	} {
		id := nextClaim("qa", focus.key)
		target := qaSurface
		method := "static code review"
		if qaSurfaceIsPlaceholder {
			// When the surface is unknown, keep the focus-specific oracle
			// TODO so the Planner must replace it during plan authoring
			// (registration will reject a literal `TODO(planner)` literal
			// with "TODO marker must be replaced"; surfacing it in `target`
			// as well makes the gap visible at draft time).
			target = "TODO(planner): which actual changed paths and dependencies require " + focus.key + " review? Use the supplied baseline; report missing inputs to Main"
			method = "TODO(planner): how will " + focus.key + " be inspected or exercised, and what observation would expose a violation? Use an actual review method or runnable check"
		}
		claims = append(claims, Claim{
			ClaimID: id, Lens: "qa", Target: target,
			Assertion:     focus.assertion,
			Oracle:        "TODO(planner): what observable condition makes " + focus.key + " pass or fail, and which source defines that condition?",
			Method:        method,
			Applicability: "required",
			SourceRefs:    qaSourceRefs,
			FocusKey:      focus.key,
		})
		assignments = append(assignments, PlanAssignment{
			AssignmentID:       "assignment-qa-" + focus.key,
			Lens:               "qa",
			ClaimIDs:           []string{id},
			FocusKeys:          []string{focus.key},
			NonOverlapBoundary: "owns the " + focus.key + " question; adjacent QA Assignments do not repeat this oracle",
			ExecutionWave:      "static",
		})
	}
	notes = append(notes, "the six QA baseline Claims are independent quality questions; keep their plan-local claim_id values and merge Assignments only when the same-lens read set and non-overlap boundary remain truthful")

	// Asset discovery is a source candidate projection. Main chooses the actual
	// execution state; explicit known CASEs still need individual dispositions.
	uiImpact := boundREQUIImpact(state)
	if strings.TrimSpace(uiImpact) == "" {
		// S7-9 (RC-07): a missing/mistyped ui_impact previously followed the
		// same not_applicable branch as an explicit "none", silently dropping
		// the whole E2E dimension. The draft refuses to choose for the
		// Planner: UI impact alone never establishes general E2E N/A.
		return nil, append(notes, "bound REQ metadata.ui_impact is empty or missing; the E2E coverage state cannot be derived — bind the REQ with an explicit ui_impact value (none | changed | unknown) via the requirement bind path, or add the metadata.ui_impact field, then re-draft (S7-9/RC-07: an empty ui_impact is an error, not an implicit not_applicable)")
	}
	inventory, discoveryDiagnostics := discoverE2EInventory(root, state, &Plan{ProductBaseline: productBaseline})
	for _, diagnostic := range discoveryDiagnostics {
		notes = append(notes, "E2E inventory: "+diagnostic+"; Main must resolve the original source gap, not assume an empty test library")
	}
	var e2eAssets []E2EAsset
	var verificationWorkspace *string
	e2eState := ""
	notes = append(notes, "Main must decide e2e_coverage_state from actual behavior, assets and environment: regression_available reuses confirmed assets; cold_start prepares only missing assets in the original workspace; not_applicable requires the original justified N/A Claim. UI impact and browser discovery do not decide general behavior applicability.")
	if uiImpact == "none" && len(inventory.Cases) == 0 {
		notes = append(notes, "ui_impact=none and no discovered browser CASE establish only the browser candidate facts; assess API, command, state and other observable behavior before choosing E2E applicability. No N/A Claim has been inferred.")
	}
	for _, candidate := range inventory.Candidates {
		notes = append(notes, fmt.Sprintf("Candidate script %s (observed sha256=%s), possible scenarios: %s. Text matches do not prove selectors, routes, environment or execution; resolve the actual binding in this ReviewPlan.", candidate.Path, candidate.SHA256, strings.Join(candidate.PossibleCases, ", ")))
	}
	if len(inventory.Cases) > 0 {
		for index, scenario := range inventory.Cases {
			claimID := fmt.Sprintf("claim-e2e-case-%d", index+1)
			claims = append(claims, Claim{
				ClaimID: claimID, Lens: "e2e",
				Target:    e2eScenarioTarget(scenario),
				Assertion: fmt.Sprintf("%s (%s) follows the declared CASE oracle without forbidden side effects", scenario.ID, scenario.Title),
				Oracle:    e2eScenarioOracle(scenario), Method: "real-browser execution",
				Applicability: "required", SourceRefs: e2eScenarioSourceRefs(scenario), FocusKey: scenarioRef(scenario),
			})
			assignments = append(assignments, PlanAssignment{
				AssignmentID: fmt.Sprintf("assignment-e2e-case-%d", index+1), Lens: "e2e", ClaimIDs: []string{claimID},
				FocusKeys:          []string{scenarioRef(scenario)},
				NonOverlapBoundary: "owns exactly " + scenarioRef(scenario) + " and its declared PATH(s); do not repeat another CASE's oracle",
				ExecutionWave:      "behavior",
				BehaviorSurface:    "browser",
			})
		}
	} else {
		notes = append(notes, "No required browser CASE inventory was discoverable; Main must restore the actual scenario sources or supply a justified applicability decision, not assume a blank test library; set each required E2E Assignment behavior_surface to browser or non_browser from explicit source facts before drafting its manifest")
		id := nextClaim("e2e", "flows")
		claims = append(claims, Claim{
			ClaimID: id, Lens: "e2e", Target: "TODO(planner): which existing scenario and source define this behavior? Name the actual browser, API, command or state entry; report missing scenario inputs to Main",
			Assertion: "declared entry points produce the expected user-observable behavior",
			Oracle:    "TODO(planner): after the declared fixture and actions, what visible/terminal state, persisted or forbidden effects and recovery distinguish pass from fail? Cite actual scenario evidence",
			Method:    "TODO(planner): choose the actual browser, API, command or state method from the behavior sources", Applicability: "required", SourceRefs: taskIDs(tasks), FocusKey: "user-flow",
		})
		assignments = append(assignments, PlanAssignment{
			AssignmentID: "assignment-e2e-flows", Lens: "e2e", ClaimIDs: []string{id},
			NonOverlapBoundary: "owns the declared flows; cold-start spec authoring stays inside the verification workspace",
			ExecutionWave:      "behavior",
		})
	}

	if len(tasks) == 0 {
		notes = append(notes, "no current-generation TASK documents found; the draft carries a placeholder DV/QA minimum — registration will reject an unjustified zero-coverage lens")
	}

	plan := &Plan{
		SchemaVersion:                 "1.0.0",
		ReviewPlanID:                  fmt.Sprintf("review-plan-draft-r%d", round),
		ReviewRound:                   round,
		BaselineGeneration:            generation,
		ProductBaseline:               productBaseline,
		FrozenSubjects:                frozen,
		CoverageInventory:             BuildCoverageInventoryForRoot(root, state),
		ChangeImpact:                  changeImpactFromPaths(changedList),
		Claims:                        claims,
		Assignments:                   assignments,
		E2ECoverageState:              e2eState,
		E2EAssets:                     e2eAssets,
		VerificationArtifactWorkspace: verificationWorkspace,
		DispatchCapacityPolicy:        "coverage_complete",
		CreatedBy:                     "orchestrator",
		CreatedAt:                     time.Now().UTC().Format(time.RFC3339Nano),
	}
	return plan, notes
}

func changeImpactFromPaths(paths []string) *ChangeImpact {
	if len(paths) == 0 {
		return nil
	}
	return &ChangeImpact{
		Summary:    "derived from the current-generation S6 completion changed_paths",
		SourceRefs: append([]string(nil), paths...),
	}
}

func taskIDs(tasks []taskDoc) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.id)
	}
	return ids
}

func sortedUniqueRefs(refs []string) []string {
	sort.Strings(refs)
	out := refs[:0]
	for _, ref := range refs {
		if ref == "" || len(out) > 0 && out[len(out)-1] == ref {
			continue
		}
		out = append(out, ref)
	}
	return out
}

func e2eScenarioTarget(scenario e2eScenario) string {
	parts := []string{scenario.ID}
	if scenario.Module != "" {
		parts = append(parts, "module="+scenario.Module)
	}
	if strings.TrimSpace(scenario.Title) != "" {
		parts = append(parts, strings.TrimSpace(scenario.Title))
	}
	if len(scenario.FlowRefs) > 0 {
		parts = append(parts, "flows="+strings.Join(scenario.FlowRefs, ","))
	}
	return strings.Join(parts, " ")
}

func e2eScenarioSourceRefs(scenario e2eScenario) []string {
	refs := []string{scenarioRef(scenario)}
	for _, ref := range scenario.FlowRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" || containsString(refs, ref) {
			continue
		}
		if strings.HasPrefix(ref, "PATH-") {
			ref = scenario.CasesPath + "#" + scenario.ID + "/" + ref
		}
		refs = append(refs, ref)
	}
	return refs
}

func e2eScenarioOracle(scenario e2eScenario) string {
	parts := []string{}
	for _, key := range []string{"visible", "terminal_state", "persisted_effects", "forbidden_side_effects", "rejection", "expected_state", "recovery"} {
		if value := e2eOracleValue(scenario.Oracle[key]); value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("execute %s and compare the browser-visible result, terminal state, persisted effects and forbidden side effects with its cases.json oracle", scenario.ID)
	}
	return "cases.json oracle: " + strings.Join(parts, "; ")
}

func e2eOracleValue(raw any) string {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				items = append(items, strings.TrimSpace(text))
			}
		}
		return strings.Join(items, " | ")
	case []string:
		return strings.Join(value, " | ")
	default:
		return ""
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// qaChangeSurface derives the QA claim `target` string. The Builder's
// completion envelopes are the authoritative record of the change
// surface; when those are missing the fingerprinted frozen subjects
// (the runtime-pinned REV/TASK paths) are the next-best projection so
// the QA reviewer at least knows the same files the rest of the round
// is reading. Only when both are empty do we emit a TODO marker — the
// `isPlaceholder` flag tells the caller to attach a planner note that
// the surface must be replaced before registration.
func qaChangeSurface(changedList []string, frozen []FrozenSubject) (string, bool) {
	if len(changedList) > 0 {
		return strings.Join(changedList, ", "), false
	}
	if len(frozen) > 0 {
		paths := make([]string, 0, len(frozen))
		for _, subject := range frozen {
			if subject.Path != "" {
				paths = append(paths, subject.Path)
			}
		}
		if len(paths) > 0 {
			sort.Strings(paths)
			return strings.Join(paths, ", "), false
		}
	}
	return "TODO(planner): change surface (no completion envelopes and no frozen subjects available)", true
}

func boundREQUIImpact(state map[string]any) string {
	bound, _ := state["bound_req"].(map[string]any)
	metadata, _ := bound["metadata"].(map[string]any)
	impact, _ := metadata["ui_impact"].(string)
	return impact
}

// ValidatePlanTaskCoverage is the registration-time coverage block
// (L3-S7 §4.4 coverage matrix): every current-generation TASK must appear
// in at least one Claim's source_refs, so a plan cannot silently drop part
// of the S6 batch.
func ValidatePlanTaskCoverage(state map[string]any, plan *Plan) error {
	generation := baselineGeneration(state)
	required := map[string]bool{}
	for _, raw := range stateDocuments(state) {
		doc, _ := raw.(map[string]any)
		if doc == nil || doc["kind"] != "task" {
			continue
		}
		if intField(doc["generation"]) != generation {
			continue
		}
		if id, _ := doc["id"].(string); id != "" {
			required[id] = true
		}
	}
	if len(required) == 0 {
		return nil
	}
	covered := map[string]bool{}
	for _, claim := range plan.Claims {
		for _, ref := range claim.SourceRefs {
			if required[ref] {
				covered[ref] = true
			}
		}
	}
	var missing []string
	for id := range required {
		if !covered[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		missingItems := make([]string, len(missing))
		for i, id := range missing {
			missingItems[i] = id + " has no Claim source_ref"
		}
		return s7GateError(
			"S7_PLAN_TASK_COVERAGE",
			"ReviewPlan drops current-generation TASKs from Claim coverage",
			missingItems,
			[]string{"add each missing TASK id to at least one Claim.source_refs; keep the Claim target and oracle specific to that TASK"},
			"runtime review-plan --file plan.json",
		)
	}
	return nil
}
