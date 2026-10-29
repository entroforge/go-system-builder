package assignment

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestRegisterS6CommitsRecordAndCanonicalActivationScopeTogether(t *testing.T) {
	fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{writePath: "src"})
	narrowRegistrationFixtureScope(t, fixture)
	state, statePath, journalPath := prepareRegistrationRuntime(t, fixture)
	manifestPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef))
	taskPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef))
	writeRegistrationJSON(t, statePath, state)

	next, err := Register(fixture.root, statePath, journalPath, Request{
		ExpectedRevision: 8,
		ManifestPath:     manifestPath,
		TaskID:           fixture.taskID,
		TaskPath:         taskPath,
		OccurredAt:       time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if next.Revision != 9 || next.State["revision"] != 9 {
		t.Fatalf("committed revision = %d / %v, want 9", next.Revision, next.State["revision"])
	}
	entities := next.State["entities"].(map[string]any)
	recordRows := entities["assignments"].([]any)
	if len(recordRows) != 1 {
		t.Fatalf("Assignment records = %d, want one", len(recordRows))
	}
	var record Record
	encodedRecord, err := json.Marshal(recordRows[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encodedRecord, &record); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRecord(record); err != nil {
		t.Fatalf("registered record is invalid: %v", err)
	}
	if record.ClosingContract != nil || record.IntegrationCheckMode != "" {
		t.Fatalf("legacy manifest was silently given a closing contract: %+v", record)
	}
	if strings.Contains(string(encodedRecord), `"closing_contract"`) || strings.Contains(string(encodedRecord), `"integration_check_mode"`) {
		t.Fatalf("legacy Record JSON shape was retrofitted: %s", encodedRecord)
	}
	if record.RegistrationRevision != next.Revision || record.Task.ID != fixture.taskID || record.Task.Ref != fixture.taskRef {
		t.Fatalf("record registration binding = revision %d, TASK %s at %s", record.RegistrationRevision, record.Task.ID, record.Task.Ref)
	}
	wantScope := []string{"src/pkg"}
	if !reflect.DeepEqual(record.EffectiveWritePaths, wantScope) {
		t.Fatalf("record effective scope = %v, want %v", record.EffectiveWritePaths, wantScope)
	}

	var agent map[string]any
	for _, raw := range entities["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == "agent-s6-registration" {
			agent = row
			break
		}
	}
	if agent == nil {
		t.Fatal("registered owner Agent is missing")
	}
	activationRef, _ := agent["activation_ref"].(string)
	if activationRef == "" {
		t.Fatal("registered Agent has no activation envelope reference")
	}
	activationBytes, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(activationRef)))
	if err != nil {
		t.Fatalf("read activation envelope %s: %v", activationRef, err)
	}
	var activation ActivationEnvelope
	if err := json.Unmarshal(activationBytes, &activation); err != nil {
		t.Fatal(err)
	}
	if activation.AgentID != record.OwnerAgentID || !reflect.DeepEqual(activation.AllowedWritePaths, record.EffectiveWritePaths) {
		t.Fatalf("activation scope %v for %s does not match canonical record scope %v for %s", activation.AllowedWritePaths, activation.AgentID, record.EffectiveWritePaths, record.OwnerAgentID)
	}
	recordDigest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if activation.AssignmentRecord == nil || *activation.AssignmentRecord != record.Reference() || activation.AssignmentRecordSHA256 != recordDigest {
		t.Fatalf("pre-staged envelope does not bind the canonical Assignment ref/digest: %+v", activation)
	}
	// The manifest's raw write scope is src. Registration must project the
	// narrower reviewed intersection stored in the canonical Assignment.
	if reflect.DeepEqual(activation.AllowedWritePaths, []string{"src"}) {
		t.Fatal("activation envelope retained the raw manifest write scope")
	}

	if len(entities["tasks"].([]any)) != 1 || len(entities["teams"].([]any)) != 1 || len(entities["agents"].([]any)) != 1 {
		t.Fatalf("registration candidate omitted its same-transaction rows: tasks=%d teams=%d agents=%d", len(entities["tasks"].([]any)), len(entities["teams"].([]any)), len(entities["agents"].([]any)))
	}
	committed, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var committedState map[string]any
	if err := json.Unmarshal(committed, &committedState); err != nil {
		t.Fatal(err)
	}
	if committedState["revision"] != float64(9) || len(committedState["entities"].(map[string]any)["assignments"].([]any)) != 1 {
		t.Fatalf("persisted Runtime does not contain registration at revision 9")
	}
	events := readRegistrationJournal(t, journalPath)
	if len(events) != 1 || events[0]["transition_id"] != "ENTITY-REGISTER" || events[0]["sequence"] != float64(1) || events[0]["after_revision"] != float64(9) {
		t.Fatalf("journal does not contain exactly the registration transaction: %#v", events)
	}
}

func TestRegisterS6FreezesExplicitEmptyClosingContractAndMainCheckMode(t *testing.T) {
	fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{writePath: "src"})
	narrowRegistrationFixtureScope(t, fixture)
	configureRegistrationClosing(t, &fixture, assignmentrecord.ClosingContractVersion, "post_merge", "main", false)
	state, statePath, journalPath := prepareRegistrationRuntime(t, fixture)
	writeRegistrationJSON(t, statePath, state)

	next, err := Register(fixture.root, statePath, journalPath, Request{
		ExpectedRevision: 8,
		ManifestPath:     filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef)),
		TaskID:           fixture.taskID,
		TaskPath:         filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef)),
		OccurredAt:       time.Date(2026, 10, 14, 12, 10, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Register explicit closing contract: %v", err)
	}
	entities := next.State["entities"].(map[string]any)
	rows := entities["assignments"].([]any)
	if len(rows) != 1 {
		t.Fatalf("Assignment Records = %d, want one", len(rows))
	}
	encoded, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var record Record
	if err := json.Unmarshal(encoded, &record); err != nil {
		t.Fatal(err)
	}
	if record.ClosingContract == nil || record.ClosingContract.Contract != assignmentrecord.ClosingContractVersion || record.ClosingContract.WorkerChecks == nil || len(record.ClosingContract.WorkerChecks) != 0 {
		t.Fatalf("canonical empty closing contract = %+v", record.ClosingContract)
	}
	if record.IntegrationCheckMode != "post_merge" {
		t.Fatalf("canonical integration_check_mode = %q, want post_merge", record.IntegrationCheckMode)
	}
	if !reflect.DeepEqual(record.RequiredChecks, []string{"go test ./pkg"}) || len(record.CheckProfiles) != 1 || record.CheckProfiles[0].Location != "main" {
		t.Fatalf("Main check denominator/location changed: checks=%v profiles=%+v", record.RequiredChecks, record.CheckProfiles)
	}
	if !reflect.DeepEqual(record.EffectiveWritePaths, []string{"src/pkg"}) {
		t.Fatalf("canonical effective scope = %v, want [src/pkg]", record.EffectiveWritePaths)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("assignment-record.schema.json", encoded); err != nil {
		t.Fatalf("standalone schema rejected registered canonical Record: %v", err)
	}
}

func TestRegisterS6RejectsInvalidClosingContractBeforeWriterCommit(t *testing.T) {
	for _, test := range []struct {
		name          string
		contract      string
		mode          string
		mainLocation  string
		wantErrorPart string
	}{
		{name: "missing explicit post merge mode", contract: assignmentrecord.ClosingContractVersion, mainLocation: "main", wantErrorPart: "integration_check_mode"},
		{name: "unsupported contract version", contract: "s6-closing-v2", mode: "post_merge", mainLocation: "main", wantErrorPart: "contract"},
		{name: "main check profile assigned to worker", contract: assignmentrecord.ClosingContractVersion, mode: "post_merge", mainLocation: "worker", wantErrorPart: "main"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
			configureRegistrationClosing(t, &fixture, test.contract, test.mode, test.mainLocation, false)
			state, statePath, journalPath := prepareRegistrationRuntime(t, fixture)
			writeRegistrationJSON(t, statePath, state)
			beforeState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Register(fixture.root, statePath, journalPath, Request{
				ExpectedRevision: 8,
				ManifestPath:     filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef)),
				TaskID:           fixture.taskID,
				TaskPath:         filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef)),
				OccurredAt:       time.Date(2026, 10, 14, 12, 11, 0, 0, time.UTC),
			})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.wantErrorPart)) {
				t.Fatalf("Register error = %v, want text %q", err, test.wantErrorPart)
			}
			afterState, readErr := os.ReadFile(statePath)
			if readErr != nil || !bytes.Equal(beforeState, afterState) {
				t.Fatalf("rejected closing contract changed Runtime state: read err=%v", readErr)
			}
			if _, statErr := os.Stat(activationEnvelopePath(fixture.root, "workgroup-s6-registration", fixture.taskID, "agent-s6-registration")); !os.IsNotExist(statErr) {
				t.Fatalf("rejected closing contract left activation artifact (stat err=%v)", statErr)
			}
			if _, statErr := os.Stat(journalPath); !os.IsNotExist(statErr) {
				t.Fatalf("rejected closing contract wrote Runtime journal (stat err=%v)", statErr)
			}
		})
	}
}

func TestRegisterS6RejectsWorkerClosingProfileSourceDriftBeforeWriterCommit(t *testing.T) {
	fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
	const workerPath = "src/pkg/worker_input.go"
	configureRegistrationClosing(t, &fixture, assignmentrecord.ClosingContractVersion, "post_merge", "main", true)
	records, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, fixture.manifest, fixture.manifestRef, fixture.taskRef, fixture.taskID, fixture.task)
	if err != nil {
		t.Fatalf("freeze nonempty Worker closing profile: %v", err)
	}
	if len(records) != 1 || records[0].ClosingContract == nil || len(records[0].ClosingContract.WorkerChecks) != 1 {
		t.Fatalf("nonempty Worker closing profile was not structurally frozen: %+v", records)
	}
	if !reflect.DeepEqual(records[0].RequiredChecks, []string{"go test ./pkg"}) || len(records[0].CheckProfiles) != 1 || records[0].CheckProfiles[0].Location != "main" {
		t.Fatalf("Worker profile changed Main integration denominator: checks=%v profiles=%+v", records[0].RequiredChecks, records[0].CheckProfiles)
	}
	foundWorkerDependency := false
	for _, source := range records[0].ContextSources {
		if source.Kind == "worker_check_profile_dependency" && source.ID == "worker-profile-s6-registration" && source.Ref == workerPath {
			foundWorkerDependency = true
		}
	}
	if !foundWorkerDependency {
		t.Fatalf("canonical Record omitted the Worker profile dependency source %s", workerPath)
	}
	workerFile := filepath.Join(fixture.root, filepath.FromSlash(workerPath))
	if err := os.WriteFile(workerFile, []byte("package pkg\n// changed after freeze\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, statePath, journalPath := prepareRegistrationRuntime(t, fixture)
	writeRegistrationJSON(t, statePath, state)
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Register(fixture.root, statePath, journalPath, Request{
		ExpectedRevision: 8,
		ManifestPath:     filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef)),
		TaskID:           fixture.taskID,
		TaskPath:         filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef)),
		OccurredAt:       time.Date(2026, 10, 14, 12, 12, 0, 0, time.UTC),
	})
	if err == nil || !strings.Contains(err.Error(), "formal file view and checkout differ") {
		t.Fatalf("Worker CheckProfile source drift error = %v", err)
	}
	afterState, readErr := os.ReadFile(statePath)
	if readErr != nil || !bytes.Equal(beforeState, afterState) {
		t.Fatalf("rejected Worker profile drift changed Runtime state: read err=%v", readErr)
	}
	if _, statErr := os.Stat(journalPath); !os.IsNotExist(statErr) {
		t.Fatalf("rejected Worker profile drift wrote Runtime journal (stat err=%v)", statErr)
	}
}

func TestRegisterLegacyManifestDoesNotCreateAssignmentRecords(t *testing.T) {
	fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
	var value map[string]any
	if err := json.Unmarshal(fixture.manifest, &value); err != nil {
		t.Fatal(err)
	}
	delete(value, "assignment_record_contract")
	manifestBytes, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef))
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	state, statePath, journalPath := prepareRegistrationRuntime(t, fixture)
	writeRegistrationJSON(t, statePath, state)
	_, err = Register(fixture.root, statePath, journalPath, Request{
		ExpectedRevision: 8,
		ManifestPath:     manifestPath,
		TaskID:           fixture.taskID,
		TaskPath:         filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef)),
		OccurredAt:       time.Date(2026, 10, 14, 12, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("legacy Register: %v", err)
	}
	committed, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var committedState map[string]any
	if err := json.Unmarshal(committed, &committedState); err != nil {
		t.Fatal(err)
	}
	entities := committedState["entities"].(map[string]any)
	if rows, ok := entities["assignments"].([]any); !ok || len(rows) != 0 {
		t.Fatalf("legacy registration Assignment Records = %#v, want empty", entities["assignments"])
	}
}

func TestRegisterS6SourceDriftLeavesRuntimePairAndActivationUntouched(t *testing.T) {
	fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
	manifestPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef))
	state, statePath, journalPath := prepareRegistrationRuntime(t, fixture)
	writeRegistrationJSON(t, statePath, state)
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, journalErr := os.ReadFile(journalPath)
	if journalErr != nil && !os.IsNotExist(journalErr) {
		t.Fatal(journalErr)
	}

	dependency := filepath.Join(fixture.root, "src", "pkg", "input.go")
	if err := os.WriteFile(dependency, []byte("package pkg\n// uncommitted source drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Register(fixture.root, statePath, journalPath, Request{
		ExpectedRevision: 8,
		ManifestPath:     manifestPath,
		TaskID:           fixture.taskID,
		TaskPath:         filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef)),
		OccurredAt:       time.Date(2026, 10, 14, 12, 2, 0, 0, time.UTC),
	})
	if err == nil || !strings.Contains(err.Error(), "formal file view and checkout differ") {
		t.Fatalf("source drift error = %v", err)
	}
	afterState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeState, afterState) {
		t.Fatal("rejected source drift changed Runtime state")
	}
	afterJournal, afterJournalErr := os.ReadFile(journalPath)
	if journalErr == nil {
		if afterJournalErr != nil || !bytes.Equal(beforeJournal, afterJournal) {
			t.Fatal("rejected source drift changed Runtime journal")
		}
	} else if !os.IsNotExist(afterJournalErr) {
		t.Fatalf("rejected source drift left or changed Runtime journal: %v", afterJournalErr)
	}
	activationPath := activationEnvelopePath(fixture.root, "workgroup-s6-registration", fixture.taskID, "agent-s6-registration")
	if _, statErr := os.Stat(activationPath); !os.IsNotExist(statErr) {
		t.Fatalf("rejected source drift left activation artifact %s (stat err=%v)", activationPath, statErr)
	}
	if entries, readErr := os.ReadDir(filepath.Dir(activationPath)); readErr == nil && len(entries) != 0 {
		t.Fatalf("rejected source drift left staged activation files: %v", entries)
	} else if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
}

func prepareRegistrationRuntime(t *testing.T, fixture recordRegistrationFixture) (map[string]any, string, string) {
	t.Helper()
	return prepareRegistrationRuntimeVersion(t, fixture, "1.2.0")
}

func prepareRegistrationRuntimeVersion(t *testing.T, fixture recordRegistrationFixture, schemaVersion string) (map[string]any, string, string) {
	t.Helper()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = schemaVersion
	state["runtime_id"] = "loop-s6-registration"
	state["revision"] = float64(8)
	delete(state, "milestone")
	state["last_transition"] = nil
	state["definition"] = registrationFingerprint(t, fixture.root, "docs/control/loop-definition.json", "1.3.0")
	state["hook_control"].(map[string]any)["policy_ref"] = registrationFingerprint(t, fixture.root, "docs/control/hook-policy.json", "v2.0.0")
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": float64(0)}
	state["bound_req"] = map[string]any{
		"path": "docs/requirements/REQ-001.md", "version": "v1.0.0", "sha256": strings.Repeat("1", 64),
		"id": "REQ-001", "status": "locked", "approved_by": "user", "approved_at": "2026-10-14T00:00:00Z",
		"workspace": map[string]any{"project_root": fixture.root, "dev_branch": "main", "release_upstream": "main", "bound_commit": "baseline"},
	}
	taskSHA := registrationSHA(fixture.task)
	indexRef := "docs/dev/tasks/index-REQ-001.md"
	indexBytes, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(indexRef)))
	if err != nil {
		t.Fatal(err)
	}
	state["documents"] = []any{
		map[string]any{"id": fixture.taskID, "kind": "task", "path": fixture.taskRef, "version": "v1.0.0", "sha256": taskSHA, "status": "locked", "generation": float64(1)},
		map[string]any{"id": "dispatch-plan:REQ-001", "kind": "dispatch_plan", "path": indexRef, "version": "1", "sha256": registrationSHA(indexBytes), "status": "locked", "generation": float64(1)},
	}
	state["baseline"] = map[string]any{"generation": float64(1), "captured_at": "2026-10-14T00:00:00Z"}
	state["entities"] = map[string]any{
		"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}, "assignments": []any{},
	}
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": float64(0), "last_event_id": nil}
	statePath := filepath.Join(fixture.root, ".claude", "loop-state.json")
	journalPath := filepath.Join(fixture.root, ".claude", "loop-events.jsonl")
	return state, statePath, journalPath
}

func narrowRegistrationFixtureScope(t *testing.T, fixture recordRegistrationFixture) {
	t.Helper()
	manifestPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef))
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	assignment := manifest["assignments"].([]any)[0].(map[string]any)
	assignment["scope"] = []string{"src/pkg"}
	assignment["write_paths"] = []string{"src"}
	assignment["output_paths"] = []string{}
	updated, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(manifestPath, updated, 0o644); err != nil {
		t.Fatal(err)
	}
	gitRegistration(t, fixture.root, "add", fixture.manifestRef)
	gitRegistration(t, fixture.root, "commit", "-m", "review narrow assignment authority")
}

func registrationFingerprint(t *testing.T, root, path, version string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"path": path, "version": version, "sha256": registrationSHA(data)}
}

func writeRegistrationJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readRegistrationJournal(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var events []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

func TestRegisterS6AgentBeginRunsCanonicalThinPlanToWorking(t *testing.T) {
	fixture, statePath, journalPath, record, stagedRef := registerAgentBeginFixture(t, true)
	planPath := writeThinS6Plan(t, fixture, record, "msg-s6-plan-happy", "corr-s6-plan-happy")

	snapshot, outcome, err := AgentBegin(fixture.root, statePath, journalPath, AgentBeginRequest{
		ExpectedRevision: -1,
		AgentID:          record.OwnerAgentID,
		PlanPath:         planPath,
		OccurredAt:       time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AgentBegin: %v", err)
	}
	if !outcome.Chained || outcome.FinalState != "working" {
		t.Fatalf("AgentBegin outcome = %+v, want a working chain", outcome)
	}
	agent := registrationAgent(snapshot.State, record.OwnerAgentID)
	if agent["state"] != "working" || agent["plan_reported_ref"] != filepath.ToSlash(mustRelative(t, fixture.root, planPath)) {
		t.Fatalf("final Agent state/readback = %v / %v", agent["state"], agent["plan_reported_ref"])
	}
	activationRef, _ := agent["activation_ref"].(string)
	if activationRef == "" || activationRef == stagedRef {
		t.Fatalf("activation_ref = %q, want the activated message ref distinct from original pre-stage %q", activationRef, stagedRef)
	}
	activationData, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(activationRef)))
	if err != nil {
		t.Fatalf("read activation message ref %s: %v", activationRef, err)
	}
	var activation map[string]any
	if err := json.Unmarshal(activationData, &activation); err != nil {
		t.Fatal(err)
	}
	if activation["assignment_record_sha256"] != mustRecordDigest(t, record) || !reflect.DeepEqual(activation["allowed_write_paths"], []any{"src/pkg"}) {
		t.Fatalf("activation message projection = %v / %v", activation["assignment_record_sha256"], activation["allowed_write_paths"])
	}
	if _, err := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(stagedRef))); err != nil {
		t.Fatalf("original pre-staged activation envelope %s is not recoverable: %v", stagedRef, err)
	}
	assertRegistrationJournalTail(t, journalPath, "AGENT-LIFECYCLE", 4)
}

func TestRegisterS6PlanAndActivationGuardsRejectTamperedProjectionAndRevision(t *testing.T) {
	t.Run("PLAN record digest", func(t *testing.T) {
		fixture, statePath, journalPath, record, _ := registerAgentBeginFixture(t, true)
		planPath := writeThinS6Plan(t, fixture, record, "msg-s6-plan-bad-digest", "corr-s6-plan-bad-digest")
		data, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatal(err)
		}
		var plan map[string]any
		if err := json.Unmarshal(data, &plan); err != nil {
			t.Fatal(err)
		}
		plan["assignment_record_sha256"] = strings.Repeat("0", 64)
		writeRegistrationJSON(t, planPath, plan)
		beforeState, beforeJournal := registrationPairBytes(t, statePath, journalPath)
		_, _, err = AgentBegin(fixture.root, statePath, journalPath, AgentBeginRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, PlanPath: planPath})
		if err == nil || !strings.Contains(err.Error(), "does not bind the current Assignment") {
			t.Fatalf("tampered thin PLAN digest error = %v", err)
		}
		assertRegistrationPairUnchanged(t, statePath, journalPath, beforeState, beforeJournal)
	})

	t.Run("activation scope projection", func(t *testing.T) {
		fixture, statePath, journalPath, record, _ := registerAgentBeginFixture(t, true)
		planPath := writeThinS6Plan(t, fixture, record, "msg-s6-plan-bad-projection", "corr-s6-plan-bad-projection")
		submitted, err := AdvanceAgent(fixture.root, statePath, journalPath, AgentEventRequest{
			ExpectedRevision: 9, AgentID: record.OwnerAgentID, Event: "readback_submitted", MessagePath: planPath,
			OccurredAt: time.Date(2026, 10, 14, 13, 1, 0, 0, time.UTC),
		})
		if err != nil || submitted.Revision != 10 {
			t.Fatalf("readback_submitted = revision %d, err %v", submitted.Revision, err)
		}
		activation := buildS6ActivationProjection(t, fixture, record, planPath, 10, "msg-s6-activation-bad-projection", "corr-s6-activation-bad-projection")
		activation["allowed_write_paths"] = []string{"src"}
		activationPath := writeRegistrationMessage(t, fixture.root, "bad-scope-activation.json", activation)
		beforeState, beforeJournal := registrationPairBytes(t, statePath, journalPath)
		_, err = AdvanceAgent(fixture.root, statePath, journalPath, AgentEventRequest{
			ExpectedRevision: 10, AgentID: record.OwnerAgentID, Event: "activation_sent", MessagePath: activationPath,
			OccurredAt: time.Date(2026, 10, 14, 13, 2, 0, 0, time.UTC),
		})
		if err == nil || !strings.Contains(err.Error(), "projection write paths differ") {
			t.Fatalf("tampered activation projection error = %v", err)
		}
		assertRegistrationPairUnchanged(t, statePath, journalPath, beforeState, beforeJournal)
	})

	t.Run("activation expected revision", func(t *testing.T) {
		fixture, statePath, journalPath, record, _ := registerAgentBeginFixture(t, true)
		planPath := writeThinS6Plan(t, fixture, record, "msg-s6-plan-stale-activation", "corr-s6-plan-stale-activation")
		submitted, err := AdvanceAgent(fixture.root, statePath, journalPath, AgentEventRequest{
			ExpectedRevision: 9, AgentID: record.OwnerAgentID, Event: "readback_submitted", MessagePath: planPath,
		})
		if err != nil || submitted.Revision != 10 {
			t.Fatalf("readback_submitted = revision %d, err %v", submitted.Revision, err)
		}
		activation := buildS6ActivationProjection(t, fixture, record, planPath, 10, "msg-s6-activation-stale-revision", "corr-s6-activation-stale-revision")
		activation["expected_runtime_revision"] = float64(9)
		activationPath := writeRegistrationMessage(t, fixture.root, "stale-revision-activation.json", activation)
		beforeState, beforeJournal := registrationPairBytes(t, statePath, journalPath)
		_, err = AdvanceAgent(fixture.root, statePath, journalPath, AgentEventRequest{
			ExpectedRevision: 10, AgentID: record.OwnerAgentID, Event: "activation_sent", MessagePath: activationPath,
		})
		if err == nil || !strings.Contains(err.Error(), "activation expected revision is stale") {
			t.Fatalf("stale activation revision error = %v", err)
		}
		assertRegistrationPairUnchanged(t, statePath, journalPath, beforeState, beforeJournal)
	})
}

func TestRegisterS6AgentBeginRejectsChangedRecordSourceWithoutRuntimeMutation(t *testing.T) {
	fixture, statePath, journalPath, record, _ := registerAgentBeginFixture(t, true)
	planPath := writeThinS6Plan(t, fixture, record, "msg-s6-plan-source-drift", "corr-s6-plan-source-drift")
	beforeState, beforeJournal := registrationPairBytes(t, statePath, journalPath)
	sourcePath := filepath.Join(fixture.root, filepath.FromSlash(record.AgentDefinition.Ref))
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, append(data, []byte("\n<!-- post-registration drift -->\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = AgentBegin(fixture.root, statePath, journalPath, AgentBeginRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, PlanPath: planPath,
	})
	if err == nil || !strings.Contains(err.Error(), "changed after registration") {
		t.Fatalf("changed source error = %v", err)
	}
	assertRegistrationPairUnchanged(t, statePath, journalPath, beforeState, beforeJournal)
}

func TestRegisterS6AgentBeginResumesOriginalActivationAndUsesRecordWithoutEnvelope(t *testing.T) {
	t.Run("resume through the original activated reference", func(t *testing.T) {
		fixture, statePath, journalPath, record, stagedRef := registerAgentBeginFixture(t, true)
		planPath := writeThinS6Plan(t, fixture, record, "msg-s6-plan-resume-activated", "corr-s6-plan-resume-activated")
		if _, err := AdvanceAgent(fixture.root, statePath, journalPath, AgentEventRequest{
			ExpectedRevision: 9, AgentID: record.OwnerAgentID, Event: "readback_submitted", MessagePath: planPath,
		}); err != nil {
			t.Fatalf("readback_submitted: %v", err)
		}
		activation := buildS6ActivationProjection(t, fixture, record, planPath, 10, "msg-s6-activation-resume", "corr-s6-activation-resume")
		activationPath, err := WriteActivationMessageFile(fixture.root, record.TeamID, fixture.taskID, record.OwnerAgentID, activation)
		if err != nil {
			t.Fatal(err)
		}
		activated, err := AdvanceAgent(fixture.root, statePath, journalPath, AgentEventRequest{
			ExpectedRevision: 10, AgentID: record.OwnerAgentID, Event: "activation_sent", MessagePath: activationPath,
		})
		if err != nil {
			t.Fatalf("activation_sent: %v", err)
		}
		originalActivatedRef := recordRegistrationString(registrationAgent(activated.State, record.OwnerAgentID)["activation_ref"])
		if originalActivatedRef != filepath.ToSlash(mustRelative(t, fixture.root, activationPath)) {
			t.Fatalf("activated Agent ref = %q, want %q", originalActivatedRef, activationPath)
		}
		snapshot, outcome, err := AgentBegin(fixture.root, statePath, journalPath, AgentBeginRequest{
			ExpectedRevision: -1, AgentID: record.OwnerAgentID, PlanPath: planPath,
		})
		if err != nil || !outcome.Chained || outcome.FinalState != "working" {
			t.Fatalf("AgentBegin resume outcome=%+v err=%v", outcome, err)
		}
		finalAgent := registrationAgent(snapshot.State, record.OwnerAgentID)
		if finalAgent["state"] != "working" || finalAgent["activation_ref"] != originalActivatedRef {
			t.Fatalf("resumed Agent state/ref = %v / %v", finalAgent["state"], finalAgent["activation_ref"])
		}
		if _, err := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(stagedRef))); err != nil {
			t.Fatalf("original staged activation envelope was lost during resume: %v", err)
		}
	})

	t.Run("missing staged envelope recovers with record scope", func(t *testing.T) {
		fixture, statePath, journalPath, record, stagedRef := registerAgentBeginFixture(t, true)
		planPath := writeThinS6Plan(t, fixture, record, "msg-s6-plan-record-recovery", "corr-s6-plan-record-recovery")
		stateData, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		var state map[string]any
		if err := json.Unmarshal(stateData, &state); err != nil {
			t.Fatal(err)
		}
		registrationAgent(state, record.OwnerAgentID)["activation_ref"] = ""
		writeRegistrationJSON(t, statePath, state)
		if err := os.Remove(filepath.Join(fixture.root, filepath.FromSlash(stagedRef))); err != nil {
			t.Fatal(err)
		}

		snapshot, outcome, err := AgentBegin(fixture.root, statePath, journalPath, AgentBeginRequest{
			ExpectedRevision: -1, AgentID: record.OwnerAgentID, PlanPath: planPath,
		})
		if err != nil || !outcome.Chained || outcome.FinalState != "working" {
			t.Fatalf("record-based AgentBegin recovery outcome=%+v err=%v", outcome, err)
		}
		agent := registrationAgent(snapshot.State, record.OwnerAgentID)
		if agent["state"] != "working" {
			t.Fatalf("recovered Agent state = %v", agent["state"])
		}
		activationRef := recordRegistrationString(agent["activation_ref"])
		data, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(activationRef)))
		if err != nil {
			t.Fatalf("read recovered activation projection %q: %v", activationRef, err)
		}
		var projection map[string]any
		if err := json.Unmarshal(data, &projection); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(projection["allowed_write_paths"], []any{"src/pkg"}) || projection["assignment_record_sha256"] != mustRecordDigest(t, record) {
			t.Fatalf("recovered activation widened or detached from the Record: paths=%v record_sha=%v", projection["allowed_write_paths"], projection["assignment_record_sha256"])
		}
	})

	t.Run("changed manifest cannot widen recovered authority", func(t *testing.T) {
		fixture, statePath, journalPath, record, stagedRef := registerAgentBeginFixture(t, true)
		planPath := writeThinS6Plan(t, fixture, record, "msg-s6-plan-reject-manifest-drift", "corr-s6-plan-reject-manifest-drift")
		stateData, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		var state map[string]any
		if err := json.Unmarshal(stateData, &state); err != nil {
			t.Fatal(err)
		}
		registrationAgent(state, record.OwnerAgentID)["activation_ref"] = ""
		writeRegistrationJSON(t, statePath, state)
		if err := os.Remove(filepath.Join(fixture.root, filepath.FromSlash(stagedRef))); err != nil {
			t.Fatal(err)
		}
		broadenRegistrationManifest(t, fixture)
		beforeState, beforeJournal := registrationPairBytes(t, statePath, journalPath)
		_, _, err = AgentBegin(fixture.root, statePath, journalPath, AgentBeginRequest{
			ExpectedRevision: -1, AgentID: record.OwnerAgentID, PlanPath: planPath,
		})
		if err == nil || !strings.Contains(err.Error(), "manifest-s6-registration.json changed after registration") {
			t.Fatalf("changed manifest error = %v", err)
		}
		assertRegistrationPairUnchanged(t, statePath, journalPath, beforeState, beforeJournal)
	})
}

func registerAgentBeginFixture(t *testing.T, narrowScope bool) (recordRegistrationFixture, string, string, Record, string) {
	t.Helper()
	return registerAgentBeginConfiguredFixtureVersion(t, narrowScope, nil, "1.2.0")
}

func registerAgentBeginConfiguredFixture(t *testing.T, narrowScope bool, configure func(*recordRegistrationFixture)) (recordRegistrationFixture, string, string, Record, string) {
	t.Helper()
	return registerAgentBeginConfiguredFixtureVersion(t, narrowScope, configure, "1.2.0")
}

func registerAgentBeginConfiguredFixtureVersion(t *testing.T, narrowScope bool, configure func(*recordRegistrationFixture), schemaVersion string) (recordRegistrationFixture, string, string, Record, string) {
	t.Helper()
	writePath := "src/pkg"
	if narrowScope {
		writePath = "src"
	}
	fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{writePath: writePath})
	if narrowScope {
		narrowRegistrationFixtureScope(t, fixture)
	}
	if configure != nil {
		configure(&fixture)
	}
	state, statePath, journalPath := prepareRegistrationRuntimeVersion(t, fixture, schemaVersion)
	writeRegistrationJSON(t, statePath, state)
	_, err := Register(fixture.root, statePath, journalPath, Request{
		ExpectedRevision: 8,
		ManifestPath:     filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef)),
		TaskID:           fixture.taskID,
		TaskPath:         filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef)),
		OccurredAt:       time.Date(2026, 10, 14, 12, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Register S6 fixture: %v", err)
	}
	postStateData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var postState map[string]any
	if err := json.Unmarshal(postStateData, &postState); err != nil {
		t.Fatal(err)
	}
	var record Record
	for _, raw := range postState["entities"].(map[string]any)["assignments"].([]any) {
		data, err := json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		var candidate Record
		if err := json.Unmarshal(data, &candidate); err != nil {
			t.Fatal(err)
		}
		if candidate.OwnerAgentID == "agent-s6-registration" {
			record = candidate
			break
		}
	}
	if record.AssignmentID == "" {
		t.Fatal("Register did not persist the canonical Assignment Record")
	}
	stagedRef := recordRegistrationString(registrationAgent(postState, record.OwnerAgentID)["activation_ref"])
	if stagedRef == "" {
		t.Fatal("Register did not pre-stage the S6 activation envelope")
	}
	return fixture, statePath, journalPath, record, stagedRef
}

func writeThinS6Plan(t *testing.T, fixture recordRegistrationFixture, record Record, messageID, correlationID string) string {
	t.Helper()
	digest := mustRecordDigest(t, record)
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "plan_report", "message_id": messageID, "correlation_id": correlationID,
		"runtime_id": record.RuntimeID, "agent_id": record.OwnerAgentID, "agent_definition_ref": record.AgentDefinition.Ref,
		"task_id": record.Task.ID, "bug_id": nil, "team_id": record.TeamID, "occurred_at": "2026-10-14T12:31:00Z",
		"assignment_id": record.AssignmentID, "assignment_revision": record.Revision,
		"assignment_record_contract": AssignmentRecordContract, "assignment_record_sha256": digest,
		"objective":        "Implement the reviewed S6 TASK inside its locked scope",
		"planned_paths":    []string{"src/pkg/input.go"},
		"assertion_checks": []any{map[string]any{"assertion": "TASK behavior is implemented", "oracle": "the selected required check passes"}},
		"dependencies":     []string{}, "risks_blockers": []string{},
	}
	if record.ClosingContract != nil {
		indexes := make([]int, len(record.DoneWhen))
		for i := range indexes {
			indexes[i] = i
		}
		message["assertion_checks"].([]any)[0].(map[string]any)["done_when_indexes"] = indexes
	}
	return writeRegistrationMessage(t, fixture.root, "thin-plan-"+messageID+".json", message)
}

func buildS6ActivationProjection(t *testing.T, fixture recordRegistrationFixture, record Record, planPath string, revision int, messageID, correlationID string) map[string]any {
	t.Helper()
	digest := mustRecordDigest(t, record)
	ref := record.Reference()
	activation, err := BuildPlanCheckpointActivationMessage(
		fixture.root, record.RuntimeID, record.OwnerAgentID, record.AgentDefinition.Ref, record.Task.ID, record.TeamID,
		revision, planPath,
		ActivationSourceEntry{
			AssignmentRecord: &ref, AssignmentRecordSHA256: digest, AgentID: record.OwnerAgentID,
			AgentDefinitionRef: record.AgentDefinition.Ref, SkillRefs: record.SkillRefs,
			WritePaths: record.EffectiveWritePaths,
		},
		"2026-10-14T12:32:00Z",
	)
	if err != nil {
		t.Fatal(err)
	}
	activation["message_id"] = messageID
	activation["correlation_id"] = correlationID
	return activation
}

func writeRegistrationMessage(t *testing.T, root, name string, message map[string]any) string {
	t.Helper()
	path := filepath.Join(root, ".claude", "evidence", "workgroup-s6-registration-round-1", "TASK-001-01", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRegistrationJSON(t, path, message)
	return path
}

func registrationAgent(state map[string]any, agentID string) map[string]any {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] == agentID {
			return row
		}
	}
	return nil
}

func mustRecordDigest(t *testing.T, record Record) string {
	t.Helper()
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func mustRelative(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}

func registrationPairBytes(t *testing.T, statePath, journalPath string) ([]byte, []byte) {
	t.Helper()
	state, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	return state, journal
}

func assertRegistrationPairUnchanged(t *testing.T, statePath, journalPath string, state, journal []byte) {
	t.Helper()
	afterState, afterJournal := registrationPairBytes(t, statePath, journalPath)
	if !bytes.Equal(state, afterState) || !bytes.Equal(journal, afterJournal) {
		t.Fatal("rejected S6 AgentBegin/Agent event changed Runtime state or journal")
	}
}

func assertRegistrationJournalTail(t *testing.T, path, transitionID string, wantEvents int) {
	t.Helper()
	events := readRegistrationJournal(t, path)
	if len(events) != wantEvents {
		t.Fatalf("journal events = %d, want %d", len(events), wantEvents)
	}
	if events[0]["event"] != "transition_committed" || events[0]["transition_id"] != "ENTITY-REGISTER" {
		t.Fatalf("first journal event is not registration: %#v", events[0])
	}
	for index := 1; index < len(events); index++ {
		if events[index]["event"] != "transition_committed" || events[index]["transition_id"] != transitionID || events[index]["sequence"] != float64(index+1) {
			t.Fatalf("journal event %d has unexpected lifecycle binding: %#v", index, events[index])
		}
	}
}

func broadenRegistrationManifest(t *testing.T, fixture recordRegistrationFixture) {
	t.Helper()
	path := filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	assignment := manifest["assignments"].([]any)[0].(map[string]any)
	assignment["scope"] = []string{"src"}
	assignment["write_paths"] = []string{"src"}
	if err := writeRegistrationJSONAndReturn(path, manifest); err != nil {
		t.Fatal(err)
	}
}

func writeRegistrationJSONAndReturn(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
