package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestS6PrepareRegistersAndPreparesOriginalBoundWorkspace(t *testing.T) {
	root, taskID, taskPath := newS6RecordContextFixture(t)
	const (
		// L3-S6 §4.1: Main 不需要先指定官方 Agent/Team ID. The native
		// delivery owner and team are derived from the reviewed TASK.
		owner        = "native-TASK-039-01"
		platformTeam = "engineering-TASK-039-01"
		manifestRef  = ".claude/workgroups/REQ-039/TASK-039-01/manifest.draft.json"
		briefRef     = ".claude/assignments/native/assignment-build-TASK-039-01-g1/r1/task.md"
	)
	before := stageSnapshot(t, root)
	// L3-S6 §4.1 documents exactly this invocation: the reviewed TASK and the
	// Builder role, with no platform identity and no launcher.
	prepareOutput := stageCLI(t, "s6", "prepare", "--root", root, "--task", taskID,
		"--role", "backend-builder")
	if !strings.Contains(prepareOutput, "Native S6 TASK registered: "+taskID) {
		t.Fatalf("S6 prepare did not register the native TASK responsibility: %s", prepareOutput)
	}
	if !strings.Contains(prepareOutput, "use Claude Code's native Agent (backend-builder) with worktree isolation") {
		t.Fatalf("S6 prepare did not hand the exact native dispatch to Main: %s", prepareOutput)
	}
	if !strings.Contains(prepareOutput, filepath.ToSlash(briefRef)) {
		t.Fatalf("S6 prepare did not name the exact native task brief: %s", prepareOutput)
	}
	if !strings.Contains(prepareOutput, "No Team, platform Agent ID or launcher is required.") {
		t.Fatalf("S6 prepare must not require a platform Team or Agent ID: %s", prepareOutput)
	}
	// The legacy frozen-workspace route is gone: Main must not be told to bind
	// a platform workspace before the native dispatch can happen.
	if strings.Contains(prepareOutput, "runtime workspace bind") {
		t.Fatalf("S6 prepare still routes Main through the removed frozen workspace bind: %s", prepareOutput)
	}
	var help, helpErr strings.Builder
	if code := cli.Run([]string{"s6", "prepare", "--help"}, nil, &help, &helpErr); code != 0 {
		t.Fatalf("s6 prepare --help failed: code=%d stderr=%s", code, helpErr.String())
	}
	for _, unwanted := range []string{"runtime workspace bind", "-agent-id", "-platform-team-id"} {
		if strings.Contains(help.String(), unwanted) {
			t.Fatalf("s6 prepare help still documents the removed legacy option %q: %s", unwanted, help.String())
		}
	}
	if !strings.Contains(help.String(), "No platform IDs or framework launcher are required.") {
		t.Fatalf("s6 prepare help must state that no platform identity is required: %s", help.String())
	}

	manifestBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifestRef)))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("team-manifest.schema.json", manifestBytes); err != nil {
		t.Fatalf("prepared manifest fails the original schema: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["runtime_id"] != before.State["runtime_id"] || manifest["req_id"] != "REQ-039" ||
		manifest["baseline_generation"] != float64(1) || manifest["platform_team_id"] != platformTeam ||
		manifest["assignment_record_contract"] != assignmentrecord.NativeDeliveryContract {
		t.Fatalf("manifest did not project the original Runtime and Main decisions: %v", manifest)
	}
	docIDs := map[string]bool{}
	for _, raw := range manifest["documents"].([]any) {
		docIDs[raw.(map[string]any)["id"].(string)] = true
	}
	for _, id := range []string{"REQ-039", "BE-039", taskID, "dispatch-plan:REQ-039"} {
		if !docIDs[id] {
			t.Errorf("prepared manifest omitted exact registered source %s: %v", id, docIDs)
		}
	}
	if docIDs["ARCH-039"] {
		t.Errorf("prepared manifest froze an unrelated TASK input: %v", docIDs)
	}
	assignments := manifest["assignments"].([]any)
	if len(assignments) != 1 {
		t.Fatalf("prepared assignments = %d, want the selected TASK's single Writer", len(assignments))
	}
	assignment := assignments[0].(map[string]any)
	if assignment["assignment_id"] != "assignment-build-TASK-039-01-g1" || assignment["agent_id"] != owner ||
		assignment["integration_check_mode"] != "post_merge" {
		t.Fatalf("prepared assignment lost its exact identity or original S6 closing mode: %v", assignment)
	}
	if got := assignment["done_when"].([]any); len(got) != 1 || got[0] != "go test ./src/pkg == pass" {
		t.Fatalf("numbered TASK Closing Contract was not projected: %v", assignment["done_when"])
	}
	if got := assignment["required_checks"].([]any); len(got) != 1 || got[0] != "go test ./src/pkg" {
		t.Fatalf("TASK command did not reach original Main required_checks: %v", got)
	}
	profiles := assignment["check_profiles"].([]any)
	if len(profiles) != 1 || profiles[0].(map[string]any)["location"] != "main" {
		t.Fatalf("TASK check was not assigned to the original Main runner: %v", profiles)
	}
	closing := assignment["closing_contract"].(map[string]any)
	if closing["contract"] != assignmentrecord.ClosingContractVersion || len(closing["worker_checks"].([]any)) != 0 {
		t.Fatalf("prepared manifest did not declare the existing empty WorkerCheck set: %v", closing)
	}

	// L3-S6 §4.1: the preparation itself establishes this engineering
	// responsibility through the original Assignment Writer; Main does not
	// re-register the manifest by hand.
	registered := stageSnapshot(t, root)
	record, found, err := assignmentrecord.ForOwner(registered.State, owner)
	if err != nil || !found {
		t.Fatalf("original Assignment Writer did not register the prepared native responsibility: found=%t err=%v", found, err)
	}
	if record.Contract != assignmentrecord.NativeDeliveryContract ||
		record.Task.ID != taskID || record.Task.Ref != taskPath {
		t.Fatalf("registered native Assignment Record lost its original TASK identity: %+v", record)
	}
	if record.ClosingContract == nil || record.IntegrationCheckMode != "post_merge" ||
		len(record.RequiredChecks) != 1 || record.RequiredChecks[0] != "go test ./src/pkg" ||
		len(record.CheckProfiles) != 1 || record.CheckProfiles[0].Location != "main" {
		t.Fatalf("original Assignment Record did not preserve projected checks: %+v", record)
	}

	// L3-S6 §4.2: the brief must let the Builder work continuously — the exact
	// TASK source, the Done when conditions, the Worker checks and the fixed
	// plan/result paths are all in the issued brief.
	briefBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(briefRef)))
	if err != nil {
		t.Fatalf("s6 prepare did not publish the native task brief: %v", err)
	}
	brief := string(briefBytes)
	for _, want := range []string{taskPath, "go test ./src/pkg == pass", "Done when:"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("native brief omitted %q: %s", want, brief)
		}
	}
}

// TestS6PrepareRefusesDraftOverwriteWithoutRuntimeWrite pins the replay
// contract of the current native preparation. The removed legacy draft route
// wrote an unregistered manifest draft and refused a second write; the current
// route registers the responsibility through the original Assignment Writer
// and, on a same-responsibility retry, replays the already issued brief
// without touching the original Runtime state or journal
// (L3-S6 §4.1/§4.2, L4-workflow-convergence §"S6 实际返工…同次重试复用已成立事实").
func TestS6PrepareRefusesDraftOverwriteWithoutRuntimeWrite(t *testing.T) {
	root, taskID, _ := newS6RecordContextFixture(t)
	args := []string{"s6", "prepare", "--root", root, "--task", taskID,
		"--role", "backend-builder"}
	stageCLI(t, args...)
	beforeState, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	beforeBrief, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(".claude/assignments/native/assignment-build-"+taskID+"-g1/r1/task.md")))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if code := cli.Run(args, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("same-responsibility retry did not replay the original preparation: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Native S6 TASK registered: "+taskID) {
		t.Fatalf("replayed preparation did not name the original responsibility: %s", stdout.String())
	}
	afterState, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	afterJournal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeState) != string(afterState) || string(beforeJournal) != string(afterJournal) {
		t.Fatal("replayed preparation changed the original Runtime state/journal")
	}
	afterBrief, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(".claude/assignments/native/assignment-build-"+taskID+"-g1/r1/task.md")))
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeBrief) != string(afterBrief) {
		t.Fatal("replayed preparation rewrote the already issued original brief")
	}
}

func TestS6PrepareUsesCurrentTaskWithoutHistoricalDigestGate(t *testing.T) {
	root, taskID, taskPath := newS6RecordContextFixture(t)
	if _, err := workspace.Git(context.Background(), root, "checkout", "test-development"); err != nil {
		t.Fatal(err)
	}
	taskFile := filepath.Join(root, filepath.FromSlash(taskPath))
	data, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\n<!-- changed after S4 registration -->\n")...)
	if err := os.WriteFile(taskFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(context.Background(), root, "add", taskPath); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(context.Background(), root, "commit", "-m", "change locked TASK source after registration"); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code := cli.Run([]string{"s6", "prepare", "--root", root, "--task", taskID,
		"--role", "backend-builder"}, nil, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "native Agent") {
		t.Fatalf("current TASK was rejected by a historical digest: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	record, found, err := assignmentrecord.ForOwner(stageSnapshot(t, root).State, "native-"+taskID)
	if err != nil || !found || record.Task.ID != taskID || record.Task.Ref != taskPath {
		t.Fatalf("native original TASK responsibility was not registered: %+v %v", record, err)
	}
}

func TestS6PrepareReportsUnmetDependencyWithoutRuntimeWrite(t *testing.T) {
	root, firstTaskID, _ := newS6RecordContextFixture(t)
	const (
		secondTaskID   = "TASK-039-02"
		secondTaskPath = "docs/dev/tasks/TASK-039-02.md"
		planPath       = "docs/dev/tasks/index-REQ-039.md"
	)
	firstTaskBytes, err := os.ReadFile(filepath.Join(root, "docs/dev/tasks/TASK-039-01.md"))
	if err != nil {
		t.Fatal(err)
	}
	secondTask := strings.Replace(string(firstTaskBytes), "TASK-039-01:", secondTaskID+":", 1)
	secondTask = strings.Replace(secondTask, "| — | — |", "| "+firstTaskID+" | waits for the base TASK integration |", 1)
	secondTaskBytes := []byte(secondTask)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(secondTaskPath)), secondTaskBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	planFile := filepath.Join(root, filepath.FromSlash(planPath))
	planBytes, err := os.ReadFile(planFile)
	if err != nil {
		t.Fatal(err)
	}
	planBytes = append(planBytes, []byte("\n## W2 · dependent TASK\n\n- [ ] [dependent TASK](TASK-039-02.md)\n")...)
	if err := os.WriteFile(planFile, planBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(context.Background(), root, "add", secondTaskPath, planPath); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(context.Background(), root, "commit", "-m", "register dependent TASK fixture inputs"); err != nil {
		t.Fatal(err)
	}

	state := stageSnapshot(t, root).State
	documents := state["documents"].([]any)
	for _, raw := range documents {
		doc := raw.(map[string]any)
		if doc["kind"] == "dispatch_plan" && doc["path"] == planPath {
			doc["sha256"] = sha256Hex(planBytes)
		}
	}
	state["documents"] = append(documents, s6RecordContextDocument(secondTaskID, "task", secondTaskPath, "v1.0.0", secondTaskBytes))
	stageJSON(t, root, ".claude/loop-state.json", state)

	beforeState, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code := cli.Run([]string{"s6", "prepare", "--root", root, "--task", secondTaskID,
		"--role", "backend-builder"}, nil, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "not ready") || !strings.Contains(stderr.String(), firstTaskID) {
		t.Fatalf("S6 prepare did not surface the original dependency wait: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/workgroups/REQ-039/TASK-039-02/manifest.draft.json")); !os.IsNotExist(err) {
		t.Fatalf("blocked dependency created a manifest draft: %v", err)
	}
	afterState, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	afterJournal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeState) != string(afterState) || string(beforeJournal) != string(afterJournal) {
		t.Fatal("blocked dependency changed the original Runtime state/journal")
	}
}
