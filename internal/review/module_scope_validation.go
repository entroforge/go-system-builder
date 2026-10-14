package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

func requireScopedPlan(plan *Plan) error {
	if plan == nil || plan.SchemaVersion != "1.1.0" || plan.ModuleScope == nil {
		return fmt.Errorf("new S7 execution requires ReviewPlan 1.1.0 with module_scope; historical 1.0 plans are read-only with this binary (finish active legacy rounds with their matching release)")
	}
	return nil
}

// ValidateModuleScopeObligations proves module-qualified CASE -> Claim ->
// Assignment coverage and independent assessment of non-browser decisions.
func ValidateModuleScopeObligations(plan *Plan) error {
	if err := requireScopedPlan(plan); err != nil {
		return err
	}
	s := plan.ModuleScope
	var problems []error
	if len(s.UnresolvedInputs) > 0 {
		problems = append(problems, fmt.Errorf("module scope unresolved inputs: %s", strings.Join(s.UnresolvedInputs, "; ")))
	}
	if len(s.Candidates) == 0 {
		problems = append(problems, fmt.Errorf("module scope has no candidates"))
	}
	claims := map[string]Claim{}
	owners := map[string]PlanAssignment{}
	counts := map[string]int{}
	for _, c := range plan.Claims {
		claims[c.ClaimID] = c
	}
	for _, a := range plan.Assignments {
		for _, id := range a.ClaimIDs {
			owners[id] = a
			counts[id]++
		}
	}
	refs := map[string]ScopeInputRef{}
	for _, r := range s.InputRefs {
		refs[r.SourceKind+":"+r.Path] = r
	}
	dispositions := map[string]ModuleDisposition{}
	for _, d := range s.Dispositions {
		if _, dup := dispositions[d.ModuleID]; dup {
			problems = append(problems, fmt.Errorf("duplicate module disposition: %s", d.ModuleID))
		}
		dispositions[d.ModuleID] = d
	}
	checkClaims := func(module string, ids []string, lens, wave, focus string) {
		if len(ids) == 0 {
			problems = append(problems, fmt.Errorf("module %s has no %s claim", module, focus))
		}
		for _, id := range ids {
			c, ok := claims[id]
			a := owners[id]
			if !ok || c.Applicability != "required" || c.Lens != lens || a.Lens != lens || a.ExecutionWave != wave || counts[id] != 1 || !containsString(c.SourceRefs, "module:"+module) || (focus != "verification" && c.FocusKey != "module-scope:"+module) {
				problems = append(problems, fmt.Errorf("module %s: %s must bind a required %s %s Claim/Assignment with module source and concrete assessment focus: %s", module, focus, lens, wave, id))
			}
		}
	}
	candidates := map[string]bool{}
	allowed := map[string]bool{}
	for _, c := range s.Candidates {
		if candidates[c.ModuleID] {
			problems = append(problems, fmt.Errorf("duplicate module candidate %s", c.ModuleID))
		}
		candidates[c.ModuleID] = true
		for _, r := range c.RequirementRefs {
			for _, p := range r.PathIDs {
				allowed[r.CasesPath+"#"+r.CaseID+"#"+p] = true
			}
		}
		d, ok := dispositions[c.ModuleID]
		if !ok {
			problems = append(problems, fmt.Errorf("module %s has no disposition", c.ModuleID))
			continue
		}
		if strings.TrimSpace(d.Rationale) == "" {
			problems = append(problems, fmt.Errorf("module %s disposition needs rationale", c.ModuleID))
		}
		switch d.Action {
		case "browser_regression":
			checkClaims(c.ModuleID, d.VerificationClaimIDs, "e2e", "behavior", "verification")
			covered := map[string]bool{}
			for _, id := range d.VerificationClaimIDs {
				for _, r := range claims[id].ScenarioRefs {
					for _, p := range r.PathIDs {
						covered[r.CasesPath+"#"+r.CaseID+"#"+p] = true
					}
				}
			}
			if len(c.RequirementRefs) == 0 {
				problems = append(problems, fmt.Errorf("module %s browser regression has no required CASE/PATH obligations", c.ModuleID))
			}
			for _, r := range c.RequirementRefs {
				for _, p := range r.PathIDs {
					key := r.CasesPath + "#" + r.CaseID + "#" + p
					if !covered[key] {
						problems = append(problems, fmt.Errorf("module %s missing required browser scenario %s", c.ModuleID, key))
					}
				}
			}
			if plan.E2ECoverageState == "not_applicable" {
				problems = append(problems, fmt.Errorf("module %s browser obligation contradicts e2e_coverage_state", c.ModuleID))
			}
			if plan.E2ECoverageState == "regression_available" {
				for _, r := range c.RequirementRefs {
					mapped := false
					for _, a := range plan.E2EAssets {
						if a.CasesPath == r.CasesPath && a.ModuleID == c.ModuleID && a.CaseRef == r.CaseID {
							mapped = true
						}
					}
					if !mapped {
						problems = append(problems, fmt.Errorf("module %s CASE %s has no module-qualified regression asset", c.ModuleID, r.CaseID))
					}
				}
			}
		case "integration_verification", "no_behavior_impact":
			if c.DirectUI {
				problems = append(problems, fmt.Errorf("module %s direct UI/CASE change cannot waive browser obligations", c.ModuleID))
			}
			if len(d.EvidenceRefs) == 0 {
				problems = append(problems, fmt.Errorf("module %s exclusion requires fingerprinted source evidence", c.ModuleID))
			}
			for _, r := range d.EvidenceRefs {
				expected, ok := refs[r.SourceKind+":"+r.Path]
				if !ok || !r.Present || r != expected {
					problems = append(problems, fmt.Errorf("module %s exclusion evidence is not an authoritative input: %s", c.ModuleID, r.Path))
				}
			}
			checkClaims(c.ModuleID, d.AssessmentClaimIDs, "qa", "static", "scope-assessment")
			if d.Action == "integration_verification" {
				checkClaims(c.ModuleID, d.VerificationClaimIDs, "qa", "behavior", "verification")
				for _, id := range d.VerificationClaimIDs {
					for _, assessment := range d.AssessmentClaimIDs {
						if owners[id].AssignmentID == owners[assessment].AssignmentID {
							problems = append(problems, fmt.Errorf("module %s scope assessment and integration need independent Assignments", c.ModuleID))
						}
						if !containsString(claims[id].DependsOn, assessment) {
							problems = append(problems, fmt.Errorf("module %s integration must depend on scope assessment %s", c.ModuleID, assessment))
						}
					}
				}
			} else if len(d.VerificationClaimIDs) != 0 {
				problems = append(problems, fmt.Errorf("module %s no_behavior_impact must carry assessment obligations only", c.ModuleID))
			}
		default:
			problems = append(problems, fmt.Errorf("module %s unresolved/unknown disposition %q", c.ModuleID, d.Action))
		}
	}
	for id := range dispositions {
		if !candidates[id] {
			problems = append(problems, fmt.Errorf("disposition %s is outside the authoritative candidate set", id))
		}
	}
	for _, c := range plan.Claims {
		for _, r := range c.ScenarioRefs {
			for _, p := range r.PathIDs {
				if !allowed[r.CasesPath+"#"+r.CaseID+"#"+p] {
					problems = append(problems, fmt.Errorf("claim %s has unknown scenario identity %s#%s#%s", c.ClaimID, r.CasesPath, r.CaseID, p))
				}
			}
		}
	}
	return errors.Join(problems...)
}
func validatePreparedScope(plan *Plan, p *preparedModuleScope) error {
	if err := ValidateModuleScopeObligations(plan); err != nil {
		return err
	}
	// This source commit becomes historical provenance in S9. Verify its
	// content, while allowing later unrelated verification commits at HEAD.
	original, err := fileview.New(p.root, plan.ModuleScope.SourceCommit, []fileview.Rule{{Path: ".", Source: "git_tree"}})
	if err != nil {
		return fmt.Errorf("module scope source_commit cannot be verified: %w", err)
	}
	for _, ref := range plan.ModuleScope.InputRefs {
		if ref.SourceKind != "git_tree" {
			continue
		}
		data, err := original.ReadFile(ref.Path)
		if ref.Present {
			if err != nil || sha256Of(data) != ref.SHA256 {
				return fmt.Errorf("module scope source_commit does not supply pinned input %s", ref.Path)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("module scope missing-input provenance differs: %s", ref.Path)
		}
	}
	got, want := *plan.ModuleScope, p.scope
	got.Dispositions = nil
	want.Dispositions = nil
	got.SourceCommit = ""
	want.SourceCommit = ""
	if scopeJSONDigest(got) != scopeJSONDigest(want) {
		return fmt.Errorf("module scope differs from authoritative inputs/candidates; re-draft and review dispositions (cannot delete candidates or change the denominator)")
	}
	return nil
}
func ValidateModuleScope(root string, state map[string]any, plan *Plan) error {
	if err := requireScopedPlan(plan); err != nil {
		return err
	}
	p, err := prepareModuleScope(root, state, plan.ReviewRound, workspaceValue(plan.VerificationArtifactWorkspace))
	if err != nil {
		return err
	}
	return validatePreparedScope(plan, p)
}
func ValidateModuleScopeResults(root string, state map[string]any, plan *Plan) error {
	if err := ValidateModuleScope(root, state, plan); err != nil {
		return err
	}
	return ValidateConsumedScopeForRoot(root, state, plan)
}

func validateModuleScopeConsumed(root string, state map[string]any, plan *Plan, pendingID string, pendingBytes []byte) error {
	dispositions := Dispositions(state)
	seen := map[string]bool{}
	for _, d := range plan.ModuleScope.Dispositions {
		for _, id := range append(append([]string{}, d.VerificationClaimIDs...), d.AssessmentClaimIDs...) {
			seen[id] = true
		}
	}
	for _, c := range plan.Claims {
		if c.Applicability == "required" {
			seen[c.ClaimID] = true
		}
	}
	ptr := PlanPointerFromState(state)
	rows := mapValue(mapValue(state["review"])["assignments"])
	for _, id := range sortedKeys(seen) {
		disp := dispositions[id]
		if disp.Disposition != "pass" || disp.ResultID == "" {
			return fmt.Errorf("scope obligation %s has no consumed pass", id)
		}
		data, err := loadIndexedEvidenceArtifact(root, state, disp.ResultID, "review_result")
		if disp.ResultID == pendingID {
			data = pendingBytes
			err = nil
		}
		if err != nil {
			return err
		}
		var envelope map[string]any
		if json.Unmarshal(data, &envelope) != nil || envelope["runtime_id"] != state["runtime_id"] || envelope["evidence_id"] != disp.ResultID {
			return fmt.Errorf("scope obligation %s Result Runtime/index binding mismatch", id)
		}
		var result Result
		if err = json.Unmarshal(data, &result); err != nil {
			return err
		}
		if result.ReviewPlanID != plan.ReviewPlanID || result.ReviewRound != plan.ReviewRound || result.BaselineGeneration != plan.BaselineGeneration || result.AssignmentID != disp.AssignmentID || result.SubjectDigest != SubjectDigest(plan) || result.Verdict != "pass" {
			return fmt.Errorf("scope obligation %s Result binding mismatch", id)
		}
		row := mapValue(rows[disp.AssignmentID])
		if ptr == nil || row["status"] != "consumed" || row["result_ref"] == nil || result.AssignmentRevision < 1 || result.AssignmentRevision > ptr.Revision {
			return fmt.Errorf("scope obligation %s assignment is not currently consumed", id)
		}
		assignment := findPlanAssignment(plan, disp.AssignmentID)
		if assignment == nil || validateClaimResultSet(assignment, &result) != nil || stringField(row["agent_id"]) != result.ProducerAgentID {
			return fmt.Errorf("scope obligation %s Result responsibility/claim set mismatch", id)
		}
		for _, raw := range evidenceEntries(state) {
			indexed := mapValue(raw)
			if indexed["id"] == disp.ResultID && indexed["path"] != row["result_ref"] {
				return fmt.Errorf("scope obligation %s Result path differs from its immutable index", id)
			}
		}
		if assignment.Lens == "e2e" && ptr.VerificationArtifactWorkspace != "" {
			if result.VerificationArtifactDigest == nil || *result.VerificationArtifactDigest == "" || row["artifact_digest"] != *result.VerificationArtifactDigest {
				return fmt.Errorf("scope obligation %s workspace digest differs from its immutable Result", id)
			}
		}
		if err := validateScopeProducerIndependence(state, plan, &result); err != nil {
			return err
		}
		pass := false
		for _, cr := range result.ClaimResults {
			if cr.ClaimID == id && cr.Conclusion == "pass" {
				pass = true
			}
		}
		if !pass {
			return fmt.Errorf("scope obligation %s is not a Result pass", id)
		}
	}
	return nil
}

func validateScopeProducerIndependence(state map[string]any, plan *Plan, result *Result) error {
	if result.ProducerAgentID == plan.CreatedBy {
		return fmt.Errorf("Planner cannot certify its own scope decisions")
	}
	assessment := map[string]bool{}
	for _, d := range plan.ModuleScope.Dispositions {
		for _, id := range d.AssessmentClaimIDs {
			assessment[id] = true
		}
	}
	evaluating := false
	for _, cr := range result.ClaimResults {
		if assessment[cr.ClaimID] {
			evaluating = true
		}
	}
	if !evaluating {
		return nil
	}
	for _, raw := range stateDocuments(state) {
		d, _ := raw.(map[string]any)
		if d["author_agent_id"] == result.ProducerAgentID {
			return fmt.Errorf("scope assessor %s authored a locked input and cannot self-certify exclusion", result.ProducerAgentID)
		}
	}
	for _, raw := range evidenceEntries(state) {
		e, _ := raw.(map[string]any)
		if !builderDeliveryEvidenceKinds[stringField(e["kind"])] {
			continue
		}
		for _, producer := range stringSliceValue(e["produced_by"]) {
			if producer == result.ProducerAgentID {
				return fmt.Errorf("scope assessor %s authored cumulative S6 changes", producer)
			}
		}
	}
	return nil
}
func changedModuleScopeClaims(old, next *Plan) []string {
	if scopeJSONDigest(old.ModuleScope) == scopeJSONDigest(next.ModuleScope) {
		return nil
	}
	ids := map[string]bool{}
	for _, p := range []*Plan{old, next} {
		if p.ModuleScope != nil {
			for _, d := range p.ModuleScope.Dispositions {
				for _, id := range append(append([]string{}, d.AssessmentClaimIDs...), d.VerificationClaimIDs...) {
					ids[id] = true
				}
			}
		}
	}
	return sortedKeys(ids)
}

// PrepareModuleScopeValidation freezes graph discovery outside the Runtime
// lock and returns a capability tied to the authority and pinned Plan identity.
func PrepareModuleScopeValidation(root string, state map[string]any, plan *Plan) (func(map[string]any) error, error) {
	if err := requireScopedPlan(plan); err != nil {
		return nil, err
	}
	prepared, err := prepareModuleScope(root, state, plan.ReviewRound, workspaceValue(plan.VerificationArtifactWorkspace))
	if err != nil {
		return nil, err
	}
	if err := validatePreparedScope(plan, prepared); err != nil {
		return nil, err
	}
	original := PlanPointerFromState(state)
	return func(current map[string]any) error {
		if original != nil {
			ptr := PlanPointerFromState(current)
			if ptr == nil || ptr.SHA256 != original.SHA256 || ptr.Revision != original.Revision {
				return fmt.Errorf("prepared module scope Plan pointer moved; prepare again")
			}
		}
		return prepared.Verify(current)
	}, nil
}

// ValidateConsumedScopeForRoot performs cheap file/projection checks after
// the caller verifies a prepared freshness capability or runs the resolver.
func ValidateConsumedScopeForRoot(root string, state map[string]any, plan *Plan) error {
	if err := requireScopedPlan(plan); err != nil {
		return err
	}
	if err := verifyFrozenSubjects(root, plan, state); err != nil {
		return err
	}
	if err := verifyRegressionAssetFingerprints(root, plan); err != nil {
		return err
	}
	ptr := PlanPointerFromState(state)
	if ptr == nil {
		return fmt.Errorf("no current scoped Plan pointer")
	}
	if err := verifySealedArtifactDigests(root, ptr, mapValue(mapValue(state["review"])["assignments"])); err != nil {
		return err
	}
	return validateModuleScopeConsumed(root, state, plan, "", nil)
}

// ReadIndexedArtifact verifies a Runtime-indexed immutable artifact's hash.
func ReadIndexedArtifact(root string, state map[string]any, id, kind string) ([]byte, error) {
	return loadIndexedEvidenceArtifact(root, state, id, kind)
}
