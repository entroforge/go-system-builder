// protected_release_test.go locks RC-06 (S10-3): the data-driven
// protected-commands table (docs/control/protected-commands.json) is
// wired into the PreToolUse enforce path via protectedReleaseDecision.
// Previously classifier.MatchProtectedCommands was reachable only from
// tests — the table was documentation, not enforcement.
package policy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

// protectedReleaseRoot returns the repository root (the only tree that ships
// the real protected_commands table).
func protectedReleaseRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(abs, "docs", "control", "protected-commands.json")); err != nil {
		t.Fatalf("protected_commands table missing at repo root: %v", err)
	}
	return abs
}

// TestProtectedCommandsBlocksPushInS10 is the RC-06 negative case: a release
// push from the S10 stage must be hard-denied by the protected-release rule,
// not silently allowed because the matcher was never wired.
func TestProtectedCommandsBlocksPushInS10(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	decision, err := engine.Evaluate(policy.Input{
		Event:    "PreToolUse",
		ToolName: "Bash",
		ToolInput: map[string]any{
			"command": "git push origin main",
		},
		Runtime: policy.RuntimeContext{
			RuntimeID:    "loop-REQ-039",
			ProjectRoot:  protectedReleaseRoot(t),
			CurrentState: "awaiting_human_release",
			CurrentStage: "s10_release_audit",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "deny" {
		t.Fatalf("git push origin main must be denied, got %s (rule %s)", decision.Decision, decision.RuleID)
	}
	if decision.RuleID != policy.RuleProtectedReleaseCommand {
		t.Fatalf("rule id = %q, want %q", decision.RuleID, policy.RuleProtectedReleaseCommand)
	}
}

// TestProtectedCommandsBlocksFormalReleaseChannels extends the S10-3 barrier
// to the other protected release channels declared in the table.
func TestProtectedCommandsBlocksFormalReleaseChannels(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	cases := []struct {
		name    string
		command string
	}{
		{"push master", "git push origin master"},
		{"push release branch", "git push origin release/1.2"},
		{"gh release create", "gh release create v1.2.0 --notes x"},
		{"npm publish", "npm publish"},
		{"goreleaser release", "goreleaser release --clean"},
		{"terraform apply", "terraform apply -auto-approve"},
		{"kubectl apply", "kubectl apply -f deploy.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := engine.Evaluate(policy.Input{
				Event:     "PreToolUse",
				ToolName:  "Bash",
				ToolInput: map[string]any{"command": tc.command},
				Runtime: policy.RuntimeContext{
					ProjectRoot:  protectedReleaseRoot(t),
					CurrentStage: "s10_release_audit",
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if decision.Decision != "deny" {
				t.Fatalf("%s must be denied, got %s", tc.command, decision.Decision)
			}
		})
	}
}

// TestProtectedCommandsAllowsOrdinaryBash proves the rule is a real matcher
// over the table, not an unconditional Bash deny: ordinary read/build
// commands stay allowed while a runtime root is supplied.
func TestProtectedCommandsAllowsOrdinaryBash(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "go test ./internal/policy"},
		Runtime: policy.RuntimeContext{
			ProjectRoot: protectedReleaseRoot(t),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision == "deny" && decision.RuleID == policy.RuleProtectedReleaseCommand {
		t.Fatalf("ordinary Bash must not trip the protected-release rule: %#v", decision)
	}
}

// TestProtectedCommandsFailClosedOnBrokenTable locks the fail-closed branch:
// a root whose protected_commands table is unreadable denies unclassified
// Bash instead of letting it through.
func TestProtectedCommandsFailClosedOnBrokenTable(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "control", "protected-commands.json"),
		[]byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := loadRepositoryPolicy(t)
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "git push origin main"},
		Runtime:   policy.RuntimeContext{ProjectRoot: root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "deny" {
		t.Fatal("broken protected_commands table must fail closed")
	}
	if !strings.Contains(decision.Reason, "unreadable") {
		t.Fatalf("reason must explain the unreadable table, got %q", decision.Reason)
	}
}

func TestProtectedCommandsEmptyTableFailsClosed(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "docs/control"), 0755)
	os.WriteFile(filepath.Join(root, "docs/control/protected-commands.json"), []byte("[]"), 0644)
	decision, err := loadRepositoryPolicy(t).Evaluate(policy.Input{Event: "PreToolUse", ToolName: "Bash", ToolInput: map[string]any{"command": "git push origin main"}, Runtime: policy.RuntimeContext{ProjectRoot: root}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "deny" {
		t.Fatalf("empty table must deny: %+v", decision)
	}
}
func TestReworkPathsUseCanonicalLayout(t *testing.T) {
	for kind, base := range map[string]string{"req": "docs/requirements", "contract": "docs/dev/contracts", "task": "docs/dev/tasks", "architecture": "docs/architecture"} {
		want := filepath.Join(base, "versions/REQ-001/g2/artifact.md")
		if got := policy.ReworkPath(kind, "REQ-001", 2, "artifact.md"); got != want {
			t.Fatalf("%s: %s != %s", kind, got, want)
		}
	}
}

func TestReworkUnknownKindDoesNotInventDirectory(t *testing.T) {
	if got := policy.ReworkPath("unknown", "REQ-001", 2, "file.md"); got != "" {
		t.Fatal(got)
	}
}

func TestLockedArchitectureReworkUsesArchitectureRoot(t *testing.T) {
	d, err := loadRepositoryPolicy(t).Evaluate(policy.Input{Event: "PreToolUse", ToolName: "Edit", ToolInput: map[string]any{"file_path": "docs/architecture/ARCHITECTURE-001.md"}, Runtime: policy.RuntimeContext{BoundREQID: "REQ-001", CurrentState: "planning", CurrentPhase: "design", LockedArtifacts: []policy.LockedArtifact{{ID: "ARCH-001", Kind: "design", Path: "docs/architecture/ARCHITECTURE-001.md", Version: "v1", SHA256: strings.Repeat("a", 64), LockedFromStage: "S5", BaselineGeneration: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Decision != "deny" || len(d.Recovery) != 1 || d.Recovery[0] != "docs/architecture/versions/REQ-001/g2/ARCHITECTURE-001.md" {
		t.Fatalf("wrong architecture rework: %+v", d)
	}
}
