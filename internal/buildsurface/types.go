// Package buildsurface reads the original S6 contribution and product baseline
// sources for S7/S10. It owns no Writer or professional verdict.
package buildsurface

import (
	"encoding/json"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

const Contract = "s7-product-baseline-v1"

type Task struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

type Contribution struct {
	AssignmentRecord assignmentrecord.RecordReference `json:"assignment_record"`
	ResultRef        string                           `json:"result_ref"`
	CheckpointRef    string                           `json:"checkpoint_ref"`
}

type Path struct {
	Path   string `json:"path"`
	Status string `json:"status,omitempty"`
}

type Snapshot struct {
	Contract           string         `json:"contract"`
	RuntimeID          string         `json:"runtime_id"`
	BaselineGeneration int            `json:"baseline_generation"`
	ProjectRoot        string         `json:"project_root"`
	DevelopmentBranch  string         `json:"development_branch"`
	Tasks              []Task         `json:"tasks"`
	Contributions      []Contribution `json:"contributions"`
	Paths              []Path         `json:"paths"`
	UnattributedPaths  []string       `json:"unattributed_paths,omitempty"`
}

// HasCanonical selects the strict reader before any legacy fallback. A broken
// registry with a claimed canonical row remains a strict-path failure.
func HasCanonical(state map[string]any) bool {
	entities, _ := state["entities"].(map[string]any)
	raw, exists := entities["assignments"]
	if !exists {
		return false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return true
	}
	var rows []map[string]any
	if json.Unmarshal(data, &rows) != nil || rows == nil {
		return true
	}
	runtimeID, _ := state["runtime_id"].(string)
	baseline, _ := state["baseline"].(map[string]any)
	for _, row := range rows {
		if row["runtime_id"] == runtimeID && number(row["baseline_generation"]) == number(baseline["generation"]) {
			return true
		}
		if strings.TrimSpace(textField(row["runtime_id"])) == "" {
			return true
		}
	}
	return false
}

func textField(value any) string { s, _ := value.(string); return s }
func number(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}
