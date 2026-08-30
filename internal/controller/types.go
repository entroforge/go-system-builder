// Package controller implements the single PreToolUse control cycle required
// by REQ-039 v2.0.0 §9.1 / BUG-039-02 §4.1. The cycle reads a Runtime
// snapshot, evaluates the active Quality Gate, optionally commits exactly one
// auto-triggered transition through the existing transition.Apply seam,
// refreshes the durable milestone, and finally runs the minimal safety
// policy on the new cursor before returning a ControlResult.
//
// The package is intentionally thin: it composes the public API of
// internal/qualitygate, internal/transition, internal/runtime, internal/policy
// and internal/hookctx without duplicating lifecycle or evidence state
// machines of its own.
package controller

import (
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// ControlStatus is the Controller-only layer on top of the Quality Gate
// evaluator statuses. The Evaluator only emits satisfied|not_ready|unknown;
// the Controller projects advanced on successful CAS and blocked when the
// final safety policy denies the tool.
type ControlStatus string

const (
	StatusAdvanced  ControlStatus = "advanced"
	StatusSatisfied ControlStatus = "satisfied"
	StatusNotReady  ControlStatus = "not_ready"
	StatusBlocked   ControlStatus = "blocked"
	StatusUnknown   ControlStatus = "unknown"
)

// ControlRequest is the immutable input the Hook entrypoint passes into the
// cycle. Every field is read-only; the cycle never mutates the request.
type ControlRequest struct {
	Root string // project root (where .claude/loop-state.json lives)
	// StatePath and JournalPath optionally redirect Runtime reads/writes to a
	// caller-owned staging pair. Empty values retain the production paths under
	// Root/.claude exactly.
	StatePath   string
	JournalPath string
	Event       string                // PreToolUse, PostToolUse, SessionStart, ...
	ToolName    string                // Write, Edit, Bash, ...
	ToolInput   map[string]any        // parsed payload (file_path, command, ...)
	TargetID    string                // optional target identifier from the Hook payload
	AgentID     string                // optional agent id for hookctx resolution
	SessionID   string                // optional session id
	Runtime     policy.RuntimeContext // optional preloaded runtime facts, including Agent identity
	HookPayload map[string]any        // raw hook payload, preserved for diagnostics
	Files       qualityGateFiles
	// AffectedPaths is the canonical list of paths the current tool call will
	// mutate. The Hook adapter is responsible for computing this; when the
	// caller leaves it empty the cycle falls back to a tool-name based probe.
	AffectedPaths []string
	// QualityCycleBudget overrides the loop-definition quality_cycle_timeout
	// for this call. Zero loads the configured/default budget.
	QualityCycleBudget time.Duration
	// GateEvaluator overrides the default registry-backed evaluator (tests).
	GateEvaluator qualitygate.Evaluator
}

// ControlResult is the single projection emitted by RunControlCycle. It
// separates the safety decision (allow / block) from the Quality Gate
// progress (advanced / satisfied / not_ready / blocked / unknown) and the
// committed Runtime snapshot.
type ControlResult struct {
	Decision    policy.Decision   // minimal safety result (allow | block)
	QualityGate QualityGateResult // gate progress for the current cursor
	Snapshot    runtime.Snapshot  // committed Runtime after the cycle
	Guidance    *policy.Guidance  // recovery / next packet
	Error       string            // non-empty when the cycle bailed out
	ErrorCode   string            // stable caller-visible code (LOOP_CAS_STALE, ...)
	Warnings    []string          // non-fatal cycle warnings
}

// QualityGateResult is the Controller projection of the Evaluator's
// Evaluation. The struct is intentionally flat so the Hook payload can
// marshal it directly (BE-039 §3.2 / SYNC-039 §3.2 / §4).
type QualityGateResult struct {
	Status              ControlStatus `json:"status"`
	GateID              string        `json:"gate_id"`
	CandidateTransition string        `json:"candidate_transition"`
	ObservedRevision    int           `json:"observed_revision"`
	Fingerprint         string        `json:"fingerprint"`
	Missing             []string      `json:"missing"`
	EvidenceRefs        []string      `json:"evidence_refs"`
	Conflicts           []string      `json:"conflicts,omitempty"`
	ErrorCode           string        `json:"error_code,omitempty"`
	TransitionCommitted bool          `json:"transition_committed"`
	NextCursor          string        `json:"next_cursor"`
}

// qualityGateFiles is the read-only file surface the Evaluator requires. It
// is satisfied by the local disk by default (production) and by an in-memory
// map in tests.
type qualityGateFiles interface {
	ReadFile(path string) ([]byte, error)
}
