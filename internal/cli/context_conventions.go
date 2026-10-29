package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/projectconventions"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func conventionFiles(root string, state map[string]any) (*fileview.View, error) {
	ref, err := fileview.DevelopmentRef(state)
	if err != nil {
		return nil, err
	}
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		return nil, err
	}
	return fileview.New(root, "refs/heads/"+strings.TrimPrefix(ref, "refs/heads/"), catalog.Definition.FileSources)
}
func contextConventions(root string, state map[string]any, a hookctx.AssignmentContext, p *workpacket.Packet) (*projectconventions.Bundle, error) {
	if p != nil {
		if p.TaskAnalysis == nil {
			return nil, nil
		}
		artifact, err := workpacket.ReadTaskAnalysis(root, *p)
		if err != nil {
			return nil, err
		}
		if len(artifact.TaskConventions) == 0 {
			return nil, nil
		}
		b := &projectconventions.Bundle{ResolverVersion: artifact.ConventionsResolverVersion}
		if err := json.Unmarshal(artifact.TaskConventions, &b.TaskConventions); err != nil {
			return nil, err
		}
		return b, nil
	}
	life, _ := state["lifecycle"].(map[string]any)
	if life["state"] != "building" || (a.RoleFamily != "backend-builder" && a.RoleFamily != "frontend-builder" && a.RoleFamily != "test-builder") {
		return nil, nil
	}
	files, err := conventionFiles(root, state)
	if err != nil {
		return nil, err
	}
	documents, err := runtimepkg.CurrentFormalInputDocuments(root, state, files)
	if err != nil {
		return nil, fmt.Errorf("conventions formal input selection is unverifiable: %w", err)
	}
	approved := []projectconventions.Reference{}
	paths := []string{}
	for _, document := range documents {
		ref := projectconventions.Reference{ID: document.Reference.ID, Path: document.Reference.Path, Version: document.Reference.Version, SHA256: document.Reference.SHA256}
		approved = append(approved, ref)
		if document.Kind == "task" && document.Reference.ID == a.TaskID {
			paths = append(paths, ref.Path)
		}
	}
	if len(paths) != 1 {
		return nil, fmt.Errorf("conventions require the original uniquely registered Builder TASK")
	}
	summaries, _, err := semantic.AnalyzeApprovedTaskContexts(root, files, paths)
	if err != nil {
		return nil, err
	}
	for i := range summaries {
		summaries[i].Writes = projectconventions.CommonScope(summaries[i].Writes, a.WritePaths)
	}
	rows := semantic.ProjectTaskConventions(root, files, summaries, approved)
	for i, row := range rows {
		if len(summaries[i].Writes) == 0 {
			rows[i].Questions = append(row.Questions, "No proven TASK/Assignment scope intersection; resolve the original scope without broadening it.")
		}
	}
	if err := files.Verify(); err != nil {
		return nil, err
	}
	return &projectconventions.Bundle{ResolverVersion: projectconventions.Version, TaskConventions: rows}, nil
}
func verifyConventionView(root string, state map[string]any, b *projectconventions.Bundle, inputs map[string]string) error {
	if b == nil {
		return nil
	}
	if err := b.ValidateInputs(inputs); err != nil {
		return err
	}
	files, err := conventionFiles(root, state)
	if err != nil {
		return err
	}
	documents, err := runtimepkg.CurrentFormalInputDocuments(root, state, files)
	if err != nil {
		return fmt.Errorf("convention formal input selection is unverifiable: %w", err)
	}
	tasks := map[string]bool{}
	registered := []projectconventions.Reference{}
	for _, document := range documents {
		ref := document.Reference
		registered = append(registered, projectconventions.Reference{ID: ref.ID, Path: ref.Path, Version: ref.Version, SHA256: ref.SHA256})
		if document.Kind == "task" {
			tasks[ref.Path] = true
		}
	}
	for _, row := range b.TaskConventions {
		for _, entry := range row.Entries {
			if entry.Status != "declared_default" {
				continue
			}
			for _, ref := range entry.Entry.SourceRefs {
				if ref.Kind == "rule" {
					continue
				}
				found := false
				for _, current := range registered {
					if current.ID == ref.ID && current.Path == ref.Path && current.Version == ref.Version && current.SHA256 == ref.SHA256 {
						found = true
					}
				}
				if !found {
					return fmt.Errorf("convention's original formal input proof changed: %s", ref.Path)
				}
			}
		}
	}
	sources, err := b.Sources(tasks)
	if err != nil {
		return err
	}
	for path, hash := range sources {
		data, err := files.ReadFile(path)
		if err != nil || workpacket.Digest(data) != hash {
			return fmt.Errorf("convention source differs from original committed authority: %s", path)
		}
	}
	return files.Verify()
}
func mergeConventionInputs(root string, b *projectconventions.Bundle, inputs map[string]string) error {
	if b == nil {
		return nil
	}
	tasks := map[string]bool{}
	for _, row := range b.TaskConventions {
		tasks[row.TaskRef] = inputs[row.TaskRef] != ""
	}
	sources, err := b.Sources(tasks)
	if err != nil {
		return err
	}
	for path, hash := range sources {
		if old, ok := inputs[path]; ok && old != hash {
			return fmt.Errorf("convention source differs from original frozen input: %s", path)
		}
		data, err := workpacket.ReadFile(root, path, 8<<20)
		if err != nil || workpacket.Digest(data) != hash {
			return fmt.Errorf("convention source is unavailable or changed: %s", path)
		}
		inputs[path] = hash
	}
	return b.ValidateInputs(inputs)
}
