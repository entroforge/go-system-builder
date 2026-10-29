package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/hookctx"
)

func TestWorkspaceDispatchUsesCanonicalAssignmentRecordForPlanTemplate(t *testing.T) {
	state, record := cliRecordProjectionFixture(t)
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	reference := record.Reference()
	a := hookctx.AssignmentContext{
		AssignmentID: record.AssignmentID, AssignmentRecord: &reference, AssignmentRecordSHA256: digest, AssignmentRevision: record.Revision,
		BaselineGeneration: record.BaselineGeneration, TaskID: record.Task.ID, OwnerAgentID: record.OwnerAgentID,
		RoleFamily: record.RoleFamily, AgentDefinitionRef: record.AgentDefinition.Ref, ManifestRef: record.Manifest.Ref,
		ResponsibilityIDs: []string{record.ResponsibilityID}, SkillRefs: record.SkillRefs, ReadPaths: record.ReadPaths,
		OutputPaths: record.ScopeBasis.ManifestOutputPaths, DependsOn: record.DependsOn, DoneWhen: record.DoneWhen,
		WritePaths: record.EffectiveWritePaths, RequiredChecks: record.RequiredChecks,
	}
	dispatch, err := workspaceDispatch(state, a)
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.AssignmentRecord == nil || *dispatch.AssignmentRecord != reference || dispatch.AssignmentRecordSHA256 != digest || dispatch.AssignmentRevision != record.Revision ||
		dispatch.Owner != record.OwnerAgentID || dispatch.TaskID != record.Task.ID || dispatch.TeamID != record.TeamID || len(dispatch.EffectiveWritePaths) != 1 || dispatch.EffectiveWritePaths[0] != "internal/pkg" {
		t.Fatalf("workspace Dispatch did not apply the original Record: %+v", dispatch)
	}
	plan, questions := agentcontext.PlanTemplate(dispatch, record.RuntimeID, "2026-10-14T00:00:00Z", dispatch.EffectiveWritePaths)
	if plan == nil || len(questions) != 5 || plan["assignment_record_contract"] != assignmentrecord.AssignmentRecordContract || plan["assignment_record_sha256"] != digest || plan["assignment_revision"] != record.Revision {
		t.Fatalf("S6 professional PLAN template was not reached: plan=%v questions=%v", plan, questions)
	}
	if plan["objective"] != nil || len(plan["planned_paths"].([]string)) != 0 || plan["steps"] != nil || plan["assertion_checks"] == nil {
		t.Fatalf("PLAN template invented answers or retained legacy machine shape: %v", plan)
	}

	a.WritePaths = []string{"internal/"}
	if _, err := workspaceDispatch(state, a); err == nil {
		t.Fatal("workspace Dispatch accepted a noncanonical effective write scope")
	}
}

func TestWorkspaceDispatchKeepsLegacyProjectionShape(t *testing.T) {
	state := map[string]any{"entities": map[string]any{
		"agents": []any{map[string]any{"id": "legacy-owner", "team_id": "legacy-team", "dispatch_mode": "plan_checkpoint"}},
	}}
	dispatch, err := workspaceDispatch(state, hookctx.AssignmentContext{
		AssignmentID: "legacy-assignment", OwnerAgentID: "legacy-owner", TaskID: "TASK-LEGACY",
		ManifestRef: "docs/workgroups/legacy/manifest.json", RoleFamily: "backend-builder",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.AssignmentRecord != nil || dispatch.AssignmentRecordSHA256 != "" || dispatch.AssignmentRevision != 0 || len(dispatch.EffectiveWritePaths) != 0 || len(dispatch.RequiredChecks) != 0 {
		t.Fatalf("legacy Dispatch received canonical S6 fields: %+v", dispatch)
	}
	data, err := json.Marshal(dispatch)
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"assignment_record", "assignment_record_sha256", "assignment_revision", "effective_write_paths", "required_checks"} {
		if _, exists := encoded[key]; exists {
			t.Errorf("legacy Dispatch JSON unexpectedly includes %q: %s", key, data)
		}
	}
}

func cliRecordProjectionFixture(t *testing.T) (map[string]any, assignmentrecord.Record) {
	t.Helper()
	const (
		runtimeID    = "loop-cli-assignment-record"
		generation   = 2
		teamID       = "workgroup-cli-assignment-record"
		owner        = "agent-cli-assignment-record"
		assignmentID = "assignment-cli-assignment-record"
		taskID       = "TASK-042-01"
		manifestRef  = "docs/workgroups/REQ-042/manifest.json"
		taskRef      = "docs/dev/tasks/TASK-042-01.md"
		roleRef      = ".claude/agents/backend-builder.md"
		skillRef     = ".claude/skills/backend-engineering/SKILL.md"
	)
	task := []byte("official TASK\n")
	role := []byte("installed role\n")
	skill := []byte("installed skill\n")
	manifest := map[string]any{
		"schema_version": "1.0.0", "assignment_record_contract": assignmentrecord.AssignmentRecordContract,
		"manifest_id": "manifest-cli-assignment-record", "version": "v1.0.0", "runtime_id": runtimeID,
		"baseline_generation": generation, "workgroup_id": teamID, "workgroup_kind": "builder",
		"documents": []any{map[string]any{"id": taskID, "path": taskRef, "version": "v1.0.0", "sha256": cliRecordSHA(task)}},
		"assignments": []any{map[string]any{
			"assignment_id": assignmentID, "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": "backend-builder", "agent_id": owner,
			"agent_definition_ref": roleRef, "skill_refs": []string{"backend-engineering"}, "read_paths": []string{"docs/dev/tasks/"},
			"scope": []string{"internal/"}, "write_paths": []string{"internal/pkg/"}, "output_paths": []string{}, "depends_on": []string{},
			"required_checks": []string{"go test ./internal/pkg"}, "done_when": []string{"the registered feature and check pass"}, "dispatch_mode": "plan_checkpoint",
		}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"runtime_id": runtimeID, "revision": 5, "lifecycle": map[string]any{"state": "building"}, "baseline": map[string]any{"generation": generation},
		"documents": []any{map[string]any{"id": taskID, "kind": "task", "path": taskRef, "version": "v1.0.0", "sha256": cliRecordSHA(task), "status": "locked", "generation": generation}},
		"entities": map[string]any{
			"assignments": []any{},
			"tasks":       []any{map[string]any{"id": taskID, "path": taskRef, "sha256": cliRecordSHA(task), "owner_agent_ids": []string{owner}}},
			"teams":       []any{map[string]any{"id": teamID, "kind": "builder", "manifest_ref": manifestRef}},
			"agents":      []any{map[string]any{"id": owner, "team_id": teamID, "task_ids": []string{taskID}, "role": "backend-builder", "definition_ref": roleRef, "dispatch_mode": "plan_checkpoint"}},
		},
	}
	record, err := assignmentrecord.BuildRecord(manifestBytes, manifestRef, state, task, taskID, assignmentID, []string{"internal"}, []assignmentrecord.ContextSource{
		{Kind: "agent_definition", Ref: roleRef, ID: "backend-builder", SHA256: cliRecordSHA(role)},
		{Kind: "authorized_skill", Ref: skillRef, ID: "backend-engineering", Version: "1.0.0", SHA256: cliRecordSHA(skill)},
	})
	if err != nil {
		t.Fatal(err)
	}
	state["revision"] = record.RegistrationRevision
	entities := state["entities"].(map[string]any)
	entities["assignments"] = []any{record}
	return state, record
}

func cliRecordSHA(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
