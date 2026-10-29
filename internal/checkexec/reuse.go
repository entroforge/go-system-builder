package checkexec

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// ReuseRequest describes current facts, not a request to waive domain checks.
type ReuseRequest struct {
	Profile       Profile
	Observation   Observation
	Binding       Binding
	Checkout      string
	Fresh         bool
	NewHypothesis bool
}

type ReuseDecision struct {
	Action     string `json:"action"`
	Reason     string `json:"reason"`
	WouldReuse bool   `json:"would_reuse"`
	ReceiptRef string `json:"receipt_ref,omitempty"`
}

type reuseGuardKey struct{}
type ReuseGuard struct {
	Fresh         bool
	NewHypothesis bool
}

func WithReuseGuard(ctx context.Context, g ReuseGuard) context.Context {
	return context.WithValue(ctx, reuseGuardKey{}, g)
}
func CurrentReuseGuard(ctx context.Context) ReuseGuard {
	g, _ := ctx.Value(reuseGuardKey{}).(ReuseGuard)
	return g
}

// DecideReuse requires actual current observations and original domain refs.
// Explicit enabled profiles may return the original reference; shadow runs.
func DecideReuse(root string, current ReuseRequest, accepted []ToolReceipt) ReuseDecision {
	run := func(reason string) ReuseDecision { return ReuseDecision{Action: "run", Reason: reason} }
	if current.Fresh {
		return run("fresh_required")
	}
	if current.NewHypothesis {
		return run("new_distinguishing_hypothesis")
	}
	if current.Binding.Responsibility != "owned_check" {
		return run("independent_or_mandatory_domain_check")
	}
	p := current.Profile
	if p.ReusePolicy != "same_input_same_responsibility" {
		return run("profile_reuse_never")
	}
	if p.EnvironmentPolicy != "declared_only" || p.SideEffects != "none" || p.InputClosure != "whole_tree" {
		return run("unknown_input_or_environment_closure")
	}
	if current.Observation.Permissions == nil || !current.Observation.Permissions.Known() || current.Observation.PermissionDigest != current.Observation.Permissions.Digest() {
		return run("unknown_permission_conditions")
	}
	if !ClosedStaticCheck(p.Command) {
		return run("unclosed_command_inputs")
	}
	if current.Binding.RuntimeID == "" || current.Binding.Owner == "" || current.Binding.AssignmentID == "" || current.Binding.Generation < 1 || current.Binding.ExecutionGeneration < 1 {
		return run("incomplete_original_binding")
	}
	if current.Observation.ProfileDigest != p.Digest() || current.Observation.ProfileID != p.ID || current.Observation.ProfileVersion != p.Version {
		return run("current_profile_mismatch")
	}
	data, _ := json.Marshal(current.Observation)
	if err := schema.NewEmbeddedValidator().ValidateBytes("check-observation.schema.json", data); err != nil {
		return run("incomplete_current_observation")
	}
	checkout, err := filepath.EvalSymlinks(current.Checkout)
	if err != nil {
		return run("unknown_checkout")
	}
	reason := "no_accepted_receipt"
	for i := len(accepted) - 1; i >= 0; i-- {
		ref := accepted[i]
		if ref.Binding != current.Binding {
			reason = "responsibility_or_generation_changed"
			continue
		}
		if ref.Checkout != checkout {
			reason = "different_checkout"
			continue
		}
		if ref.Command != p.Command || ref.ProfileDigest != p.Digest() {
			reason = "profile_changed"
			continue
		}
		verified, err := VerifyToolReceipt(root, ref)
		if err != nil {
			reason = "untrusted_or_damaged_receipt"
			continue
		}
		if !verified.Passed {
			reason = "previous_check_failed"
			continue
		}
		previous := verified.Observation
		now := current.Observation
		if previous.Permissions == nil || !previous.Permissions.Known() || previous.PermissionDigest != previous.Permissions.Digest() {
			reason = "previous_permission_conditions_unknown"
			continue
		}
		if previous.PermissionDigest != now.PermissionDigest {
			reason = "execution_permissions_changed"
			continue
		}
		// Human-readable collection labels and the permanently false enable bit
		// are not key material; every actual digest and environment fact is.
		previous.VerificationScope, now.VerificationScope = "", ""
		previous.ReuseEligible, now.ReuseEligible = false, false
		if !reflect.DeepEqual(previous, now) {
			reason = "checked_inputs_environment_or_runner_changed"
			continue
		}
		if p.ReuseMode == "enabled" {
			return ReuseDecision{Action: "reuse", Reason: "accepted_same_input_same_responsibility", WouldReuse: true, ReceiptRef: ref.Path}
		}
		return ReuseDecision{Action: "run", Reason: "eligible_shadow_actual_execution_required", WouldReuse: true, ReceiptRef: ref.Path}
	}
	return run(reason)
}
