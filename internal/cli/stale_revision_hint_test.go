package cli

// stale_revision_hint_test.go — regression tests for the next-action hint
// appended to runtime.ErrStaleRevision by formatFailure. Every verb that
// goes through `--expected-revision` shares this rendering path, so the
// hint has to survive any verb (register-workgroup, agent-event,
// review-result submit, finding-supplement, etc.) without the caller
// having to decode which verb raised the CAS conflict.
//
// The fix is at the formatFailure boundary (not at the verb handlers),
// so these tests exercise the helper directly and assert the
// actionable-next-action keywords the operator needs to recover.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// TestFormatFailureStaleRevisionCarriesNextAction verifies the bare
// runtime.ErrStaleRevision surfaces the recovery recipe in the rendered
// line — every keyword the operator needs to recover from a CAS conflict
// without reading source code must be present.
func TestFormatFailureStaleRevisionCarriesNextAction(t *testing.T) {
	rendered := formatFailure("runtime register-workgroup", runtime.ErrStaleRevision)

	// The verb name must still be prefixed (existing contract).
	if !strings.HasPrefix(rendered, "runtime register-workgroup: ") {
		t.Fatalf("rendered line missing verb prefix: %q", rendered)
	}
	// The raw error must still be in the line so logs and grep stay useful.
	if !strings.Contains(rendered, "stale runtime revision") {
		t.Fatalf("rendered line lost the raw error: %q", rendered)
	}
	// Next-action: the recovery recipe must be present.
	for _, keyword := range []string{
		"loop-harness status", // read current revision
		"--root",              // required flag of the next command
		"--expected-revision", // retry flag for the original verb
		"runtime reconcile",   // durable cure for concurrent commits
	} {
		if !strings.Contains(rendered, keyword) {
			t.Errorf("rendered line missing recovery keyword %q: %q", keyword, rendered)
		}
	}
}

// TestFormatFailureStaleRevisionWrapped carries through errors.Is: a
// wrapping fmt.Errorf("%w", runtime.ErrStaleRevision) must still trigger
// the hint, so reviewers can wrap the error with verb-specific context
// without losing the actionable next step.
func TestFormatFailureStaleRevisionWrapped(t *testing.T) {
	wrapped := fmt.Errorf("runtime review-result submit: %w", runtime.ErrStaleRevision)
	rendered := formatFailure("runtime review-result", wrapped)

	if !errors.Is(wrapped, runtime.ErrStaleRevision) {
		t.Fatal("test setup: errors.Is should match the wrapped error")
	}
	if !strings.Contains(rendered, "loop-harness status") {
		t.Fatalf("wrapped stale revision lost the next-action hint: %q", rendered)
	}
	if !strings.Contains(rendered, "--expected-revision") {
		t.Fatalf("wrapped stale revision lost the retry flag: %q", rendered)
	}
}

// TestFormatFailureStaleRevisionAcrossVerbs confirms the hint is identical
// across every shared-verb path. The verbs below all go through
// --expected-revision and therefore share the recovery recipe.
func TestFormatFailureStaleRevisionAcrossVerbs(t *testing.T) {
	verbs := []string{
		"runtime transition",
		"runtime register-workgroup",
		"runtime agent-begin",
		"runtime agent-event",
		"runtime task-complete",
		"runtime task-integrate",
		"runtime review-result",
		"runtime finding-supplement",
		"runtime bug-event",
	}
	for _, verb := range verbs {
		t.Run(verb, func(t *testing.T) {
			rendered := formatFailure(verb, runtime.ErrStaleRevision)
			if !strings.HasPrefix(rendered, verb+": ") {
				t.Fatalf("verb %q lost its prefix: %q", verb, rendered)
			}
			if !strings.Contains(rendered, "loop-harness status") {
				t.Errorf("verb %q lost the next-action status command: %q", verb, rendered)
			}
			if !strings.Contains(rendered, "runtime reconcile") {
				t.Errorf("verb %q lost the runtime reconcile hint: %q", verb, rendered)
			}
		})
	}
}

// TestFormatFailureUnrelatedErrorsUnchanged guards the non-stale path:
// formatFailure must keep its original "<cmd>: <err>" shape when the
// error is not a CAS conflict, so we don't accidentally leak the new
// hint into other failure modes.
func TestFormatFailureUnrelatedErrorsUnchanged(t *testing.T) {
	rendered := formatFailure("runtime transition", errors.New("decode runtime: unexpected EOF"))
	if rendered != "runtime transition: decode runtime: unexpected EOF" {
		t.Fatalf("non-stale error shape changed: %q", rendered)
	}
	if strings.Contains(rendered, "loop-harness status") {
		t.Fatalf("non-stale error must not carry the stale-revision hint: %q", rendered)
	}
}
