package assignment

import (
	"fmt"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

// commitRevision is only used to make deterministic internal event IDs for
// compatibility records. A negative request value means the caller chose the
// normal single-writer path, so the current value is read from the runtime
// snapshot rather than supplied by the Agent.
func commitRevision(requested int, state map[string]any) (int, error) {
	if requested >= 0 {
		return requested, nil
	}
	value, ok := state["revision"].(float64)
	if ok {
		return int(value), nil
	}
	valueInt, ok := state["revision"].(int)
	if ok {
		return valueInt, nil
	}
	return 0, fmt.Errorf("runtime state has no valid internal commit sequence")
}

func updateRuntime(store *loopruntime.Store, expected int, mutation loopruntime.Mutation) (loopruntime.Snapshot, error) {
	if expected < 0 {
		return store.UpdateCurrent(mutation)
	}
	return store.Update(expected, mutation)
}
