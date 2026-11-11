package hook

import (
	"regexp"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
)

const (
	RulePostToolUseFailureObserved = "post_tool_use_failure_observed"
	RuleConfigChangeObserved       = "config_change_observed"
)

// NativeObserverDecision creates an audit-only decision for platform events
// whose job is to improve evidence, not to block the platform. It keeps the
// existing wrapper/probe paths intact; the native event is an additional
// durable signal with a stable rule id and bounded reason text.
func NativeObserverDecision(input policy.Input, elapsed time.Duration) policy.Decision {
	ruleID := RulePostToolUseFailureObserved
	if input.Event == "ConfigChange" {
		ruleID = RuleConfigChangeObserved
	}
	parts := []string{input.Event + " observed"}
	if input.ToolName != "" {
		parts = append(parts, "tool="+boundedObserverValue(input.ToolName))
	}
	if input.FilePath != "" {
		parts = append(parts, "path="+boundedObserverValue(input.FilePath))
	}
	if input.Source != "" {
		parts = append(parts, "source="+boundedObserverValue(input.Source))
	}
	if input.Error != "" {
		parts = append(parts, "error="+boundedObserverValue(input.Error))
	}
	return policy.Decision{
		Decision:       "audit",
		RuleID:         ruleID,
		Reason:         strings.Join(parts, "; "),
		Retry:          "not_applicable",
		MatchedRuleIDs: []string{ruleID},
		ElapsedMS:      elapsed.Milliseconds(),
	}
}

var observerSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)password\s*[:=]`),
	regexp.MustCompile(`(?i)api[_-]?key\s*[:=]`),
	regexp.MustCompile(`(?i)secret\s*[:=]`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{12,}`),
	regexp.MustCompile(`(?i)token\s*[:=]\s*[A-Za-z0-9._-]{8,}`),
}

func boundedObserverValue(value string) string {
	const limit = 240
	value = strings.TrimSpace(value)
	// Inspect the complete external field before truncation can separate a
	// credential from its marker. Reuse the existing finite capture policy;
	// withholding audit text neither rejects the tool nor rewrites authority.
	for _, pattern := range observerSecretPatterns {
		if pattern.MatchString(value) {
			return "[withheld: capture secret pattern]"
		}
	}
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}

// NativeObserverSummary keeps errors concise while identifying the event.
func NativeObserverSummary(input policy.Input) string {
	if input.Event == "" {
		return "native Hook observer"
	}
	return "native " + input.Event + " observer"
}
