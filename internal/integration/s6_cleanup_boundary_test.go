package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
)

// Resource retention happens after the original merge, Main checks and ACK;
// neither preserving private material nor a cleanup retry repeats that work.
func TestS6CleanupRetainsPrivateMaterialAfterAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name      string
		originals map[string]string
	}{
		{"normal private answers", map[string]string{"plan.json": "plan", "result.json": "result"}},
		{"unknown neighbour", map[string]string{"plan.json": "plan", "result.json": "result", "unknown.txt": "private"}},
		{"changed after receipt", map[string]string{"plan.json": "plan", "result.json": "later changes"}},
		{"original already absent", map[string]string{"plan.json": "plan"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			gitRevisionTest(t, root, "init", "-b", "main")
			gitRevisionTest(t, root, "config", "user.email", "test@example.com")
			gitRevisionTest(t, root, "config", "user.name", "Test")
			if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".claude/\n"), 0600); err != nil {
				t.Fatal(err)
			}
			gitRevisionTest(t, root, "add", ".gitignore")
			gitRevisionTest(t, root, "commit", "-m", "initial")
			worker := filepath.Join(t.TempDir(), "worker")
			gitRevisionTest(t, root, "worktree", "add", "-b", "worker", worker, "main")
			if err := os.WriteFile(filepath.Join(worker, "product.txt"), []byte("delivered\n"), 0600); err != nil {
				t.Fatal(err)
			}
			gitRevisionTest(t, worker, "add", "product.txt")
			gitRevisionTest(t, worker, "commit", "-m", "delivery")
			privateDir := filepath.Join(worker, ".claude", "assignments", "native", "assignment", "r1")
			if err := os.MkdirAll(privateDir, 0700); err != nil {
				t.Fatal(err)
			}
			for name, data := range tc.originals {
				if err := os.WriteFile(filepath.Join(privateDir, name), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			assignment := hookctx.AssignmentContext{AssignmentID: "assignment", TaskID: "TASK", WorktreePath: worker, Branch: "worker", TargetBranch: "main"}
			inspection, err := Inspect(context.Background(), InspectRequest{Root: root, Assignment: assignment, RuntimeID: "loop", BaselineGeneration: 1}, InspectConfig{SkipCompletionCheck: true})
			if err != nil || !inspection.Ready {
				t.Fatalf("inspect: ready=%v blockers=%v err=%v", inspection.Ready, inspection.Blockers, err)
			}
			checks := 0
			cfg := IntegrateConfig{Root: root, GitRoot: root, RuntimeID: "loop", RequiredChecks: []string{"test -f product.txt"}, CheckRunner: func(ctx context.Context, root, command string) error {
				checks++
				return CommandCheckRunner(ctx, root, command)
			}}
			verified, err := Integrate(context.Background(), IntegrateRequest{Inspection: inspection}, cfg)
			if err != nil || verified.Checkpoint.State != StateVerified || checks != 1 {
				t.Fatalf("real merge/check: state=%s checks=%d err=%v", verified.Checkpoint.State, checks, err)
			}
			request := IntegrateRequest{Inspection: inspection, Acknowledge: true, Cleanup: true}
			for attempt := 0; attempt < 2; attempt++ {
				got, err := Integrate(context.Background(), request, cfg)
				if !errors.Is(err, ErrCleanupPending) || got.Checkpoint.State != StateCleanupPending || got.Checkpoint.VerifiedAt != verified.Checkpoint.VerifiedAt {
					t.Fatalf("cleanup attempt %d: state=%s err=%v", attempt, got.Checkpoint.State, err)
				}
				if checks != 1 {
					t.Fatalf("cleanup reran Main checks: %d", checks)
				}
				for _, want := range []string{worker, ".claude/", "cleanup authorization"} {
					if !strings.Contains(err.Error(), want) || !strings.Contains(got.Checkpoint.FailureReason, want) {
						t.Fatalf("retention feedback lacks %q: %v / %q", want, err, got.Checkpoint.FailureReason)
					}
				}
			}
			for name, want := range tc.originals {
				data, err := os.ReadFile(filepath.Join(privateDir, name))
				if err != nil || string(data) != want {
					t.Fatalf("retained %s: %q, %v", name, data, err)
				}
			}
			if data, err := os.ReadFile(filepath.Join(root, "product.txt")); err != nil || string(data) != "delivered\n" {
				t.Fatalf("Main delivery: %q, %v", data, err)
			}
		})
	}
}
