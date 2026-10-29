package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// The durable integration checkpoint is the completion authority. Repeating this
// projection recovers a crash between filesystem cleanup and the Runtime CAS.
func completeExecutionState(root string, state map[string]any, id string) (bool, error) {
	b, err := workspace.Decode(state)
	if err != nil || b == nil {
		return false, err
	}
	// A relocation can also recover an older registered baseline; its own
	// checkpoint identity remains authoritative, without reactivating the Worker.
	e, ok := b.Executions[id]
	if !ok {
		return false, fmt.Errorf("completion execution is not registered: %s", id)
	}
	cp, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, e))
	if err != nil {
		return false, err
	}
	if !found || cp.State != integration.StateComplete || cp.AssignmentID != id || cp.BaselineGeneration != e.BaselineGeneration || cp.WorktreePath != e.Path || cp.SourceBranch != e.Branch || cp.TargetBranch != e.TargetBranch || cp.MergeCommit == "" || cp.TestedHead == "" {
		return false, fmt.Errorf("execution completion requires its complete integration checkpoint: %s", id)
	}
	if e.AssignmentRecord != nil {
		records, err := assignmentrecord.RecordsFromState(state)
		if err != nil {
			return false, err
		}
		record, exists, err := assignmentrecord.FindRecordRevision(records, *e.AssignmentRecord)
		if err != nil || !exists {
			return false, fmt.Errorf("execution completion lacks its original Record: %v", err)
		}
		sha, err := record.Digest()
		if err != nil || sha != e.AssignmentRecordSHA256 {
			return false, fmt.Errorf("execution completion Record digest differs")
		}
		if cp.CanonicalMainFacts == nil {
			return false, fmt.Errorf("canonical execution completion requires its own Main facts")
		}
		if err := integration.ValidateCheckpointRecordBinding(cp, record); err != nil {
			return false, err
		}
	} else if cp.CanonicalMainFacts != nil || cp.AssignmentRecord != nil || cp.AssignmentRecordSHA256 != "" {
		return false, fmt.Errorf("legacy execution cannot complete from canonical checkpoint facts")
	}
	if e.Status == "complete" {
		return false, nil
	}
	if e.Status != "ready" {
		return false, fmt.Errorf("cannot complete execution in state %s", e.Status)
	}
	e.Status = "complete"
	b.Executions[id] = e
	state["workspace"] = workspace.Encode(b)
	return true, nil
}

func persistExecutionCompletion(root string, snap runtime.Snapshot, id string) (runtime.Snapshot, error) {
	return persistExecutionCompletionContext(context.Background(), root, snap, id)
}

func persistExecutionCompletionContext(ctx context.Context, root string, snap runtime.Snapshot, id string) (runtime.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return runtime.Snapshot{}, err
	}
	b, err := workspace.Decode(snap.State)
	if err != nil {
		return runtime.Snapshot{}, err
	}
	if b != nil {
		if e, ok := b.Executions[id]; ok && e.Status == "complete" {
			_, err := completeExecutionState(root, snap.State, id)
			return snap, err
		}
	}
	writer := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{}).WithContext(ctx)
	key := fmt.Sprintf("workspace-complete:%s:%d", id, snap.Revision)
	return writer.Update(snap.Revision, runtime.Mutation{EventID: key, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "main", RuntimeID: workspace.RuntimeID(snap.State), IdempotencyKey: key, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Message: "project completed integration checkpoint", Apply: func(state map[string]any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := completeExecutionState(root, state, id)
		return err
	}})
}
