package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// auditMu guards the audit-only append to .claude/hook-decisions.jsonl so
// concurrent HA-* writes (e.g. parallel SessionStart hooks) do not interleave
// half-written lines. Closing contract: hook_decisions_jsonl_writes_are_concurrency_safe.
var auditMu sync.Mutex

// RenderWithRoot projects a policy.Decision into the protocol-specific
// PreToolUse permissionDecision payload (Claude Code Hook) or a plain
// lifecycle systemMessage. The adapter does not own lifecycle legality or
// transition state; it renders the Controller's positive guidance alongside
// policy outcomes and never promotes warn → block.
//
// The layered Controller-driven PreToolUse path lives in pretooluse.go
// (`PreToolUseWithQualityGate`); RenderWithRoot continues to serve the
// legacy hook-policy envelope and the non-PreToolUse lifecycle events
// (SessionStart, SubagentStart, SubagentStop, TeammateIdle, Stop, PreCompact).
// PreToolUse callers MUST use PreToolUseWithQualityGate so the layered
// `quality_gate` object reaches the wire (BUG-039-03 §4.1).
//
//	allow  → nil bytes, exit 0 (PreToolUse: no override)
//	info   → Controller recovery packet when present, otherwise stage banner
//	         systemMessage; PreToolUse: permissionDecision="allow" + systemMessage.
//	         Lifecycle context only, never blocks.
//	warn   → PreToolUse: permissionDecision="allow" + systemMessage (prefixed
//	         with the stage banner when rt is populated);
//	         non-PreToolUse: plain systemMessage with banner + missing/recovery/retry.
//	         The original tool action always proceeds (HO-* observation only).
//	audit  → append-only line to .claude/hook-decisions.jsonl + nil bytes (no
//	         protocol payload). Hook must not influence the tool caller.
//	deny   → PreToolUse rejection with recovery instructions; no human Gateway.
//	block  → PreToolUse rejection reserved for a human-only hard stop.
//	         TeammateIdle/SubagentStop: the transport (cli evaluate) exits 2
//	         with RenderStopBlockFeedback on stderr instead of using this
//	         payload — the official platform control that continues the same
//	         agent (L4 §15.2 P0-2; see stopidle.go).
//
// When decision.Decision is "audit" the partial audit record is appended to
// <root>/.claude/hook-decisions.jsonl; when root is empty the call falls back
// to os.Getwd() so in-process library users stay bit-identical.
//
// CLI-driven invocations (cmd/loop-harness) must pass root explicitly to avoid
// writing audit records to the host's working directory instead of the target
// repository's outbox.
//
// rt is the runtime snapshot already loaded by the caller (hookctx.Load). It
// is read-only here — the adapter never mutates runtime state. An empty
// RuntimeContext (e.g. integrity failure, missing file) produces an empty
// stage prefix/banner so existing tests that pass RuntimeContext{} remain
// byte-identical with their pre-banner output.
func RenderWithRoot(root, event string, decision policy.Decision, rt policy.RuntimeContext) ([]byte, int, error) {
	return RenderWithAdditionalContext(root, event, decision, rt, "")
}

// RenderWithAdditionalContext is the lifecycle-aware renderer. Claude Code
// accepts additionalContext on SessionStart and SubagentStart; keeping it in
// this adapter lets the CLI inject a short authoritative checkpoint without
// exposing the project's internal runtime JSON shape to the platform.
// PreToolUse and stop-control events intentionally keep their existing wire
// shapes even if a caller supplies context.
func RenderWithAdditionalContext(root, event string, decision policy.Decision, rt policy.RuntimeContext, additionalContext string) ([]byte, int, error) {
	switch decision.Decision {
	case "allow":
		if decision.Guidance == nil {
			return nil, 0, nil
		}
		body := formatGuidance(*decision.Guidance)
		if event == "PreToolUse" {
			data, err := renderPreToolUsePayload(body, "allow")
			if err != nil {
				return nil, 1, fmt.Errorf("encode Hook output: %w", err)
			}
			return data, 0, nil
		}
		data, err := renderSystemMessage(body, event, additionalContext)
		if err != nil {
			return nil, 1, fmt.Errorf("encode Hook output: %w", err)
		}
		return data, 0, nil
	case "info":
		body := stageBanner(decision, rt)
		if decision.Guidance != nil {
			body = formatGuidance(*decision.Guidance)
		}
		if event == "PreToolUse" {
			data, err := renderPreToolUsePayload(body, "allow")
			if err != nil {
				return nil, 1, fmt.Errorf("encode Hook output: %w", err)
			}
			return data, 0, nil
		}
		data, err := renderSystemMessage(body, event, additionalContext)
		if err != nil {
			return nil, 1, fmt.Errorf("encode Hook output: %w", err)
		}
		return data, 0, nil
	case "audit":
		if err := appendAuditLine(root, decision); err != nil {
			return nil, 1, fmt.Errorf("audit append: %w", err)
		}
		return nil, 0, nil
	case "warn":
		body := withStagePrefix(message(decision), rt)
		body = appendGuidance(body, decision.Guidance)
		if event == "PreToolUse" {
			data, err := renderPreToolUsePayload(body, "allow")
			if err != nil {
				return nil, 1, fmt.Errorf("encode Hook output: %w", err)
			}
			return data, 0, nil
		}
		data, err := renderSystemMessage(body, event, additionalContext)
		if err != nil {
			return nil, 1, fmt.Errorf("encode Hook output: %w", err)
		}
		return data, 0, nil
	case "deny", "block":
		body := withStagePrefix(message(decision), rt)
		body = appendGuidance(body, decision.Guidance)
		if event == "PreToolUse" {
			data, err := renderPreToolUsePayload(body, "deny")
			if err != nil {
				return nil, 1, fmt.Errorf("encode Hook output: %w", err)
			}
			return data, 0, nil
		}
		data, err := renderSystemMessage(body, event, additionalContext)
		if err != nil {
			return nil, 1, fmt.Errorf("encode Hook output: %w", err)
		}
		return data, 0, nil
	default:
		return nil, 0, nil
	}
}

func appendGuidance(body string, guidance *policy.Guidance) string {
	if guidance == nil {
		return body
	}
	return body + "\n\n" + formatGuidance(*guidance)
}

func formatGuidance(guidance policy.Guidance) string {
	var b strings.Builder
	b.WriteString("LOOP RECOVERY — Stage ")
	b.WriteString(guidance.Stage)
	b.WriteString(" ")
	b.WriteString(guidance.LifecycleState)
	if guidance.LifecyclePhase != "" {
		b.WriteString(".")
		b.WriteString(guidance.LifecyclePhase)
	}
	fmt.Fprintf(&b, " @ rev=%d. ", guidance.Revision)
	b.WriteString("Objective: ")
	b.WriteString(guidance.Objective)
	b.WriteString(". ")
	b.WriteString("Next: ")
	b.WriteString(guidance.Action)
	b.WriteString(". Read ")
	b.WriteString(guidance.ProtocolRef)
	b.WriteString("; if blocked, read ")
	b.WriteString(guidance.ManualRef)
	b.WriteString(".")
	if len(guidance.ReadOrder) > 0 {
		b.WriteString(" Read in order: ")
		b.WriteString(strings.Join(guidance.ReadOrder, " -> "))
		b.WriteString(".")
	}
	if len(guidance.Questions) > 0 {
		b.WriteString(" Preflight questions: ")
		b.WriteString(strings.Join(guidance.Questions, " | "))
		b.WriteString(".")
	}
	if len(guidance.Automation) > 0 {
		b.WriteString(" Automation: ")
		b.WriteString(strings.Join(guidance.Automation, " | "))
		b.WriteString(".")
	}
	if len(guidance.Integration) > 0 {
		b.WriteString(" Integration: ")
		b.WriteString(strings.Join(guidance.Integration, " | "))
		b.WriteString(".")
	}
	if len(guidance.Missing) > 0 {
		b.WriteString(" Missing: ")
		b.WriteString(strings.Join(guidance.Missing, "; "))
		b.WriteString(".")
	}
	if guidance.Blocked {
		b.WriteString(" Blocked: ")
		b.WriteString(guidance.Blocker)
		b.WriteString(".")
	}
	if guidance.HumanRequired {
		b.WriteString(" Human Gateway required; stop automation.")
	}
	return b.String()
}

// renderPreToolUsePayload renders the PreToolUse-specific JSON envelope.
// Warnings allow; recoverable denies and human blocks deny.
func renderPreToolUsePayload(body, permission string) ([]byte, error) {
	payload := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       permission,
			"permissionDecisionReason": body,
		},
		"systemMessage": body,
	}
	return json.Marshal(payload)
}

// renderSystemMessage renders the non-PreToolUse plain envelope for warn and
// block decisions (the systemMessage-only shape used by SessionStart,
// SubagentStop, TeammateIdle, etc.). Block decisions on
// TeammateIdle/SubagentStop never reach this renderer from the CLI
// transport — they exit 2 with stderr feedback (stopidle.go); this shape
// remains for warn and for in-process/library callers.
func renderSystemMessage(body, event, additionalContext string) ([]byte, error) {
	payload := map[string]any{"systemMessage": body}
	if (event == "SessionStart" || event == "SubagentStart") && strings.TrimSpace(additionalContext) != "" {
		payload["additionalContext"] = additionalContext
	}
	return json.Marshal(payload)
}

// auditRecord is the on-disk representation of a policy.Decision in
// .claude/hook-decisions.jsonl. Defined as a struct (not a map) so the Go
// compiler catches missing fields when Decision evolves.
type auditRecord struct {
	EvaluatedAt    string   `json:"evaluated_at"`
	ElapsedMS      int64    `json:"elapsed_ms,omitempty"`
	Decision       string   `json:"decision"`
	RuleID         string   `json:"rule_id"`
	Reason         string   `json:"reason"`
	Missing        []string `json:"missing"`
	Recovery       []string `json:"recovery"`
	Retry          string   `json:"retry"`
	HumanRequired  bool     `json:"human_required"`
	MatchedRuleIDs []string `json:"matched_rule_ids"`
}

// appendAuditLine serializes a Decision as one line and appends it to
// .claude/hook-decisions.jsonl under repoRoot. The file (and parent dir) are
// created lazily; writes are serialized via auditMu so concurrent hooks do not
// interleave bytes. When repoRoot is empty (legacy callers), the path falls
// back to os.Getwd() so in-process library use stays unchanged.
func appendAuditLine(root string, decision policy.Decision) error {
	repoRoot := root
	if repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve cwd: %w", err)
		}
		repoRoot = cwd
	}
	auditPath := filepath.Join(repoRoot, ".claude", "hook-decisions.jsonl")
	if err := os.MkdirAll(filepath.Dir(auditPath), 0o755); err != nil {
		return fmt.Errorf("ensure audit dir: %w", err)
	}
	record := auditRecord{
		EvaluatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		ElapsedMS:      decision.ElapsedMS,
		Decision:       decision.Decision,
		RuleID:         decision.RuleID,
		Reason:         decision.Reason,
		Missing:        decision.Missing,
		Recovery:       decision.Recovery,
		Retry:          decision.Retry,
		HumanRequired:  decision.HumanRequired,
		MatchedRuleIDs: decision.MatchedRuleIDs,
	}
	serialized, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode audit record: %w", err)
	}
	serialized = append(serialized, '\n')
	auditMu.Lock()
	defer auditMu.Unlock()
	f, err := os.OpenFile(auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()
	n, err := f.Write(serialized)
	if err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	if n != len(serialized) {
		return fmt.Errorf("short write to audit log: %d of %d bytes", n, len(serialized))
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync audit log: %w", err)
	}
	return nil
}

// message builds the human-facing text for warn and block payloads. Per
// REQ-004 §4.4 the missing[] list is interpolated so the Driver does not have
// to do string surgery on the JSON.
//
// Per REQ-005 P1 discoverability, every warn/block message ends with a
// deep-link sentence to the relevant guard anchor in the manual so an agent
// that hits a gate failure can jump straight to the spec. The anchor uses
// the lowercase rule ID.
func message(decision policy.Decision) string {
	var b strings.Builder
	b.WriteString(decision.RuleID)
	b.WriteString(" — ")
	b.WriteString(decision.Reason)
	if len(decision.Missing) > 0 {
		b.WriteString(" Missing: ")
		b.WriteString(strings.Join(decision.Missing, "; "))
		b.WriteString(".")
	}
	if len(decision.Recovery) > 0 {
		b.WriteString(" Recovery: ")
		b.WriteString(strings.Join(decision.Recovery, " → "))
		b.WriteString(".")
	}
	if decision.Retry != "" {
		b.WriteString(" Retry=")
		b.WriteString(decision.Retry)
		b.WriteString(".")
	}
	if decision.HumanRequired {
		b.WriteString(" Human required.")
	}
	if decision.RuleID != "" {
		b.WriteString(" See ")
		b.WriteString(transition.ManualTargetPath())
		b.WriteString("#")
		b.WriteString(strings.ToLower(decision.RuleID))
		b.WriteString(".")
	}
	return b.String()
}

// stageLine renders the compact stage line shared by warn/block prefix and
// info banner. Returns "" when CurrentState is empty (runtime not loaded —
// integrity failure or missing file). Format:
//
//	S<cursor> <state>.<phase> @ rev=<N>
//
// e.g. "S2 planning.design @ rev=3". The cursor and dotted label come from
// runtime.StageFor, the single shared Main Spine projection.
func stageLine(rt policy.RuntimeContext) string {
	if rt.CurrentState == "" {
		return ""
	}
	cursor, label := runtime.StageFor(rt.CurrentState, rt.CurrentPhase, rt.ProjectRoot)
	return fmt.Sprintf("%s %s @ rev=%d", cursor, label, rt.Revision)
}

// withStagePrefix prepends "[<stageLine>] " to body when the runtime snapshot
// is populated, so a warn/block surfaces the agent's position alongside the
// gate failure. Returns body unchanged when the runtime is empty (preserves
// byte-identical output for tests and integrity-failure paths).
func withStagePrefix(body string, rt policy.RuntimeContext) string {
	line := stageLine(rt)
	if line == "" {
		return body
	}
	return "[" + line + "] " + body
}

// stageBanner renders the standalone info banner shown at SessionStart,
// SubagentStart, and PreCompact — the moments an agent is most at risk of
// losing its position after a compact or subagent dispatch. Shape:
//
//	Stage <stageLine> — bound REQ-<id>. <decision.Reason>
//
// When the runtime snapshot is empty (integrity failure, no bound REQ), the
// banner degrades to a recovery hint rather than silence, because info exists
// to re-seat the agent — silence defeats the purpose.
func stageBanner(decision policy.Decision, rt policy.RuntimeContext) string {
	var b strings.Builder
	b.WriteString("Stage ")
	line := stageLine(rt)
	if line == "" {
		b.WriteString("unknown — runtime not loaded.")
	} else {
		b.WriteString(line)
		if rt.BoundREQPath != "" {
			b.WriteString(" — bound ")
			b.WriteString(stageREQID(rt.BoundREQPath))
			b.WriteString(".")
		} else {
			b.WriteString(" — no bound REQ.")
		}
	}
	if decision.Reason != "" {
		b.WriteString(" ")
		b.WriteString(decision.Reason)
	}
	return b.String()
}

// stageREQID derives a REQ-<id> token from a bound-REQ path basename. Returns
// the basename with extension stripped (e.g. "REQ-001-ui-checker.md" →
// "REQ-001-ui-checker"); callers that need the canonical short ID can split
// on "-" themselves. Good enough for a human-readable banner.
func stageREQID(path string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}
