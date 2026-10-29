package workpacket

import (
	"encoding/json"
	"fmt"
)

func TaskAnalysisInputs(root string, p Packet) (map[string]string, error) {
	inputs := map[string]string{}
	if p.TaskAnalysis == nil {
		return inputs, nil
	}
	artifact, err := ReadTaskAnalysis(root, p)
	if err != nil {
		return nil, err
	}
	inputs[p.TaskAnalysis.Path] = p.TaskAnalysis.SHA256
	behaviorSources, err := taskBehaviorSources(artifact, p)
	if err != nil {
		return nil, err
	}
	conventionSources, err := taskConventionsSources(artifact, p)
	if err != nil {
		return nil, err
	}
	for path, hash := range conventionSources {
		if prior, ok := behaviorSources[path]; ok && prior != hash {
			return nil, fmt.Errorf("analysis source hashes conflict")
		}
		behaviorSources[path] = hash
	}
	for path, hash := range behaviorSources {
		inputs[path] = hash
	}
	var tasks []struct {
		Reading []struct {
			Path   string `json:"path"`
			SHA    string `json:"sha256"`
			Status string `json:"status"`
		} `json:"reading"`
	}
	if err := json.Unmarshal(artifact.TaskContexts, &tasks); err != nil {
		return nil, err
	}
	for _, task := range tasks {
		for _, ref := range task.Reading {
			if ref.Status == "readable" {
				if !safeRelative(ref.Path) || !digestPattern.MatchString(ref.SHA) {
					return nil, fmt.Errorf("invalid frozen analysis input")
				}
				if prior, ok := inputs[ref.Path]; ok && prior != ref.SHA {
					return nil, fmt.Errorf("conflicting analysis input")
				}
				inputs[ref.Path] = ref.SHA
			}
		}
	}
	return inputs, nil
}
