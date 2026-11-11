package req039_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Unknown helpers sharing Main's cwd may perform ordinary tools, but cannot
// consume Main's ready design or mutate its Runtime and journal.
func TestUnknownHookAudienceCannotClaimReadyMainDesign(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	req039fixtures.SeedPlanningDesignComplete(t, root, state)
	writeSystemState(t, root, state)

	for _, identity := range []struct {
		name   string
		fields map[string]any
	}{
		{"unknown_actor", map[string]any{"agent_id": "agent-1"}},
		{"nested_type", map[string]any{"agent_type": "general-purpose"}},
		{"nested_transcript", map[string]any{"agent_transcript_path": "/not-read/helper.jsonl"}},
	} {
		t.Run(identity.name, func(t *testing.T) {
			for _, event := range []string{"PreToolUse", "PostToolUse"} {
				before := readSystemRuntimePair(t, root)
				payload := map[string]any{
					"cwd": root, "session_id": "unknown-" + identity.name,
					"hook_event_name": event, "tool_name": "Edit",
					"tool_input": map[string]any{"file_path": filepath.Join(root, "docs/architecture/ARCHITECTURE-039-loop-control-plane.md")},
				}
				for key, value := range identity.fields {
					payload[key] = value
				}
				raw, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				code, stdout, stderr := runHook(t, root, event, string(raw))
				if code != 0 || strings.TrimSpace(stdout) != "" {
					t.Fatalf("%s must allow ordinary Edit and remain silent: code=%d stdout=%s stderr=%s", event, code, stdout, stderr)
				}
				assertSystemRuntimePairUnchanged(t, before)
			}
		})
	}

	// The same source remains receivable by the actual Main session.
	code, stdout, stderr := runHook(t, root, "PreToolUse", req039fixtures.MainPreToolUseBody(
		root, "main-after-unknown", "Edit", map[string]any{"file_path": "docs/architecture/ARCHITECTURE-039-loop-control-plane.md"}))
	if code != 0 {
		t.Fatalf("Main Hook failed: code=%d stderr=%s", code, stderr)
	}
	qg := req039fixtures.ParseHookQualityGate(t, stdout)
	if !req039fixtures.TransitionCommitted(qg) || req039fixtures.CandidateTransition(qg) != "PTR-PLAN-01" {
		t.Fatalf("Main must still receive its ready design: %v", qg)
	}
	req039fixtures.AssertLifecycle(t, req039fixtures.ReadState(t, root), "planning", "contracts")
}
