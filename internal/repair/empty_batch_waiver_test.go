package repair

import "testing"

func TestConfirmationSubjectsRequireBoundPredecessor(t *testing.T) {
	if _, err := confirmationSubjects(t.TempDir(), nil, "loop-REQ-039", "REQ-039", "repair-session-current", ApprovedContract{}); err == nil {
		t.Fatal("unbound empty confirmation accepted")
	}
}
