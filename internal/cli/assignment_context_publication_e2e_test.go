package cli_test

import (
	"bytes"
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/cli"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeContextHookRegistersActualPathsOnceAndPreservesOccupants(t *testing.T) {
	root, taskID, taskPath := newS6RecordContextFixture(t)
	stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", ".claude/workgroups/manifest-s6-native-context.json", "--task-id", taskID, "--task", taskPath)
	snap := stageSnapshot(t, root)
	owner := "agent-s6-native-context"
	runtimeID := snap.State["runtime_id"].(string)
	occupiedContext := filepath.Join(root, ".claude/assignment-context/native")
	if err := os.MkdirAll(filepath.Dir(occupiedContext), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(occupiedContext, []byte("unknown context"), 0600); err != nil {
		t.Fatal(err)
	}
	occupiedResult := filepath.Join(root, ".claude/evidence", runtimeID, "g1/assignments", owner, "a1/completion.json")
	if err := os.MkdirAll(filepath.Dir(occupiedResult), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(occupiedResult, []byte("unknown answer"), 0600); err != nil {
		t.Fatal(err)
	}
	// Unknown owners and ordinary tool events remain neutral and do not bind a context.
	for _, event := range []string{"SubagentStart", "PreToolUse"} {
		body, _ := json.Marshal(map[string]any{"hook_event_name": event, "agent_id": "unknown-agent", "cwd": root, "tool_name": "Read", "tool_input": map[string]any{"file_path": "docs/requirements/REQ-039-loop-control-plane.md"}})
		var out, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
			t.Fatalf("unrelated event blocked: %d %s", code, diagnostic.String())
		}
		if strings.Contains(out.String(), "permissionDecision\":\"deny") {
			t.Fatalf("unrelated tool denied: %s", out.String())
		}
	}
	if got := stageSnapshot(t, root).Revision; got != snap.Revision {
		t.Fatalf("unknown identity created a context: %d -> %d", snap.Revision, got)
	}
	call := func() (string, int, string) {
		body, _ := json.Marshal(map[string]any{"hook_event_name": "SessionStart", "agent_id": owner, "cwd": root})
		var out, diagnostic bytes.Buffer
		code := cli.Run([]string{"hook", "--root", root, "--event", "SessionStart"}, bytes.NewReader(body), &out, &diagnostic)
		return out.String(), code, diagnostic.String()
	}
	text, code, diag := call()
	if code != 0 {
		t.Fatalf("hook %d %s", code, diag)
	}
	if !strings.Contains(text, "Assignment sources:") {
		t.Fatalf("natural context was not offered: %s %s", text, diag)
	}
	after := stageSnapshot(t, root)
	var row map[string]any
	for _, raw := range after.State["entities"].(map[string]any)["agents"].([]any) {
		a := raw.(map[string]any)
		if a["id"] == owner {
			row = a
		}
	}
	binding, ok := row["context_binding"].(map[string]any)
	if !ok {
		t.Fatalf("no original context binding: %+v", row)
	}
	ref := binding["context_ref"].(map[string]any)["path"].(string)
	raw, err := os.ReadFile(filepath.Join(root, ref))
	if err != nil {
		t.Fatal(err)
	}
	var context map[string]any
	if err := json.Unmarshal(raw, &context); err != nil {
		t.Fatal(err)
	}
	result := context["result_path"].(string)
	if !strings.Contains(result, "native-result-candidate-") {
		t.Fatalf("unknown result claimed: %s", result)
	}
	for _, pair := range []struct{ path, value string }{{occupiedContext, "unknown context"}, {occupiedResult, "unknown answer"}} {
		data, err := os.ReadFile(pair.path)
		if err != nil || string(data) != pair.value {
			t.Fatalf("unknown file changed: %s %v", pair.path, err)
		}
	}
	_, code, diag = call()
	if code != 0 {
		t.Fatalf("replay %d: %s", code, diag)
	}
	again := stageSnapshot(t, root)
	if again.Revision != after.Revision {
		t.Fatalf("replay created another binding: %d -> %d", after.Revision, again.Revision)
	}
}
