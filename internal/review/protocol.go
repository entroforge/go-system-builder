package review

import "fmt"

// ValidateProtocolBinding rejects unsupported versions before projecting a
// review artifact. A version bump alone does not supply v2 E2E/repair semantics
// or isolate every historical writer; that protocol is not enabled yet.
func ValidateProtocolBinding(state map[string]any, artifactVersion string) error {
	if artifactVersion != "1.0.0" {
		return fmt.Errorf("unsupported review artifact schema_version %q", artifactVersion)
	}
	switch state["schema_version"] {
	case nil, "1.1.0":
	default:
		return fmt.Errorf("unsupported Runtime schema_version %v", state["schema_version"])
	}
	return nil
}

// ValidatePlanProtocolBinding separates new writable plans from historical
// readable plans and from Result/Finding's unchanged 1.0 protocol.
func ValidatePlanProtocolBinding(state map[string]any, plan *Plan) error {
	if err := ValidateProtocolBinding(state, "1.0.0"); err != nil {
		return err
	}
	return requireScopedPlan(plan)
}
