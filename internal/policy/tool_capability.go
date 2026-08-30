package policy

import (
	"fmt"
	"strings"
)

// RuleUnknownMCPTool identifies an MCP call whose capability cannot be
// proven from the tool name and payload. Unknown MCP calls must never fall
// through the default allow branch when a Worker or a controlled review phase
// is active.
const RuleUnknownMCPTool = "unknown_mcp_tool"

// IsMCPTool reports whether toolName uses Claude Code's MCP tool namespace.
func IsMCPTool(toolName string) bool {
	return strings.HasPrefix(toolName, "mcp__") && strings.Count(toolName, "__") >= 2
}

// verifiedReadOnlyMCPTools is deliberately small. A tool belongs here only
// when its server contract guarantees that it cannot mutate a file, command,
// or remote resource. Unknown MCP tools remain guarded instead of being
// treated as read-only because the platform namespace alone carries no
// capability information.
var verifiedReadOnlyMCPTools = map[string]struct{}{
	"mcp__filesystem__list_directory": {},
	"mcp__filesystem__read_file":      {},
	"mcp__filesystem__stat":           {},
}

func isVerifiedReadOnlyMCPTool(toolName string) bool {
	_, ok := verifiedReadOnlyMCPTools[toolName]
	return ok
}

// unknownMCPToolDecision is intentionally evaluated before the existing
// path/scope rules. A path-bearing MCP call can reuse those rules, while a
// pathless call has no trustworthy target to check and must be visible rather
// than silently allowed.
func unknownMCPToolDecision(input Input) (Decision, bool) {
	if !IsMCPTool(input.ToolName) || isVerifiedReadOnlyMCPTool(input.ToolName) {
		return Decision{}, false
	}
	if toolPath(input.ToolInput) != "" {
		return Decision{}, false
	}

	decision := "warn"
	if input.Runtime.Agent != nil || input.Runtime.CurrentState == "verification" || input.Runtime.RepairStatus != "" {
		decision = "deny"
	}
	return Decision{
		Decision: decision,
		RuleID:   RuleUnknownMCPTool,
		Reason: fmt.Sprintf(
			"MCP tool %s has no verified read-only contract or identifiable mutation path; it cannot be silently allowed",
			input.ToolName,
		),
		Recovery: []string{
			"classify the MCP tool in the verified read-only allowlist or provide a path-bearing mutation contract",
			"for a Worker, use an allowed tool from the activated Assignment envelope and retry after classification",
		},
		Retry:         RetryAfterRecoveryValidation,
		HumanRequired: false,
		MatchedRuleIDs: []string{
			RuleUnknownMCPTool,
		},
	}, true
}
