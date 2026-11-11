package cli_test

import (
	"bytes"
	"encoding/json"
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

// The original S6 starting point keeps the exact TASK set of the first
// TR-003, and the registered Record revision still reads that same source
// (blueprint/l3-stages/L3-S6-build.md:399). A repeated `s6 prepare` therefore
// reuses the original brief by the recorded TASK identity and version: it
// decides reuse from the existing original TASK, the stage entry and the
// responsibility version, never from a commit OID, a source file hash or a
// reconciliation certificate. An external byte edit that the Runtime never
// recorded is therefore not machine detectable from the same version, while a
// formal TASK change (a different locked TASK version) must be refused
// instead of being borrowed by the old preparation.
func TestNativeS6PrepareReplayCannotBorrowChangedTASK(t *testing.T) {
	for _, scenario := range []string{"same-task-later-main", "changed-task"} {
		t.Run(scenario, func(t *testing.T) {
			root, _, record := nativeDeliveryFixture(t)
			brief, _, _ := assignmentrecord.NativePaths(record)
			beforeBrief, err := os.ReadFile(filepath.Join(root, brief))
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "changed-task" {
				// A formal TASK change is a different locked TASK version in
				// the Runtime. Re-registering the file, writing another
				// TR-003 or refreshing a fingerprint does not give the new
				// TASK a complete execution source, so the original Record
				// keeps its reviewed TASK and the replay must refuse.
				state := stageSnapshot(t, root).State
				documents, _ := state["documents"].([]any)
				relocked := false
				for _, raw := range documents {
					doc, _ := raw.(map[string]any)
					if doc["kind"] != "task" || doc["id"] != record.Task.ID {
						continue
					}
					doc["version"] = "v9.9.9"
					relocked = true
				}
				if !relocked {
					t.Fatalf("locked TASK document is unavailable: %+v", documents)
				}
				stageJSON(t, root, ".claude/loop-state.json", state)
			} else {
				stageJSON(t, root, "unrelated-main.json", map[string]any{"other_contribution": true})
				if _, err := workspace.Git(t.Context(), root, "add", "unrelated-main.json"); err != nil {
					t.Fatal(err)
				}
				if _, err := workspace.Git(t.Context(), root, "commit", "-m", "later Main source fixture"); err != nil {
					t.Fatal(err)
				}
			}
			statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
			beforeState, _ := os.ReadFile(statePath)
			beforeJournal, _ := os.ReadFile(journalPath)
			var out, diagnostic bytes.Buffer
			code := cli.Run([]string{"s6", "prepare", "--root", root, "--task", record.Task.ID, "--role", record.RoleFamily}, nil, &out, &diagnostic)
			if scenario == "changed-task" {
				if code == 0 || !strings.Contains(diagnostic.String(), record.Task.ID) || !strings.Contains(diagnostic.String(), "Main") || !strings.Contains(diagnostic.String(), "TASK") {
					t.Fatalf("changed TASK borrowed an old native brief or lacked its Main owner: %d %s %s", code, &out, &diagnostic)
				}
				if after, err := os.ReadFile(filepath.Join(root, brief)); err != nil || !bytes.Equal(after, beforeBrief) {
					t.Fatalf("rejected changed TASK rewrote original brief: %v", err)
				}
			} else if code != 0 {
				t.Fatalf("same locked TASK cannot resume after unrelated Main advancement: %d %s", code, &diagnostic)
			}
			if after, _ := os.ReadFile(statePath); !bytes.Equal(after, beforeState) {
				t.Fatal("prepare replay changed Runtime")
			}
			if after, _ := os.ReadFile(journalPath); !bytes.Equal(after, beforeJournal) {
				t.Fatal("prepare replay changed Runtime journal")
			}
			// The retired source proof and the retired fingerprint refresh must
			// not come back with the replay: the original TASK source is the
			// recorded TASK identity, not a captured source artifact.
			source := fmt.Sprintf(".claude/assignments/native/%s/r%d/source.json", record.AssignmentID, record.Revision)
			if _, err := os.Stat(filepath.Join(root, source)); !os.IsNotExist(err) {
				t.Fatalf("prepare replay republished the retired source proof: %v", err)
			}
			var fingerprintOut, fingerprintDiagnostic bytes.Buffer
			if code := cli.Run([]string{"runtime", "fingerprint", "--root", root}, nil, &fingerprintOut, &fingerprintDiagnostic); code == 0 {
				t.Fatalf("retired runtime fingerprint refresh succeeded again: %s", &fingerprintOut)
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
	// The rework basis is the delivery's actual closing blocker: the first
	// Result stays unfinished, so Main integrates it and records a preserved
	// responsibility. An untidy checkout is not a closing blocker
	// (blueprint/l3-stages/L3-S6-build.md:392).
	nativeS6IncompleteClosingResult(t, oldWorker, previous)
	brief, _, result := assignmentrecord.NativePaths(previous)
	if code, out := nativeEvent(t, root, oldWorker, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(oldWorker, result), false); code != 0 {
		t.Fatalf("original first delivery: %d %s", code, out)
	}
	// Material the first delivery never received stays in the old checkout:
	// cleanup only reclaims what it can safely remove
	// (blueprint/l4-mechanisms/L4-workflow-convergence.md:302).
	dirty := filepath.Join(oldWorker, "src/pkg/unreceived.txt")
	retained := []byte("preserve original unreceived work\n")
	if err := os.WriteFile(dirty, retained, 0600); err != nil {
		t.Fatal(err)
	}
	var blockedOut, blockedDiagnostic bytes.Buffer
	if code := cli.Run([]string{"runtime", "task-integrate", "--root", root, "--assignment-id", previous.AssignmentID, "--agent-id", previous.OwnerAgentID}, nil, &blockedOut, &blockedDiagnostic); code != 1 || !strings.Contains(blockedDiagnostic.String(), "closing contract is pending") {
		t.Fatalf("first delivery did not record its actual closing blocker: %d %s %s", code, &blockedOut, &blockedDiagnostic)
	}
	failed, found, err := integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, previous.Reference()))
	if err != nil || !found || failed.State != integration.StatePreserved {
		t.Fatalf("original rework basis missing: %+v %v", failed, err)
	}
	stageCLI(t, "s6", "prepare", "--root", root, "--task", previous.Task.ID, "--role", previous.RoleFamily, "--revise-assignment", previous.AssignmentID)
	snapshot := stageSnapshot(t, root)
	records, err := assignmentrecord.RecordsFromState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	current, found, err := assignmentrecord.FindRecord(records, previous.RuntimeID, previous.BaselineGeneration, previous.AssignmentID)
	if err != nil || !found || current.Revision != previous.Revision+1 || current.Task != previous.Task {
		t.Fatalf("execution revision changed the formal TASK: %+v %v", current, err)
	}
	if current.Task.ID != previous.Task.ID || current.Task.Ref != previous.Task.Ref || current.Task.Version != previous.Task.Version {
		t.Fatalf("execution revision changed the formal TASK identity: %+v", current.Task)
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
	// The native execution checkout is temporary: the platform removes it when
	// the child returns, and Main integrates the returned Git source
	// (blueprint/l4-mechanisms/L4-workflow-convergence.md:300).
	if _, err := workspace.Git(t.Context(), root, "worktree", "remove", "--force", worker); err != nil {
		t.Fatal(err)
	}
	stageCLI(t, "runtime", "task-integrate", "--root", root, "--assignment-id", current.AssignmentID, "--agent-id", current.OwnerAgentID)
	complete, found, err := integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, current.Reference()))
	if err != nil || !found || complete.State != integration.StateComplete || complete.CanonicalMainFacts == nil || complete.VerifiedAt == "" {
		t.Fatalf("original same-TASK correction did not close Main work: %+v %v", complete, err)
	}
	if _, err := os.Stat(worker); !os.IsNotExist(err) {
		t.Fatalf("revised delivery cleanup incomplete: %v", err)
	}
	// The old checkout and the material the first delivery never received stay
	// pending cleanup: the blueprint keeps untracked or private material
	// instead of deleting a worktree to make the test pass
	// (blueprint/l4-mechanisms/L4-workflow-convergence.md:302, :435).
	if _, err := os.Stat(oldWorker); err != nil {
		t.Fatalf("correction discarded the old checkout: %v", err)
	}
	if data, err := os.ReadFile(dirty); err != nil || !bytes.Equal(data, retained) {
		t.Fatalf("correction discarded old unreceived work: %q %v", data, err)
	}
	status, err := workspace.Git(t.Context(), oldWorker, "status", "--porcelain=v1", "--ignored=matching", "--untracked-files=all")
	if err != nil || !strings.Contains(status, "unreceived.txt") {
		t.Fatalf("old checkout is no longer cleanup_pending material: %q %v", status, err)
	}
	old, found, err := integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, previous.Reference()))
	if err != nil || !found || old.State != integration.StatePreserved {
		t.Fatalf("old responsibility lost its preserved closing blocker: %+v %v", old, err)
	}
}

// nativeS6IncompleteClosingResult rewrites the private Result of an accepted
// native delivery into a genuinely unfinished closing assessment, so Main has
// an actual closing blocker to record instead of an untidy checkout.
func nativeS6IncompleteClosingResult(t *testing.T, source string, record assignmentrecord.Record) {
	t.Helper()
	_, _, answerPath := assignmentrecord.NativePaths(record)
	data, err := os.ReadFile(filepath.Join(source, answerPath))
	if err != nil {
		t.Fatal(err)
	}
	var answer map[string]any
	if err := json.Unmarshal(data, &answer); err != nil {
		t.Fatal(err)
	}
	closing, _ := answer["closing_assessment"].(map[string]any)
	if closing == nil {
		t.Fatalf("private Result has no closing assessment: %s", data)
	}
	closing["disposition"] = "incomplete"
	conditions, _ := closing["conditions"].([]any)
	for _, raw := range conditions {
		condition, _ := raw.(map[string]any)
		condition["judgment"] = "incomplete"
		condition["rationale"] = "The assigned work remains incomplete."
	}
	stageJSON(t, source, answerPath, answer)
}
