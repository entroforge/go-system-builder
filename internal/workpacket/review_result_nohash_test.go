package workpacket

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReviewResultOriginalResponsibilityHasNoNewDigest(t *testing.T) {
	binding := reviewResultTestBinding()
	first, err := BuildReviewResult(binding, reviewResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildReviewResult(binding, reviewResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.ResultID != second.ResultID {
		t.Fatal("replay changed the original responsibility identity")
	}
	binding.AssignmentID = "assignment-other"
	third, err := BuildReviewResult(binding, reviewResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	if third.ID == first.ID {
		t.Fatal("different assignment reused a packet identity")
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"template_sha256", "review_plan_sha256", "assignment_sha256", "subject_digest", "verification_artifact_digest", `"sha256"`} {
		if strings.Contains(string(encoded), field) {
			t.Fatalf("new packet registers %q: %s", field, encoded)
		}
	}
	for _, path := range []string{first.ViewPath, first.AnswerPath} {
		for _, component := range strings.Split(path, "/") {
			if len(component) > 255 {
				t.Fatalf("path component too long: %d in %s", len(component), path)
			}
		}
	}
	long := reviewResultTestBinding()
	long.RuntimeID = "runtime-" + strings.Repeat("r", 300)
	long.ReviewPlanID = "review-plan-" + strings.Repeat("p", 300)
	long.AssignmentID = "assignment-" + strings.Repeat("a", 300)
	long.OwnerAgentID = "agent-" + strings.Repeat("o", 300)
	longPacket, err := BuildReviewResult(long, reviewResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	if !ReviewResultUsesNormalAuthoring(longPacket) {
		t.Fatal("long original answer path not recognized")
	}
	for _, path := range []string{longPacket.ViewPath, longPacket.AnswerPath} {
		for _, component := range strings.Split(path, "/") {
			if len(component) > 255 {
				t.Fatalf("long path component: %d", len(component))
			}
		}
	}

}
