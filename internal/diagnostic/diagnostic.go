// Package diagnostic contains the small, shared error contract used by
// controller-facing tools. A diagnostic is both a human-readable recovery
// message and a machine-readable description of the missing fact and next
// command. It deliberately carries no lifecycle state; it only explains why
// an existing gate refused an operation.
package diagnostic

import (
	"strings"
)

// ErrorInput describes one recoverable gate failure.
type ErrorInput struct {
	Code    string   `json:"code"`
	Summary string   `json:"summary"`
	Missing []string `json:"missing,omitempty"`
	Repair  []string `json:"repair,omitempty"`
	Next    string   `json:"next,omitempty"`
	Verify  string   `json:"verify,omitempty"`
	Ref     string   `json:"ref,omitempty"`
}

// Error is a structured, recoverable gate failure.
type Error struct {
	ErrorInput
}

// New constructs a diagnostic error. Code and Summary are intentionally
// required by convention; callers that omit them still get a useful error
// rather than a panic.
func New(input ErrorInput) *Error {
	return &Error{ErrorInput: input}
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil diagnostic>"
	}
	var b strings.Builder
	if e.Code != "" {
		b.WriteString(e.Code)
		b.WriteString(": ")
	}
	b.WriteString(e.Summary)
	if len(e.Missing) > 0 {
		b.WriteString("; missing: ")
		b.WriteString(strings.Join(e.Missing, " | "))
	}
	if len(e.Repair) > 0 {
		b.WriteString("; repair: ")
		b.WriteString(strings.Join(e.Repair, " | "))
	}
	if e.Next != "" {
		b.WriteString("; next: ")
		b.WriteString(e.Next)
	}
	if e.Verify != "" {
		b.WriteString("; verify: ")
		b.WriteString(e.Verify)
	}
	if e.Ref != "" {
		b.WriteString("; ref: ")
		b.WriteString(e.Ref)
	}
	return b.String()
}
