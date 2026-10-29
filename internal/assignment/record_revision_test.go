package assignment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestRegisterS6RevisionUsesOriginalWriterAndCurrentOwnerProjection(t *testing.T) {
	fixture, statePath, journalPath, previous, _ := registerAgentBeginFixture(t, true)
	previousSHA, err := previous.Digest()
	if err != nil {
		t.Fatal(err)
	}
	setRevisionManifest(t, fixture, "team-manifest-s6-registration-r2", "workgroup-s6-registration-r2", "platform-s6-r2", "agent-s6-registration-r2")

	request := Request{
		ExpectedRevision: 9,
		ManifestPath:     filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef)),
		TaskID:           fixture.taskID,
		TaskPath:         filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef)),
		Revision: &RevisionRequest{
			AssignmentID:         previous.AssignmentID,
			ExpectedRecord:       previous.Reference(),
			ExpectedRecordSHA256: previousSHA,
			IntentKey:            "s6-test-revision-2",
		},
		OccurredAt: time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC),
	}
	issued, err := Register(fixture.root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("Register revision: %v", err)
	}
	if issued.Revision != 10 {
		t.Fatalf("revision Writer commit = %d, want 10", issued.Revision)
	}
	records, err := assignmentrecord.RecordsFromState(issued.State)
	if err != nil {
		t.Fatal(err)
	}
	current, found, err := assignmentrecord.FindRecord(records, previous.RuntimeID, previous.BaselineGeneration, previous.AssignmentID)
	if err != nil || !found {
		t.Fatalf("find current revised Record: found=%v err=%v", found, err)
	}
	if current.Revision != 2 || current.OwnerAgentID != "agent-s6-registration-r2" || current.RevisionIssuance == nil {
		t.Fatalf("current Record = revision %d owner %s issuance %+v", current.Revision, current.OwnerAgentID, current.RevisionIssuance)
	}
	if current.RevisionIssuance.PreviousRecord != previous.Reference() || current.RevisionIssuance.PreviousRecordSHA256 != previousSHA {
		t.Fatalf("revision issuance does not bind prior Record: %+v", current.RevisionIssuance)
	}
	entities := issued.State["entities"].(map[string]any)
	if len(entities["assignments"].([]any)) != 2 || len(entities["assignment_revision_receipts"].([]any)) != 1 {
		t.Fatalf("revision did not preserve history and append one receipt: assignments=%d receipts=%d", len(entities["assignments"].([]any)), len(entities["assignment_revision_receipts"].([]any)))
	}
	if got := anyStrings(entityRowByID(t, entities["tasks"], fixture.taskID)["owner_agent_ids"]); len(got) != 1 || got[0] != current.OwnerAgentID {
		t.Fatalf("current TASK owners = %v, want only %s", got, current.OwnerAgentID)
	}
	if entityRowByID(t, entities["agents"], previous.OwnerAgentID)["state"] != "stopped" {
		t.Fatalf("superseded owner state = %v, want stopped", entityRowByID(t, entities["agents"], previous.OwnerAgentID)["state"])
	}
	if entityRowByID(t, entities["teams"], previous.TeamID)["status"] != "retired" {
		t.Fatalf("superseded Team status = %v, want retired", entityRowByID(t, entities["teams"], previous.TeamID)["status"])
	}

	journal, err := readRevisionJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := loopruntime.ValidateAssignmentRevisionReceipts(issued.State, journal); err != nil {
		t.Fatalf("validate original revision Writer receipt: %v", err)
	}

	loaded, err := hookctx.LoadFull(fixture.root, current.OwnerAgentID)
	if err != nil {
		t.Fatalf("LoadFull current owner: %v", err)
	}
	loadedCurrentOwner := false
	if loaded != nil {
		for _, assignment := range loaded.Assignments {
			if assignment.OwnerAgentID == current.OwnerAgentID && assignment.AssignmentRevision == current.Revision {
				loadedCurrentOwner = true
			}
		}
	}
	if !loadedCurrentOwner {
		t.Fatalf("LoadFull omitted current owner %s revision %d: %+v", current.OwnerAgentID, current.Revision, loaded)
	}
	if _, err := hookctx.LoadFull(fixture.root, previous.OwnerAgentID); err == nil || !strings.Contains(err.Error(), "no longer owns a current Assignment") {
		t.Fatalf("LoadFull superseded owner error = %v", err)
	}

	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	request.ExpectedRevision = -1
	replayed, err := Register(fixture.root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("exact revision replay: %v", err)
	}
	if replayed.Revision != issued.Revision {
		t.Fatalf("exact replay revision = %d, want original %d", replayed.Revision, issued.Revision)
	}
	afterState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	afterJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
		t.Fatal("exact revision replay wrote Runtime state or journal")
	}

	staleRequest := request
	staleRequest.Revision = &RevisionRequest{
		AssignmentID:         previous.AssignmentID,
		ExpectedRecord:       previous.Reference(),
		ExpectedRecordSHA256: previousSHA,
		IntentKey:            "s6-test-stale-revision",
	}
	if _, err := Register(fixture.root, statePath, journalPath, staleRequest); err == nil || !strings.Contains(err.Error(), "stale Assignment revision") {
		t.Fatalf("stale expected Record error = %v", err)
	}

	rolePath := filepath.Join(fixture.root, ".claude", "agents", "backend-builder.md")
	role, err := os.ReadFile(rolePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rolePath, append(role, []byte("\n<!-- revision source drift -->\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRegistration(t, fixture.root, "add", ".claude/agents/backend-builder.md")
	gitRegistration(t, fixture.root, "commit", "-m", "change current S6 role source")
	if _, err := Register(fixture.root, statePath, journalPath, request); err == nil || !strings.Contains(err.Error(), "already issued for different input bytes") {
		t.Fatalf("same intent after current role-source drift error = %v", err)
	}
}

func TestRegisterS6UnifiedRevisionWritesFiveStateWithOriginalEvents(t *testing.T) {
	fixture, statePath, journalPath, previous, _ := registerAgentBeginConfiguredFixtureVersion(t, true, nil, "1.3.0")
	previousSHA, err := previous.Digest()
	if err != nil {
		t.Fatal(err)
	}
	setRevisionManifest(t, fixture, "team-manifest-s6-unified-r2", "workgroup-s6-unified-r2", "platform-s6-unified-r2", "agent-s6-unified-r2")
	issued, err := Register(fixture.root, statePath, journalPath, Request{
		ExpectedRevision: 9,
		ManifestPath:     filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef)),
		TaskID:           fixture.taskID,
		TaskPath:         filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef)),
		Revision: &RevisionRequest{
			AssignmentID: previous.AssignmentID, ExpectedRecord: previous.Reference(),
			ExpectedRecordSHA256: previousSHA, IntentKey: "s6-unified-revision-2",
		},
		OccurredAt: time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Register unified revision: %v", err)
	}
	if issued.State["schema_version"] != "1.3.0" {
		t.Fatalf("revision changed Runtime format: %v", issued.State["schema_version"])
	}
	entities := issued.State["entities"].(map[string]any)
	priorAgent := entityRowByID(t, entities["agents"], previous.OwnerAgentID)
	if priorAgent["state"] != "cancelled" || priorAgent["last_lifecycle_event"] != "assignment_revision_issued" {
		t.Fatalf("prior Agent state/event = %v/%v, want cancelled/assignment_revision_issued", priorAgent["state"], priorAgent["last_lifecycle_event"])
	}
	newAgent := entityRowByID(t, entities["agents"], "agent-s6-unified-r2")
	if newAgent["state"] != "active" || newAgent["last_lifecycle_event"] != "workgroup_registered" {
		t.Fatalf("new Agent state/event = %v/%v, want active/workgroup_registered", newAgent["state"], newAgent["last_lifecycle_event"])
	}
	task := entityRowByID(t, entities["tasks"], fixture.taskID)
	if task["state"] != "pending" || task["last_lifecycle_event"] != "assignment_revision_issued" {
		t.Fatalf("revised TASK state/event = %v/%v, want pending/assignment_revision_issued", task["state"], task["last_lifecycle_event"])
	}
	journal, err := readRevisionJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := loopruntime.ValidateAssignmentRevisionReceipts(issued.State, journal); err != nil {
		t.Fatalf("validate unified revision receipt in original journal: %v", err)
	}
}

func TestRegisterS6RevisionMovesPriorWorkspaceExecutionIntoHistory(t *testing.T) {
	fixture, statePath, journalPath, previous, _ := registerAgentBeginFixture(t, true)
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	binding, err := workspace.New(context.Background(), fixture.root)
	if err != nil {
		t.Fatalf("open original Workspace authority: %v", err)
	}
	inputPaths := []string{}
	seen := map[string]bool{}
	for _, source := range previous.ContextSources {
		if strings.Contains(source.Ref, "#") || strings.HasPrefix(source.Kind, "embedded_") || source.Kind == "check_profile" || seen[source.Ref] {
			continue
		}
		seen[source.Ref] = true
		inputPaths = append(inputPaths, source.Ref)
	}
	execution, err := binding.Plan(context.Background(), state, previous.AssignmentID, previous.OwnerAgentID,
		previous.EffectiveWritePaths, previous.RequiredChecks, inputPaths)
	if err != nil {
		t.Fatalf("plan original canonical workspace execution: %v", err)
	}
	execution, err = binding.FreezeCheckProfiles(execution, previous.CheckProfiles)
	if err != nil {
		t.Fatalf("freeze original workspace CheckProfiles: %v", err)
	}
	binding.Executions[previous.AssignmentID] = execution
	state["workspace"] = workspace.Encode(binding)
	encodedState, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(encodedState, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	previousSHA, err := previous.Digest()
	if err != nil {
		t.Fatal(err)
	}
	setRevisionManifest(t, fixture, "team-manifest-s6-workspace-r2", "workgroup-s6-workspace-r2", "platform-s6-workspace-r2", "agent-s6-workspace-r2")
	issued, err := Register(fixture.root, statePath, journalPath, Request{
		ExpectedRevision: -1,
		ManifestPath:     filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef)),
		TaskID:           fixture.taskID,
		TaskPath:         filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef)),
		Revision: &RevisionRequest{
			AssignmentID: previous.AssignmentID, ExpectedRecord: previous.Reference(),
			ExpectedRecordSHA256: previousSHA, IntentKey: "s6-workspace-revision-r2",
		},
		OccurredAt: time.Date(2026, 10, 14, 13, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("issue next Record with an existing Workspace execution: %v", err)
	}
	retired, err := workspace.Decode(issued.State)
	if err != nil || retired == nil {
		t.Fatalf("decode revised Workspace registry: %v", err)
	}
	if _, current := retired.Executions[previous.AssignmentID]; current {
		t.Fatal("revision retained the prior workspace execution in the current slot")
	}
	if len(retired.History) != 1 {
		t.Fatalf("workspace execution history count=%d, want original execution retained once", len(retired.History))
	}
	historical := retired.History[0]
	if historical.Status != "retired" || historical.AgentID != previous.OwnerAgentID || historical.AssignmentRecord == nil ||
		*historical.AssignmentRecord != previous.Reference() || historical.AssignmentRecordSHA256 != previousSHA || historical.Generation != execution.Generation {
		t.Fatalf("prior workspace execution history lost its original authorization: %+v", historical)
	}
}

func TestRegisterS6RevisionSharesMainWorkspaceLock(t *testing.T) {
	fixture, statePath, journalPath, previous, _ := registerAgentBeginFixture(t, true)
	previousSHA, err := previous.Digest()
	if err != nil {
		t.Fatal(err)
	}
	setRevisionManifest(t, fixture, "team-manifest-s6-lock-r2", "workgroup-s6-lock-r2", "platform-s6-lock-r2", "agent-s6-lock-r2")
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := integration.LockWorkspace(context.Background(), fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{
		ExpectedRevision: -1,
		ManifestPath:     filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef)),
		TaskID:           fixture.taskID,
		TaskPath:         filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef)),
		Revision: &RevisionRequest{
			AssignmentID: previous.AssignmentID, ExpectedRecord: previous.Reference(),
			ExpectedRecordSHA256: previousSHA, IntentKey: "s6-lock-busy-revision",
		},
		OccurredAt: time.Date(2026, 10, 14, 13, 40, 0, 0, time.UTC),
	}
	_, err = Register(fixture.root, statePath, journalPath, request)
	release()
	if !errors.Is(err, integration.ErrIntegrationBusy) {
		t.Fatalf("revision while Main holds integration lock error=%v, want integration busy", err)
	}
	afterState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	afterJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
		t.Fatal("busy revision request changed the Runtime pair before acquiring Main's lock")
	}
	issued, err := Register(fixture.root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("same revision after Main releases its lock: %v", err)
	}
	current, found, err := assignmentrecord.ForOwner(issued.State, "agent-s6-lock-r2")
	if err != nil || !found || current.Revision != 2 {
		t.Fatalf("released-lock retry did not issue r2: found=%v record=%+v err=%v", found, current, err)
	}
}

func setRevisionManifest(t *testing.T, fixture recordRegistrationFixture, manifestID, workgroupID, platformID, agentID string) {
	t.Helper()
	manifestPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef))
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["manifest_id"] = manifestID
	manifest["version"] = "v1.0.2"
	manifest["workgroup_id"] = workgroupID
	manifest["platform_team_id"] = platformID
	assignments, ok := manifest["assignments"].([]any)
	if !ok || len(assignments) != 1 {
		t.Fatalf("test revision manifest assignments = %#v", manifest["assignments"])
	}
	assignments[0].(map[string]any)["agent_id"] = agentID
	updated, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(manifestPath, updated, 0o644); err != nil {
		t.Fatal(err)
	}
	gitRegistration(t, fixture.root, "add", fixture.manifestRef)
	gitRegistration(t, fixture.root, "commit", "-m", "prepare explicit S6 Assignment revision")
	fixture.manifest = updated
}

func entityRowByID(t *testing.T, rows any, id string) map[string]any {
	t.Helper()
	values, ok := rows.([]any)
	if !ok {
		t.Fatalf("entity rows = %T, want []any", rows)
	}
	for _, raw := range values {
		row, _ := raw.(map[string]any)
		if row["id"] == id {
			return row
		}
	}
	t.Fatalf("entity row %s is missing", id)
	return nil
}
