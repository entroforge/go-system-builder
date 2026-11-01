package cli

import (
	"encoding/base64"

	"github.com/entroforge/go-system-builder/internal/policy"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeReminderOnlyAtSessionStartPreservesUnknownFile(t *testing.T) {
	f := newRuntimeFixture(t)
	for _, args := range [][]string{{"init", "-qb", "delivery"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.com"}, {"config", "commit.gpgsign", "false"}, {"commit", "--allow-empty", "-qm", "baseline"}} {
		if out, err := runGit(t, f.root, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	f.state["bound_req"].(map[string]any)["workspace"] = map[string]any{"project_root": f.root, "dev_branch": "delivery", "release_upstream": "origin/release", "bound_commit": "fixture"}
	f.persist(t)
	wt := filepath.Join(t.TempDir(), "worker")
	if out, err := runGit(t, f.root, "worktree", "add", "-b", "worker", wt, "delivery"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	input := policy.Input{Event: "SessionStart", SessionID: "main"}
	unknown := filepath.Join(f.root, ".claude/worktree-reminders", base64.RawURLEncoding.EncodeToString([]byte(input.SessionID))+".txt")
	if err := os.MkdirAll(filepath.Dir(unknown), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unknown, []byte("user note"), 0600); err != nil {
		t.Fatal(err)
	}
	first, delivered := reminderDelivery(f.root, input)
	if !strings.Contains(first, "unassociated checkout") {
		t.Fatalf("missing inventory warning: %s", first)
	}
	delivered()
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		ordinary := input
		ordinary.Event = event
		if repeated, _ := reminderDelivery(f.root, ordinary); repeated != "" {
			t.Fatalf("%s repeated advisory: %s", event, repeated)
		}
	}
	if got, err := os.ReadFile(unknown); err != nil || string(got) != "user note" {
		t.Fatalf("unknown same-name file changed: %q %v", got, err)
	}
	if out, err := runGit(t, f.root, "worktree", "remove", wt); err != nil {
		t.Fatalf("remove fixture: %v %s", err, out)
	}
	if next, commit := reminderDelivery(f.root, input); next != "" {
		t.Fatalf("unneeded reminder: %s", next)
	} else {
		commit()
	}
	if got, err := os.ReadFile(unknown); err != nil || string(got) != "user note" {
		t.Fatalf("unknown same-name file removed when facts cleared: %q %v", got, err)
	}
	input.AgentID = "worker"
	if child, _ := reminderDelivery(f.root, input); child != "" {
		t.Fatal("Main warning delivered to child")
	}

	if _, err := os.Stat(unknown); err != nil {
		t.Fatal("reminder removed unknown user data")
	}
}
