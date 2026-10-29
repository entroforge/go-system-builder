package hook

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/missingtokens"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
)

// PreToolUseWithQualityGate projects the Controller's Quality Gate progress
// onto the official Claude Code Hook PreToolUse envelope (BUG-039-03 §4.1 /
// REQ-039 §10.2 / SYNC-039
// §3.2 / §4).
//
// Mapping rules (per the BUG contract):
//
//   - quality_gate.status ∈ {not_ready, satisfied, unknown} surfaces pending
//     work in the Recovery Packet so the agent can resume.
//   - quality_gate.status == "advanced" reports the committed transition,
//     incremented revision and next cursor.
//   - PreToolUse reports graph progress and recovery context; it does not
//     authorize or deny individual tool calls.
//
// User-visible notices and model context are separate. Ordinary tool calls
// carry a compact checkpoint; a committed stage change restores full guidance.
// hookSpecificOutput.additionalContext carries the Agent-facing Recovery Packet, derived
// from decision.Missing / decision.Recovery and the Quality Gate missing[]
// list. When the Decision carries a Guidance the legacy LOOP RECOVERY
// packet is appended after the quality_gate summary so existing recovery
// consumers (e.g. the Controller integration tests that grep for
// "LOOP RECOVERY") continue to find the canonical recovery text. The
// Decision never fabricates status="advanced" — that projection only fires
// when controller.ControlResult.QualityGate.TransitionCommitted is true.
func PreToolUseWithQualityGate(decision policy.Decision, result controller.ControlResult) ([]byte, int, error) {
	qg := normalizeQualityGate(result.QualityGate)

	projection, _ := json.Marshal(qg)
	var body strings.Builder
	body.WriteString("QUALITY_GATE ")
	body.Write(projection)
	body.WriteByte('\n')
	// AdditionalContext may contain the current responsibility's exact answer
	// path. Put it before the potentially long Quality Gate recovery details so
	// the existing bounded wire packet cannot cut that path off at its tail.
	if decision.AdditionalContext != "" {
		body.WriteString(decision.AdditionalContext)
		body.WriteString("\n\n")
	}
	body.WriteString(boundedContext(formatPreToolUseRecoveryPacket(decision, qg)))
	if decision.Guidance != nil {
		body.WriteString("\n\n")
		if qg.TransitionCommitted {
			body.WriteString(formatGuidance(*decision.Guidance))
		} else {
			body.WriteString(compactGuidance(*decision.Guidance))
		}
	}
	specific := map[string]any{"hookEventName": "PreToolUse", "additionalContext": body.String()}
	payload := map[string]any{"hookSpecificOutput": specific, "systemMessage": qualityNotice(decision, qg)}
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
// well-formed arrays (never nil) and preserves the Controller's status.
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
// text for the PreToolUse additionalContext. The Quality Gate's missing[] list
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
		// L3-S6 §9.3: the token legend turns bare missing tokens into
		// executable next actions instead of leaving the agent to guess.
		if qg.Status == controller.StatusNotReady {
			if legend := missingtokens.RenderMissingTokenLegend(qg.GateID, qg.Missing); legend != "" {
				b.WriteString("\n\n")
				b.WriteString(legend)
			}
		}
		b.WriteString(".")
	}

	// Conflicts carry the UNKNOWN verdict's diagnosis (L3-S10 walkthrough
	// 2026-08-28: a fully-coached evidence_ref_missing conflict never reached
	// the agent because only missing[] was rendered). Without them an
	// unknown gate is un-actionable.
	if len(qg.Conflicts) > 0 {
		var conflicts []string
		var transitionDiagnostics []string
		for _, conflict := range qg.Conflicts {
			if qg.ErrorCode == controller.CodeTransitionGua && strings.HasPrefix(conflict, "transition:") {
				transitionDiagnostics = append(transitionDiagnostics, conflict)
				continue
			}
			conflicts = append(conflicts, conflict)
		}
		if len(conflicts) > 0 {
			fmt.Fprintf(&b, " Conflicts: %s.", strings.Join(conflicts, "; "))
		}
		for _, conflict := range conflicts {
			if strings.HasSuffix(conflict, ":producer") {
				for _, requirement := range qualitygate.ProducerRequirements(qg.GateID) {
					fmt.Fprintf(&b, " Producer contract: %s requires %s.", requirement.Kind, strings.Join(requirement.Responsibilities, " or "))
				}
				b.WriteString(" Verify the actual producer's responsibility and audit work; register new qualified evidence and retire invalid evidence through Harness. Do not relabel registered evidence or edit Runtime directly.")
				break
			}
		}
		for _, diagnostic := range transitionDiagnostics {
			fmt.Fprintf(&b, " Transition diagnostic: %s.", diagnostic)
			if qg.CandidateTransition != "" {
				fmt.Fprintf(&b, " Read next (read-only): `loop-harness explain %s --root .`.", qg.CandidateTransition)
			}
		}
	}

	// Include recorded rule and recovery details as graph context; these fields
	// do not change the tool's permission decision.
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
	} else if decision.Decision == "warn" {
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
	} else if len(decision.Recovery) > 0 {
		b.WriteString(" Recovery: ")
		b.WriteString(strings.Join(decision.Recovery, " → "))
		b.WriteString(".")
	}

	return b.String()
}

// FormatPreToolUseRecoveryPacket exposes the stable Agent-facing recovery
// text to integration tests and adjacent adapters without exposing the
// internal formatting helpers used by the wire renderer.
func FormatPreToolUseRecoveryPacket(decision policy.Decision, qg controller.QualityGateResult) string {
	return formatPreToolUseRecoveryPacket(decision, qg)
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
