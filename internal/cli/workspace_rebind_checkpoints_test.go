package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestRelocatedCheckpointRecoveryPreservesEvidence(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(t.TempDir(), "old")
	e := workspace.Execution{RuntimeID: "loop-test", BaselineGeneration: 1, AssignmentID: "assignment", Path: filepath.Join(old, ".worktrees/worker"), Branch: "worker", TargetBranch: "test2", Status: "ready", DeliveryRef: "candidate.json", DeliverySHA256: "candidate-hash", Checks: []string{"test product"}}
	path := integration.CheckpointPath(root, e.RuntimeID, 1, e.AssignmentID)
	receipt := filepath.Join(filepath.Dir(path), "checks/receipt.json")
	if err := os.MkdirAll(filepath.Dir(receipt), 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"command":"test product","cwd":` + quoteJSON(old) + `,"head_before":"tested","head_after":"tested","status":"pass","exit_code":0}`)
	if err := os.WriteFile(receipt, data, 0600); err != nil {
		t.Fatal(err)
	}
	cp := integration.Checkpoint{AssignmentID: e.AssignmentID, BaselineGeneration: 1, WorktreePath: e.Path, SourceBranch: e.Branch, TargetBranch: e.TargetBranch, State: integration.StateComplete, SourceHead: "source", MergeCommit: "merge", TestedHead: "tested", CompletionReportSHA256: "report-hash", CompletionReportPath: "reports/result.json", CheckReceipts: []string{filepath.Join(old, ".claude/evidence/loop-test/g1/worktree/assignment/checks/receipt.json")}}
	cp, err := integration.DefaultCheckpointStore().ForceWrite(path, cp)
	if err != nil {
		t.Fatal(err)
	}
	b := &workspace.ExecutionRegistry{Executions: map[string]workspace.Execution{e.AssignmentID: e}}
	req := workspace.RebindRequest{OldMainRoot: old, NewMainRoot: root, Paths: map[string]string{e.Path: filepath.Join(root, ".worktrees/worker")}}
	records, err := relocationCheckpoints(root, b, req)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"before-runtime", "after-runtime-before-checkpoint", "after-checkpoint-before-completion", "retry"} {
		t.Run(stage, func(t *testing.T) {
			write := stage != "before-runtime"
			if err := recoverRelocatedCheckpoints(root, records, write); err != nil {
				t.Fatal(err)
			}
		})
	}
	got, _, err := integration.DefaultCheckpointStore().Load(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := records[0].After
	expected.Revision, expected.UpdatedAt = got.Revision, got.UpdatedAt
	if !reflect.DeepEqual(got, expected) || got.Revision != cp.Revision+1 {
		t.Fatalf("non-idempotent checkpoint migration: %+v", got)
	}
	bytes, err := os.ReadFile(receipt)
	if err != nil || string(bytes) != string(data) {
		t.Fatal("receipt bytes changed")
	}
	if _, err := verifiedDomainChecks(root, e, got); err != nil {
		t.Fatal(err)
	}
	got.PreviousMainRoots = nil
	if _, err := verifiedDomainChecks(root, e, got); err == nil {
		t.Fatal("accepted old receipt CWD without explicit mapping")
	}
	got.TestedHead = "different"
	if _, err := integration.DefaultCheckpointStore().ForceWrite(path, got); err != nil {
		t.Fatal(err)
	}
	if err := recoverRelocatedCheckpoints(root, records, true); err == nil {
		t.Fatal("overwrote changed checkpoint")
	}
}

func quoteJSON(s string) string { data, _ := json.Marshal(s); return string(data) }

func TestRelocationRejectsUnfinishedDelivery(t *testing.T) {
	for _, state := range []string{"missing", integration.StateVerified, integration.StateCleanupPending, integration.StatePreserved} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			e := workspace.Execution{RuntimeID: "loop-test", BaselineGeneration: 1, AssignmentID: "assignment", Path: "/old/worker", Branch: "worker", TargetBranch: "test2", Status: "ready", DeliveryRef: "candidate.json"}
			if state != "missing" {
				if _, err := integration.DefaultCheckpointStore().ForceWrite(integration.CheckpointPath(root, e.RuntimeID, 1, e.AssignmentID), integration.Checkpoint{AssignmentID: e.AssignmentID, BaselineGeneration: 1, State: state, WorktreePath: e.Path, SourceBranch: e.Branch, TargetBranch: e.TargetBranch, TestedHead: "tested", MergeCommit: "merge"}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := relocationCheckpoints(root, &workspace.ExecutionRegistry{Executions: map[string]workspace.Execution{e.AssignmentID: e}}, workspace.RebindRequest{}); err == nil {
				t.Fatal("unfinished delivery accepted")
			}
		})
	}
}

// Replay both sides of the checkpoint/Runtime boundary with a removed Worker.
// The Git/Runtime fixture is real; only the already-completed checkpoint is seeded.
func TestTerminalRebindRecoversRuntimeAndCheckpointBoundaries(t *testing.T) {
	for _, stage := range []string{"intent", "runtime", "checkpoint", "completion"} {
		t.Run(stage, func(t *testing.T) {
			fix := newRuntimeFixture(t)
			for _, args := range [][]string{{"init", "-b", "test2"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"add", "docs"}, {"commit", "-m", "base"}} {
				if out, err := runGit(t, fix.root, args...); err != nil {
					t.Fatalf("%s %v", out, err)
				}
			}
			bindFixtureWorkspace(t, fix)
			var out, errout bytes.Buffer
			if code := runRuntimeWorkspace([]string{"bind", "--root", fix.root}, &out, &errout); code != 0 {
				t.Fatal(&errout)
			}
			b, state, err := workspace.Load(fix.root)
			if err != nil {
				t.Fatal(err)
			}
			e, err := b.Plan(context.Background(), state, "assignment-terminal", "builder", []string{"src/**"}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			e.Status = "ready"
			b.Executions[e.AssignmentID] = e
			state["workspace"] = workspace.Encode(b)
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(fix.root, ".claude/loop-state.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			cp := integration.Checkpoint{AssignmentID: e.AssignmentID, BaselineGeneration: e.BaselineGeneration, SourceBranch: e.Branch, TargetBranch: e.TargetBranch, WorktreePath: e.Path, State: integration.StateComplete, SourceHead: e.BaseCommit, MergeCommit: e.BaseCommit, TestedHead: e.BaseCommit, CompletionReportSHA256: "unchanged-report"}
			if _, err := integration.DefaultCheckpointStore().ForceWrite(integration.CheckpointPath(fix.root, e.RuntimeID, e.BaselineGeneration, e.AssignmentID), cp); err != nil {
				t.Fatal(err)
			}
			moved := filepath.Join(t.TempDir(), "moved")
			req := workspace.RebindRequest{RuntimeID: workspace.RuntimeID(state), OldMainRoot: fix.root, OldCommonDir: b.CommonDir, NewMainRoot: moved, NewCommonDir: filepath.Join(moved, ".git"), Branch: b.Branch, ExpectedHead: b.BoundHead, Paths: map[string]string{e.Path: filepath.Join(moved, ".worktrees/worker")}}
			if err := os.Rename(fix.root, moved); err != nil {
				t.Fatal(err)
			}
			source := runtime.Snapshot{Revision: int(state["revision"].(float64)), State: state}
			records, err := relocationCheckpoints(moved, b, req)
			if err != nil {
				t.Fatal(err)
			}
			next, err := planTerminalRelocation(context.Background(), b, source, req, records)
			if err != nil {
				t.Fatal(err)
			}

			for _, change := range []func(*relocationCheckpoint){
				func(r *relocationCheckpoint) { r.After.TestedHead = "forged-test" },
				func(r *relocationCheckpoint) { r.After.CompletionReportSHA256 = "forged-report" },
				func(r *relocationCheckpoint) { r.After.SourceHead = "forged-source" },
				func(r *relocationCheckpoint) { r.After.WorktreePath = moved },
				func(r *relocationCheckpoint) { r.Path = "checkpoint-elsewhere.json" },
			} {
				altered := append([]relocationCheckpoint(nil), records...)
				change(&altered[0])
				if _, err := planTerminalRelocation(context.Background(), b, source, req, altered); err == nil {
					t.Fatal("altered relocation checkpoint intent accepted")
				}
			}
			if _, err := planTerminalRelocation(context.Background(), b, source, req, nil); err == nil {
				t.Fatal("omitted checkpoint accepted")
			}
			intent := relocationIntent{Request: req, Source: source, Before: b, After: next, Checkpoints: records}
			requestBytes, _ := json.Marshal(req)
			sum := sha256.Sum256(requestBytes)
			receipt := filepath.Join(moved, ".claude/evidence/workspace-relocations", hex.EncodeToString(sum[:])+".json")
			if err := os.MkdirAll(filepath.Dir(receipt), 0700); err != nil {
				t.Fatal(err)
			}
			data, _ = json.Marshal(intent)
			if err := os.WriteFile(receipt, data, 0600); err != nil {
				t.Fatal(err)
			}
			writer := runtime.NewWriter(filepath.Join(moved, ".claude/loop-state.json"), filepath.Join(moved, ".claude/loop-events.jsonl"), moved, semantic.RuntimeCandidateValidator{})
			var snap runtime.Snapshot
			if stage != "intent" {
				snap, err = writer.RelocateWorkspace(source, workspace.Encode(next), "workspace-relocation-"+hex.EncodeToString(sum[:]), "test interrupted relocation")
				if err != nil {
					t.Fatal(err)
				}
			}
			if stage == "checkpoint" || stage == "completion" {
				if err := recoverRelocatedCheckpoints(moved, records, true); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "completion" {
				if _, err := persistExecutionCompletion(moved, snap, e.AssignmentID); err != nil {
					t.Fatal(err)
				}
			}
			request := filepath.Join(t.TempDir(), "request.json")
			if err := os.WriteFile(request, requestBytes, 0600); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				out.Reset()
				errout.Reset()
				if code := runWorkspaceRebind([]string{"--root", moved, "--request", request, "--reason", "recover terminal relocation"}, &out, &errout); code != 0 {
					t.Fatalf("rebind %d: %s", code, &errout)
				}
			}
			got, err := writer.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if got.Revision != source.Revision+2 {
				t.Fatalf("completion replayed: revisions %d -> %d", source.Revision, got.Revision)
			}
			bound, err := workspace.Decode(got.State)
			if err != nil || bound.Executions[e.AssignmentID].Status != "complete" {
				t.Fatalf("terminal projection missing: %v", err)
			}
			current, _, err := integration.DefaultCheckpointStore().Load(integration.CheckpointPath(moved, e.RuntimeID, e.BaselineGeneration, e.AssignmentID))
			if err != nil {
				t.Fatal(err)
			}
			if current.WorktreePath != req.Paths[e.Path] || current.SourceHead != cp.SourceHead || current.MergeCommit != cp.MergeCommit || current.TestedHead != cp.TestedHead || current.CompletionReportSHA256 != cp.CompletionReportSHA256 {
				t.Fatal("relocation changed evidence identity")
			}
			// A later baseline must not strand an older registered terminal execution.
			bound.Executions[e.AssignmentID] = func() workspace.Execution { old := bound.Executions[e.AssignmentID]; old.Status = "ready"; return old }()
			got.State["workspace"] = workspace.Encode(bound)
			got.State["baseline"].(map[string]any)["generation"] = e.BaselineGeneration + 1
			if changed, err := completeExecutionState(moved, got.State, e.AssignmentID); err != nil || !changed {
				t.Fatalf("historical terminal projection: changed=%v err=%v", changed, err)
			}

		})
	}
}
