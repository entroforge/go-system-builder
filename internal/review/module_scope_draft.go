package review

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

func populateScopedDraft(root string, state map[string]any, plan *Plan, notes []string) (*Plan, []string) {
	plan.SchemaVersion = "1.1.0"
	prepared, err := prepareModuleScope(root, state, plan.ReviewRound, workspaceValue(plan.VerificationArtifactWorkspace))
	scope := ModuleScope{}
	if err == nil {
		scope = prepared.scope
	}
	if err != nil {
		scope = ModuleScope{ResolverVersion: ModuleScopeResolverVersion, RuntimeID: stringField(state["runtime_id"]), REQID: stringField(mapValue(state["bound_req"])["id"]), ReviewRound: plan.ReviewRound, BaselineGeneration: plan.BaselineGeneration, InputRefs: []ScopeInputRef{}, Candidates: []ModuleCandidate{}, Dispositions: []ModuleDisposition{}, Resolution: "conservative_fallback", UnresolvedInputs: []string{err.Error()}, DiscoveryManifestDigest: scopeJSONDigest([]string{}), InputDigest: scopeJSONDigest(err.Error())}
	}
	if err == nil {
		changed := prepared.changedPaths
		frozen := map[string]bool{}
		covered := map[string]bool{}
		for _, s := range plan.FrozenSubjects {
			frozen[s.Path] = true
		}
		for _, c := range plan.CoverageInventory {
			covered[c.SourceRef] = true
		}
		for _, path := range changed {
			if !frozen[path] {
				for _, ref := range scope.InputRefs {
					if ref.Path == path && ref.SourceKind == "git_tree" && ref.Present {
						plan.FrozenSubjects = append(plan.FrozenSubjects, FrozenSubject{Path: path, SHA256: ref.SHA256, Kind: "cumulative_changed_surface"})
						frozen[path] = true
						break
					}
				}
				if !frozen[path] {
					for _, ref := range scope.InputRefs {
						if ref.Path == path && ref.SourceKind == "baseline_tree" && ref.Present {
							plan.FrozenSubjects = append(plan.FrozenSubjects, FrozenSubject{Path: path, SHA256: ref.SHA256, Kind: "deleted"})
							frozen[path] = true
							break
						}
					}
				}
			}
			if !covered[path] {
				plan.CoverageInventory = append(plan.CoverageInventory, CoverageItem{ID: "coverage-scope-" + sha256Of([]byte(path))[:12], Kind: "cumulative_change", SourceRef: path, Target: path, Lens: "qa"})
			}
		}
		if len(changed) > 0 {
			for i := range plan.Claims {
				if plan.Claims[i].Lens == "qa" {
					plan.Claims[i].Target = strings.Join(changed, ", ")
					plan.Claims[i].SourceRefs = sortedDistinct(append(plan.Claims[i].SourceRefs, changed...))
				}
			}
		}
		if plan.ChangeImpact == nil {
			plan.ChangeImpact = &ChangeImpact{Summary: "cumulative REQ/S6/S9 impact scope", SourceRefs: []string{}}
		}
		plan.ChangeImpact.SourceRefs = sortedDistinct(append(plan.ChangeImpact.SourceRefs, changed...))
		entry := mapValue(mapValue(state["review"])["round_entry"])
		if entry["transition_id"] == "TR-012" {
			plan.ChangeImpact.SourceRefs = sortedDistinct(append(plan.ChangeImpact.SourceRefs, stringField(entry["change_impact_ref"])))
		}
	}
	plan.ModuleScope = &scope
	for _, diagnostic := range scope.UnresolvedInputs {
		notes = append(notes, "Module scope: "+diagnostic+"; draft is not ready for registration")
	}
	if err != nil || len(scope.Candidates) == 0 {
		return plan, notes
	}
	// Replace the legacy REQ-filtered inventory with the authoritative scope.
	claims := []Claim{}
	assignments := []PlanAssignment{}
	for _, c := range plan.Claims {
		if c.Lens != "e2e" {
			claims = append(claims, c)
		}
	}
	for _, a := range plan.Assignments {
		if a.Lens != "e2e" {
			assignments = append(assignments, a)
		}
	}
	plan.E2EAssets = nil
	plan.VerificationArtifactWorkspace = nil
	plan.E2ECoverageState = "not_applicable"
	view, _ := fileview.ForState(root, state)
	for _, candidate := range scope.Candidates {
		d := ModuleDisposition{ModuleID: candidate.ModuleID, Action: "unknown", Rationale: "TODO(planner): assess the recorded impact chain and choose verification or an independently checked exclusion", EvidenceRefs: []ScopeInputRef{}, VerificationClaimIDs: []string{}, AssessmentClaimIDs: []string{}}
		if len(candidate.RequirementRefs) > 0 {
			d.Action = "browser_regression"
			d.Rationale = "conservative regression over the module's full required browser CASE/PATH inventory"
			plan.E2ECoverageState = "cold_start"
			for i, ref := range candidate.RequirementRefs {
				id := fmt.Sprintf("claim-e2e-%s-%d", candidate.ModuleID, i+1)
				scenario := e2eScenario{Module: candidate.ModuleID, ID: ref.CaseID}
				if view != nil {
					data, _ := view.ReadFile(ref.CasesPath)
					if data == nil {
						for _, input := range scope.InputRefs {
							if input.Path == ref.CasesPath && input.SourceKind == "baseline_tree" && input.Present {
								binding := mapValue(mapValue(state["bound_req"])["workspace"])
								old, e := fileview.New(root, stringField(binding["bound_commit"]), []fileview.Rule{{Path: ".", Source: "git_tree"}})
								if e == nil {
									data, _ = old.ReadFile(ref.CasesPath)
								}
							}
						}
					}
					var doc e2eCasesDocument
					if json.Unmarshal(data, &doc) == nil {
						for _, item := range doc.Cases {
							if item.ID == ref.CaseID {
								scenario.Title = item.Title
								scenario.Oracle = item.Oracle
								scenario.FlowRefs = item.FlowRefs
							}
						}
					}
				}
				claims = append(claims, Claim{ClaimID: id, Lens: "e2e", Target: ref.CasesPath + "#" + ref.CaseID, Assertion: fmt.Sprintf("%s/%s follows its declared oracle on every required PATH", candidate.ModuleID, ref.CaseID), Oracle: e2eScenarioOracle(scenario), Method: "real-browser execution", Applicability: "required", SourceRefs: append([]string{"module:" + candidate.ModuleID, ref.CasesPath, ref.CaseID}, ref.PathIDs...), FocusKey: candidate.ModuleID + ":" + ref.CaseID, ScenarioRefs: []ScenarioRef{ref}})
				d.VerificationClaimIDs = append(d.VerificationClaimIDs, id)
				assignments = append(assignments, PlanAssignment{AssignmentID: fmt.Sprintf("assignment-e2e-%s-%d", candidate.ModuleID, i+1), Lens: "e2e", ClaimIDs: []string{id}, FocusKeys: []string{candidate.ModuleID + ":" + ref.CaseID}, NonOverlapBoundary: "owns " + candidate.ModuleID + "/" + ref.CaseID + " and its PATHs", ExecutionWave: "behavior"})
			}
		} else if !candidate.BrowserRequired && !candidate.DirectUI {
			d.Action = "integration_verification"
			assessmentID := "claim-scope-" + candidate.ModuleID
			verifyID := "claim-integration-" + candidate.ModuleID
			d.AssessmentClaimIDs = []string{assessmentID}
			d.VerificationClaimIDs = []string{verifyID}
			for _, r := range scope.InputRefs {
				if r.Present && r.Path == ModuleRelationsPath && r.SourceKind == "git_tree" {
					d.EvidenceRefs = append(d.EvidenceRefs, r)
				}
			}
			claims = append(claims, Claim{ClaimID: assessmentID, Lens: "qa", Target: "module:" + candidate.ModuleID, Assertion: "the module's scope decision and absence of browser impact follow the pinned contracts and ownership", Oracle: "TODO(planner): name the independently reviewable exclusion evidence and browser boundary", Method: "static impact assessment", Applicability: "required", SourceRefs: []string{"module:" + candidate.ModuleID, ModuleRelationsPath}, FocusKey: "module-scope:" + candidate.ModuleID})
			joined := false
			for i := range assignments {
				if assignments[i].AssignmentID == "assignment-qa-design-boundary" {
					assignments[i].ClaimIDs = append(assignments[i].ClaimIDs, assessmentID)
					joined = true
				}
			}
			if !joined {
				assignments = append(assignments, PlanAssignment{AssignmentID: "assignment-scope-" + candidate.ModuleID, Lens: "qa", ClaimIDs: []string{assessmentID}, NonOverlapBoundary: "owns module scope assessment", ExecutionWave: "static"})
			}
			claims = append(claims, Claim{ClaimID: verifyID, Lens: "qa", Target: "module:" + candidate.ModuleID, Assertion: "module contracts remain valid across the recorded impact chain", Oracle: "TODO(planner): name integration inputs and observable pass/fail outcomes", Method: "integration execution", Applicability: "required", SourceRefs: []string{"module:" + candidate.ModuleID, ModuleRelationsPath}, FocusKey: "module-integration:" + candidate.ModuleID, DependsOn: []string{assessmentID}})
			assignments = append(assignments, PlanAssignment{AssignmentID: "assignment-integration-" + candidate.ModuleID, Lens: "qa", ClaimIDs: []string{verifyID}, NonOverlapBoundary: "owns integration execution for " + candidate.ModuleID, ExecutionWave: "behavior"})
		}
		scope.Dispositions = append(scope.Dispositions, d)
	}
	if plan.E2ECoverageState == "cold_start" {
		workspace := fmt.Sprintf("e2e-workspace/%s", plan.ReviewPlanID)
		plan.VerificationArtifactWorkspace = &workspace
	} else {
		claims = append(claims, Claim{ClaimID: "claim-e2e-na-1", Lens: "e2e", Target: "scoped module assessments", Assertion: "no browser obligation remains after independent module assessment", Oracle: "all module scope assessment Claims pass against fingerprinted sources", Method: "impact analysis", Applicability: "not_applicable", NARationale: "every non-browser disposition must be independently assessed and verified", NAChecklistID: "module-scope-v1", SourceRefs: []string{ModuleRelationsPath}})
	}
	plan.Claims = claims
	plan.Assignments = assignments
	plan.ModuleScope = &scope
	notes = append(notes, fmt.Sprintf("module scope: %d candidates, resolution=%s; module-qualified CASE/PATH identities are mandatory; refine non-browser decisions and their independent assessment oracles before registration", len(scope.Candidates), scope.Resolution))
	return plan, notes
}
func mapValue(v any) map[string]any { m, _ := v.(map[string]any); return m }

// module-qualified asset identity is required in the new protocol.
func validateScopedAssetIdentity(plan *Plan) error {
	if plan.SchemaVersion != "1.1.0" {
		return nil
	}
	for _, a := range plan.E2EAssets {
		if !validScopePath(a.CasesPath, false) || strings.TrimSpace(a.ModuleID) == "" {
			return fmt.Errorf("E2E asset %s needs cases_path and module_id", a.AssetID)
		}
	}
	return nil
}
