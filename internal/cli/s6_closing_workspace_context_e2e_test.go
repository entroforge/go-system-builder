package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestS6ClosingWorkspaceRejectsWorkerLocationAndDefaultsToMain(t *testing.T) {
	root, taskID, taskPath := newS6RecordContextFixture(t)
	const manifestRef = ".claude/workgroups/manifest-s6-native-context.json"
	manifestBytes, err := os.ReadFile(filepath.Join(root, manifestRef))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	assignment := manifest["assignments"].([]any)[0].(map[string]any)
	assignment["integration_check_mode"] = "post_merge"
	assignment["closing_contract"] = map[string]any{"contract": assignmentrecord.ClosingContractVersion, "worker_checks": []any{}}
	assignment["check_profiles"].([]any)[0].(map[string]any)["location"] = "main"
	stageJSON(t, root, manifestRef, manifest)
	stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", manifestRef, "--task-id", taskID, "--task", taskPath)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	snapshot := stageSnapshot(t, root)
	record, found, err := assignmentrecord.ForOwner(snapshot.State, "agent-s6-native-context")
	if err != nil || !found || record.ClosingContract == nil || record.IntegrationCheckMode != "post_merge" {
		t.Fatalf("registration did not freeze original closing mode: found=%t record=%+v err=%v", found, record, err)
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "workspace", "prepare", "--root", root, "--assignment", record.AssignmentID, "--agent", record.OwnerAgentID, "--check-location", "worker"}, nil, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "differs from the Assignment's frozen integration check mode") {
		t.Fatalf("explicit Worker location did not reject a Main-owned closing profile: code=%d stderr=%s", code, stderr.String())
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatal("rejected Worker check-location changed the original Runtime state/journal pair")
	}
	if _, err := os.Stat(filepath.Join(root, ".worktrees")); !os.IsNotExist(err) {
		t.Fatalf("rejected location created a Worker checkout: %v", err)
	}

	output := stageCLI(t, "runtime", "workspace", "prepare", "--root", root, "--assignment", record.AssignmentID, "--agent", record.OwnerAgentID)
	var prepared struct {
		Status        string   `json:"status"`
		CheckLocation string   `json:"check_location"`
		CheckCommands []string `json:"check_commands"`
	}
	if err := json.Unmarshal([]byte(output), &prepared); err != nil {
		t.Fatalf("decode original workspace preparation: %v\n%s", err, output)
	}
	if prepared.Status != "ready" || prepared.CheckLocation != "main" || len(prepared.CheckCommands) != 0 {
		t.Fatalf("closing Assignment did not keep checks on Main: %+v", prepared)
	}
	current, err := workspace.Decode(stageSnapshot(t, root).State)
	if err != nil || current == nil {
		t.Fatalf("read original Workspace execution: %v", err)
	}
	execution, ok := current.Execution(record.AssignmentID, record.RuntimeID, record.BaselineGeneration)
	if !ok || execution.CheckLocation != "main" {
		t.Fatalf("persisted execution lost the frozen Main check location: %+v", execution)
	}
}
