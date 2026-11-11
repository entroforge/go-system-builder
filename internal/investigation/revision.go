package investigation

import "github.com/entroforge/go-system-builder/internal/runtime"

// updateRuntime commits a mutation. A negative expectation selects the
// current-value entry point: the Writer owns the actual Runtime commit
// sequence, so no caller has to name it.
func updateRuntime(store *runtime.Store, expected int, mutation runtime.Mutation) (runtime.Snapshot, error) {
	if expected < 0 {
		return store.UpdateCurrent(mutation)
	}
	return store.Update(expected, mutation)
}
