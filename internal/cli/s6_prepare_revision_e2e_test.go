package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
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
	recordSHA, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	writeCanonicalS6PendingReworkFixture(t, fixture)

	const (
		newAgent        = "agent-s6-native-context-r2"
		newPlatformTeam = "platform-s6-native-context-r2"
		intentKey       = "s6-revision-intent-r2"
	)
	draftRef := filepath.ToSlash(filepath.Join(".claude", "workgroups", "REQ-039", record.Task.ID, "manifest-s6-r2.json"))
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

	args := []string{"s6", "prepare", "--root", root, "--task", record.Task.ID,
		"--role", record.RoleFamily, "--agent-id", newAgent, "--platform-team-id", newPlatformTeam,
		"--revise-assignment", record.AssignmentID, "--revision-intent-key", intentKey}
	var stdout, stderr bytes.Buffer
	if code := cli.Run(args, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("prepare the current canonical S6 revision: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "S6 revision manifest draft written (not registered): "+draftRef) {
		t.Fatalf("revision prepare omitted the fixed exact draft path: %s", stdout.String())
	}
	refJSON, err := json.Marshal(record.Reference())
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"--manifest " + strconv.Quote(draftRef),
		"--task-id " + strconv.Quote(record.Task.ID),
		"--task " + strconv.Quote(record.Task.Ref),
		"--revise-assignment " + strconv.Quote(record.AssignmentID),
		"--revision-intent-key " + strconv.Quote(intentKey),
		"--expected-record-ref " + strconv.Quote(string(refJSON)),
		"--expected-record-sha256 " + strconv.Quote(recordSHA),
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("returned original Writer command is missing %s: %s", expected, stdout.String())
		}
	}
	stateAfterPrepare, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfterPrepare, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateBefore, stateAfterPrepare) || !bytes.Equal(journalBefore, journalAfterPrepare) {
		t.Fatal("S6 revision draft preparation changed the Runtime state or journal")
	}
	var repeatOut, repeatErr bytes.Buffer
	if code := cli.Run(args, nil, &repeatOut, &repeatErr); code == 0 || !strings.Contains(repeatErr.String(), "refusing to overwrite existing draft") {
		t.Fatalf("revision preparation overwrote its existing fixed draft: code=%d stderr=%s stdout=%s", code, repeatErr.String(), repeatOut.String())
	}
	stateAfterRepeat, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfterRepeat, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateBefore, stateAfterRepeat) || !bytes.Equal(journalBefore, journalAfterRepeat) {
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

	// The original registration Writer remains the authority and must accept
	// the prepared bytes with the exact current Record assertion it returned.
	stageCLI(t, "runtime", "register-workgroup", "--root", root,
		"--manifest", draftRef, "--task-id", record.Task.ID, "--task", record.Task.Ref,
		"--revise-assignment", record.AssignmentID, "--revision-intent-key", intentKey,
		"--expected-record-ref", string(refJSON), "--expected-record-sha256", recordSHA)
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
		"--role", record.RoleFamily, "--agent-id", "agent-s6-no-rework-r2", "--platform-team-id", "platform-s6-no-rework-r2",
		"--revise-assignment", record.AssignmentID, "--revision-intent-key", "s6-no-rework-r2"}, nil, &stdout, &stderr)
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

func TestS6PrepareRevisionRejectsRegisteredManifestSourceDrift(t *testing.T) {
	fixture := readyS6CanonicalResultCLI(t)
	writeCanonicalS6PendingReworkFixture(t, fixture)
	root, record := fixture.root, fixture.record
	manifestPath := filepath.Join(root, filepath.FromSlash(record.Manifest.Ref))
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(manifestBytes, []byte("\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s6", "prepare", "--root", root, "--task", record.Task.ID,
		"--role", record.RoleFamily, "--agent-id", "agent-s6-source-drift-r2", "--platform-team-id", "platform-s6-source-drift-r2",
		"--revise-assignment", record.AssignmentID, "--revision-intent-key", "s6-source-drift-r2"}, nil, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "registered source drift") {
		t.Fatalf("revision preparation did not preserve current source integrity: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "workgroups", "REQ-039", record.Task.ID, "manifest-s6-r2.json")); !os.IsNotExist(err) {
		t.Fatalf("source drift created a revision draft: %v", err)
	}
}

func writeCanonicalS6PendingReworkFixture(t *testing.T, fixture s6CanonicalResultCLIFixture) {
	t.Helper()
	sha, err := fixture.record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	ref := fixture.record.Reference()
	path := assignmentrecord.IntegrationCheckpointPath(fixture.root, ref)
	if _, err := integration.DefaultCheckpointStore().ForceWrite(path, integration.Checkpoint{
		AssignmentID: fixture.record.AssignmentID, TaskID: fixture.record.Task.ID,
		BaselineGeneration: fixture.record.BaselineGeneration, State: integration.StatePreserved,
		AssignmentRecord: &ref, AssignmentRecordSHA256: sha,
		FailureReason: "fixture records an original pending canonical rework entry",
	}); err != nil {
		t.Fatalf("write the existing Record-bound pending checkpoint fixture: %v", err)
	}
}

func normalizedS6RevisionManifest(value map[string]any) map[string]any {
	copy := make(map[string]any, len(value))
	for key, item := range value {
		copy[key] = item
	}
	for _, key := range []string{"manifest_id", "version", "platform_team_id", "workgroup_id", "status"} {
		delete(copy, key)
	}
	assignments := copy["assignments"].([]any)
	clonedAssignments := make([]any, len(assignments))
	for i, raw := range assignments {
		row := raw.(map[string]any)
		cloned := make(map[string]any, len(row))
		for key, item := range row {
			cloned[key] = item
		}
		delete(cloned, "agent_id")
		delete(cloned, "status")
		clonedAssignments[i] = cloned
	}
	copy["assignments"] = clonedAssignments
	return copy
}
