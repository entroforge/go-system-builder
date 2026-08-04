package hook

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/policy"
)

// PreToolUseWithQualityGate projects the Controller's layered result
// (Quality Gate progress + minimal safety Decision) onto the official Claude
// Code Hook PreToolUse envelope (BUG-039-03 §4.1 / REQ-039 §10.2 / SYNC-039
// §3.2 / §4).
//
// Mapping rules (per the BUG contract):
//
//   - quality_gate.status ∈ {not_ready, satisfied, unknown}
//     → permissionDecision="allow", tool proceeds; Recovery Packet still
//     surfaces missing / gate_id / fingerprint so the agent can resume.
//   - quality_gate.status == "advanced"
//     → permissionDecision="allow", transition_committed=true,
//     observed_revision=N+1, next_cursor points at the post-commit cursor.
//   - safety block (locked artifact / squash merge)
//     → permissionDecision="deny", quality_gate.status="blocked".
//   - "info" on PreToolUse still allows (lifecycle context only).
//
// systemMessage always carries the Agent-facing Recovery Packet, derived
// from decision.Missing / decision.Recovery and the Quality Gate missing[]
// list. When the Decision carries a Guidance the legacy LOOP RECOVERY
// packet is appended after the quality_gate summary so existing recovery
// consumers (e.g. the Controller integration tests that grep for
// "LOOP RECOVERY") continue to find the canonical recovery text. The
// Decision never fabricates status="advanced" — that projection only fires
// when controller.ControlResult.QualityGate.TransitionCommitted is true.
func PreToolUseWithQualityGate(decision policy.Decision, result controller.ControlResult) ([]byte, int, error) {
	qg := normalizeQualityGate(result.QualityGate)

	// Safety block: only the minimal policy may produce deny/block on
	// PreToolUse. The Quality Gate status collapses to "blocked" exactly
	// here, never as a side-effect of not_ready / unknown.
	permissionDecision := "allow"
	if decision.Decision == "block" || decision.Decision == "deny" {
		permissionDecision = "deny"
		qg.Status = controller.StatusBlocked
	}

	body := formatPreToolUseRecoveryPacket(decision, qg)
	if decision.Guidance != nil {
		// Append the canonical LOOP RECOVERY packet so the existing
		// recovery-text grep (Controller integration test, agent-protocol
		// readers) continues to find the legacy summary. The new
		// QUALITY GATE summary above remains the authoritative status
		// line; this append is purely additive.
		body = body + "\n\n" + formatGuidance(*decision.Guidance)
	}

	payload := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       permissionDecision,
			"permissionDecisionReason": body,
			"quality_gate": map[string]any{
				"status":               string(qg.Status),
				"gate_id":              qg.GateID,
				"candidate_transition": qg.CandidateTransition,
				"observed_revision":    qg.ObservedRevision,
				"fingerprint":          qg.Fingerprint,
				"missing":              nonNil(qg.Missing),
				"evidence_refs":        nonNil(qg.EvidenceRefs),
				"transition_committed": qg.TransitionCommitted,
				"next_cursor":          qg.NextCursor,
			},
		},
		"systemMessage": body,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, 1, fmt.Errorf("encode Hook output: %w", err)
	}
	return data, 0, nil
}

// PreToolUseEnvelopeBytes is a thin convenience wrapper that lets the CLI
// pass the raw bytes through without exposing the struct field layout. It
// exists so callers that already hold a controller.ControlResult can write
// the bytes once and continue down the audit / stdout split without a
// second allocation.
func PreToolUseEnvelopeBytes(decision policy.Decision, result controller.ControlResult) ([]byte, int, error) {
	return PreToolUseWithQualityGate(decision, result)
}

// normalizeQualityGate guarantees the JSON projection always carries
// well-formed arrays (never nil), and keeps the controller's verbatim
// status — never overrides not_ready / unknown with blocked or advanced.
func normalizeQualityGate(qg controller.QualityGateResult) controller.QualityGateResult {
	qg.Missing = nonNil(qg.Missing)
	qg.EvidenceRefs = nonNil(qg.EvidenceRefs)
	qg.Conflicts = nonNil(qg.Conflicts)
	if qg.NextCursor == "" {
		qg.NextCursor = ""
	}
	return qg
}

// formatPreToolUseRecoveryPacket renders the Agent-facing human-readable
// text for the PreToolUse systemMessage. The Quality Gate's missing[] list
// is always included so the agent can resume work without re-deriving it
// from JSON.
func formatPreToolUseRecoveryPacket(decision policy.Decision, qg controller.QualityGateResult) string {
	var b strings.Builder

	// Lead with the gate status so the agent knows whether to continue or
	// whether a transition was just committed.
	switch {
	case qg.Status == controller.StatusAdvanced:
		fmt.Fprintf(&b, "QUALITY GATE ADVANCED — transition %q committed at revision %d.",
			qg.CandidateTransition, qg.ObservedRevision)
	case qg.Status == controller.StatusBlocked:
		fmt.Fprintf(&b, "QUALITY GATE BLOCKED — final safety block on %s.", decision.RuleID)
	case qg.Status == controller.StatusSatisfied:
		b.WriteString("QUALITY GATE SATISFIED — current cursor is eligible for auto-transition.")
	case qg.Status == controller.StatusUnknown:
		if qg.ErrorCode != "" {
			fmt.Fprintf(&b, "QUALITY GATE UNKNOWN — %s; tool allowed, please reconcile.", qg.ErrorCode)
		} else {
			b.WriteString("QUALITY GATE UNKNOWN — tool allowed, please reconcile.")
		}
	default:
		// not_ready and any unrecognised fallback.
		fmt.Fprintf(&b, "QUALITY GATE NOT READY — gate %q pending evidence.", nonEmptyQG(qg.GateID, "current"))
	}

	if qg.GateID != "" {
		fmt.Fprintf(&b, " Gate %s.", qg.GateID)
	}
	if qg.CandidateTransition != "" {
		fmt.Fprintf(&b, " Candidate: %s.", qg.CandidateTransition)
	}
	if qg.ObservedRevision > 0 {
		fmt.Fprintf(&b, " Observed revision: %d.", qg.ObservedRevision)
	}
	if qg.Fingerprint != "" {
		fmt.Fprintf(&b, " Fingerprint: %s.", qg.Fingerprint)
	}
	if qg.NextCursor != "" {
		fmt.Fprintf(&b, " Next cursor: %s.", qg.NextCursor)
	}

	// Pull in the gate's missing[] list verbatim. The agent can resume work
	// without re-parsing the JSON.
	if len(qg.Missing) > 0 {
		b.WriteString(" Missing: ")
		b.WriteString(strings.Join(qg.Missing, "; "))
		b.WriteString(".")
	}

	// For blocked decisions the agent needs the rule id + recovery.
	if qg.Status == controller.StatusBlocked || decision.Decision == "block" || decision.Decision == "deny" {
		if decision.RuleID != "" {
			fmt.Fprintf(&b, " Rule: %s.", decision.RuleID)
		}
		if decision.Reason != "" {
			fmt.Fprintf(&b, " %s", decision.Reason)
		}
		if len(decision.Recovery) > 0 {
			b.WriteString(" Recovery: ")
			b.WriteString(strings.Join(decision.Recovery, " → "))
			b.WriteString(".")
		}
		if decision.HumanRequired {
			b.WriteString(" Human required.")
		}
	} else if len(decision.Recovery) > 0 {
		b.WriteString(" Recovery: ")
		b.WriteString(strings.Join(decision.Recovery, " → "))
		b.WriteString(".")
	}

	return b.String()
}

func nonEmptyQG(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
