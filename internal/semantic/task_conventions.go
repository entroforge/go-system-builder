package semantic

import (
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/projectconventions"
)

func ProjectTaskConventions(root string, files fileview.Reader, tasks []TaskContextSummary, approved []projectconventions.Reference) []projectconventions.Projection {
	requests := []projectconventions.Request{}
	for _, task := range tasks {
		refs := []projectconventions.Reference{}
		for _, r := range task.Reading {
			if r.Path != "" {
				refs = append(refs, projectconventions.Reference{Path: r.Path, Anchor: r.Anchor, SHA256: r.SHA256})
			}
		}
		requests = append(requests, projectconventions.Request{TaskID: task.TaskID, TaskRef: task.TaskRef, Scope: task.Writes, Reading: refs, ApprovedRefs: approved})
	}
	return projectconventions.ResolveMany(root, files, requests)
}
