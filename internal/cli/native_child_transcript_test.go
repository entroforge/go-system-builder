package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestNativeChildTranscriptUsesObservedActorSiblingOnly(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "platform-session.jsonl")
	in := policy.Input{Event: "PreToolUse", AgentID: "a123-native", TranscriptPath: parent}
	child := filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-"+in.AgentID+".jsonl")
	write := func(path, actor string, side bool) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"type": "user", "isSidechain": side, "agentId": actor, "message": map[string]any{"role": "user", "content": "exact professional responsibility"}})
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Even a parent quoting the right responsibility and metadata cannot stand
	// in for the actual child file selected by this event's agent_id.
	write(parent, in.AgentID, true)
	if path, ok := nativeChildTranscript(in); ok {
		t.Fatalf("parent substituted for missing child: %s", path)
	}
	write(child, "another-child", true)
	if _, ok := nativeChildTranscript(in); ok {
		t.Fatal("wrong platform actor accepted")
	}
	write(child, in.AgentID, false)
	if _, ok := nativeChildTranscript(in); ok {
		t.Fatal("top-level transcript accepted")
	}
	write(child, in.AgentID, true)
	if path, ok := nativeChildTranscript(in); !ok || path != child {
		t.Fatalf("actual child was not resolved: %s %t", path, ok)
	}
	for _, actor := range []string{"", "Main", "../another", "a/b", "a\\b"} {
		in.AgentID = actor
		if _, ok := nativeChildTranscript(in); ok {
			t.Fatalf("invalid child actor accepted: %q", actor)
		}
	}
}
