package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// The explicit main-session integration command acknowledges only a durable
// verified checkpoint, through the normal Agent lifecycle CAS surface.
func acknowledgeVerifiedIntegration(root string, snapshot runtime.Snapshot, a *hookctx.AssignmentContext, cp integration.Checkpoint) (runtime.Snapshot, error) {
	if cp.State != integration.StateVerified {
		return snapshot, fmt.Errorf("ack requires verified integration")
	}
	if a.CompletionAckRef != "" {
		return snapshot, nil
	}
	entities, _ := snapshot.State["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	var agent map[string]any
	for _, raw := range rows {
		v, _ := raw.(map[string]any)
		if v["id"] == a.OwnerAgentID {
			agent = v
			break
		}
	}
	if agent == nil || agent["state"] != "reported" {
		return snapshot, fmt.Errorf("integration verified; acknowledgment requires the owner's recorded completion_reported event")
	}
	runtimeID, _ := snapshot.State["runtime_id"].(string)
	// Agent acknowledgement covers all of its assignments. Never retire an
	// owner merely because the first of several deliveries was verified.
	loaded, err := hookctx.LoadFull(root, a.OwnerAgentID)
	if err != nil {
		return snapshot, err
	}
	owned := map[string]bool{}
	for _, other := range loaded.Assignments {
		if other.OwnerAgentID == a.OwnerAgentID && other.WorktreePath != "" {
			owned[other.AssignmentID] = true
		}
	}
	binding, err := workspace.Decode(snapshot.State)
	if err != nil {
		return snapshot, err
	}
	if binding != nil {
		for _, e := range binding.Executions {
			if e.AgentID == a.OwnerAgentID && e.RuntimeID == runtimeID && e.BaselineGeneration == cp.BaselineGeneration {
				owned[e.AssignmentID] = true
			}
		}
	}
	for id := range owned {
		if id == a.AssignmentID {
			continue
		}
		other, found, err := integration.DefaultCheckpointStore().Load(integration.CheckpointPath(root, runtimeID, cp.BaselineGeneration, id))
		if err != nil {
			return snapshot, err
		}
		if !found || other.TestedHead == "" || (other.State != integration.StateVerified && other.State != integration.StateAcknowledged && other.State != integration.StateCleanupPending && other.State != integration.StateComplete) {
			return snapshot, fmt.Errorf("delivery %s is verified; integrate remaining assignment %s before acknowledging owner %s, then retry this command for cleanup", a.AssignmentID, id, a.OwnerAgentID)
		}
	}
	now := time.Now().UTC()
	message := map[string]any{"schema_version": "1.0.0", "message_type": "completion_ack", "message_id": fmt.Sprintf("msg-integration-r%d", snapshot.Revision+1), "correlation_id": "corr-integration", "runtime_id": runtimeID, "agent_id": a.OwnerAgentID, "agent_definition_ref": agent["definition_ref"], "task_id": a.TaskID, "bug_id": nil, "team_id": agent["team_id"], "occurred_at": now.Format(time.RFC3339Nano), "body": fmt.Sprintf("Integration verified at %s; merge %s. Cleanup is a separate recoverable operation.", cp.TestedHead, cp.MergeCommit)}
	dir := filepath.Dir(integration.CheckpointPath(root, runtimeID, cp.BaselineGeneration, a.AssignmentID))
	f, err := os.CreateTemp(dir, "completion-ack-*.json")
	if err != nil {
		return snapshot, err
	}
	err = json.NewEncoder(f).Encode(message)
	closeErr := f.Close()
	if err != nil {
		return snapshot, err
	}
	if closeErr != nil {
		return snapshot, closeErr
	}
	next, err := assignment.AdvanceAgent(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), assignment.AgentEventRequest{ExpectedRevision: snapshot.Revision, AgentID: a.OwnerAgentID, Event: "completion_acknowledged", MessagePath: f.Name(), OccurredAt: now})
	if err != nil {
		return snapshot, err
	}
	a.CompletionAckRef = f.Name()
	return next, nil
}
