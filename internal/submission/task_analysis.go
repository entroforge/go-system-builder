package submission

import (
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
	inputs := make([]workpacket.Reference, len(p.Inputs))
	for i, ref := range p.Inputs {
		inputs[i] = workpacket.Reference{ID: ref.ID, Path: ref.Path, Version: ref.Version}
	}
	artifact := workpacket.TaskAnalysisArtifact{
		SchemaVersion:     "1.0.0",
		ParserVersion:     semantic.TaskContextAnalysisVersion,
		Inputs:            inputs,
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
	for _, ref := range inputs {
		approved = append(approved, projectconventions.Reference{ID: ref.ID, Path: ref.Path, Version: ref.Version})
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
	// A prior pathname alone does not establish that an unissued analysis is
	// this responsibility's original. Preserve it and choose an unused slot;
	// the existing Runtime writer records the slot actually used.
	for n := 0; ; n++ {
		name := p.ID + ".json"
		if n > 0 || len(name) > 200 {
			name = fmt.Sprintf("candidate-%d.json", n)
		}
		path := filepath.ToSlash(filepath.Join(".claude/work-packets/analysis", name))
		created, err := workpacket.PublishNewBounded(s.Root, path, data, 2<<20)
		if err != nil {
			return p, err
		}
		if created {
			return workpacket.BindTaskAnalysis(p, &workpacket.TaskAnalysisRef{Path: path, ParserVersion: semantic.TaskContextAnalysisVersion})
		}
		// The analysis is optional advisory material. If the directory itself
		// is occupied by an unknown non-directory, retain that object and issue
		// the original responsibility without manufacturing an analysis gate.
		if err := workpacket.CheckPath(s.Root, ".claude/work-packets/analysis/probe.json"); err != nil {
			return p, nil
		}
	}
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
