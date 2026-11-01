// Package productbaseline contains immutable, dependency-neutral S6 baseline
// records shared by the TR-003 writer and recovery validators.
package productbaseline

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	S6MainStartContract = "s6-main-start-v1"
	MaxS6MainStarts     = 128
)

// Task is the exact document identity registered and locked by the original
// TR-003 transition. These values are historical facts and are not refreshed
// when a later Assignment revision changes a TASK.
type Task struct {
	TaskID  string `json:"task_id"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

// S6MainStart is the durable common Main responsibility captured by the
// original TR-003 Writer transaction.
type S6MainStart struct {
	Contract                string `json:"contract"`
	RuntimeID               string `json:"runtime_id"`
	BaselineGeneration      int    `json:"baseline_generation"`
	ProjectRoot             string `json:"project_root"`
	DevelopmentBranch       string `json:"development_branch"`
	TransitionEventID       string `json:"transition_event_id"`
	TransitionAfterRevision int    `json:"transition_after_revision"`
	TransitionSequence      int    `json:"transition_sequence"`
	TaskSet                 []Task `json:"task_set"`
}

func SortTasks(tasks []Task) []Task {
	out := append([]Task(nil), tasks...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].TaskID != out[j].TaskID {
			return out[i].TaskID < out[j].TaskID
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// Seal prepares the original task responsibility and event coordinates.
func Seal(record S6MainStart) (S6MainStart, error) {
	record.Contract = S6MainStartContract
	record.TaskSet = SortTasks(record.TaskSet)
	if err := record.Validate(); err != nil {
		return S6MainStart{}, err
	}
	return record, nil
}

func (record S6MainStart) Validate() error {
	if record.Contract != S6MainStartContract {
		return fmt.Errorf("contract must be %q", S6MainStartContract)
	}
	if !strings.HasPrefix(record.RuntimeID, "loop-") || strings.TrimSpace(record.RuntimeID) != record.RuntimeID {
		return fmt.Errorf("runtime_id is invalid")
	}
	if record.BaselineGeneration < 1 {
		return fmt.Errorf("baseline_generation must be positive")
	}
	if !filepath.IsAbs(record.ProjectRoot) || filepath.Clean(record.ProjectRoot) != record.ProjectRoot {
		return fmt.Errorf("project_root must be an absolute normalized path")
	}
	if strings.TrimSpace(record.DevelopmentBranch) == "" ||
		strings.HasPrefix(record.DevelopmentBranch, "refs/") ||
		strings.TrimSpace(record.DevelopmentBranch) != record.DevelopmentBranch {
		return fmt.Errorf("development_branch must be a normalized branch name")
	}
	if strings.TrimSpace(record.TransitionEventID) == "" {
		return fmt.Errorf("transition_event_id is required")
	}
	if record.TransitionAfterRevision < 1 || record.TransitionSequence < 1 {
		return fmt.Errorf("transition after_revision and sequence must be positive")
	}
	if len(record.TaskSet) == 0 {
		return fmt.Errorf("task_set must be non-empty")
	}
	seenIDs := make(map[string]bool, len(record.TaskSet))
	seenPaths := make(map[string]bool, len(record.TaskSet))
	for i, task := range record.TaskSet {
		if strings.TrimSpace(task.TaskID) == "" || strings.TrimSpace(task.TaskID) != task.TaskID {
			return fmt.Errorf("task_set[%d].task_id is required", i)
		}
		if task.Path == "" || path.Clean(task.Path) != task.Path || path.IsAbs(task.Path) || task.Path == "." || strings.HasPrefix(task.Path, "../") || strings.Contains(task.Path, "\\") {
			return fmt.Errorf("task_set[%d].path must be a normalized repository-relative path", i)
		}
		if strings.TrimSpace(task.Version) == "" || strings.TrimSpace(task.Version) != task.Version {
			return fmt.Errorf("task_set[%d].version is required", i)
		}
		if seenIDs[task.TaskID] || seenPaths[task.Path] {
			return fmt.Errorf("task_set contains duplicate task id or path")
		}
		seenIDs[task.TaskID] = true
		seenPaths[task.Path] = true
		if i > 0 {
			prev := record.TaskSet[i-1]
			if prev.TaskID > task.TaskID || (prev.TaskID == task.TaskID && prev.Path >= task.Path) {
				return fmt.Errorf("task_set must be sorted by task_id and path")
			}
		}
	}
	return nil
}

// ReadS6MainStarts decodes original responsibilities from the existing baseline.
// Unrelated fields do not change or authenticate the recorded responsibility.
func ReadS6MainStarts(state map[string]any) ([]S6MainStart, error) {
	baseline, _ := state["baseline"].(map[string]any)
	if baseline == nil {
		return nil, fmt.Errorf("runtime baseline is missing")
	}
	raw, exists := baseline["s6_main_starts"]
	if !exists || raw == nil {
		return []S6MainStart{}, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var starts []S6MainStart
	if err := json.Unmarshal(data, &starts); err != nil {
		return nil, fmt.Errorf("decode s6_main_starts: %w", err)
	}
	if len(starts) > MaxS6MainStarts {
		return nil, fmt.Errorf("s6_main_starts exceeds %d entries", MaxS6MainStarts)
	}
	previous := 0
	for i := range starts {
		if err := starts[i].Validate(); err != nil {
			return nil, fmt.Errorf("s6_main_starts[%d]: %w", i, err)
		}
		if starts[i].BaselineGeneration <= previous {
			return nil, fmt.Errorf("s6_main_starts must have unique increasing generations")
		}
		previous = starts[i].BaselineGeneration
	}
	return starts, nil
}

func FindS6MainStart(state map[string]any, generation int) (S6MainStart, bool, error) {
	starts, err := ReadS6MainStarts(state)
	if err != nil {
		return S6MainStart{}, false, err
	}
	for _, start := range starts {
		if start.BaselineGeneration == generation {
			return start, true, nil
		}
	}
	return S6MainStart{}, false, nil
}

func AppendS6MainStart(state map[string]any, start S6MainStart) error {
	if err := start.Validate(); err != nil {
		return err
	}
	starts, err := ReadS6MainStarts(state)
	if err != nil {
		return err
	}
	if len(starts) >= MaxS6MainStarts {
		return fmt.Errorf("s6_main_starts capacity %d is exhausted", MaxS6MainStarts)
	}
	if len(starts) > 0 && start.BaselineGeneration <= starts[len(starts)-1].BaselineGeneration {
		return fmt.Errorf("new S6 Main start generation must exceed all retained generations")
	}
	starts = append(starts, start)
	baseline, _ := state["baseline"].(map[string]any)
	encoded, err := json.Marshal(starts)
	if err != nil {
		return err
	}
	var raw []any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return err
	}
	baseline["s6_main_starts"] = raw
	return nil
}
