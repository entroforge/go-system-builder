package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
)

// Synthetic approved-Case/PlanReport inputs exercise original mechanisms;
// these fixtures are not claims of human or model professional acceptance.
func repairExecutionPlanningFixture(t *testing.T, assertionCopies ...int) (string, repair.RepairPlan, repair.ArtifactRef, repair.ArtifactRef, string) {
	t.Helper()
	root := repairLeadFreshRoot(t)
	writeRepairLeadFile(t, root, "docs/control/agent-protocol.md", "# Installed protocol\n")
	writeRepairLeadFile(t, root, "docs/rules/test.md", "# Fixture rules\n")
	writeRepairLeadFile(t, root, ".claude/agents/backend-builder.md", "# Backend Builder\n")
	names := append(agentcontext.CoreSkills("backend-builder"), agentcontext.RequiredSkills("BUILD-WORK-PACKAGE")...)
	names = append(names, "code-quality", "testing-strategy", "api-contracts", "state-machine-design", "integration-verification")
	for _, name := range names {
		writeRepairLeadFile(t, root, ".claude/skills/"+name+"/SKILL.md", "# "+name+"\n")
	}
	for _, path := range []string{"internal/api/one.go", "internal/api/two.go"} {
		writeRepairLeadFile(t, root, path, "package api\n")
	}
	repairLeadApprovedCase(t, root, "investigation-case-execution-choice", []map[string]any{
		{"id": "unit-one", "description": "restore first authority", "scope": []string{"internal/api/one.go"}, "assertion_ids": []string{"all"}},
		{"id": "unit-two", "description": "restore dependent authority", "scope": []string{"internal/api/two.go"}, "assertion_ids": []string{"all"}, "depends_on": []string{"unit-one"}},
	}, assertionCopies...)
	snapshot := repairDispatchSnapshot(t, root)
	mapFieldCLI(snapshot.State, "configuration")["optimization"] = map[string]any{"packet": true}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), snapshot.State)
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	_, _, sessionRef, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, SessionID: "repair-session-execution-choice", CreatedBy: "Main"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, plan, planRef, err := repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, PlanID: "repair-plan-execution-choice", CreatedBy: "Main"})
	if err != nil || len(plan.Assignments) != 2 {
		t.Fatalf("original two-assignment Plan: %#v %v", plan, err)
	}
	if _, err := prepareRepairEntry(root, statePath, journalPath, snapshot.State); err != nil {
		t.Fatal(err)
	}
	path := repairDispatchPaths(root, snapshot.State)[0]
	return root, plan, sessionRef, planRef, path
}

// Workspace-only tests deliberately register through passive dispatch setup:
// their existing legacy PlanReport fixtures do not claim the normal typed
// dispatch/PlanReport author path and must not bypass an issued-packet guard.
func repairExecutionFixture(t *testing.T, begin bool) (string, repair.RepairPlan, string, []repairDispatchChoice) {
	t.Helper()
	root, plan, sessionRef, planRef, path := repairExecutionPlanningFixture(t)
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	rows := make([]repairDispatchChoice, 0, 2)
	for _, assignment := range plan.Assignments {
		rows = append(rows, repairDispatchChoice{AssignmentID: assignment.AssignmentID, RoleFamily: "backend-builder", CheckLocation: "main"})
	}
	if err := persistRepairLeadQuestionnaire(path, rows); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if handled, code := runObservedRepairDispatch(root, repairLeadSaveInput(root, path, "Main"), &stdout, &stderr); !handled || code != 0 {
		t.Fatalf("original passive dispatch setup: handled=%t code=%d %s %s", handled, code, stdout.String(), stderr.String())
	}
	feedback := stdout.String()
	if strings.Contains(feedback, "did not register") || repairDispatchSnapshot(t, root).State["workspace"] != nil {
		t.Fatalf("pre-execution choice must dispatch without workspace preparation: %s", feedback)
	}
	if !begin {
		return root, plan, path, rows
	}
	snapshot := repairDispatchSnapshot(t, root)
	owners := stringMapCLI(repairPointerCLI(snapshot.State)["assignment_owners"])
	for index, assignment := range plan.Assignments {
		_, reportRef, err := repair.CreatePlanReport(root, repair.PlanReportRequest{Session: sessionRef, Plan: planRef, AssignmentID: assignment.AssignmentID, AgentID: owners[assignment.AssignmentID], ReportID: fmt.Sprintf("repair-plan-report-execution-choice-%d", index), PlanText: "synthetic fixture plan", RedChecks: []repair.RepairCheck{{Name: "fixture red signal", Command: "test -f " + assignment.Scope[0], Result: "fail", EvidenceRefs: []string{"test://fixture-red"}}}, ProposedPaths: assignment.Scope})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := repair.SubmitRepairPlanReportToRuntime(root, statePath, journalPath, repair.SubmitPlanReportRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: owners[assignment.AssignmentID]}, Report: reportRef}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repair.BeginRepairExecution(root, statePath, journalPath, repair.BeginRepairExecutionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}}); err != nil {
		t.Fatal(err)
	}
	return root, plan, path, rows
}

func TestS9RepairExecutionChoiceOriginalPrepareFrozenAndPassiveReadOnly(t *testing.T) {
	root, _, path, rows := repairExecutionFixture(t, true)
	// This existing legacy fixture validates workspace business only; it has
	// no normal typed Plan receipt and does not claim typed Result coverage.
	legacy := repairDispatchSnapshot(t, root)
	mapFieldCLI(legacy.State, "configuration")["optimization"] = map[string]any{"packet": false}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), legacy.State)
	before := repairDispatchRuntimeBytes(t, root)
	var observed, stderr bytes.Buffer
	if handled, code := runObservedRepairDispatch(root, repairLeadSaveInput(root, path, "Main"), &observed, &stderr); !handled || code != 0 || !strings.Contains(observed.String(), "Existing original dispatch reused") {
		t.Fatalf("passive division observation: handled=%t code=%d %s %s", handled, code, observed.String(), stderr.String())
	}
	repairDispatchAssertUnchanged(t, root, before)
	feedback := repairDispatchSave(t, root, path, rows[:1])
	if !strings.Contains(feedback, "runtime workspace prepare --root") {
		t.Fatalf("save omitted the actual Main startup command: %s", feedback)
	}
	repairDispatchAssertUnchanged(t, root, before)
	selected := repairDispatchSnapshot(t, root)
	manifestID, _ := repairExecutionForChoice(selected.State, rows[0])
	owner := stringMapCLI(repairPointerCLI(selected.State)["assignment_owners"])[rows[0].AssignmentID]
	for _, args := range [][]string{{"bind", "--root", root}, {"prepare", "--root", root, "--assignment", manifestID, "--agent", owner, "--check-location", "main"}} {
		var stdout, stderr bytes.Buffer
		if code := runRuntimeWorkspace(args, &stdout, &stderr); code != 0 {
			t.Fatalf("explicit legacy workspace startup %v: %s %s", args, stdout.String(), stderr.String())
		}
	}
	prepared := repairDispatchSnapshot(t, root)
	_, execution := repairExecutionForChoice(prepared.State, rows[0])
	worker := stringValue(execution["worktree_path"])
	if stringValue(execution["status"]) != "ready" || stringValue(execution["check_location"]) != "main" || stringValue(execution["target_branch"]) != "test-development" || worker == "" {
		t.Fatalf("original execution did not freeze the real branch/runner: %#v", execution)
	}
	if _, err := os.Stat(filepath.Join(worker, ".git")); err != nil {
		t.Fatalf("real prepared worktree missing/omitted: %v %s", err, feedback)
	}
	before = repairDispatchRuntimeBytes(t, root)
	for _, event := range []string{"PreToolUse", "PreCompact", "Stop"} {
		context := stageRepairDispatchContext(root, policy.Input{Event: event}, prepared)
		if !strings.Contains(context, path) || !strings.Contains(context, worker) || !strings.Contains(context, "normal tool") || !strings.Contains(context, "this reminder only reads current facts") {
			t.Fatalf("%s omitted exact read-only execution facts: %s", event, context)
		}
	}
	if feedback := repairDispatchSave(t, root, path, rows[:1]); !strings.Contains(feedback, "runtime workspace prepare --root") {
		t.Fatalf("same ready execution was prepared again: %s", feedback)
	}
	repairDispatchAssertUnchanged(t, root, before)
	changed := rows[0]
	changed.CheckLocation = "worker"
	if feedback := repairDispatchSave(t, root, path, []repairDispatchChoice{changed}); !strings.Contains(feedback, "conflict") || !strings.Contains(feedback, "froze check_location=main") {
		t.Fatalf("frozen runner silently changed: %s", feedback)
	}
	repairDispatchAssertUnchanged(t, root, before)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if context := stageRepairDispatchContext(root, policy.Input{Event: "Stop"}, prepared); !strings.Contains(context, path) || !strings.Contains(context, "not prepared yet") {
		t.Fatalf("read-only missing division context: %s", context)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Stop recreated the missing division file: %v", err)
	}
	stageRepairDispatchContext(root, policy.Input{Event: "PreToolUse"}, prepared)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Pre recreated the missing division file: %v", err)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestS9RepairExecutionChoiceQueuedSameSaveAfterOriginalResult(t *testing.T) {
	root, plan, path, rows := repairExecutionFixture(t, true)
	// This existing legacy fixture validates workspace business only; it has
	// no normal typed Plan receipt and does not claim typed Result coverage.
	legacy := repairDispatchSnapshot(t, root)
	mapFieldCLI(legacy.State, "configuration")["optimization"] = map[string]any{"packet": false}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), legacy.State)
	feedback := repairDispatchSave(t, root, path, rows[1:])
	if !strings.Contains(feedback, "runtime workspace prepare --root") {
		t.Fatalf("queued original dependency was not normal pending: %s", feedback)
	}
	queued := repairDispatchSnapshot(t, root)
	context := stageRepairExecutionPrepareContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main"}, queued)
	if !strings.Contains(context, "unit-one") || !strings.Contains(context, "runtime workspace prepare --root") || !strings.Contains(context, "--check-location") {
		t.Fatalf("read-only context omitted the named dependency or saved startup command: %s", context)
	}
	if _, execution := repairExecutionForChoice(queued.State, rows[1]); len(execution) != 0 {
		t.Fatalf("queued sibling was prepared prematurely: %#v", execution)
	}
	before := repairDispatchRuntimeBytes(t, root)
	for i := 0; i < 3; i++ {
		stageRepairExecutionPrepareContext(root, policy.Input{Event: "PreToolUse"}, queued)
		var stdout, stderr bytes.Buffer
		runObservedRepairDispatch(root, repairLeadSaveInput(root, path, "Main"), &stdout, &stderr)
	}
	repairDispatchAssertUnchanged(t, root, before)
	changedPath := plan.Assignments[0].Scope[0]
	changed := []byte("package api\n\n// Synthetic original Result fixture.\n")
	writeRepairLeadFile(t, root, changedPath, string(changed))
	repairLeadGit(t, root, "add", changedPath)
	repairLeadGit(t, root, "commit", "-qm", "fixture predecessor repair")
	owner := stringMapCLI(repairPointerCLI(queued.State)["assignment_owners"])[rows[0].AssignmentID]
	_, _, _, err := repair.SubmitRepairResultToRuntime(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: owner}, Result: repair.RepairResultRequest{ResultID: "repair-result-prepare-predecessor", AssignmentID: rows[0].AssignmentID, ProducerAgentID: owner, UnitResults: []repair.RepairUnitResult{{UnitID: plan.Assignments[0].UnitIDs[0], Status: "pass", EvidenceRefs: []string{"test://fixture-green"}}}, ChangedArtifacts: []repair.ChangedArtifact{{Path: changedPath, SHA256: repairLeadDigest(changed), Status: "modified"}}, Checks: []repair.RepairCheck{{Name: "fixture green", Command: "test -f " + changedPath, Result: "pass", EvidenceRefs: []string{"test://fixture-green"}}}, Result: "pass"}})
	if err != nil {
		t.Fatal(err)
	}
	if feedback := repairDispatchSave(t, root, path, rows[1:]); !strings.Contains(feedback, "runtime workspace prepare --root") {
		t.Fatalf("same choice omitted explicit startup after original Result: %s", feedback)
	}
	current := repairDispatchSnapshot(t, root)
	manifestID, _ := repairExecutionForChoice(current.State, rows[1])
	secondOwner := stringMapCLI(repairPointerCLI(current.State)["assignment_owners"])[rows[1].AssignmentID]
	for _, args := range [][]string{{"bind", "--root", root}, {"prepare", "--root", root, "--assignment", manifestID, "--agent", secondOwner, "--check-location", "main"}} {
		var stdout, stderr bytes.Buffer
		if code := runRuntimeWorkspace(args, &stdout, &stderr); code != 0 {
			t.Fatalf("explicit legacy dependent startup %v: %s %s", args, stdout.String(), stderr.String())
		}
	}
	if _, execution := repairExecutionForChoice(repairDispatchSnapshot(t, root).State, rows[1]); execution["status"] != "ready" {
		t.Fatalf("explicit original startup did not prepare the released dependency: %+v", execution)
	}
}

func TestS9RepairExecutionChoicePlanSourceAndPreExecutionRemainOriginal(t *testing.T) {
	root, _, path, rows := repairExecutionFixture(t, false)
	snapshot := repairDispatchSnapshot(t, root)
	if len(repairDispatchPaths(root, snapshot.State)) != 1 || snapshot.State["workspace"] != nil {
		t.Fatal("pre-execution choice manufactured a workspace")
	}
	rows[0].CheckLocation = "guess"
	before := repairDispatchRuntimeBytes(t, root)
	if feedback := repairDispatchSave(t, root, path, rows); !strings.Contains(feedback, "check_location must") {
		t.Fatalf("invalid runner choice was not named: %s", feedback)
	}
	repairDispatchAssertUnchanged(t, root, before)
	data, err := os.ReadFile(resolveRootPath(root, stringValue(repairPointerCLI(snapshot.State)["plan_ref"])))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	_ = json.Unmarshal(data, &document)
	document["execution_policy"] = "not-an-original-schema-value"
	data, _ = json.Marshal(document)
	ref := repairPointerCLI(snapshot.State)
	if err := os.WriteFile(resolveRootPath(root, stringValue(ref["plan_ref"])), data, 0o644); err != nil {
		t.Fatal(err)
	}
	ref["plan_sha256"] = repairLeadDigest(data)
	if _, err := repairDispatchObservedPlan(root, snapshot.State); err != nil {
		t.Fatalf("ordinary projection unexpectedly ran the full schema validator: %v", err)
	}
	if _, err := repairDispatchPlan(root, snapshot.State); err == nil {
		t.Fatal("save path no longer retains original Plan validation")
	}
	if !repairDispatchHasProfessionalContent(path, []byte(`[{"assignment_id":"given","check_location":"main"}]`)) {
		t.Fatal("actual runner choice was lost by the professional-content detector")
	}
}
