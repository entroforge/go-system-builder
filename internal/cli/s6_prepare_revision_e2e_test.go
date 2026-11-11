package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/integration"
)

func TestS6PrepareRevisionProjectsCurrentCanonicalReworkForOriginalWriter(t *testing.T) {
	fixture := readyS6CanonicalResultCLI(t)
	root := fixture.root
	record := fixture.record
	writeCanonicalS6PendingReworkFixture(t, fixture)

	newAgent := fmt.Sprintf("native-%s-r%d", record.Task.ID, record.Revision+1)
	newPlatformTeam := fmt.Sprintf("engineering-%s-r%d", record.Task.ID, record.Revision+1)
	draftRef := filepath.ToSlash(filepath.Join(".claude", "workgroups", "REQ-039", record.Task.ID, "manifest-s6-r2.json"))
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	args := []string{"s6", "prepare", "--root", root, "--task", record.Task.ID,
		"--role", record.RoleFamily,
		"--revise-assignment", record.AssignmentID}
	var stdout, stderr bytes.Buffer
	if code := cli.Run(args, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("prepare the current canonical S6 revision: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "Native S6 TASK registered: "+record.Task.ID) || !strings.Contains(stdout.String(), "brief") {
		t.Fatalf("original Writer did not return the natural Builder brief: %s", stdout.String())
	}
	stateAfterPrepare, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfterPrepare, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var repeatOut, repeatErr bytes.Buffer
	if code := cli.Run(args, nil, &repeatOut, &repeatErr); code != 0 {
		t.Fatalf("revision preparation did not resume its original draft: code=%d stderr=%s stdout=%s", code, repeatErr.String(), repeatOut.String())
	}
	stateAfterRepeat, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfterRepeat, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateAfterPrepare, stateAfterRepeat) || !bytes.Equal(journalAfterPrepare, journalAfterRepeat) {
		t.Fatal("refused revision draft overwrite changed the Runtime state or journal")
	}

	originalBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(record.Manifest.Ref)))
	if err != nil {
		t.Fatalf("read the current registered manifest: %v", err)
	}
	draftBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(draftRef)))
	if err != nil {
		t.Fatalf("read the exact revision draft: %v", err)
	}
	var original, draft map[string]any
	if err := json.Unmarshal(originalBytes, &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(draftBytes, &draft); err != nil {
		t.Fatal(err)
	}
	if !s6SameJSONValue(normalizedS6RevisionManifest(original), normalizedS6RevisionManifest(draft)) {
		t.Fatalf("revision draft changed contract/source/check fields beyond its new attempt coordinates:\noriginal=%v\ndraft=%v", original, draft)
	}
	draftAssignment := draft["assignments"].([]any)[0].(map[string]any)
	if draft["manifest_id"] != "team-manifest-build-"+record.Task.ID+"-r2" ||
		draft["workgroup_id"] != "workgroup-build-"+record.Task.ID+"-r2" ||
		draft["platform_team_id"] != newPlatformTeam || draftAssignment["agent_id"] != newAgent {
		t.Fatalf("revision draft did not project only the Main-selected new attempt identity: %v", draft)
	}

	// The natural prepare call has already invoked the original registration Writer.
	registered := stageSnapshot(t, root)
	next, found, err := assignmentrecord.ForOwner(registered.State, newAgent)
	if err != nil || !found || next.Revision != record.Revision+1 || next.AssignmentID != record.AssignmentID {
		t.Fatalf("original registration Writer did not accept the prepared revision: found=%t record=%+v err=%v", found, next, err)
	}
}

func TestS6PrepareRevisionRequiresOriginalCanonicalReworkProjection(t *testing.T) {
	fixture := readyS6CanonicalResultCLI(t)
	root, record := fixture.root, fixture.record
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s6", "prepare", "--root", root, "--task", record.Task.ID,
		"--role", record.RoleFamily,
		"--revise-assignment", record.AssignmentID}, nil, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "no current canonical ReworkCommand") {
		t.Fatalf("revision without the original pending rework projection was not rejected: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "workgroups", "REQ-039", record.Task.ID, "manifest-s6-r2.json")); !os.IsNotExist(err) {
		t.Fatalf("revision without pending canonical rework wrote a draft: %v", err)
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatal("rejected revision preparation changed the Runtime state or journal")
	}
}

func TestS6PrepareRevisionAcceptsOriginalManifestWithoutDigestComparison(t *testing.T) {
	fixture := readyS6CanonicalResultCLI(t)
	writeCanonicalS6PendingReworkFixture(t, fixture)
	root, record := fixture.root, fixture.record
	manifestPath := filepath.Join(root, filepath.FromSlash(record.Manifest.Ref))
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]any
	if err := json.Unmarshal(manifestBytes, &original); err != nil {
		t.Fatal(err)
	}
	document := original["documents"].([]any)[0].(map[string]any)
	document["sha256"] = map[string]any{"old": true}
	document["author_note"] = map[string]any{"sha256": "business description"}
	manifestBytes, _ = json.Marshal(original)
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s6", "prepare", "--root", root, "--task", record.Task.ID,
		"--role", record.RoleFamily,
		"--revise-assignment", record.AssignmentID}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("revision preparation rejected readable original manifest: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "workgroups", "REQ-039", record.Task.ID, "manifest-s6-r2.json")); err != nil {
		t.Fatalf("revision draft missing: %v", err)
	}
	draftBytes, err := os.ReadFile(filepath.Join(root, ".claude", "workgroups", "REQ-039", record.Task.ID, "manifest-s6-r2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if err := json.Unmarshal(draftBytes, &draft); err != nil {
		t.Fatal(err)
	}
	current := draft["documents"].([]any)[0].(map[string]any)
	if _, exists := current["sha256"]; exists {
		t.Fatal("new revision registered old machine digest")
	}
	if current["author_note"].(map[string]any)["sha256"] != "business description" {
		t.Fatal("new revision lost professional reference extension")
	}
	if data, err := os.ReadFile(manifestPath); err != nil || !bytes.Equal(data, manifestBytes) {
		t.Fatalf("revision rewrote original manifest: %v", err)
	}
}

func writeCanonicalS6PendingReworkFixture(t *testing.T, fixture s6CanonicalResultCLIFixture) {
	t.Helper()
	ref := fixture.record.Reference()
	path := assignmentrecord.IntegrationCheckpointPath(fixture.root, ref)
	if _, err := integration.DefaultCheckpointStore().ForceWrite(path, integration.Checkpoint{
		AssignmentID: fixture.record.AssignmentID, TaskID: fixture.record.Task.ID,
		BaselineGeneration: fixture.record.BaselineGeneration, State: integration.StatePreserved,
		AssignmentRecord: &ref,
		FailureReason:    "fixture records an original pending canonical rework entry",
	}); err != nil {
		t.Fatalf("write the existing Record-bound pending checkpoint fixture: %v", err)
	}
}

func normalizedS6RevisionManifest(value map[string]any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var copy map[string]any
	if err := json.Unmarshal(data, &copy); err != nil {
		panic(err)
	}
	for _, raw := range copy["documents"].([]any) {
		delete(raw.(map[string]any), "sha256")
	}
	for _, key := range []string{"manifest_id", "version", "platform_team_id", "workgroup_id", "status"} {
		delete(copy, key)
	}
	assignments := copy["assignments"].([]any)
	for _, raw := range assignments {
		row := raw.(map[string]any)
		for _, rawProfile := range row["check_profiles"].([]any) {
			profile := rawProfile.(map[string]any)
			for _, field := range []string{"dependencies", "fixtures"} {
				for _, rawRef := range profile[field].([]any) {
					delete(rawRef.(map[string]any), "sha256")
				}
			}
		}
		delete(row, "agent_id")
		delete(row, "status")
	}
	return copy
}
