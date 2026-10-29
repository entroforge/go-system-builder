package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func repairDispatchFixture(t *testing.T) (string, runtime.Snapshot, repair.RepairPlan, string) {
	t.Helper()
	root := repairLeadFreshRoot(t)
	writeRepairLeadFile(t, root, "docs/control/agent-protocol.md", "# protocol\n")
	writeRepairLeadFile(t, root, "agents/backend-builder.md", "# Backend Builder\n")
	writeRepairLeadFile(t, root, "agents/test-builder.md", "# Test Builder\n")
	// The explicit-save callback now issues the original packet immediately,
	// so this fixture needs the real approved Case and original Open/Compile
	// journal chain rather than a hand-built repair pointer.
	repairLeadApprovedCase(t, root, "investigation-case-dispatch-questionnaire", []map[string]any{
		{"id": "unit-first", "description": "restore first authority", "scope": []string{"internal/api/one.go"}, "assertion_ids": []string{"all"}},
		{"id": "unit-second", "description": "restore second authority", "scope": []string{"internal/api/two.go"}, "assertion_ids": []string{"all"}},
	})
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	_, _, _, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, SessionID: "repair-session-division", CreatedBy: "Main"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, plan, _, err := repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, PlanID: "repair-plan-division", CreatedBy: "Main"})
	if err != nil || len(plan.Assignments) != 2 {
		t.Fatalf("two original Assignments: %#v %v", plan, err)
	}
	if _, err := prepareRepairEntry(root, statePath, journalPath, snapshot.State); err != nil {
		t.Fatal(err)
	}
	path := repairDispatchPaths(root, snapshot.State)[0]
	return root, snapshot, plan, path
}

func repairDispatchSnapshot(t *testing.T, root string) runtime.Snapshot {
	t.Helper()
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func repairDispatchSave(t *testing.T, root, path string, rows []repairDispatchChoice) string {
	t.Helper()
	if err := persistRepairLeadQuestionnaire(path, rows); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	handled, code := runSavedRepairDispatch(root, repairLeadSaveInput(root, path, "Main"), &stdout, &stderr)
	if !handled || code != 0 {
		t.Fatalf("division save: handled=%t code=%d stdout=%s stderr=%s", handled, code, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func repairDispatchRuntimeBytes(t *testing.T, root string) [][]byte {
	t.Helper()
	var result [][]byte
	for _, rel := range []string{".claude/loop-state.json", ".claude/loop-events.jsonl"} {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		result = append(result, data)
	}
	return result
}

func repairDispatchAssertUnchanged(t *testing.T, root string, before [][]byte) {
	t.Helper()
	after := repairDispatchRuntimeBytes(t, root)
	for i := range before {
		if !bytes.Equal(before[i], after[i]) {
			t.Fatalf("Runtime file %d changed on a read-only replay", i)
		}
	}
}

func TestS9RepairDispatchQuestionnaireNormalPendingAndRealDispatch(t *testing.T) {
	root, snapshot, plan, path := repairDispatchFixture(t)
	snapshot.State["schema_version"] = "1.2.0"
	mapFieldCLI(snapshot.State, "configuration")["optimization"] = map[string]any{"packet": true}
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), snapshot.State)
	if _, err := prepareRepairEntry(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), snapshot.State); err != nil {
		t.Fatal(err)
	}
	snapshot = repairDispatchSnapshot(t, root)
	path = repairDispatchPaths(root, snapshot.State)[0]
	before := repairDispatchRuntimeBytes(t, root)
	context := stageRepairDispatchContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main"}, snapshot)
	contractPath := resolveRootPath(root, stringValue(repairPointerCLI(snapshot.State)["contract_ref"]))
	for _, want := range []string{path, contractPath, "awaiting Main", "backend-builder, frontend-builder or test-builder", "restore first authority", "native Agent", "engineering responsibility name", "check_location"} {
		if !strings.Contains(context, want) {
			t.Fatalf("normal empty-owner context lacks %q: %s", want, context)
		}
	}
	for _, redundant := range []string{"Original repair units:", "symptom_assertions:", "root_invariant_assertions:", "detection_gap_assertions:", "depends_on=", "resource_locks="} {
		if strings.Contains(context, redundant) {
			t.Fatalf("Main division reminder repeated full or empty source data %q: %s", redundant, context)
		}
	}
	withDependencies := plan.Assignments[0]
	withDependencies.DependsOn = []string{"unit-upstream"}
	withDependencies.ResourceLocks = []string{"authority-lock"}
	summary := repairDispatchAssignmentSummary(plan, withDependencies, "current-builder")
	for _, want := range []string{withDependencies.AssignmentID, "restore first authority", "current-builder", "depends_on=[unit-upstream]", "resource_locks=[authority-lock]"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("compact assignment omitted actual professional context %q: %s", want, summary)
		}
	}
	if strings.Contains(context, "restore the original repair dispatch") {
		t.Fatal("normal pending owner was presented as recovery")
	}
	var rows []repairDispatchChoice
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("check_location")) {
		t.Fatalf("new native division still asks for legacy check_location: %s", data)
	}
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) != 2 || repairDispatchHasProfessionalContent(path, data) {
		t.Fatalf("untouched division draft: %s %v", data, err)
	}
	if got := repairDispatchSave(t, root, path, rows); !strings.Contains(got, "normal pending draft") {
		t.Fatalf("blank draft was not treated as normal pending: %s", got)
	}
	repairDispatchAssertUnchanged(t, root, before)
	rows[0].RoleFamily = "backend-builder"
	feedback := repairDispatchSave(t, root, path, rows)
	registered := repairDispatchSnapshot(t, root)
	owners := stringMapCLI(repairPointerCLI(registered.State)["assignment_owners"])
	owner := owners[plan.Assignments[0].AssignmentID]
	if owner == "" || owners[plan.Assignments[1].AssignmentID] != "" {
		t.Fatalf("selected row must register while blank row stays pending: %#v", owners)
	}
	for _, want := range []string{owner, "TASK", "Manifest:", "platform", "private PlanReport output relative to the actual native worktree"} {
		if !strings.Contains(feedback, want) {
			t.Fatalf("real dispatch feedback lacks %q: %s", want, feedback)
		}
	}
	entities := mapFieldCLI(registered.State, "entities")
	if len(objectSliceAny(entities["teams"])) != 1 || len(objectSliceAny(entities["tasks"])) != 1 || len(objectSliceAny(entities["agents"])) != 1 {
		t.Fatalf("original dispatch did not create exactly one real Team/Task/Agent: %#v", entities)
	}
	team := objectSliceAny(entities["teams"])[0]
	task := objectSliceAny(entities["tasks"])[0]
	taskData, taskErr := os.ReadFile(resolveRootPath(root, stringValue(task["path"])))
	var taskDocument map[string]any
	if taskErr != nil || json.Unmarshal(taskData, &taskDocument) != nil {
		t.Fatalf("packet Builder TASK: %v", taskErr)
	}
	instruction := stringValue(taskDocument["instruction"]) + " " + stringValue(taskDocument["next_action"])
	if !strings.Contains(instruction, "before implementation") || !strings.Contains(instruction, "PreToolUse") || !strings.Contains(instruction, "PostToolUse") || !strings.Contains(instruction, "Stop/SubagentStop as fallback") || !strings.Contains(instruction, "native worktree isolation") || strings.Contains(instruction, "Return the exact brief") || strings.Contains(instruction, "PLAN_REPORT") || strings.Contains(instruction, "runtime repair plan-report") {
		t.Fatalf("packet Builder TASK retained manual/double plan submission: %s", instruction)
	}
	for _, ref := range []string{stringValue(team["manifest_ref"]), stringValue(task["path"])} {
		if _, err := os.Stat(resolveRootPath(root, ref)); err != nil || !strings.Contains(feedback, resolveRootPath(root, ref)) {
			t.Fatalf("actual artifact %s missing or omitted: %v", ref, err)
		}
	}
	answerBefore, _ := os.ReadFile(path)
	stageRepairDispatchContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main"}, registered)
	answerAfter, _ := os.ReadFile(path)
	if !bytes.Equal(answerBefore, answerAfter) {
		t.Fatal("Main preparation overwrote author selection")
	}
	before = repairDispatchRuntimeBytes(t, root)
	if got := repairDispatchSave(t, root, path, rows); !strings.Contains(got, "without a Runtime write") {
		t.Fatalf("same choice did not reuse original dispatch: %s", got)
	}
	repairDispatchAssertUnchanged(t, root, before)
	if after, err := os.ReadFile(resolveRootPath(root, stringValue(task["path"]))); err != nil || !bytes.Equal(after, taskData) {
		t.Fatalf("same choice rewrote its already issued TASK: %v", err)
	}
}

func TestS9RepairDispatchQuestionnairePartialRecoveryAndConflicts(t *testing.T) {
	root, snapshot, plan, path := repairDispatchFixture(t)
	stageRepairDispatchContext(root, policy.Input{Event: "PreToolUse"}, snapshot)
	rows := []repairDispatchChoice{
		{AssignmentID: plan.Assignments[0].AssignmentID, RoleFamily: "backend-builder", AgentID: "builder-selected"},
		{AssignmentID: plan.Assignments[1].AssignmentID, RoleFamily: "frontend-builder"},
	}
	feedback := repairDispatchSave(t, root, path, rows)
	partial := repairDispatchSnapshot(t, root)
	owners := stringMapCLI(repairPointerCLI(partial.State)["assignment_owners"])
	if owners[rows[0].AssignmentID] != "builder-selected" || owners[rows[1].AssignmentID] != "" || !strings.Contains(feedback, "did not register") {
		t.Fatalf("partial original registration lost facts: %#v %s", owners, feedback)
	}
	writeRepairLeadFile(t, root, "agents/frontend-builder.md", "# Frontend Builder\n")
	feedback = repairDispatchSave(t, root, path, rows)
	complete := repairDispatchSnapshot(t, root)
	// Only the remaining original Register and its PACKET-ISSUE may write;
	// the first person's already issued question remains a read-only replay.
	if complete.Revision != partial.Revision+2 || len(stringMapCLI(repairPointerCLI(complete.State)["assignment_owners"])) != 2 || !strings.Contains(feedback, "Existing original dispatch reused") {
		t.Fatalf("retry did not supplement only remaining dispatch: revision %d -> %d %s", partial.Revision, complete.Revision, feedback)
	}
	before := repairDispatchRuntimeBytes(t, root)
	for _, edit := range []struct{ name, role, agent, want string }{
		{"changed owner", "backend-builder", "builder-other", "conflict"},
		{"changed role", "test-builder", "builder-selected", "conflict"},
		{"invalid role", "investigator", "builder-selected", "role_family must"},
	} {
		t.Run(edit.name, func(t *testing.T) {
			changed := append([]repairDispatchChoice(nil), rows...)
			changed[0].RoleFamily, changed[0].AgentID = edit.role, edit.agent
			if got := repairDispatchSave(t, root, path, changed); !strings.Contains(got, edit.want) {
				t.Fatalf("changed selection not rejected clearly: %s", got)
			}
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
}

func TestS9RepairDispatchQuestionnaireResponsibilityAndS8Isolation(t *testing.T) {
	root, snapshot, _, path := repairDispatchFixture(t)
	// Entry preparation already created this draft. Remove it deliberately
	// to test that subsequent read-only Worker reminders do not recreate it.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	for _, request := range []policy.Input{{Event: "PreToolUse", AgentID: "builder-unselected"}, {Event: "SubagentStart"}} {
		if got := stageRepairDispatchContext(root, request, snapshot); got != "" {
			t.Fatalf("Main division delivered to a Worker: %s", got)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Worker reminder created Main division file: %v", err)
	}
	for _, event := range []string{"PreCompact", "Stop", "Idle"} {
		got := stageRepairDispatchContext(root, policy.Input{Event: event}, snapshot)
		if !strings.Contains(got, "not prepared yet") || !strings.Contains(got, path) {
			t.Fatalf("%s omitted accurate system preparation gap: %s", event, got)
		}
	}
	mapFieldCLI(snapshot.State, "lifecycle")["state"] = "investigation"
	mapFieldCLI(snapshot.State, "lifecycle")["phase"] = nil
	if got := stageRepairDispatchContext(root, policy.Input{Event: "PreToolUse"}, snapshot); got != "" || len(repairDispatchPaths(root, snapshot.State)) != 0 {
		t.Fatalf("S9 division leaked into S8: %s", got)
	}
	repairDispatchAssertUnchanged(t, root, before)
	if !repairDispatchHasProfessionalContent(path, []byte("{")) || repairDispatchHasProfessionalContent("hypotheses.json", []byte("{")) {
		t.Fatal("content detector swallowed malformed current content or claimed S8 content")
	}
}

func TestS9RepairDispatchExplicitSavePreparesNativeBriefAndPreservesOriginalSources(t *testing.T) {
	root, plan, _, _, path := repairExecutionPlanningFixture(t)
	rows := []repairDispatchChoice{
		{AssignmentID: plan.Assignments[0].AssignmentID, RoleFamily: "backend-builder"},
		{AssignmentID: plan.Assignments[1].AssignmentID, RoleFamily: "backend-builder"},
	}
	feedback := repairDispatchSave(t, root, path, rows[:1])
	first := repairDispatchSnapshot(t, root)
	packets, err := runtime.CurrentRepairPlanReportPackets(root, first.State, "")
	if err != nil || len(packets) != 1 || first.State["workspace"] != nil {
		t.Fatalf("same explicit division save did not issue the first original PlanReport without an execution: packets=%d err=%v feedback=%s", len(packets), err, feedback)
	}
	firstPacket := packets[0]
	brief, _, _ := nativeS9Paths(firstPacket)
	firstAuthorPlan := nativeS9TestPlanPath(t, root, firstPacket)
	for _, want := range []string{firstPacket.Binding.OwnerAgentID, resolveRootPath(root, firstPacket.ViewPath), resolveRootPath(root, brief), firstAuthorPlan, "private PlanReport output relative to the actual native worktree", "exact S9 brief", "worktree isolation", "SubagentStop", "No workspace prepare/launch", "Main does not write professional answers"} {
		if !strings.Contains(feedback, want) {
			t.Fatalf("same-callback native brief lacks %q: %s", want, feedback)
		}
	}
	// A later passive script may register the second professional choice but
	// cannot issue its PlanReport or prepare a Worker execution.
	if err := persistRepairLeadQuestionnaire(path, rows); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if handled, code := runObservedRepairDispatch(root, repairLeadSaveInput(root, path, "Main"), &stdout, &stderr); !handled || code != 0 {
		t.Fatalf("passive original dispatch: handled=%t code=%d %s %s", handled, code, stdout.String(), stderr.String())
	}
	passive := repairDispatchSnapshot(t, root)
	passivePackets, err := runtime.CurrentRepairPlanReportPackets(root, passive.State, "")
	if err != nil || len(passivePackets) != 1 || len(stringMapCLI(repairPointerCLI(passive.State)["assignment_owners"])) != 2 || passive.State["workspace"] != nil {
		t.Fatalf("passive observation issued or lost a responsibility: packets=%d err=%v state=%#v", len(passivePackets), err, repairPointerCLI(passive.State))
	}
	feedback = repairDispatchSave(t, root, path, rows)
	complete := repairDispatchSnapshot(t, root)
	packets, err = runtime.CurrentRepairPlanReportPackets(root, complete.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("explicit same choice did not fill only the missing person's question: packets=%d err=%v %s", len(packets), err, feedback)
	}
	originalFiles := map[string][]byte{}
	for _, packet := range packets {
		if err := runtime.VerifyRepairPlanReportPacketIssue(root, complete.State, packet); err != nil {
			t.Fatalf("question did not use the original PACKET-ISSUE Writer: %v", err)
		}
		if err := runtime.VerifyRepairPlanReportCurrent(root, complete.State, packet); err != nil {
			t.Fatalf("original source binding differs: %v", err)
		}
		// Main retains the original issued source/draft. The actual isolated
		// Worker receives its private output relative to its own worktree.
		for _, ref := range []string{packet.ViewPath, packet.AnswerPath} {
			if _, err := os.Stat(resolveRootPath(root, ref)); err != nil {
				t.Fatalf("original issued metadata artifact missing: %s %v", ref, err)
			}
		}
		brief, _, _ := nativeS9Paths(packet)
		authorPlan := nativeS9TestPlanPath(t, root, packet)
		for _, ref := range []string{resolveRootPath(root, packet.ViewPath), resolveRootPath(root, brief), authorPlan} {
			if !strings.Contains(feedback, ref) {
				t.Fatalf("native handoff omitted the original precise reference %s: %s", ref, feedback)
			}
		}
	}
	for _, kind := range []string{"teams", "tasks"} {
		for _, entity := range objectSliceAny(mapFieldCLI(complete.State, "entities")[kind]) {
			ref, sha := stringValue(entity["manifest_ref"]), stringValue(entity["manifest_sha256"])
			if kind == "tasks" {
				ref, sha = stringValue(entity["path"]), stringValue(entity["sha256"])
			}
			data, err := os.ReadFile(resolveRootPath(root, ref))
			if err != nil || repairLeadDigest(data) != sha {
				t.Fatalf("issuance changed the original frozen %s bytes: %s %v", kind, ref, err)
			}
			originalFiles[ref] = data
		}
	}
	// Restore a missing issued view without replacing the Builder's authored
	// answer or writing another issuance/dispatch event.
	answerPath := resolveRootPath(root, firstPacket.AnswerPath)
	answerData, _ := os.ReadFile(answerPath)
	var draft map[string]any
	if err := json.Unmarshal(answerData, &draft); err != nil {
		t.Fatal(err)
	}
	mapFieldCLI(draft, "answer")["plan"] = "Builder-owned draft preserved during exact issue replay"
	answerData, _ = json.MarshalIndent(draft, "", "  ")
	if err := os.WriteFile(answerPath, answerData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(resolveRootPath(root, firstPacket.ViewPath)); err != nil {
		t.Fatal(err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	repairDispatchSave(t, root, path, rows)
	repairDispatchAssertUnchanged(t, root, before)
	retainedAnswer, _ := os.ReadFile(answerPath)
	if !bytes.Equal(answerData, retainedAnswer) {
		t.Fatal("same choice replaced the Builder-owned professional answer")
	}
	for ref, original := range originalFiles {
		current, err := os.ReadFile(resolveRootPath(root, ref))
		if err != nil || !bytes.Equal(current, original) {
			t.Fatalf("callback rewrote original frozen dispatch artifact %s: %v", ref, err)
		}
	}
}
