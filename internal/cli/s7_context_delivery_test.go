package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestS7ExactNativeStartAndChangedMainContext(t *testing.T) {
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	plan := minimalReviewPlan(t, root, "review-plan-guidance")
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	stageCLI(t, "runtime", "review-plan", "--root", root, "--file", plan)
	markAssignmentDispatched(t, root, "assignment-dv-1", "reviewer-guidance-dv")
	markAssignmentDispatched(t, root, "assignment-qa-1", "reviewer-guidance-qa")
	registered := stageSnapshot(t, root)
	for _, raw := range registered.State["entities"].(map[string]any)["agents"].([]any) {
		raw.(map[string]any)["dispatch_mode"] = "plan_checkpoint"
	}
	stageJSON(t, root, ".claude/loop-state.json", registered.State)

	var packets []workpacket.ReviewResultPacket
	issued := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")
	if err := json.Unmarshal([]byte(issued), &packets); err != nil || len(packets) != 2 {
		t.Fatalf("issue: %s %v", issued, err)
	}
	before := stageSnapshot(t, root).Revision
	call := func(event, agent, text string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"hook_event_name": event, "agent_id": agent, "session_id": "s7-native", "cwd": root, "last_assistant_message": text})
		var out, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
			t.Fatalf("%s: %d %s %s", event, code, &out, &diagnostic)
		}
		return out.String()
	}
	ref := filepath.Join(root, packets[0].ViewPath)
	for _, agent := range []string{"Explore", "qa", "unknown-native"} {
		if got := call("SubagentStart", agent, "Explore the project"); got != "" {
			t.Fatalf("unknown Start got formal questionnaire: %s", got)
		}
	}
	for _, tc := range []struct{ agent, text string }{{"opaque-native", ref}, {packets[0].Binding.OwnerAgentID, "continue assigned work"}, {packets[0].Binding.OwnerAgentID, ref}} {
		if got := call("SubagentStart", tc.agent, tc.text); !strings.Contains(got, packets[0].ViewPath) || !strings.Contains(got, packets[0].AnswerPath) {
			t.Fatalf("exact Start lost references: %s", got)
		}
	}
	if got := call("SubagentStart", packets[1].Binding.OwnerAgentID, ref); !strings.Contains(got, "responsibility mismatch") || !strings.Contains(got, packets[0].ID) || !strings.Contains(got, packets[1].ID) || strings.Contains(got, "Read that exact original questionnaire") {
		t.Fatalf("exact reference overrode another current owner binding: %s", got)
	}
	if got := call("PreToolUse", packets[1].Binding.OwnerAgentID, ref); got != "" {
		t.Fatalf("ordinary worker tool was affected by a handoff mismatch: %s", got)
	}
	if got := call("SubagentStart", "opaque-native", ref+"\n"+filepath.Join(root, packets[1].ViewPath)); got != "" {
		t.Fatalf("ambiguous Start selected responsibility: %s", got)
	}
	first := designGuidanceEvent(t, root, "s7-context", "PreToolUse", "Read")
	for _, tool := range []string{"Read", "Bash", "Write", "mcp__sandbox__execute"} {
		next := designGuidanceEvent(t, root, "s7-context", "PreToolUse", tool)
		if !strings.Contains(next, "S7 guidance is unchanged") || len(next) >= len(first)/2 {
			t.Fatalf("%s repeated S7 guidance: first=%d next=%d %s", tool, len(first), len(next), next)
		}
	}
	// Stop stores its actual rejection. Main observation must not re-read the
	// private draft to replace that cause with a fresh validation.
	call("SubagentStop", "opaque-native", ref)
	changed := designGuidanceEvent(t, root, "s7-context", "PreToolUse", "Read")
	if strings.Contains(changed, "guidance is unchanged") || !strings.Contains(changed, "Last S7 native Stop") || !strings.Contains(changed, packets[0].ID) {
		t.Fatalf("new Stop error hidden: %s", changed)
	}
	if err := os.WriteFile(filepath.Join(root, packets[0].AnswerPath), []byte("private unreadable draft"), 0600); err != nil {
		t.Fatal(err)
	}
	observed := mainNativeReturn(t, root, ref)
	if !strings.Contains(observed, "Last S7 native Stop") || strings.Contains(observed, "invalid character") {
		t.Fatalf("Main read private unaccepted draft: %s", observed)
	}
	for _, event := range []string{"SessionStart", "PreCompact"} {
		designGuidanceEvent(t, root, "s7-context", event, "Read")
		restored := designGuidanceEvent(t, root, "s7-context", "PreToolUse", "Read")
		if strings.Contains(restored, "guidance is unchanged") || !strings.Contains(restored, packets[0].ViewPath) || !strings.Contains(restored, "Last S7 native Stop") {
			t.Fatalf("%s lost recovery context: %s", event, restored)
		}
	}
	if stageSnapshot(t, root).Revision != before {
		t.Fatal("presentation or rejected Stop changed Runtime")
	}
}
