package acceptance

import "github.com/entroforge/go-system-builder/internal/reviewmeta"

// CurrentReviewIdentity reads the saved professional responsibility, preserving
// absent legacy numbers instead of turning them into a shared round zero.
func CurrentReviewIdentity(state map[string]any) reviewmeta.Identity {
	review, _ := state["review"].(map[string]any)
	return reviewmeta.FromReview(review)
}

// CurrentReviewArtifact selects facts whose original source is known to be the
// active review. Unknown material remains unresolved; it is not a negative
// professional conclusion and must not be promoted to a current PASS.
func CurrentReviewArtifact(state map[string]any, planID string) bool {
	return reviewmeta.CompareIdentity(CurrentReviewIdentity(state), reviewmeta.ArtifactIdentity(planID)) == reviewmeta.Same
}

// StampReviewIdentity copies the existing source association to machine-owned
// material. New responsibilities carry no numeric review_round.
func StampReviewIdentity(state map[string]any, artifact map[string]any) {
	identity := CurrentReviewIdentity(state)
	if identity.PlanID != "" {
		artifact["review_plan_id"] = identity.PlanID
	}
	delete(artifact, "review_round")
}
