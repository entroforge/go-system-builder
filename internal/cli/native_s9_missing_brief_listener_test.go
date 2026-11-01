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

// The display brief may disappear while the child still has its original
// task reference. Root routing and the receiver must use the original facts.
func TestNativeS9MissingBriefWorkerPreFindsControlRootAndReceivesOriginalPlan(t *testing.T) {
	root, worker, packet, in, _ := nativeS9WorkerFixture(t)
	brief := nativeS9BriefPath(packet)
	_, result, err := nativeS9AuthorPaths(root, packet)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, brief)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(worker, result)); err != nil {
		t.Fatal(err)
	}
	in.Event, in.CWD, in.ToolName, in.LastAssistantMessage = "PreToolUse", worker, "Bash", ""
	parent := filepath.Join(t.TempDir(), "parent.jsonl")
	child := filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-"+in.AgentID+".jsonl")
	row, _ := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "agentId": in.AgentID, "message": map[string]any{"role": "user", "content": "Repair with the original brief " + filepath.Join(root, brief)}})
	if err := os.MkdirAll(filepath.Dir(child), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("parent"), 0600); err != nil {
		t.Fatal(err)
	}
	in.TranscriptPath = parent
	request, _ := json.Marshal(in)
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"hook", "--root", worker, "--event", in.Event}, bytes.NewReader(request), &out, &diagnostic); code != 0 {
		t.Fatalf("public Worker Pre: %d %s %s", code, &out, &diagnostic)
	}
	service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl")}
	receipt, err := service.RepairPlanReportReceipt(packet.Binding.OwnerAgentID, packet.ID)
	if err != nil || !receipt.Accepted {
		t.Fatalf("original Plan not received: %+v %v output=%s diagnostic=%s", receipt, err, &out, &diagnostic)
	}
	if _, err := os.Stat(filepath.Join(root, brief)); !os.IsNotExist(err) {
		t.Fatalf("Worker fabricated or replaced display brief: %v", err)
	}
}
