package policy

import "fmt"

const RulePolicyUnavailable = "policy_unavailable"

// UnavailableDecision is shared by the transport and the final safety pass.
// With no usable policy we cannot trust shell classification or an MCP tool's
// name to prove that it is read-only. Preserve only explicit inspection tools.
func UnavailableDecision(input Input, cause error) Decision {
	decision := Decision{
		Decision: "warn",
		RuleID:   RulePolicyUnavailable,
		Reason:   fmt.Sprintf("safety policy unavailable: %v", cause),
		Recovery: []string{
			"Use Read/Grep/Glob to inspect docs/control/hook-policy.json.",
			"Restore the approved policy from a trusted installation or backup using an external terminal, then retry. Do not disable Hooks or retry blocked Bash commands from the agent.",
		},
		Retry: RetryAfterRecoveryValidation,
	}
	if input.Event != "PreToolUse" {
		return decision
	}
	switch input.ToolName {
	case "Read", "Grep", "Glob", "LS", "WebFetch", "WebSearch", "ToolSearch":
		return decision
	default:
		decision.Decision = "deny"
		decision.MatchedRuleIDs = []string{RulePolicyUnavailable}
		return decision
	}
}
