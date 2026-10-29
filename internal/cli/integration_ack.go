package cli

import (
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// The explicit main-session integration command acknowledges only a durable
// verified checkpoint, through the normal Agent lifecycle CAS surface.
func acknowledgeVerifiedIntegration(root string, snapshot runtime.Snapshot, a *hookctx.AssignmentContext, cp integration.Checkpoint) (runtime.Snapshot, error) {
	if cp.State != integration.StateVerified {
		return snapshot, fmt.Errorf("ack requires verified integration")
	}
	loaded, err := hookctx.LoadFull(root, a.OwnerAgentID)
	if err != nil {
		return snapshot, err
	}
	checkpointPath, err := checkpointPathForLoadedAssignment(root, loaded, *a)
	if err != nil {
		return snapshot, err
	}
	if err := validateAckRecordResultBinding(root, snapshot.State, *a, cp); err != nil {
		return snapshot, err
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
	if agent == nil || workstate.AgentActivity(agent) != "reported" {
		return snapshot, fmt.Errorf("integration verified; acknowledgment requires the owner's recorded completion_reported event")
	}
	runtimeID, _ := snapshot.State["runtime_id"].(string)
	// Agent acknowledgement covers all of its assignments. Never retire an
	// owner merely because the first of several deliveries was verified.
	owned := map[string]bool{}
	canonicalWorkspace := map[string]bool{}
	byAssignment := map[string]hookctx.AssignmentContext{}
	for _, other := range loaded.Assignments {
		if other.OwnerAgentID == a.OwnerAgentID && other.WorktreePath != "" {
			owned[other.AssignmentID] = true
			byAssignment[other.AssignmentID] = other
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
				canonicalWorkspace[e.AssignmentID] = e.AssignmentRecord != nil || e.AssignmentRecordSHA256 != ""
			}
		}
	}
	for id := range owned {
		if id == a.AssignmentID {
			continue
		}
		otherPath := integration.CheckpointPath(root, runtimeID, cp.BaselineGeneration, id)
		if current, ok := byAssignment[id]; ok {
			otherPath, err = checkpointPathForLoadedAssignment(root, loaded, current)
			if err != nil {
				return snapshot, err
			}
		} else if canonicalWorkspace[id] {
			return snapshot, fmt.Errorf("canonical Workspace Assignment %s has no current loaded Record projection for ACK", id)
		}
		other, found, err := integration.DefaultCheckpointStore().Load(otherPath)
		if err != nil {
			return snapshot, err
		}
		if !found || other.TestedHead == "" || (other.State != integration.StateVerified && other.State != integration.StateAcknowledged && other.State != integration.StateCleanupPending && other.State != integration.StateComplete) {
			return snapshot, fmt.Errorf("delivery %s is verified; integrate remaining assignment %s before acknowledging owner %s, then retry this command for cleanup", a.AssignmentID, id, a.OwnerAgentID)
		}
	}
	now := time.Now().UTC()
	message := map[string]any{"schema_version": "1.0.0", "message_type": "completion_ack", "message_id": fmt.Sprintf("msg-integration-r%d", snapshot.Revision+1), "correlation_id": "corr-integration", "runtime_id": runtimeID, "agent_id": a.OwnerAgentID, "agent_definition_ref": agent["definition_ref"], "task_id": a.TaskID, "bug_id": nil, "team_id": agent["team_id"], "occurred_at": now.Format(time.RFC3339Nano), "body": fmt.Sprintf("Integration verified at %s; merge %s. Cleanup is a separate recoverable operation.", cp.TestedHead, cp.MergeCommit)}
	dir := filepath.Dir(checkpointPath)
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

// checkpointPathForLoadedAssignment maps the current Hook projection to its
// revision slot. The projection must already have been resolved from the
// current Runtime by hookctx; this helper does not read or establish authority.
func checkpointPathForLoadedAssignment(root string, loaded *hookctx.LoadedContext, a hookctx.AssignmentContext) (string, error) {
	if loaded == nil {
		return "", fmt.Errorf("checkpoint path requires the current loaded Assignment")
	}
	canonicalFieldsPresent := a.AssignmentRecord != nil || a.AssignmentRecordSHA256 != "" || a.AssignmentRevision != 0
	if !canonicalFieldsPresent {
		return integration.CheckpointPath(root, loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration, a.AssignmentID), nil
	}
	if a.AssignmentRecord == nil || len(a.AssignmentRecordSHA256) != 64 || strings.ToLower(a.AssignmentRecordSHA256) != a.AssignmentRecordSHA256 {
		return "", fmt.Errorf("Assignment %s has an incomplete canonical Record projection", a.AssignmentID)
	}
	ref := *a.AssignmentRecord
	if ref.RuntimeID != loaded.PolicyContext.RuntimeID || ref.BaselineGeneration != loaded.BaselineGeneration || ref.AssignmentID != a.AssignmentID || ref.Revision != a.AssignmentRevision {
		return "", fmt.Errorf("Assignment %s Record reference differs from current Runtime coordinates", a.AssignmentID)
	}
	path := integration.CheckpointPathForRecord(root, ref)
	if path == "" {
		return "", fmt.Errorf("Assignment %s Record reference has no checkpoint slot", a.AssignmentID)
	}
	return path, nil
}

func validateAckRecordResultBinding(root string, state map[string]any, a hookctx.AssignmentContext, cp integration.Checkpoint) error {
	canonical := a.AssignmentRecord != nil || a.AssignmentRecordSHA256 != "" || a.AssignmentRevision != 0
	if !canonical {
		if cp.CanonicalMainFacts != nil || cp.AssignmentRecord != nil || cp.AssignmentRecordSHA256 != "" {
			return fmt.Errorf("legacy Assignment cannot acknowledge a canonical revision checkpoint")
		}
		return nil
	}
	if a.AssignmentRecord == nil || a.AssignmentRecordSHA256 == "" || cp.CanonicalMainFacts == nil {
		return fmt.Errorf("canonical integration ACK requires current Record and Result Main facts")
	}
	record, found, err := assignmentrecord.ForOwner(state, a.OwnerAgentID)
	if err != nil || !found {
		return fmt.Errorf("resolve current Assignment Record for ACK: %v", err)
	}
	recordSHA, err := record.Digest()
	if err != nil {
		return err
	}
	if record.Reference() != *a.AssignmentRecord || recordSHA != a.AssignmentRecordSHA256 || record.AssignmentID != a.AssignmentID || record.Task.ID != a.TaskID {
		return fmt.Errorf("ACK Assignment projection differs from the current canonical Record")
	}
	resultRef := a.CompletionRef
	if resultRef == "" || resultRef != cp.CanonicalMainFacts.Binding.ResultPath {
		return fmt.Errorf("ACK Result is not the current Agent/TASK Result bound by Main facts")
	}
	read := func(ref string) ([]byte, error) {
		return workpacket.ReadFile(root, ref, assignmentresult.EvidenceReadLimit)
	}
	accepted, err := assignmentresult.LoadAccepted(state, record, resultRef, read)
	if err != nil {
		return fmt.Errorf("validate current accepted Result for ACK: %w", err)
	}
	resultBytes, err := read(resultRef)
	if err != nil {
		return err
	}
	expected := assignmentresult.ResultBinding{
		AssignmentRecord:       record.Reference(),
		AssignmentRecordSHA256: recordSHA,
		ResultPath:             resultRef,
		ResultSHA256:           checkexec.Hash(resultBytes),
		ResultRevision:         accepted.ResultRevision,
		AnswerDigest:           accepted.AnswerDigest,
	}
	if cp.CanonicalMainFacts.Binding != expected || cp.CompletionReportPath != expected.ResultPath || cp.CompletionReportSHA256 != expected.ResultSHA256 {
		return fmt.Errorf("verified Main checkpoint Record/Result binding is stale for ACK")
	}
	return nil
}
