package cli

import (
	"github.com/entroforge/go-system-builder/internal/policy"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeReminderDeliveryTracksFactsAndDoesNotConsumeBeforeEmission(t *testing.T) {
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
	input := policy.Input{SessionID: "main"}
	first, delivered := reminderDelivery(f.root, input)
	if !strings.Contains(first, "unassociated checkout") {
		t.Fatalf("missing inventory warning: %s", first)
	}
	if second, _ := reminderDelivery(f.root, input); second != first {
		t.Fatal("computed but undelivered reminder was consumed")
	}
	delivered()
	if second, _ := reminderDelivery(f.root, input); second != "" {
		t.Fatal("unchanged facts repeated")
	}
	if out, err := runGit(t, wt, "commit", "--allow-empty", "-qm", "completed work"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if second, _ := reminderDelivery(f.root, input); second == "" || second == first {
		t.Fatal("new work did not renew the warning")
	}
	input.AgentID = "worker"
	if second, _ := reminderDelivery(f.root, input); second != "" {
		t.Fatal("Main warning delivered to child")
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatal("reminder removed the worktree")
	}
}
