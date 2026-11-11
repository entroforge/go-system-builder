package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS6MainCommandRecoversActualLostMergeAndLaterTarget(t *testing.T) {
	for _, interruptWrite := range []int{2, 3} {
		t.Run(map[int]string{2: "ready-before-merge-receipt", 3: "merged-before-verification-receipt"}[interruptWrite], func(t *testing.T) {
			root, worker, record := nativeDeliveryFixture(t)
			brief, _, resultPath := assignmentrecord.NativePaths(record)
			nativeCommit(t, worker)
			nativeAnswer(t, worker, record)
			if code, output := nativeEvent(t, root, worker, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(worker, resultPath), false); code != 0 {
				t.Fatalf("original native S6 delivery: %d %s", code, output)
			}
			loaded, err := hookctx.LoadFull(root, record.OwnerAgentID)
			if err != nil {
				t.Fatal(err)
			}
			var assignment hookctx.AssignmentContext
			for _, row := range loaded.Assignments {
				if row.AssignmentID == record.AssignmentID && row.OwnerAgentID == record.OwnerAgentID {
					assignment = row
				}
			}
			if assignment.WorktreePath != worker || assignment.Branch == "" {
				t.Fatalf("native return did not record actual Main integration coordinates: %+v", assignment)
			}
			inspectConfig, err := integration.AssignmentInspectConfig(assignment)
			if err != nil {
				t.Fatal(err)
			}
			inspection, err := integration.Inspect(t.Context(), integration.InspectRequest{Root: root, Assignment: assignment, RuntimeID: record.RuntimeID, BaselineGeneration: record.BaselineGeneration}, inspectConfig)
			if err != nil || !inspection.Ready {
				t.Fatalf("original native S6 Inspect: %+v %v", inspection, err)
			}
			beforeMerge, err := workspace.Git(t.Context(), root, "rev-parse", "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			const interrupted = "actual native S6 merge response lost"
			writes, caught := 0, false
			store := &integration.CheckpointStore{Clock: func() time.Time {
				writes++
				if writes == interruptWrite {
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
				_, err := integration.Integrate(t.Context(), integration.IntegrateRequest{Inspection: inspection}, integration.IntegrateConfig{Root: root, GitRoot: root, RuntimeID: record.RuntimeID, CheckpointStore: store})
				t.Fatalf("did not interrupt actual native S6 merge/receipt window: %v", err)
			}()
			slot := assignmentrecord.IntegrationCheckpointPath(root, record.Reference())
			ready, found, err := store.Load(slot)
			expectedState := map[int]string{2: integration.StateReady, 3: integration.StateMerged}[interruptWrite]
			if !caught || err != nil || !found || ready.State != expectedState {
				t.Fatalf("original interrupted checkpoint: %+v %v", ready, err)
			}
			git := func(args ...string) string {
				t.Helper()
				out, err := workspace.Git(t.Context(), root, args...)
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
			merge := git("rev-parse", "HEAD")
			if merge == beforeMerge {
				t.Fatal("actual Git merge never happened")
			}
			if err := os.WriteFile(filepath.Join(root, "src/pkg/later.go"), []byte("package pkg\n\nconst LaterMainContribution = true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			git("add", "src/pkg/later.go")
			git("commit", "-m", "Later Main contribution after lost native merge response")
			head, count := git("rev-parse", "HEAD"), git("rev-list", "--all", "--count")
			var output, diagnostic bytes.Buffer
			args := []string{"runtime", "task-integrate", "--root", root, "--assignment-id", record.AssignmentID, "--agent-id", record.OwnerAgentID}
			if interruptWrite == 3 {
				args = append(args, "--retry-preserved")
			}
			code := cli.Run(args, nil, &output, &diagnostic)
			reported := output.String() + diagnostic.String()
			if interruptWrite == 2 {
				// The merge outcome is unknown: the framework does not guess
				// success and does not repeat the merge; Main inspects the
				// actual state (L4-workflow-convergence.md:300).
				if code == 0 || !strings.Contains(reported, "merge outcome is unknown") {
					t.Fatalf("interrupted merge was not reported as unknown: %d %s %s", code, &output, &diagnostic)
				}
				preserved, found, err := store.Load(slot)
				if err != nil || !found || preserved.State == integration.StateComplete {
					t.Fatalf("interrupted checkpoint was not preserved as unknown: %+v %v; %s %s", preserved, err, &output, &diagnostic)
				}
			} else {
				// The merge itself already happened; the framework continues the
				// real Main integration and records its actual outcome instead
				// of re-running the merge or replaying a recorded merge proof.
				if code != 0 {
					t.Fatalf("original native S6 Main recovery: %d %s %s", code, &output, &diagnostic)
				}
				complete, found, err := store.Load(slot)
				if err != nil || !found || (complete.State != integration.StateComplete && complete.State != integration.StateCleanupPending) {
					t.Fatalf("public Main recovery did not record the actual integration state: %+v %v; %s %s", complete, err, &output, &diagnostic)
				}
			}
			if git("rev-parse", "HEAD") != head || git("rev-list", "--all", "--count") != count {
				t.Fatal("Main recovery repeated the Git merge")
			}
			// Cleanup stays independent of the recorded delivery: unknown,
			// untracked, ignored or private material is preserved as
			// cleanup_pending instead of being removed
			// (L4-workflow-convergence.md:302).
			if _, err := os.Stat(worker); err != nil {
				t.Fatalf("Main recovery removed the original worktree: %v", err)
			}
			stateBefore, found, err := store.Load(slot)
			if err != nil || !found {
				t.Fatalf("original checkpoint disappeared: %v", err)
			}
			if interruptWrite == 3 {
				args = args[:len(args)-1]
			}
			replayOut, replayDiagnostic := &bytes.Buffer{}, &bytes.Buffer{}
			replayCode := cli.Run(args, nil, replayOut, replayDiagnostic)
			replayed := replayOut.String() + replayDiagnostic.String()
			if interruptWrite == 2 {
				if replayCode == 0 || !strings.Contains(replayed, "merge outcome is unknown") {
					t.Fatalf("Main command replay stopped reporting the unknown merge: %d %s %s", replayCode, replayOut, replayDiagnostic)
				}
			} else if replayCode != 0 {
				t.Fatalf("completed Main recovery was not replay-safe: %d %s %s", replayCode, replayOut, replayDiagnostic)
			}
			stateAfter, found, err := store.Load(slot)
			if err != nil || !found {
				t.Fatalf("Main command replay lost the checkpoint: %v", err)
			}
			if stateAfter.State != stateBefore.State || git("rev-parse", "HEAD") != head || git("rev-list", "--all", "--count") != count {
				t.Fatalf("Main command replay repeated merge/checks or changed their recorded state: %s -> %s", stateBefore.State, stateAfter.State)
			}
		})
	}
}
