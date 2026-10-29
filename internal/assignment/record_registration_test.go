package assignment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/catalog"
)

func TestBuildRegisteredAssignmentRecordsUsesLockedReviewedSources(t *testing.T) {
	fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
	records, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, fixture.manifest, fixture.manifestRef, fixture.taskRef, fixture.taskID, fixture.task)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want one", len(records))
	}
	record := records[0]
	if record.RuntimeID != "loop-s6-registration" || record.BaselineGeneration != 1 || record.RegistrationRevision != 9 {
		t.Fatalf("Runtime binding = %s/%d/%d", record.RuntimeID, record.BaselineGeneration, record.RegistrationRevision)
	}
	if record.AssignmentID != "assignment-s6-registration" || record.OwnerAgentID != "agent-s6-registration" || record.Task.ID != fixture.taskID || record.Task.Ref != fixture.taskRef {
		t.Fatalf("record identity = %+v", record)
	}
	if got := strings.Join(record.EffectiveWritePaths, ","); got != "src/pkg" {
		t.Fatalf("effective writes = %q, want reviewed intersection src/pkg", got)
	}
	if len(record.CheckProfiles) != 1 || len(record.ContextSources) == 0 {
		t.Fatalf("CheckProfile or source closure missing: profiles=%d sources=%d", len(record.CheckProfiles), len(record.ContextSources))
	}
	if err := ValidateRecord(record); err != nil {
		t.Fatal(err)
	}
}

func TestBuildRegisteredAssignmentRecordsWorksWithoutHarnessInternalTree(t *testing.T) {
	fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
	for _, path := range []string{
		"internal/schema/assets/team-manifest.schema.json",
		"internal/schema/assets/check-profile.schema.json",
		"internal/schema/assets/assignment-record.schema.json",
		"internal/schema/assets/agent-message.schema.json",
		"internal/agentcontext/agent-context-profiles.json",
	} {
		if _, err := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(path))); !os.IsNotExist(err) {
			t.Fatalf("installed-project fixture unexpectedly has %s (stat err=%v)", path, err)
		}
	}
	records, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, fixture.manifest, fixture.manifestRef, fixture.taskRef, fixture.taskID, fixture.task)
	if err != nil {
		t.Fatalf("build records without source-tree internals: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want one", len(records))
	}
	var schemas, profiles int
	for _, source := range records[0].ContextSources {
		switch source.Kind {
		case "embedded_schema":
			schemas++
			data, err := assignmentinputs.Read(fixture.root, source.Ref)
			if err != nil || source.SHA256 != registrationSHA(data) {
				t.Fatalf("embedded schema source %s is not bound to binary bytes: %v", source.Ref, err)
			}
		case "embedded_context_profile":
			profiles++
			data, err := assignmentinputs.Read(fixture.root, source.Ref)
			if err != nil || source.SHA256 != registrationSHA(data) {
				t.Fatalf("embedded context profile is not bound to binary bytes: %v", err)
			}
		}
	}
	if schemas != 4 || profiles != 1 {
		t.Fatalf("embedded source closure has %d schemas and %d profiles, want 4 and 1", schemas, profiles)
	}
}

func TestBuildRegisteredAssignmentRecordsUsesUntrackedInstalledInputs(t *testing.T) {
	fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
	for _, path := range []string{".claude/workgroups", ".claude/agents", ".claude/skills"} {
		if err := exec.Command("git", "-C", fixture.root, "rm", "--cached", "-r", "--", path).Run(); err != nil {
			t.Fatalf("remove installed input %s from Git index: %v", path, err)
		}
		if info, err := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(path))); err != nil || !info.IsDir() {
			t.Fatalf("installed input directory %s is missing: %v", path, err)
		}
		tracked, err := exec.Command("git", "-C", fixture.root, "ls-files", "--", path).Output()
		if err != nil {
			t.Fatalf("check Git tracking for %s: %v", path, err)
		}
		if strings.TrimSpace(string(tracked)) != "" {
			t.Fatalf("installed input %s is still Git-tracked: %q", path, tracked)
		}
	}
	records, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, fixture.manifest, fixture.manifestRef, fixture.taskRef, fixture.taskID, fixture.task)
	if err != nil {
		t.Fatalf("build records from formal installed-input file sources: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want one", len(records))
	}
}

func TestBuildRegisteredAssignmentRecordsRejectsStaleOrUnboundInputs(t *testing.T) {
	t.Run("runtime version", func(t *testing.T) {
		fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
		fixture.state["schema_version"] = "1.1.0"
		if _, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, fixture.manifest, fixture.manifestRef, fixture.taskRef, fixture.taskID, fixture.task); err == nil || !strings.Contains(err.Error(), "schema 1.2.0") {
			t.Fatalf("legacy Runtime error = %v", err)
		}
	})
	t.Run("uncommitted opt-in manifest bytes", func(t *testing.T) {
		fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
		changed := append(append([]byte(nil), fixture.manifest...), '\n')
		if _, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, changed, fixture.manifestRef, fixture.taskRef, fixture.taskID, fixture.task); err == nil || !strings.Contains(err.Error(), "manifest bytes differ") {
			t.Fatalf("manifest drift error = %v", err)
		}
	})
	t.Run("legacy manifest cannot opt in implicitly", func(t *testing.T) {
		fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
		var manifest map[string]any
		if err := json.Unmarshal(fixture.manifest, &manifest); err != nil {
			t.Fatal(err)
		}
		delete(manifest, "assignment_record_contract")
		legacy, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, legacy, fixture.manifestRef, fixture.taskRef, fixture.taskID, fixture.task); err == nil || !strings.Contains(err.Error(), "explicit s6-v1.2") {
			t.Fatalf("implicit opt-in error = %v", err)
		}
	})
	t.Run("uncommitted opt-in TASK bytes", func(t *testing.T) {
		fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
		changed := append(append([]byte(nil), fixture.task...), []byte("\nlocal edit\n")...)
		if _, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, fixture.manifest, fixture.manifestRef, fixture.taskRef, fixture.taskID, changed); err == nil || !strings.Contains(err.Error(), "TASK bytes differ") {
			t.Fatalf("TASK drift error = %v", err)
		}
	})
	t.Run("checkout differs from git view", func(t *testing.T) {
		fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
		path := filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef))
		if err := os.WriteFile(path, append(append([]byte(nil), fixture.task...), []byte("\nlocal edit\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, fixture.manifest, fixture.manifestRef, fixture.taskRef, fixture.taskID, fixture.task); err == nil || !strings.Contains(err.Error(), "view and checkout differ") {
			t.Fatalf("checkout drift error = %v", err)
		}
	})
	t.Run("profile dependency SHA", func(t *testing.T) {
		fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{badProfileSHA: true})
		if _, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, fixture.manifest, fixture.manifestRef, fixture.taskRef, fixture.taskID, fixture.task); err == nil || !strings.Contains(err.Error(), "declared SHA differs") {
			t.Fatalf("profile source error = %v", err)
		}
	})
	t.Run("registered Task owner is required", func(t *testing.T) {
		fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{})
		entities := fixture.state["entities"].(map[string]any)
		tasks := entities["tasks"].([]any)
		tasks[0].(map[string]any)["owner_agent_ids"] = []any{"another-agent"}
		if _, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, fixture.manifest, fixture.manifestRef, fixture.taskRef, fixture.taskID, fixture.task); err == nil || !strings.Contains(err.Error(), "does not bind manifest owner") {
			t.Fatalf("owner binding error = %v", err)
		}
	})
}

func TestBuildRegisteredAssignmentRecordsRejectsProtectedScope(t *testing.T) {
	for _, path := range []string{"docs/dev/tasks", ".claude/agents"} {
		t.Run(path, func(t *testing.T) {
			fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{writePath: path})
			if _, err := buildRegisteredAssignmentRecords(fixture.root, fixture.state, fixture.manifest, fixture.manifestRef, fixture.taskRef, fixture.taskID, fixture.task); err == nil || !strings.Contains(err.Error(), "overlaps protected original path") {
				t.Fatalf("protected source scope error = %v", err)
			}
		})
	}
}

type recordRegistrationFixtureOptions struct {
	badProfileSHA bool
	writePath     string
}

type recordRegistrationFixture struct {
	root        string
	state       map[string]any
	manifest    []byte
	manifestRef string
	task        []byte
	taskRef     string
	taskID      string
}

func newRecordRegistrationFixture(t *testing.T, options recordRegistrationFixtureOptions) recordRegistrationFixture {
	t.Helper()
	root := t.TempDir()
	sourceRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	copyRegistrationTree(t, filepath.Join(sourceRoot, "docs", "control"), filepath.Join(root, "docs", "control"))
	copyRegistrationFile(t, filepath.Join(sourceRoot, "agents", "backend-builder.md"), filepath.Join(root, ".claude", "agents", "backend-builder.md"))
	for _, spec := range catalog.Skills {
		copyRegistrationFile(t, filepath.Join(sourceRoot, "skills", spec.Name, "SKILL.md"), filepath.Join(root, ".claude", "skills", spec.Name, "SKILL.md"))
	}

	const taskID = "TASK-001-01"
	const reqID = "REQ-001"
	const taskRef = "docs/dev/tasks/TASK-001-01.md"
	const indexRef = "docs/dev/tasks/index-REQ-001.md"
	manifestRef := ".claude/workgroups/manifest-s6-registration.json"
	writePath := options.writePath
	if writePath == "" {
		writePath = "src/pkg"
	}
	task := []byte(fmt.Sprintf(`# %s: Current reviewed task

> Status: complete
> Version: v1.0.0
> Source REQ refs: %s
> Dispatch policy: waves-v1
> Primary contract: BE-001

## Scope

| Type | Paths |
| --- | --- |
| prospective write paths | %s |
`, taskID, reqID, "`"+writePath+"`"))
	index := []byte(fmt.Sprintf(`# %s dispatch plan

> REQ: %s
> Dispatch policy: waves-v1
> Revision: 1
> Status: complete

## W1 · one reviewed assignment

- [ ] [current task](TASK-001-01.md)
`, reqID, reqID))
	writeRegistrationFile(t, root, taskRef, task)
	writeRegistrationFile(t, root, indexRef, index)
	writeRegistrationFile(t, root, ".claude/loop-state.json", []byte(`{"bound_req":{"id":"REQ-001"}}`))
	writeRegistrationFile(t, root, "src/pkg/input.go", []byte("package pkg\n"))
	writeRegistrationFile(t, root, "testdata/input.txt", []byte("fixture\n"))

	dependencySHA := registrationSHA([]byte("package pkg\n"))
	if options.badProfileSHA {
		dependencySHA = strings.Repeat("0", 64)
	}
	manifestValue := map[string]any{
		"schema_version": "1.0.0", "assignment_record_contract": AssignmentRecordContract,
		"manifest_id": "team-manifest-s6-registration", "version": "v1.0.0",
		"runtime_id": "loop-s6-registration", "req_id": reqID, "baseline_generation": 1,
		"review_round": 1, "platform_team_id": "platform-s6", "workgroup_id": "workgroup-s6-registration",
		"workgroup_kind": "builder", "status": "planned",
		"documents": []any{map[string]any{"id": taskID, "path": taskRef, "version": "v1.0.0", "sha256": registrationSHA(task)}},
		"risk_tags": []any{},
		"responsibility_dispositions": []any{map[string]any{
			"responsibility_id": "BUILD-WORK-PACKAGE", "disposition": "assigned", "trigger": "one current TASK",
			"assignment_ids": []string{"assignment-s6-registration"}, "na_rationale": nil, "evidence_ref": nil,
		}},
		"assignments": []any{map[string]any{
			"assignment_id": "assignment-s6-registration", "responsibility_id": "BUILD-WORK-PACKAGE",
			"role_family": "backend-builder", "agent_id": "agent-s6-registration",
			"agent_definition_ref": ".claude/agents/backend-builder.md", "skill_refs": []string{"backend-engineering"},
			"read_paths": []string{}, "scope": []string{writePath}, "write_paths": []string{writePath},
			"output_paths": []string{}, "depends_on": []string{}, "required_checks": []string{"go test ./pkg"},
			"done_when": []string{"owned package checks pass"}, "dispatch_mode": "plan_checkpoint",
			"check_profiles": []any{map[string]any{
				"id": "profile-s6-registration", "version": "v1.0.0", "check_index": 0, "command": "go test ./pkg",
				"location": "worker", "timeout_seconds": 30, "environment_policy": "declared_only", "environment": []any{},
				"dependencies":  []any{map[string]any{"path": "src/pkg/input.go", "sha256": dependencySHA}},
				"fixtures":      []any{map[string]any{"path": "testdata/input.txt", "sha256": registrationSHA([]byte("fixture\n"))}},
				"input_closure": "whole_tree", "side_effects": "none", "reuse_policy": "never",
			}},
			"reuse_decision": "create", "grouping_rationale": "one current reviewed TASK", "status": "planned",
		}},
		"separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1,
		"quantity_rationale": "one owner for one reviewed TASK",
		"validation":         map[string]any{"result": "pass", "missing_responsibilities": []any{}, "unresolved_conflicts": []any{}, "warnings": []any{}, "validated_at": "2026-10-14T00:00:00Z"},
	}
	manifest, err := json.Marshal(manifestValue)
	if err != nil {
		t.Fatal(err)
	}
	writeRegistrationFile(t, root, manifestRef, manifest)
	gitRegistration(t, root, "init", "-b", "main")
	gitRegistration(t, root, "config", "user.name", "S6 test")
	gitRegistration(t, root, "config", "user.email", "s6-test@example.invalid")
	gitRegistration(t, root, "add", ".")
	gitRegistration(t, root, "commit", "-m", "fixture baseline")

	taskSHA := registrationSHA(task)
	state := map[string]any{
		"schema_version": "1.2.0", "runtime_id": "loop-s6-registration", "revision": float64(8),
		"lifecycle": map[string]any{"state": "building"}, "baseline": map[string]any{"generation": float64(1)},
		"bound_req": map[string]any{
			"id": reqID, "status": "locked",
			"workspace": map[string]any{"project_root": root, "dev_branch": "main", "release_upstream": "main", "bound_commit": "baseline"},
		},
		"documents": []any{
			map[string]any{"id": taskID, "kind": "task", "path": taskRef, "version": "v1.0.0", "sha256": taskSHA, "status": "locked", "generation": float64(1)},
			map[string]any{"id": "dispatch-plan:REQ-001", "kind": "dispatch_plan", "path": indexRef, "version": "1", "sha256": registrationSHA(index), "status": "locked", "generation": float64(1)},
		},
		"entities": map[string]any{
			"tasks": []any{map[string]any{"id": taskID, "path": taskRef, "sha256": taskSHA, "owner_agent_ids": []any{"agent-s6-registration"}}},
			"teams": []any{map[string]any{"id": "workgroup-s6-registration", "kind": "builder", "manifest_ref": manifestRef}},
			"agents": []any{map[string]any{
				"id": "agent-s6-registration", "team_id": "workgroup-s6-registration", "task_ids": []any{taskID},
				"role": "backend-builder", "definition_ref": ".claude/agents/backend-builder.md", "dispatch_mode": "plan_checkpoint",
			}},
			"assignments": []any{},
		},
	}
	return recordRegistrationFixture{root: root, state: state, manifest: manifest, manifestRef: manifestRef, task: task, taskRef: taskRef, taskID: taskID}
}

func copyRegistrationTree(t *testing.T, source, target string) {
	t.Helper()
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		output := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(output, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			return err
		}
		return os.WriteFile(output, data, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
}

func copyRegistrationFile(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRegistrationFile(t *testing.T, root, path string, data []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitRegistration(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func configureRegistrationClosing(t *testing.T, fixture *recordRegistrationFixture, contract, mode, mainProfileLocation string, includeWorkerProfile bool) {
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
	assignment := manifest["assignments"].([]any)[0].(map[string]any)
	if mode == "" {
		delete(assignment, "integration_check_mode")
	} else {
		assignment["integration_check_mode"] = mode
	}
	if profiles, ok := assignment["check_profiles"].([]any); ok && len(profiles) != 0 {
		profiles[0].(map[string]any)["location"] = mainProfileLocation
	}
	workerChecks := []any{}
	var stagedWorkerPath string
	if includeWorkerProfile {
		stagedWorkerPath = "src/pkg/worker_input.go"
		workerBytes := []byte("package pkg\n// frozen Worker check source\n")
		writeRegistrationFile(t, fixture.root, stagedWorkerPath, workerBytes)
		profile := map[string]any{
			"id": "worker-profile-s6-registration", "version": "v1.0.0", "check_index": 0, "command": "go test ./pkg",
			"location": "worker", "timeout_seconds": 30, "environment_policy": "declared_only", "environment": []any{},
			"dependencies": []any{map[string]any{"path": stagedWorkerPath, "sha256": registrationSHA(workerBytes)}},
			"fixtures":     []any{}, "input_closure": "whole_tree", "side_effects": "none", "reuse_policy": "never",
		}
		workerChecks = append(workerChecks, map[string]any{"index": 0, "command": "go test ./pkg", "profile": profile})
	}
	assignment["closing_contract"] = map[string]any{"contract": contract, "worker_checks": workerChecks}
	updated, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(manifestPath, updated, 0o644); err != nil {
		t.Fatal(err)
	}
	gitArgs := []string{"add", fixture.manifestRef}
	if stagedWorkerPath != "" {
		gitArgs = append(gitArgs, stagedWorkerPath)
	}
	gitRegistration(t, fixture.root, gitArgs...)
	gitRegistration(t, fixture.root, "commit", "-m", "freeze explicit S6 closing contract")
	fixture.manifest = updated
}

func registrationSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
