package workpacket

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/projectconventions"
)

func taskConventionsSources(a TaskAnalysisArtifact, p Packet) (map[string]string, error) {
	result := map[string]string{}
	if len(a.TaskConventions) == 0 {
		return result, nil
	}
	var projections []projectconventions.Projection
	if err := json.Unmarshal(a.TaskConventions, &projections); err != nil {
		return nil, err
	}
	var tasks []struct {
		TaskID  string `json:"task_id"`
		TaskRef string `json:"task_ref"`
	}
	if err := json.Unmarshal(a.TaskContexts, &tasks); err != nil {
		return nil, err
	}
	parsedIDs := map[string]string{}
	for _, task := range tasks {
		parsedIDs[task.TaskRef] = task.TaskID
	}
	for _, row := range projections {
		declared := false
		for _, ref := range p.Inputs {
			if ref.Path == row.TaskRef && parsedIDs[row.TaskRef] == row.TaskID && strings.HasPrefix(ref.Path, "docs/dev/tasks/TASK-") {
				declared = true
			}
		}
		if !declared || row.ResolverVersion != a.ConventionsResolverVersion {
			return nil, fmt.Errorf("convention projection differs from formal TASK/resolver binding")
		}
		for _, source := range row.Sources {
			if source.Status != "readable" {
				continue
			}
			if !safeRelative(source.Path) || !digestPattern.MatchString(source.SHA256) {
				return nil, fmt.Errorf("convention source lacks a safe original digest")
			}
			if prior, ok := result[source.Path]; ok && prior != source.SHA256 {
				return nil, fmt.Errorf("convention source hashes conflict")
			}
			result[source.Path] = source.SHA256
		}
	}
	return result, nil
}
