package submission

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/projectconventions"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func (s *Service) freezeTaskAnalysis(p workpacket.Packet, reqID string) (workpacket.Packet, error) {
	paths := taskAnalysisPaths(p.Inputs)
	tasks, advice, err := semantic.AnalyzeApprovedTaskContexts(s.Root, s.Files, paths)
	if err != nil {
		return p, err
	}
	structural, err := semantic.TasksCheckWithFiles(s.Root, s.Files, reqID)
	if err != nil {
		return p, err
	}
	taskData, err := json.Marshal(tasks)
	if err != nil {
		return p, err
	}
	adviceData, err := json.Marshal(advice)
	if err != nil {
		return p, err
	}
	artifact := workpacket.TaskAnalysisArtifact{
		SchemaVersion:     "1.0.0",
		ParserVersion:     semantic.TaskContextAnalysisVersion,
		Inputs:            p.Inputs,
		TaskContexts:      taskData,
		GranularityAdvice: adviceData,
		StructuralCheck:   structuralCheckProjection(structural),
	}
	artifact.BehaviorProjectionVersion = semantic.TaskBehaviorProjectionVersion
	artifact.BehaviorExamples, err = json.Marshal(semantic.ProjectTaskBehavior(s.Root, s.Files, tasks))
	if err != nil {
		return p, err
	}
	approved := []projectconventions.Reference{}
	for _, ref := range p.Inputs {
		approved = append(approved, projectconventions.Reference{ID: ref.ID, Path: ref.Path, Version: ref.Version, SHA256: ref.SHA256})
	}
	artifact.ConventionsResolverVersion = projectconventions.Version
	artifact.TaskConventions, err = json.Marshal(semantic.ProjectTaskConventions(s.Root, s.Files, tasks, approved))
	if err != nil {
		return p, err
	}
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return p, err
	}
	data = append(data, '\n')
	digest := workpacket.Digest(data)
	path := filepath.ToSlash(filepath.Join(".claude/work-packets/analysis", digest+".json"))
	stored, err := workpacket.PublishBounded(s.Root, path, data, 2<<20)
	if err != nil {
		return p, err
	}
	if !bytes.Equal(stored, data) {
		return p, fmt.Errorf("frozen task analysis conflicts with existing bytes")
	}
	return workpacket.BindTaskAnalysis(p, &workpacket.TaskAnalysisRef{Path: path, SHA256: digest, ParserVersion: semantic.TaskContextAnalysisVersion})
}

func structuralCheckProjection(result semantic.TaskCheckResult) *workpacket.TaskStructuralCheck {
	return &workpacket.TaskStructuralCheck{
		Tasks:          result.Tasks,
		Cancelled:      result.Cancelled,
		ClausesTotal:   result.ClausesTotal,
		ClausesCovered: result.ClausesCovered,
		Problems:       result.Problems,
	}
}

func taskAnalysisPaths(inputs []workpacket.Reference) []string {
	paths := []string{}
	for _, ref := range inputs {
		if strings.HasPrefix(ref.Path, "docs/dev/tasks/TASK-") &&
			strings.HasSuffix(ref.Path, ".md") &&
			!strings.Contains(strings.ToLower(filepath.Base(ref.Path)), "template") {
			paths = append(paths, ref.Path)
		}
	}
	return paths
}
