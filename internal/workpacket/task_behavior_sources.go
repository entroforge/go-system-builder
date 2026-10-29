package workpacket

import (
	"encoding/json"
	"fmt"
	"strings"
)

func taskBehaviorSources(a TaskAnalysisArtifact, p Packet) (map[string]string, error) {
	result := map[string]string{}
	if len(a.BehaviorExamples) == 0 {
		return result, nil
	}
	var examples []struct {
		TaskRef string `json:"task_ref"`
		CaseRef string `json:"case_ref"`
		Status  string `json:"status"`
		Example *struct {
			Case struct {
				ID string `json:"id"`
			} `json:"case"`
			Sources []struct {
				Path string `json:"path"`
				SHA  string `json:"sha256"`
			} `json:"sources"`
		} `json:"example"`
	}
	if err := json.Unmarshal(a.BehaviorExamples, &examples); err != nil {
		return nil, err
	}
	for _, e := range examples {
		declared := false
		for _, ref := range p.Inputs {
			if e.TaskRef == ref.Path && strings.HasPrefix(ref.Path, "docs/dev/tasks/TASK-") {
				declared = true
			}
		}
		if !declared {
			return nil, fmt.Errorf("behavior example belongs to an unregistered TASK")
		}
		if e.Status != "source_example" {
			continue
		}
		if e.Example == nil || e.CaseRef != e.Example.Case.ID || len(e.Example.Sources) == 0 {
			return nil, fmt.Errorf("behavior projection has no original CASE/source")
		}
		for _, ref := range e.Example.Sources {
			formalREQ := false
			for _, input := range p.Inputs {
				if strings.HasPrefix(input.Path, "docs/requirements/REQ-") && input.Path == ref.Path && input.SHA256 == ref.SHA {
					formalREQ = true
				}
			}
			if !safeRelative(ref.Path) || (!strings.HasPrefix(ref.Path, "docs/design/prototypes/") && !formalREQ) || !digestPattern.MatchString(ref.SHA) {
				return nil, fmt.Errorf("behavior source escapes the current module package")
			}
			if prior, ok := result[ref.Path]; ok && prior != ref.SHA {
				return nil, fmt.Errorf("behavior source hashes conflict")
			}
			result[ref.Path] = ref.SHA
		}
	}
	return result, nil
}
