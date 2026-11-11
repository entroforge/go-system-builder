package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

const (
	canonicalRuntimeID  = "loop-s6-main-fixture"
	canonicalTeamID     = "workgroup-s6-main-fixture"
	canonicalAgentID    = "agent-s6-main-fixture"
	canonicalAssignment = "assignment-s6-main-fixture"
	canonicalTaskID     = "TASK-001-01"
	canonicalManifest   = ".claude/workgroups/manifest-s6-main-fixture.json"
	canonicalTask       = "docs/dev/tasks/TASK-001-01.md"
	canonicalPlanRef    = ".claude/evidence/workgroup-s6-main-fixture-round-1/TASK-001-01/plan-report.json"
	canonicalProofRef   = "src/pkg/proof.txt"
	canonicalCheckFile  = "tools/main-check.sh"
	canonicalSourceFile = "src/pkg/behavior.txt"
	canonicalBranch     = "s6-main-fixture-worker"
)

type canonicalMainFixture struct {
	hookBinary  string // Set only for production-process Hook acceptance tests.
	root        string
	worker      string
	statePath   string
	journalPath string
	counterPath string
	failPath    string
	command     string
	record      assignmentrecord.Record
	planPath    string
	result      assignment.ResultAcceptanceReceipt
}

func TestCanonicalMainIntegrationBlocksActualOutOfScopeGitDelta(t *testing.T) {
	ctx := context.Background()
	fixture := newCanonicalMainFixture(t, "docs/unowned.txt")
	inspection := fixture.inspect(t, "")
	if inspection.Ready || !reflect.DeepEqual(inspection.OutOfScopeDiff, []string{"docs/unowned.txt"}) {
		t.Fatalf("out-of-scope Inspect = ready %t diff %v blockers %v", inspection.Ready, inspection.OutOfScopeDiff, inspection.Blockers)
	}
	before := readGit(t, fixture.root, "rev-parse", "main")
	if _, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig()); err == nil {
		t.Fatal("out-of-scope actual Git delta reached Main integration")
	}
	if after := readGit(t, fixture.root, "rev-parse", "main"); after != before {
		t.Fatalf("out-of-scope delta moved target from %s to %s", before, after)
	}
	if count := fileSize(t, fixture.counterPath); count != 0 {
		t.Fatalf("out-of-scope delta ran Main checks %d times", count)
	}
}

func newCanonicalMainFixture(t *testing.T, changedPath string) *canonicalMainFixture {
	return newCanonicalMainFixtureWithClosing(t, changedPath, false, false)
}

func newCanonicalMainFixtureWithClosing(t *testing.T, changedPath string, closing, workerCheck bool) *canonicalMainFixture {
	return newCanonicalMainFixtureWithSetup(t, changedPath, closing, workerCheck, nil)
}

func newCanonicalMainFixtureWithSetup(t *testing.T, changedPath string, closing, workerCheck bool, beforeRegister func(t *testing.T, root, statePath, journalPath string)) *canonicalMainFixture {
	t.Helper()
	return newCanonicalMainFixtureWithWorkerSetup(t, changedPath, closing, workerCheck, beforeRegister, nil)
}

func newCanonicalMainFixtureWithWorkerSetup(t *testing.T, changedPath string, closing, workerCheck bool, beforeRegister func(t *testing.T, root, statePath, journalPath string), workerSetup func(t *testing.T, worker string)) *canonicalMainFixture {
	t.Helper()
	root := t.TempDir()
	counterDir := t.TempDir()
	counterPath := filepath.Join(counterDir, "main-check-count")
	failPath := filepath.Join(counterDir, "fail-main-check")
	if err := os.WriteFile(counterPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	command := "sh " + shellQuote(canonicalCheckFile) + " " + shellQuote(counterPath) + " " + shellQuote(failPath)
	profile := checkexec.Profile{
		ID: "profile-s6-main-integration", Version: "v1.0.0", CheckIndex: 0, Command: command,
		Location: "main", TimeoutSeconds: 60, EnvironmentPolicy: "inherited", Environment: []checkexec.EnvFact{},
		Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree",
		SideEffects: "unknown", ReusePolicy: "never",
	}
	copyFixtureTree(t, filepath.Join(sourceRepositoryRoot(t), "docs", "control"), filepath.Join(root, "docs", "control"))
	copyFixtureFile(t, filepath.Join(sourceRepositoryRoot(t), "agents", "backend-builder.md"), filepath.Join(root, ".claude", "agents", "backend-builder.md"))
	for _, skill := range catalog.Skills {
		copyFixtureFile(t, filepath.Join(sourceRepositoryRoot(t), "skills", skill.Name, "SKILL.md"), filepath.Join(root, ".claude", "skills", skill.Name, "SKILL.md"))
	}

	taskBytes := []byte(fmt.Sprintf("# %s: Main integration fixture\n\n> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-001\n> Dispatch policy: waves-v1\n> Primary contract: BE-001\n\n## Scope\n\n| Type | Paths |\n| --- | --- |\n| prospective write paths | `src/pkg` |\n", canonicalTaskID))
	writeFixtureFile(t, root, canonicalTask, taskBytes)
	index := []byte(`# REQ-001 dispatch plan

> REQ: REQ-001
> Dispatch policy: waves-v1
> Revision: 1
> Status: complete

## W1 · one current builder assignment

- [ ] [current task](TASK-001-01.md)
`)
	writeFixtureFile(t, root, "docs/dev/tasks/index-REQ-001.md", index)
	writeFixtureFile(t, root, canonicalProofRef, []byte("original scoped evidence source\n"))
	writeFixtureFile(t, root, "src/pkg/input.go", []byte("package pkg\n"))
	writeFixtureFile(t, root, canonicalCheckFile, []byte(`#!/bin/sh
set -eu
counter="$1"
fail="$2"
if [ -f "$fail" ]; then
  echo "injected check failure" >&2
  exit 17
fi
test -f src/pkg/behavior.txt
grep -q 'main-check-marker' src/pkg/behavior.txt
printf x >> "$counter"
`))
	writeFixtureFile(t, root, ".gitignore", []byte("/.claude/loop-state.json\n/.claude/loop-state.json.lock.process\n/.claude/loop-events.jsonl\n/.claude/loop-metrics.json\n/.claude/integration.lock\n/.claude/runtime-checkpoints/\n/.claude/evidence/\n/.claude/hook-metrics/\n"))
	writeFixtureFile(t, root, ".claude/loop-state.json", []byte(`{"bound_req":{"id":"REQ-001"}}`))

	assignmentManifest := map[string]any{
		"assignment_id": canonicalAssignment, "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": "backend-builder",
		"agent_id": canonicalAgentID, "agent_definition_ref": ".claude/agents/backend-builder.md",
		"skill_refs": []string{"backend-engineering"}, "read_paths": []string{"docs/dev/tasks/"},
		"scope": []string{"src/pkg"}, "write_paths": []string{"src/pkg"}, "output_paths": []string{},
		"depends_on": []string{}, "required_checks": []string{command}, "done_when": []string{"the real Main required check passes"},
		"dispatch_mode": "plan_checkpoint", "check_profiles": []checkexec.Profile{profile},
		"reuse_decision": "create", "grouping_rationale": "one complete S6 Main integration slice", "status": "planned",
	}
	if workerCheck {
		workerProfile := profile
		workerProfile.ID = "profile-s6-worker-owned"
		workerProfile.Command = "go test ./src/pkg"
		workerProfile.Location = "worker"
		assignmentManifest["integration_check_mode"] = "post_merge"
		assignmentManifest["closing_contract"] = map[string]any{"contract": assignmentrecord.ClosingContractVersion, "worker_checks": []any{
			map[string]any{"index": 0, "command": workerProfile.Command, "profile": workerProfile},
		}}
	} else if closing {
		// Empty owner-check sets still require the explicit, frozen contract.
		assignmentManifest["integration_check_mode"] = "post_merge"
		assignmentManifest["closing_contract"] = map[string]any{"contract": assignmentrecord.ClosingContractVersion, "worker_checks": []any{}}
	}
	manifest := map[string]any{
		"schema_version": "1.0.0", "assignment_record_contract": assignmentrecord.AssignmentRecordContract,
		"manifest_id": "team-manifest-s6-main-fixture", "version": "v1.0.0", "runtime_id": canonicalRuntimeID,
		"req_id": "REQ-001", "baseline_generation": 1,
		"platform_team_id": "platform-s6-main-fixture", "workgroup_id": canonicalTeamID,
		"workgroup_kind": "builder", "status": "planned",
		"documents": []any{map[string]any{"id": canonicalTaskID, "path": canonicalTask, "version": "v1.0.0"}},
		"risk_tags": []any{},
		"responsibility_dispositions": []any{map[string]any{
			"responsibility_id": "BUILD-WORK-PACKAGE", "disposition": "assigned", "trigger": "one scoped builder TASK",
			"assignment_ids": []string{canonicalAssignment}, "na_rationale": nil, "evidence_ref": nil,
		}},
		"assignments":      []any{assignmentManifest},
		"separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1,
		"quantity_rationale": "one owner for the bounded Main integration fixture",
		"validation": map[string]any{
			"result": "pass", "missing_responsibilities": []string{}, "unresolved_conflicts": []string{},
			"warnings": []string{}, "validated_at": "2026-10-14T00:00:00Z",
		},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, root, canonicalManifest, manifestBytes)
	gitFixture(t, root, "init", "-b", "main")
	gitFixture(t, root, "config", "user.name", "S6 integration test")
	gitFixture(t, root, "config", "user.email", "s6-integration@example.invalid")
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "commit", "-m", "S6 canonical Main fixture baseline")

	statePath, journalPath := prepareCanonicalRuntime(t, root, taskBytes, index)
	if beforeRegister != nil {
		beforeRegister(t, root, statePath, journalPath)
	}

	worker := filepath.Join(t.TempDir(), "worker")
	gitFixture(t, root, "worktree", "add", "-b", canonicalBranch, worker, "main")
	if changedPath == canonicalSourceFile {
		writeFixtureFile(t, worker, canonicalSourceFile, []byte("main-check-marker\n"))
	} else {
		writeFixtureFile(t, worker, changedPath, []byte("outside the original Assignment scope\n"))
	}
	if workerSetup != nil {
		workerSetup(t, worker)
	}
	gitFixture(t, worker, "add", ".")
	gitFixture(t, worker, "commit", "-m", "S6 worker source commit")

	preRegister := readRuntimeSnapshot(t, statePath, journalPath)
	if _, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: preRegister.Revision, ManifestPath: filepath.Join(root, filepath.FromSlash(canonicalManifest)),
		TaskID: canonicalTaskID, TaskPath: filepath.Join(root, filepath.FromSlash(canonicalTask)),
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("original Register: %v", err)
	}
	snapshot := readRuntimeSnapshot(t, statePath, journalPath)
	record, found, err := assignmentrecord.ForOwner(snapshot.State, canonicalAgentID)
	if err != nil || !found {
		t.Fatalf("read registered Assignment Record: found %t err %v", found, err)
	}
	planPath := writeCanonicalPlan(t, root, record)
	if _, outcome, err := assignment.AgentBegin(root, statePath, journalPath, assignment.AgentBeginRequest{
		ExpectedRevision: -1, AgentID: canonicalAgentID, PlanPath: planPath,
		OccurredAt: time.Now().UTC(),
	}); err != nil || !outcome.Chained {
		t.Fatalf("original AgentBegin: outcome %+v err %v", outcome, err)
	}
	fixture := &canonicalMainFixture{
		root: root, worker: worker, statePath: statePath, journalPath: journalPath,
		counterPath: counterPath, failPath: failPath, command: command, record: record, planPath: planPath,
	}
	fixture.result = fixture.acceptResult(t, "msg-s6-main-result-1", "The scoped behavior was implemented and evidence was recorded.")
	return fixture
}

func (f *canonicalMainFixture) acceptResult(t *testing.T, messageID, summary string) assignment.ResultAcceptanceReceipt {
	return f.acceptResultWithClosing(t, messageID, summary, "complete", nil, nil)
}

func (f *canonicalMainFixture) acceptResultWithClosing(t *testing.T, messageID, summary, disposition string, risks, deviations []string) assignment.ResultAcceptanceReceipt {
	t.Helper()
	occurredAt := time.Now().UTC()
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "completion_report", "message_id": messageID,
		"correlation_id": "corr-" + messageID, "runtime_id": f.record.RuntimeID,
		"baseline_generation": f.record.BaselineGeneration, "agent_id": f.record.OwnerAgentID,
		"agent_definition_ref": f.record.AgentDefinition.Ref, "task_id": f.record.Task.ID, "bug_id": nil,
		"team_id": f.record.TeamID, "occurred_at": occurredAt.Format(time.RFC3339Nano),
		"assignment_id": f.record.AssignmentID, "assignment_revision": f.record.Revision,
		"assignment_record_contract": f.record.Contract,
		"summary":                    summary,
		"assertion_evidence": []any{map[string]any{
			"assertion": "TASK behavior is implemented", "oracle": "the real Main required check passes",
			"evidence_refs": []string{canonicalProofRef},
		}},
		"risks_blockers": nonNilStrings(risks), "scope_deviations": nonNilStrings(deviations),
	}
	if f.record.ClosingContract != nil {
		judgment := "satisfied"
		if disposition == "incomplete" || disposition == "blocked" {
			judgment = disposition
		}
		message["closing_assessment"] = map[string]any{
			"disposition": disposition,
			"conditions": []any{map[string]any{
				"done_when_index": 0, "judgment": judgment,
				"plan_assertion_indexes": []int{0}, "rationale": "The owner assessed the frozen DoneWhen against its accepted PLAN assertion and evidence.",
			}},
		}
	}
	messagePath := filepath.Join(f.root, ".claude", "evidence", canonicalTeamID, canonicalTaskID, messageID+".json")
	writeJSONFixture(t, messagePath, message)
	_, receipt, err := assignment.CompleteTaskWithReceipt(f.root, f.statePath, f.journalPath, assignment.CompletionRequest{
		ExpectedRevision: -1, AgentID: f.record.OwnerAgentID, MessagePath: messagePath,
		OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatalf("original CompleteTask Result writer: %v", err)
	}
	if !receipt.Current || receipt.ResultRevision == 0 || receipt.ResultRef == "" {
		t.Fatalf("original Result receipt = %+v", receipt)
	}
	resultBytes, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(receipt.ResultRef)))
	if err != nil {
		t.Fatalf("read original accepted Result %s: %v", receipt.ResultRef, err)
	}
	var accepted assignmentresult.AcceptedResult
	if err := json.Unmarshal(resultBytes, &accepted); err != nil {
		t.Fatalf("decode original accepted Result: %v", err)
	}
	if accepted.Conclusion != "submitted" || accepted.RuntimeID != f.record.RuntimeID ||
		accepted.BaselineGeneration != f.record.BaselineGeneration || accepted.AssignmentRecord != f.record.Reference() ||
		accepted.ProducerAgentID != f.record.OwnerAgentID || accepted.TaskID != f.record.Task.ID ||
		accepted.ResultRevision != receipt.ResultRevision || accepted.SubmissionKey != messageID || accepted.Answer.Summary != summary {
		t.Fatalf("accepted Result does not preserve original responsibility and answer: %+v", accepted)
	}
	if len(accepted.Answer.AssertionEvidence) != 1 || accepted.Answer.AssertionEvidence[0].Assertion != "TASK behavior is implemented" ||
		accepted.Answer.AssertionEvidence[0].Oracle != "the real Main required check passes" ||
		!reflect.DeepEqual(accepted.Answer.AssertionEvidence[0].EvidenceRefs, []string{canonicalProofRef}) {
		t.Fatalf("accepted Result did not preserve author's actual assertion/evidence: %+v", accepted.Answer.AssertionEvidence)
	}
	if got := f.currentResultRef(t); got != receipt.ResultRef {
		t.Fatalf("original owner Result reference = %s, want %s", got, receipt.ResultRef)
	}
	return receipt
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func (f *canonicalMainFixture) inspect(t *testing.T, _ string) integration.Inspection {
	t.Helper()
	snapshot := readRuntimeSnapshot(t, f.statePath, f.journalPath)
	record, found, err := assignmentrecord.ForOwner(snapshot.State, f.record.OwnerAgentID)
	if err != nil || !found || record.Reference() != f.record.Reference() {
		t.Fatalf("current original Record = found %t record %+v err %v", found, record.Reference(), err)
	}
	ref := record.Reference()
	projection := hookctx.AssignmentContext{
		AssignmentID: record.AssignmentID, AssignmentRecord: &ref,
		AssignmentRevision: record.Revision, BaselineGeneration: record.BaselineGeneration,
		TaskID: record.Task.ID, OwnerAgentID: record.OwnerAgentID, RoleFamily: record.RoleFamily,
		AgentDefinitionRef: record.AgentDefinition.Ref, WorktreePath: f.worker, Branch: canonicalBranch,
		TargetBranch: "main", ManifestRef: record.Manifest.Ref, State: "reported",
		CompletionRef: f.currentResultRef(t), WritePaths: append([]string{}, record.EffectiveWritePaths...),
		ReadPaths: append([]string{}, record.ReadPaths...), OutputPaths: append([]string{}, record.ScopeBasis.ManifestOutputPaths...),
		SkillRefs: append([]string{}, record.SkillRefs...), DependsOn: append([]string{}, record.DependsOn...),
		RequiredChecks: append([]string{}, record.RequiredChecks...), CheckProfiles: append([]checkexec.Profile{}, record.CheckProfiles...),
		DoneWhen: append([]string{}, record.DoneWhen...), ResponsibilityIDs: []string{record.ResponsibilityID},
	}
	return func() integration.Inspection {
		inspection, err := integration.Inspect(context.Background(), integration.InspectRequest{
			Root: f.root, Assignment: projection, TargetBranch: "main", BaselineGeneration: record.BaselineGeneration,
			RuntimeID: record.RuntimeID,
		}, integration.InspectConfig{})
		if err != nil {
			t.Fatalf("original Inspect: %v", err)
		}
		return inspection
	}()
}

func (f *canonicalMainFixture) currentResultRef(t *testing.T) string {
	t.Helper()
	snapshot := readRuntimeSnapshot(t, f.statePath, f.journalPath)
	entities := snapshot.State["entities"].(map[string]any)
	for _, raw := range entities["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == f.record.OwnerAgentID {
			ref, _ := row["completion_reported_ref"].(string)
			if ref == "" {
				t.Fatal("original Runtime Agent has no current Result ref")
			}
			return ref
		}
	}
	t.Fatal("original Runtime Agent is missing")
	return ""
}

func (f *canonicalMainFixture) integrateConfig() integration.IntegrateConfig {
	return integration.IntegrateConfig{Root: f.root, GitRoot: f.root, RuntimeID: canonicalRuntimeID}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func prepareCanonicalRuntime(t *testing.T, root string, taskBytes, indexBytes []byte) (string, string) {
	t.Helper()
	raw, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = "1.2.0"
	state["runtime_id"] = canonicalRuntimeID
	state["revision"] = float64(8)
	delete(state, "milestone")
	state["last_transition"] = nil
	state["definition"] = fixtureFingerprint(t, root, "docs/control/loop-definition.json", "1.3.0")
	state["hook_control"].(map[string]any)["policy_ref"] = fixtureFingerprint(t, root, "docs/control/hook-policy.json", "v2.0.0")
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": float64(0)}
	state["bound_req"] = map[string]any{
		"path": "docs/requirements/REQ-001.md", "version": "v1.0.0",
		"id": "REQ-001", "status": "locked", "approved_by": "user", "approved_at": "2026-10-14T00:00:00Z",
		"workspace": map[string]any{"project_root": root, "dev_branch": "main", "release_upstream": "main"},
	}
	state["documents"] = []any{
		map[string]any{"id": canonicalTaskID, "kind": "task", "path": canonicalTask, "version": "v1.0.0", "status": "locked", "generation": float64(1)},
		map[string]any{"id": "dispatch-plan:REQ-001", "kind": "dispatch_plan", "path": "docs/dev/tasks/index-REQ-001.md", "version": "1", "status": "locked", "generation": float64(1)},
	}
	state["baseline"] = map[string]any{"generation": float64(1), "captured_at": "2026-10-14T00:00:00Z"}
	state["entities"] = map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}, "assignments": []any{}}
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": float64(0), "last_event_id": nil}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	writeJSONFixture(t, statePath, state)
	return statePath, journalPath
}

func writeCanonicalPlan(t *testing.T, root string, record assignmentrecord.Record) string {
	t.Helper()
	assertion := map[string]any{"assertion": "TASK behavior is implemented", "oracle": "the real Main required check passes"}
	if record.ClosingContract != nil {
		assertion["done_when_indexes"] = []int{0}
	}
	plan := map[string]any{
		"schema_version": "1.0.0", "message_type": "plan_report", "message_id": "msg-s6-main-plan",
		"correlation_id": "corr-s6-main-plan", "runtime_id": record.RuntimeID,
		"agent_id": record.OwnerAgentID, "agent_definition_ref": record.AgentDefinition.Ref,
		"task_id": record.Task.ID, "bug_id": nil, "team_id": record.TeamID,
		"occurred_at": time.Now().UTC().Format(time.RFC3339Nano), "assignment_id": record.AssignmentID,
		"assignment_revision": record.Revision, "assignment_record_contract": record.Contract,
		"objective":        "Implement the reviewed builder responsibility.",
		"planned_paths":    []string{canonicalSourceFile},
		"assertion_checks": []any{assertion},
		"dependencies":     []string{}, "risks_blockers": []string{},
	}
	path := filepath.Join(root, filepath.FromSlash(canonicalPlanRef))
	writeJSONFixture(t, path, plan)
	return path
}

func fixtureFingerprint(t *testing.T, root, ref, version string) map[string]any {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ref))); err != nil {
		t.Fatal(err)
	}
	return map[string]any{"path": ref, "version": version}
}

func sourceRepositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func copyFixtureTree(t *testing.T, source, target string) {
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

func copyFixtureFile(t *testing.T, source, target string) {
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

func writeFixtureFile(t *testing.T, root, ref string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readRuntimeSnapshot(t *testing.T, statePath, journalPath string) loopruntime.Snapshot {
	t.Helper()
	snapshot, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatalf("read original Runtime snapshot: %v", err)
	}
	return snapshot
}

func gitFixture(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func readGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	return gitFixture(t, root, args...)
}

func digestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileSize(t *testing.T, path string) int {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return int(info.Size())
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
