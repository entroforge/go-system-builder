// Snapshot pre-commit validation. Per BUG-001 §4b.2(g) every transition must
// marshal its post-mutation state to JSON bytes and call
// semantic.ValidateRuntimeBytes before the runtime store performs its atomic
// write. If validation fails the post-mutation state is rejected; no atomic
// write happens; no journal append happens. This guarantees
// `post_mutation_invalid_runtime == never_committed`.
package transition

import (
	"encoding/json"

	"github.com/entroforge/go-system-builder/internal/semantic"
)

// ValidateRuntimeBytes is the canonical pre-write validator. The runtime
// store imports this wrapper so it does not depend on the transition
// package's internal helpers — and so the call site is single-purpose and
// obvious in code review.
func ValidateRuntimeBytes(root string, data []byte) error {
	return semantic.ValidateRuntimeBytes(root, data)
}

// MarshalAndValidateRuntime serializes the supplied post-mutation state and
// runs it through ValidateRuntimeBytes. Used by the engine after all actions
// have run and before the store.Update atomic write.
func MarshalAndValidateRuntime(root string, state map[string]any) error {
	bytes, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return ValidateRuntimeBytes(root, bytes)
}
