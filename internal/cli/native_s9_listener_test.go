package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/submission"
)

func TestNativeS9ListenerReceivesPlanBeforeResultAndPreservesDirtyWork(t *testing.T) {
	root, worker, p, in, _ := nativeS9WorkerFixture(t)
	_, result, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(worker, result)
	original, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(resultPath); err != nil {
		t.Fatal(err)
	}
	dirty := "package api\nconst StillInProgress = true\n"
	writeRepairLeadFile(t, worker, "internal/api/one.go", dirty)
	in.Event, in.CWD, in.ToolName = "PreToolUse", worker, "Bash"
	// Actual ordinary payloads identify the child but transcript_path points
	// at the parent's session file. Only the sibling child file is authority.
	brief, _, _ := nativeS9Paths(p)
	parent := filepath.Join(t.TempDir(), "parent-session.jsonl")
	child := filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-"+in.AgentID+".jsonl")
	row, _ := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "agentId": in.AgentID, "message": map[string]any{"role": "user", "content": "Repair using this exact brief " + filepath.Join(root, brief)}})
	if err := os.MkdirAll(filepath.Dir(child), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("parent text is never read as child task"), 0600); err != nil {
		t.Fatal(err)
	}
	in.LastAssistantMessage, in.TranscriptPath = "", parent
	var out, diagnostic bytes.Buffer
	request, _ := json.Marshal(in)
	if code := Run([]string{"hook", "--root", root, "--event", in.Event}, bytes.NewReader(request), &out, &diagnostic); code != 0 {
		t.Fatalf("public natural Plan: %d %s %s", code, &out, &diagnostic)
	}
	planFeedback := nativeS9ListenerFeedback(t, &out)
	if !strings.Contains(planFeedback, "PlanReport") || !strings.Contains(planFeedback, "was accepted") || !strings.Contains(planFeedback, p.Binding.OwnerAgentID) {
		t.Fatalf("accepted Plan has no actual Worker context: %s", planFeedback)
	}
	out.Reset()
	service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl")}
	receipt, err := service.RepairPlanReportReceipt(p.Binding.OwnerAgentID, p.ID)
	if err != nil || !receipt.Accepted {
		t.Fatalf("original Plan consumer not reached: %+v %v %s", receipt, err, &diagnostic)
	}
	if _, err := os.Stat(resultPath); !os.IsNotExist(err) {
		t.Fatal("missing Result was fabricated")
	}
	if raw, err := os.ReadFile(filepath.Join(worker, "internal/api/one.go")); err != nil || string(raw) != dirty {
		t.Fatal("in-progress product work changed", err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	for _, body := range [][]byte{[]byte(`{"verdict":`), []byte(`{"verdict":""}`)} {
		if err := os.WriteFile(resultPath, body, 0600); err != nil {
			t.Fatal(err)
		}
		if _, code := nativeS9Hook(root, in, &out, &diagnostic); code != 0 {
			t.Fatal(&diagnostic)
		}
		repairDispatchAssertUnchanged(t, root, before)
		if out.Len() != 0 {
			t.Fatalf("unfinished Result generated feedback: %s", &out)
		}
	}
	// Restore committed product bytes and a complete professional Result. No
	// submit, done marker, or Worker-side receipt action is involved.
	repairLeadGit(t, worker, "checkout", "--", "internal/api/one.go")
	if err := os.WriteFile(resultPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	in.Event = "PostToolUse"
	diagnostic.Reset()
	request, _ = json.Marshal(in)
	if code := Run([]string{"hook", "--root", root, "--event", in.Event}, bytes.NewReader(request), &out, &diagnostic); code != 0 || !strings.Contains(diagnostic.String(), "professional Result received") {
		t.Fatalf("natural Result: %d %s", code, &diagnostic)
	}
	resultFeedback := nativeS9ListenerFeedback(t, &out)
	if !strings.Contains(resultFeedback, "S9 Result") || !strings.Contains(resultFeedback, "was accepted") || !strings.Contains(resultFeedback, resultPath) || !strings.Contains(resultFeedback, "Main owns") || strings.Contains(resultFeedback, "runtime workspace integrate") {
		t.Fatalf("Result feedback misstates acceptance or assigns Main work to Worker: %s", resultFeedback)
	}
	out.Reset()
	before = repairDispatchRuntimeBytes(t, root)
	if _, code := nativeS9Hook(root, in, &out, &diagnostic); code != 0 {
		t.Fatal(&diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
	if out.Len() != 0 {
		t.Fatalf("accepted replay repeated Worker feedback: %s", &out)
	}
}

func TestNativeS9ListenerIgnoresFailedPostAndOtherCheckout(t *testing.T) {
	root, worker, _, in, _ := nativeS9WorkerFixture(t)
	before := repairDispatchRuntimeBytes(t, root)
	in.Event, in.CWD = "PostToolUse", worker
	child := filepath.Join(t.TempDir(), "agent.jsonl")
	row, _ := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "agentId": in.AgentID, "message": map[string]any{"role": "user", "content": in.LastAssistantMessage}})
	if err := os.WriteFile(child, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	in.AgentTranscriptPath = child
	var out, diagnostic bytes.Buffer
	in.ToolResponse = map[string]any{"is_error": true}
	nativeS9Hook(root, in, &out, &diagnostic)
	repairDispatchAssertUnchanged(t, root, before)
	in.ToolResponse = nil
	other := filepath.Join(t.TempDir(), "other-owner")
	repairLeadGit(t, root, "worktree", "add", "-b", "listener-other", other, "HEAD")
	in.Event, in.CWD = "PreToolUse", other
	nativeS9Hook(root, in, &out, &diagnostic)
	repairDispatchAssertUnchanged(t, root, before)
	if feedback := nativeS9ListenerFeedback(t, &out); !strings.Contains(feedback, "different sources") || !strings.Contains(feedback, "Original consumer diagnostic") {
		t.Fatalf("source rejection did not reach actual Worker: %s", feedback)
	}
	if !strings.Contains(diagnostic.String(), "different sources") {
		t.Fatalf("contradictory source was not rejected: %s", &diagnostic)
	}
}

// The model sees additionalContext, not successful Hook stderr. Feedback must
// preserve the natural tool event and must never deny the requested tool.
func nativeS9ListenerFeedback(t *testing.T, out *bytes.Buffer) string {
	t.Helper()
	if out.Len() == 0 {
		return ""
	}
	var message struct {
		Decision string `json:"decision"`
		Output   struct {
			Event      string `json:"hookEventName"`
			Context    string `json:"additionalContext"`
			Permission string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &message); err != nil {
		t.Fatalf("invalid Worker feedback: %v %s", err, out)
	}
	if message.Decision != "" || message.Output.Permission != "" {
		t.Fatalf("reception feedback changed tool authorization: %s", out)
	}
	return message.Output.Context
}
