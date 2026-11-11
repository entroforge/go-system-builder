package req039_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Start/Stop receive the actual professional answer; Main's Agent return
// reads its durable receipt and then performs the long integration itself.
// This representative chain also covers CT-039-20: the original Result stays
// durable and shared by Task, Assignment and owner across Main integration.
func TestSubagent_Integration_Guidance(t *testing.T) {
	root, worker, record := systemNativeBuilder(t)
	ref := receiveSystemNativeBuilder(t, root, worker, record)
	original, err := os.ReadFile(filepath.Join(root, ref))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, assignment := range loaded.Assignments {
		if assignment.AssignmentID == record.AssignmentID {
			found = true
			if assignment.TaskID != record.Task.ID || assignment.OwnerAgentID != record.OwnerAgentID || assignment.CompletionRef != ref {
				t.Fatalf("accepted responsibility split: %+v; Result %s", assignment, ref)
			}
		}
	}
	if !found {
		t.Fatal("accepted Assignment missing from original Hook context")
	}
	// Main's actual Agent return reads the accepted source without receiving
	// Result again or relying on a fabricated Stop systemMessage.
	brief, _, result := assignmentrecord.NativePaths(record)
	raw, err := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": "system-main-return", "cwd": root, "tool_name": "Agent", "tool_input": map[string]any{"prompt": filepath.Join(root, brief)}, "tool_response": map[string]any{"status": "completed", "text": filepath.Join(worker, result)}})
	if err != nil {
		t.Fatal(err)
	}
	runtimeBefore := readSystemRuntimePair(t, root)
	code, out, errout := runHook(t, root, "PostToolUse", string(raw))
	if code != 0 {
		t.Fatalf("Main return: %d %s %s", code, out, errout)
	}
	for _, want := range []string{record.AssignmentID, "task-integrate"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Main return omitted %q: %s %s", want, out, errout)
		}
	}
	assertSystemRuntimePairUnchanged(t, runtimeBefore)
	before := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
	code, out, errout = runTaskIntegrate(t, root, record.AssignmentID)
	if code != 0 || !strings.Contains(errout, "integrated") {
		t.Fatalf("Main integration: %d %s %s", code, out, errout)
	}
	for _, want := range []string{"TR-006", "ReviewPlan questionnaire:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Main integration omitted original successor %q: %s", want, out)
		}
	}
	req039fixtures.AssertLifecycle(t, req039fixtures.ReadState(t, root), "verification", "planned")
	after := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
	if after == before {
		t.Fatal("Main did not merge received work")
	}
	if parents := strings.Fields(runGitIn(t, root, "show", "-s", "--format=%P", after)); len(parents) != 2 {
		t.Fatalf("integration must retain the Worker merge parent: %v", parents)
	}
	if _, err := os.Stat(filepath.Join(root, "internal/proof.txt")); err != nil {
		t.Fatal(err)
	}
	// Pin actual Main checks and ACK to the same accepted Result, not only
	// the human-readable "integrated" output.
	checkpointData, err := os.ReadFile(assignmentrecord.IntegrationCheckpointPath(root, record.Reference()))
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint integration.Checkpoint
	if err := json.Unmarshal(checkpointData, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if (checkpoint.State != integration.StateComplete && checkpoint.State != integration.StateCleanupPending) || checkpoint.AssignmentID != record.AssignmentID || checkpoint.TaskID != record.Task.ID || checkpoint.VerifiedAt == "" || len(checkpoint.CheckReceipts) == 0 || checkpoint.CompletionReportPath != ref {
		t.Fatalf("Main checkpoint lost accepted Result or actual verification: %+v", checkpoint)
	}
	if _, err := os.Stat(worker); checkpoint.State == integration.StateComplete && !os.IsNotExist(err) {
		t.Fatalf("completed cleanup retained worktree: %v", err)
	} else if checkpoint.State == integration.StateCleanupPending && err != nil {
		t.Fatalf("pending cleanup did not preserve worktree: %v", err)
	}
	state := req039fixtures.ReadState(t, root)
	entities := state["entities"].(map[string]any)
	ownerFound := false
	for _, rawAgent := range entities["agents"].([]any) {
		agent := rawAgent.(map[string]any)
		if agent["id"] == record.OwnerAgentID {
			ownerFound = true
			// This chain has no dispatch backpressure; its optional report
			// ACK projection need not exist. The original ACK and verified
			// checkpoint still bind completion to the accepted Result.
			if agent["state"] != "done" || agent["completion_reported_ref"] != ref || agent["completion_acknowledged_ref"] == nil {
				t.Fatalf("owner lost original reception/ACK: %+v", agent)
			}
		}
	}
	if !ownerFound {
		t.Fatal("accepted owner missing after integration")
	}
	if got := completionTaskRef(t, state, record.Task.ID); got != ref {
		t.Fatalf("Result source changed: %q -> %q", ref, got)
	}
	afterResult, err := os.ReadFile(filepath.Join(root, ref))
	if err != nil || !bytes.Equal(original, afterResult) {
		t.Fatalf("integration lost immutable Result: %v", err)
	}
	code, out, errout = runTaskIntegrate(t, root, record.AssignmentID)
	if code != 0 || strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD")) != after {
		t.Fatalf("integration replay repeated work: %d %s %s", code, out, errout)
	}
}
