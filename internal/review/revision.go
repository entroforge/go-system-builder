package review

import loopruntime "github.com/entroforge/go-system-builder/internal/runtime"

// currentCommitRevision supplies the internal sequence used to name audit
// artifacts when the normal caller omits an expected revision. It is not a
// ReviewPlan or Assignment version and is never copied into reviewer input.
func currentCommitRevision(requested int, state map[string]any) int {
	if requested >= 0 {
		return requested
	}
	return intField(state["revision"])
}

func updateRuntime(store *loopruntime.Store, expected int, mutation loopruntime.Mutation) (loopruntime.Snapshot, error) {
	if expected < 0 {
		return store.UpdateCurrent(mutation)
	}
	return store.Update(expected, mutation)
}
