package repair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS9CandidateUsesOriginalResponsibilityWithoutDigest(t *testing.T) {
	root := t.TempDir()
	candidate := WorkspaceCandidate{Version: 1, RecordType: "repair_workspace_candidate", RuntimeID: "runtime-1", BaselineGeneration: 1, ExecutionGeneration: 1, AssignmentID: "assignment-1", SessionID: "session-1", PlanID: "plan-1"}
	first, err := writeResponsibilityCandidate(root, "packet-original", candidate)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := writeResponsibilityCandidate(root, "packet-original", candidate)
	if err != nil || first.Path == replay.Path {
		t.Fatalf("unregistered candidate must not be claimed as a replay: %+v %+v %v", first, replay, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, first.Path))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if _, present := saved["contract_sha256"]; present || strings.Contains(first.Path, strings.Repeat("0", 64)) {
		t.Fatalf("new Candidate must not register a digest: %s %+v", raw, first)
	}
	other := candidate
	other.AssignmentID = "assignment-2"
	second, err := writeResponsibilityCandidate(root, "packet-original", other)
	if err != nil || first.Path == second.Path {
		t.Fatalf("unknown existing candidate must be preserved: %+v %+v %v", first, second, err)
	}
	if still, err := os.ReadFile(filepath.Join(root, first.Path)); err != nil || string(still) != string(raw) {
		t.Fatalf("existing Candidate was overwritten: %v", err)
	}
}
