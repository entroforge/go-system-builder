package assignmentresult

import (
	"fmt"
	"math"
)

// ProjectCompactResult expands only references to the author's actual Plan.
// The Plan schema is validated before this projection; the unchanged Result
// schema and original consumer still validate professional judgments, exact
// assertion coverage, evidence and the closing contract afterwards.
//
// The original receiver and the native delivery comparison must agree on this
// projection. The receiver stores the expanded Answer, while the Worker keeps
// the compact professional file in its own worktree, so a comparison that
// skipped the projection would compare two different representations of the
// same professional answer.
func ProjectCompactResult(plan, result map[string]any, doneWhenCount int) error {
	assertions, _ := plan["assertion_checks"].([]any)
	rows, _ := result["assertion_evidence"].([]any)
	seen := make(map[int]bool)
	for i, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue // The original Result schema explains malformed rows.
		}
		value, compact := row["plan_assertion_index"]
		if !compact {
			continue // Existing full assertion/oracle rows retain their exact bytes.
		}
		pointer := fmt.Sprintf("/assertion_evidence/%d", i)
		index, ok := compactIndex(value, len(assertions))
		if !ok {
			return fmt.Errorf("%s/plan_assertion_index must be an integer index into this Plan's assertion_checks", pointer)
		}
		if seen[index] {
			return fmt.Errorf("%s/plan_assertion_index duplicates Plan assertion %d", pointer, index)
		}
		seen[index] = true
		for _, key := range []string{"assertion", "oracle"} {
			if _, exists := row[key]; exists {
				return fmt.Errorf("%s/%s cannot be combined with plan_assertion_index; use the reference or the original full pair", pointer, key)
			}
		}
		assertion, ok := assertions[index].(map[string]any)
		if !ok {
			return fmt.Errorf("/assertion_checks/%d is not an original Plan assertion", index)
		}
		delete(row, "plan_assertion_index")
		row["assertion"], row["oracle"] = assertion["assertion"], assertion["oracle"]
	}
	closing, _ := result["closing_assessment"].(map[string]any)
	conditions, _ := closing["conditions"].([]any)
	for i, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, explicit := condition["plan_assertion_indexes"]; explicit {
			continue // Explicit null, empty or wrong references are never repaired.
		}
		pointer := fmt.Sprintf("/closing_assessment/conditions/%d", i)
		index, ok := compactIndex(condition["done_when_index"], doneWhenCount)
		if !ok {
			return fmt.Errorf("%s/done_when_index must be an integer index into the TASK Done when conditions", pointer)
		}
		mapped := make([]int, 0)
		for assertionIndex, raw := range assertions {
			assertion, _ := raw.(map[string]any)
			indexes, _ := assertion["done_when_indexes"].([]any)
			for _, value := range indexes {
				if target, ok := compactIndex(value, doneWhenCount); ok && target == index {
					mapped = append(mapped, assertionIndex)
					break
				}
			}
		}
		if len(mapped) == 0 {
			return fmt.Errorf("%s/plan_assertion_indexes cannot be derived: this Plan maps no assertion to Done when %d", pointer, index)
		}
		condition["plan_assertion_indexes"] = mapped
	}
	return nil
}

func compactIndex(value any, length int) (int, bool) {
	index, ok := value.(float64) // Private files are decoded with json.Unmarshal.
	if !ok || math.IsNaN(index) || index < 0 || index >= float64(length) || math.Trunc(index) != index {
		return 0, false
	}
	return int(index), true
}
