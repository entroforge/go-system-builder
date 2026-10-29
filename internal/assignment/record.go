package assignment

import "github.com/entroforge/go-system-builder/internal/assignmentrecord"

// The canonical S6 Record implementation lives in assignmentrecord so lower
// level consumers (plancheckpoint, Runtime and context projections) can share
// the same contract without importing this package's lifecycle operations.
type ContextSource = assignmentrecord.ContextSource
type Record = assignmentrecord.Record
type RecordReference = assignmentrecord.RecordReference
type ScopeBasis = assignmentrecord.ScopeBasis

const (
	AssignmentRecordContract = assignmentrecord.AssignmentRecordContract
	AssignmentRecordSchema   = assignmentrecord.AssignmentRecordSchema
)

func ManifestAssignmentRecordEnabled(manifestBytes []byte) (bool, error) {
	return assignmentrecord.ManifestAssignmentRecordEnabled(manifestBytes)
}

func BuildRecord(
	manifestBytes []byte,
	manifestRef string,
	runtimeState map[string]any,
	taskBytes []byte,
	taskID string,
	assignmentID string,
	taskWritePaths []string,
	contextSources []ContextSource,
) (Record, error) {
	return assignmentrecord.BuildRecord(manifestBytes, manifestRef, runtimeState, taskBytes, taskID, assignmentID, taskWritePaths, contextSources)
}

func EffectiveScope(basis ScopeBasis) ([]string, error) {
	return assignmentrecord.EffectiveScope(basis)
}

func ValidateRecord(record Record) error { return assignmentrecord.ValidateRecord(record) }

func FindRecord(records []Record, runtimeID string, generation int, assignmentID string) (Record, bool, error) {
	return assignmentrecord.FindRecord(records, runtimeID, generation, assignmentID)
}

func FindRecordRevision(records []Record, ref RecordReference) (Record, bool, error) {
	return assignmentrecord.FindRecordRevision(records, ref)
}
