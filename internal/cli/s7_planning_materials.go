package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// s7PlanningMaterials projects only the original current-batch references and
// their directly associated Result/checkpoint facts. It is guidance, not a
// proof of a complete product diff or a condition of ReviewPlan reception.
func s7PlanningMaterials(ctx context.Context, root string, state map[string]any) []string {
	notes := []string{"Main's own changes and deleted paths are not fully determined by S6 receipts. Review the actual available work materials, including normal Git inspection when useful; an unlisted path is not evidence of no change."}
	baseline, _ := state["baseline"].(map[string]any)
	generation := tolerantInt(baseline["generation"])
	bound, _ := state["bound_req"].(map[string]any)
	runtimeID, _ := state["runtime_id"].(string)
	if path, _ := bound["path"].(string); path != "" {
		notes = append(notes, "Bound REQ material: "+path)
	}
	tasks := map[string]string{}
	documents, _ := state["documents"].([]any)
	for _, raw := range documents {
		row, _ := raw.(map[string]any)
		if row["kind"] != "task" || tolerantInt(row["generation"]) != generation {
			continue
		}
		id, _ := row["id"].(string)
		path, _ := row["path"].(string)
		if id != "" && path != "" {
			tasks[id] = path
		}
	}
	for id, path := range tasks {
		notes = append(notes, fmt.Sprintf("Original TASK %s: %s", id, path))
	}
	byRef := map[assignmentrecord.RecordReference]assignmentrecord.Record{}
	owners := map[string]bool{}
	entities, _ := state["entities"].(map[string]any)
	records, _ := entities["assignments"].([]any)
	for _, raw := range records {
		encoded, err := json.Marshal(raw)
		var record assignmentrecord.Record
		if err != nil || json.Unmarshal(encoded, &record) != nil {
			notes = append(notes, "An S6 Assignment record could not be read; its contribution remains unknown.")
			continue
		}
		if record.RuntimeID == runtimeID && record.BaselineGeneration == generation && tasks[record.Task.ID] == record.Task.Ref {
			byRef[record.Reference()] = record
			owners[record.OwnerAgentID] = true
		}
	}
	rows, _ := state["evidence"].([]any)
	seen := map[string]bool{}
	for _, raw := range rows {
		if err := ctx.Err(); err != nil {
			notes = append(notes, "S6 material observation was interrupted; remaining contributions are unknown.")
			break
		}
		row, _ := raw.(map[string]any)
		status, _ := row["status"].(string)
		if row["kind"] != "completion_report" || tolerantInt(row["baseline_generation"]) != generation || (status != "valid" && status != "superseded") {
			continue
		}
		ownerMatch := false
		for _, rawOwner := range s6RowsAsStrings(row["produced_by"]) {
			ownerMatch = ownerMatch || owners[rawOwner]
		}
		ref, _ := row["path"].(string)
		if !ownerMatch || ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		data, err := workpacket.ReadFile(root, ref, 1<<20)
		if err != nil {
			notes = append(notes, fmt.Sprintf("Registered S6 Result %s is unavailable; its original responsibility and contribution remain unknown: %v", ref, err))
			continue
		}
		var result assignmentresult.AcceptedResult
		if err := json.Unmarshal(data, &result); err != nil {
			notes = append(notes, fmt.Sprintf("Registered S6 Result %s cannot be read; its contribution remains unknown: %v", ref, err))
			continue
		}
		record, ok := byRef[result.AssignmentRecord]
		if !ok || result.ProducerAgentID != record.OwnerAgentID || result.TaskID != record.Task.ID {
			notes = append(notes, fmt.Sprintf("Registered S6 Result %s cannot be associated with a current-batch original responsibility; its contribution remains unknown.", ref))
			continue
		}
		label := fmt.Sprintf("S6 %s / %s r%d, owner %s", record.Task.ID, record.AssignmentID, record.Revision, record.OwnerAgentID)
		notes = append(notes, fmt.Sprintf("%s: original Result %s (registered %s).", label, ref, status))
		checkpointPath := assignmentrecord.IntegrationCheckpointPath(root, record.Reference())
		checkpoint, found, err := integration.DefaultCheckpointStore().Load(checkpointPath)
		if err != nil || !found || checkpoint.CanonicalMainFacts == nil || checkpoint.CanonicalMainFacts.Binding.AssignmentRecord != record.Reference() || checkpoint.CanonicalMainFacts.Binding.ResultPath != ref || checkpoint.CanonicalMainFacts.Binding.ResultRevision != result.ResultRevision {
			notes = append(notes, fmt.Sprintf("%s: Main integration for this Result is unknown; original checkpoint %s is unavailable or does not identify it.", label, checkpointPath))
			continue
		}
		step := checkpoint.State
		if step == integration.StatePreserved || step == integration.StateBlocked {
			step = checkpoint.ResumeState
		}
		switch step {
		case integration.StateMerged, integration.StateVerified, integration.StateAcknowledged, integration.StateCleanupPending, integration.StateComplete:
			notes = append(notes, fmt.Sprintf("%s: Main checkpoint %s records %s; recorded changed paths: %s. Path status, Main-only work and deletions still require professional judgment.", label, checkpointPath, checkpoint.State, s7MaterialPaths(checkpoint.CanonicalMainFacts.ChangedPaths)))
		default:
			notes = append(notes, fmt.Sprintf("%s: Main integration is not confirmed by checkpoint %s (state %s).", label, checkpointPath, checkpoint.State))
		}
	}
	if len(seen) == 0 {
		notes = append(notes, "No current-batch original S6 Result reference was available to this guide; earlier contributions remain unknown.")
	}
	sort.Strings(notes)
	return notes
}

func s6RowsAsStrings(raw any) []string {
	var values []string
	switch rows := raw.(type) {
	case []any:
		for _, value := range rows {
			if value, ok := value.(string); ok {
				values = append(values, value)
			}
		}
	case []string:
		values = append(values, rows...)
	}
	return values
}

func s7MaterialPaths(paths []string) string {
	if len(paths) == 0 {
		return "not recorded (not evidence of no changes)"
	}
	return strings.Join(paths, ", ")
}
