package buildsurface

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	loopruntime "github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	"github.com/entroforge/go-system-builder/internal/productbaseline"
	"github.com/entroforge/go-system-builder/internal/runtimemeta"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func sourceView(root string, state map[string]any) (*fileview.View, error) {
	return sourceViewContext(context.Background(), root, state)
}
func sourceViewContext(ctx context.Context, root string, state map[string]any) (*fileview.View, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	def, _ := state["definition"].(map[string]any)
	ref := textField(def["path"])
	data, err := workpacket.ReadFile(root, ref, 16<<20)
	if err != nil || hashBytes(data) != def["sha256"] {
		return nil, fmt.Errorf("original Loop Definition bytes are missing or changed")
	}
	var config struct {
		FileSources []fileview.Rule `json:"file_sources"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	branch, err := fileview.DevelopmentRef(state)
	if err != nil {
		return nil, err
	}
	return fileview.NewContext(ctx, root, "refs/heads/"+strings.TrimPrefix(branch, "refs/heads/"), config.FileSources)
}

func currentTasks(ctx context.Context, state map[string]any, files *fileview.View) ([]Task, error) {
	baseline, _ := state["baseline"].(map[string]any)
	generation := number(baseline["generation"])
	rows, _ := state["documents"].([]any)
	tasks := []Task{}
	ids := map[string]bool{}
	paths := map[string]bool{}
	for _, raw := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row, _ := raw.(map[string]any)
		if row["kind"] != "task" || number(row["generation"]) != generation {
			continue
		}
		task := Task{ID: textField(row["id"]), Path: textField(row["path"]), Version: textField(row["version"]), SHA256: textField(row["sha256"])}
		if row["status"] != "locked" || task.ID == "" || !safePath(task.Path) || task.Version == "" || len(task.SHA256) != 64 || ids[task.ID] || paths[task.Path] {
			return nil, fmt.Errorf("TASK denominator is missing, ambiguous or not locked")
		}
		data, err := files.ReadFile(task.Path)
		if err != nil {
			return nil, fmt.Errorf("read locked TASK source %s: %w", task.ID, err)
		}
		if hashBytes(data) != task.SHA256 {
			return nil, fmt.Errorf("locked TASK source %s changed", task.ID)
		}
		ids[task.ID], paths[task.Path] = true, true
		tasks = append(tasks, task)
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("canonical product baseline requires nonempty exact locked TASK set")
	}
	sortTasks(tasks)
	return tasks, nil
}

func sortTasks(tasks []Task) {
	for i := 1; i < len(tasks); i++ {
		for j := i; j > 0 && tasks[j].ID < tasks[j-1].ID; j-- {
			tasks[j], tasks[j-1] = tasks[j-1], tasks[j]
		}
	}
}

// Until the formal TASK-source revision reader is connected, only the exact
// original lock set is supported. A later authorized relock must supply its
// own original receipt proof before it may replace this denominator.
func verifyLockedTaskSet(locked []productbaseline.Task, current []Task) error {
	if len(locked) != len(current) {
		return fmt.Errorf("current TASK denominator differs from original TR-003 lock set; Main owns formal specification rework through original planning/document review. Assignment revision cannot replace this TASK set; do not refresh fingerprints or invent a relock proof. Same-generation TASK replacement is not supported by this product reader")
	}
	byID := make(map[string]productbaseline.Task, len(locked))
	for _, task := range locked {
		byID[task.TaskID] = task
	}
	for _, task := range current {
		original, found := byID[task.ID]
		if !found || original.Path != task.Path || original.Version != task.Version || original.SHA256 != task.SHA256 {
			return fmt.Errorf("current TASK %s differs from original TR-003 lock set; Main owns formal specification rework through original planning/document review. Assignment revision requires the original TASK; do not refresh fingerprints or invent a relock proof. Same-generation TASK replacement is not supported by this product reader", task.ID)
		}
		delete(byID, task.ID)
	}
	return nil
}

func entity(state map[string]any, collection, id string) (map[string]any, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities[collection].([]any)
	var found map[string]any
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] == id {
			if found != nil {
				return nil, fmt.Errorf("duplicate %s identity %s", collection, id)
			}
			found = row
		}
	}
	if found == nil {
		return nil, fmt.Errorf("missing original %s %s", collection, id)
	}
	return found, nil
}
func contains(raw any, value string) bool {
	switch rows := raw.(type) {
	case []any:
		for _, v := range rows {
			if v == value {
				return true
			}
		}
	case []string:
		for _, v := range rows {
			if v == value {
				return true
			}
		}
	}
	return false
}

func currentResultRef(ctx context.Context, root string, state map[string]any, record assignmentrecord.Record, view *fileview.View) (string, error) {
	registered, found, err := assignmentrecord.ForOwner(state, record.OwnerAgentID)
	if err != nil || !found || registered.Reference() != record.Reference() {
		return "", fmt.Errorf("canonical owner has no current registered Record")
	}
	agent, err := entity(state, "agents", record.OwnerAgentID)
	if err != nil {
		return "", err
	}
	task, err := entity(state, "tasks", record.Task.ID)
	if err != nil {
		return "", err
	}
	team, err := entity(state, "teams", record.TeamID)
	if err != nil {
		return "", err
	}
	if workstate.AgentActivity(agent) != "reported" && workstate.AgentActivity(agent) != "done" && workstate.AgentActivity(agent) != "stopped" {
		return "", fmt.Errorf("canonical owner has no original delivered Result")
	}
	_, assignment, _ := strings.Cut(textField(agent["prompt_ref"]), "#")
	if workstate.TaskActivity(task) == "blocked" || workstate.TaskActivity(task) == "cancelled" || !contains(task["owner_agent_ids"], record.OwnerAgentID) || !contains(agent["task_ids"], record.Task.ID) || assignment != record.AssignmentID || agent["team_id"] != record.TeamID || agent["definition_ref"] != record.AgentDefinition.Ref || team["manifest_ref"] != record.Manifest.Ref {
		return "", fmt.Errorf("current canonical TASK/Owner/Team sources differ from the Record")
	}
	read := func(ref string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.HasPrefix(ref, assignmentinputs.SchemaPrefix) || ref == assignmentinputs.ContextProfileRef {
			return assignmentinputs.Read(root, ref)
		}
		data, err := view.ReadFile(ref)
		if err != nil {
			return nil, err
		}
		physical, err := assignmentinputs.Read(root, ref)
		if err != nil || !bytes.Equal(data, physical) {
			return nil, fmt.Errorf("canonical source differs from original file view: %s", ref)
		}
		return data, nil
	}
	if err := assignmentrecord.VerifySources(record, read); err != nil {
		return "", err
	}
	manifest, err := read(record.Manifest.Ref)
	if err != nil {
		return "", err
	}
	enabled, err := assignmentrecord.ManifestAssignmentRecordEnabled(manifest)
	if err != nil || !enabled {
		return "", fmt.Errorf("original manifest no longer enables canonical Record")
	}
	plan, err := plancheckpoint.ActivatedRef(root, runtimemeta.Snapshot{State: state}, record.OwnerAgentID)
	if err != nil || plan == "" || (record.Contract != assignmentrecord.NativeDeliveryContract && agent["readback_ref"] != plan) || agent["plan_reported_ref"] != plan {
		return "", fmt.Errorf("canonical Result lacks original current accepted professional plan: %v", err)
	}
	ref := textField(agent["completion_reported_ref"])
	if ref == "" || task["completion_report_ref"] != ref {
		return "", fmt.Errorf("current Agent/TASK Result references differ")
	}
	return ref, nil
}

func revisionEvent(state map[string]any, previous, successor assignmentrecord.Record) (string, error) {
	entities, _ := state["entities"].(map[string]any)
	b, err := json.Marshal(entities["assignment_revision_receipts"])
	if err != nil {
		return "", err
	}
	var receipts []loopruntime.AssignmentRevisionReceipt
	if json.Unmarshal(b, &receipts) != nil {
		return "", fmt.Errorf("invalid original revision receipt index")
	}
	var found *loopruntime.AssignmentRevisionReceipt
	for i := range receipts {
		r := &receipts[i]
		if r.AssignmentRecord == successor.Reference() && r.PreviousRecord == previous.Reference() {
			if found != nil {
				return "", fmt.Errorf("duplicate original revision receipt")
			}
			found = r
		}
	}
	if found == nil {
		return "", fmt.Errorf("historical Assignment has no original supersession receipt")
	}
	return found.RuntimeEventID, nil
}

func supersededResultRef(state map[string]any, record assignmentrecord.Record, event string) (string, error) {
	rows, _ := state["evidence"].([]any)
	ref := ""
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["kind"] != "completion_report" || row["status"] != "superseded" || row["invalidated_by"] != event || row["invalidation_rule"] != "assignment_revision_issued" || number(row["baseline_generation"]) != record.BaselineGeneration || !contains(row["produced_by"], record.OwnerAgentID) {
			continue
		}
		if ref != "" {
			return "", fmt.Errorf("historical Result association is ambiguous")
		}
		ref = textField(row["path"])
	}
	if ref == "" {
		return "", fmt.Errorf("missing original superseded Result")
	}
	return ref, nil
}

func verifySourceSnapshots(ctx context.Context, root string, record assignmentrecord.Record) error {
	if len(record.SourceSnapshots) == 0 {
		return fmt.Errorf("historical Record lacks complete immutable source snapshots")
	}
	var total int
	for _, source := range record.SourceSnapshots {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := workpacket.ReadFile(root, source.Path, 16<<20)
		if err != nil || hashBytes(data) != source.SHA256 {
			return fmt.Errorf("historical permission source snapshot changed: %s", source.SourceRef)
		}
		total += len(data)
		if total > 64<<20 {
			return fmt.Errorf("historical source bundle exceeds read budget")
		}
	}
	return nil
}

func executionGeneration(registry *executionRegistry, record assignmentrecord.Record, historical bool) (int, error) {
	if registry == nil {
		return 0, nil
	}
	sha, err := record.Digest()
	if err != nil {
		return 0, err
	}
	generation := 0
	matched := false
	for _, e := range registry.AllExecutions() {
		sameRef := e.AssignmentRecord != nil && *e.AssignmentRecord == record.Reference()
		sameOwner := e.RuntimeID == record.RuntimeID && e.BaselineGeneration == record.BaselineGeneration && e.AssignmentID == record.AssignmentID && e.AgentID == record.OwnerAgentID
		if !sameRef && !sameOwner {
			continue
		}
		if matched || !sameOwner || !sameRef || e.AssignmentRecordSHA256 != sha || e.Generation < 1 || historical && e.Status != "retired" {
			return 0, fmt.Errorf("original workspace execution does not uniquely bind Record/owner/generation")
		}
		matched = true
		generation = e.Generation
	}
	return generation, nil
}

func checkpointRelative(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("checkpoint outside original root")
	}
	return filepath.ToSlash(rel), nil
}
