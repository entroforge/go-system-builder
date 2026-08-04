package transition_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

// TestRepairLimitErrorMessageFormat covers repair_limit.go:46-48 — the
// Error() string is intentionally machine-parseable so logs can extract
// BugID/Attempts/Max without %w chain inspection.
func TestRepairLimitErrorMessageFormat(t *testing.T) {
	err := &transition.RepairLimitError{BugID: "BUG-003", Attempts: 3, Max: 3}
	if !strings.Contains(err.Error(), "BUG-003") {
		t.Fatalf("error must mention bug id BUG-003, got: %v", err)
	}
	if !strings.Contains(err.Error(), "attempts=3") {
		t.Fatalf("error must mention attempts=3, got: %v", err)
	}
	if !strings.Contains(err.Error(), "max=3") {
		t.Fatalf("error must mention max=3, got: %v", err)
	}
}

// TestRepairLimitErrorStatusCode covers repair_limit.go:54 — StatusCode
// returns the canonical HTTP code so external callers can map to user-
// facing messages.
func TestRepairLimitErrorStatusCode(t *testing.T) {
	err := &transition.RepairLimitError{BugID: "BUG-001", Attempts: 1, Max: 1}
	if got := err.StatusCode(); got != http.StatusFailedDependency {
		t.Fatalf("StatusCode = %d, want %d", got, http.StatusFailedDependency)
	}
}

// TestCheckRepairLimitTriggersAtMax covers repair_limit.go:67-89 — when
// bug.attempt_count >= max_attempts_per_bug, CheckRepairLimit must
// return a *RepairLimitError.
func TestCheckRepairLimitTriggersAtMax(t *testing.T) {
	state := map[string]any{
		"configuration": map[string]any{
			"repair": map[string]any{"max_attempts_per_bug": float64(3)},
		},
	}
	bug := map[string]any{"id": "BUG-001", "attempt_count": float64(3)}
	err := transition.CheckRepairLimit(state, bug)
	if err == nil {
		t.Fatal("CheckRepairLimit must return *RepairLimitError at the cap")
	}
	var rle *transition.RepairLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("returned error must be *RepairLimitError, got %T", err)
	}
	if rle.BugID != "BUG-001" || rle.Attempts != 3 || rle.Max != 3 {
		t.Fatalf("limit fields wrong: %+v", rle)
	}
}

// TestCheckRepairLimitTriggersAboveMax covers repair_limit.go:84 — when
// bug.attempt_count > max_attempts_per_bug (defensive), the function
// still returns an error.
func TestCheckRepairLimitTriggersAboveMax(t *testing.T) {
	state := map[string]any{
		"configuration": map[string]any{
			"repair": map[string]any{"max_attempts_per_bug": float64(2)},
		},
	}
	bug := map[string]any{"id": "BUG-002", "attempt_count": float64(5)}
	err := transition.CheckRepairLimit(state, bug)
	if err == nil {
		t.Fatal("CheckRepairLimit must return *RepairLimitError above the cap")
	}
	if err.Max != 2 || err.Attempts != 5 {
		t.Fatalf("limit fields wrong: %+v", err)
	}
}

// TestCheckRepairLimitPassesWhenUnderMax covers the happy path — bug
// has not yet reached the cap.
func TestCheckRepairLimitPassesWhenUnderMax(t *testing.T) {
	state := map[string]any{
		"configuration": map[string]any{
			"repair": map[string]any{"max_attempts_per_bug": float64(5)},
		},
	}
	bug := map[string]any{"id": "BUG-001", "attempt_count": float64(2)}
	if err := transition.CheckRepairLimit(state, bug); err != nil {
		t.Fatalf("CheckRepairLimit must return nil under the cap, got: %v", err)
	}
}

// TestCheckRepairLimitPassesWhenNoConfig covers the legacy path — the
// runtime has no configuration block, so the check is a no-op.
func TestCheckRepairLimitPassesWhenNoConfig(t *testing.T) {
	state := map[string]any{}
	bug := map[string]any{"id": "BUG-001", "attempt_count": float64(99)}
	if err := transition.CheckRepairLimit(state, bug); err != nil {
		t.Fatalf("CheckRepairLimit must return nil when no config, got: %v", err)
	}
}

// TestCheckRepairLimitPassesWhenNoRepairBlock covers the partial-config
// path — the configuration has no repair block, so the check is a no-op.
func TestCheckRepairLimitPassesWhenNoRepairBlock(t *testing.T) {
	state := map[string]any{
		"configuration": map[string]any{"other": "value"},
	}
	bug := map[string]any{"id": "BUG-001", "attempt_count": float64(99)}
	if err := transition.CheckRepairLimit(state, bug); err != nil {
		t.Fatalf("CheckRepairLimit must return nil when no repair block, got: %v", err)
	}
}

// TestCheckRepairLimitPassesWhenNoMaxConfigured covers the
// no-max-configured path — max_attempts_per_bug is missing, so the
// check is a no-op.
func TestCheckRepairLimitPassesWhenNoMaxConfigured(t *testing.T) {
	state := map[string]any{
		"configuration": map[string]any{"repair": map[string]any{}},
	}
	bug := map[string]any{"id": "BUG-001", "attempt_count": float64(99)}
	if err := transition.CheckRepairLimit(state, bug); err != nil {
		t.Fatalf("CheckRepairLimit must return nil when max_attempts_per_bug is unset, got: %v", err)
	}
}

// TestCheckRepairLimitAcceptsNativeInt covers the intField helper —
// native int values (not just float64) are accepted.
func TestCheckRepairLimitAcceptsNativeInt(t *testing.T) {
	state := map[string]any{
		"configuration": map[string]any{
			"repair": map[string]any{"max_attempts_per_bug": 3},
		},
	}
	bug := map[string]any{"id": "BUG-001", "attempt_count": 3}
	if err := transition.CheckRepairLimit(state, bug); err == nil {
		t.Fatal("CheckRepairLimit must return *RepairLimitError with native int fields")
	}
}
