package cli

import (
	"bytes"
	stdcontext "context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/targetedauthoring"
)

func repairVerifierDispatchSave(t *testing.T, root, path string, rows []repairVerifierDispatchChoice) string {
	t.Helper()
	if err := persistRepairLeadQuestionnaire(path, rows); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	handled, code := runSavedRepairVerifierDispatch(root, repairLeadSaveInput(root, path, "Main"), &stdout, &stderr)
	if !handled || code != 0 {
		t.Fatalf("verifier division save: handled=%t code=%d stdout=%s stderr=%s", handled, code, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func repairVerifierDispatchBuilder(t *testing.T, root, assignmentID, owner string) runtime.Snapshot {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := runRuntimeRepairDispatch([]string{"--root", root, "--assignment-id", assignmentID, "--agent-id", owner}, &stdout, &stderr); code != 0 {
		t.Fatalf("original Builder dispatch: code=%d %s %s", code, stdout.String(), stderr.String())
	}
	return repairDispatchSnapshot(t, root)
}

func TestS9RepairVerifierDispatchQuestionnaireOneIndependentContractOwner(t *testing.T) {
	root, snapshot, plan, _ := repairDispatchFixture(t)
	snapshot.State["schema_version"] = "1.2.0"
	mapFieldCLI(snapshot.State, "configuration")["optimization"] = map[string]any{"packet": true}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), snapshot.State)
	path := repairVerifierDispatchPaths(root, snapshot.State)[0]
	before := repairDispatchRuntimeBytes(t, root)
	context := stageRepairVerifierDispatchContext(root, policy.Input{Event: "PreToolUse"}, snapshot)
	contractPath := resolveRootPath(root, stringValue(repairPointerCLI(snapshot.State)["contract_ref"]))
	for _, want := range []string{path, contractPath, "all current Contract assertions", "not required for every Builder", "role_family=test-builder", "restore first authority", "ordinary Builder selection", "dispatch only when the current Targeted responsibility", "intentionally use the same Builder assignment_id", "Do not write a verifier assignment ID"} {
		if !strings.Contains(context, want) {
			t.Fatalf("verifier professional view lacks %q: %s", want, context)
		}
	}
	for _, redundant := range []string{"symptom_assertions:", "root_invariant_assertions:", "detection_gap_assertions:", "depends_on=", "resource_locks="} {
		if strings.Contains(context, redundant) {
			t.Fatalf("Main verifier division repeated full or empty source data %q: %s", redundant, context)
		}
	}
	data, err := os.ReadFile(path)
	var rows []repairVerifierDispatchChoice
	if err != nil || json.Unmarshal(data, &rows) != nil || len(rows) != 2 || repairVerifierDispatchHasProfessionalContent(path, data) {
		t.Fatalf("unselected original anchors: %s %v", data, err)
	}
	if feedback := repairVerifierDispatchSave(t, root, path, rows); !strings.Contains(feedback, "normal pending draft") {
		t.Fatalf("blank verifier answer was not normal pending: %s", feedback)
	}
	repairDispatchAssertUnchanged(t, root, before)
	snapshot = repairVerifierDispatchBuilder(t, root, plan.Assignments[0].AssignmentID, "builder-for-verifier")
	rows[0].IndependentVerification = true
	feedback := repairVerifierDispatchSave(t, root, path, rows)
	registered := repairDispatchSnapshot(t, root)
	verificationID := repair.VerificationAssignmentID(plan.Assignments[0].AssignmentID)
	owners := stringMapCLI(repairPointerCLI(registered.State)["assignment_owners"])
	owner := owners[verificationID]
	if owner == "" || owner == "builder-for-verifier" || len(owners) != 2 || registered.Revision != snapshot.Revision+1 {
		t.Fatalf("one original verifier must register independently, not per Builder: owners=%#v revision=%d", owners, registered.Revision)
	}
	for _, want := range []string{owner, verificationID, "TASK", "Manifest:", "wait for the original Final integration and ChangeImpact", "no native agent was started"} {
		if !strings.Contains(feedback, want) {
			t.Fatalf("original verifier dispatch feedback lacks %q: %s", want, feedback)
		}
	}
	entities := mapFieldCLI(registered.State, "entities")
	var verifierAgent map[string]any
	for _, agent := range objectSliceAny(entities["agents"]) {
		if stringValue(agent["id"]) == owner {
			verifierAgent = agent
		}
	}
	if verifierAgent == nil || stringValue(verifierAgent["role"]) != "test-builder" {
		t.Fatalf("original verifier Agent role: %#v", verifierAgent)
	}
	manifestRef, _, _ := strings.Cut(stringValue(verifierAgent["prompt_ref"]), "#")
	manifestData, err := os.ReadFile(resolveRootPath(root, manifestRef))
	var manifest map[string]any
	if err != nil || json.Unmarshal(manifestData, &manifest) != nil {
		t.Fatalf("real original manifest: %v", err)
	}
	assignments := objectSliceAny(manifest["assignments"])
	if len(assignments) != 1 || stringValue(assignments[0]["role_family"]) != "test-builder" || len(stringSliceAny(assignments[0]["write_paths"])) != 0 {
		t.Fatalf("verifier acquired product writes or a different role: %#v", assignments)
	}
	targetPath, pathErr := repairTargetedQuestionnairePath(root, registered.State, verificationID)
	if pathErr != nil || !strings.Contains(filepath.ToSlash(targetPath), "/.loop-submissions/s9/") {
		t.Fatalf("new original Targeted source path %s: %v", targetPath, pathErr)
	}
	relativeTarget, err := filepath.Rel(root, targetPath)
	if err != nil {
		t.Fatal(err)
	}
	actualPaths, err := targetedauthoring.ControlPaths(root, registered.State, []string{filepath.ToSlash(relativeTarget)}, stdcontext.Background())
	if err != nil || len(actualPaths) != 1 || actualPaths[0] != filepath.ToSlash(relativeTarget) {
		t.Fatalf("actual generated TASK control path proof: %v %v", actualPaths, err)
	}
	historyBytes, _ := json.Marshal(registered.State)
	var historyState map[string]any
	json.Unmarshal(historyBytes, &historyState)
	mapFieldCLI(historyState, "review")["repair"] = map[string]any{"session_id": "later-session"}
	historyPaths, err := targetedauthoring.ControlPaths(root, historyState, []string{filepath.ToSlash(relativeTarget)}, stdcontext.Background())
	if err != nil || len(historyPaths) != 1 || historyPaths[0] != filepath.ToSlash(relativeTarget) {
		t.Fatalf("actual generated TASK historical control proof: %v %v", historyPaths, err)
	}
	taskFound := false
	for _, task := range objectSliceAny(entities["tasks"]) {
		for _, id := range stringSliceAny(verifierAgent["task_ids"]) {
			if task["id"] != id {
				continue
			}
			taskData, readErr := os.ReadFile(resolveRootPath(root, stringValue(task["path"])))
			var taskDocument map[string]any
			if readErr != nil || json.Unmarshal(taskData, &taskDocument) != nil {
				t.Fatalf("packet verifier TASK: %v", readErr)
			}
			declaration, _ := taskDocument["targeted_authoring"].(map[string]any)
			if declaration["protocol"] != repair.SharedTargetedAuthoringProtocol {
				t.Fatalf("new verifier TASK omitted its fixed source protocol: %v", declaration)
			}
			normal, sourceErr := repair.UsesSharedTargetedAuthoring(root, registered.State, plan.Assignments[0].AssignmentID, verificationID, owner)
			if sourceErr != nil || !normal {
				t.Fatalf("new original registered source proof %v: %v", normal, sourceErr)
			}
			instruction := stringValue(taskDocument["instruction"]) + " " + stringValue(taskDocument["next_action"])
			if !strings.Contains(instruction, "Fill and save") || !strings.Contains(instruction, targetPath) || !strings.Contains(instruction, "PreToolUse") || !strings.Contains(instruction, "PostToolUse") || !strings.Contains(instruction, "Stop/SubagentStop as fallback") || strings.Contains(instruction, "then return that path") || strings.Contains(instruction, "runtime repair targeted commit") || strings.Contains(instruction, "PLAN_REPORT") {
				t.Fatalf("packet verifier TASK retained manual submission or omitted actual path: %s", instruction)
			}
			taskFound = true
		}
	}
	if !taskFound {
		t.Fatal("packet verifier TASK was not registered")
	}
	if strings.Contains(feedback, "spawn/resume") {
		t.Fatalf("pending registration prematurely requested native launch: %s", feedback)
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("verifier registration prepared a premature independent result: %v", err)
	}
	before = repairDispatchRuntimeBytes(t, root)
	if replay := repairVerifierDispatchSave(t, root, path, rows); !strings.Contains(replay, "without a Runtime write") {
		t.Fatalf("verifier replay did not reuse original facts: %s", replay)
	}
	repairDispatchAssertUnchanged(t, root, before)
	rows[0].AgentID = "different-verifier"
	if conflict := repairVerifierDispatchSave(t, root, path, rows); !strings.Contains(conflict, "conflict") || !strings.Contains(conflict, owner) {
		t.Fatalf("changed verifier silently replaced existing owner: %s", conflict)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestS9RepairVerifierDispatchQuestionnaireOriginalPrerequisitesAndRetry(t *testing.T) {
	root, snapshot, plan, _ := repairDispatchFixture(t)
	stageRepairVerifierDispatchContext(root, policy.Input{Event: "PreToolUse"}, snapshot)
	path := repairVerifierDispatchPaths(root, snapshot.State)[0]
	rows := []repairVerifierDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, IndependentVerification: true, AgentID: "independent-selected"}}
	before := repairDispatchRuntimeBytes(t, root)
	if feedback := repairVerifierDispatchSave(t, root, path, rows); !strings.Contains(feedback, "no recorded owner") {
		t.Fatalf("original missing repair-owner prerequisite was bypassed: %s", feedback)
	}
	repairDispatchAssertUnchanged(t, root, before)
	repairVerifierDispatchBuilder(t, root, plan.Assignments[0].AssignmentID, "builder-selected")
	before = repairDispatchRuntimeBytes(t, root)
	rows[0].AgentID = "builder-selected"
	if feedback := repairVerifierDispatchSave(t, root, path, rows); !strings.Contains(feedback, "verifier must be a different agent") {
		t.Fatalf("original independence consumer was bypassed: %s", feedback)
	}
	repairDispatchAssertUnchanged(t, root, before)
	rows[0].AgentID = "independent-selected"
	if feedback := repairVerifierDispatchSave(t, root, path, rows); !strings.Contains(feedback, "Registered independent verifier independent-selected") {
		t.Fatalf("same professional answer did not resume after original prerequisite: %s", feedback)
	}
	registered := repairDispatchSnapshot(t, root)
	answerBefore, _ := os.ReadFile(path)
	stageRepairVerifierDispatchContext(root, policy.Input{Event: "PreToolUse"}, registered)
	answerAfter, _ := os.ReadFile(path)
	if !bytes.Equal(answerBefore, answerAfter) {
		t.Fatal("verifier preparation overwrote Main's existing professional choice")
	}
}

func TestS9RepairVerifierDispatchQuestionnaireOriginalPhasesAndResponsibility(t *testing.T) {
	root, snapshot, _, _ := repairDispatchFixture(t)
	before := repairDispatchRuntimeBytes(t, root)
	path := repairVerifierDispatchPaths(root, snapshot.State)[0]
	// Exercise the missing-draft case after the normal entry prepared one.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, request := range []policy.Input{{Event: "PreToolUse", AgentID: "worker"}, {Event: "SubagentStart"}} {
		if context := stageRepairVerifierDispatchContext(root, request, snapshot); context != "" {
			t.Fatalf("Main verifier division delivered to Worker: %s", context)
		}
	}
	// Idle has no platform Main identity; only actual Main events receive this division.
	if got := stageRepairVerifierDispatchContext(root, policy.Input{Event: "Idle"}, snapshot); got != "" {
		t.Fatalf("unidentified Idle inherited Main verifier responsibility: %s", got)
	}
	for _, event := range []string{"PreCompact", "Stop", "SessionStart"} {
		context := stageRepairVerifierDispatchContext(root, policy.Input{Event: event}, snapshot)
		if !strings.Contains(context, path) || !strings.Contains(context, "must prepare this draft") {
			t.Fatalf("%s omitted exact preparation gap: %s", event, context)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("read-only/Worker events created verifier draft: %v", err)
	}
	for _, status := range []string{"planning", "reproducing", "targeted_reverification"} {
		repairPointerCLI(snapshot.State)["status"] = status
		if len(repairVerifierDispatchPaths(root, snapshot.State)) != 1 {
			t.Fatalf("original admitted phase %s lost verifier entry", status)
		}
	}
	for _, status := range []string{"repairing", "impact_reconciliation", "blocked", "ready_for_full_review"} {
		repairPointerCLI(snapshot.State)["status"] = status
		if len(repairVerifierDispatchPaths(root, snapshot.State)) != 0 {
			t.Fatalf("adapter invented verifier dispatch admission in %s", status)
		}
	}
	repairPointerCLI(snapshot.State)["status"] = "planning"
	mapFieldCLI(snapshot.State, "lifecycle")["state"] = "investigation"
	if len(repairVerifierDispatchPaths(root, snapshot.State)) != 0 || stageRepairVerifierDispatchContext(root, policy.Input{Event: "PreToolUse"}, snapshot) != "" {
		t.Fatal("verifier entry leaked into S8")
	}
	repairDispatchAssertUnchanged(t, root, before)
	if !repairVerifierDispatchHasProfessionalContent(path, []byte("{")) || repairVerifierDispatchHasProfessionalContent("builder-dispatch.json", []byte("{")) {
		t.Fatal("content detector swallowed invalid current content or claimed Builder answer")
	}
}
