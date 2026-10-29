package semantic

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/scenario"
)

const TaskBehaviorProjectionVersion = "1.0.0"

type TaskBehaviorProjection struct {
	TaskID           string                    `json:"task_id"`
	TaskRef          string                    `json:"task_ref"`
	CaseRef          string                    `json:"case_ref,omitempty"`
	Source           string                    `json:"source"`
	Mode             string                    `json:"mode,omitempty"`
	Purpose          string                    `json:"purpose,omitempty"`
	ConditionUnknown bool                      `json:"condition_unknown"`
	Status           string                    `json:"status"`
	Example          *scenario.BehaviorExample `json:"example,omitempty"`
	Question         string                    `json:"question,omitempty"`
}

var taskCaseReference = regexp.MustCompile(`\bCASE-[A-Z0-9]+(?:-[A-Z0-9]+)*\b`)

func ProjectTaskBehavior(root string, files fileview.Reader, summaries []TaskContextSummary) []TaskBehaviorProjection {
	result := []TaskBehaviorProjection{}
	type cached struct {
		examples map[string]scenario.BehaviorExample
		err      error
	}
	cache := map[string]cached{}
	for _, task := range summaries {
		seen := map[string]bool{}
		for _, ref := range task.Reading {
			if !strings.HasPrefix(ref.Path, "docs/design/prototypes/") || filepath.Base(ref.Path) != "cases.json" {
				continue
			}
			parts := strings.Split(ref.Path, "/")
			if len(parts) != 5 {
				continue
			}
			module := parts[3]
			ids := uniqueContextStrings(taskCaseReference.FindAllString(ref.Clauses, -1))
			if len(ids) == 0 {
				result = append(result, TaskBehaviorProjection{TaskID: task.TaskID, TaskRef: task.TaskRef, Source: ref.Source, Mode: ref.Mode, Purpose: ref.Purpose, ConditionUnknown: ref.ConditionUnknown, Status: "unresolved", Question: "Name the specific approved CASE references this TASK needs; a module reading link does not assign every CASE."})
				continue
			}
			entry, ok := cache[module]
			if !ok && ref.Status == "readable" {
				entry.examples, entry.err = scenario.ProjectBehaviorWithFiles(root, module, files)
				cache[module] = entry
			}
			for _, id := range ids {
				key := ref.Source + "/" + module + "/" + id
				if seen[key] {
					continue
				}
				seen[key] = true
				p := TaskBehaviorProjection{TaskID: task.TaskID, TaskRef: task.TaskRef, CaseRef: id, Source: ref.Source, Mode: ref.Mode, Purpose: ref.Purpose, ConditionUnknown: ref.ConditionUnknown, Status: "unresolved"}
				if ref.Status != "readable" {
					p.Question = "Restore the declared current CASE input through the original source view."
				} else if entry.err != nil {
					p.Question = "Current scenario source package is not validated: " + entry.err.Error()
				} else if example, ok := entry.examples[id]; ok {
					copy := example
					p.Example = &copy
					p.Status = "source_example"
				} else {
					p.Question = "The declared CASE does not exist in the current validated module; resolve its original contract/design responsibility."
				}
				result = append(result, p)
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.TaskID != b.TaskID {
			return a.TaskID < b.TaskID
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.CaseRef < b.CaseRef
	})
	return result
}
