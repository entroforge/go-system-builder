package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

func nativeDeliveryFixture(t *testing.T) (string, string, assignmentrecord.Record) {
	return nativeDeliveryFixtureVersion(t, "1.2.0")
}

func nativeDeliveryFixtureVersion(t *testing.T, version string) (string, string, assignmentrecord.Record) {
	t.Helper()
	root, task, _ := newS6RecordContextFixtureWithReadSources(t, map[string][]byte{"go.mod": []byte("module native.fixture\n\ngo 1.26\n"), "src/pkg/input_test.go": []byte("package pkg\nimport \"testing\"\nfunc TestPackage(t *testing.T) {}\n")})
	if version == "1.3.0" {
		state := stageSnapshot(t, root).State
		state["schema_version"] = version
		stageJSON(t, root, ".claude/loop-state.json", state)
	}
	output := stageCLI(t, "s6", "prepare", "--root", root, "--task", task, "--role", "backend-builder")
	if strings.Contains(output, "workspace launch") || !strings.Contains(output, "native Agent") {
		t.Fatal(output)
	}
	snap := stageSnapshot(t, root)
	r, found, err := assignmentrecord.ForOwner(snap.State, "native-"+task)
	if err != nil || !found || r.Contract != assignmentrecord.NativeDeliveryContract {
		t.Fatalf("native registration: %+v %v", r, err)
	}
	for _, raw := range snap.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == r.OwnerAgentID && row["activation_ref"] != nil {
			t.Fatal("native registration staged activation")
		}
		if row["id"] == r.OwnerAgentID && workstate.AgentActivity(row) != "queued" {
			t.Fatal("preparation fabricated a native start/read fact")
		}
	}
	source := filepath.Join(t.TempDir(), "native-worktree")
	if _, err := workspace.Git(t.Context(), root, "worktree", "add", "-b", "native/delivery", source, "HEAD"); err != nil {
		t.Fatal(err)
	}
	return root, source, r
}

func nativeEvent(t *testing.T, root, source, event, text string, active bool) (int, string) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"hook_event_name": event, "session_id": "actual-parent-session", "agent_id": "actual-opaque-" + filepath.Base(source), "agent_type": "backend-builder", "cwd": source, "last_assistant_message": text, "stop_hook_active": active, "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(source, "src/pkg/proof.txt")}})
	var out, err bytes.Buffer
	code := cli.Run([]string{"hook", "--event", event, "--root", root}, bytes.NewReader(data), &out, &err)
	return code, out.String() + err.String()
}

func nativeAnswer(t *testing.T, source string, r assignmentrecord.Record) {
	t.Helper()
	_, plan, result := assignmentrecord.NativePaths(r)
	assertion, oracle := "The reviewed TASK delivery is implemented.", "Inspect the committed proof and run the TASK checks."
	stageJSON(t, source, plan, map[string]any{"objective": "Implement the reviewed TASK and run real self-checks.", "planned_paths": []string{"src/pkg/proof.txt"}, "assertion_checks": []any{map[string]any{"assertion": assertion, "oracle": oracle, "done_when_indexes": []int{0}}}, "risks_blockers": []string{}})
	stageJSON(t, source, result, map[string]any{"summary": "Implemented the reviewed change; go test ./src/pkg executed successfully.", "assertion_evidence": []any{map[string]any{"assertion": assertion, "oracle": oracle, "evidence_refs": []string{"src/pkg/proof.txt"}}}, "risks_blockers": []string{}, "scope_deviations": []string{}, "closing_assessment": map[string]any{"disposition": "complete", "conditions": []any{map[string]any{"done_when_index": 0, "judgment": "satisfied", "plan_assertion_indexes": []int{0}, "rationale": "The committed proof and actual TASK self-check cover this condition."}}}})
}

func nativeCommit(t *testing.T, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(source, "src/pkg/proof.txt"), []byte("Actual implementation evidence.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Exercise real checks and Git, not authored PASS receipts.
	check := exec.CommandContext(t.Context(), "go", "test", "./src/pkg")
	check.Dir = source
	check.Env = append(os.Environ(), "GOWORK=off")
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("Worker self-check: %v %s", err, output)
	}
	if _, err := workspace.Git(t.Context(), source, "add", "src/pkg/proof.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), source, "commit", "-m", "Native TASK delivery"); err != nil {
		t.Fatal(err)
	}
}

func TestNativeS6StartStopReceivesWithoutActivationOrSaveHook(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	brief, _, result := assignmentrecord.NativePaths(r)
	mainInput, _ := json.Marshal(map[string]any{"hook_event_name": "SessionStart", "session_id": "native-main-session", "cwd": root, "source": "startup"})
	var mainOut, mainErr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "SessionStart", "--root", root}, bytes.NewReader(mainInput), &mainOut, &mainErr); code != 0 {
		t.Fatalf("native Main context: %d %s %s", code, &mainOut, &mainErr)
	}
	for _, stale := range []string{"Dispatch budget unknown", "S6 current Main action: wait_or_observe", "questionnaires using the paths supplied by the listener"} {
		if strings.Contains(mainOut.String(), stale) {
			t.Fatalf("native Main reintroduced a legacy dispatch protocol: %s", &mainOut)
		}
	}
	before := stageSnapshot(t, root)
	if code, output := nativeEvent(t, source, source, "SubagentStart", filepath.Join(root, brief), false); code != 0 || !strings.Contains(output, "without activation") {
		t.Fatalf("start %d %s", code, output)
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("Start mutated execution state")
	}
	nativeCommit(t, source)
	nativeAnswer(t, source, r)
	if code, output := nativeEvent(t, source, source, "PostToolUse", "", false); code != 0 || output != "" {
		t.Fatalf("ordinary tool managed native execution: %d %s", code, output)
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("save hook received native delivery")
	}
	text := filepath.Join(root, brief) + "\n" + filepath.Join(source, result)
	code, stopOutput := nativeEvent(t, root, source, "SubagentStop", text, false)
	if code != 0 {
		t.Fatalf("native end %d %s", code, stopOutput)
	}
	accepted := stageSnapshot(t, root)
	if accepted.Revision <= before.Revision {
		t.Fatalf("end did not receive delivery: %s", stopOutput)
	}
	for _, raw := range accepted.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == r.OwnerAgentID {
			if row["activation_ref"] != nil || row["readback_ref"] != nil || row["completion_reported_ref"] == nil {
				t.Fatalf("fabricated activation or missing result: %v", row)
			}
		}
	}
	if code, output := nativeEvent(t, root, source, "SubagentStop", text, false); code != 0 {
		t.Fatalf("replay %d %s", code, output)
	}
	if stageSnapshot(t, root).Revision != accepted.Revision {
		t.Fatal("repeated end changed Runtime")
	}
	var integrateOut, integrateErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "task-integrate", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID}, nil, &integrateOut, &integrateErr); code != 0 {
		t.Fatalf("Main integration: %d %s %s", code, &integrateOut, &integrateErr)
	}
	if strings.Contains(integrateOut.String(), "retry runtime task-integrate to finish acknowledgement") {
		t.Fatalf("completed native integration still asks for missing ACK: %s", &integrateOut)
	}
	cp, found, err := integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, r.Reference()))
	if err != nil || !found || cp.State != integration.StateComplete || cp.CanonicalMainFacts == nil || cp.VerifiedAt == "" {
		t.Fatalf("Main did not verify and clean up: %+v %v / %s %s", cp, err, &integrateOut, &integrateErr)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("verified native worktree was not removed: %v", err)
	}
	receipt := stageCLI(t, "runtime", "task-receipt", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID)
	if !strings.Contains(receipt, `"integrated":true`) || !strings.Contains(receipt, `"work_complete":true`) {
		t.Fatalf("Main receipt incomplete: %s", receipt)
	}
	closed := stageSnapshot(t, root)
	if code, out := nativeEvent(t, root, root, "SubagentStop", text, false); code != 0 {
		t.Fatalf("late Stop reopened cleaned delivery: %d %s", code, out)
	}
	if stageSnapshot(t, root).Revision != closed.Revision {
		t.Fatal("late Stop changed closed responsibility")
	}

}

func TestNativeS6ProfessionalFieldFeedbackTargetsOriginalSchema(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	nativeCommit(t, source)
	nativeAnswer(t, source, r)
	brief, _, result := assignmentrecord.NativePaths(r)
	data, err := os.ReadFile(filepath.Join(source, result))
	if err != nil {
		t.Fatal(err)
	}
	var answer map[string]any
	if err := json.Unmarshal(data, &answer); err != nil {
		t.Fatal(err)
	}
	evidence := answer["assertion_evidence"].([]any)[0].(map[string]any)
	evidence["plan_assertion"] = evidence["assertion"]
	delete(evidence, "assertion")
	stageJSON(t, source, result, answer)
	before := stageSnapshot(t, root)
	text := filepath.Join(root, brief) + "\n" + filepath.Join(source, result)
	code, output := nativeEvent(t, root, source, "SubagentStop", text, false)
	if code != 2 || !strings.Contains(output, "professional Result fields") || !strings.Contains(output, filepath.Join(source, result)) || !strings.Contains(output, "assertion_evidence/0") || strings.Contains(output, "closest to branch") {
		t.Fatalf("unhelpful professional feedback: %d %s", code, output)
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("invalid professional Result changed Runtime")
	}
	// The person's corrected professional answer can be received on the
	// native continuation; no machine envelope is repaired by the caller.
	nativeAnswer(t, source, r)
	data, err = os.ReadFile(filepath.Join(source, result))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		t.Fatal(err)
	}
	answer["assertion_evidence"].([]any)[0].(map[string]any)["evidence_refs"] = []string{"go test ./src/pkg -> PASS"}
	stageJSON(t, source, result, answer)
	if code, output := nativeEvent(t, root, source, "SubagentStop", text, true); code != 0 || !strings.Contains(output, "must be exact existing worktree-relative files") {
		t.Fatalf("unhelpful or unbounded evidence feedback: %d %s", code, output)
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("command prose was accepted as file evidence")
	}
	nativeAnswer(t, source, r)
	if code, output := nativeEvent(t, root, source, "SubagentStop", text, true); code != 0 {
		t.Fatalf("corrected native delivery: %d %s", code, output)
	}
	if stageSnapshot(t, root).Revision <= before.Revision {
		t.Fatal("corrected native delivery was not received")
	}
}

func TestNativeS6PlanAndResultKeepTheirOwnFields(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	brief, plan, result := assignmentrecord.NativePaths(r)
	briefData, err := os.ReadFile(filepath.Join(root, brief))
	if err != nil {
		t.Fatal(err)
	}
	// Read the actual two professional shapes delivered by normal prepare,
	// without constructing a machine envelope or looking up metadata.
	for _, section := range []struct {
		label, path string
	}{{"Plan", plan}, {"Result", result}} {
		_, tail, ok := strings.Cut(string(briefData), section.label+": "+section.path+"\n```json\n")
		if !ok {
			t.Fatalf("missing exact %s question in brief", section.label)
		}
		shape, _, _ := strings.Cut(tail, "\n```")
		var fields map[string]any
		if err := json.Unmarshal([]byte(shape), &fields); err != nil {
			t.Fatalf("%s shape: %v", section.label, err)
		}
		_, hasDependencies := fields["dependencies"]
		if hasDependencies {
			t.Fatalf("%s dependencies field = %t", section.label, hasDependencies)
		}
	}
	_, resultInstructions, _ := strings.Cut(string(briefData), "Result: "+result)
	if !strings.Contains(resultInstructions, "Result has no dependencies field") || strings.Contains(resultInstructions, "dependencies, risks_blockers and scope_deviations") {
		t.Fatalf("Result instructions mix Plan fields: %s", resultInstructions)
	}
	nativeCommit(t, source)
	before := stageSnapshot(t, root)
	text := filepath.Join(root, brief) + "\n" + filepath.Join(source, result)
	for _, invalid := range []struct{ label, path, field string }{{"Plan", plan, "runtime_id"}, {"Result", result, "dependencies"}} {
		nativeAnswer(t, source, r)
		data, err := os.ReadFile(filepath.Join(source, invalid.path))
		if err != nil {
			t.Fatal(err)
		}
		var answer map[string]any
		if err := json.Unmarshal(data, &answer); err != nil {
			t.Fatal(err)
		}
		answer[invalid.field] = []string{}
		stageJSON(t, source, invalid.path, answer)
		code, output := nativeEvent(t, root, source, "SubagentStop", text, false)
		for _, want := range []string{"professional " + invalid.label, filepath.Join(source, invalid.path), "unsupported field \"" + invalid.field + "\""} {
			if code != 2 || !strings.Contains(output, want) || strings.Contains(output, "machine field") {
				t.Fatalf("missing precise field feedback %q: %d %s", want, code, output)
			}
		}
		if stageSnapshot(t, root).Revision != before.Revision {
			t.Fatal("invalid field changed Runtime")
		}
	}
	// Old explicit dependency fields still use the original exact-set check.
	nativeAnswer(t, source, r)
	privateData, err := os.ReadFile(filepath.Join(source, plan))
	if err != nil {
		t.Fatal(err)
	}
	var explicit map[string]any
	if err := json.Unmarshal(privateData, &explicit); err != nil {
		t.Fatal(err)
	}
	explicit["dependencies"] = []string{"TASK-not-a-registered-assignment"}
	stageJSON(t, source, plan, explicit)
	if _, output := nativeEvent(t, root, source, "SubagentStop", text, false); !strings.Contains(output, "dependencies differ from the canonical Assignment") {
		t.Fatalf("wrong explicit dependency was not rejected: %s", output)
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("wrong explicit dependency changed Runtime")
	}
	nativeAnswer(t, source, r)
	if code, output := nativeEvent(t, root, source, "SubagentStop", text, false); code != 0 {
		t.Fatalf("corrected original professional files: %d %s", code, output)
	}
	if stageSnapshot(t, root).Revision <= before.Revision {
		t.Fatal("corrected professional files were not received")
	}
	privateData, err = os.ReadFile(filepath.Join(source, plan))
	if err != nil {
		t.Fatal(err)
	}
	var privatePlan map[string]any
	if err := json.Unmarshal(privateData, &privatePlan); err != nil {
		t.Fatal(err)
	}
	if _, hasDependencies := privatePlan["dependencies"]; hasDependencies {
		t.Fatal("normal private answer copied system-owned dependencies")
	}
	canonicalRef := filepath.Join(filepath.Dir(plan), "plan-"+workpacket.Digest(privateData)+".json")
	canonicalData, err := os.ReadFile(filepath.Join(root, canonicalRef))
	if err != nil {
		t.Fatal(err)
	}
	var canonical struct {
		Dependencies []string `json:"dependencies"`
	}
	if err := json.Unmarshal(canonicalData, &canonical); err != nil {
		t.Fatal(err)
	}
	// Normal native prepare owns one TASK per workgroup; its TASK prerequisites
	// are satisfied before registration, so the local Assignment set is empty.
	if canonical.Dependencies == nil || len(canonical.Dependencies) != 0 || len(r.DependsOn) != 0 {
		t.Fatalf("registered dependencies were not automatically bound: %#v / %#v", canonical.Dependencies, r.DependsOn)
	}
}

func TestNativeS6MissingDeliveryHasBoundedFeedbackAndNoRoleGuess(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	before := stageSnapshot(t, root)
	if code, _ := nativeEvent(t, root, source, "SubagentStop", "Finished backend work.", false); code != 0 {
		t.Fatal("role-only end was guessed")
	}
	brief, _, _ := assignmentrecord.NativePaths(r)
	text := filepath.Join(root, brief)
	if code, _ := nativeEvent(t, root, source, "SubagentStop", text, false); code != 2 {
		t.Fatal("exact missing delivery must name its gap")
	}
	if code, _ := nativeEvent(t, root, source, "SubagentStop", text, true); code != 0 {
		t.Fatal("Stop feedback loops indefinitely")
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("missing delivery fabricated completion")
	}
}

func TestNativeS6UnifiedReworkAndRecoveryUseOriginalRecordWriter(t *testing.T) {
	root, source, r := nativeDeliveryFixtureVersion(t, "1.3.0")
	before := stageSnapshot(t, root)
	stageCLI(t, "s6", "prepare", "--root", root, "--task", r.Task.ID, "--role", r.RoleFamily)
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("recovery re-registered responsibility")
	}
	nativeCommit(t, source)
	nativeAnswer(t, source, r)
	brief, _, result := assignmentrecord.NativePaths(r)
	text := filepath.Join(root, brief) + "\n" + filepath.Join(source, result)
	if code, out := nativeEvent(t, source, source, "SubagentStop", text, false); code != 0 {
		t.Fatalf("unified Stop: %d %s", code, out)
	}
	dirty := filepath.Join(source, "src/pkg/unfinished.txt")
	if err := os.WriteFile(dirty, []byte("preserve uncommitted work"), 0600); err != nil {
		t.Fatal(err)
	}
	stageCLI(t, "runtime", "task-integrate", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID)
	cp, found, err := integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, r.Reference()))
	if err != nil || !found || cp.State != integration.StatePreserved {
		t.Fatalf("expected original dirty checkpoint, got %+v %v", cp, err)
	}
	args := []string{"s6", "prepare", "--root", root, "--task", r.Task.ID, "--role", r.RoleFamily, "--revise-assignment", r.AssignmentID, "--revision-intent-key", "native-rework-1"}
	output := stageCLI(t, args...)
	if strings.Contains(output, "workspace launch") || !strings.Contains(output, "native Agent") {
		t.Fatal(output)
	}
	revised := stageSnapshot(t, root)
	records, err := assignmentrecord.RecordsFromState(revised.State)
	if err != nil {
		t.Fatal(err)
	}
	current, found, err := assignmentrecord.FindRecord(records, r.RuntimeID, r.BaselineGeneration, r.AssignmentID)
	if err != nil || !found || current.Revision != 2 || current.Contract != assignmentrecord.NativeDeliveryContract || current.OwnerAgentID == r.OwnerAgentID {
		t.Fatalf("revision bypassed original writer: %+v %v", current, err)
	}
	for _, raw := range revised.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == current.OwnerAgentID && row["activation_ref"] != nil {
			t.Fatal("revision staged activation")
		}
	}
	stageCLI(t, args...)
	if stageSnapshot(t, root).Revision != revised.Revision {
		t.Fatal("revision replay issued another responsibility")
	}
	if _, err := os.Stat(dirty); err != nil {
		t.Fatalf("rework discarded original source: %v", err)
	}
	if code, _ := nativeEvent(t, root, source, "SubagentStop", text, true); code != 0 {
		t.Fatal("historical Worker could not return")
	}
	if stageSnapshot(t, root).Revision != revised.Revision {
		t.Fatal("historical Stop acquired current authority")
	}
}

func TestNativeS6BlockerWithoutPlanCanReturnWithoutCompletion(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	brief, _, result := assignmentrecord.NativePaths(r)
	stageJSON(t, source, result, map[string]any{"summary": "Cannot access required input; Main must resolve the missing dependency.", "risks_blockers": []string{"Required external fixture is unavailable."}, "closing_assessment": map[string]any{"disposition": "blocked"}})
	before := stageSnapshot(t, root)
	if code, out := nativeEvent(t, source, source, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(source, result), false); code != 0 || !strings.Contains(out, "no completion receipt") {
		t.Fatalf("blocked handoff: %d %s", code, out)
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("blocker fabricated completed delivery")
	}
}

func TestNativeS6HandbackUsesInitialTaskAndActualArtifacts(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	nativeCommit(t, source)
	nativeAnswer(t, source, r)
	brief, _, result := assignmentrecord.NativePaths(r)
	transcript := filepath.Join(t.TempDir(), "native.jsonl")
	initial, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": filepath.Join(root, brief) + "\n" + filepath.Join(source, result)}}}})
	if err := os.WriteFile(transcript, append(initial, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": "opaque-handback-worker", "agent_type": "backend-builder", "session_id": "actual-native-session", "cwd": root, "agent_transcript_path": transcript, "last_assistant_message": "Handback complete.", "stop_hook_active": false})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "SubagentStop", "--root", root}, bytes.NewReader(payload), &out, &diagnostic); code != 0 {
		t.Fatalf("handback: %d %s %s", code, &out, &diagnostic)
	}
	snap := stageSnapshot(t, root)
	for _, raw := range snap.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == r.OwnerAgentID && row["completion_reported_ref"] == nil {
			t.Fatal("closing text was mistaken for a professional report")
		}
	}
	// A real post-report commit must not borrow the old accepted delivery.
	if err := os.WriteFile(filepath.Join(source, "src/pkg/proof.txt"), []byte("unreported new source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), source, "add", "src/pkg/proof.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), source, "commit", "-m", "Unreported source after Stop"); err != nil {
		t.Fatal(err)
	}
	mainBefore, err := workspace.Git(t.Context(), root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	blocked := stageCLI(t, "runtime", "task-integrate", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID)
	if !strings.Contains(blocked, "changed after accepted Result") {
		t.Fatalf("unreported source borrowed receipt: %s", blocked)
	}
	mainAfter, err := workspace.Git(t.Context(), root, "rev-parse", "HEAD")
	if err != nil || mainBefore != mainAfter {
		t.Fatal("unreported source was merged")
	}

}

func TestNativeS6SameRoleConcurrentWorktreesDoNotBorrowDeliveries(t *testing.T) {
	root, task, taskPath := newS6RecordContextFixtureWithReadSources(t, map[string][]byte{"go.mod": []byte("module native.fixture\n\ngo 1.26\n")})
	const second = "TASK-039-02"
	taskBytes, err := os.ReadFile(filepath.Join(root, taskPath))
	if err != nil {
		t.Fatal(err)
	}
	secondBytes := []byte(strings.ReplaceAll(strings.ReplaceAll(string(taskBytes), task, second), "src/pkg", "src/other"))
	secondPath := "docs/dev/tasks/" + second + ".md"
	if err := os.WriteFile(filepath.Join(root, secondPath), secondBytes, 0600); err != nil {
		t.Fatal(err)
	}
	planPath := "docs/dev/tasks/index-REQ-039.md"
	plan, err := os.ReadFile(filepath.Join(root, planPath))
	if err != nil {
		t.Fatal(err)
	}
	plan = append(plan, []byte("\n- [ ] [second independent TASK](TASK-039-02.md)\n")...)
	if err := os.WriteFile(filepath.Join(root, planPath), plan, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src/other"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src/other/input.go"), []byte("package other\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), root, "add", secondPath, planPath, "src/other/input.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(t.Context(), root, "commit", "-m", "Reviewed independent TASK batch"); err != nil {
		t.Fatal(err)
	}
	state := stageSnapshot(t, root).State
	docs := state["documents"].([]any)
	for _, raw := range docs {
		row := raw.(map[string]any)
		if row["path"] == planPath {
			row["sha256"] = workpacket.Digest(plan)
		}
	}
	state["documents"] = append(docs, s6RecordContextDocument(second, "task", secondPath, "v1.0.0", secondBytes))
	stageJSON(t, root, ".claude/loop-state.json", state)
	for _, id := range []string{task, second} {
		stageCLI(t, "s6", "prepare", "--root", root, "--task", id, "--role", "backend-builder")
	}
	snap := stageSnapshot(t, root)
	first, found, err := assignmentrecord.ForOwner(snap.State, "native-"+task)
	if err != nil || !found {
		t.Fatal(err)
	}
	other, found, err := assignmentrecord.ForOwner(snap.State, "native-"+second)
	if err != nil || !found {
		t.Fatal(err)
	}
	source1, source2 := filepath.Join(t.TempDir(), "worker-one"), filepath.Join(t.TempDir(), "worker-two")
	for i, source := range []string{source1, source2} {
		branch := []string{"native/one", "native/two"}[i]
		if _, err := workspace.Git(t.Context(), root, "worktree", "add", "-b", branch, source, "HEAD"); err != nil {
			t.Fatal(err)
		}
		if code, out := nativeEvent(t, source, source, "SubagentStart", "", false); code != 0 {
			t.Fatalf("Start: %s", out)
		}
	}
	if stageSnapshot(t, root).Revision != snap.Revision {
		t.Fatal("same-role Start guessed or mutated a task")
	}
	nativeCommit(t, source1)
	nativeAnswer(t, source1, first)
	brief1, _, result1 := assignmentrecord.NativePaths(first)
	brief2, _, _ := assignmentrecord.NativePaths(other)
	for _, event := range []string{"SubagentStart", "SubagentStop"} {
		payload, _ := json.Marshal(map[string]any{"hook_event_name": event, "cwd": source1, "agent_id": first.OwnerAgentID, "agent_type": "backend-builder", "last_assistant_message": filepath.Join(root, brief2)})
		var output, diagnostic bytes.Buffer
		code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(payload), &output, &diagnostic)
		if code != 0 || !strings.Contains(output.String()+diagnostic.String(), "responsibility mismatch") || strings.Contains(output.String(), "commit and save Result") {
			t.Fatalf("known owner adopted another exact responsibility: %d %s %s", code, &output, &diagnostic)
		}
	}
	for _, brief := range []string{brief1, brief2} {
		// Inspect each recorded diagnostic without claiming that both
		// responsibilities have returned from the platform.
		context := mainNativeReturnResponse(t, root, filepath.Join(root, brief), map[string]any{"status": "unknown"})
		if !strings.Contains(context, "responsibility mismatch") || !strings.Contains(context, first.OwnerAgentID) || !strings.Contains(context, filepath.Join(root, brief1)) || !strings.Contains(context, filepath.Join(root, brief2)) {
			t.Fatalf("Main lost owner/brief mismatch: %s", context)
		}
	}
	if stageSnapshot(t, root).Revision != snap.Revision {
		t.Fatal("owner/brief mismatch received another responsibility")
	}
	if code, _ := nativeEvent(t, root, source2, "SubagentStop", "backend-builder completed", false); code != 0 {
		t.Fatal("role-only stop was guessed")
	}
	if code, out := nativeEvent(t, root, source1, "SubagentStart", filepath.Join(root, brief1)+"\n"+filepath.Join(root, brief2), false); code != 0 || strings.Contains(out, "commit and save Result") {
		t.Fatalf("ambiguous Start adopted a queued responsibility: %d %s", code, out)
	}
	if code, out := nativeEvent(t, root, source1, "SubagentStop", filepath.Join(root, brief1)+"\n"+filepath.Join(root, brief2), false); code != 0 || !strings.Contains(out, "multiple exact task briefs") {
		t.Fatalf("ambiguous task: %d %s", code, out)
	}
	if stageSnapshot(t, root).Revision != snap.Revision {
		t.Fatal("ambiguous or role-only return received a delivery")
	}
	if code, out := nativeEvent(t, source1, source1, "SubagentStop", filepath.Join(root, brief1)+"\n"+filepath.Join(source1, result1), false); code != 0 {
		t.Fatalf("exact delivery: %d %s", code, out)
	}
	accepted := stageSnapshot(t, root)
	for _, raw := range accepted.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == other.OwnerAgentID && row["completion_reported_ref"] != nil {
			t.Fatal("same-role sibling borrowed first delivery")
		}
	}
}

func TestNativeS6PrepareResumesOriginalDraftAfterReceiverFailure(t *testing.T) {
	root, task, _ := newS6RecordContextFixture(t)
	path := filepath.Join(root, ".claude/skills/agent-dispatch/SKILL.md")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(original), "description: Use when", "description: invalid fixture", 1)
	if err := os.WriteFile(path, []byte(broken), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"s6", "prepare", "--root", root, "--task", task, "--role", "backend-builder"}
	var out, diagnostic bytes.Buffer
	if code := cli.Run(args, nil, &out, &diagnostic); code == 0 {
		t.Fatal("invalid installed Skill unexpectedly registered")
	}
	draft := filepath.Join(root, ".claude/workgroups/REQ-039", task, "manifest.draft.json")
	prepared, err := os.ReadFile(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	stageCLI(t, args...)
	after, err := os.ReadFile(draft)
	if err != nil || !bytes.Equal(prepared, after) {
		t.Fatal("recovery overwrote original preparation")
	}
	if r, found, err := assignmentrecord.ForOwner(stageSnapshot(t, root).State, "native-"+task); err != nil || !found || r.Contract != assignmentrecord.NativeDeliveryContract {
		t.Fatalf("original preparation was not resumed: %+v %v", r, err)
	}
}

func TestNativeS6RecoveredGitDeliveryDoesNotNeedAuthoringCaches(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	nativeCommit(t, source)
	nativeAnswer(t, source, r)
	brief, plan, result := assignmentrecord.NativePaths(r)
	if code, out := nativeEvent(t, root, source, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(source, result), false); code != 0 {
		t.Fatalf("original Stop: %d %s", code, out)
	}
	head, err := workspace.Git(t.Context(), source, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	branch, err := workspace.Git(t.Context(), source, "branch", "--show-current")
	if err != nil {
		t.Fatal(err)
	}
	// The platform may remove its execution checkout. Main recovers only
	// the already returned Git source; no Agent/bootstrap/session is created.
	if _, err := workspace.Git(t.Context(), root, "worktree", "remove", "--force", source); err != nil {
		t.Fatal(err)
	}
	beforeState := mustStageFile(t, filepath.Join(root, ".claude/loop-state.json"))
	beforeJournal := mustStageFile(t, filepath.Join(root, ".claude/loop-events.jsonl"))
	var missingOut, missingErr bytes.Buffer
	code := cli.Run([]string{"runtime", "task-integrate", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID}, nil, &missingOut, &missingErr)
	diagnostic := missingOut.String() + missingErr.String()
	// task-integrate represents a normal domain blocker in its JSON result;
	// its process exit code alone is not a completion verdict.
	if code != 0 || !strings.Contains(missingOut.String(), `"blocked":true`) {
		t.Fatalf("missing source was not returned as a preserved blocker: %d %s", code, diagnostic)
	}
	for _, want := range []string{source, strings.TrimSpace(head), strings.TrimSpace(branch), "Main restores", "do not recreate private answer caches"} {
		if !strings.Contains(diagnostic, want) {
			t.Fatalf("missing original source omitted recovery coordinate %q: %d %s", want, code, diagnostic)
		}
	}
	if !bytes.Equal(beforeState, mustStageFile(t, filepath.Join(root, ".claude/loop-state.json"))) || !bytes.Equal(beforeJournal, mustStageFile(t, filepath.Join(root, ".claude/loop-events.jsonl"))) {
		t.Fatal("missing source manufactured or changed a reception")
	}
	if _, err := workspace.Git(t.Context(), root, "worktree", "add", source, strings.TrimSpace(branch)); err != nil {
		t.Fatal(err)
	}
	if got, err := workspace.Git(t.Context(), source, "rev-parse", "HEAD"); err != nil || strings.TrimSpace(got) != strings.TrimSpace(head) {
		t.Fatalf("recovery drifted Git source: %s %v", got, err)
	}
	for _, path := range []string{plan, result} {
		if _, err := os.Stat(filepath.Join(source, path)); !os.IsNotExist(err) {
			t.Fatalf("private authoring cache unexpectedly survived: %s %v", path, err)
		}
	}
	var out, recoveredDiagnostic bytes.Buffer
	if code := cli.Run([]string{"runtime", "task-integrate", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID}, nil, &out, &recoveredDiagnostic); code != 0 {
		t.Fatalf("Main integration of recovered accepted source: %d %s %s", code, &out, &recoveredDiagnostic)
	}
	cp, found, err := integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, r.Reference()))
	if err != nil || !found || cp.State != integration.StateComplete || cp.CanonicalMainFacts == nil || cp.VerifiedAt == "" {
		t.Fatalf("original Main checks/ACK/cleanup incomplete: %+v %v", cp, err)
	}
}

func TestNativeS6ReceiverSourceFailureBelongsToMain(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	nativeCommit(t, source)
	nativeAnswer(t, source, r)
	brief, _, result := assignmentrecord.NativePaths(r)
	if err := os.Remove(filepath.Join(root, ".claude/assignments", r.AssignmentID+".json")); err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, root).Revision
	code, output := nativeEvent(t, root, source, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(source, result), false)
	if code != 0 || !strings.Contains(output, "Main owns recovery") || !strings.Contains(output, r.AssignmentID+".json") {
		t.Fatalf("receiver failure sent back to author: %d %s", code, output)
	}
	if stageSnapshot(t, root).Revision != before {
		t.Fatal("failed reception fabricated completion")
	}
}

func TestNativeS6ResumeReceivesLatestActualTaskWithoutToolResultGuess(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	nativeCommit(t, source)
	nativeAnswer(t, source, r)
	brief, _, result := assignmentrecord.NativePaths(r)
	refs := filepath.Join(root, brief) + "\n" + filepath.Join(source, result)
	transcript := filepath.Join(t.TempDir(), "resume.jsonl")
	var rows bytes.Buffer
	appendRow := func(content any) {
		data, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}})
		rows.Write(data)
		rows.WriteByte('\n')
	}
	appendRow("Earlier unrelated task /retired/brief.md")
	appendRow([]map[string]string{{"type": "tool_result", "text": refs}})
	call := func() (int, string) {
		if err := os.WriteFile(transcript, rows.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": "opaque-resumed-worker", "cwd": root, "agent_transcript_path": transcript, "last_assistant_message": "Finished the resumed responsibility."})
		var out, diagnostic bytes.Buffer
		code := cli.Run([]string{"hook", "--root", root, "--event", "SubagentStop"}, bytes.NewReader(body), &out, &diagnostic)
		return code, out.String() + diagnostic.String()
	}
	before := stageSnapshot(t, root).Revision
	if code, output := call(); code != 0 || stageSnapshot(t, root).Revision != before {
		t.Fatalf("tool result claimed a responsibility: %d %s", code, output)
	}
	appendRow("Resumed actual TASK: " + refs)
	if code, output := call(); code != 0 {
		t.Fatalf("resume: %d %s", code, output)
	}
	found := false
	for _, raw := range stageSnapshot(t, root).State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == r.OwnerAgentID {
			found = row["completion_reported_ref"] != nil
		}
	}
	if !found {
		t.Fatal("latest actual TASK was not received")
	}
}
