package transition

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/productbaseline"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

func captureS6MainStart(state map[string]any, ctx *ActionContext, root string) (productbaseline.S6MainStart, error) {
	if ctx == nil || ctx.Spec.ID != "TR-003" || ctx.Request == nil {
		return productbaseline.S6MainStart{}, fmt.Errorf("S6 Main start capture requires the original TR-003 request")
	}
	bound, _ := state["bound_req"].(map[string]any)
	workspace, _ := bound["workspace"].(map[string]any)
	if workspace == nil {
		return productbaseline.S6MainStart{}, fmt.Errorf("S6 Main start capture requires the bound REQ development workspace")
	}
	boundRoot := stringValue(workspace["project_root"])
	branch := strings.TrimPrefix(stringValue(workspace["dev_branch"]), "refs/heads/")
	if boundRoot == "" || branch == "" || strings.HasPrefix(branch, "refs/") {
		return productbaseline.S6MainStart{}, fmt.Errorf("bound REQ development root/branch is incomplete")
	}
	canonicalRoot, err := canonicalS6MainRoot(root)
	if err != nil {
		return productbaseline.S6MainStart{}, fmt.Errorf("resolve Runtime root for S6 Main start: %w", err)
	}
	canonicalBoundRoot, err := canonicalS6MainRoot(boundRoot)
	if err != nil {
		return productbaseline.S6MainStart{}, fmt.Errorf("resolve bound REQ root for S6 Main start: %w", err)
	}
	if canonicalRoot != canonicalBoundRoot {
		return productbaseline.S6MainStart{}, fmt.Errorf("TR-003 root differs from the bound REQ development workspace")
	}
	if ctx.EventID == "" || ctx.BeforeRevision < 0 || ctx.AfterRevision != ctx.BeforeRevision+1 || ctx.TransitionSequence < 1 {
		return productbaseline.S6MainStart{}, fmt.Errorf("TR-003 locked commit receipt coordinates are incomplete")
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation := integerOf(baseline["generation"])
	if generation < 1 {
		return productbaseline.S6MainStart{}, fmt.Errorf("current baseline generation is invalid")
	}
	prior, found, err := productbaseline.FindS6MainStart(state, generation)
	if err != nil {
		return productbaseline.S6MainStart{}, err
	}
	if found {
		return prior, nil
	}
	// Do not reconstruct an earlier TR-003 from today's worktree. A missing
	// historical record is unknown, not a reason to reject the new transition.
	journal, err := loopruntime.ReadJournalForState(root, state)
	if err != nil {
		return productbaseline.S6MainStart{}, nil
	}
	priorTR003, err := loopruntime.HasCommittedTransitionForGeneration(state, journal, "TR-003", generation)
	if err != nil || priorTR003 {
		return productbaseline.S6MainStart{}, nil
	}
	tasks, err := s6MainStartTaskSet(state, generation)
	if err != nil {
		return productbaseline.S6MainStart{}, err
	}
	if len(tasks) == 0 {
		return productbaseline.S6MainStart{}, fmt.Errorf("S6 Main start cannot be captured for an empty TR-003 TASK set")
	}
	start, err := productbaseline.Seal(productbaseline.S6MainStart{
		RuntimeID:               stringValue(state["runtime_id"]),
		BaselineGeneration:      generation,
		ProjectRoot:             filepath.Clean(canonicalBoundRoot),
		DevelopmentBranch:       branch,
		TransitionEventID:       ctx.EventID,
		TransitionAfterRevision: ctx.AfterRevision,
		TransitionSequence:      ctx.TransitionSequence,
		TaskSet:                 tasks,
	})
	if err != nil {
		return productbaseline.S6MainStart{}, fmt.Errorf("seal S6 Main start record: %w", err)
	}
	if err := productbaseline.AppendS6MainStart(state, start); err != nil {
		return productbaseline.S6MainStart{}, fmt.Errorf("append S6 Main start record: %w", err)
	}
	return start, nil
}

func s6MainStartTaskSet(state map[string]any, generation int) ([]productbaseline.Task, error) {
	docs, _ := state["documents"].([]any)
	tasks := make([]productbaseline.Task, 0)
	for _, raw := range docs {
		doc, _ := raw.(map[string]any)
		if doc == nil || doc["kind"] != "task" || integerOf(doc["generation"]) != generation {
			continue
		}
		task := productbaseline.Task{
			TaskID:  stringValue(doc["id"]),
			Path:    filepath.ToSlash(stringValue(doc["path"])),
			Version: stringValue(doc["version"]),
		}
		if task.Version == "" {
			task.Version = "unversioned"
		}
		tasks = append(tasks, task)
	}
	return productbaseline.SortTasks(tasks), nil
}

func canonicalS6MainRoot(root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		return filepath.Clean(absolute), nil
	}
	return filepath.Clean(resolved), nil
}
