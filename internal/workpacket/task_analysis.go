package workpacket

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/schema"
)

type TaskAnalysisArtifact struct {
	ConventionsResolverVersion string               `json:"conventions_resolver_version,omitempty"`
	TaskConventions            json.RawMessage      `json:"task_conventions,omitempty"`
	SchemaVersion              string               `json:"schema_version"`
	ParserVersion              string               `json:"parser_version"`
	Inputs                     []Reference          `json:"inputs"`
	TaskContexts               json.RawMessage      `json:"task_contexts"`
	GranularityAdvice          json.RawMessage      `json:"granularity_advice"`
	StructuralCheck            *TaskStructuralCheck `json:"structural_check,omitempty"`
	BehaviorProjectionVersion  string               `json:"behavior_projection_version,omitempty"`
	BehaviorExamples           json.RawMessage      `json:"behavior_examples,omitempty"`
}

// TaskStructuralCheck is the structural portion of the original S4 checker
// result, frozen for the S5 reviewer. It is optional to preserve old analysis
// artifacts byte-for-byte.
type TaskStructuralCheck struct {
	Tasks          int      `json:"tasks"`
	Cancelled      int      `json:"cancelled"`
	ClausesTotal   int      `json:"clauses_total"`
	ClausesCovered int      `json:"clauses_covered"`
	Problems       []string `json:"problems"`
}

func ReadTaskAnalysis(root string, p Packet) (TaskAnalysisArtifact, error) {
	var artifact TaskAnalysisArtifact
	if p.TaskAnalysis == nil {
		return artifact, fmt.Errorf("packet has no frozen task analysis")
	}
	data, err := ReadFile(root, p.TaskAnalysis.Path, 2<<20)
	if err != nil {
		return artifact, fmt.Errorf("frozen analysis unavailable; restore original bytes at %s: %w", filepath.Join(root, p.TaskAnalysis.Path), err)
	}
	if Digest(data) != p.TaskAnalysis.SHA256 {
		return artifact, fmt.Errorf("frozen task analysis digest differs at %s", filepath.Join(root, p.TaskAnalysis.Path))
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("task-analysis-artifact.schema.json", data); err != nil {
		return artifact, err
	}
	if err := json.Unmarshal(data, &artifact); err != nil {
		return artifact, err
	}
	if artifact.ParserVersion != p.TaskAnalysis.ParserVersion || !reflect.DeepEqual(artifact.Inputs, p.Inputs) {
		return artifact, fmt.Errorf("task analysis parser/formal inputs differ from packet")
	}
	if _, err := taskConventionsSources(artifact, p); err != nil {
		return artifact, err
	}
	if _, err := taskBehaviorSources(artifact, p); err != nil {
		return artifact, err
	}
	return artifact, nil
}

// ValidateTaskAnalysisSources retains the source view used by the original
// issuer. It does not run a new parser or interpret advice as a verdict.
func ValidateTaskAnalysisSources(root string, p Packet, files interface{ ReadFile(string) ([]byte, error) }) error {
	if p.TaskAnalysis == nil {
		return nil
	}
	artifact, err := ReadTaskAnalysis(root, p)
	if err != nil {
		return err
	}
	var tasks []struct {
		TaskRef string `json:"task_ref"`
		Reading []struct {
			Path   string `json:"path"`
			SHA    string `json:"sha256"`
			Status string `json:"status"`
		} `json:"reading"`
	}
	if err := json.Unmarshal(artifact.TaskContexts, &tasks); err != nil {
		return err
	}
	seen := map[string]string{}
	behaviorSources, err := taskBehaviorSources(artifact, p)
	if err != nil {
		return err
	}
	conventionSources, err := taskConventionsSources(artifact, p)
	if err != nil {
		return err
	}
	for path, hash := range conventionSources {
		if prior, ok := behaviorSources[path]; ok && prior != hash {
			return fmt.Errorf("analysis source hashes conflict")
		}
		behaviorSources[path] = hash
	}
	for path, hash := range behaviorSources {
		data, err := files.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read frozen behavior source %s: %w", path, err)
		}
		if Digest(data) != hash {
			return fmt.Errorf("frozen behavior source changed: %s", path)
		}
		seen[path] = hash
	}
	for _, task := range tasks {
		declared := false
		for _, input := range p.Inputs {
			if input.Path == task.TaskRef {
				declared = true
			}
		}
		if !declared {
			return fmt.Errorf("analysis includes an unregistered TASK")
		}
		for _, ref := range task.Reading {
			if ref.Status != "readable" {
				continue
			}
			if !safeRelative(ref.Path) || !digestPattern.MatchString(ref.SHA) {
				return fmt.Errorf("analysis reading has no safe original source")
			}
			if prior, ok := seen[ref.Path]; ok {
				if prior != ref.SHA {
					return fmt.Errorf("analysis has conflicting source hashes")
				}
				continue
			}
			data, err := files.ReadFile(ref.Path)
			if err != nil {
				return fmt.Errorf("read frozen analysis source %s: %w", ref.Path, err)
			}
			if Digest(data) != ref.SHA {
				return fmt.Errorf("frozen analysis source changed: %s", ref.Path)
			}
			seen[ref.Path] = ref.SHA
		}
	}
	return nil
}
