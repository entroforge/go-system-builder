package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS9MissingWorkerCleanupResumesOriginalCheckpoint(t *testing.T) {
	for _, acceptedFinal := range []bool{false, true} {
		t.Run(map[bool]string{false: "candidate-only", true: "accepted-final"}[acceptedFinal], func(t *testing.T) {
			root, worker, p, _, _ := nativeS9WorkerFixture(t)
			var out, diagnostic bytes.Buffer
			if err := receiveNativeS9BuilderFrom(context.Background(), root, repairDispatchSnapshot(t, root).State, p, worker, false, &diagnostic, nil); err != nil {
				t.Fatal(err)
			}
			snap := repairDispatchSnapshot(t, root)
			registry, err := workspace.Decode(snap.State)
			if err != nil {
				t.Fatal(err)
			}
			e := registry.Executions[p.Binding.ManifestAssignmentID]
			args := []string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--check-command", "test -f internal/api/one.go"}
			var original integration.Checkpoint
			if acceptedFinal {
				// Preserve an ignored owner file so the real public command records
				// Final/ACK and leaves its original cleanup checkpoint pending.
				exclude := repairLeadGit(t, worker, "rev-parse", "--git-path", "info/exclude")
				raw, err := os.ReadFile(exclude)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(exclude, append(raw, []byte("\nowner-private.txt\n")...), 0600); err != nil {
					t.Fatal(err)
				}
				writeRepairLeadFile(t, worker, "owner-private.txt", "synthetic owner data")
				if code := runWorkspaceDelivery(args, &out, &diagnostic); code == 0 {
					t.Fatal("unknown ignored file did not preserve Worker")
				}
				var found bool
				original, found, err = integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, e))
				if err != nil || !found || original.State != integration.StateCleanupPending {
					t.Fatalf("original cleanup checkpoint: %+v %v %s", original, err, &diagnostic)
				}
			}
			// Simulate the worktree disappearing before the final complete CAS.
			// Keep its Git registration for the original cleanup helper to retire.
			if err := os.RemoveAll(worker); err != nil {
				t.Fatal(err)
			}
			diagnostic.Reset()
			out.Reset()
			code := runWorkspaceDelivery(args, &out, &diagnostic)
			if !acceptedFinal {
				if code == 0 {
					t.Fatal("unverified missing Worker acquired cleanup authority")
				}
				return
			}
			if code != 0 {
				t.Fatalf("original public cleanup retry: %s", &diagnostic)
			}
			recovered, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, e))
			if err != nil || !found || recovered.State != integration.StateComplete || recovered.VerifiedAt != original.VerifiedAt || recovered.MergeCommit != original.MergeCommit {
				t.Fatalf("original verified facts changed: %+v %v", recovered, err)
			}
			if _, err := os.Lstat(filepath.Join(worker, ".git")); !os.IsNotExist(err) {
				t.Fatal("recovery recreated the Worker")
			}
			planPath, resultPath, err := nativeS9AuthorPaths(root, p)
			if err != nil {
				t.Fatal(err)
			}
			before := repairDispatchRuntimeBytes(t, root)
			replay := []string{"consume", "--root", root, "--path", filepath.Join(worker, resultPath)}
			if code := runStageAnswer(replay, &out, &diagnostic); code != 0 {
				t.Fatalf("completed original receipt replay: %s", &diagnostic)
			}
			repairDispatchAssertUnchanged(t, root, before)
			// A new file at the retired coordinate is not the accepted original.
			writeRepairLeadFile(t, worker, planPath, "changed private original")
			if code := runStageAnswer(replay, &out, &diagnostic); code == 0 {
				t.Fatal("completed replay swallowed a changed private original")
			}
			repairDispatchAssertUnchanged(t, root, before)
			if err := os.RemoveAll(worker); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), worker); err != nil {
				t.Fatal(err)
			}
			if code := runStageAnswer(replay, &out, &diagnostic); code == 0 {
				t.Fatal("completed replay trusted a replacement symlink root")
			}
			repairDispatchAssertUnchanged(t, root, before)
		})
	}
}
