package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS9MainCompletedReturnContinuesOriginalSourceAfterCleanup(t *testing.T) {
	root, worker, p, stop, _ := nativeS9LegacyWorkerFixture(t)
	repairLeadGit(t, worker, "reset", "--hard", repairLeadGit(t, root, "rev-parse", "HEAD"))
	blocker := filepath.Join(root, ".claude/evidence/s9-source", p.ID, "typed")
	writeRepairLeadFile(t, root, filepath.ToSlash(filepath.Join(".claude/evidence/s9-source", p.ID, "typed")), "injected typed publication failure")
	stop.Event = "SubagentStop"
	var diagnostic bytes.Buffer
	nativeS9Hook(root, stop, &bytes.Buffer{}, &diagnostic)
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil || registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef != "" {
		t.Fatalf("fault not reached: %v %s", err, &diagnostic)
	}
	repairLeadGit(t, root, "worktree", "remove", "--force", worker)
	repairLeadGit(t, root, "branch", "-D", "native-s9-test")
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	brief, _, _ := nativeS9Paths(p)
	original := filepath.Join(root, brief)
	invoke := func(prompt string, response map[string]any, failure string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Agent", "tool_use_id": "actual-platform-tool", "error": failure, "tool_input": map[string]any{"prompt": prompt}, "tool_response": response})
		var out, diagnostic bytes.Buffer
		if code := Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
			t.Fatalf("Post: %d %s %s", code, &out, &diagnostic)
		}
		return out.String()
	}
	for _, test := range []struct {
		name, prompt, status, actor, failure string
		isError                              bool
	}{
		{name: "async", prompt: original, status: "async_launched", actor: stop.AgentID},
		{name: "unknown", prompt: original, actor: stop.AgentID},
		{name: "failed", prompt: original, status: "completed", actor: stop.AgentID, isError: true},
		{name: "transport-failed", prompt: original, status: "completed", actor: stop.AgentID, failure: "platform failure"},
		{name: "wrong-actor", prompt: original, status: "completed", actor: "another-actor"},
		{name: "response-only", prompt: "unrelated issued task", status: "completed", actor: stop.AgentID},
		{name: "stale-reference", prompt: original + ".old", status: "completed", actor: stop.AgentID},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := repairDispatchRuntimeBytes(t, root)
			invoke(test.prompt, map[string]any{"status": test.status, "agentId": test.actor, "is_error": test.isError, "text": original}, test.failure)
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
	packets, err := runtime.CurrentRepairResultPacketsContext(t.Context(), root, snap.State, p.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original result: %v %+v", err, packets)
	}
	before := repairDispatchRuntimeBytes(t, root)
	invoke(original+"\n"+filepath.Join(root, packets[0].ViewPath), map[string]any{"status": "completed", "agentId": stop.AgentID}, "")
	repairDispatchAssertUnchanged(t, root, before)
	// Cancellation remains the actual original request lifetime.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	in := policy.Input{Context: ctx, Event: "PostToolUse", CWD: root, ToolName: "Agent", ToolUseID: "cancelled", ToolInput: map[string]any{"prompt": original}, ToolResponse: map[string]any{"status": "completed", "agentId": stop.AgentID}}
	_, attempted, err := continueNativeS9MainReturn(ctx, root, in, snap, nativeReturnSubject{kind: "s9-plan-report-v1", id: p.ID, ref: original})
	if attempted || err != nil {
		t.Fatalf("cancelled request selected a consumer: %t %v", attempted, err)
	}
	repairDispatchAssertUnchanged(t, root, before)
	text := invoke(original, map[string]any{"status": "completed", "agentId": stop.AgentID}, "")
	after := repairDispatchSnapshot(t, root)
	registry, err = workspace.Decode(after.State)
	if err != nil || registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef == "" || !strings.Contains(text, "System continued the original") || !strings.Contains(text, "candidate_accepted") {
		t.Fatalf("original source not continued: %v %s", err, text)
	}
	before = repairDispatchRuntimeBytes(t, root)
	invoke(original, map[string]any{"status": "completed", "agentId": stop.AgentID}, "")
	repairDispatchAssertUnchanged(t, root, before)
	if _, err := os.Stat(worker); !os.IsNotExist(err) {
		t.Fatalf("source recreated: %v", err)
	}
}

// Historical C preparations must remain explicit when new tasks default to D.
func nativeS9LegacyWorkerFixture(t *testing.T, assertionCopies ...int) (string, string, workpacket.RepairPlanReportPacket, policy.Input, workpacket.RepairResultProfessionalAnswer) {
	t.Helper()
	root, worker, p, stop, answer := nativeS9WorkerFixture(t, assertionCopies...)
	plan, result, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		t.Fatal(err)
	}
	brief, pin, oldResult := nativeS9Paths(p)
	var saved map[string]any
	readRepairLeadJSON(t, filepath.Join(root, pin), &saved)
	delete(saved, "private_submission_version")
	saved["source_retention_version"] = 1
	if err := persistRepairLeadQuestionnaire(filepath.Join(root, pin), saved); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{plan, p.AnswerPath}, {result, oldResult}} {
		raw, err := os.ReadFile(filepath.Join(worker, pair[0]))
		if err != nil {
			t.Fatal(err)
		}
		writeRepairLeadFile(t, worker, pair[1], string(raw))
		if pair[0] != pair[1] {
			if err := os.Remove(filepath.Join(worker, pair[0])); err != nil {
				t.Fatal(err)
			}
		}
	}
	stop.LastAssistantMessage = filepath.Join(root, brief) + "\n" + filepath.Join(worker, oldResult)
	return root, worker, p, stop, answer
}

func nativeS9TestAuthorPaths(t *testing.T, root string, p workpacket.RepairPlanReportPacket) (string, string) {
	t.Helper()
	plan, result, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		t.Fatal(err)
	}
	return plan, result
}

func nativeS9TestPlanPath(t *testing.T, root string, p workpacket.RepairPlanReportPacket) string {
	t.Helper()
	plan, _ := nativeS9TestAuthorPaths(t, root, p)
	return plan
}

func nativeS9TestResultPath(t *testing.T, root string, p workpacket.RepairPlanReportPacket) string {
	t.Helper()
	_, result := nativeS9TestAuthorPaths(t, root, p)
	return result
}

func TestNativeS9MainCompletedReturnContinuesExactLiveDSource(t *testing.T) {
	root, worker, p, stop, _ := nativeS9WorkerFixture(t)
	unknown := "unreceived-sibling.txt"
	writeRepairLeadFile(t, worker, unknown, "preserve until its actual owner resolves it\n")
	stop.Event = "SubagentStop"
	var diagnostic bytes.Buffer
	nativeS9Hook(root, stop, &bytes.Buffer{}, &diagnostic)
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil {
		t.Fatalf("original source: %v %s", err, &diagnostic)
	}
	e := registry.Executions[p.Binding.ManifestAssignmentID]
	if e.Path != worker || e.DeliveryRef != "" || repair.NativeS9SourceReference(e).Path != "" {
		t.Fatalf("did not preserve incomplete D source: %#v %s", e, &diagnostic)
	}
	if err := os.Remove(filepath.Join(worker, unknown)); err != nil {
		t.Fatal(err)
	}
	brief, _, _ := nativeS9Paths(p)
	original := filepath.Join(root, brief)
	invoke := func(prompt string, response map[string]any, failure string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Agent", "tool_use_id": "actual-platform-d-return", "error": failure, "tool_input": map[string]any{"prompt": prompt}, "tool_response": response})
		var out, diagnostic bytes.Buffer
		if code := Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
			t.Fatalf("Post: %d %s %s", code, &out, &diagnostic)
		}
		return out.String()
	}
	response := func() map[string]any {
		return map[string]any{"status": "completed", "agentId": stop.AgentID, "worktreePath": worker, "worktreeBranch": e.Branch}
	}
	for _, scenario := range []string{"async", "unknown", "failed", "transport-failed", "missing-root", "missing-branch", "wrong-root", "wrong-branch", "no-actor", "main-actor", "response-only", "stale-reference"} {
		t.Run(scenario, func(t *testing.T) {
			before := repairDispatchRuntimeBytes(t, root)
			r := response()
			prompt, failure := original, ""
			switch scenario {
			case "async":
				r["status"] = "async_launched"
			case "unknown":
				delete(r, "status")
			case "failed":
				r["is_error"] = true
			case "transport-failed":
				failure = "platform failure"
			case "missing-root":
				delete(r, "worktreePath")
				r["text"] = "worktreePath: " + worker
			case "missing-branch":
				delete(r, "worktreeBranch")
			case "wrong-root":
				r["worktreePath"] = root
			case "wrong-branch":
				r["worktreeBranch"] = "unrelated-branch"
			case "no-actor":
				delete(r, "agentId")
			case "main-actor":
				r["agentId"] = "Main"
			case "response-only":
				prompt = "unrelated issued task"
				r["text"] = original
			case "stale-reference":
				prompt = original + ".old"
			}
			invoke(prompt, r, failure)
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
	before := repairDispatchRuntimeBytes(t, root)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	in := policy.Input{Context: ctx, Event: "PostToolUse", CWD: root, ToolName: "Agent", ToolUseID: "cancelled", ToolInput: map[string]any{"prompt": original}, ToolResponse: response()}
	_, attempted, err := continueNativeS9MainReturn(ctx, root, in, snap, nativeReturnSubject{kind: "s9-plan-report-v1", id: p.ID, ref: original})
	if attempted || err != nil {
		t.Fatalf("cancelled request selected a consumer: %t %v", attempted, err)
	}
	repairDispatchAssertUnchanged(t, root, before)
	text := invoke(original, response(), "")
	after := repairDispatchSnapshot(t, root)
	registry, err = workspace.Decode(after.State)
	if err != nil || registry.Executions[p.Binding.ManifestAssignmentID].DeliveryRef == "" || !strings.Contains(text, "candidate_accepted") {
		t.Fatalf("D source not continued: %v %s", err, text)
	}
	before = repairDispatchRuntimeBytes(t, root)
	invoke(original, response(), "")
	repairDispatchAssertUnchanged(t, root, before)
}
