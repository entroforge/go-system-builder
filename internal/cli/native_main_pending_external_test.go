package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/review"
)

func TestNativeMainPendingContinuesActualReviewPlanSave(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		event         string
		tool          string
		shouldReceive bool
	}{
		{name: "pre-receives-ready-original", event: "PreToolUse", tool: "Bash", shouldReceive: true},
		{name: "session-does-not-receive", event: "SessionStart"},
		{name: "read-post-does-not-save", event: "PostToolUse", tool: "Read"},
		{name: "write-post-receives", event: "PostToolUse", tool: "Write", shouldReceive: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := worktreeProjectRoot(t)
			path := review.PlanQuestionnairePath(root, stageSnapshot(t, root).State)
			entryQuestionnaireHook(t, root, "PreToolUse", path)
			authored := minimalReviewPlan(t, t.TempDir(), "review-plan-main-pending")
			data, err := os.ReadFile(authored)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			marker := nativeStopCommitPending(t, root)
			payload := map[string]any{"hook_event_name": scenario.event, "cwd": root, "session_id": "main-pending", "tool_name": scenario.tool}
			if scenario.tool == "Write" {
				payload["tool_input"] = map[string]any{"file_path": path}
				payload["tool_response"] = map[string]any{}
			}
			out := audienceHook(t, root, payload)
			if strings.Contains(out, "Automatic original Writer recovery could not complete") {
				t.Fatal(out)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("pending not retired: %v", err)
			}
			ptr := review.PlanPointerFromState(stageSnapshot(t, root).State)
			if scenario.shouldReceive {
				if ptr == nil || !strings.Contains(out, "review-plan: registered") {
					t.Fatalf("original plan not received: %s", out)
				}
			} else if ptr != nil {
				t.Fatalf("%s unexpectedly received plan: %s", scenario.name, out)
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(data, after) {
				t.Fatal("recovery changed original answer")
			}
			if scenario.tool == "Write" {
				nativeStopCommitPending(t, root)
				before := pendingRawRevision(t, root)
				out = audienceHook(t, root, payload)
				if strings.Contains(out, "review-plan: registered") || strings.Contains(out, "review-plan: revised") || stageSnapshot(t, root).Revision != before {
					t.Fatalf("replay was not idempotent: %s", out)
				}
			}
		})
	}
}

func TestNativeMainPendingPublicExcludesFailedPostAndOtherAudiences(t *testing.T) {
	root := worktreeProjectRoot(t)
	marker := nativeStopCommitPending(t, root)
	original, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	state, _ := os.ReadFile(statePath)
	for _, payload := range []map[string]any{
		{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write", "error": "actual failure"},
		{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Write", "tool_response": map[string]any{"is_error": true}},
		{"hook_event_name": "PreToolUse", "cwd": root, "agent_id": "opaque-worker", "agent_type": "general-purpose", "tool_name": "Read"},
	} {
		out := audienceHook(t, root, payload)
		if strings.Contains(out, "Main Hook recovered") {
			t.Fatalf("unrelated event recovered Main: %s", out)
		}
		if got, _ := os.ReadFile(marker); !bytes.Equal(got, original) {
			t.Fatalf("excluded event changed pending pair: %+v output=%s", payload, out)
		}
		if got, _ := os.ReadFile(statePath); !bytes.Equal(got, state) {
			t.Fatalf("excluded event changed state: %+v output=%s", payload, out)
		}
	}
}
