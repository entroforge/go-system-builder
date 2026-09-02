package review

import (
	"fmt"
	"sort"
	"strings"
)

// typedEvidenceKinds is intentionally small. A Claim declares the kind of
// observation it needs; the concrete ref may come from a product-side wrapper,
// a local artifact, or a registered Runtime evidence row.
var typedEvidenceKinds = map[string]struct{}{
	"console":    {},
	"network":    {},
	"path":       {},
	"runtime":    {},
	"screenshot": {},
	"state":      {},
	"timeline":   {},
	"trace":      {},
}

func sortedTypedEvidenceKinds() []string {
	kinds := make([]string, 0, len(typedEvidenceKinds))
	for kind := range typedEvidenceKinds {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

func validatePlanEvidenceRequirements(plan *Plan) error {
	for _, claim := range plan.Claims {
		for _, rawRequirement := range claim.RequiredEvidence {
			requirement := strings.ToLower(strings.TrimSpace(rawRequirement))
			if _, ok := typedEvidenceKinds[requirement]; ok {
				continue
			}
			return s7GateError(
				"S7_PLAN_EVIDENCE_KIND",
				fmt.Sprintf("claim %s declares unknown required evidence kind %q", claim.ClaimID, rawRequirement),
				[]string{fmt.Sprintf("claim %s requires %q, which is not in the S7 evidence vocabulary", claim.ClaimID, rawRequirement)},
				[]string{"replace it with one of: " + strings.Join(sortedTypedEvidenceKinds(), ", ")},
				"runtime review-plan --file plan.json",
			)
		}
	}
	return nil
}

func evidenceRefKind(ref string) string {
	ref = strings.TrimSpace(ref)
	if index := strings.IndexByte(ref, ':'); index > 0 {
		return strings.ToLower(strings.TrimSpace(ref[:index]))
	}
	return ""
}

func evidenceRefMatchesRequirement(ref, requirement string) bool {
	refKind := evidenceRefKind(ref)
	requirement = strings.ToLower(strings.TrimSpace(requirement))
	// A trace may be a local immutable artifact. Browser/product evidence
	// kinds remain strict: a path to a report cannot satisfy console or
	// network merely because it is non-empty.
	if requirement == "trace" && refKind == "path" {
		return true
	}
	return refKind == requirement
}
