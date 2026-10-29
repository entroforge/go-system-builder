package assignment

import (
	"fmt"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// ApplyIntegrationRework is used only inside the Main workspace-rework CAS
// after validating a failed checkpoint and preserving its immutable evidence.
// This is not a Worker-issued lifecycle approval.
func ApplyIntegrationRework(state map[string]any, catalog *transition.Catalog, owner, taskID, receipt, reason string) error {
	entities, _ := state["entities"].(map[string]any)
	version := fmt.Sprint(state["schema_version"])
	agents, _ := entities["agents"].([]any)
	found := false
	for _, raw := range agents {
		a, _ := raw.(map[string]any)
		if a["id"] != owner {
			continue
		}
		projection, err := workstate.AgentProjection(a, version)
		if err != nil {
			return fmt.Errorf("resolve integration rework Agent activity: %w", err)
		}
		from := projection.Activity
		to, ok := ResolveAgentTransition(catalog, from, "integration_rework_requested")
		if !ok {
			return fmt.Errorf("integration rework requires a reported owner and current rework catalog")
		}
		if err := workstate.RecordAgentActivity(state, a, to, "integration_rework_requested"); err != nil {
			return fmt.Errorf("record integration rework Agent work state: %w", err)
		}
		delete(a, "completion_reported_ref")
		delete(a, "completion_acknowledged_ref")
		found = true
	}
	if !found {
		return fmt.Errorf("rework owner is unregistered")
	}
	tasks, _ := entities["tasks"].([]any)
	oldReport := ""
	for _, raw := range tasks {
		task, _ := raw.(map[string]any)
		if task["id"] != taskID {
			continue
		}
		projection, err := workstate.TaskProjection(task, version)
		if err != nil {
			return fmt.Errorf("resolve integration rework TASK activity: %w", err)
		}
		from := projection.Activity
		to, ok := taskEntityTransitions(catalog).resolve(from, "integration_rework_requested")
		if !ok {
			return fmt.Errorf("integration rework requires TASK review state")
		}
		oldReport, _ = task["completion_report_ref"].(string)
		if err := workstate.RecordTaskActivity(state, task, to, "integration_rework_requested"); err != nil {
			return fmt.Errorf("record integration rework TASK work state: %w", err)
		}
		delete(task, "completion_report_ref")
	}
	evidence, _ := state["evidence"].([]any)
	for _, raw := range evidence {
		item, _ := raw.(map[string]any)
		if oldReport != "" && item["path"] == oldReport && item["kind"] == "completion_report" {
			item["status"] = "superseded"
			item["invalidated_by"] = receipt
			item["invalidation_rule"] = "integration_rework_requested"
			item["invalidation_reason"] = reason
		}
	}
	return nil
}
