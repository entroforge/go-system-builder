package req039fixtures

import (
	"testing"
)

// WriteDocumentVerificationPassEvidence seeds S5 dual DV evidence without moving cursor.
func WriteDocumentVerificationPassEvidence(t *testing.T, root string, state map[string]any, specAgent, taskAgent string) {
	t.Helper()
	SeedDocumentPassS5(t, root, state, specAgent, taskAgent)
	SetLifecyclePhase(state, "document_verification", "")
}

// WriteBuilderBatchReadyEvidence seeds S6 builder batch evidence without moving cursor.
func WriteBuilderBatchReadyEvidence(t *testing.T, root string, state map[string]any) {
	t.Helper()
	SeedBuilderBatchReady(t, root, state)
	SetLifecyclePhase(state, "building", "")
}

// WriteAcceptancePassEvidence seeds S10 acceptance evidence without moving cursor.
func WriteAcceptancePassEvidence(t *testing.T, root string, state map[string]any) {
	t.Helper()
	SeedAcceptanceReady(t, root, state)
	SetLifecyclePhase(state, "acceptance", "")
}

// WriteReleaseAuditPassEvidence seeds release audit evidence without moving cursor.
func WriteReleaseAuditPassEvidence(t *testing.T, root string, state map[string]any) {
	t.Helper()
	SeedReleaseAuditReady(t, root, state)
	SetLifecyclePhase(state, "release_audit", "")
}
