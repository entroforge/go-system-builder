package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func audienceHook(t *testing.T, root string, payload map[string]any) string {
	t.Helper()
	body, _ := json.Marshal(payload)
	var out, diagnostic bytes.Buffer
	code := cli.Run([]string{"hook", "--root", root, "--event", payload["hook_event_name"].(string)}, bytes.NewReader(body), &out, &diagnostic)
	if code != 0 {
		t.Fatalf("Hook code=%d output=%s diagnostic=%s", code, &out, &diagnostic)
	}
	if strings.Contains(out.String(), "permissionDecision") {
		t.Fatalf("ordinary audience observation changed tool permission: %s", &out)
	}
	return out.String()
}

func TestHookAudienceSameCWDHelperCannotReceiveMainDesign(t *testing.T) {
	root, p := newDesignFixture(t, "none")
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, p.AnswerPath, designDraft(p))
	statePath := filepath.Join(root, ".claude/loop-state.json")
	before, _ := os.ReadFile(statePath)
	// Original CROSS-ROUTE #4: an explicit general-purpose SubagentStart
	// shares Main's directory but has no registered professional responsibility.
	for _, event := range []string{"SubagentStart", "PreToolUse", "PostToolUse"} {
		out := audienceHook(t, root, map[string]any{"hook_event_name": event, "session_id": "same-cwd-helper", "cwd": root, "agent_id": "a91273b3cd4b09ea1", "agent_type": "general-purpose", "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, p.AnswerPath)}})
		if out != "" {
			t.Fatalf("helper received Main control/questionnaire direction: %s", out)
		}
	}
	if after, _ := os.ReadFile(statePath); !bytes.Equal(before, after) {
		t.Fatal("helper save changed Main Runtime")
	}
	if _, found, err := runtime.FindSubmission(stageSnapshot(t, root).State, p.ID); err != nil || found {
		t.Fatalf("helper received Main answer: found=%t %v", found, err)
	}
	// The same actual save by the official top-level session (no agent_id)
	// still reaches the original consumer after native routing declines it.
	main := audienceHook(t, root, map[string]any{"hook_event_name": "PostToolUse", "session_id": "main-save", "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, p.AnswerPath)}})
	if !strings.Contains(main, "S2 SUBMISSION RECEIPT") {
		t.Fatalf("legal Main save was lost: %s", main)
	}
}

func TestHookAudienceUnknownAndBlankNestedContextDoNotClaimMain(t *testing.T) {
	root, p := newDesignFixture(t, "none")
	for _, payload := range []map[string]any{
		{"agent_id": "unregistered-actor"},
		{"agent_type": "general-purpose"},
		{"agent_transcript_path": "/not-read/by/audience.jsonl"},
	} {
		payload["hook_event_name"] = "PreToolUse"
		payload["cwd"] = root
		payload["session_id"] = "unknown"
		payload["tool_name"] = "Read"
		if out := audienceHook(t, root, payload); out != "" {
			t.Fatalf("unknown/nested actor received Main responsibility: %s", out)
		}
	}
	main := audienceHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "session_id": "legitimate-main", "tool_name": "Read"})
	if !strings.Contains(main, p.AnswerPath) {
		t.Fatalf("no-actor official Main lost initial reading paths: %s", main)
	}
	// Old callers may supply an empty Runtime.Agent projection. It supplies
	// no Worker identity and must not suppress an explicit actual Main event.
	explicitMain := audienceHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "session_id": "explicit-main-empty-agent", "agent_id": "Main", "tool_name": "Read", "runtime_context": map[string]any{"agent": map[string]any{}}})
	if !strings.Contains(explicitMain, p.AnswerPath) {
		t.Fatalf("empty Runtime.Agent suppressed explicit Main: %s", explicitMain)
	}
}

func TestHookAudienceLateS5WorkerRetainsOriginalResponsibilityAfterS2Return(t *testing.T) {
	root, packets := currentS5ReworkFixture(t)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range packets {
		verdict := "pass"
		if i == 1 {
			verdict = "fix_required"
		}
		draft := completedDraft(p, verdict)
		if verdict == "fix_required" { // Use the original valid finding helper shape.
			draft.Answer.Findings = append(draft.Answer.Findings, workpacket.Finding{Path: "docs/dev/tasks/TASK-039-01.md#scope", Expected: "all deliverables named", Observed: "a concrete missing output"})
		}
		stageJSON(t, root, p.AnswerPath, draft)
		receipt, err := service.Save(context.Background(), p.Binding.Owner, p.AnswerPath)
		if err != nil || !receipt.Accepted {
			t.Fatalf("original S5 reception: %+v %v", receipt, err)
		}
		settleOriginalS5Followup(t, root, p)
	}
	snap := stageSnapshot(t, root)
	design, err := runtime.CurrentDesignPacket(snap.State)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	out := audienceHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "session_id": "late-worker", "agent_id": packets[0].Binding.Owner, "tool_name": "Read"})
	if strings.Contains(out, design.AnswerPath) || strings.Contains(out, "LOOP RECOVERY") {
		t.Fatalf("late S5 Worker was assigned S2 Main work: %s", out)
	}
	if !strings.Contains(out, "retired/superseded") || !strings.Contains(out, packets[0].AnswerPath) {
		t.Fatalf("late Worker lost original responsibility: %s", out)
	}
	if after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json")); !bytes.Equal(before, after) {
		t.Fatal("late worker observation changed Runtime")
	}
}

func TestHookAudiencePendingEarlyReturnKeepsMainAndExcludesHelper(t *testing.T) {
	root, _ := newDesignFixture(t, "none")
	statePath := filepath.Join(root, ".claude/loop-state.json")
	before, _ := os.ReadFile(statePath)
	marker := statePath + ".commit-pending.json"
	pending := []byte("original pending operation")
	if err := os.WriteFile(marker, pending, 0600); err != nil {
		t.Fatal(err)
	}
	helper := audienceHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "session_id": "nested-pending", "agent_type": "general-purpose", "tool_name": "Read"})
	if helper != "" {
		t.Fatalf("blank nested actor received Main pending recovery: %s", helper)
	}
	main := audienceHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "session_id": "main-pending", "tool_name": "Read"})
	if !strings.Contains(main, "pending durable write") || !strings.Contains(main, "runtime stage-answer recover") {
		t.Fatalf("Main pending early return lost its original recovery: %s", main)
	}
	if after, _ := os.ReadFile(statePath); !bytes.Equal(before, after) {
		t.Fatal("pending observation changed Runtime")
	}
	if after, _ := os.ReadFile(marker); !bytes.Equal(pending, after) {
		t.Fatal("pending observation changed original durable operation")
	}
}
