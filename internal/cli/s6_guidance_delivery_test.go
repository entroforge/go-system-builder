package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

func TestS6NativeMainGuidanceReducesRepeatedToolsAndRestoresContext(t *testing.T) {
	root, _, record := nativeDeliveryFixture(t)
	brief, _, _ := assignmentrecord.NativePaths(record)
	first := designGuidanceEvent(t, root, "s6-main-guidance", "PreToolUse", "Read")
	if !strings.Contains(first, brief) {
		t.Fatalf("first observation lost the current exact brief: %s", first)
	}
	// The first worktree advisory has its own one-time delivery. Allow its
	// disappearance to establish the stable responsibility presentation.
	designGuidanceEvent(t, root, "s6-main-guidance", "PreToolUse", "Read")
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	state, _ := os.ReadFile(statePath)
	journal, _ := os.ReadFile(journalPath)
	for _, tool := range []string{"Read", "Write", "Bash", "mcp__sandbox__execute"} {
		repeated := designGuidanceEvent(t, root, "s6-main-guidance", "PreToolUse", tool)
		if !strings.Contains(repeated, "S6 guidance is unchanged") || len(repeated) >= len(first)/2 {
			t.Fatalf("%s repeated full responsibility: first=%d repeated=%d %s", tool, len(first), len(repeated), repeated)
		}
	}
	if after, _ := os.ReadFile(statePath); !bytes.Equal(after, state) {
		t.Fatal("unchanged delivery mutated Runtime")
	}
	if after, _ := os.ReadFile(journalPath); !bytes.Equal(after, journal) {
		t.Fatal("unchanged delivery mutated the business journal")
	}
	// A new stage entry must re-deliver even if its original records and
	// human-readable missing items happen to match a previous observation.
	snapshot := stageSnapshot(t, root)
	snapshot.State["lifecycle"].(map[string]any)["phase_revision"] = snapshot.Revision + 1
	snapshot.State["revision"] = snapshot.Revision + 1
	stageJSON(t, root, ".claude/loop-state.json", snapshot.State)
	reentered := designGuidanceEvent(t, root, "s6-main-guidance", "PreToolUse", "Read")
	if strings.Contains(reentered, "guidance is unchanged") || !strings.Contains(reentered, brief) {
		t.Fatalf("new stage entry borrowed old presentation: %s", reentered)
	}
	for _, event := range []string{"SessionStart", "PreCompact"} {
		designGuidanceEvent(t, root, "s6-main-guidance", event, "Read")
		full := designGuidanceEvent(t, root, "s6-main-guidance", "PreToolUse", "Read")
		if strings.Contains(full, "guidance is unchanged") || !strings.Contains(full, brief) {
			t.Fatalf("%s failed to restore the original responsibility: %s", event, full)
		}
	}
	for _, session := range []string{"new-main-session", "", ""} {
		full := designGuidanceEvent(t, root, session, "PreToolUse", "Read")
		if !strings.Contains(full, brief) {
			t.Fatalf("new or absent session lost the responsibility: %s", full)
		}
	}
	t.Logf("S6 first=%d bytes; repeated=%d bytes", len(first), len(designGuidanceEvent(t, root, "s6-main-guidance", "PreToolUse", "Read")))
}

func TestS6NewBlockedReturnRestoresMainGuidance(t *testing.T) {
	root, source, record := nativeDeliveryFixture(t)
	brief, _, result := assignmentrecord.NativePaths(record)
	for i := 0; i < 3; i++ {
		designGuidanceEvent(t, root, "s6-blocked-guidance", "PreToolUse", "Read")
	}
	marker := "Actual fixture service is unavailable; restore its endpoint before resuming this TASK."
	stageJSON(t, source, result, map[string]any{
		"summary": marker, "risks_blockers": []string{marker},
		"closing_assessment": map[string]any{"disposition": "blocked"},
	})
	if code, text := nativeEvent(t, root, source, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(source, result), false); code != 0 {
		t.Fatalf("blocked handoff: %d %s", code, text)
	}
	changed := designGuidanceEvent(t, root, "s6-blocked-guidance", "PreToolUse", "Read")
	if strings.Contains(changed, "guidance is unchanged") || !strings.Contains(changed, marker) || !strings.Contains(changed, result) {
		t.Fatalf("new actual blocker disappeared behind observation caching: %s", changed)
	}
}
