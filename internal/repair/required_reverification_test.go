package repair

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Compatibility labels are not a substitute for the actual observations.
func TestTargetedScopeMetadataDoesNotReplaceActualPassObservations(t *testing.T) {
	root := t.TempDir()
	base := TargetedReverificationRequest{
		RuntimeID: "loop-runtime-1", BugID: "BUG-001", BaselineGeneration: 1,
		OriginalAssignmentID: "repair-assignment-1", PerformingAssignmentID: "assignment-qa", ImpactID: "impact-1",
		AssertionResults: []AssertionResult{{AssertionID: "root-1", Result: "pass", EvidenceRefs: []string{"test://actual-observation"}}}, Result: "pass",
	}
	for i, scope := range []string{"", "pass", "fail"} {
		request := base
		request.ReverificationID, request.ScopeCompliance = fmt.Sprintf("reverify-scope-%d", i), scope
		value, ref, err := CreateTargetedReverification(root, request)
		if err != nil {
			t.Fatalf("truthful report with compatibility scope %q: %v", scope, err)
		}
		if value.ScopeCompliance != scope || value.Result != "pass" {
			t.Fatalf("compatibility metadata rewrote observations: %+v", value)
		}
		if _, err := ValidateTargetedReverification(root, ref); err != nil {
			t.Fatal(err)
		}
	}
	// A PASS label cannot hide a real unfinished assertion, even if the legacy
	// scope flag says pass. The professional error is rejected before writing.
	incomplete := base
	incomplete.ReverificationID, incomplete.ScopeCompliance = "reverify-incomplete", "pass"
	incomplete.AssertionResults = []AssertionResult{{AssertionID: "root-1", Result: "blocked", EvidenceRefs: []string{"test://unavailable"}}}
	if _, _, err := CreateTargetedReverification(root, incomplete); err == nil || !strings.Contains(err.Error(), "unfinished assertion") {
		t.Fatalf("scope label hid actual unfinished work: %v", err)
	}
	for i, result := range []string{"blocked", "fail"} {
		request := incomplete
		request.ReverificationID, request.Result = fmt.Sprintf("reverify-nonpass-%d", i), result
		value, _, err := CreateTargetedReverification(root, request)
		if err != nil || value.Result != result {
			t.Fatalf("non-pass observation was not retained: %+v %v", value, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "loop-state.json")); !os.IsNotExist(err) {
		t.Fatalf("professional Creator created Runtime authority: %v", err)
	}
}
