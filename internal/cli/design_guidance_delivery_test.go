package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func designGuidanceEvent(t *testing.T, root, session, event, tool string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"hook_event_name": event, "session_id": session, "cwd": root, "tool_name": tool, "tool_input": map[string]any{"file_path": "docs/architecture/ARCHITECTURE-039.md"}})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("%s: %d %s %s", event, code, &out, &diagnostic)
	}
	if strings.Contains(out.String(), "permissionDecision") {
		t.Fatalf("ordinary design guidance changed tool permissions: %s", &out)
	}
	return out.String()
}

func TestS2GuidanceDeliveryReducesRepeatsAndRestoresChangedContext(t *testing.T) {
	root, packet := newDesignFixture(t, "none")
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	before, _ := os.ReadFile(statePath)
	journal, _ := os.ReadFile(journalPath)
	first := designGuidanceEvent(t, root, "design-session", "PreToolUse", "Read")
	if !strings.Contains(first, packet.AnswerPath) {
		t.Fatal("first delivery lacks exact answer path")
	}
	for _, tool := range []string{"Read", "Write", "Bash", "mcp__sandbox__execute"} {
		repeated := designGuidanceEvent(t, root, "design-session", "PreToolUse", tool)
		if !strings.Contains(repeated, "guidance is unchanged") || len(repeated) >= len(first)/2 {
			t.Fatalf("ordinary %s repeated full context: first=%d repeated=%d %s", tool, len(first), len(repeated), repeated)
		}
	}
	// Author changes are discovered read-only and must get their new feedback.
	stageJSON(t, root, packet.AnswerPath, designDraft(packet))
	changed := designGuidanceEvent(t, root, "design-session", "PreToolUse", "Read")
	if strings.Contains(changed, "guidance is unchanged") || !strings.Contains(changed, packet.AnswerPath) {
		t.Fatalf("changed answer feedback suppressed: %s", changed)
	}
	if _, found, err := runtime.FindSubmission(stageSnapshot(t, root).State, packet.ID); err != nil || found {
		t.Fatalf("ordinary observation received an answer: %v %v", found, err)
	}
	if got, _ := os.ReadFile(statePath); !bytes.Equal(got, before) {
		t.Fatal("delivery cache changed Runtime")
	}
	if got, _ := os.ReadFile(journalPath); !bytes.Equal(got, journal) {
		t.Fatal("delivery cache changed business journal")
	}
	for _, event := range []string{"SessionStart", "PreCompact"} {
		resumed := designGuidanceEvent(t, root, "design-session", event, "Read")
		if event == "SessionStart" && !strings.Contains(resumed, packet.AnswerPath) {
			t.Fatalf("%s lost current paths: %s", event, resumed)
		}
		if event == "PreCompact" && resumed != "" {
			t.Fatalf("PreCompact should only reset presentation, not emit substitute context: %s", resumed)
		}
		full := designGuidanceEvent(t, root, "design-session", "PreToolUse", "Read")
		if strings.Contains(full, "guidance is unchanged") || !strings.Contains(full, packet.AnswerPath) {
			t.Fatalf("%s did not reset delivery: %s", event, full)
		}
	}
	for _, session := range []string{"new-session", "", ""} {
		full := designGuidanceEvent(t, root, session, "PreToolUse", "Read")
		if !strings.Contains(full, packet.AnswerPath) {
			t.Fatalf("new or absent session lost context: %s", full)
		}
	}
	if err := os.Remove(filepath.Join(root, packet.ViewPath)); err != nil {
		t.Fatal(err)
	}
	missing := designGuidanceEvent(t, root, "design-session", "PreToolUse", "Read")
	if strings.Contains(missing, "guidance is unchanged") || !strings.Contains(missing, packet.ViewPath) || !strings.Contains(missing, "no such file") {
		t.Fatalf("new missing-file diagnostic was hidden: %s", missing)
	}
	t.Logf("S2 guidance: first=%d bytes; unchanged=%d bytes", len(first), len(designGuidanceEvent(t, root, "design-session", "PreToolUse", "Read")))
}

func TestS2ReceivedProjectionKeepsActualGapWithoutStaticReceptionMissing(t *testing.T) {
	root, packet := newDesignFixture(t, "unknown")
	stageJSON(t, root, packet.AnswerPath, designDraft(packet))
	observedQuestionnaireHook(t, root, "Write", map[string]any{"file_path": filepath.Join(root, packet.AnswerPath)}, map[string]any{})
	if record, found, err := runtime.FindSubmission(stageSnapshot(t, root).State, packet.ID); err != nil || !found || record.ControlStatus == "advanced" {
		t.Fatalf("fixture must be received with a pending actual gap: %+v %v", record, err)
	}
	context := designGuidanceEvent(t, root, "received", "PreToolUse", "Write")
	if strings.Contains(context, "architecture_record") || !strings.Contains(context, "already recorded") || !strings.Contains(context, "UI impact remains unknown") {
		t.Fatalf("receipt and actual gap disagree with static projection: %s", context)
	}
}
