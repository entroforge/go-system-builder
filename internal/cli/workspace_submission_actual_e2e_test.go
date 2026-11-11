package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestFrozenWorkerTaskCompleteReadsOriginalAndReplays(t *testing.T) {
	fixture := readyS6CanonicalResultCLI(t, func(root string) {
		const manifestRef = ".claude/workgroups/manifest-s6-native-context.json"
		data, err := os.ReadFile(filepath.Join(root, manifestRef))
		if err != nil {
			t.Fatal(err)
		}
		var manifest map[string]any
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		assignment := manifest["assignments"].([]any)[0].(map[string]any)
		assignment["integration_check_mode"] = "post_merge"
		assignment["check_profiles"].([]any)[0].(map[string]any)["location"] = "main"
		stageJSON(t, root, manifestRef, manifest)
	})
	stageCLI(t, "runtime", "workspace", "bind", "--root", fixture.root)
	stageCLI(t, "runtime", "workspace", "prepare", "--root", fixture.root,
		"--assignment", fixture.record.AssignmentID, "--agent", fixture.record.OwnerAgentID, "--check-location", "main")
	registry, err := workspace.Decode(stageSnapshot(t, fixture.root).State)
	if err != nil || registry == nil {
		t.Fatalf("load original workspace: %v", err)
	}
	execution, found := registry.Execution(fixture.record.AssignmentID, fixture.record.RuntimeID, fixture.record.BaselineGeneration)
	if !found || execution.Status != "ready" {
		t.Fatalf("original Worker not ready: %+v", execution)
	}
	message := s6CanonicalCompletionMessage(fixture, "msg-frozen-worker-original", "The original Worker result was reported.")
	messagePath := ".loop-submissions/original-lifecycle-result.json"
	stageJSON(t, execution.Path, messagePath, message)
	original, err := os.ReadFile(filepath.Join(execution.Path, messagePath))
	if err != nil {
		t.Fatal(err)
	}
	output := stageCLI(t, "runtime", "task-complete", "--root", execution.Path,
		"--agent-id", fixture.record.OwnerAgentID, "--message", messagePath)
	accepted := s6DecodeCanonicalTaskCompleteOutput(t, output)
	if accepted.Acceptance.ResultRef == "" || accepted.Acceptance.Replayed {
		t.Fatalf("original Worker result was not accepted: %+v", accepted.Acceptance)
	}
	got, err := os.ReadFile(filepath.Join(execution.Path, messagePath))
	if err != nil || string(got) != string(original) {
		t.Fatalf("original Worker message changed: %v", err)
	}
	again := stageCLI(t, "runtime", "task-complete", "--root", execution.Path,
		"--agent-id", fixture.record.OwnerAgentID, "--message", messagePath)
	replayed := s6DecodeCanonicalTaskCompleteOutput(t, again)
	if !replayed.Acceptance.Replayed || replayed.Acceptance.ResultRef != accepted.Acceptance.ResultRef || replayed.Revision != accepted.Revision {
		t.Fatalf("original Worker replay changed acceptance: %+v", replayed.Acceptance)
	}
}
