package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
)

func TestCanonicalDirtyInspectionPreservesCauseAndOffersNamedRevision(t *testing.T) {
	fixture := newCanonicalMainFixture(t, canonicalSourceFile)
	mainHead := readGit(t, fixture.root, "rev-parse", "main")
	runtimeBefore := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	productPath := filepath.Join(fixture.worker, filepath.FromSlash(canonicalSourceFile))
	if err := os.WriteFile(productPath, []byte("dirty uncommitted source\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	inspection := fixture.inspect(t, "")
	if inspection.Ready || len(inspection.Blockers) != 1 || !strings.Contains(inspection.Blockers[0], integration.ErrDirtyWorktree.Error()) {
		t.Fatalf("dirty canonical Inspect = ready %t blockers %v", inspection.Ready, inspection.Blockers)
	}
	for _, want := range []string{
		"Worker checkout \"" + fixture.worker + "\" is dirty",
		"git -C \"" + fixture.worker + "\" status --short",
	} {
		if !strings.Contains(inspection.Blockers[0], want) {
			t.Fatalf("Worker dirty blocker %q does not identify exact root/inspection command %q", inspection.Blockers[0], want)
		}
	}
	result, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig())
	if err != nil || result.Checkpoint.State != integration.StatePreserved || result.Checkpoint.MergeCommit != "" ||
		strings.Join(result.Checkpoint.Blockers, "\n") != strings.Join(inspection.Blockers, "\n") || result.Checkpoint.FailureReason != inspection.Blockers[0] {
		t.Fatalf("dirty canonical checkpoint = state %q merge %q blockers %v reason %q err %v", result.Checkpoint.State, result.Checkpoint.MergeCommit, result.Checkpoint.Blockers, result.Checkpoint.FailureReason, err)
	}
	stored, found, err := integration.DefaultCheckpointStore().Load(integration.CheckpointPathForRecord(fixture.root, fixture.record.Reference()))
	if err != nil || !found || stored.State != integration.StatePreserved || stored.MergeCommit != "" || strings.Join(stored.Blockers, "\n") != strings.Join(inspection.Blockers, "\n") {
		t.Fatalf("persisted dirty canonical checkpoint = found %t state %q merge %q blockers %v err %v", found, stored.State, stored.MergeCommit, stored.Blockers, err)
	}
	if got := readGit(t, fixture.root, "rev-parse", "main"); got != mainHead || fileSize(t, fixture.counterPath) != 0 {
		t.Fatalf("dirty inspection changed Main or ran checks: head %s want %s checks=%d", got, mainHead, fileSize(t, fixture.counterPath))
	}
	runtimeAfter := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	if runtimeAfter.Revision != runtimeBefore.Revision || !reflect.DeepEqual(runtimeAfter.State, runtimeBefore.State) {
		t.Fatalf("dirty Worker inspection advanced Runtime revision %d -> %d", runtimeBefore.Revision, runtimeAfter.Revision)
	}

	loaded, err := hookctx.LoadFull(fixture.root, canonicalAgentID)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := integration.Pending(fixture.root, loaded)
	if err != nil {
		t.Fatal(err)
	}
	var row *integration.PendingIntegration
	for i := range pending {
		if pending[i].AssignmentID == canonicalAssignment {
			row = &pending[i]
			break
		}
	}
	if row == nil {
		t.Fatal("current canonical Assignment was absent from pending projection")
	}
	wantCommand := []string{
		"loop-harness", "runtime", "register-workgroup", "--root", fixture.root,
		"--manifest", "<fresh-manifest>", "--task-id", canonicalTaskID, "--task", canonicalTask,
		"--revise-assignment", canonicalAssignment, "--revision-intent-key", "<new-key>",
	}
	if !reflect.DeepEqual(row.ReworkCommand, wantCommand) || strings.Contains(strings.Join(row.ReworkCommand, " "), "workspace rework") {
		t.Fatalf("canonical correction route = %v, want original named Assignment revision %v", row.ReworkCommand, wantCommand)
	}
}
