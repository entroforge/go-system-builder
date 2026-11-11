package repair

import (
	"fmt"
	"os"
	"path/filepath"
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
	// Reception preserves the author's overall conclusion and observations;
	// Main and the verifier resolve the professional discrepancy.
	incomplete := base
	incomplete.ReverificationID, incomplete.ScopeCompliance = "reverify-incomplete", "pass"
	incomplete.AssertionResults = []AssertionResult{{AssertionID: "root-1", Result: "blocked", EvidenceRefs: []string{}}}
	if value, _, err := CreateTargetedReverification(root, incomplete); err != nil || value.Result != "pass" || value.AssertionResults[0].Result != "blocked" {
		t.Fatalf("receiver recomputed professional conclusion: %+v %v", value, err)
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
