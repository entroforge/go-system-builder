package buildsurface

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/fileview"
)

// These are read-only coordinates from the original Runtime execution index.
// They do not prepare a worktree, derive permissions or replace its Writer.
type executionMetadata struct {
	AssignmentRecord       *assignmentrecord.RecordReference `json:"assignment_record"`
	AssignmentRecordSHA256 string                            `json:"assignment_record_sha256"`
	AssignmentID           string                            `json:"assignment_id"`
	RuntimeID              string                            `json:"runtime_id"`
	BaselineGeneration     int                               `json:"baseline_generation"`
	Generation             int                               `json:"execution_generation"`
	AgentID                string                            `json:"agent_id"`
	Status                 string                            `json:"status"`
}
type executionRegistry struct {
	Version    int                          `json:"binding_version"`
	MainRoot   string                       `json:"main_root"`
	Branch     string                       `json:"integration_branch"`
	Executions map[string]executionMetadata `json:"executions"`
	History    []executionMetadata          `json:"execution_history"`
}

func (r *executionRegistry) AllExecutions() []executionMetadata {
	out := append([]executionMetadata{}, r.History...)
	for _, row := range r.Executions {
		out = append(out, row)
	}
	return out
}
func readExecutions(state map[string]any) (*executionRegistry, error) {
	raw, exists := state["workspace"]
	if !exists || raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var registry executionRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("decode original execution coordinates: %w", err)
	}
	if registry.Version != 1 || registry.MainRoot == "" || registry.Branch == "" {
		return nil, fmt.Errorf("invalid original execution index identity")
	}
	if err := fileview.ValidateAuthority(registry.MainRoot, state); err != nil {
		return nil, err
	}
	branch, err := fileview.DevelopmentRef(state)
	if err != nil || registry.Branch != branch {
		return nil, fmt.Errorf("original execution registry differs from bound development branch")
	}
	rootAbs, err := filepath.Abs(registry.MainRoot)
	if err != nil || !filepath.IsAbs(rootAbs) {
		return nil, fmt.Errorf("invalid original execution authority root")
	}
	return &registry, nil
}
