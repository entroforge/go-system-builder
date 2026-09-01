package investigation

import "github.com/entroforge/go-system-builder/internal/runtime"

// runtimeCommitRevision is only used to construct internal event and
// idempotency identifiers. The normal caller does not provide it; the Writer
// owns the actual Runtime commit sequence.
func runtimeCommitRevision(requested int, state map[string]any) int {
	if requested >= 0 {
		return requested
	}
	return integerValueOrZero(state["revision"])
}

func updateRuntime(store *runtime.Store, expected int, mutation runtime.Mutation) (runtime.Snapshot, error) {
	if expected < 0 {
		return store.UpdateCurrent(mutation)
	}
	return store.Update(expected, mutation)
}
