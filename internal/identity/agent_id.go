// Package identity owns the small, shared identity boundary used by runtime
// workgroup registration and agent lifecycle observers. Agent IDs are issued
// by the platform, so this package deliberately validates only identity
// syntax and authoring-placeholder rejection; it does not impose an
// agent-/builder- prefix.
package identity

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidateAgentID rejects values that cannot be treated as an exact platform
// identity. The value is not normalized: leading/trailing whitespace would
// create a different identity and therefore must be corrected by the caller.
func ValidateAgentID(value string) error {
	if value == "" {
		return fmt.Errorf("agent_id is required")
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("agent_id %q must not have leading or trailing whitespace", value)
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("agent_id %q must not contain whitespace or control characters", value)
		}
	}
	for _, marker := range []string{"TODO(planner):", "<agent-id>", "${AGENT_ID}"} {
		if strings.Contains(value, marker) {
			return fmt.Errorf("agent_id %q is an authoring placeholder (%s); replace it with the real platform Agent ID before registration", value, marker)
		}
	}
	return nil
}
