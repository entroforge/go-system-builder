package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// DefaultRecoveryReplayMaxSteps bounds a replay whose caller did not provide
// an explicit limit. Recovery callers should normally choose a smaller limit
// for a known stage and inspect the trace before applying the resulting plan.
const DefaultRecoveryReplayMaxSteps = 32

// ReplayStopReason explains why RecoveryReplay stopped without treating a
// non-advanced gate result as an error.
type ReplayStopReason string

const (
	ReplayStopNotReady   ReplayStopReason = "not_ready"
	ReplayStopUnknown    ReplayStopReason = "unknown"
	ReplayStopConflict   ReplayStopReason = "conflict"
	ReplayStopHuman      ReplayStopReason = "human_required"
	ReplayStopNoAuto     ReplayStopReason = "no_auto"
	ReplayStopMaxSteps   ReplayStopReason = "max_steps"
	ReplayStopNoProgress ReplayStopReason = "no_progress"
)

// ErrRecoveryReplayRepeatedCursor indicates that an allegedly advanced cycle
// returned a cursor/revision pair already observed by the replay.
var ErrRecoveryReplayRepeatedCursor = errors.New("recovery replay repeated cursor or revision")

// ErrRecoveryReplayNoProgress indicates that a committed transition did not
// change both the durable revision and the lifecycle cursor.
var ErrRecoveryReplayNoProgress = errors.New("recovery replay made no progress")

// ErrRecoveryReplayPathOutsideRoot indicates that a caller supplied staging
// path resolves outside the repository root. The check is performed before
// the staging pair is opened and before every replay cycle is dispatched.
var ErrRecoveryReplayPathOutsideRoot = errors.New("recovery replay path is outside recovery root")

// RecoveryReplayRequest configures a replay over a caller-owned Runtime pair.
// Root remains the project root for Loop Definition, policy, artifacts, and
// evidence. StatePath and JournalPath redirect the Runtime pair and must be
// supplied together for RecoveryReplay; the ordinary ControlRequest path
// defaults remain available through RunControlCycle.
type RecoveryReplayRequest struct {
	Root               string
	StatePath          string
	JournalPath        string
	MaxSteps           int
	Event              string
	ToolName           string
	ToolInput          map[string]any
	AffectedPaths      []string
	QualityCycleBudget time.Duration
	GateEvaluator      qualitygate.Evaluator
}

// RecoveryReplayTrace records one production control cycle. A trace is
// appended before the replay decides whether to continue, so a conflict,
// unknown gate, or failed progress check is always retained for the recovery
// plan.
type RecoveryReplayTrace struct {
	Step                int             `json:"step"`
	BeforeCursor        string          `json:"before_cursor"`
	AfterCursor         string          `json:"after_cursor"`
	BeforeRevision      int             `json:"before_revision"`
	AfterRevision       int             `json:"after_revision"`
	Status              ControlStatus   `json:"status"`
	GateID              string          `json:"gate_id,omitempty"`
	CandidateTransition string          `json:"candidate_transition,omitempty"`
	ErrorCode           string          `json:"error_code,omitempty"`
	Error               string          `json:"error,omitempty"`
	Missing             []string        `json:"missing,omitempty"`
	EvidenceRefs        []string        `json:"evidence_refs,omitempty"`
	Conflicts           []string        `json:"conflicts,omitempty"`
	TransitionCommitted bool            `json:"transition_committed"`
	Decision            policy.Decision `json:"decision"`
}

// RecoveryReplayResult is the read-only replay projection. The FinalSnapshot
// is the last staging snapshot observed; active Runtime files are never read
// or written by RecoveryReplay unless the caller explicitly supplies them as
// the staging pair.
type RecoveryReplayResult struct {
	Trace         []RecoveryReplayTrace `json:"trace"`
	FinalSnapshot runtime.Snapshot      `json:"final_snapshot"`
	FinalCursor   string                `json:"final_cursor"`
	StopReason    ReplayStopReason      `json:"stop_reason"`
}

// RecoveryReplay repeatedly invokes the production RunControlCycle against a
// caller-prepared, schema-valid staging state/journal pair. Each cycle can
// commit at most one transition through the normal selector, Quality Gate,
// transition Apply, and Guard path. Replay stops at the first non-advanced
// outcome, a human-required result, the configured step limit, or a repeated
// cursor/revision. It never creates a second recovery state machine.
func RecoveryReplay(ctx context.Context, request RecoveryReplayRequest) (RecoveryReplayResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if request.MaxSteps < 0 {
		return RecoveryReplayResult{}, fmt.Errorf("recovery replay: max steps must not be negative: %d", request.MaxSteps)
	}
	if request.Root == "" {
		return RecoveryReplayResult{}, fmt.Errorf("recovery replay: root is required")
	}
	if (request.StatePath == "") != (request.JournalPath == "") {
		return RecoveryReplayResult{}, fmt.Errorf("recovery replay: state and journal paths must be supplied as a pair")
	}
	maxSteps := request.MaxSteps
	if maxSteps == 0 {
		maxSteps = DefaultRecoveryReplayMaxSteps
	}
	if err := ctx.Err(); err != nil {
		return RecoveryReplayResult{}, fmt.Errorf("recovery replay: %w", err)
	}

	rootPath, err := canonicalRecoveryReplayRoot(request.Root)
	if err != nil {
		return RecoveryReplayResult{}, fmt.Errorf("recovery replay: resolve root: %w", err)
	}
	cycleRequest := ControlRequest{
		Root:               rootPath,
		StatePath:          request.StatePath,
		JournalPath:        request.JournalPath,
		Event:              nonEmpty(request.Event, "PreToolUse"),
		ToolName:           nonEmpty(request.ToolName, "RecoveryReplay"),
		ToolInput:          request.ToolInput,
		AffectedPaths:      append([]string(nil), request.AffectedPaths...),
		QualityCycleBudget: request.QualityCycleBudget,
		GateEvaluator:      request.GateEvaluator,
	}
	statePath, journalPath := controlRuntimePaths(cycleRequest)
	statePath, err = resolveRecoveryReplayPath(rootPath, statePath, "state")
	if err != nil {
		return RecoveryReplayResult{}, fmt.Errorf("recovery replay: %w", err)
	}
	journalPath, err = resolveRecoveryReplayPath(rootPath, journalPath, "journal")
	if err != nil {
		return RecoveryReplayResult{}, fmt.Errorf("recovery replay: %w", err)
	}
	cycleRequest.StatePath = statePath
	cycleRequest.JournalPath = journalPath
	activeStatePath, activeJournalPath := controlRuntimePaths(ControlRequest{Root: rootPath})
	activeStatePath, err = resolveRecoveryReplayPath(rootPath, activeStatePath, "active state")
	if err != nil {
		return RecoveryReplayResult{}, fmt.Errorf("recovery replay: %w", err)
	}
	activeJournalPath, err = resolveRecoveryReplayPath(rootPath, activeJournalPath, "active journal")
	if err != nil {
		return RecoveryReplayResult{}, fmt.Errorf("recovery replay: %w", err)
	}
	if filepath.Clean(statePath) == filepath.Clean(activeStatePath) || filepath.Clean(journalPath) == filepath.Clean(activeJournalPath) {
		return RecoveryReplayResult{}, fmt.Errorf("recovery replay: staging pair must not use the active Runtime paths")
	}
	store := runtime.NewStore(statePath, journalPath)
	snapshot, err := store.Snapshot()
	if err != nil {
		return RecoveryReplayResult{}, fmt.Errorf("recovery replay: read staging runtime: %w", err)
	}

	result := RecoveryReplayResult{Trace: []RecoveryReplayTrace{}}
	seenCursors := map[string]struct{}{snapshotCursorString(snapshot): {}}
	seenRevisions := map[int]struct{}{snapshot.Revision: {}}
	for step := 1; step <= maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			result = finalizeRecoveryReplay(result, snapshot)
			return result, fmt.Errorf("recovery replay canceled before step %d: %w", step, err)
		}
		if err := validateResolvedRecoveryReplayPair(rootPath, statePath, journalPath); err != nil {
			result = finalizeRecoveryReplay(result, snapshot)
			return result, fmt.Errorf("recovery replay step %d: %w", step, err)
		}

		beforeCursor := snapshotCursorString(snapshot)
		controlResult, cycleErr := RunControlCycle(ctx, cycleRequest)
		if cycleErr != nil {
			return finalizeRecoveryReplay(result, snapshot), fmt.Errorf("recovery replay step %d: %w", step, cycleErr)
		}
		if err := ctx.Err(); err != nil {
			result.Trace = append(result.Trace, replayTrace(step, beforeCursor, snapshot, controlResult))
			result = finalizeRecoveryReplay(result, snapshot)
			return result, fmt.Errorf("recovery replay canceled after step %d: %w", step, err)
		}

		trace := replayTrace(step, beforeCursor, snapshot, controlResult)
		result.Trace = append(result.Trace, trace)
		after := controlResult.Snapshot
		if after.State == nil {
			after = snapshot
		}
		result.FinalSnapshot = after

		switch controlResult.QualityGate.Status {
		case StatusAdvanced:
			if !controlResult.QualityGate.TransitionCommitted {
				result.StopReason = ReplayStopNoProgress
				return finalizeRecoveryReplay(result, after), fmt.Errorf("recovery replay step %d: %w", step, ErrRecoveryReplayNoProgress)
			}
			if after.Revision <= snapshot.Revision || snapshotCursorString(after) == beforeCursor {
				result.StopReason = ReplayStopNoProgress
				return finalizeRecoveryReplay(result, after), fmt.Errorf("recovery replay step %d: %w", step, ErrRecoveryReplayNoProgress)
			}
			afterCursor := snapshotCursorString(after)
			if _, exists := seenCursors[afterCursor]; exists {
				result.StopReason = ReplayStopNoProgress
				return finalizeRecoveryReplay(result, after), fmt.Errorf("recovery replay step %d: %w (cursor=%s)", step, ErrRecoveryReplayRepeatedCursor, afterCursor)
			}
			if _, exists := seenRevisions[after.Revision]; exists {
				result.StopReason = ReplayStopNoProgress
				return finalizeRecoveryReplay(result, after), fmt.Errorf("recovery replay step %d: %w (revision=%d)", step, ErrRecoveryReplayRepeatedCursor, after.Revision)
			}
			seenCursors[afterCursor] = struct{}{}
			seenRevisions[after.Revision] = struct{}{}
			snapshot = after
		case StatusNotReady:
			result.StopReason = ReplayStopNotReady
			return finalizeRecoveryReplay(result, after), nil
		case StatusUnknown:
			if controlResult.QualityGate.ErrorCode == CodeTriggerConfl {
				result.StopReason = ReplayStopConflict
			} else {
				result.StopReason = ReplayStopUnknown
			}
			return finalizeRecoveryReplay(result, after), nil
		case StatusBlocked:
			result.StopReason = ReplayStopHuman
			return finalizeRecoveryReplay(result, after), nil
		case StatusSatisfied:
			result.StopReason = ReplayStopNoAuto
			return finalizeRecoveryReplay(result, after), nil
		default:
			return finalizeRecoveryReplay(result, after), fmt.Errorf("recovery replay step %d: unsupported controller status %q", step, controlResult.QualityGate.Status)
		}
	}

	result.StopReason = ReplayStopMaxSteps
	return finalizeRecoveryReplay(result, snapshot), nil
}

func replayTrace(step int, beforeCursor string, before runtime.Snapshot, result ControlResult) RecoveryReplayTrace {
	after := result.Snapshot
	if after.State == nil {
		after = before
	}
	return RecoveryReplayTrace{
		Step:                step,
		BeforeCursor:        beforeCursor,
		AfterCursor:         snapshotCursorString(after),
		BeforeRevision:      before.Revision,
		AfterRevision:       after.Revision,
		Status:              result.QualityGate.Status,
		GateID:              result.QualityGate.GateID,
		CandidateTransition: result.QualityGate.CandidateTransition,
		ErrorCode:           result.QualityGate.ErrorCode,
		Error:               result.Error,
		Missing:             append([]string(nil), result.QualityGate.Missing...),
		EvidenceRefs:        append([]string(nil), result.QualityGate.EvidenceRefs...),
		Conflicts:           append([]string(nil), result.QualityGate.Conflicts...),
		TransitionCommitted: result.QualityGate.TransitionCommitted,
		Decision:            result.Decision,
	}
}

func finalizeRecoveryReplay(result RecoveryReplayResult, snapshot runtime.Snapshot) RecoveryReplayResult {
	result.FinalSnapshot = snapshot
	result.FinalCursor = snapshotCursorString(snapshot)
	return result
}

func snapshotCursorString(snapshot runtime.Snapshot) string {
	state, phase := snapshotCursor(snapshot.State)
	return cursorString(state, phase)
}

func canonicalRecoveryReplayRoot(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("absolute root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(filepath.Clean(absRoot))
	if err != nil {
		return "", fmt.Errorf("evaluate root symlinks: %w", err)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return "", fmt.Errorf("stat root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("root is not a directory: %s", resolvedRoot)
	}
	return filepath.Clean(resolvedRoot), nil
}

func resolveRecoveryReplayPath(root, path, kind string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s path: %w", kind, err)
	}
	resolvedPath, err := resolveExistingRecoveryReplayPrefix(filepath.Clean(absPath))
	if err != nil {
		return "", fmt.Errorf("resolve %s path: %w", kind, err)
	}
	if !recoveryReplayPathWithinRoot(root, resolvedPath) {
		return "", fmt.Errorf("%w: %s path %q resolves to %q (root %q)", ErrRecoveryReplayPathOutsideRoot, kind, path, resolvedPath, root)
	}
	return resolvedPath, nil
}

// resolveExistingRecoveryReplayPrefix resolves all symlinks in the existing
// portion of a path and appends any not-yet-created suffix. This catches both
// an existing staging-file symlink and a parent-directory symlink before the
// Runtime Store opens or creates either file.
func resolveExistingRecoveryReplayPrefix(path string) (string, error) {
	candidate := filepath.Clean(path)
	var suffix []string
	for {
		_, err := os.Lstat(candidate)
		if err == nil {
			resolved, evalErr := filepath.EvalSymlinks(candidate)
			if evalErr != nil {
				return "", evalErr
			}
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", fmt.Errorf("no existing path prefix for %q", path)
		}
		suffix = append(suffix, filepath.Base(candidate))
		candidate = parent
	}
}

func validateResolvedRecoveryReplayPair(root, statePath, journalPath string) error {
	if _, err := resolveRecoveryReplayPath(root, statePath, "state"); err != nil {
		return err
	}
	if _, err := resolveRecoveryReplayPath(root, journalPath, "journal"); err != nil {
		return err
	}
	return nil
}

func recoveryReplayPathWithinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative != ".." && relative != "." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
