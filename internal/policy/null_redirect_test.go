package policy_test

import (
	"github.com/entroforge/go-system-builder/internal/policy"
	"testing"
)

func TestNullRedirectionPreservesFrozenProductBoundary(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	root := protectedReleaseRoot(t)
	t.Chdir(root)
	for _, state := range []string{"release_audit", "acceptance", "bug_resolution", "verification"} {
		for _, tc := range []struct {
			cmd  string
			deny bool
		}{
			{"git diff --stat >/dev/null", false},
			{"git diff 2> '/dev/null'", false},
			{"git diff >>\"/dev/null\"", false},
			{"git diff >docs/reports/diff.txt 2>/dev/null", false},
			{"git diff >server/result.txt", true},
			{"git diff >server/result.txt 2>/dev/null", true},
			{"rm server/example.go >/dev/null", true},
			{"cp source server/example.go >/dev/null", true},
			{"python3 script.py >/dev/null", true},
			{"python3 -c 'print(1)' >/dev/null", true},
			{"git diff >/dev/null; rm server/example.go", true},
			{"git diff >/dev/null && rm server/example.go", true},
			{"echo $(rm server/example.go) >/dev/null", true},
			{"rm /dev/null", true},
			{"bash -c 'rm server/example.go' >/dev/null", true},
			{"custom-writer >/dev/null", true},
			{"cat <(rm server/example.go) >/dev/null", true},
			{"file -C -m server/magic >/dev/null", true},
			{"find server -delete >/dev/null", true},
			{"git diff --output=server/result.txt >/dev/null", true},
			{"rg --pre=custom-writer pattern >/dev/null", true},
			{"git diff >/dev/null.backup", true},
		} {
			t.Run(state+"/"+tc.cmd, func(t *testing.T) {
				d, err := engine.Evaluate(policy.Input{Event: "PreToolUse", ToolName: "Bash", ToolInput: map[string]any{"command": tc.cmd}, Runtime: policy.RuntimeContext{ProjectRoot: root, CurrentState: state}})
				if err != nil {
					t.Fatal(err)
				}
				denied := d.Decision == "deny" || d.Decision == "block"
				if denied != tc.deny {
					t.Fatalf("deny=%v, want %v: %+v", denied, tc.deny, d)
				}
			})
		}
	}
}
