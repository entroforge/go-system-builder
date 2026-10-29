package semantic

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

type approvedTaskFiles struct {
	fileview.Reader
	root    string
	allowed map[string]bool
}

func (f approvedTaskFiles) ReadDir(p string) ([]os.DirEntry, error) {
	entries, err := f.Reader.ReadDir(p)
	if err != nil {
		return nil, err
	}
	if filepath.Clean(p) != filepath.Join(f.root, "docs/dev/tasks") {
		return entries, nil
	}
	result := []os.DirEntry{}
	for _, e := range entries {
		if f.allowed[filepath.ToSlash(filepath.Join("docs/dev/tasks", e.Name()))] {
			result = append(result, e)
		}
	}
	return result, nil
}
func (f approvedTaskFiles) Source(p string) (string, error) {
	if sourced, ok := f.Reader.(interface{ Source(string) (string, error) }); ok {
		return sourced.Source(p)
	}
	return "disk", nil
}

// AnalyzeApprovedTaskContexts limits discovery to the issuer's formal TASKs.
// It does not register a batch, interpret assertions, or change permissions.
func AnalyzeApprovedTaskContexts(root string, files fileview.Reader, paths []string) ([]TaskContextSummary, []TaskGranularityAdvisory, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	allowed := map[string]bool{}
	for _, p := range paths {
		allowed[p] = true
	}
	tasks, err := loadTaskDocumentsWithFiles(abs, approvedTaskFiles{Reader: files, root: abs, allowed: allowed})
	if err != nil {
		return nil, nil, err
	}
	if len(tasks) != len(allowed) {
		return nil, nil, fmt.Errorf("approved TASK analysis cannot locate the exact registered task set")
	}
	summaries := analyzeTaskContexts(abs, files, tasks)
	if summaries == nil {
		summaries = []TaskContextSummary{}
	}
	return summaries, AnalyzeTaskGranularity(summaries), nil
}
