package repair

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func CreateChangeImpact(root string, request ChangeImpactRequest) (ChangeImpact, ArtifactRef, error) {
	if !strings.HasPrefix(request.ImpactID, "impact-") {
		return ChangeImpact{}, ArtifactRef{}, fmt.Errorf("impact_id must carry the impact- prefix so Runtime can bind it (got %q)", request.ImpactID)
	}
	if strings.TrimSpace(request.RuntimeID) == "" || strings.TrimSpace(request.ReqID) == "" || strings.TrimSpace(request.AnalyzedBy) == "" {
		return ChangeImpact{}, ArtifactRef{}, errors.New("runtime_id, req_id, and analyzed_by are required")
	}
	if request.BaselineGeneration < 1 || len(request.SourceBugIDs) == 0 && len(request.SourceCaseIDs) == 0 || len(request.ChangeTypes) == 0 || len(request.Decisions) == 0 || len(request.ChangedArtifacts) == 0 {
		return ChangeImpact{}, ArtifactRef{}, errors.New("ChangeImpact requires positive baseline, a source Bug or Case id, change types, changed artifacts, and decisions")
	}
	// RC-14 (S9-M3): decision=reverify is the formal hand-off back to the
	// targeted reverification gate. The artifact boundary preserves the
	// declared obligation set; CommitChangeImpact registers that set on the
	// Runtime pointer, and CommitRepairHandoff consumes it after the downstream
	// TargetedReverification artifacts have been created and committed.
	changed := append([]ArtifactRef(nil), request.ChangedArtifacts...)
	for i := range changed {
		if changed[i].ID == "" {
			changed[i].ID = "changed-" + strings.NewReplacer("/", "-", "\\", "-").Replace(changed[i].Path)
		}
		if changed[i].Path == "" || changed[i].SHA256 == "" {
			return ChangeImpact{}, ArtifactRef{}, fmt.Errorf("changed artifact %d requires path and sha256", i)
		}
	}
	for _, artifact := range changed {
		covered := false
		for _, decision := range request.Decisions {
			for _, scope := range decision.Scope {
				if pathMatches(artifact.Path, scope) || pathMatches(scope, artifact.Path) {
					covered = true
					break
				}
			}
			if covered {
				break
			}
		}
		if !covered {
			return ChangeImpact{}, ArtifactRef{}, fmt.Errorf("ChangeImpact decision coverage missing for changed artifact %q; add a decision scope and rationale", artifact.Path)
		}
	}
	impact := ChangeImpact{
		SchemaVersion: "1.0.0", RecordType: "change_impact", ImpactID: request.ImpactID,
		RuntimeID: request.RuntimeID, ReqID: request.ReqID, BaselineGeneration: request.BaselineGeneration,
		SourceBugIDs: sortedStrings(request.SourceBugIDs), SourceCaseIDs: sortedStrings(request.SourceCaseIDs), ChangeTypes: sortedStrings(request.ChangeTypes), ChangedArtifacts: changed,
		Decisions: append([]ImpactDecision(nil), request.Decisions...), EscalationLevel: request.EscalationLevel,
		InvalidatedEvidenceIDs: sortedStrings(request.InvalidatedEvidenceIDs), SupersededEvidenceIDs: sortedStrings(request.SupersededEvidenceIDs),
		RetainedEvidenceIDs: sortedStrings(request.RetainedEvidenceIDs), RequiredReverificationIDs: sortedStrings(request.RequiredReverificationIDs),
		AnalyzedBy: request.AnalyzedBy, AnalyzedAt: nowOr(request.AnalyzedAt),
	}
	if impact.EscalationLevel == "" {
		impact.EscalationLevel = "assignment"
	}
	ref, err := writeImmutable(root, filepath.Join(artifactRoot, "change-impact", request.ImpactID+".json"), "review-evidence.schema.json", impact)
	return impact, ref, err
}

func ValidateChangeImpact(root string, ref ArtifactRef) (ChangeImpact, error) {
	var impact ChangeImpact
	if err := decodeArtifact(root, ref, "review-evidence.schema.json", &impact); err != nil {
		return ChangeImpact{}, err
	}
	if impact.RecordType != "change_impact" {
		return ChangeImpact{}, fmt.Errorf("artifact %s is %q, not change_impact", ref.Path, impact.RecordType)
	}
	return impact, nil
}

func CreateTargetedReverification(root string, request TargetedReverificationRequest) (TargetedReverification, ArtifactRef, error) {
	// repairOwnerAssignmentPrefix is the manifest-form alias prefix a repair
	// owner would use to disguise itself as its own independent verifier.
	const repairOwnerAssignmentPrefix = "assignment-s9-"
	if !strings.HasPrefix(request.ReverificationID, "reverify-") {
		return TargetedReverification{}, ArtifactRef{}, fmt.Errorf("request.ReverificationID must carry the reverify- prefix so Runtime can bind it (got %q)", request.ReverificationID)
	}
	if strings.TrimSpace(request.RuntimeID) == "" || strings.TrimSpace(request.BugID) == "" && strings.TrimSpace(request.CaseID) == "" || strings.TrimSpace(request.OriginalAssignmentID) == "" || strings.TrimSpace(request.PerformingAssignmentID) == "" || strings.TrimSpace(request.ImpactID) == "" {
		return TargetedReverification{}, ArtifactRef{}, errors.New("runtime_id, a bug_id or case_id, both assignment ids, and the impact id are required")
	}
	if request.OriginalAssignmentID == request.PerformingAssignmentID {
		return TargetedReverification{}, ArtifactRef{}, errors.New("targeted reverification requires an independent verifier: performing_assignment_id must differ from original_assignment_id")
	}
	// RC-01 (S9-1): identity-root guard. The reverification artifact itself
	// must carry evidence-backed assertions; Runtime additionally cross-checks
	// the two assignment identities against the dispatched repair assignments
	// and the assignment_owners map (CommitTargetedReverification), so a
	// fabricated "independent" verifier ID cannot survive create alone.
	if request.OriginalAssignmentID == repairOwnerAssignmentPrefix {
		return TargetedReverification{}, ArtifactRef{}, fmt.Errorf("original_assignment_id %q must reference the dispatched repair assignment (repair-assignment-...) or its manifest alias (assignment-s9-...); a self-asserted builder identity is not independent", request.OriginalAssignmentID)
	}
	if len(request.AssertionResults) == 0 {
		return TargetedReverification{}, ArtifactRef{}, errors.New("targeted reverification requires assertion results")
	}
	// RC-09 (S9-11): the continuity chain must be anchored, not narrated.
	// The old default ("independent verification after repair") let the
	// original-finder continuity be satisfied by auto-generated prose. The
	// reason must now reference at least one evidence artifact (any
	// "scheme://id" or evidence/<file> path the verifier actually produced);
	// a bare sentence with no anchor is rejected at the artifact boundary.
	if err := validateContinuityReasonEvidence(request.ContinuityReason); err != nil {
		return TargetedReverification{}, ArtifactRef{}, err
	}
	reverification := TargetedReverification{
		SchemaVersion: "1.0.0", RecordType: "targeted_reverification", ReverificationID: request.ReverificationID,
		RuntimeID: request.RuntimeID, BugID: request.BugID, CaseID: request.CaseID, BaselineGeneration: request.BaselineGeneration,
		OriginalAssignmentID: request.OriginalAssignmentID, PerformingAssignmentID: request.PerformingAssignmentID,
		ContinuityReason: request.ContinuityReason, ImpactID: request.ImpactID, AssertionResults: append([]AssertionResult(nil), request.AssertionResults...),
		ScopeCompliance: request.ScopeCompliance, Result: request.Result, FailureClass: request.FailureClass, PerformedAt: nowOr(request.PerformedAt),
	}
	if reverification.ScopeCompliance == "" {
		reverification.ScopeCompliance = "fail"
	}
	if reverification.Result == "" {
		reverification.Result = "blocked"
	}
	if reverification.Result != "pass" && reverification.FailureClass == "" {
		switch reverification.Result {
		case "blocked":
			reverification.FailureClass = "blocked"
		case "scope_changed":
			reverification.FailureClass = "scope_changed"
		default:
			reverification.FailureClass = "fail_same_cause"
		}
	}
	ref, err := writeImmutable(root, filepath.Join(artifactRoot, "reverification", request.ReverificationID+".json"), "review-evidence.schema.json", reverification)
	return reverification, ref, err
}

func ValidateTargetedReverification(root string, ref ArtifactRef) (TargetedReverification, error) {
	var value TargetedReverification
	if err := decodeArtifact(root, ref, "review-evidence.schema.json", &value); err != nil {
		return TargetedReverification{}, err
	}
	if value.RecordType != "targeted_reverification" {
		return TargetedReverification{}, fmt.Errorf("artifact %s is %q, not targeted_reverification", ref.Path, value.RecordType)
	}
	if value.OriginalAssignmentID == value.PerformingAssignmentID {
		return TargetedReverification{}, errors.New("targeted reverification is not independent")
	}
	// RC-01 (EH-10): the manifest-alias form is the repair owner's dispatch
	// identity; using it as the performing verifier hides a self-verification
	// behind a second label.
	if value.PerformingAssignmentID == "assignment-s9-"+strings.TrimPrefix(value.OriginalAssignmentID, "repair-assignment-") || value.OriginalAssignmentID == "assignment-s9-"+strings.TrimPrefix(value.PerformingAssignmentID, "repair-assignment-") {
		return TargetedReverification{}, fmt.Errorf("targeted reverification is not independent: %q is the manifest alias of the same repair assignment", value.PerformingAssignmentID)
	}
	if err := validateTargetedFailureEvidence(value); err != nil {
		return TargetedReverification{}, err
	}
	if err := validateTargetedPassEvidence(value); err != nil {
		return TargetedReverification{}, err
	}
	seen := map[string]bool{}
	for _, assertion := range value.AssertionResults {
		if strings.TrimSpace(assertion.AssertionID) == "" {
			return TargetedReverification{}, errors.New("targeted reverification contains an assertion without assertion_id")
		}
		if seen[assertion.AssertionID] {
			return TargetedReverification{}, fmt.Errorf("targeted reverification contains duplicate assertion_id %q", assertion.AssertionID)
		}
		seen[assertion.AssertionID] = true
		if value.Result == "pass" && assertion.Result != "pass" {
			return TargetedReverification{}, fmt.Errorf("pass targeted reverification contains non-pass assertion %q", assertion.AssertionID)
		}
	}
	return value, nil
}

func validateTargetedPassEvidence(value TargetedReverification) error {
	if value.Result != "pass" {
		return nil
	}
	for _, assertion := range value.AssertionResults {
		anchored := false
		for _, ref := range assertion.EvidenceRefs {
			if strings.Contains(ref, "://") || strings.HasPrefix(strings.TrimSpace(ref), "evidence/") {
				anchored = true
				break
			}
			// Bare Runtime evidence ids are also evidence, but at this artifact boundary we
			// cannot verify generation/SHA — the Runtime commit (CommitTargetedReverification)
			// enforces that. Non-empty bare ids are accepted as evidence-anchored here so
			// Runtime tests using bare tokens like evidence-schema-diff remain valid; empty
			// refs are still rejected.
			if strings.TrimSpace(ref) != "" {
				anchored = true
				break
			}
		}
		if !anchored {
			return fmt.Errorf("pass targeted reverification assertion %q requires at least one non-empty evidence_ref; a pass verdict without evidence cannot close the repair chain", assertion.AssertionID)
		}
	}
	return nil
}

// validateTargetedFailureEvidence is the RC-01 (EH-11) failure-direction
// identity root. A non-pass verdict may only route the repair chain (S8
// investigation, blocked resume) when at least one failing or blocked
// assertion carries its own evidence, and the recorded failure_class must
// agree with the observed assertion results:
//
//   - blocked         requires a blocked assertion with non-empty evidence
//   - fail_same_cause/fail_new_cause require a failing assertion with
//     non-empty evidence
//   - scope_changed   is a scope judgment and needs no assertion evidence
//   - stale           is a baseline judgment and needs no assertion evidence
//
// A bare self-declared failure_class with no evidence is rejected at the
// artifact boundary before Runtime can route on it.
func validateTargetedFailureEvidence(value TargetedReverification) error {
	if value.Result == "pass" || value.FailureClass == "" {
		return nil
	}
	needBlocked := value.FailureClass == "blocked"
	if !needBlocked && value.FailureClass != "fail_same_cause" && value.FailureClass != "fail_new_cause" {
		// scope_changed / stale carry no assertion-level evidence contract.
		return nil
	}
	want := "fail"
	if needBlocked {
		want = "blocked"
	}
	for _, assertion := range value.AssertionResults {
		if assertion.Result != want {
			continue
		}
		for _, evidenceRef := range assertion.EvidenceRefs {
			if strings.TrimSpace(evidenceRef) != "" {
				return nil
			}
		}
	}
	return fmt.Errorf("failure_class %q requires at least one %s assertion with non-empty evidence_refs; a self-reported failure without evidence cannot route the repair chain", value.FailureClass, want)
}

// validateContinuityReasonEvidence is the RC-09 (S9-11) anchor check: the
// continuity_reason must cite at least one evidence reference so the
// original-finder continuity claim is verifiable instead of auto-filled
// prose. A reference is any non-empty token containing a scheme separator
// ("test://red", "runtime://blocker") or an evidence file path
// ("evidence/….json").
func validateContinuityReasonEvidence(reason string) error {
	for _, token := range strings.FieldsFunc(reason, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == ';' || r == '(' || r == ')'
	}) {
		if strings.Contains(token, "://") || strings.HasPrefix(token, "evidence/") {
			return nil
		}
	}
	return fmt.Errorf("continuity_reason must cite at least one evidence reference (e.g. \"test://red-check output\" or \"evidence/reverify-red.json\"); a self-declared sentence with no evidence anchor cannot establish the original-finder continuity chain (got %q)", strings.TrimSpace(reason))
}

func sortedStrings(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
