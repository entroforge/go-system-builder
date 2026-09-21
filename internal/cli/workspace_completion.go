package cli

import (
	"fmt"
	"path/filepath"
	"time"

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
	cp, found, err := integration.DefaultCheckpointStore().Load(integration.CheckpointPath(root, e.RuntimeID, e.BaselineGeneration, id))
	if err != nil {
		return false, err
	}
	if !found || cp.State != integration.StateComplete || cp.AssignmentID != id || cp.BaselineGeneration != e.BaselineGeneration || cp.WorktreePath != e.Path || cp.SourceBranch != e.Branch || cp.TargetBranch != e.TargetBranch || cp.MergeCommit == "" || cp.TestedHead == "" {
		return false, fmt.Errorf("execution completion requires its complete integration checkpoint: %s", id)
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
	writer := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{})
	key := fmt.Sprintf("workspace-complete:%s:%d", id, snap.Revision)
	return writer.Update(snap.Revision, runtime.Mutation{EventID: key, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "main", RuntimeID: workspace.RuntimeID(snap.State), IdempotencyKey: key, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Message: "project completed integration checkpoint", Apply: func(state map[string]any) error { _, err := completeExecutionState(root, state, id); return err }})
}
