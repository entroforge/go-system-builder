package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type relocationCheckpoint struct {
	Path   string                 `json:"path"`
	Before integration.Checkpoint `json:"before"`
	After  integration.Checkpoint `json:"after"`
}

func relocationCheckpoints(root string, b *workspace.ExecutionRegistry, req workspace.RebindRequest) ([]relocationCheckpoint, error) {
	var records []relocationCheckpoint
	for _, e := range b.Executions {
		path := integration.CheckpointPath(root, e.RuntimeID, e.BaselineGeneration, e.AssignmentID)
		cp, found, err := integration.DefaultCheckpointStore().Load(path)
		if err != nil {
			return nil, err
		}
		if !found {
			if e.DeliveryRef != "" || e.Status == "complete" {
				return nil, fmt.Errorf("terminal delivery lacks checkpoint: %s", e.AssignmentID)
			}
			continue
		}
		after, err := relocatedCheckpoint(cp, e, req)
		if err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(root, path)
		records = append(records, relocationCheckpoint{Path: rel, Before: cp, After: after})
	}
	return records, nil
}

func relocatedCheckpoint(cp integration.Checkpoint, e workspace.Execution, req workspace.RebindRequest) (integration.Checkpoint, error) {
	if cp.State != integration.StateComplete || cp.AssignmentID != e.AssignmentID || cp.WorktreePath != e.Path || cp.SourceBranch != e.Branch || cp.TargetBranch != e.TargetBranch || cp.BaselineGeneration != e.BaselineGeneration || cp.TestedHead == "" || cp.MergeCommit == "" || (e.Status != "ready" && e.Status != "complete") {
		return integration.Checkpoint{}, fmt.Errorf("finish integration/cleanup before relocation: %s", e.AssignmentID)
	}
	after := cp
	after.WorktreePath = req.Paths[e.Path]
	move := func(path string) (string, error) {
		if !filepath.IsAbs(path) {
			return path, nil
		}
		rel, err := filepath.Rel(req.OldMainRoot, path)
		if err != nil || !filepath.IsLocal(rel) {
			return "", fmt.Errorf("checkpoint evidence outside old Main: %s", path)
		}
		return filepath.Join(req.NewMainRoot, rel), nil
	}
	var err error
	after.CompletionReportPath, err = move(cp.CompletionReportPath)
	if err != nil {
		return integration.Checkpoint{}, err
	}
	after.CheckReceipts = nil
	for _, receipt := range cp.CheckReceipts {
		next, err := move(receipt)
		if err != nil {
			return integration.Checkpoint{}, err
		}
		after.CheckReceipts = append(after.CheckReceipts, next)
	}
	after.PreviousMainRoots = append(append([]string(nil), cp.PreviousMainRoots...), req.OldMainRoot)
	return after, nil
}

// Treat a legacy ready entry as terminal only for native filesystem validation;
// the Runtime relocation remains coordinate-only. Completion is a separate CAS
// after the durable checkpoint coordinates have been recovered.
func planTerminalRelocation(ctx context.Context, b *workspace.ExecutionRegistry, source runtime.Snapshot, req workspace.RebindRequest, records []relocationCheckpoint) (*workspace.ExecutionRegistry, error) {
	data, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	var copy workspace.ExecutionRegistry
	err = json.Unmarshal(data, &copy)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, record := range records {
		e, ok := copy.Executions[record.Before.AssignmentID]
		if !ok || seen[e.AssignmentID] {
			return nil, fmt.Errorf("relocation checkpoint execution mismatch")
		}
		seen[e.AssignmentID] = true
		expectedPath, _ := filepath.Rel(req.NewMainRoot, integration.CheckpointPath(req.NewMainRoot, e.RuntimeID, e.BaselineGeneration, e.AssignmentID))
		expected, err := relocatedCheckpoint(record.Before, e, req)
		if err != nil {
			return nil, err
		}
		if record.Path != expectedPath || !reflect.DeepEqual(expected, record.After) {
			return nil, fmt.Errorf("relocation checkpoint may only change mapped coordinates")
		}

		e.Status = "complete"
		copy.Executions[e.AssignmentID] = e
	}
	for id, e := range b.Executions {
		if !seen[id] {
			_, found, err := integration.DefaultCheckpointStore().Load(integration.CheckpointPath(req.NewMainRoot, e.RuntimeID, e.BaselineGeneration, id))
			if err != nil {
				return nil, err
			}
			if found || e.Status == "complete" || e.DeliveryRef != "" {
				return nil, fmt.Errorf("relocation intent lacks checkpoint: %s", id)
			}
		}
	}
	next, err := copy.PlanRebind(ctx, source.State, req)
	if err != nil {
		return nil, err
	}
	for id, e := range next.Executions {
		e.Status = b.Executions[id].Status
		next.Executions[id] = e
	}
	return next, nil
}

func recoverRelocatedCheckpoints(root string, records []relocationCheckpoint, write bool) error {
	for _, record := range records {
		if !filepath.IsLocal(record.Path) {
			return fmt.Errorf("invalid relocation checkpoint path")
		}
		path := filepath.Join(root, record.Path)
		current, found, err := integration.DefaultCheckpointStore().Load(path)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("relocation checkpoint disappeared: %s", record.Path)
		}
		after := record.After
		after.Revision, after.UpdatedAt = current.Revision, current.UpdatedAt
		if reflect.DeepEqual(current, after) {
			continue
		}
		if !reflect.DeepEqual(current, record.Before) {
			return fmt.Errorf("checkpoint changed during relocation: %s", record.Path)
		}
		if write {
			if _, err := integration.DefaultCheckpointStore().CompareAndSwap(path, current, record.After); err != nil {
				return err
			}
		}
	}
	return nil
}

// A retry after completion projection may differ from the immutable relocation
// target only by ready -> complete, backed by that same terminal checkpoint.
func completedRelocationSnapshot(writer *runtime.Store, intent relocationIntent) (runtime.Snapshot, bool) {
	snap, err := writer.Snapshot()
	if err != nil {
		return runtime.Snapshot{}, false
	}
	b, err := workspace.Decode(snap.State)
	if err != nil || b == nil {
		return runtime.Snapshot{}, false
	}
	for id, e := range b.Executions {
		old, ok := intent.After.Executions[id]
		if !ok {
			return runtime.Snapshot{}, false
		}
		if e.Status != old.Status {
			if old.Status != "ready" || e.Status != "complete" {
				return runtime.Snapshot{}, false
			}
			if _, err := completeExecutionState(b.MainRoot, snap.State, id); err != nil {
				return runtime.Snapshot{}, false
			}
			e.Status = old.Status
			b.Executions[id] = e
		}
	}
	return snap, workspace.RuntimeID(snap.State) == workspace.RuntimeID(intent.Source.State) && reflect.DeepEqual(b, intent.After)
}
