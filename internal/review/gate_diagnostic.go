package review

import "github.com/entroforge/go-system-builder/internal/diagnostic"

// s7GateError keeps validator failures actionable at the tool boundary. The
// validator still returns an ordinary error, but callers can also inspect the
// stable diagnostic code and recovery fields without parsing prose.
func s7GateError(code, summary string, missing, repair []string, next string) error {
	return diagnostic.New(diagnostic.ErrorInput{
		Code:    code,
		Summary: summary,
		Missing: missing,
		Repair:  repair,
		Next:    next,
		Verify:  "loop-harness s7 status",
		Ref:     "docs/agent-protocol.md#s7",
	})
}
