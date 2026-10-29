package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS6PrepareReplayCannotBorrowChangedTASK(t *testing.T) {
	for _, scenario := range []string{"same-task-later-main", "changed-task", "changed-task-after-fingerprint"} {
		t.Run(scenario, func(t *testing.T) {
			changed := scenario != "same-task-later-main"
			root, _, record := nativeDeliveryFixture(t)
			brief, _, _ := assignmentrecord.NativePaths(record)
			source := fmt.Sprintf(".claude/assignments/native/%s/r%d/source.json", record.AssignmentID, record.Revision)
			beforeBrief, err := os.ReadFile(filepath.Join(root, brief))
			if err != nil {
				t.Fatal(err)
			}
			beforeSource, err := os.ReadFile(filepath.Join(root, source))
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				path := filepath.Join(root, record.Task.Ref)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(data, []byte("\nA different formal TASK specification, not an execution correction.\n")...), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := workspace.Git(t.Context(), root, "add", record.Task.Ref); err != nil {
					t.Fatal(err)
				}
			} else {
				stageJSON(t, root, "unrelated-main.json", map[string]any{"other_contribution": true})
				if _, err := workspace.Git(t.Context(), root, "add", "unrelated-main.json"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := workspace.Git(t.Context(), root, "commit", "-m", "later Main source fixture"); err != nil {
				t.Fatal(err)
			}
			if scenario == "changed-task-after-fingerprint" {
				output := stageCLI(t, "runtime", "fingerprint", "--root", root)
				if !strings.Contains(output, "drifted") || !strings.Contains(output, record.Task.Ref) {
					t.Fatalf("fingerprint refresh concealed the formal TASK drift: %s", output)
				}
			}
			statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
			beforeState, _ := os.ReadFile(statePath)
			beforeJournal, _ := os.ReadFile(journalPath)
			var out, diagnostic bytes.Buffer
			code := cli.Run([]string{"s6", "prepare", "--root", root, "--task", record.Task.ID, "--role", record.RoleFamily}, nil, &out, &diagnostic)
			if changed {
				if code == 0 || !strings.Contains(diagnostic.String(), record.Task.ID) || !strings.Contains(diagnostic.String(), "Main") || !strings.Contains(diagnostic.String(), "TASK") {
					t.Fatalf("changed TASK borrowed an old native brief or lacked its Main owner: %d %s %s", code, &out, &diagnostic)
				}
				if after, err := os.ReadFile(filepath.Join(root, brief)); err != nil || !bytes.Equal(after, beforeBrief) {
					t.Fatalf("rejected changed TASK rewrote original brief: %v", err)
				}
			} else if code != 0 {
				t.Fatalf("same locked TASK cannot resume after unrelated Main advancement: %d %s", code, &diagnostic)
			}
			if after, err := os.ReadFile(filepath.Join(root, source)); err != nil || !bytes.Equal(after, beforeSource) {
				t.Fatalf("prepare recaptured current Main as the original source: %v", err)
			}
			if after, _ := os.ReadFile(statePath); !bytes.Equal(after, beforeState) {
				t.Fatal("prepare replay changed Runtime")
			}
			if after, _ := os.ReadFile(journalPath); !bytes.Equal(after, beforeJournal) {
				t.Fatal("prepare replay changed Runtime journal")
			}
		})
	}
}

// Simulated professional answers plus actual original native/domain APIs,
// real Git and fixture package checks; not a real Claude/product build.
func TestNativeS6SameTASKReworkClosesWithoutChangingSpecification(t *testing.T) {
	root, oldWorker, previous := nativeDeliveryFixtureVersion(t, "1.3.0")
	nativeCommit(t, oldWorker)
	nativeAnswer(t, oldWorker, previous)
	brief, _, result := assignmentrecord.NativePaths(previous)
	if code, out := nativeEvent(t, root, oldWorker, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(oldWorker, result), false); code != 0 {
		t.Fatalf("original first delivery: %d %s", code, out)
	}
	dirty := filepath.Join(oldWorker, "src/pkg/unreceived.txt")
	if err := os.WriteFile(dirty, []byte("preserve original unreceived work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stageCLI(t, "runtime", "task-integrate", "--root", root, "--assignment-id", previous.AssignmentID, "--agent-id", previous.OwnerAgentID)
	failed, found, err := integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, previous.Reference()))
	if err != nil || !found || failed.State != integration.StatePreserved {
		t.Fatalf("original rework basis missing: %+v %v", failed, err)
	}
	stageCLI(t, "s6", "prepare", "--root", root, "--task", previous.Task.ID, "--role", previous.RoleFamily, "--revise-assignment", previous.AssignmentID, "--revision-intent-key", "r19-original-task-rework")
	snapshot := stageSnapshot(t, root)
	records, err := assignmentrecord.RecordsFromState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	current, found, err := assignmentrecord.FindRecord(records, previous.RuntimeID, previous.BaselineGeneration, previous.AssignmentID)
	if err != nil || !found || current.Revision != previous.Revision+1 || current.Task != previous.Task {
		t.Fatalf("execution revision changed the formal TASK: %+v %v", current, err)
	}
	worker := filepath.Join(t.TempDir(), "native-correction")
	if _, err := workspace.Git(t.Context(), root, "worktree", "add", "-b", "native/r19-correction", worker, "HEAD"); err != nil {
		t.Fatal(err)
	}
	nativeCommit(t, worker)
	nativeAnswer(t, worker, current)
	brief, _, result = assignmentrecord.NativePaths(current)
	if code, out := nativeEvent(t, root, worker, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(worker, result), false); code != 0 {
		t.Fatalf("original revised delivery: %d %s", code, out)
	}
	stageCLI(t, "runtime", "task-integrate", "--root", root, "--assignment-id", current.AssignmentID, "--agent-id", current.OwnerAgentID)
	complete, found, err := integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, current.Reference()))
	if err != nil || !found || complete.State != integration.StateComplete || complete.CanonicalMainFacts == nil {
		t.Fatalf("original same-TASK correction did not close Main work: %+v %v", complete, err)
	}
	if _, err := os.Stat(worker); !os.IsNotExist(err) {
		t.Fatalf("revised delivery cleanup incomplete: %v", err)
	}
	if _, err := os.Stat(dirty); err != nil {
		t.Fatalf("correction discarded old unreceived work: %v", err)
	}
}
