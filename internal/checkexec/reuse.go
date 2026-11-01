package checkexec

import (
	"context"
	"path/filepath"
	"reflect"
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

// DecideReuse selects an already accepted check for the same responsibility.
// It does not authenticate unrecorded input changes or invent a new run.
func DecideReuse(root string, current ReuseRequest, accepted []ToolReceipt) ReuseDecision {
	run := ReuseDecision{Action: "run", Reason: "no_accepted_receipt"}
	p := current.Profile
	if current.Fresh || current.NewHypothesis || p.ReusePolicy != "same_input_same_responsibility" || p.EnvironmentPolicy != "declared_only" || p.SideEffects != "none" || p.InputClosure != "whole_tree" || !ClosedStaticCheck(p.Command) {
		run.Reason = "reuse_not_applicable"
		return run
	}
	for i := len(accepted) - 1; i >= 0; i-- {
		ref := accepted[i]
		if ref.Binding != current.Binding || ref.Command != p.Command || filepath.Clean(ref.Checkout) != filepath.Clean(current.Checkout) {
			continue
		}
		verified, err := VerifyToolReceipt(root, ref)
		if err != nil || !verified.Passed || verified.Observation.ProfileID != p.ID || verified.Observation.ProfileVersion != p.Version {
			continue
		}
		if current.Observation.Environment != nil && verified.Observation.Environment != nil && !reflect.DeepEqual(current.Observation.Environment, verified.Observation.Environment) {
			continue
		}
		decision := ReuseDecision{Action: "run", Reason: "shadow", WouldReuse: true, ReceiptRef: ref.Path}
		if p.ReuseMode == "enabled" && p.Location == "worker" && verified.Observation.ReuseEligible {
			decision.Action, decision.Reason = "reuse", "accepted_original_result"
		}
		return decision
	}
	return run
}
