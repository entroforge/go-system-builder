package e2erun

import (
	"fmt"
	"sort"
	"strings"
)

// ValidateCollectionCoverage checks exact selection and oracle coverage. It
// proves only that the controlled runner collected eligible annotated tests;
// it never creates execution results or treats annotation text as an assertion.
func ValidateCollectionCoverage(required []Requirement, collection Receipt, selectedIDs []string) error {
	if collection.Mode != "collection" || !collection.Complete || collection.Outcome != "pass" || collection.ExitCode != 0 || len(collection.Attempts) != 0 {
		return fmt.Errorf("collection is not a complete successful test discovery")
	}
	if len(required) == 0 || len(selectedIDs) == 0 {
		return fmt.Errorf("collection coverage requires explicit requirements and selected tests")
	}
	wanted, covered := map[string]bool{}, map[string]string{}
	for _, requirement := range required {
		key := RequirementKey(requirement)
		if wanted[key] {
			return fmt.Errorf("duplicate collection requirement %s", key)
		}
		wanted[key] = true
	}
	byID := map[string]Test{}
	for _, test := range collection.Tests {
		if _, exists := byID[test.ID]; exists || test.ID == "" {
			return fmt.Errorf("collection has a missing/duplicate test ID")
		}
		byID[test.ID] = test
	}
	selected := map[string]bool{}
	for _, id := range selectedIDs {
		test, ok := byID[id]
		if !ok || selected[id] || test.ExpectedStatus != "passed" {
			return fmt.Errorf("selected test %s is missing, repeated or ineligible", id)
		}
		selected[id] = true
		matched := false
		annotations := map[string]bool{}
		for _, ref := range test.CaseRefs {
			key := RequirementKey(Requirement{CaseRef: ref, Project: test.Project, Browser: test.Browser})
			if annotations[key] {
				return fmt.Errorf("test %s repeats the same CASE/oracle annotation", id)
			}
			annotations[key] = true
			if !wanted[key] {
				continue
			}
			matched = true
			if prior := covered[key]; prior != "" {
				return fmt.Errorf("ambiguous collection coverage for %s: %s and %s", key, prior, id)
			}
			covered[key] = id
		}
		if !matched {
			return fmt.Errorf("selected test %s answers no assigned CASE/oracle/environment requirement", id)
		}
	}
	missing := []string{}
	for key := range wanted {
		if covered[key] == "" {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) != 0 {
		return fmt.Errorf("collection lacks assigned CASE/oracle/environment tuples: %s", strings.Join(missing, ", "))
	}
	return nil
}
