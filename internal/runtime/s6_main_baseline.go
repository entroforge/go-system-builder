package runtime

import (
	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/productbaseline"
)

// Read-only proof APIs retain their original Runtime surface; they share the
// exact dependency-neutral journal validators with downstream consumers.
func ReadJournalForState(root string, state map[string]any) ([]byte, error) {
	return controlproof.ReadJournalForState(root, state)
}
func HasCommittedTransitionForGeneration(state map[string]any, journal []byte, transition string, generation int) (bool, error) {
	return controlproof.HasCommittedTransitionForGeneration(state, journal, transition, generation)
}
func ValidateS6MainStartReceipts(state map[string]any, journal []byte) error {
	return controlproof.ValidateS6MainStartReceipts(state, journal)
}
func RequireCurrentS6MainStart(state map[string]any, journal []byte) (productbaseline.S6MainStart, error) {
	return controlproof.RequireCurrentS6MainStart(state, journal)
}
