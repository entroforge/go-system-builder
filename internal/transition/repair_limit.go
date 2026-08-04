// repair_limit.go — typed RepairLimitError + structural limit check.
//
// Per BUG-003 §4b.2(f), exceeding max_attempts_per_bug must raise a typed
// RepairLimitError. The caller (the BUG lifecycle executor or the hook
// adapter) catches the typed error and dispatches transition.Apply(GTR-004)
// which transitions the runtime to paused with capture_pause_checkpoint.
//
// The limit check is structural: it reads state.configuration.repair (the
// canonical schema field) and the BUG's current attempt_count. The check is
// invoked from two paths:
//  1. The BUG lifecycle executor when a closing_contract_failed event
//     triggers a retry into investigating.
//  2. The repair completion action that also raises RepairLimitError when
//     attempts have been exhausted.
//
// Both paths surface the same typed error so the dispatcher can handle it
// uniformly.
package transition

import (
	"fmt"
	"net/http"
)

// RepairLimitError is the typed signal that a BUG has exceeded its allowed
// number of repair attempts. Per BUG-003 §4b.2(f) this is the SOLE error
// shape the dispatcher recognizes for the GTR-004 bridge. A typed error is
// used (not a sentinel + payload) so callers can inspect the bug id, the
// current attempt count, and the configured maximum without parsing a
// string.
//
// fields:
//   - BugID:    the canonical BUG id (e.g. "BUG-003").
//   - Attempts: the attempt count that triggered the limit (>= Max).
//   - Max:      the configured max_attempts_per_bug from
//     state.configuration.repair.max_attempts_per_bug.
type RepairLimitError struct {
	BugID    string
	Attempts int
	Max      int
}

// Error implements the error interface. The message is intentionally
// machine-parseable so logs can extract BugID/Attempts/Max without relying
// on %w chain inspection.
func (e *RepairLimitError) Error() string {
	return fmt.Sprintf("repair_limit_exceeded: BUG %s attempts=%d >= max=%d", e.BugID, e.Attempts, e.Max)
}

// StatusCode returns the HTTP-style code associated with this error. The
// dispatcher does not need this today; it is provided so external callers
// (CLI, hook layer) can map to user-facing messages without re-parsing the
// Error() string.
func (e *RepairLimitError) StatusCode() int { return http.StatusFailedDependency }

// CheckRepairLimit inspects the runtime state and the supplied bug entity
// and returns *RepairLimitError when bug.attempt_count has reached or
// exceeded the configured max_attempts_per_bug. The check returns nil when:
//   - the runtime has no configuration block (legacy state),
//   - the configuration has no repair block,
//   - max_attempts_per_bug is unset or zero (treated as "no limit"), or
//   - the BUG has not yet reached the cap.
//
// On any structural read failure the function returns nil and lets the
// caller proceed. The limit is a structural safety net; missing config must
// never silently block a BUG's lifecycle.
func CheckRepairLimit(state map[string]any, bug map[string]any) *RepairLimitError {
	if state == nil || bug == nil {
		return nil
	}
	configuration, ok := state["configuration"].(map[string]any)
	if !ok {
		return nil
	}
	repair, ok := configuration["repair"].(map[string]any)
	if !ok {
		return nil
	}
	maxAttempts := intField(repair, "max_attempts_per_bug")
	if maxAttempts <= 0 {
		return nil
	}
	attempts := intField(bug, "attempt_count")
	if attempts < maxAttempts {
		return nil
	}
	id, _ := bug["id"].(string)
	return &RepairLimitError{BugID: id, Attempts: attempts, Max: maxAttempts}
}

// intField is a defensive integer extractor for json-decoded map[string]any
// values. JSON numbers decode as float64; the helper also accepts native
// ints for tests that build maps directly.
func intField(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}
