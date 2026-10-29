package assignmentrecord

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestManifestAssignmentRecordOptInIsExplicit(t *testing.T) {
	for _, test := range []struct {
		name     string
		manifest string
		want     bool
		wantErr  bool
	}{
		{name: "legacy despite runtime style schema version", manifest: `{"schema_version":"1.2.0"}`},
		{name: "legacy despite manifest schema version", manifest: `{"schema_version":"1.0.0"}`},
		{name: "explicit v1.2", manifest: `{"schema_version":"1.0.0","assignment_record_contract":"s6-v1.2"}`, want: true},
		{name: "unknown contract rejected", manifest: `{"schema_version":"1.0.0","assignment_record_contract":"s6-v1.3"}`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ManifestAssignmentRecordEnabled([]byte(test.manifest))
			if (err != nil) != test.wantErr {
				t.Fatalf("ManifestAssignmentRecordEnabled() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("ManifestAssignmentRecordEnabled() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestEffectiveScopeIntersectsOriginalAndReviewedTaskPaths(t *testing.T) {
	got, err := EffectiveScope(ScopeBasis{
		ManifestScope:       []string{"docs/reports/", "internal/"},
		ManifestWritePaths:  []string{"internal/pkg/", "internal/other/"},
		ManifestOutputPaths: []string{"docs/reports/result.json"},
		TaskWritePaths:      []string{"docs/", "internal/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"docs/reports/result.json", "internal/other", "internal/pkg"}
	if !sameStrings(got, want) {
		t.Fatalf("EffectiveScope() = %v, want %v", got, want)
	}
	if _, err := EffectiveScope(ScopeBasis{
		ManifestScope:      []string{"internal/"},
		ManifestWritePaths: []string{"docs/"},
		TaskWritePaths:     []string{"internal/"},
	}); err == nil {
		t.Fatal("disjoint source scopes must not produce an Assignment scope")
	}
	if _, err := EffectiveScope(ScopeBasis{
		ManifestScope:      []string{"../outside"},
		ManifestWritePaths: []string{"internal/"},
		TaskWritePaths:     []string{"internal/"},
	}); err == nil {
		t.Fatal("escaping source scope must be rejected")
	}
	if _, err := EffectiveScope(ScopeBasis{
		ManifestScope:       []string{"internal/", "internal"},
		ManifestWritePaths:  []string{"internal/pkg/"},
		ManifestOutputPaths: []string{"internal/pkg/"},
		TaskWritePaths:      []string{"internal"},
	}); err == nil {
		t.Fatal("duplicate source scopes after normalization must be rejected")
	}
	got, err = EffectiveScope(ScopeBasis{
		ManifestScope:       []string{"internal/"},
		ManifestWritePaths:  []string{"internal/pkg/"},
		ManifestOutputPaths: []string{"internal/pkg"},
		TaskWritePaths:      []string{"internal"},
	})
	if err != nil || !sameStrings(got, []string{"internal/pkg"}) {
		t.Fatalf("write/output overlap across the two declared categories = %v, %v", got, err)
	}
}

func TestBuildRecordUsesCurrentRuntimeTaskAndOwnerSources(t *testing.T) {
	taskBytes := []byte("# TASK-042-01\n\n> Version: v1.0.0\n")
	taskSHA := recordTestSHA(taskBytes)
	manifest := map[string]any{
		"schema_version":             "1.0.0",
		"assignment_record_contract": AssignmentRecordContract,
		"manifest_id":                "team-manifest-s6",
		"version":                    "v1.2.0",
		"runtime_id":                 "loop-s6-record",
		"baseline_generation":        3,
		"workgroup_id":               "workgroup-s6-record",
		"workgroup_kind":             "builder",
		"documents": []any{map[string]any{
			"id": "TASK-042-01", "path": "docs/dev/tasks/TASK-042-01.md",
			"version": "v1.0.0", "sha256": taskSHA,
		}},
		"assignments": []any{map[string]any{
			"assignment_id": "assignment-s6-record", "responsibility_id": "BUILD-WORK-PACKAGE",
			"role_family": "backend-builder", "agent_id": "agent-s6-record",
			"agent_definition_ref": ".claude/agents/backend-builder.md",
			"skill_refs":           []string{"backend-engineering"}, "read_paths": []string{"docs/dev/tasks/"},
			"scope": []string{"internal/"}, "write_paths": []string{"internal/pkg/"},
			"output_paths": []string{}, "depends_on": []string{},
			"required_checks": []string{"go test ./..."}, "done_when": []string{"owned checks pass"},
		}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	runtimeState := map[string]any{
		"runtime_id": "loop-s6-record",
		"revision":   float64(8),
		"lifecycle":  map[string]any{"state": "building"},
		"baseline":   map[string]any{"generation": float64(3)},
		"documents": []any{map[string]any{
			"id": "TASK-042-01", "kind": "task", "path": "docs/dev/tasks/TASK-042-01.md",
			"version": "v1.0.0", "sha256": taskSHA, "status": "locked", "generation": float64(3),
		}},
		"entities": map[string]any{
			"tasks": []any{map[string]any{
				"id": "TASK-042-01", "path": "docs/dev/tasks/TASK-042-01.md", "sha256": taskSHA,
				"owner_agent_ids": []string{"agent-s6-record"},
			}},
			"teams": []any{map[string]any{
				"id": "workgroup-s6-record", "kind": "builder", "manifest_ref": "docs/workgroups/s6/manifest.json",
			}},
			"agents": []any{map[string]any{
				"id": "agent-s6-record", "team_id": "workgroup-s6-record", "task_ids": []string{"TASK-042-01"},
				"role": "backend-builder", "definition_ref": ".claude/agents/backend-builder.md", "dispatch_mode": "plan_checkpoint",
			}},
		},
	}
	roleSHA := strings.Repeat("a", 64)
	skillSHA := strings.Repeat("b", 64)
	record, err := BuildRecord(
		manifestBytes,
		"docs/workgroups/s6/manifest.json",
		runtimeState,
		taskBytes,
		"TASK-042-01",
		"assignment-s6-record",
		[]string{"internal"},
		[]ContextSource{
			{Kind: "agent_definition", Ref: ".claude/agents/backend-builder.md", ID: "backend-builder", SHA256: roleSHA},
			{Kind: "authorized_skill", Ref: ".claude/skills/backend-engineering/SKILL.md", ID: "backend-engineering", Version: "1.0.0", SHA256: skillSHA},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if record.Revision != 1 || record.RegistrationRevision != 9 {
		t.Fatalf("Record revisions = assignment %d / Runtime %d, want 1 / 9", record.Revision, record.RegistrationRevision)
	}
	if record.Task.ID != "TASK-042-01" || record.Task.Version != "v1.0.0" || record.Task.SHA256 != taskSHA {
		t.Fatalf("Record TASK source = %+v", record.Task)
	}
	if record.OwnerAgentID != "agent-s6-record" || !sameStrings(record.EffectiveWritePaths, []string{"internal/pkg"}) {
		t.Fatalf("Record owner/scope = %s / %v", record.OwnerAgentID, record.EffectiveWritePaths)
	}
	if record.DispatchMode != "plan_checkpoint" {
		t.Fatalf("omitted manifest dispatch mode defaulted to %q", record.DispatchMode)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"objective"`) || strings.Contains(string(encoded), `"summary"`) || strings.Contains(string(encoded), `"assertion_evidence"`) {
		t.Fatalf("canonical Record contains a professional PLAN/Result field: %s", encoded)
	}
	if strings.Contains(string(encoded), `"closing_contract"`) || strings.Contains(string(encoded), `"integration_check_mode"`) {
		t.Fatalf("legacy-shape Record projected an absent closing contract or integration mode: %s", encoded)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("assignment-record.schema.json", encoded); err != nil {
		t.Fatalf("standalone Assignment Record schema rejected BuildRecord output: %v", err)
	}
}

func TestBuildRecordFreezesExplicitEmptyClosingContractWithoutChangingMainChecks(t *testing.T) {
	taskBytes := []byte("official task bytes")
	manifestBytes, runtimeState, sources := recordTestSources(t, taskBytes)
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	assignment := manifest["assignments"].([]any)[0].(map[string]any)
	assignment["integration_check_mode"] = "post_merge"
	assignment["closing_contract"] = map[string]any{
		"contract":      ClosingContractVersion,
		"worker_checks": []any{},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	record, err := BuildRecord(manifestBytes, "docs/workgroups/s6/manifest.json", runtimeState, taskBytes, "TASK-042-01", "assignment-s6-record", []string{"internal"}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if record.ClosingContract == nil || record.ClosingContract.Contract != ClosingContractVersion || record.ClosingContract.WorkerChecks == nil || len(record.ClosingContract.WorkerChecks) != 0 {
		t.Fatalf("explicit empty closing contract was not preserved: %+v", record.ClosingContract)
	}
	if record.IntegrationCheckMode != "post_merge" || len(record.RequiredChecks) != 0 || len(record.CheckProfiles) != 0 {
		t.Fatalf("closing contract changed Main integration/check denominator: mode=%q checks=%v profiles=%v", record.IntegrationCheckMode, record.RequiredChecks, record.CheckProfiles)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("assignment-record.schema.json", encoded); err != nil {
		t.Fatalf("standalone Assignment Record schema rejected explicit empty contract: %v", err)
	}
}

func TestBuildRecordRequiresPostMergeOnlyForExplicitClosingContract(t *testing.T) {
	taskBytes := []byte("official task bytes")
	manifestBytes, runtimeState, sources := recordTestSources(t, taskBytes)
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	assignment := manifest["assignments"].([]any)[0].(map[string]any)
	assignment["closing_contract"] = map[string]any{"contract": ClosingContractVersion, "worker_checks": []any{}}
	if _, err := BuildRecord(mustJSON(t, manifest), "docs/workgroups/s6/manifest.json", runtimeState, taskBytes, "TASK-042-01", "assignment-s6-record", []string{"internal"}, sources); err == nil || !strings.Contains(err.Error(), "explicit post_merge") {
		t.Fatalf("closing contract without explicit post_merge mode error = %v", err)
	}

	delete(assignment, "closing_contract")
	assignment["integration_check_mode"] = "post_merge"
	if _, err := BuildRecord(mustJSON(t, manifest), "docs/workgroups/s6/manifest.json", runtimeState, taskBytes, "TASK-042-01", "assignment-s6-record", []string{"internal"}, sources); err != nil {
		t.Fatalf("manifest mode without new closing contract must keep the legacy Record shape: %v", err)
	}
}

func TestValidateClosingContractRequiresExplicitArrayAndWorkerProfileBinding(t *testing.T) {
	if err := ValidateClosingContract(nil); err != nil {
		t.Fatalf("missing contract is a valid legacy shape: %v", err)
	}
	if err := ValidateClosingContract(&ClosingContract{Contract: ClosingContractVersion}); err == nil || !strings.Contains(err.Error(), "explicit JSON array") {
		t.Fatalf("missing worker_checks error = %v", err)
	}
	if err := ValidateClosingContract(&ClosingContract{Contract: "s6-closing-v0", WorkerChecks: []WorkerCheck{}}); err == nil {
		t.Fatal("unsupported closing contract version must fail")
	}
	if err := ValidateClosingContract(&ClosingContract{Contract: ClosingContractVersion, WorkerChecks: []WorkerCheck{{Index: 1}}}); err == nil || !strings.Contains(err.Error(), "contiguous") {
		t.Fatalf("noncontiguous worker check index error = %v", err)
	}
	if err := validateMainCheckProfiles([]checkexec.Profile{{ID: "main-profile", Location: "worker"}}); err == nil {
		t.Fatal("new closing contracts must reject Main profiles at worker location")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBuildRecordRejectsStaleOrUnboundSources(t *testing.T) {
	taskBytes := []byte("official task bytes")
	manifestBytes, runtimeState, sources := recordTestSources(t, taskBytes)
	if _, err := BuildRecord(manifestBytes, "docs/workgroups/s6/manifest.json", runtimeState, []byte("changed task bytes"), "TASK-042-01", "assignment-s6-record", []string{"internal"}, sources); err == nil {
		t.Fatal("TASK bytes that differ from current Runtime registration must fail")
	}
	manifestBytes, runtimeState, sources = recordTestSources(t, taskBytes)
	deleteOwner(runtimeState)
	if _, err := BuildRecord(manifestBytes, "docs/workgroups/s6/manifest.json", runtimeState, taskBytes, "TASK-042-01", "assignment-s6-record", []string{"internal"}, sources); err == nil {
		t.Fatal("manifest owner absent from current TASK/Agent binding must fail")
	}
	manifestBytes, runtimeState, sources = recordTestSources(t, taskBytes)
	sources = sources[:1]
	if _, err := BuildRecord(manifestBytes, "docs/workgroups/s6/manifest.json", runtimeState, taskBytes, "TASK-042-01", "assignment-s6-record", []string{"internal"}, sources); err == nil {
		t.Fatal("missing actual role/Skill frozen source hashes must fail")
	}
}

func TestRecordFindAndDigestBindExactRevision(t *testing.T) {
	record := recordTestRecord()
	if err := ValidateRecord(record); err != nil {
		t.Fatal(err)
	}
	firstDigest, err := record.Digest()
	if err != nil || !recordSHA256RE.MatchString(firstDigest) {
		t.Fatalf("Digest() = %q, %v", firstDigest, err)
	}
	second := record
	second.Revision = 2
	second.RegistrationRevision++
	got, ok, err := FindRecord([]Record{record, second}, record.RuntimeID, record.BaselineGeneration, record.AssignmentID)
	if err != nil || !ok || got.Revision != 2 {
		t.Fatalf("FindRecord() = revision %d, found %v, err %v", got.Revision, ok, err)
	}
	got, ok, err = FindRecordRevision([]Record{record, second}, record.Reference())
	if err != nil || !ok || got.Revision != 1 {
		t.Fatalf("FindRecordRevision() = revision %d, found %v, err %v", got.Revision, ok, err)
	}
	if _, _, err := FindRecord([]Record{record, record}, record.RuntimeID, record.BaselineGeneration, record.AssignmentID); err == nil {
		t.Fatal("duplicate current revision must fail")
	}
	broken := record
	broken.Revision = 0
	if err := ValidateRecord(broken); err == nil {
		t.Fatal("zero assignment revision must fail")
	}
}

func TestValidateRecordRequiresJSONArrayShapesAndCanonicalSetOrder(t *testing.T) {
	record := recordTestRecord()
	record.DependsOn = nil
	if err := ValidateRecord(record); err == nil {
		t.Fatal("null array-shaped Record fields must fail validation")
	}
	record = recordTestRecord()
	record.ReadPaths = []string{"z/path", "a/path"}
	if err := ValidateRecord(record); err == nil {
		t.Fatal("set-valued Record fields must use canonical sorted order")
	}
}

func recordTestSources(t *testing.T, taskBytes []byte) ([]byte, map[string]any, []ContextSource) {
	t.Helper()
	taskSHA := recordTestSHA(taskBytes)
	manifest := map[string]any{
		"assignment_record_contract": AssignmentRecordContract,
		"manifest_id":                "team-manifest-s6", "version": "v1.2.0", "runtime_id": "loop-s6-record",
		"baseline_generation": 3, "workgroup_id": "workgroup-s6-record", "workgroup_kind": "builder",
		"documents": []any{map[string]any{"id": "TASK-042-01", "path": "docs/dev/tasks/TASK-042-01.md", "version": "v1.0.0", "sha256": taskSHA}},
		"assignments": []any{map[string]any{
			"assignment_id": "assignment-s6-record", "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": "backend-builder",
			"agent_id": "agent-s6-record", "agent_definition_ref": ".claude/agents/backend-builder.md",
			"skill_refs": []string{"backend-engineering"}, "read_paths": []string{}, "scope": []string{"internal/"},
			"write_paths": []string{"internal/pkg/"}, "output_paths": []string{}, "depends_on": []string{},
			"required_checks": []string{}, "done_when": []string{"owned output exists"},
		}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"runtime_id": "loop-s6-record", "revision": float64(8), "lifecycle": map[string]any{"state": "building"},
		"baseline":  map[string]any{"generation": float64(3)},
		"documents": []any{map[string]any{"id": "TASK-042-01", "kind": "task", "path": "docs/dev/tasks/TASK-042-01.md", "version": "v1.0.0", "sha256": taskSHA, "status": "locked", "generation": float64(3)}},
		"entities": map[string]any{
			"tasks":  []any{map[string]any{"id": "TASK-042-01", "path": "docs/dev/tasks/TASK-042-01.md", "sha256": taskSHA, "owner_agent_ids": []string{"agent-s6-record"}}},
			"teams":  []any{map[string]any{"id": "workgroup-s6-record", "kind": "builder", "manifest_ref": "docs/workgroups/s6/manifest.json"}},
			"agents": []any{map[string]any{"id": "agent-s6-record", "team_id": "workgroup-s6-record", "role": "backend-builder", "definition_ref": ".claude/agents/backend-builder.md", "dispatch_mode": "plan_checkpoint", "task_ids": []string{"TASK-042-01"}}},
		},
	}
	sources := []ContextSource{
		{Kind: "agent_definition", Ref: ".claude/agents/backend-builder.md", ID: "backend-builder", SHA256: strings.Repeat("a", 64)},
		{Kind: "authorized_skill", Ref: ".claude/skills/backend-engineering/SKILL.md", ID: "backend-engineering", SHA256: strings.Repeat("b", 64)},
	}
	return manifestBytes, state, sources
}

func deleteOwner(state map[string]any) {
	entities := state["entities"].(map[string]any)
	tasks := entities["tasks"].([]any)
	tasks[0].(map[string]any)["owner_agent_ids"] = []any{}
}

func recordTestRecord() Record {
	hashA := strings.Repeat("a", 64)
	hashB := strings.Repeat("b", 64)
	hashC := strings.Repeat("c", 64)
	manifest := ContextSource{Kind: "manifest", Ref: "docs/workgroups/s6/manifest.json", ID: "team-manifest-s6", Version: "v1.2.0", SHA256: hashA}
	task := ContextSource{Kind: "task", Ref: "docs/dev/tasks/TASK-042-01.md", ID: "TASK-042-01", Version: "v1.0.0", SHA256: hashB}
	role := ContextSource{Kind: "agent_definition", Ref: ".claude/agents/backend-builder.md", ID: "backend-builder", SHA256: hashC}
	skill := ContextSource{Kind: "authorized_skill", Ref: ".claude/skills/backend-engineering/SKILL.md", ID: "backend-engineering", SHA256: hashA}
	sources, _ := canonicalContextSources([]ContextSource{manifest, task, role, skill})
	scope := ScopeBasis{
		ManifestScope:       []string{"internal"},
		ManifestWritePaths:  []string{"internal/pkg"},
		ManifestOutputPaths: []string{},
		TaskWritePaths:      []string{"internal"},
	}
	effective, _ := EffectiveScope(scope)
	return Record{
		SchemaVersion: AssignmentRecordSchema, Contract: AssignmentRecordContract, RuntimeID: "loop-s6-record",
		BaselineGeneration: 3, RegistrationRevision: 9, AssignmentID: "assignment-s6-record", Revision: 1,
		Task: task, Manifest: manifest, AgentDefinition: role, TeamID: "workgroup-s6-record",
		ResponsibilityID: "BUILD-WORK-PACKAGE", OwnerAgentID: "agent-s6-record", RoleFamily: "backend-builder",
		DispatchMode: "plan_checkpoint", ReadPaths: []string{}, SkillRefs: []string{"backend-engineering"}, DependsOn: []string{},
		DoneWhen: []string{"owned output exists"}, RequiredChecks: []string{}, CheckProfiles: copyCheckProfiles(nil),
		ScopeBasis: canonicalScopeBasis(scope), EffectiveWritePaths: effective, ContextSources: sources,
	}
}

func recordTestSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
