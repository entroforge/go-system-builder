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
		owner        = "agent-s6-prepared"
		platformTeam = "platform-s6-prepared"
		manifestRef  = ".claude/workgroups/REQ-039/TASK-039-01/manifest.draft.json"
	)
	before := stageSnapshot(t, root)
	prepareOutput := stageCLI(t, "s6", "prepare", "--root", root, "--task", taskID,
		"--role", "backend-builder", "--agent-id", owner, "--platform-team-id", platformTeam)
	if !strings.Contains(prepareOutput, "S6 manifest draft written (not registered): "+manifestRef) {
		t.Fatalf("S6 prepare did not return the exact manifest path: %s", prepareOutput)
	}
	if !strings.Contains(prepareOutput, "runtime workspace bind --root \""+root+"\"") {
		t.Fatalf("S6 prepare did not provide the exact Main workspace bind command: %s", prepareOutput)
	}
	var help, helpErr strings.Builder
	if code := cli.Run([]string{"s6", "prepare", "--help"}, nil, &help, &helpErr); code != 0 || !strings.Contains(help.String(), "runtime workspace bind --root <authority-root>") {
		t.Fatalf("S6 prepare help did not document the existing workspace bind prerequisite: code=%d help=%s stderr=%s", code, help.String(), helpErr.String())
	}
	if after := stageSnapshot(t, root); after.Revision != before.Revision {
		t.Fatalf("preparation mutated Runtime revision: before=%d after=%d", before.Revision, after.Revision)
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
		manifest["assignment_record_contract"] != assignmentrecord.AssignmentRecordContract {
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

	stageCLI(t, "runtime", "register-workgroup", "--root", root,
		"--manifest", manifestRef, "--task-id", taskID, "--task", taskPath)
	registered := stageSnapshot(t, root)
	record, found, err := assignmentrecord.ForOwner(registered.State, owner)
	if err != nil || !found {
		t.Fatalf("original register-workgroup rejected the prepared manifest: found=%t err=%v", found, err)
	}
	if record.ClosingContract == nil || record.IntegrationCheckMode != "post_merge" ||
		len(record.RequiredChecks) != 1 || record.RequiredChecks[0] != "go test ./src/pkg" ||
		len(record.CheckProfiles) != 1 || record.CheckProfiles[0].Location != "main" {
		t.Fatalf("original Assignment Record did not preserve projected checks: %+v", record)
	}

	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	workspaceOutput := stageCLI(t, "runtime", "workspace", "prepare", "--root", root,
		"--assignment", record.AssignmentID, "--agent", record.OwnerAgentID)
	var prepared struct {
		Status        string   `json:"status"`
		CheckLocation string   `json:"check_location"`
		CheckCommands []string `json:"check_commands"`
	}
	if err := json.Unmarshal([]byte(workspaceOutput), &prepared); err != nil {
		t.Fatalf("decode original bound workspace preparation: %v\n%s", err, workspaceOutput)
	}
	if prepared.Status != "ready" || prepared.CheckLocation != "main" || len(prepared.CheckCommands) != 0 {
		t.Fatalf("original bound workspace did not prepare the canonical assignment: %+v", prepared)
	}
}

func TestS6PrepareRefusesDraftOverwriteWithoutRuntimeWrite(t *testing.T) {
	root, taskID, _ := newS6RecordContextFixture(t)
	args := []string{"s6", "prepare", "--root", root, "--task", taskID,
		"--role", "backend-builder", "--agent-id", "agent-s6-prepared", "--platform-team-id", "platform-s6-prepared"}
	stageCLI(t, args...)
	beforeState, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if code := cli.Run(args, nil, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "refusing to overwrite existing draft") {
		t.Fatalf("second preparation did not refuse the existing draft: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
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
		t.Fatal("rejected draft overwrite changed the original Runtime state/journal")
	}
}

func TestS6PrepareRejectsLockedSourceDriftWithoutRuntimeWrite(t *testing.T) {
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
	beforeState, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code := cli.Run([]string{"s6", "prepare", "--root", root, "--task", taskID,
		"--role", "backend-builder", "--agent-id", "agent-s6-prepared", "--platform-team-id", "platform-s6-prepared"}, nil, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "registered dispatch input drift") {
		t.Fatalf("source drift did not remain behind original registration checks: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
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
		t.Fatal("rejected source drift changed the original Runtime state/journal")
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
		"--role", "backend-builder", "--agent-id", "agent-s6-dependent", "--platform-team-id", "platform-s6-dependent"}, nil, &stdout, &stderr)
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
