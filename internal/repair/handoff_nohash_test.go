package repair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestS9HandoffStoresOriginalPathsWithoutHistoricalDigests(t *testing.T) {
	root := t.TempDir()
	sha := strings.Repeat("a", 64)
	ref := func(path string) ArtifactRef { return ArtifactRef{Path: path} }
	request := HandoffRequest{
		HandoffID: "repair-handoff-original", Session: ref("session.json"), Plan: ref("plan.json"),
		Contract: ContractRef{Path: "contract.json"}, ChangeImpact: ref("impact.json"),
		TargetedReverifications: []ArtifactRef{ref("targeted.json")}, HandedOffBy: "Main", NextAction: "continue original S7",
		OccurredAt: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC),
	}
	_, result, err := CreateRepairHandoff(root, request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(result.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"sha256"`) || strings.Contains(string(data), sha) {
		t.Fatalf("historical hash copied: %s", data)
	}
	var handoff RepairHandoff
	if err := json.Unmarshal(data, &handoff); err != nil {
		t.Fatal(err)
	}
	if handoff.SessionRef.Path != "session.json" || handoff.TargetedReverificationRefs[0].Path != "targeted.json" {
		t.Fatalf("original paths lost: %s", data)
	}
	state := map[string]any{"evidence": []any{}}
	indexHandoffEvidence(state, result, handoff, 1, request.OccurredAt, "review-plan-entry")
	indexed, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(indexed), `"sha256"`) {
		t.Fatalf("index writes a hash: %s", indexed)
	}
	if len(state["evidence"].([]any)) != 3 {
		t.Fatalf("original references not indexed: %s", indexed)
	}
}

func TestS9SeedDoesNotTransferOldHashesAndKeepsProfessionalText(t *testing.T) {
	root := t.TempDir()
	sha := strings.Repeat("b", 64)
	note := "The product itself displays sha256 values."
	ref, err := createS7ReviewPlanSeedForEntry(root, "review-plan-handoff", 1,
		ChangeImpact{ChangedArtifacts: []ArtifactRef{{Path: "src/file.go"}}, IntegrationNotes: note},
		ContractRef{Path: "contract.json"}, nil, "impact.json", "2026-10-31T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"sha256"`) || strings.Contains(string(data), sha) {
		t.Fatalf("seed transfers digest: %s", data)
	}
	if !strings.Contains(string(data), note) || !strings.Contains(string(data), "src/file.go") {
		t.Fatalf("professional content lost: %s", data)
	}
}

func TestS9ArtifactPathComparisonDoesNotUseHistoricalDigests(t *testing.T) {
	changes := []ChangedArtifact{{Path: "src/file.go", Status: "modified"}}
	if err := changedArtifactsWithinActual(changes, []ArtifactRef{{Path: "src/file.go"}}); err != nil {
		t.Fatalf("old hash became a qualification: %v", err)
	}
	if err := changedArtifactsWithinActual(changes, []ArtifactRef{{Path: "src/other.go"}}); err == nil {
		t.Fatal("different original paths were treated as the same")
	}
}

func TestS9TargetedReceiptAppendKeepsEarlierDirectConclusions(t *testing.T) {
	rows := []any{map[string]any{"path": "old.json", "sha256": strings.Repeat("a", 64), "performing_assignment_id": "verifier-old", "result": "pass", "author_note": map[string]any{"sha256": "business text"}}}
	got := appendArtifactRefFromPointer(rows, ArtifactRef{Path: "new.json"})
	if len(got) != 2 {
		t.Fatalf("receipts lost: %#v", got)
	}
	previous := got[0].(map[string]any)
	if previous["performing_assignment_id"] != "verifier-old" || previous["result"] != "pass" {
		t.Fatalf("earlier accepted conclusion lost: %#v", previous)
	}
	if !reflect.DeepEqual(previous["author_note"], map[string]any{"sha256": "business text"}) {
		t.Fatal("old professional reference extension lost")
	}
	if _, exists := rows[0].(map[string]any)["sha256"]; !exists {
		t.Fatal("original historical reference changed")
	}
	again := appendArtifactRefFromPointer(got, ArtifactRef{Path: "new.json"})
	if !reflect.DeepEqual(again, got) {
		t.Fatal("reference replay changed accepted content")
	}
	if _, exists := previous["sha256"]; exists {
		t.Fatalf("historical digest copied: %#v", previous)
	}
}

func TestS9LongTargetedResponsibilityUsesStableBoundedArtifactPath(t *testing.T) {
	root := t.TempDir()
	id := "reverify-" + strings.Repeat("original-", 40)
	req := TargetedReverificationRequest{ReverificationID: id, RuntimeID: "loop-1", CaseID: "case-1", BaselineGeneration: 1,
		OriginalAssignmentID: "repair-assignment-one", PerformingAssignmentID: "repair-assignment-verification-one", Result: "pass",
		PerformedAt: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)}
	_, ref, err := CreateTargetedReverification(root, req)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Path != ArtifactStoragePath("reverification", id) {
		t.Fatalf("inconsistent original path: %q", ref.Path)
	}
	for _, component := range strings.Split(ref.Path, "/") {
		if len(component) > 200 {
			t.Fatalf("unbounded component %d", len(component))
		}
	}
	if _, _, err := CreateTargetedReverification(root, req); err != nil {
		t.Fatalf("same original replay: %v", err)
	}
}
