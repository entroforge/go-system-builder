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
	inputs[p.TaskAnalysis.Path] = ""
	behaviorSources, err := taskBehaviorSources(artifact, p)
	if err != nil {
		return nil, err
	}
	conventionSources, err := taskConventionsSources(artifact, p)
	if err != nil {
		return nil, err
	}
	for path := range conventionSources {
		behaviorSources[path] = ""
	}
	for path, hash := range behaviorSources {
		inputs[path] = hash
	}
	var tasks []struct {
		Reading []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"reading"`
	}
	if err := json.Unmarshal(artifact.TaskContexts, &tasks); err != nil {
		return nil, err
	}
	for _, task := range tasks {
		for _, ref := range task.Reading {
			if ref.Status == "readable" {
				if !safeRelative(ref.Path) {
					return nil, fmt.Errorf("invalid frozen analysis input")
				}
				inputs[ref.Path] = ""
			}
		}
	}
	return inputs, nil
}
