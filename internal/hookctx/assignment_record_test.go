package hookctx

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
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestAssignmentRecordResolverProjectsCanonicalScopeAndBinding(t *testing.T) {
	f := newHookAssignmentRecordFixture(t)
	resolver := assignmentRecordResolver{root: f.root, state: f.state}
	legacy := &AssignmentContext{AssignmentID: f.record.AssignmentID, CompletionRef: "report.json"}
	projected, canonical, err := resolver.project(
		buildAgentRow{ID: f.owner, TaskID: f.taskID, PromptRef: f.manifestRef + "#" + f.record.AssignmentID},
		loadedTask{RuntimeID: f.runtimeID, State: "assigned", OwnerIDs: []string{f.owner}},
		registeredManifestRef{PromptRef: f.manifestRef + "#" + f.record.AssignmentID, TeamRef: f.manifestRef}, "", legacy,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !canonical || projected == nil {
		t.Fatal("S6 record was not projected")
	}
	digest, err := f.record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if projected.AssignmentRecord == nil || *projected.AssignmentRecord != f.record.Reference() || projected.AssignmentRecordSHA256 != digest || projected.AssignmentRevision != f.record.Revision {
		t.Fatalf("record identity/digest/revision was not projected: %+v", projected)
	}
	if len(projected.WritePaths) != 1 || projected.WritePaths[0] != "internal/pkg" {
		t.Fatalf("write paths are not the canonical intersection: %v", projected.WritePaths)
	}
	if len(projected.RequiredChecks) != 1 || projected.RequiredChecks[0] != "go test ./internal/pkg" {
		t.Fatalf("required checks differ from the Record: %v", projected.RequiredChecks)
	}
	if projected.CompletionRef != "report.json" {
		t.Fatal("read-only completion projection was lost")
	}
	if projected.WorktreePath != ".worktrees/planned-e1" || projected.Branch != "codex/planned-e1" || projected.TargetBranch != "development" {
		t.Fatalf("unbound canonical Assignment changed legacy manifest coordinates: path=%q branch=%q target=%q", projected.WorktreePath, projected.Branch, projected.TargetBranch)
	}

	if _, _, err := resolver.projectRecord(buildAgentRow{ID: f.owner, TaskID: f.taskID}, loadedTask{}, registeredManifestRef{}, f.record, projected); err != nil {
		t.Fatalf("source projection should remain stable when its output completion report changes: %v", err)
	}
}

func TestAssignmentRecordResolverProjectsCurrentExecutionCoordinates(t *testing.T) {
	for _, status := range []string{"ready", "complete"} {
		t.Run(status, func(t *testing.T) {
			f := newHookAssignmentRecordFixture(t)
			execution := hookRecordExecution(&f, status)
			setHookAssignmentExecution(t, &f, &execution)
			projected := projectHookAssignment(t, f)
			if projected.WorktreePath != filepath.Join(f.root, ".worktrees/current-e3") || projected.Branch != "codex/current-e3" || projected.TargetBranch != "development" {
				t.Fatalf("canonical Assignment retained manifest-planned coordinates: path=%q branch=%q target=%q", projected.WorktreePath, projected.Branch, projected.TargetBranch)
			}
		})
	}
}

func TestAssignmentRecordResolverDoesNotFallbackToPlannedCoordinatesForBoundWorkspace(t *testing.T) {
	t.Run("missing execution", func(t *testing.T) {
		f := newHookAssignmentRecordFixture(t)
		setHookAssignmentExecution(t, &f, nil)
		projected := projectHookAssignment(t, f)
		if projected.WorktreePath != "" || projected.Branch != "" || projected.TargetBranch != "" {
			t.Fatalf("bound workspace without a current execution reused planned coordinates: %+v", projected)
		}
	})
	t.Run("retired execution", func(t *testing.T) {
		f := newHookAssignmentRecordFixture(t)
		execution := hookRecordExecution(&f, "retired")
		setHookAssignmentExecution(t, &f, &execution)
		projected := projectHookAssignment(t, f)
		if projected.WorktreePath != "" || projected.Branch != "" || projected.TargetBranch != "" {
			t.Fatalf("non-current execution reused planned coordinates: %+v", projected)
		}
	})
	t.Run("wrong owner", func(t *testing.T) {
		f := newHookAssignmentRecordFixture(t)
		execution := hookRecordExecution(&f, "ready")
		execution.AgentID = "another-owner"
		setHookAssignmentExecution(t, &f, &execution)
		if _, err := projectHookAssignmentResult(f); err == nil {
			t.Fatal("workspace execution with a different owner was projected")
		}
	})
	t.Run("wrong record revision", func(t *testing.T) {
		f := newHookAssignmentRecordFixture(t)
		execution := hookRecordExecution(&f, "ready")
		ref := f.record.Reference()
		ref.Revision++
		execution.AssignmentRecord = &ref
		setHookAssignmentExecution(t, &f, &execution)
		if _, err := projectHookAssignmentResult(f); err == nil {
			t.Fatal("workspace execution with a different Assignment revision was projected")
		}
	})
	t.Run("wrong record digest", func(t *testing.T) {
		f := newHookAssignmentRecordFixture(t)
		execution := hookRecordExecution(&f, "ready")
		execution.AssignmentRecordSHA256 = strings.Repeat("0", 64)
		setHookAssignmentExecution(t, &f, &execution)
		if _, err := projectHookAssignmentResult(f); err == nil {
			t.Fatal("workspace execution with a different Assignment digest was projected")
		}
	})
	t.Run("stale runtime tuple", func(t *testing.T) {
		f := newHookAssignmentRecordFixture(t)
		execution := hookRecordExecution(&f, "ready")
		execution.RuntimeID = "loop-historical"
		setHookAssignmentExecution(t, &f, &execution)
		if _, err := projectHookAssignmentResult(f); err == nil {
			t.Fatal("workspace execution from a different Runtime was treated as current")
		}
	})
}

func projectHookAssignment(t *testing.T, f hookAssignmentRecordFixture) *AssignmentContext {
	t.Helper()
	projected, err := projectHookAssignmentResult(f)
	if err != nil {
		t.Fatal(err)
	}
	return projected
}

func projectHookAssignmentResult(f hookAssignmentRecordFixture) (*AssignmentContext, error) {
	resolver := assignmentRecordResolver{root: f.root, state: f.state}
	projected, canonical, err := resolver.project(
		buildAgentRow{ID: f.owner, TaskID: f.taskID, PromptRef: f.manifestRef + "#" + f.record.AssignmentID},
		loadedTask{RuntimeID: f.runtimeID, State: "assigned", OwnerIDs: []string{f.owner}},
		registeredManifestRef{PromptRef: f.manifestRef + "#" + f.record.AssignmentID, TeamRef: f.manifestRef}, "", nil,
	)
	if err != nil {
		return nil, err
	}
	if !canonical || projected == nil {
		return nil, fmt.Errorf("S6 record was not projected")
	}
	return projected, nil
}

func hookRecordExecution(f *hookAssignmentRecordFixture, status string) workspace.Execution {
	ref := f.record.Reference()
	digest, _ := f.record.Digest()
	inputs := map[string]string{}
	for _, source := range f.record.ContextSources {
		if strings.HasPrefix(source.Kind, "embedded_") || source.Kind == "check_profile" {
			continue
		}
		inputs[source.Ref] = source.SHA256
	}
	return workspace.Execution{
		AssignmentRecord:       &ref,
		AssignmentRecordSHA256: digest,
		AssignmentID:           f.record.AssignmentID,
		RuntimeID:              f.runtimeID,
		BaselineGeneration:     f.record.BaselineGeneration,
		Generation:             3,
		AgentID:                f.owner,
		Path:                   filepath.Join(f.root, ".worktrees/current-e3"),
		Branch:                 "codex/current-e3",
		TargetBranch:           "development",
		BaseCommit:             "fixture-base",
		Status:                 status,
		WritePaths:             append([]string{}, f.record.EffectiveWritePaths...),
		Checks:                 append([]string{}, f.record.RequiredChecks...),
		Inputs:                 inputs,
		CheckProfiles:          append([]checkexec.Profile{}, f.record.CheckProfiles...),
	}
}

func setHookAssignmentExecution(t *testing.T, f *hookAssignmentRecordFixture, execution *workspace.Execution) {
	t.Helper()
	registry := &workspace.ExecutionRegistry{
		Version:    1,
		MainRoot:   f.root,
		CommonDir:  filepath.Join(f.root, ".git"),
		Branch:     "development",
		BoundHead:  "fixture-head",
		Executions: map[string]workspace.Execution{},
	}
	if execution != nil {
		registry.Executions[f.record.AssignmentID] = *execution
	}
	stateWorkspace := f.state["bound_req"].(map[string]any)["workspace"].(map[string]any)
	stateWorkspace["release_upstream"] = "origin/release"
	f.state["workspace"] = workspace.Encode(registry)
}

func TestAssignmentRecordResolverFailsClosedOnMissingRecordAndSourceDrift(t *testing.T) {
	f := newHookAssignmentRecordFixture(t)
	entities := f.state["entities"].(map[string]any)
	registered := entities["assignments"]
	entities["assignments"] = []any{}
	resolver := assignmentRecordResolver{root: f.root, state: f.state}
	_, _, err := resolver.project(
		buildAgentRow{ID: f.owner, TaskID: f.taskID},
		loadedTask{RuntimeID: f.runtimeID, State: "assigned", OwnerIDs: []string{f.owner}},
		registeredManifestRef{PromptRef: f.manifestRef + "#" + f.record.AssignmentID, TeamRef: f.manifestRef}, "", nil,
	)
	if err == nil || !strings.Contains(err.Error(), "no canonical Assignment Record") {
		t.Fatalf("opted-in manifest without a Runtime Record was not rejected: %v", err)
	}
	entities["assignments"] = registered

	writeHookRecordFile(t, f.root, f.record.Task.Ref, []byte("uncommitted TASK drift\n"))
	resolver = assignmentRecordResolver{root: f.root, state: f.state}
	_, _, err = resolver.project(
		buildAgentRow{ID: f.owner, TaskID: f.taskID},
		loadedTask{RuntimeID: f.runtimeID, State: "assigned", OwnerIDs: []string{f.owner}},
		registeredManifestRef{PromptRef: f.manifestRef + "#" + f.record.AssignmentID, TeamRef: f.manifestRef}, "", nil,
	)
	if err == nil || !strings.Contains(err.Error(), "checkout bytes differ") {
		t.Fatalf("uncommitted physical checkout drift was not rejected: %v", err)
	}
	writeHookRecordFile(t, f.root, f.record.Task.Ref, f.taskBytes)

	writeHookRecordFile(t, f.root, ".claude/agents/backend-builder.md", []byte("changed original role source\n"))
	resolver = assignmentRecordResolver{root: f.root, state: f.state}
	_, _, err = resolver.project(
		buildAgentRow{ID: f.owner, TaskID: f.taskID},
		loadedTask{RuntimeID: f.runtimeID, State: "assigned", OwnerIDs: []string{f.owner}},
		registeredManifestRef{PromptRef: f.manifestRef + "#" + f.record.AssignmentID, TeamRef: f.manifestRef}, "", nil,
	)
	if err == nil || !strings.Contains(err.Error(), "changed after registration") {
		t.Fatalf("drifted installed Assignment source was not rejected: %v", err)
	}
}

type hookAssignmentRecordFixture struct {
	root        string
	state       map[string]any
	record      assignmentrecord.Record
	taskBytes   []byte
	runtimeID   string
	owner       string
	taskID      string
	manifestRef string
}

func newHookAssignmentRecordFixture(t *testing.T) hookAssignmentRecordFixture {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "development"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}} {
		runHookRecordGit(t, root, args...)
	}
	const (
		runtimeID    = "loop-hook-assignment-test"
		generation   = 3
		teamID       = "workgroup-hook-assignment-test"
		owner        = "agent-hook-assignment-test"
		assignmentID = "assignment-hook-assignment-test"
		taskID       = "TASK-042-01"
		manifestRef  = ".claude/workgroups/s6/manifest.json"
		taskRef      = "docs/dev/tasks/TASK-042-01.md"
		roleRef      = ".claude/agents/backend-builder.md"
		skillRef     = ".claude/skills/backend-engineering/SKILL.md"
	)
	taskBytes := []byte("# TASK-042-01\n\n> Version: v1.0.0\n\nImplement the registered feature.\n")
	roleBytes := []byte("original backend role source\n")
	skillBytes := []byte("original authorized skill source\n")
	manifest := map[string]any{
		"schema_version": "1.0.0", "assignment_record_contract": assignmentrecord.AssignmentRecordContract,
		"manifest_id": "team-manifest-hook-assignment-test", "version": "v1.2.0", "runtime_id": runtimeID,
		"req_id": "REQ-042", "baseline_generation": generation, "workgroup_id": teamID, "workgroup_kind": "builder",
		"documents": []any{map[string]any{"id": taskID, "path": taskRef, "version": "v1.0.0", "sha256": hookRecordSHA(taskBytes)}},
		"assignments": []any{map[string]any{
			"assignment_id": assignmentID, "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": "backend-builder",
			"agent_id": owner, "agent_definition_ref": roleRef, "skill_refs": []string{"backend-engineering"},
			"read_paths": []string{"docs/dev/tasks/"}, "scope": []string{"internal/"}, "write_paths": []string{"internal/pkg/"},
			"output_paths": []string{}, "depends_on": []string{}, "required_checks": []string{"go test ./internal/pkg"},
			"done_when": []string{"registered feature behavior is implemented and verified"}, "dispatch_mode": "plan_checkpoint",
			"worktree_path": ".worktrees/planned-e1", "branch": "codex/planned-e1", "target_branch": "development",
		}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	definition := map[string]any{"file_sources": []any{
		map[string]any{"path": ".", "source": "git_tree"},
		map[string]any{"path": ".claude/workgroups", "source": "disk"},
		map[string]any{"path": ".claude/agents", "source": "disk"},
		map[string]any{"path": ".claude/skills", "source": "disk"},
	}}
	definitionBytes, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	writeHookRecordFile(t, root, "docs/control/loop-definition.json", definitionBytes)
	for path, data := range map[string][]byte{manifestRef: manifestBytes, taskRef: taskBytes, roleRef: roleBytes, skillRef: skillBytes} {
		writeHookRecordFile(t, root, path, data)
	}
	runHookRecordGit(t, root, "add", "docs/control/loop-definition.json", taskRef)
	runHookRecordGit(t, root, "commit", "-m", "original S6 assignment sources")
	definitionSHA := hookRecordSHA(definitionBytes)

	state := map[string]any{
		"runtime_id": runtimeID, "revision": 8, "schema_version": "1.2.0",
		"definition": map[string]any{"path": "docs/control/loop-definition.json", "sha256": definitionSHA},
		"bound_req":  map[string]any{"workspace": map[string]any{"project_root": root, "dev_branch": "development", "bound_commit": "test"}},
		"lifecycle":  map[string]any{"state": "building"},
		"baseline":   map[string]any{"generation": generation},
		"documents":  []any{map[string]any{"id": taskID, "kind": "task", "path": taskRef, "version": "v1.0.0", "sha256": hookRecordSHA(taskBytes), "status": "locked", "generation": generation}},
		"entities": map[string]any{
			"assignments": []any{},
			"tasks":       []any{map[string]any{"id": taskID, "path": taskRef, "sha256": hookRecordSHA(taskBytes), "owner_agent_ids": []string{owner}}},
			"teams":       []any{map[string]any{"id": teamID, "kind": "builder", "manifest_ref": manifestRef}},
			"agents":      []any{map[string]any{"id": owner, "team_id": teamID, "task_ids": []string{taskID}, "role": "backend-builder", "definition_ref": roleRef, "dispatch_mode": "plan_checkpoint"}},
		},
	}
	contextSources := []assignmentrecord.ContextSource{
		{Kind: "agent_definition", Ref: roleRef, ID: "backend-builder", SHA256: hookRecordSHA(roleBytes)},
		{Kind: "authorized_skill", Ref: skillRef, ID: "backend-engineering", Version: "1.0.0", SHA256: hookRecordSHA(skillBytes)},
	}
	for _, name := range []string{"team-manifest.schema.json", "check-profile.schema.json", "assignment-record.schema.json", "agent-message.schema.json"} {
		ref := assignmentinputs.SchemaPrefix + name
		data, err := assignmentinputs.Read(root, ref)
		if err != nil {
			t.Fatal(err)
		}
		contextSources = append(contextSources, assignmentrecord.ContextSource{Kind: "embedded_schema", Ref: ref, SHA256: hookRecordSHA(data)})
	}
	profileBytes, err := assignmentinputs.Read(root, assignmentinputs.ContextProfileRef)
	if err != nil {
		t.Fatal(err)
	}
	contextSources = append(contextSources, assignmentrecord.ContextSource{Kind: "embedded_context_profile", Ref: assignmentinputs.ContextProfileRef, SHA256: hookRecordSHA(profileBytes)})
	record, err := assignmentrecord.BuildRecord(manifestBytes, manifestRef, state, taskBytes, taskID, assignmentID, []string{"internal"}, contextSources)
	if err != nil {
		t.Fatal(err)
	}
	state["revision"] = record.RegistrationRevision
	entities := state["entities"].(map[string]any)
	entities["assignments"] = []any{record}
	return hookAssignmentRecordFixture{root: root, state: state, record: record, taskBytes: taskBytes, runtimeID: runtimeID, owner: owner, taskID: taskID, manifestRef: manifestRef}
}

func writeHookRecordFile(t *testing.T, root, ref string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func runHookRecordGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func hookRecordSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
