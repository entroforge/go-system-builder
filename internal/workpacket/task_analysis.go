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
	if err := schema.NewEmbeddedValidator().ValidateBytes("task-analysis-artifact.schema.json", data); err != nil {
		return artifact, err
	}
	if err := json.Unmarshal(data, &artifact); err != nil {
		return artifact, err
	}
	inputs := append([]Reference(nil), p.Inputs...)
	if artifact.ParserVersion != p.TaskAnalysis.ParserVersion || !reflect.DeepEqual(artifact.Inputs, inputs) {
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
	_, err := ReadTaskAnalysis(root, p)
	return err
}
