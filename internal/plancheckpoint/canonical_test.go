package plancheckpoint_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	"github.com/entroforge/go-system-builder/internal/runtimemeta"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/team"
)

func TestValidateCanonicalThinPlanBindsRecordAndFrozenSources(t *testing.T) {
	fixture := newCanonicalFixture(t)
	if err := fixture.validate(t, fixture.message); err != nil {
		t.Fatalf("valid thin S6 PLAN rejected: %v", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("assignment-record.schema.json", mustJSON(t, fixture.record)); err != nil {
		t.Fatalf("fixture Record does not satisfy its published schema: %v", err)
	}

	tests := []struct {
		name   string
		owner  string
		change func(*canonicalFixture, map[string]any)
	}{
		{
			name: "wrong revision",
			change: func(_ *canonicalFixture, message map[string]any) {
				message["assignment_revision"] = float64(2)
			},
		},
		{
			name: "wrong digest",
			change: func(_ *canonicalFixture, message map[string]any) {
				message["assignment_record_sha256"] = hashBytes([]byte("another Record"))
			},
		},
		{
			name:  "wrong owner",
			owner: "agent-s6-other",
			change: func(_ *canonicalFixture, message map[string]any) {
				message["agent_id"] = "agent-s6-other"
			},
		},
		{
			name: "wrong team",
			change: func(_ *canonicalFixture, message map[string]any) {
				message["team_id"] = "workgroup-other"
			},
		},
		{
			name: "wrong dependencies",
			change: func(_ *canonicalFixture, message map[string]any) {
				message["dependencies"] = []string{"assignment-unregistered-dependency"}
			},
		},
		{
			name: "out of scope planned path",
			change: func(_ *canonicalFixture, message map[string]any) {
				message["planned_paths"] = []string{"docs/outside/plan.md"}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCanonicalFixture(t)
			message := cloneMap(t, fixture.message)
			test.change(fixture, message)
			owner := test.owner
			if owner == "" {
				owner = "agent-s6-plancheck"
			}
			if err := fixture.validateAs(t, message, owner); err == nil {
				t.Fatal("mutated PLAN unexpectedly passed canonical validation")
			}
		})
	}

	t.Run("frozen TASK source drift", func(t *testing.T) {
		fixture := newCanonicalFixture(t)
		if err := os.WriteFile(filepath.Join(fixture.root, filepath.FromSlash(fixture.record.Task.Ref)), []byte("changed after registration"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := fixture.validate(t, fixture.message); err == nil {
			t.Fatal("changed frozen TASK source unexpectedly passed canonical validation")
		}
	})

	t.Run("record missing despite S6 plan marker", func(t *testing.T) {
		fixture := newCanonicalFixture(t)
		entities := fixture.snapshot.State["entities"].(map[string]any)
		delete(entities, "assignments")
		if err := fixture.validate(t, fixture.message); err == nil {
			t.Fatal("S6 plan marker without original Runtime Assignment registration was accepted")
		}
	})
}

func TestValidateCanonicalOptInDoesNotChangeLegacyPlanCheckpoint(t *testing.T) {
	fixture := newCanonicalFixture(t)
	entities := fixture.snapshot.State["entities"].(map[string]any)
	delete(entities, "assignments")

	manifest := map[string]any{}
	if err := json.Unmarshal(fixture.manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	delete(manifest, "assignment_record_contract")
	legacyManifest := mustJSON(t, manifest)
	if err := schema.NewEmbeddedValidator().ValidateBytes("team-manifest.schema.json", legacyManifest); err != nil {
		t.Fatalf("legacy fixture manifest invalid: %v", err)
	}
	if err := team.ValidateBytes(legacyManifest); err != nil {
		t.Fatalf("legacy fixture manifest semantics invalid: %v", err)
	}
	manifestPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.record.Manifest.Ref))
	if err := os.WriteFile(manifestPath, legacyManifest, 0o644); err != nil {
		t.Fatal(err)
	}

	legacyPlan := cloneMap(t, fixture.message)
	delete(legacyPlan, "assignment_record_contract")
	delete(legacyPlan, "assignment_record_sha256")
	delete(legacyPlan, "assertion_checks")
	legacyPlan["steps"] = []any{map[string]any{"description": "implement the scoped change"}}
	if err := fixture.validate(t, legacyPlan); err != nil {
		t.Fatalf("legacy plan checkpoint changed after adding the opt-in Record path: %v", err)
	}
}

func TestValidateRejectsOptedInManifestWithoutOriginalRecord(t *testing.T) {
	fixture := newCanonicalFixture(t)
	entities := fixture.snapshot.State["entities"].(map[string]any)
	delete(entities, "assignments")

	legacyShape := cloneMap(t, fixture.message)
	delete(legacyShape, "assignment_record_contract")
	delete(legacyShape, "assignment_record_sha256")
	delete(legacyShape, "assertion_checks")
	legacyShape["steps"] = []any{map[string]any{"description": "implement the scoped change"}}
	if err := fixture.validate(t, legacyShape); err == nil {
		t.Fatal("explicitly opted-in manifest without its original Runtime Record was accepted")
	}
}

func TestValidateCanonicalClosingPlanRequiresDoneWhenCoverage(t *testing.T) {
	fixture := newCanonicalFixtureWithClosing(t)
	if err := fixture.validate(t, fixture.message); err != nil {
		t.Fatalf("valid closing-contract PLAN rejected: %v", err)
	}
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "missing mapping", value: []any{map[string]any{"assertion": "implemented behavior", "oracle": "original proof matches"}}},
		{name: "out of range mapping", value: []any{map[string]any{"assertion": "implemented behavior", "oracle": "original proof matches", "done_when_indexes": []int{1}}}},
		{name: "duplicate mapping", value: []any{map[string]any{"assertion": "implemented behavior", "oracle": "original proof matches", "done_when_indexes": []int{0, 0}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneMap(t, fixture.message)
			candidate["assertion_checks"] = test.value
			if err := fixture.validate(t, candidate); err == nil {
				t.Fatal("invalid closing-contract PLAN coverage accepted")
			}
		})
	}
}

type canonicalFixture struct {
	root          string
	snapshot      runtimemeta.Snapshot
	manifestBytes []byte
	record        assignmentrecord.Record
	message       map[string]any
}

func newCanonicalFixture(t *testing.T) *canonicalFixture {
	return newCanonicalFixtureConfigured(t, false)
}

func newCanonicalFixtureWithClosing(t *testing.T) *canonicalFixture {
	return newCanonicalFixtureConfigured(t, true)
}

func newCanonicalFixtureConfigured(t *testing.T, withClosing bool) *canonicalFixture {
	t.Helper()
	const (
		runtimeID    = "loop-s6-plancheck"
		teamID       = "workgroup-s6-plancheck"
		agentID      = "agent-s6-plancheck"
		assignmentID = "assignment-s6-plancheck"
		taskID       = "TASK-042-01"
		manifestRef  = "docs/workgroups/s6/manifest.json"
		taskRef      = "docs/dev/tasks/TASK-042-01.md"
		roleRef      = ".claude/agents/backend-builder.md"
		skillRef     = ".claude/skills/backend-engineering/SKILL.md"
	)
	taskBytes := []byte("# TASK-042-01\n\n> Version: v1.0.0\n\nImplement the scoped change.\n")
	roleBytes := []byte("backend-builder role source used by this test\n")
	skillBytes := []byte("backend engineering skill source used by this test\n")
	taskSHA, roleSHA, skillSHA := hashBytes(taskBytes), hashBytes(roleBytes), hashBytes(skillBytes)
	manifestAssignment := map[string]any{
		"assignment_id": assignmentID, "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": "backend-builder",
		"agent_id": agentID, "agent_definition_ref": roleRef, "skill_refs": []string{"backend-engineering"},
		"read_paths": []string{"docs/dev/tasks/"}, "scope": []string{"internal/"}, "write_paths": []string{"internal/pkg/"},
		"output_paths": []string{}, "depends_on": []string{}, "required_checks": []string{"go test ./internal/example"},
		"done_when": []string{"owned behavior is implemented and checked"}, "dispatch_mode": "plan_checkpoint",
		"reuse_decision": "create", "grouping_rationale": "One cohesive builder responsibility.", "status": "planned",
	}
	if withClosing {
		manifestAssignment["closing_contract"] = map[string]any{"contract": assignmentresult.S6ClosingContract, "worker_checks": []any{}}
		manifestAssignment["integration_check_mode"] = "post_merge"
	}
	manifest := map[string]any{
		"schema_version": "1.0.0", "assignment_record_contract": assignmentrecord.AssignmentRecordContract,
		"manifest_id": "team-manifest-s6-plancheck", "version": "v1.2.0", "runtime_id": runtimeID,
		"req_id": "REQ-042", "baseline_generation": 3, "review_round": nil,
		"platform_team_id": "platform-team-s6", "workgroup_id": teamID, "workgroup_kind": "builder", "status": "planned",
		"documents": []any{map[string]any{"id": taskID, "path": taskRef, "version": "v1.0.0", "sha256": taskSHA}},
		"risk_tags": []any{},
		"responsibility_dispositions": []any{map[string]any{
			"responsibility_id": "BUILD-WORK-PACKAGE", "disposition": "assigned", "trigger": "builder work is in scope",
			"assignment_ids": []string{assignmentID}, "na_rationale": nil, "evidence_ref": nil,
		}},
		"assignments":      []any{manifestAssignment},
		"separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1,
		"quantity_rationale": "One assignment owns this bounded builder result.",
		"validation": map[string]any{
			"result": "pass", "missing_responsibilities": []string{}, "unresolved_conflicts": []string{}, "warnings": []string{},
			"validated_at": "2026-10-14T00:00:00Z",
		},
	}
	manifestBytes := mustJSON(t, manifest)
	if err := schema.NewEmbeddedValidator().ValidateBytes("team-manifest.schema.json", manifestBytes); err != nil {
		t.Fatalf("canonical fixture manifest schema invalid: %v", err)
	}
	if err := team.ValidateBytes(manifestBytes); err != nil {
		t.Fatalf("canonical fixture manifest semantics invalid: %v", err)
	}

	state := map[string]any{
		"runtime_id": runtimeID, "revision": 8,
		"lifecycle": map[string]any{"state": "building"}, "baseline": map[string]any{"generation": 3},
		"documents": []any{map[string]any{
			"id": taskID, "kind": "task", "path": taskRef, "version": "v1.0.0", "sha256": taskSHA,
			"status": "locked", "generation": 3,
		}},
		"entities": map[string]any{
			"assignments": []any{},
			"tasks":       []any{map[string]any{"id": taskID, "path": taskRef, "sha256": taskSHA, "owner_agent_ids": []string{agentID}}},
			"teams":       []any{map[string]any{"id": teamID, "kind": "builder", "manifest_ref": manifestRef}},
			"agents": []any{map[string]any{
				"id": agentID, "team_id": teamID, "task_ids": []string{taskID}, "role": "backend-builder",
				"definition_ref": roleRef, "dispatch_mode": "plan_checkpoint",
			}},
		},
	}
	record, err := assignmentrecord.BuildRecord(
		manifestBytes, manifestRef, state, taskBytes, taskID, assignmentID, []string{"internal/"},
		[]assignmentrecord.ContextSource{
			{Kind: "agent_definition", Ref: roleRef, ID: "backend-builder", SHA256: roleSHA},
			{Kind: "authorized_skill", Ref: skillRef, ID: "backend-engineering", Version: "1.0.0", SHA256: skillSHA},
		},
	)
	if err != nil {
		t.Fatalf("BuildRecord fixture: %v", err)
	}
	state["revision"] = record.RegistrationRevision
	entities := state["entities"].(map[string]any)
	entities["assignments"] = []any{mustJSONMap(t, record)}

	root := t.TempDir()
	for ref, data := range map[string][]byte{
		manifestRef: manifestBytes,
		taskRef:     taskBytes,
		roleRef:     roleBytes,
		skillRef:    skillBytes,
	} {
		writeRootFile(t, root, ref, data)
	}
	digest, err := record.Digest()
	if err != nil {
		t.Fatalf("Record digest: %v", err)
	}
	assertionCheck := map[string]any{"assertion": "the assigned behavior is implemented", "oracle": "the required check passes"}
	if withClosing {
		assertionCheck["done_when_indexes"] = []int{0}
	}
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "plan_report", "message_id": "msg-s6-plancheck-01",
		"correlation_id": "corr-s6-plancheck-01", "runtime_id": runtimeID, "agent_id": agentID,
		"agent_definition_ref": roleRef, "task_id": taskID, "bug_id": nil, "team_id": teamID,
		"occurred_at": "2026-10-14T00:00:00Z", "assignment_id": assignmentID, "assignment_revision": record.Revision,
		"assignment_record_contract": assignmentrecord.AssignmentRecordContract, "assignment_record_sha256": digest,
		"objective":        "Implement the reviewed bounded builder responsibility.",
		"planned_paths":    []string{"internal/pkg/handler.go"},
		"assertion_checks": []any{assertionCheck},
		"dependencies":     []string{}, "risks_blockers": []string{},
	}
	return &canonicalFixture{root: root, snapshot: runtimemeta.Snapshot{Revision: record.RegistrationRevision, State: state}, manifestBytes: manifestBytes, record: record, message: message}
}

func (f *canonicalFixture) validate(t *testing.T, message map[string]any) error {
	return f.validateAs(t, message, "agent-s6-plancheck")
}

func (f *canonicalFixture) validateAs(t *testing.T, message map[string]any, agentID string) error {
	t.Helper()
	path := filepath.Join("plans", "plan-report.json")
	writeRootFile(t, f.root, path, mustJSON(t, message))
	return plancheckpoint.Validate(f.root, f.snapshot, agentID, path)
}

func writeRootFile(t *testing.T, root, ref string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func cloneMap(t *testing.T, source map[string]any) map[string]any {
	t.Helper()
	var copy map[string]any
	if err := json.Unmarshal(mustJSON(t, source), &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustJSONMap(t *testing.T, value any) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(mustJSON(t, value), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
