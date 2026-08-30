package policy

// Retry values are intentionally coarse machine instructions. The detailed
// recovery operation belongs in Decision.Recovery; keeping retry vocabulary
// small prevents each Hook rule from inventing a value that downstream
// consumers cannot interpret.
const (
	RetryNever                   = "never"
	RetryNotApplicable           = "not_applicable"
	RetryAfterRecoveryValidation = "rerun after recovery validation"
	RetryWithinActivatedScope    = "rerun within activated scope"
	RetryAfterPolicyRefresh      = "rerun after policy refresh"
)

// IsRetryValue reports whether value is part of the hook-decision contract.
func IsRetryValue(value string) bool {
	switch value {
	case RetryNever,
		RetryNotApplicable,
		RetryAfterRecoveryValidation,
		RetryWithinActivatedScope,
		RetryAfterPolicyRefresh:
		return true
	default:
		return false
	}
}

// canonicalRetry converts a rule-specific or legacy retry phrase into the
// small wire vocabulary. A human-only block never retries; recoverable
// decisions retry only after their Recovery instructions have been followed.
func canonicalRetry(decision, value string) string {
	if decision == "block" {
		return RetryNever
	}
	if value == "" {
		if decision == "deny" || decision == "warn" {
			return RetryAfterRecoveryValidation
		}
		return RetryNotApplicable
	}
	if IsRetryValue(value) {
		return value
	}
	if decision == "deny" || decision == "warn" {
		return RetryAfterRecoveryValidation
	}
	return RetryNotApplicable
}
