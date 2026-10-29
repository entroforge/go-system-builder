package cli

// Shared CAS feedback must preserve the original cause and identify recovery
// ownership without guessing whether a number came from an author or adapter.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestFormatFailureInternalCASDoesNotInventAuthorAssertion(t *testing.T) {
	for _, command := range []string{"s6 prepare", "S8 result reception", "S11 decision application"} {
		rendered := formatFailure(command, fmt.Errorf("original responsibility changed: %w", runtime.ErrStaleRevision))
		for _, falseRecovery := range []string{"used an explicit revision", "--expected-revision <N>", "runtime reconcile"} {
			if strings.Contains(rendered, falseRecovery) {
				t.Errorf("%s invented author responsibility or persistence damage: %s", command, rendered)
			}
		}
		if !strings.Contains(rendered, "Main") || !strings.Contains(rendered, "receipt") || !strings.Contains(rendered, "stale runtime revision") {
			t.Errorf("%s omitted conflict ownership or original evidence: %s", command, rendered)
		}
	}
}

// TestFormatFailureStaleRevisionCarriesNextAction verifies the bare
// runtime.ErrStaleRevision surfaces the recovery recipe in the rendered
// line while retaining a concrete original-state query and Main ownership.
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
		"loop-harness status", // inspect the current responsibility
		"--root",
		"Main",
		"receipt",
		"business snapshot", // explicit CAS callers still own their assertion
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
	if !strings.Contains(rendered, "original receipt") {
		t.Fatalf("wrapped stale revision lost its original recovery source: %q", rendered)
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
			if !strings.Contains(rendered, "Main") || !strings.Contains(rendered, "receipt") {
				t.Errorf("verb %q lost conflict ownership or original receipt: %q", verb, rendered)
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
