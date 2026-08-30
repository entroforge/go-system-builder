package hook

import (
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

func boundedObserverValue(value string) string {
	const limit = 240
	value = strings.TrimSpace(value)
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
