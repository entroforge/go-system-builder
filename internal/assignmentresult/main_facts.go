package assignmentresult

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

// ResultBinding names the already accepted result and its original responsibility.
type ResultBinding struct {
	AssignmentRecord assignmentrecord.RecordReference `json:"assignment_record"`
	ResultPath       string                           `json:"result_path"`
	ResultRevision   int                              `json:"result_revision"`
}

type MainCheckFact struct {
	CheckIndex int    `json:"check_index"`
	Command    string `json:"command"`
	Status     string `json:"status"`
	ReceiptRef string `json:"receipt_ref,omitempty"`
	LogRef     string `json:"log_ref,omitempty"`
}

type MainFacts struct {
	Binding             ResultBinding   `json:"binding"`
	MainRoot            string          `json:"main_root"`
	TargetBranch        string          `json:"target_branch"`
	ExecutionGeneration int             `json:"execution_generation"`
	ChangedPaths        []string        `json:"changed_paths"`
	Checks              []MainCheckFact `json:"checks"`
}

// ValidateChangedPaths validates the real path set without a content digest.
func ValidateChangedPaths(paths []string) error {
	if paths == nil {
		return fmt.Errorf("actual changed paths must be a JSON array")
	}
	for i, file := range paths {
		if file == "" || file == "." || file == ".." || strings.HasPrefix(file, "/") || strings.HasPrefix(file, "../") || path.Clean(file) != file || strings.ContainsAny(file, "\\\x00") || i > 0 && paths[i-1] >= file {
			return fmt.Errorf("actual changed paths must be safe, sorted and unique")
		}
	}
	return nil
}

// ValidateMainFacts preserves the original result responsibility and actual
// required-check conclusions. Legacy digests and Git OIDs are not validity
// conditions; the saved facts do not authenticate later external changes.
func ValidateMainFacts(record assignmentrecord.Record, result AcceptedResult, resultRef string, facts MainFacts) error {
	if facts.Binding.AssignmentRecord != record.Reference() || facts.Binding.ResultPath != resultRef || facts.Binding.ResultRevision != result.ResultRevision || result.AssignmentRecord != record.Reference() || facts.MainRoot == "" || facts.TargetBranch == "" {
		return fmt.Errorf("Main facts do not correspond to the original Record and Result")
	}
	if facts.ChangedPaths != nil {
		if err := ValidateChangedPaths(facts.ChangedPaths); err != nil {
			return err
		}
		for _, file := range facts.ChangedPaths {
			if !assignmentrecord.AllowsPath(record, file) {
				return fmt.Errorf("Main actual diff escapes canonical scope: %s", file)
			}
		}
	}
	required := map[int]string{}
	for index, command := range record.RequiredChecks {
		if !strings.HasPrefix(command, "locked:") {
			required[index] = command
		}
	}
	if len(facts.Checks) != len(required) {
		return fmt.Errorf("Main facts lack original RequiredCheck results")
	}
	seen := map[int]bool{}
	for _, fact := range facts.Checks {
		command, exists := required[fact.CheckIndex]
		if !exists || seen[fact.CheckIndex] || command != fact.Command || fact.Status != "pass" {
			return fmt.Errorf("Main check differs from its original required responsibility")
		}
		seen[fact.CheckIndex] = true
	}
	return nil
}

func CanonicalChangedPaths(paths []string) ([]string, error) {
	copyOfPaths := append([]string{}, paths...)
	sort.Strings(copyOfPaths)
	err := ValidateChangedPaths(copyOfPaths)
	return copyOfPaths, err
}
