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
	// Complete authored bytes do not turn a Pre/SessionStart into delivery.
	for _, event := range []string{"PreToolUse", "SessionStart", "PostToolUse"} {
		marker := nativeStopFingerprintPending(t, root)
		out := audienceHook(t, root, map[string]any{"hook_event_name": event, "cwd": root, "session_id": "main-pending", "tool_name": "Read"})
		if strings.Contains(out, "Automatic original Writer recovery could not complete") {
			t.Fatal(out)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("pending not retired: %v", err)
		}
		if review.PlanPointerFromState(stageSnapshot(t, root).State) != nil {
			t.Fatal("observation received complete unsaved plan")
		}
	}
	// This successful save is the actual original delivery event.
	marker := nativeStopFingerprintPending(t, root)
	out := audienceHook(t, root, map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "session_id": "main-pending", "tool_name": "Write", "tool_input": map[string]any{"file_path": path}, "tool_response": map[string]any{}})
	if !strings.Contains(out, "review-plan: registered") {
		t.Fatalf("same save was swallowed: %s", out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("pending not retired: %v", err)
	}
	if review.PlanPointerFromState(stageSnapshot(t, root).State) == nil {
		t.Fatal("actual save not received")
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(data, after) {
		t.Fatal("recovery changed original answer")
	}
	// Recovered repeat save preserves the accepted plan and its revision.
	before := stageSnapshot(t, root).Revision
	nativeStopFingerprintPending(t, root)
	out = audienceHook(t, root, map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "session_id": "main-pending", "tool_name": "Write", "tool_input": map[string]any{"file_path": path}})
	if !strings.Contains(out, "same ReviewPlan was already received") || stageSnapshot(t, root).Revision != before {
		t.Fatalf("replay was not idempotent: %s", out)
	}
}

func TestNativeMainPendingPublicExcludesFailedPostAndOtherAudiences(t *testing.T) {
	root := worktreeProjectRoot(t)
	marker := nativeStopFingerprintPending(t, root)
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
