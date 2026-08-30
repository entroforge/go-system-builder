// posttooluse.go — the PostToolUse(SendMessage) observer (L3-S7 §8, L4 §7.4).
//
// This event NEVER blocks and never advances lifecycle state on its own:
// the authoritative transitions stay in `runtime agent-event` /
// `runtime review-result submit`. What the observer does is record the
// dispatch envelope the Worker sent (PLAN_REPORT), so the first-write
// barrier (policy rule assignment_write_before_plan) has a fact to read
// and SessionStart recovery can see the plan checkpoint.
//
// Payload identification (platform reality: subagent payloads may not carry
// agent_id): payload agent_id → tool_input.teammate_name matched against
// entities.agents[].id. If identification fails, the observation is silently
// skipped (exit 0) with an actionable reason — never fabricate an agent
// binding, and never guess one from lifecycle state (S7-12, RC-12: the old
// "sole reading agent" fallback could attribute a plan_report to the wrong
// agent when several Workers were dispatched in parallel).
package hook

import (
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/identity"
	"github.com/entroforge/go-system-builder/internal/policy"
)

// PostToolUseObservation is the outcome the CLI renders.
type PostToolUseObservation struct {
	Recorded  bool   `json:"recorded"`
	AgentID   string `json:"agent_id,omitempty"`
	Message   string `json:"message_type,omitempty"`
	Reason    string `json:"reason,omitempty"`
	SystemMsg string `json:"-"`
}

// HandlePostToolUse observes a PostToolUse(SendMessage) payload. It is
// fail-open by contract: any identification or consistency gap returns an
// observation with Recorded=false and no error.
func HandlePostToolUse(input policy.Input, agents []AgentRow) PostToolUseObservation {
	if input.ToolName != "SendMessage" {
		return PostToolUseObservation{Reason: "not a SendMessage payload"}
	}
	messageType, _ := input.ToolInput["message_type"].(string)
	if messageType == "" {
		messageType, _ = input.ToolInput["type"].(string)
	}
	switch messageType {
	case "plan_report", "blocker_report", "completion_report":
	default:
		return PostToolUseObservation{Reason: "unrelated message type"}
	}
	if messageType == "plan_report" {
		planRef, _ := input.ToolInput["plan_ref"].(string)
		if strings.TrimSpace(planRef) == "" {
			planRef, _ = input.ToolInput["plan_path"].(string)
		}
		if strings.TrimSpace(planRef) == "" {
			return PostToolUseObservation{Message: messageType, Reason: "plan_report has no plan_ref/plan_path; authoritative checkpoint was not captured"}
		}
	}
	agentID := identifySender(input, agents)
	if agentID == "" {
		// S7-12 (RC-12): the observation stays fail-open (exit 0, no block),
		// but the reason now names the actionable fix instead of silently
		// dropping the envelope — the payload must carry an explicit agent_id
		// (or a teammate_name that matches a registered agent).
		return PostToolUseObservation{Message: messageType, Reason: "S7-12: missing agent_id — re-send the SendMessage payload with agent_id set to the dispatched agent id (or a teammate_name that matches a registered agent); nothing was recorded"}
	}
	return PostToolUseObservation{
		Recorded:  true,
		AgentID:   agentID,
		Message:   messageType,
		SystemMsg: fmt.Sprintf("%s observed for %s (authoritative registration stays in runtime agent-event)", messageType, agentID),
	}
}

// AgentRow is the minimal agent fact the observer reads. DispatchMode is
// populated by the CLI transport so the plan_checkpoint auto-chain gate
// can run before any side-effecting CAS call. Keep the field optional so
// the existing HandlePostToolUse ladder keeps its current contract.
type AgentRow struct {
	ID           string
	State        string
	DispatchMode string
}

// identifySender applies the identification ladder: payload agent_id →
// official top-level teammate_name (2.1.218 Agent Teams payloads) →
// tool_input.teammate_name. There is deliberately no lifecycle-state
// fallback (S7-12): guessing a sender from "whoever is in reading" can
// mis-attribute a plan_report when several Workers are dispatched in
// parallel, so an unidentifiable sender must surface an explicit, actionable
// gap instead.
func identifySender(input policy.Input, agents []AgentRow) string {
	if input.AgentID != "" {
		if identity.ValidateAgentID(input.AgentID) == nil {
			for _, a := range agents {
				if identity.ValidateAgentID(a.ID) != nil {
					continue
				}
				if a.ID == input.AgentID {
					return a.ID
				}
			}
		}
	}
	if input.TeammateName != "" {
		if identity.ValidateAgentID(input.TeammateName) == nil {
			for _, a := range agents {
				if identity.ValidateAgentID(a.ID) != nil {
					continue
				}
				if a.ID == input.TeammateName {
					return a.ID
				}
			}
		}
	}
	if teammate, _ := input.ToolInput["teammate_name"].(string); teammate != "" {
		if identity.ValidateAgentID(teammate) == nil {
			for _, a := range agents {
				if identity.ValidateAgentID(a.ID) != nil {
					continue
				}
				if a.ID == teammate {
					return a.ID
				}
			}
		}
	}
	// S7-12 (RC-12): no lifecycle-state guessing. When no explicit identity
	// was carried on the payload, return an actionable reason naming the fix
	// instead of attributing the message to whichever agent happens to be
	// waiting on its plan checkpoint.
	return ""
}

// RenderPostToolUseEnvelope renders the observer output as the hook stdout
// envelope. The decision is always allow-shaped (systemMessage only).
func RenderPostToolUseEnvelope(obs PostToolUseObservation) string {
	msg := obs.SystemMsg
	if msg == "" {
		msg = "PostToolUse observed (no dispatch message captured: " + obs.Reason + ")"
	}
	return fmt.Sprintf(`{"systemMessage": %q}`, strings.ReplaceAll(msg, `"`, `'`))
}
