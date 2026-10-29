package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateChangeImpactReplaysIdenticalStableArtifactOnly(t *testing.T) {
	root := t.TempDir()
	analyzedAt := time.Date(2026, 10, 16, 12, 30, 0, 0, time.UTC)
	request := ChangeImpactRequest{
		ImpactID: "impact-replay", RuntimeID: "loop-runtime-replay", ReqID: "REQ-001", BaselineGeneration: 1,
		SourceBugIDs: []string{"BUG-001"}, ChangeTypes: []string{"implementation"},
		ChangedArtifacts: []ArtifactRef{{ID: "changed-file", Path: "src/file.go", SHA256: strings.Repeat("a", 64)}},
		Decisions:        []ImpactDecision{{SourceID: "BUG-001", TargetID: "root-1", Relation: "invalidates", RuleID: "IM-REPLAY", Decision: "reverify", Scope: []string{"src/file.go"}, Rationale: "same source decision", RecoveryEvidence: []string{"test://repair-result"}}},
		AnalyzedBy:       "qa", AnalyzedAt: analyzedAt,
	}
	_, originalRef, err := CreateChangeImpact(root, request)
	if err != nil {
		t.Fatal(err)
	}
	originalBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(originalRef.Path)))
	if err != nil {
		t.Fatal(err)
	}

	_, replayRef, err := CreateChangeImpact(root, request)
	if err != nil {
		t.Fatalf("identical stable ChangeImpact replay: %v", err)
	}
	if replayRef != originalRef {
		t.Fatalf("identical replay ref = %#v, want original %#v", replayRef, originalRef)
	}
	if _, err := ValidateChangeImpact(root, replayRef); err != nil {
		t.Fatalf("replayed ChangeImpact ref is not valid: %v", err)
	}
	if after, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(originalRef.Path))); readErr != nil || string(after) != string(originalBytes) {
		t.Fatalf("identical replay changed original bytes: %v", readErr)
	}

	request.Decisions[0].Rationale = "different decision content"
	if _, _, err := CreateChangeImpact(root, request); err == nil {
		t.Fatal("different ChangeImpact content must not replace the immutable canonical artifact")
	}
	if after, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(originalRef.Path))); readErr != nil || string(after) != string(originalBytes) {
		t.Fatalf("rejected different replay changed original bytes: %v", readErr)
	}
}

func TestCreateTargetedReverificationReplaysIdenticalStableArtifactOnly(t *testing.T) {
	root := t.TempDir()
	request := TargetedReverificationRequest{
		ReverificationID: "reverify-replay", RuntimeID: "loop-runtime-replay", BugID: "BUG-001", BaselineGeneration: 1,
		OriginalAssignmentID: "repair-assignment-unit-1", PerformingAssignmentID: "assignment-qa",
		ContinuityReason: "test://verifier-log: independently checked the root assertion", ImpactID: "impact-1",
		AssertionResults: []AssertionResult{{AssertionID: "root-1", Result: "pass", EvidenceRefs: []string{"test://root-pass"}}},
		ScopeCompliance:  "pass", Result: "pass", PerformedAt: time.Date(2026, 10, 16, 12, 35, 0, 0, time.UTC),
	}
	_, originalRef, err := CreateTargetedReverification(root, request)
	if err != nil {
		t.Fatal(err)
	}
	originalBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(originalRef.Path)))
	if err != nil {
		t.Fatal(err)
	}

	_, replayRef, err := CreateTargetedReverification(root, request)
	if err != nil {
		t.Fatalf("identical stable TargetedReverification replay: %v", err)
	}
	if replayRef != originalRef {
		t.Fatalf("identical replay ref = %#v, want original %#v", replayRef, originalRef)
	}
	if _, err := ValidateTargetedReverification(root, replayRef); err != nil {
		t.Fatalf("replayed TargetedReverification ref is not valid: %v", err)
	}
	if after, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(originalRef.Path))); readErr != nil || string(after) != string(originalBytes) {
		t.Fatalf("identical replay changed original bytes: %v", readErr)
	}

	request.ContinuityReason = "test://other-verifier-log: different verification record"
	if _, _, err := CreateTargetedReverification(root, request); err == nil {
		t.Fatal("different TargetedReverification content must not replace the immutable canonical artifact")
	}
	if after, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(originalRef.Path))); readErr != nil || string(after) != string(originalBytes) {
		t.Fatalf("rejected different replay changed original bytes: %v", readErr)
	}
}
