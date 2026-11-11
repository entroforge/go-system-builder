package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
)

func TestS6WorktreeObservationFailureIsAdvisoryAndTransient(t *testing.T) {
	root := t.TempDir()
	gitRevisionTest(t, root, "init", "-b", "main")
	gitRevisionTest(t, root, "config", "user.email", "test@example.com")
	gitRevisionTest(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "product.txt"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRevisionTest(t, root, "add", "product.txt")
	gitRevisionTest(t, root, "commit", "-m", "base")
	gitRevisionTest(t, root, "branch", "worker")
	missing := filepath.Join(t.TempDir(), "missing-worktree")
	request := InspectRequest{Root: root, Assignment: hookctx.AssignmentContext{AssignmentID: "assignment", TaskID: "TASK", WorktreePath: missing, Branch: "worker", TargetBranch: "main"}, BaselineGeneration: 1}
	without, err := Inspect(context.Background(), request, InspectConfig{SkipCompletionCheck: true})
	if err != nil || !without.Ready || len(without.WorktreeObservation) != 0 {
		t.Fatalf("normal inspection should not observe worktree: %+v %v", without, err)
	}
	observed, err := Inspect(context.Background(), request, InspectConfig{SkipCompletionCheck: true, ObserveWorktree: true})
	if err != nil || !observed.Ready || len(observed.Blockers) != 0 {
		t.Fatalf("observation failure changed readiness: %+v %v", observed, err)
	}
	text := strings.Join(observed.WorktreeObservation, " ")
	if !strings.Contains(text, "could not be observed") || !strings.Contains(text, "unknown") || !strings.Contains(text, missing) {
		t.Fatalf("missing explicit unknown guidance: %s", text)
	}
	data, err := json.Marshal(observed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"WorktreeObservation"`) || strings.Contains(string(data), "could not be observed") {
		t.Fatalf("advisory observation was persisted: %s", data)
	}
}
