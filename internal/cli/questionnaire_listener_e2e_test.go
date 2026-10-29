package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestQuestionnaireListenerReportsMissingViewReadOnlyAndPreservesAnswer(t *testing.T) {
	root, packet := newDesignFixture(t, "none")
	beforeSnapshot := stageSnapshot(t, root)
	beforeJournal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	answerPath := filepath.Join(root, packet.AnswerPath)
	stageJSON(t, root, packet.AnswerPath, designDraft(packet))
	before, err := os.ReadFile(answerPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, packet.ViewPath)); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"Read", "Write", "Bash"} {
		body, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "cwd": root, "tool_input": map[string]any{"file_path": "docs/requirements/REQ-039.md", "command": "git push origin main"}})
		var out, errout bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", root, "--event", "PreToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
			t.Fatalf("listener failed: code=%d %s %s", code, &out, &errout)
		}
		var envelope struct {
			Output struct {
				Context    string `json:"additionalContext"`
				Permission string `json:"permissionDecision"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(envelope.Output.Context, answerPath) || !strings.Contains(envelope.Output.Context, filepath.Join(root, packet.ViewPath)) || !strings.Contains(envelope.Output.Context, "Current missing work/material") || !strings.Contains(envelope.Output.Context, "runtime stage-answer issue --root "+questionnaireRecoveryShellQuote(root)+" --agent Main") || envelope.Output.Permission == "deny" {
			t.Fatalf("missing exact questionnaire path or unexpected operation gate: %s", &out)
		}
	}
	after, err := os.ReadFile(answerPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("listener overwrote the answer: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, packet.ViewPath)); !os.IsNotExist(err) {
		t.Fatalf("ordinary Pre must report missing entry material without reissuing: %v", err)
	}
	if stageSnapshot(t, root).Revision != beforeSnapshot.Revision {
		t.Fatal("missing-view Pre changed Runtime")
	}
	afterJournal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil || !bytes.Equal(beforeJournal, afterJournal) {
		t.Fatalf("missing-view Pre changed journal: %v", err)
	}
}

func questionnaireRecoveryShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// Observe a successful platform save and return the existing lifecycle API's
// JSON result. The Hook adds guidance, not another completion authority.
func s6SaveQuestionnaireHook(t *testing.T, root, cwd, owner, path string) string {
	result, _ := s6SaveQuestionnaireHookFeedback(t, root, cwd, owner, path)
	return result
}

func s6SaveQuestionnaireHookFeedback(t *testing.T, root, cwd, owner, path string) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Write", "agent_id": owner, "cwd": cwd, "tool_input": map[string]any{"file_path": path}})
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("saved questionnaire Hook failed: %d %s %s", code, &out, &errout)
	}
	var envelope struct {
		Output struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	context := envelope.Output.Context
	if !strings.Contains(context, path) {
		t.Fatalf("saved question path missing: %s", context)
	}
	index := strings.Index(context, "\n{")
	if index < 0 {
		t.Fatalf("original lifecycle result missing: %s; stderr: %s", context, &errout)
	}
	return context[index+1:], context
}
