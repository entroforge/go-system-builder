package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestSavedS9OrdinaryCandidateKeepsMainAndSuccessfulPostBoundary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".claude/submissions/runtime/g1/S9-result/packet/answer.json")
	base := policy.Input{Event: "PostToolUse", AgentID: "opaque-platform-actor", CWD: root, ToolName: "Write", ToolInput: map[string]any{"file_path": path}}
	for _, name := range []string{"main", "pre", "failed", "tool_failed", "read", "nonfamily"} {
		t.Run(name, func(t *testing.T) {
			in := base
			switch name {
			case "main":
				in.AgentID = "Main"
			case "pre":
				in.Event = "PreToolUse"
			case "failed":
				in.Error = "save failed"
			case "tool_failed":
				in.ToolResponse = map[string]any{"is_error": true}
			case "read":
				in.ToolName = "Read"
			case "nonfamily":
				in.ToolInput = map[string]any{"file_path": filepath.Join(root, "answer.json")}
			}
			var out, diagnostic bytes.Buffer
			if handled, code := runSavedS9RepairResultHook(root, in, &out, &diagnostic); handled || code != 0 || out.Len() != 0 || diagnostic.Len() != 0 {
				t.Fatalf("ineligible candidate handled: %t %d %s %s", handled, code, &out, &diagnostic)
			}
		})
	}
	// An explicit response path follows the same normalization as an input
	// path. A missing Runtime must produce rejection feedback, never silence.
	base.ToolName = "mcp__example__save"
	base.ToolInput = nil
	base.ToolResponse = map[string]any{"file_path": ".claude/submissions/runtime/g1/S9-result/packet/answer.json"}
	var out, diagnostic bytes.Buffer
	if handled, code := runSavedS9RepairResultHook(root, base, &out, &diagnostic); !handled || code != 0 || !strings.Contains(out.String(), path) || !strings.Contains(out.String(), "not_accepted") || !strings.Contains(diagnostic.String(), "S9 RepairResult not accepted") {
		t.Fatalf("response-path error lost: %t %d %s %s", handled, code, &out, &diagnostic)
	}
}
