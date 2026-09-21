package policy_test

import (
	"github.com/entroforge/go-system-builder/internal/policy"
	"path/filepath"
	"testing"
)

func TestRetiredProductMutationIsDeniedBeforeCreation(t *testing.T) {
	root := protectedReleaseRoot(t)
	engine := loadRepositoryPolicy(t)
	for _, tc := range []struct {
		name, tool, path, command, cwd string
		deny                           bool
	}{
		{name: "write", tool: "Write", path: "docs/product/requirements/REQ-001.md", deny: true},
		{name: "edit", tool: "Edit", path: "docs/product/README.md", deny: true},
		{name: "multi edit", tool: "MultiEdit", path: "docs/product/README.md", deny: true},
		{name: "notebook", tool: "NotebookEdit", path: "docs/product/test.ipynb", deny: true},
		{name: "absolute", tool: "Write", path: filepath.Join(root, "docs/product/requirements/REQ-001.md"), deny: true},
		{name: "normalized", tool: "Write", path: "docs/requirements/../product/REQ-001.md", deny: true},
		{name: "nested cwd", tool: "Write", cwd: filepath.Join(root, "docs"), path: "product/REQ-001.md", deny: true},
		{name: "git restore", tool: "Bash", command: "git restore -- docs/product/requirements/REQ-001.md", deny: true},
		{name: "mkdir", tool: "Bash", command: "mkdir -p docs/product/requirements", deny: true},
		{name: "redirect", tool: "Bash", command: "echo text > docs/product/REQ-001.md", deny: true},
		{name: "read", tool: "Read", path: "docs/product/README.md"},
		{name: "bash read", tool: "Bash", command: "cat docs/product/README.md"},
		{name: "new layout", tool: "Write", path: "docs/requirements/REQ-001.md"},
		{name: "unrelated prefix", tool: "Write", path: "docs/productivity/README.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := tc.cwd
			if cwd == "" {
				cwd = root
			}
			input := map[string]any{"file_path": tc.path}
			if tc.command != "" {
				input = map[string]any{"command": tc.command}
			}
			decision, err := engine.Evaluate(policy.Input{Event: "PreToolUse", ToolName: tc.tool, ToolInput: input, CWD: cwd, Runtime: policy.RuntimeContext{ProjectRoot: root, CurrentState: "planning", CurrentStage: "S0"}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.deny {
				if decision.Decision != "deny" || decision.RuleID != policy.RuleRetiredLayoutWrite {
					t.Fatalf("old path not rejected: %+v", decision)
				}
			} else if decision.Decision != "allow" {
				t.Fatalf("valid/read-only path rejected: %+v", decision)
			}
		})
	}
}
