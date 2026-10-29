package cli

import (
	"context"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestHookReaderUsesOfficialEventAndExplicitNestedContext(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "SessionStart", "Stop"} {
		if !hookReaderIsMain(policy.Input{Event: event}) {
			t.Fatalf("official top-level %s requires no agent_id", event)
		}
	}
	for _, event := range []string{"Idle", ""} {
		if !hookReaderIsMain(policy.Input{Event: event, AgentID: "Main"}) {
			t.Fatalf("explicit Main was lost at legacy/direct event %q", event)
		}
		if hookReaderIsMain(policy.Input{Event: event}) {
			t.Fatalf("blank actor was inferred Main at legacy/direct event %q", event)
		}
	}
	for _, in := range []policy.Input{
		{}, {Event: "SubagentStart"}, {Event: "SubagentStart", AgentID: "Main"},
		{Event: "PreToolUse", AgentType: "general-purpose"},
		{Event: "PreToolUse", AgentTranscriptPath: "/worker/transcript"},
		{Event: "PreToolUse", AgentID: "unregistered"},
		{Event: "PreToolUse", TeammateName: "Main"},
		{Event: "PreToolUse", Runtime: policy.RuntimeContext{Agent: &policy.AgentContext{ID: "original-worker"}}},
	} {
		if hookReaderIsMain(in) {
			t.Fatalf("non-Main event classified Main: %+v", in)
		}
	}
}

func TestHookReaderCachesOriginalPayloadAndDoesNotRedefaultOnFallback(t *testing.T) {
	root := t.TempDir()
	original := policy.Input{Event: "SubagentStart", AgentID: "a91273b3cd4b09ea1", AgentType: "general-purpose", CWD: root}
	in := withHookReader(root, original)
	if in.AgentID != original.AgentID || in.Event != original.Event || in.AgentType != original.AgentType {
		t.Fatal("reader rewrote original platform payload")
	}
	reader, ok := contextForHook(in).Value(hookReaderKey{}).(*hookReader)
	if !ok || reader.Kind != "Helper" || reader.Actor != original.AgentID {
		t.Fatalf("wrong actual reader: %+v", reader)
	}
	// A native adapter can decline and a fallback can see less payload. Cached
	// audience evidence must still win over the otherwise legal Main event.
	in.Event = "PreToolUse"
	in.AgentID = ""
	in.AgentType = ""
	if hookReaderIsMain(in) {
		t.Fatal("fallback reclassified cached helper as Main")
	}
	if again := withHookReader(root, in); again.Context != in.Context {
		t.Fatal("same request repeated reader/session resolution")
	}
	unknown := policy.Input{Context: context.WithValue(context.Background(), hookReaderKey{}, &hookReader{Kind: "Unknown"}), Event: "PreToolUse"}
	if hookReaderIsMain(unknown) {
		t.Fatal("unknown cached audience was inferred Main")
	}
}
