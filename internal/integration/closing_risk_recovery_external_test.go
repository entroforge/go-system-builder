package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
)

// Inspect the real accepted result without supplying a digest as a gate.
func inspectOriginalS6Result(t *testing.T, f *canonicalMainFixture) integration.Inspection {
	t.Helper()
	ref := f.record.Reference()
	a := hookctx.AssignmentContext{
		AssignmentID: f.record.AssignmentID, AssignmentRecord: &ref,
		AssignmentRevision: f.record.Revision, BaselineGeneration: f.record.BaselineGeneration,
		TaskID: f.record.Task.ID, OwnerAgentID: f.record.OwnerAgentID, RoleFamily: f.record.RoleFamily,
		AgentDefinitionRef: f.record.AgentDefinition.Ref, WorktreePath: f.worker, Branch: canonicalBranch,
		TargetBranch: "main", ManifestRef: f.record.Manifest.Ref, State: "reported",
		CompletionRef: f.currentResultRef(t), WritePaths: append([]string{}, f.record.EffectiveWritePaths...),
	}
	in, err := integration.Inspect(context.Background(), integration.InspectRequest{
		Root: f.root, Assignment: a, TargetBranch: "main", BaselineGeneration: f.record.BaselineGeneration,
		RuntimeID: f.record.RuntimeID,
	}, integration.InspectConfig{})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func TestS6NonBlockingOwnerNotesIntegrateWithoutReanswer(t *testing.T) {
	f := newCanonicalMainFixtureWithClosing(t, canonicalSourceFile, true, false)
	accepted := f.acceptResultWithClosing(t, "msg-nonblocking-owner-notes", "Owner completed work.", "complete",
		[]string{"Non-blocking operational assumption"}, []string{"Information about the chosen scope"})
	in := inspectOriginalS6Result(t, f)
	if !in.Ready {
		t.Fatalf("notes must not block inspection: %v", in.Blockers)
	}
	result, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: in, Acknowledge: true}, f.integrateConfig())
	if err != nil || result.Checkpoint.State != integration.StateAcknowledged || result.Checkpoint.CanonicalMainFacts == nil {
		t.Fatalf("actual merge/check/ack failed: state=%q err=%v", result.Checkpoint.State, err)
	}
	if result.Checkpoint.CanonicalMainFacts.Binding.ResultPath != accepted.ResultRef || fileSize(t, f.counterPath) != 1 {
		t.Fatalf("integration did not use the same accepted result and one actual check: facts=%+v", result.Checkpoint.CanonicalMainFacts)
	}
	text, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(accepted.ResultRef)))
	if err != nil || !strings.Contains(string(text), "Non-blocking operational assumption") || !strings.Contains(string(text), "Information about the chosen scope") {
		t.Fatalf("accepted owner notes were not preserved: %v", err)
	}
}

func TestS6RealClosingAndEngineeringFailuresStillBlock(t *testing.T) {
	for _, disposition := range []string{"incomplete", "blocked"} {
		t.Run(disposition, func(t *testing.T) {
			f := newCanonicalMainFixtureWithClosing(t, canonicalSourceFile, true, false)
			f.acceptResultWithClosing(t, "msg-real-"+disposition, "Owner has unfinished work.", disposition, []string{"The work is not finished."}, nil)
			in := inspectOriginalS6Result(t, f)
			if in.Ready {
				t.Fatal("the owner's actual unfinished conclusion was ignored")
			}
			// A caller cannot turn an unfinished owner conclusion into an ACK
			// merely by supplying a ready inspection.
			in.Ready = true
			in.Blockers = nil
			got, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: in, Acknowledge: true}, f.integrateConfig())
			if !errors.Is(err, integration.ErrClosingContractPending) || got.Checkpoint.State != integration.StatePreserved || fileSize(t, f.counterPath) != 0 {
				t.Fatalf("actual unfinished conclusion did not block: checkpoint=%+v err=%v", got.Checkpoint, err)
			}
			// A pre-existing interrupted merge is still unknown. A later
			// unfinished answer must not overwrite that recovery boundary.
			path := integration.DefaultCheckpointStore().PathForRecord(f.root, f.record.Reference())
			unknown := got.Checkpoint
			unknown.State, unknown.ResumeState, unknown.LastErrorCode = integration.StateReady, "", ""
			unknown, err = integration.DefaultCheckpointStore().CompareAndSwap(path, got.Checkpoint, unknown)
			if err != nil {
				t.Fatal(err)
			}
			again, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: in}, f.integrateConfig())
			if !errors.Is(err, integration.ErrClosingContractPending) || again.Checkpoint.State != integration.StateReady || again.Checkpoint.Revision != unknown.Revision {
				t.Fatalf("unfinished conclusion overwrote unknown merge boundary: checkpoint=%+v err=%v", again.Checkpoint, err)
			}
		})
	}
	t.Run("actual scope violation", func(t *testing.T) {
		f := newCanonicalMainFixtureWithClosing(t, "outside.txt", true, false)
		in := inspectOriginalS6Result(t, f)
		if in.Ready || len(in.OutOfScopeDiff) == 0 {
			t.Fatalf("actual diff outside original write scope was not detected: %+v", in)
		}
	})
	t.Run("actual Main check failure", func(t *testing.T) {
		f := newCanonicalMainFixtureWithClosing(t, canonicalSourceFile, true, false)
		if err := os.WriteFile(f.failPath, []byte("fail"), 0o600); err != nil {
			t.Fatal(err)
		}
		in := inspectOriginalS6Result(t, f)
		if !in.Ready {
			t.Fatalf("unexpected inspect blocker: %v", in.Blockers)
		}
		got, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: in, Acknowledge: true}, f.integrateConfig())
		if !errors.Is(err, integration.ErrCheckFailed) || got.Checkpoint.State != integration.StatePreserved || got.Checkpoint.ResumeState != integration.StateMerged {
			t.Fatalf("failed actual Main check was not preserved: checkpoint=%+v err=%v", got.Checkpoint, err)
		}
	})
	t.Run("actual merge conflict", func(t *testing.T) {
		f := newCanonicalMainFixtureWithClosing(t, canonicalSourceFile, true, false)
		writeFixtureFile(t, f.root, canonicalSourceFile, []byte("conflicting Main contribution\n"))
		gitFixture(t, f.root, "add", canonicalSourceFile)
		gitFixture(t, f.root, "commit", "-m", "conflicting Main change")
		in := inspectOriginalS6Result(t, f)
		if !in.Ready {
			t.Fatalf("unexpected inspect blocker before real merge: %v", in.Blockers)
		}
		got, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: in, Acknowledge: true}, f.integrateConfig())
		if !errors.Is(err, integration.ErrMergeConflict) || got.Checkpoint.State != integration.StatePreserved {
			t.Fatalf("actual merge conflict was not preserved: checkpoint=%+v err=%v", got.Checkpoint, err)
		}
		if _, err := os.Stat(f.worker); err != nil {
			t.Fatalf("worker worktree was not retained: %v", err)
		}
	})
}

// A prior incomplete owner Result stopped before any merge. The same original
// responsibility can continue once its current owner Result is complete.
func TestS6ClosingPendingResumesOriginalResponsibility(t *testing.T) {
	f := newCanonicalMainFixtureWithClosing(t, canonicalSourceFile, true, false)
	in := inspectOriginalS6Result(t, f)
	if !in.Ready {
		t.Fatalf("original result is not ready: %v", in.Blockers)
	}
	ref := f.record.Reference()
	path := integration.DefaultCheckpointStore().PathForRecord(f.root, ref)
	cp := integration.Checkpoint{AssignmentID: ref.AssignmentID, AssignmentRecord: &ref, TaskID: f.record.Task.ID, BaselineGeneration: ref.BaselineGeneration, WorktreePath: f.worker, SourceBranch: canonicalBranch, TargetBranch: "main", State: integration.StatePreserved, ResumeState: integration.StatePending, LastErrorCode: "LOOP_S6_CLOSING_PENDING"}
	before, err := integration.DefaultCheckpointStore().CompareAndSwap(path, integration.Checkpoint{}, cp)
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: in}, f.integrateConfig())
	if err != nil || !waiting.Reused || waiting.Checkpoint.Revision != before.Revision || fileSize(t, f.counterPath) != 0 {
		t.Fatalf("ordinary observation must retain the pending boundary: %+v %v", waiting.Checkpoint, err)
	}
	resumed, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: in, RetryPreserved: true, Acknowledge: true}, f.integrateConfig())
	if err != nil || resumed.Checkpoint.State != integration.StateAcknowledged || fileSize(t, f.counterPath) != 1 {
		t.Fatalf("original corrected responsibility did not merge/check/ACK once: %+v %v", resumed.Checkpoint, err)
	}
}

// A previously interrupted merge is unknown; neither retry nor inspection
// may turn it into a fabricated success or replay the real merge.
func TestS6UnknownMergeWindowPreservesOriginalFacts(t *testing.T) {
	f := newCanonicalMainFixtureWithClosing(t, canonicalSourceFile, true, false)
	in := inspectOriginalS6Result(t, f)
	if !in.Ready {
		t.Fatalf("original result is not ready: %v", in.Blockers)
	}
	ref := f.record.Reference()
	path := integration.DefaultCheckpointStore().PathForRecord(f.root, ref)
	cp := integration.Checkpoint{AssignmentID: ref.AssignmentID, AssignmentRecord: &ref, TaskID: f.record.Task.ID, BaselineGeneration: ref.BaselineGeneration, WorktreePath: f.worker, SourceBranch: canonicalBranch, TargetBranch: "main", State: integration.StateReady}
	before, err := integration.DefaultCheckpointStore().CompareAndSwap(path, integration.Checkpoint{}, cp)
	if err != nil {
		t.Fatal(err)
	}
	beforeCommit := readGit(t, f.root, "rev-parse", "main")
	for _, retry := range []bool{false, true} {
		got, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: in, RetryPreserved: retry}, f.integrateConfig())
		if err == nil || !strings.Contains(err.Error(), "unknown") || got.Checkpoint.Revision != before.Revision {
			t.Fatalf("unknown merge boundary changed (retry=%v): %+v %v", retry, got.Checkpoint, err)
		}
	}
	if after := readGit(t, f.root, "rev-parse", "main"); after != beforeCommit || fileSize(t, f.counterPath) != 0 {
		t.Fatal("unknown merge boundary triggered a merge or Main check")
	}
}
