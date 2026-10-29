package runtime

import (
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/controlproof"
)

// AssignmentRevisionReceipt retains the original public receipt API.
type AssignmentRevisionReceipt = controlproof.AssignmentRevisionReceipt

func AssignmentRevisionEventID(ref assignmentrecord.RecordReference, intentKey string) string {
	return controlproof.AssignmentRevisionEventID(ref, intentKey)
}
func AssignmentRevisionIdempotencyKey(ref assignmentrecord.RecordReference, intentKey string) string {
	return controlproof.AssignmentRevisionIdempotencyKey(ref, intentKey)
}
func ValidateAssignmentRevisionReceipts(state map[string]any, journalBytes []byte) error {
	return controlproof.ValidateAssignmentRevisionReceipts(state, journalBytes)
}
