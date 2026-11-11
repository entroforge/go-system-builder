package reviewmeta

import "testing"

func TestEntryIdentityStableAcrossRetriesAndDifferentForSameBaselineReentry(t *testing.T) {
	first := NewPlanID("runtime", "event-original")
	if first == "" || first != NewPlanID("runtime", "event-original") || first == NewPlanID("runtime", "event-reentry") || first == NewPlanID("other-runtime", "event-original") {
		t.Fatal("entry event identity must remain stable and distinguish real entries")
	}
	if NewPlanID("", "event") != "" || NewPlanID("runtime", "") != "" {
		t.Fatal("missing source must stay unknown")
	}
}

func TestIdentityComparisonUsesPlanAndPreservesUnknown(t *testing.T) {
	for _, tc := range []struct {
		a, b Identity
		want Match
	}{
		{Identity{}, Identity{}, Unknown},
		{ArtifactIdentity("a"), ArtifactIdentity(""), Unknown},
		{ArtifactIdentity(""), ArtifactIdentity(""), Unknown},
		{ArtifactIdentity("a"), ArtifactIdentity("a"), Same},
		{ArtifactIdentity("a"), ArtifactIdentity("b"), Different},
	} {
		if got := CompareIdentity(tc.a, tc.b); got != tc.want {
			t.Fatalf("%+v / %+v: %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestActiveEntryDoesNotBorrowPreviousPlan(t *testing.T) {
	review := map[string]any{"round": 9, "plan": map[string]any{"plan_id": "old"}, "round_entry": map[string]any{"plan_id": "new"}}
	if got := FromReview(review); got.PlanID != "new" {
		t.Fatalf("new entry borrowed history: %+v", got)
	}
	review["round_entry"] = map[string]any{"round": 2}
	if got := FromReview(review); got.PlanID != "" {
		t.Fatalf("absent plan must remain unknown: %+v", got)
	}
	delete(review, "round_entry")
	if got := FromReview(review); got.PlanID != "old" {
		t.Fatalf("original plan lost: %+v", got)
	}
}
