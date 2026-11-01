package review

import "github.com/entroforge/go-system-builder/internal/reviewmeta"

func activeReviewIdentity(state map[string]any) reviewmeta.Identity {
	review, _ := state["review"].(map[string]any)
	return reviewmeta.FromReview(review)
}

func rowReviewIdentity(row map[string]any) reviewmeta.Identity {
	return reviewmeta.Identity{PlanID: stringField(row["review_plan_id"])}
}

// reviewArtifactKey identifies the active review by its Plan, never a counter.
func reviewArtifactKey(state map[string]any) string {
	identity := activeReviewIdentity(state)
	return identity.PlanID
}

// UnresolvedFindingIdentity reports a stored Finding whose original review
// relationship is unknown. It is neither assigned to today's Plan nor treated
// as a known historical Finding. This read-only diagnostic does not gate
// completion of the current professional responsibilities.
func UnresolvedFindingIdentity(state map[string]any) bool {
	entities, _ := state["entities"].(map[string]any)
	findings, _ := entities["findings"].([]any)
	for _, raw := range findings {
		row, _ := raw.(map[string]any)
		if row != nil && reviewmeta.CompareIdentity(rowReviewIdentity(row), activeReviewIdentity(state)) == reviewmeta.Unknown {
			return true
		}
	}
	return false
}
