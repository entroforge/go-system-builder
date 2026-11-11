package assignment_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

// Exercise the original registration, lifecycle Writer and recovery readers;
// the root occupant forces every activation family onto an actual alternate.
func TestRegisteredActivationChainUsesActualReferences(t *testing.T) {
	for _, occupant := range []string{"file", "symlink"} {
		t.Run(occupant, func(t *testing.T) {
			root := assignmentTestRoot(t)
			const agentID = "agent-ver-req-gap"
			statePath, journalPath, manifestPath := planCheckpointWorkgroupState(t, root, "REQ-002", "workgroup-delivery-round-1", "TASK-012", agentID)
			// The shared lifecycle fixture starts from a REQ-001 manifest while its
			// Runtime and PLAN are REQ-002. Recovery validates the original manifest
			// association too, so bind this fixture before registering it.
			initial, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			manifestBytes, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			var manifest map[string]any
			if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest["runtime_id"] = initial.State["runtime_id"]
			manifestBytes, err = json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifestPath, manifestBytes, 0600); err != nil {
				t.Fatal(err)
			}

			taskPath := filepath.Join(filepath.Dir(manifestPath), "TASK-012.md")
			if err := os.WriteFile(taskPath, []byte("# TASK-012\n"), 0600); err != nil {
				t.Fatal(err)
			}
			occupied := filepath.Join(root, ".claude", "evidence")
			unknown := []byte("unknown evidence root")
			var target string
			if occupant == "file" {
				if err := os.WriteFile(occupied, unknown, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				target = t.TempDir()
				if err := os.WriteFile(filepath.Join(target, "unknown.txt"), unknown, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, occupied); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			registered, err := assignment.Register(root, statePath, journalPath, assignment.Request{ExpectedRevision: 5, ManifestPath: manifestPath, TaskID: "TASK-012", TaskPath: taskPath})
			if err != nil {
				t.Fatal(err)
			}
			assertActualAgentRef(t, root, registered.State, agentID, "activation_ref")
			if _, err := hookctx.FromSnapshot(root, agentID, registered); err != nil {
				t.Fatalf("pre-staged Hook context: %v", err)
			}
			// The Runtime pair is in a separate fixture temp directory; the
			// professional plan must remain inside the registered project.
			planPath := writeAutoChainPlanReport(t, filepath.Dir(manifestPath), 6)
			outcome, err := assignment.AutoAdvanceToWorking(assignment.AutoChainInput{Root: root, StatePath: statePath, JournalPath: journalPath, ExpectedRevision: -1, AgentID: agentID, PlanPath: planPath})
			if err != nil || !outcome.Chained || outcome.FinalState != "working" {
				t.Fatalf("auto-chain: %+v %v", outcome, err)
			}
			snapshot, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			assertActualAgentRef(t, root, snapshot.State, agentID, "activation_ref")
			assertActualAgentRef(t, root, snapshot.State, agentID, "work_started_ref")
			if _, err := hookctx.FromSnapshot(root, agentID, snapshot); err != nil {
				t.Fatalf("activated Hook context: %v", err)
			}
			recovered, err := plancheckpoint.ActivatedRef(root, snapshot, agentID)
			if err != nil || recovered == "" {
				t.Fatalf("original recovery: %q %v", recovered, err)
			}
			replay, err := assignment.AutoAdvanceToWorking(assignment.AutoChainInput{Root: root, StatePath: statePath, JournalPath: journalPath, ExpectedRevision: -1, AgentID: agentID, PlanPath: planPath})
			if err != nil || replay.Chained {
				t.Fatalf("replay: %+v %v", replay, err)
			}
			if target == "" {
				got, err := os.ReadFile(occupied)
				if err != nil || !bytes.Equal(got, unknown) {
					t.Fatalf("unknown root changed: %q %v", got, err)
				}
			} else {
				got, err := os.Readlink(occupied)
				if err != nil || got != target {
					t.Fatalf("unknown symlink changed: %s %v", got, err)
				}
				entries, err := os.ReadDir(target)
				if err != nil || len(entries) != 1 {
					t.Fatalf("unknown target changed: %v %v", entries, err)
				}
			}
		})
	}
}

func assertActualAgentRef(t *testing.T, root string, state map[string]any, agentID, key string) {
	t.Helper()
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row["id"] != agentID {
			continue
		}
		ref, _ := row[key].(string)
		if !strings.HasPrefix(ref, ".claude/evidence-alt-") {
			t.Fatalf("%s=%q", key, ref)
		}
		if _, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref))); err != nil {
			t.Fatalf("read registered %s: %v", key, err)
		}
		return
	}
	t.Fatalf("missing agent %s", agentID)
}
