package cli

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/integration"
)

func TestNativeS9UnrecordedMergePreservesUnknownAndWorktree(t *testing.T) {
	root, worker, e, _, inspection := s9NativeIntegrationCandidate(t)
	const interrupted = "merge returned before checkpoint update"
	writes, caught := 0, false
	store := &integration.CheckpointStore{Clock: func() time.Time {
		writes++
		if writes == 2 {
			panic(interrupted)
		}
		return time.Now().UTC()
	}, NowString: func(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }}
	func() {
		defer func() {
			if p := recover(); p != nil {
				if p != interrupted {
					panic(p)
				}
				caught = true
			}
		}()
		_, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection}, integration.IntegrateConfig{Root: root, GitRoot: root, RuntimeID: e.RuntimeID, RequiredChecks: []string{"true"}, CheckRunner: integration.CommandCheckRunner, CheckpointStore: store})
		t.Fatalf("did not reach merge/checkpoint boundary: %v", err)
	}()
	before, found, err := store.Load(workspaceExecutionCheckpointPath(root, e))
	if !caught || err != nil || !found || before.State != integration.StateReady {
		t.Fatalf("missing interrupted boundary: %+v %v", before, err)
	}
	args := []string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--check-command", "true"}
	var diagnostic bytes.Buffer
	if code := runWorkspaceDelivery(args, &bytes.Buffer{}, &diagnostic); code == 0 {
		t.Fatalf("unrecorded merge was assumed complete: %s", &diagnostic)
	}
	after, found, err := store.Load(workspaceExecutionCheckpointPath(root, e))
	if err != nil || !found || after.State != integration.StateReady {
		t.Fatalf("unknown merge state was overwritten: %+v %v", after, err)
	}
	if _, err := os.Stat(worker); err != nil {
		t.Fatalf("unknown boundary lost original worktree: %v", err)
	}
}
