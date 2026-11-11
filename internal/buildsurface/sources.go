package buildsurface

import (
	"fmt"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

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
func executionGeneration(registry *executionRegistry, record assignmentrecord.Record, historical bool) (int, error) {
	if registry == nil {
		return 0, nil
	}
	generation := 0
	matched := false
	for _, e := range registry.AllExecutions() {
		sameRef := e.AssignmentRecord != nil && *e.AssignmentRecord == record.Reference()
		sameOwner := e.RuntimeID == record.RuntimeID && e.BaselineGeneration == record.BaselineGeneration && e.AssignmentID == record.AssignmentID && e.AgentID == record.OwnerAgentID
		if !sameRef && !sameOwner {
			continue
		}
		if matched || !sameOwner || !sameRef || e.Generation < 1 || historical && e.Status != "retired" {
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
