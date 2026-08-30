package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// Stable error codes returned on the QualityGate.ErrorCode field and on
// ControlResult.ErrorCode. These mirror the BE-039 §9 / SYNC-039 §11 codes
// the cycle uses to tell callers whether to retry or surface a reconcile.
const (
	CodeCASStale      = "LOOP_CAS_STALE"
	CodeGateUnknown   = "LOOP_GATE_UNKNOWN"
	CodeTriggerConfl  = "LOOP_TRIGGER_CONFLICT"
	CodeRuntimeInval  = "LOOP_RUNTIME_INVALID"
	CodeTransitionGua = "LOOP_TRANSITION_GUARD"
	CodeMilestoneStl  = "LOOP_MILESTONE_STALE"
)

// Metrics counters exported for the ARCHITECTURE-039 §14.1 owner wiring.
// The Controller owns the gate/transition/CAS counters (TASK-039-04).
var (
	MetricsGateEvaluations   = 0
	MetricsTransitionCommits = 0
	MetricsCASConflicts      = 0
)

// diskFiles is the production FileView used to back the Quality Gate
// evaluator. It reads from the project root relative paths.
type diskFiles struct {
	root string
}

func (d diskFiles) ReadDir(dir string) ([]os.DirEntry, error) {
	if dir == "" {
		dir = "."
	}
	cleaned := filepath.Clean(dir)
	if filepath.IsAbs(cleaned) {
		return os.ReadDir(cleaned)
	}
	return os.ReadDir(filepath.Join(d.root, cleaned))
}

func (d diskFiles) ReadFile(path string) ([]byte, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}
	cleaned := filepath.Clean(path)
	if filepath.IsAbs(cleaned) {
		return os.ReadFile(cleaned)
	}
	return os.ReadFile(filepath.Join(d.root, cleaned))
}

// snapshotCursor reads the current state/phase from a snapshot. It mirrors
// the qualitygate.currentStatePhase helper but lives here so we don't have
// to export internal types.
func snapshotCursor(state map[string]any) (string, string) {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	stateName, _ := lifecycle["state"].(string)
	phaseName, _ := lifecycle["phase"].(string)
	return stateName, phaseName
}

// RunControlCycle is the single PreToolUse entrypoint consumed by the Hook
// adapter (and re-used by SessionStart/PreCompact in projection form). It
// implements the eleven steps of BUG-039-02 §4.1 verbatim:
//
//  1. Parse Hook payload / tool name / input paths.
//  2. Read snapshot revision N via runtime.Store.
//  3. Resolve current cursor, bound REQ, active assignment, Milestone.
//  4. Compute affected paths.
//  5. transition.LoadCatalog + ResolveAutomaticTransition for the cursor.
//  6. qualitygate.Evaluator.Evaluate against the current cursor.
//  7. On satisfied, call transition.Apply with expected_revision=N (at
//     most once per cycle).
//  8. On CAS stale, re-read and recompute once; second stale returns
//     unknown + LOOP_CAS_STALE.
//  9. On success, refresh Milestone / Guidance / Journal (via the cli
//     refreshMilestone helper).
//  10. On the new cursor, run final safety (locked artifact / squash
//     merge) via policy.Engine.Evaluate.
//  11. Return ControlResult.
//
// CallerHooks MUST treat the Decision field as the authoritative tool
// verdict; a non-block Decision always implies the tool may proceed
// (BE-039 §3.2: "Quality not_ready must NOT map to tool block").
func RunControlCycle(ctx context.Context, req ControlRequest) (ControlResult, error) {
	if strings.TrimSpace(req.Root) == "" {
		return ControlResult{}, fmt.Errorf("controller: root is required")
	}
	result := ControlResult{
		QualityGate: QualityGateResult{
			Status:           StatusUnknown,
			ObservedRevision: 0,
			Missing:          []string{},
			EvidenceRefs:     []string{},
		},
		Warnings: []string{},
	}
	if ctx == nil {
		ctx = context.Background()
	}

	statePath, journalPath := controlRuntimePaths(req)

	// --- Step 2: read snapshot revision N ---
	store := runtime.NewStore(statePath, journalPath)
	snapshot, err := store.Snapshot()
	if err != nil {
		result.Error = fmt.Sprintf("read runtime: %v", err)
		result.ErrorCode = CodeRuntimeInval
		result.QualityGate.Status = StatusUnknown
		result.QualityGate.ErrorCode = CodeRuntimeInval
		return result, nil
	}
	result.Snapshot = snapshot
	result.QualityGate.ObservedRevision = snapshot.Revision

	cursorState, cursorPhase := snapshotCursor(snapshot.State)
	result.QualityGate.NextCursor = cursorString(cursorState, cursorPhase)

	// --- Step 5: build catalog + resolve automatic candidate ---
	catalog, err := transition.LoadCatalog(req.Root)
	if err != nil {
		result.Error = fmt.Sprintf("load catalog: %v", err)
		result.ErrorCode = CodeRuntimeInval
		return result, nil
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		result.Error = fmt.Sprintf("build gate registry: %v", err)
		result.ErrorCode = CodeRuntimeInval
		return result, nil
	}
	var evaluator qualitygate.Evaluator = qualitygate.NewEvaluator(registry)
	if req.GateEvaluator != nil {
		evaluator = req.GateEvaluator
	}

	// --- Step 4: compute affected paths (fall back to tool-derived probe) ---
	affected := normalizeAffectedPaths(req)

	cursor := transition.Cursor{State: cursorState, Phase: cursorPhase}
	candidates := automaticCandidatesFor(catalog, cursor)

	// --- Step 6: Evaluate every automatic candidate gate within budget ---
	files := req.Files
	if files == nil {
		files = diskFiles{root: req.Root}
	}
	if len(candidates) == 0 {
		// No auto-trigger candidate at the current cursor (planning already
		// complete, terminal, or non-eligible phase). The cycle still
		// returns a ControlResult; the final safety policy is what governs
		// the tool.
		result.QualityGate.Status = StatusSatisfied
		result.QualityGate.TransitionCommitted = false
		result.Decision = allowDecision()
		result.Snapshot = snapshot
		return result, nil
	}
	budget := ResolveQualityCycleBudget(catalog, req.QualityCycleBudget)
	evalResults, timedOut := evaluateAutomaticGates(ctx, budget, evaluator, req, snapshot, candidates, affected, files)
	if timedOut {
		result.QualityGate.Status = StatusUnknown
		result.QualityGate.ErrorCode = CodeGateUnknown
		result.ErrorCode = CodeGateUnknown
		result.Error = "quality cycle timed out"
		result.QualityGate.TransitionCommitted = false
		return applyFinalSafety(result, req, snapshot, affected), nil
	}

	// --- Step 5/7: selector seam — gate outcomes + requested events ---
	facts := buildTriggerFacts(evaluator, req, snapshot, candidates, evalResults, affected, files)
	if conflict, conflictIDs := detectSelectorEvidenceConflict(facts, evalResults); conflict {
		metrics.RecordGateEvaluation(req.Root, string(StatusUnknown))
		result.QualityGate.Status = StatusUnknown
		result.QualityGate.ErrorCode = CodeTriggerConfl
		result.QualityGate.Conflicts = conflictIDs
		result.Error = (&transition.TriggerConflictError{
			Cursor:       cursor,
			CandidateIDs: conflictIDs,
		}).Error()
		result.ErrorCode = CodeTriggerConfl
		result.Decision = allowDecision()
		return result, nil
	}
	resolution, err := catalog.ResolveAutomaticTransition(cursor, facts)
	if err != nil {
		var conflict *transition.TriggerConflictError
		if errors.As(err, &conflict) {
			metrics.RecordGateEvaluation(req.Root, string(StatusUnknown))
			result.QualityGate.Status = StatusUnknown
			result.QualityGate.ErrorCode = CodeTriggerConfl
			result.QualityGate.Conflicts = append([]string{}, conflict.CandidateIDs...)
			result.Error = err.Error()
			result.ErrorCode = CodeTriggerConfl
			result.Decision = allowDecision()
			return result, nil
		}
		result.Error = fmt.Sprintf("resolve automatic transition: %v", err)
		result.ErrorCode = CodeGateUnknown
		return result, nil
	}

	var candidate *transition.TransitionSpec
	var gateID string
	var evaluation qualitygate.Evaluation
	if resolution.Transition != nil {
		candidate = resolution.Transition
		gateID = candidate.AutoTrigger.QualityGateID
		for _, item := range evalResults {
			if item.candidate.ID == candidate.ID {
				evaluation = item.evaluation
				break
			}
		}
	} else {
		gateID, evaluation, candidate = projectZeroSelected(candidates, evalResults)
	}

	gateStatus, gateError, conflicts := projectGateResult(evaluation)
	result.QualityGate.Status = gateStatus
	result.QualityGate.GateID = gateID
	result.QualityGate.Fingerprint = evaluation.Fingerprint
	result.QualityGate.Missing = nonNilStrings(evaluation.Missing)
	result.QualityGate.EvidenceRefs = nonNilStrings(evaluation.EvidenceRefs)
	result.QualityGate.Conflicts = nonNilStrings(conflicts)
	result.QualityGate.ErrorCode = gateError
	result.QualityGate.NextCursor = nonEmpty(evaluation.NextCursor, result.QualityGate.NextCursor)
	if candidate != nil {
		result.QualityGate.CandidateTransition = candidate.ID
	}
	metrics.RecordGateEvaluation(req.Root, string(gateStatus))

	// --- Step 7: optionally commit one transition ---
	if gateStatus == StatusSatisfied && candidate != nil && candidate.AutoTrigger != nil && candidate.AutoTrigger.Actor != "" {
		// Honor the AutoTrigger.actor contract: only the configured actor
		// (typically "hook_controller") may drive this transition from the
		// controller seam.
		next, applyErr := transition.Apply(req.Root, statePath, journalPath, autoTransitionRequest(req, registry, snapshot, candidate, gateID, evaluation))
		if applyErr != nil {
			// CAS stale: re-read, recompute once, and try again.
			if errors.Is(applyErr, runtime.ErrStaleRevision) {
				MetricsCASConflicts++
				metrics.RecordCASConflict(req.Root)
				rebroadcast, _, recomputeErr := recomputeAfterStale(req, store, catalog, registry, evaluator, ctx)
				if recomputeErr != nil {
					result.QualityGate.Status = StatusUnknown
					result.QualityGate.ErrorCode = CodeCASStale
					result.Error = recomputeErr.Error()
					result.ErrorCode = CodeCASStale
					result.Decision = allowDecision()
					return result, nil
				}
				result.QualityGate = rebroadcast.QualityGate
				result.Snapshot = rebroadcast.Snapshot
				result.Decision = rebroadcast.Decision
				if rebroadcast.Guidance != nil {
					result.Guidance = rebroadcast.Guidance
				}
				return result, nil
			}
			// Any other apply error: surface as unknown + transition guard.
			// The tool MUST still be allowed unless the final safety layer
			// blocks it.
			result.QualityGate.Status = StatusUnknown
			result.QualityGate.ErrorCode = CodeTransitionGua
			result.Error = applyErr.Error()
			result.ErrorCode = CodeTransitionGua
			result.QualityGate.TransitionCommitted = false
			result.Decision = allowDecision()
			result.Snapshot = snapshot
			return result, nil
		}
		MetricsTransitionCommits++
		metrics.RecordTransitionCommit(req.Root, candidate.ID)
		snapshot = next
		result.Snapshot = next
		result.QualityGate.ObservedRevision = next.Revision
		result.QualityGate.TransitionCommitted = true
		result.QualityGate.Status = StatusAdvanced
		// Recompute next-cursor from the post-commit snapshot.
		cursorState, cursorPhase = snapshotCursor(next.State)
		result.QualityGate.NextCursor = cursorString(cursorState, cursorPhase)
	}

	// --- Step 10: final safety on the new cursor ---
	return applyFinalSafety(result, req, snapshot, affected), nil
}

// evaluateGateWithBudget runs gate evaluation under a local quality-cycle
// deadline. The controller owns this budget (ARCHITECTURE-039 §14.1); it does
// not rely on the host hook timeout.
func evaluateGateWithBudget(
	ctx context.Context,
	budget time.Duration,
	evaluator qualitygate.Evaluator,
	input qualitygate.Input,
) (qualitygate.Evaluation, error, bool) {
	evalCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	type outcome struct {
		evaluation qualitygate.Evaluation
		err        error
	}
	ch := make(chan outcome, 1)
	go func() {
		evaluation, err := evaluator.Evaluate(evalCtx, input)
		ch <- outcome{evaluation: evaluation, err: err}
	}()

	select {
	case res := <-ch:
		return res.evaluation, res.err, false
	case <-evalCtx.Done():
		return qualitygate.Evaluation{}, nil, true
	}
}

// applyFinalSafety runs the minimal safety policy on the committed snapshot
// cursor and projects the tool decision. It is shared by the normal path and
// the quality-cycle timeout degradation path.
func applyFinalSafety(
	result ControlResult,
	req ControlRequest,
	snapshot runtime.Snapshot,
	affected []string,
) ControlResult {
	safetyInput := buildSafetyInput(req, snapshot, affected)
	engine, err := policy.Load(filepath.Join(req.Root, "docs", "hook-policy.json"))
	if err != nil {
		// A missing policy document must not block the tool — fall back to
		// allow. The Hook adapter will surface a separate warning.
		result.Warnings = append(result.Warnings, fmt.Sprintf("load hook-policy: %v", err))
		engine = nil
	}
	if engine != nil {
		decision, err := engine.Evaluate(safetyInput)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("safety evaluate: %v", err))
			decision = allowDecision()
		}
		// Quality gate may ONLY be projected to blocked when the safety
		// layer actually denied the call. not_ready is intentionally
		// distinct (BE-039 §3.2 / §5.2).
		if decision.Decision == "block" || decision.Decision == "deny" {
			decision.HumanRequired = decision.HumanRequired || (decision.RuleID == policy.RuleLockedArtifactWrite)
			result.Decision = decision
			result.QualityGate.Status = StatusBlocked
			return result
		}
		result.Decision = decision
	} else {
		result.Decision = allowDecision()
	}

	// Default: the tool may proceed regardless of gate progress. not_ready
	// is the documented "continue working" signal.
	if result.Decision.Decision == "" {
		result.Decision = allowDecision()
	}
	return result
}

// recomputeAfterStale re-reads the runtime, re-evaluates the gate, and
// (if the gate is now satisfied) retries the transition with the new
// expected revision. Per BUG-039-02 §4.1 step 8 the cycle only retries
// once: a second CAS stale returns unknown + LOOP_CAS_STALE.
func recomputeAfterStale(
	req ControlRequest,
	store *runtime.Store,
	catalog *transition.Catalog,
	registry *qualitygate.Registry,
	evaluator qualitygate.Evaluator,
	ctx context.Context,
) (ControlResult, int, error) {
	refreshed, err := store.Snapshot()
	if err != nil {
		return ControlResult{}, 1, fmt.Errorf("reread runtime: %w", err)
	}
	cursorState, cursorPhase := snapshotCursor(refreshed.State)
	cursor := transition.Cursor{State: cursorState, Phase: cursorPhase}
	candidates := automaticCandidatesFor(catalog, cursor)
	if len(candidates) == 0 {
		// Even on retry we never block the tool.
		out := ControlResult{
			Snapshot: refreshed,
			Decision: allowDecision(),
			QualityGate: QualityGateResult{
				Status:           StatusUnknown,
				ObservedRevision: refreshed.Revision,
				NextCursor:       cursorString(cursorState, cursorPhase),
				ErrorCode:        CodeCASStale,
				Missing:          []string{},
				EvidenceRefs:     []string{},
			},
			Error:     "cas stale and no auto candidate after reread",
			ErrorCode: CodeCASStale,
		}
		return out, 1, nil
	}
	files := req.Files
	if files == nil {
		files = diskFiles{root: req.Root}
	}
	affected := normalizeAffectedPaths(req)
	budget := ResolveQualityCycleBudget(catalog, req.QualityCycleBudget)
	evalResults, timedOut := evaluateAutomaticGates(ctx, budget, evaluator, req, refreshed, candidates, affected, files)
	if timedOut {
		out := ControlResult{
			Snapshot: refreshed,
			Decision: allowDecision(),
			QualityGate: QualityGateResult{
				Status:           StatusUnknown,
				ObservedRevision: refreshed.Revision,
				NextCursor:       cursorString(cursorState, cursorPhase),
				ErrorCode:        CodeCASStale,
				Missing:          []string{},
				EvidenceRefs:     []string{},
			},
			Error:     "cas stale and quality cycle timed out after reread",
			ErrorCode: CodeCASStale,
		}
		return out, 1, nil
	}
	facts := buildTriggerFacts(evaluator, req, refreshed, candidates, evalResults, affected, files)
	resolution, err := catalog.ResolveAutomaticTransition(cursor, facts)
	if err != nil {
		var conflict *transition.TriggerConflictError
		if errors.As(err, &conflict) {
			out := ControlResult{
				Snapshot: refreshed,
				Decision: allowDecision(),
				QualityGate: QualityGateResult{
					Status:           StatusUnknown,
					ObservedRevision: refreshed.Revision,
					NextCursor:       cursorString(cursorState, cursorPhase),
					ErrorCode:        CodeTriggerConfl,
					Conflicts:        append([]string{}, conflict.CandidateIDs...),
					Missing:          []string{},
					EvidenceRefs:     []string{},
				},
				Error:     err.Error(),
				ErrorCode: CodeTriggerConfl,
			}
			return out, 1, nil
		}
		return ControlResult{}, 1, fmt.Errorf("resolve automatic transition after stale: %w", err)
	}
	if resolution.Transition == nil {
		gateID, evaluation, candidate := projectZeroSelected(candidates, evalResults)
		gateStatus, _, _ := projectGateResult(evaluation)
		metrics.RecordGateEvaluation(req.Root, string(gateStatus))
		out := ControlResult{
			Snapshot: refreshed,
			Decision: allowDecision(),
			QualityGate: QualityGateResult{
				Status:              gateStatus,
				GateID:              gateID,
				CandidateTransition: candidateID(candidate),
				ObservedRevision:    refreshed.Revision,
				Fingerprint:         evaluation.Fingerprint,
				Missing:             nonNilStrings(evaluation.Missing),
				EvidenceRefs:        nonNilStrings(evaluation.EvidenceRefs),
				NextCursor:          cursorString(cursorState, cursorPhase),
				ErrorCode:           CodeCASStale,
			},
			Error:     "cas stale and gate not satisfied after reread",
			ErrorCode: CodeCASStale,
		}
		return out, 1, nil
	}
	candidate := resolution.Transition
	gateID := candidate.AutoTrigger.QualityGateID
	var evaluation qualitygate.Evaluation
	for _, item := range evalResults {
		if item.candidate.ID == candidate.ID {
			evaluation = item.evaluation
			break
		}
	}
	gateStatus, _, _ := projectGateResult(evaluation)
	metrics.RecordGateEvaluation(req.Root, string(gateStatus))
	if gateStatus != StatusSatisfied {
		out := ControlResult{
			Snapshot: refreshed,
			Decision: allowDecision(),
			QualityGate: QualityGateResult{
				Status:              gateStatus,
				GateID:              gateID,
				CandidateTransition: candidate.ID,
				ObservedRevision:    refreshed.Revision,
				Fingerprint:         evaluation.Fingerprint,
				Missing:             nonNilStrings(evaluation.Missing),
				EvidenceRefs:        nonNilStrings(evaluation.EvidenceRefs),
				NextCursor:          cursorString(cursorState, cursorPhase),
				ErrorCode:           CodeCASStale,
			},
			Error:     "cas stale and gate not satisfied after reread",
			ErrorCode: CodeCASStale,
		}
		return out, 1, nil
	}
	// Retry the transition once with the refreshed expected revision.
	statePath, journalPath := controlRuntimePaths(req)
	_, applyErr := transition.Apply(req.Root, statePath, journalPath,
		autoTransitionRequest(req, registry, refreshed, candidate, gateID, evaluation))
	if applyErr != nil {
		if errors.Is(applyErr, runtime.ErrStaleRevision) {
			MetricsCASConflicts++
			metrics.RecordCASConflict(req.Root)
		}
		// Second stale: the cycle must NOT retry a third time.
		out := ControlResult{
			Snapshot: refreshed,
			Decision: allowDecision(),
			QualityGate: QualityGateResult{
				Status:              StatusUnknown,
				GateID:              gateID,
				CandidateTransition: candidate.ID,
				ObservedRevision:    refreshed.Revision,
				Fingerprint:         evaluation.Fingerprint,
				NextCursor:          cursorString(cursorState, cursorPhase),
				ErrorCode:           CodeCASStale,
			},
			Error:     fmt.Sprintf("second CAS stale: %v", applyErr),
			ErrorCode: CodeCASStale,
		}
		return out, 1, nil
	}
	final, err := store.Snapshot()
	if err != nil {
		return ControlResult{}, 1, fmt.Errorf("reread runtime after retry: %w", err)
	}
	MetricsTransitionCommits++
	metrics.RecordTransitionCommit(req.Root, candidate.ID)
	cursorState, cursorPhase = snapshotCursor(final.State)
	out := ControlResult{
		Snapshot: final,
		Decision: allowDecision(),
		QualityGate: QualityGateResult{
			Status:              StatusAdvanced,
			GateID:              gateID,
			CandidateTransition: candidate.ID,
			ObservedRevision:    final.Revision,
			Fingerprint:         evaluation.Fingerprint,
			EvidenceRefs:        nonNilStrings(evaluation.EvidenceRefs),
			NextCursor:          cursorString(cursorState, cursorPhase),
			TransitionCommitted: true,
		},
	}
	return out, 1, nil
}

type automaticGateEval struct {
	candidate  transition.TransitionSpec
	gateID     string
	evaluation qualitygate.Evaluation
}

func evaluateAutomaticGates(
	ctx context.Context,
	budget time.Duration,
	evaluator qualitygate.Evaluator,
	req ControlRequest,
	snapshot runtime.Snapshot,
	candidates []transition.TransitionSpec,
	affected []string,
	files qualityGateFiles,
) ([]automaticGateEval, bool) {
	deadline := time.Now().Add(budget)
	results := make([]automaticGateEval, 0, len(candidates))
	for _, spec := range candidates {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return results, true
		}
		gateID := spec.AutoTrigger.QualityGateID
		evaluation, err, timedOut := evaluateGateWithBudget(ctx, remaining, evaluator, qualitygate.Input{
			Root:          req.Root,
			Snapshot:      snapshot,
			TransitionID:  spec.ID,
			GateID:        gateID,
			AffectedPaths: affected,
			Files:         files,
		})
		MetricsGateEvaluations++
		if timedOut {
			return results, true
		}
		if err != nil {
			evaluation = qualitygate.Evaluation{
				Status: qualitygate.StatusUnknown,
				GateID: gateID,
			}
		}
		results = append(results, automaticGateEval{
			candidate:  spec,
			gateID:     gateID,
			evaluation: evaluation,
		})
	}
	return results, false
}

func buildTriggerFacts(
	evaluator qualitygate.Evaluator,
	req ControlRequest,
	snapshot runtime.Snapshot,
	candidates []transition.TransitionSpec,
	results []automaticGateEval,
	affected []string,
	files qualityGateFiles,
) transition.TriggerFacts {
	outcomes := make([]transition.GateOutcome, 0, len(results))
	for _, item := range results {
		outcomes = append(outcomes, transition.GateOutcome{
			GateID: item.gateID,
			Status: string(item.evaluation.Status),
		})
	}
	baseInput := qualitygate.Input{
		Root:          req.Root,
		Snapshot:      snapshot,
		AffectedPaths: affected,
		Files:         files,
	}
	if len(candidates) > 0 {
		baseInput.TransitionID = candidates[0].ID
		baseInput.GateID = candidates[0].AutoTrigger.QualityGateID
	}
	var requested []string
	if eng, ok := evaluator.(*qualitygate.Engine); ok {
		requested = eng.RequestedEvents(baseInput)
	}
	return transition.TriggerFacts{
		RequestedEvents: requested,
		GateOutcomes:    outcomes,
	}
}

func projectZeroSelected(
	candidates []transition.TransitionSpec,
	results []automaticGateEval,
) (gateID string, evaluation qualitygate.Evaluation, candidate *transition.TransitionSpec) {
	// The projected candidate is user-facing guidance (ready / recovery
	// packets): a tie between equally-eligible candidates must resolve
	// deterministically regardless of upstream collection order, so sort
	// by candidate ID before picking (2026-08-23 E2E dogfood: ready
	// flapped between two pause gates in a running verification round).
	results = append([]automaticGateEval(nil), results...)
	sort.Slice(results, func(i, j int) bool { return results[i].candidate.ID < results[j].candidate.ID })
	eventByID := make(map[string]string, len(candidates))
	phaseLocal := make(map[string]bool, len(candidates))
	for _, spec := range candidates {
		eventByID[spec.ID] = spec.Event
		// Phase-machine transitions keep From as the phase name; top-level
		// transitions keep From as the lifecycle state. Prefer phase-local
		// candidates when projecting a zero-selected cursor.
		if spec.FromPhase != "" || (spec.From != "" && !isTopLevelLifecycleState(spec.From)) {
			phaseLocal[spec.ID] = true
		}
	}

	pick := func(preferSuccess bool, preferPhase bool) *automaticGateEval {
		var chosen *automaticGateEval
		for i := range results {
			item := &results[i]
			if item.evaluation.Status != qualitygate.StatusNotReady {
				continue
			}
			if preferSuccess && !isSuccessPathEvent(eventByID[item.candidate.ID]) {
				continue
			}
			if preferPhase && !phaseLocal[item.candidate.ID] {
				continue
			}
			if len(item.evaluation.Missing) == 0 && preferSuccess {
				continue
			}
			if chosen == nil {
				chosen = item
				continue
			}
			// Prefer non-empty Missing for actionable guidance.
			if len(chosen.evaluation.Missing) == 0 && len(item.evaluation.Missing) > 0 {
				chosen = item
			}
		}
		return chosen
	}

	for _, preferPhase := range []bool{true, false} {
		for _, preferSuccess := range []bool{true, false} {
			if selected := pick(preferSuccess, preferPhase); selected != nil {
				c := selected.candidate
				return selected.gateID, selected.evaluation, &c
			}
		}
	}
	for _, item := range results {
		if item.evaluation.Status == qualitygate.StatusUnknown {
			selected := item.candidate
			return item.gateID, item.evaluation, &selected
		}
	}
	if len(results) > 0 {
		item := results[0]
		selected := item.candidate
		return item.gateID, item.evaluation, &selected
	}
	return "", qualitygate.Evaluation{}, nil
}

func isTopLevelLifecycleState(name string) bool {
	switch name {
	case "inactive", "planning", "document_verification", "building", "verification",
		"bug_resolution", "acceptance", "release_audit", "awaiting_human_release", "release_authorized", "paused", "aborted":
		return true
	default:
		return false
	}
}

func isSuccessPathEvent(event string) bool {
	for _, marker := range []string{"_required", "_fail", "_incomplete", "_rejected", "_blocked"} {
		if strings.Contains(event, marker) {
			return false
		}
	}
	return event != ""
}

func candidateID(spec *transition.TransitionSpec) string {
	if spec == nil {
		return ""
	}
	return spec.ID
}

// automaticCandidatesFor returns the auto-triggered transitions whose
// `from`/`from_phase` matches the supplied cursor. It is the local mirror
// of transition.Catalog.automaticCandidates (private today) — the
// selection must come from the catalog, not from a hard-coded list.
//
// Phase-machine transitions store their `from` as the phase and their owner
// state in the catalog separately; we have to match against both keys
// to find planning.design -> planning.contracts, etc.
func automaticCandidatesFor(catalog *transition.Catalog, cursor transition.Cursor) []transition.TransitionSpec {
	// Human release decisions and terminal cursors are explicit operator
	// surfaces. Even if a stale or malformed catalog accidentally carries an
	// auto-trigger on one of these states, the Hook controller must not turn it
	// into an automatic candidate.
	switch cursor.State {
	case "awaiting_human_release", "release_authorized", "aborted":
		return nil
	}

	var matches []transition.TransitionSpec
	// Top-level transitions match on state; phase is optional and the
	// transition's `from_phase` (if set) must equal the cursor phase.
	for _, spec := range catalog.Transitions {
		if spec.AutoTrigger == nil || !spec.AutoTrigger.Enabled {
			continue
		}
		if spec.From != cursor.State {
			continue
		}
		if spec.FromPhase != "" && spec.FromPhase != cursor.Phase {
			continue
		}
		matches = append(matches, spec)
	}
	// Phase-machine transitions are owned by the cursor's state; the spec's
	// `from` is the phase we are leaving. Match catalog.automaticCandidates:
	// only when the cursor carries an active phase.
	if cursor.State != "" && cursor.Phase != "" {
		if machine, ok := catalog.Definition.PhaseMachines[cursor.State]; ok {
			for _, spec := range machine.Transitions {
				if spec.AutoTrigger == nil || !spec.AutoTrigger.Enabled {
					continue
				}
				if spec.From != cursor.Phase {
					continue
				}
				matches = append(matches, spec)
			}
		}
	}
	return matches
}

// projectGateResult collapses the Evaluator's three-status output into the
// Controller's five-status enum, surfacing the stable error code for any
// non-satisfied path. not_ready / unknown MUST be passed through with
// their original code; only the Evaluator can produce
// LOOP_GATE_UNKNOWN / LOOP_TRIGGER_CONFLICT.
func projectGateResult(eval qualitygate.Evaluation) (ControlStatus, string, []string) {
	switch eval.Status {
	case qualitygate.StatusSatisfied:
		return StatusSatisfied, "", nil
	case qualitygate.StatusNotReady:
		return StatusNotReady, "", nil
	case qualitygate.StatusUnknown:
		code := eval.ErrorCode
		if code == "" {
			code = CodeGateUnknown
		}
		return StatusUnknown, code, eval.Conflicts
	}
	return StatusUnknown, CodeGateUnknown, eval.Conflicts
}

// normalizeAffectedPaths derives a deterministic, non-empty list of
// affected paths from the request. The Hook adapter is the preferred
// source; for plain Edit/Write tools the cycle probes the file_path
// field. For Bash commands without resolved paths the list is empty
// (the final safety policy still runs).
func normalizeAffectedPaths(req ControlRequest) []string {
	if len(req.AffectedPaths) > 0 {
		cleaned := make([]string, 0, len(req.AffectedPaths))
		seen := map[string]struct{}{}
		for _, p := range req.AffectedPaths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			cleaned = append(cleaned, p)
		}
		sort.Strings(cleaned)
		return cleaned
	}
	// Fall back to a single-path probe from tool input.
	if req.ToolInput == nil {
		return nil
	}
	for _, key := range []string{"file_path", "path", "notebook_path"} {
		if v, ok := req.ToolInput[key].(string); ok && v != "" {
			return []string{v}
		}
	}
	return nil
}

// buildSafetyInput projects the controller request onto the minimal
// policy.Input contract. The Controller never re-reads the runtime here;
// it uses the already-committed snapshot revision so the safety verdict
// matches the cursor the agent just observed.
func buildSafetyInput(req ControlRequest, snapshot runtime.Snapshot, affected []string) policy.Input {
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	stateName, _ := lifecycle["state"].(string)
	phaseName, _ := lifecycle["phase"].(string)
	rt := policy.RuntimeContext{
		RuntimeID:    stringValue(snapshot.State["runtime_id"]),
		Revision:     snapshot.Revision,
		ProjectRoot:  req.Root,
		CurrentState: stateName,
		CurrentPhase: stringValue(phaseName),
		Agent:        req.Runtime.Agent,
		// RC-04 (S7-3): preserve the dispatched-Assignment facts so the
		// L4 first-write barrier stays awake on the controller safety path,
		// which evaluates policy without a hookctx-resolved AgentContext.
		AssignmentID:    req.Runtime.AssignmentID,
		PlanReportedRef: req.Runtime.PlanReportedRef,
		DispatchMode:    req.Runtime.DispatchMode,
	}
	if bound, ok := snapshot.State["bound_req"].(map[string]any); ok {
		rt.BoundREQID, _ = bound["id"].(string)
		rt.BoundREQPath, _ = bound["path"].(string)
		if meta, ok := bound["metadata"].(map[string]any); ok {
			rt.BoundREQUIImpact, _ = meta["ui_impact"].(string)
		}
	}
	// The locked-artifact screen was previously only alive in unit tests —
	// the wire path never threaded LockedArtifacts or the stage here, so
	// lockedArtifactDecision never ran (S5 final-round F1). Project them
	// with the same loader the hook transport uses.
	rt.LockedArtifacts = hookctx.LockedArtifactsFromSnapshot(snapshot)
	rt.CurrentStage = stageOf(snapshot.State)
	// The reviewer product-write rule allows the ReviewPlan's declared
	// verification artifact workspace (E2E cold-start spec/fixture surface).
	// Without this projection the wire path hard-denies the one write
	// surface a cold-start E2E Reviewer is supposed to use (L3-S7 §8).
	if reviewState, ok := snapshot.State["review"].(map[string]any); ok {
		if plan, ok := reviewState["plan"].(map[string]any); ok {
			rt.VerificationWorkspace, _ = plan["verification_artifact_workspace"].(string)
		}
	}
	if baseline, ok := snapshot.State["baseline"].(map[string]any); ok {
		rt.CurrentBaselineGeneration = int(baseline["generation"].(float64))
	}
	return policy.Input{
		SessionID: req.SessionID,
		Event:     req.Event,
		AgentID:   req.AgentID,
		ToolName:  req.ToolName,
		ToolInput: req.ToolInput,
		TargetID:  req.TargetID,
		Runtime:   rt,
	}
}

// stageOf reads the milestone projection's stage label.
func stageOf(state map[string]any) string {
	if milestone, ok := state["milestone"].(map[string]any); ok {
		if stage, ok := milestone["stage"].(string); ok {
			return stage
		}
	}
	return ""
}

func allowDecision() policy.Decision {
	return policy.Decision{Decision: "allow", Reason: "no policy rule blocked this action", Retry: "not_applicable"}
}

// controlRuntimePaths resolves the Runtime pair for one control request.
// Root remains the artifact/evidence boundary; only the optional pair is
// redirected. Keeping the defaults here preserves the production path
// contract for every existing caller.
func controlRuntimePaths(req ControlRequest) (string, string) {
	statePath := req.StatePath
	if statePath == "" {
		statePath = filepath.Join(req.Root, ".claude", "loop-state.json")
	}
	journalPath := req.JournalPath
	if journalPath == "" {
		journalPath = filepath.Join(req.Root, ".claude", "loop-events.jsonl")
	}
	return statePath, journalPath
}

func cursorString(state, phase string) string {
	if phase == "" {
		return state
	}
	return state + "." + phase
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	out := make([]string, len(values))
	copy(out, values)
	sort.Strings(out)
	return out
}

func nonEmpty(left, right string) string {
	if left != "" {
		return left
	}
	return right
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func controlRequestID(req ControlRequest) string {
	if req.SessionID != "" {
		return req.SessionID
	}
	if req.TargetID != "" {
		return req.TargetID
	}
	return fmt.Sprintf("control-%s-%s", req.Event, req.ToolName)
}

func snapshotBaselineGeneration(state map[string]any) int {
	baseline, ok := state["baseline"].(map[string]any)
	if !ok {
		return 1
	}
	switch gen := baseline["generation"].(type) {
	case float64:
		if int(gen) >= 1 {
			return int(gen)
		}
	case int:
		if gen >= 1 {
			return gen
		}
	}
	return 1
}

func gateProducerResponsibility(registry *qualitygate.Registry, gateID string) string {
	spec, ok := registry.Lookup(gateID)
	if !ok || len(spec.EvidenceRequirements) == 0 {
		return "Orchestrator"
	}
	if len(spec.EvidenceRequirements[0].Responsibilities) > 0 {
		return spec.EvidenceRequirements[0].Responsibilities[0]
	}
	return "Orchestrator"
}

func autoTransitionRequest(
	req ControlRequest,
	registry *qualitygate.Registry,
	snapshot runtime.Snapshot,
	candidate *transition.TransitionSpec,
	gateID string,
	evaluation qualitygate.Evaluation,
) transition.Request {
	runtimeIdentity, _ := snapshot.State["runtime_id"].(string)
	return transition.Request{
		TransitionID:           candidate.ID,
		ExpectedRevision:       snapshot.Revision,
		ExpectedRuntimeID:      runtimeIdentity,
		Actor:                  candidate.AutoTrigger.Actor,
		Evidence:               buildTransitionEvidence(snapshot, candidate, evaluation),
		AffectedPaths:          normalizeAffectedPaths(req),
		OccurredAt:             time.Now().UTC(),
		RequestID:              controlRequestID(req),
		BaselineGeneration:     snapshotBaselineGeneration(snapshot.State),
		GateID:                 gateID,
		GateFingerprint:        evaluation.Fingerprint,
		ProducerResponsibility: gateProducerResponsibility(registry, gateID),
	}
}

func detectSelectorEvidenceConflict(facts transition.TriggerFacts, evalResults []automaticGateEval) (bool, []string) {
	if len(facts.RequestedEvents) > 1 {
		events := append([]string{}, facts.RequestedEvents...)
		sort.Strings(events)
		return true, events
	}
	for _, item := range evalResults {
		if item.evaluation.ErrorCode == qualitygate.ErrorTriggerConflict {
			conflicts := append([]string{}, item.evaluation.Conflicts...)
			if len(conflicts) == 0 {
				conflicts = append([]string{}, facts.RequestedEvents...)
			}
			sort.Strings(conflicts)
			return true, conflicts
		}
	}
	return false, nil
}

func buildTransitionEvidence(
	snapshot runtime.Snapshot,
	candidate *transition.TransitionSpec,
	evaluation qualitygate.Evaluation,
) map[string]string {
	if candidate == nil || len(candidate.RequiredEvidence) == 0 {
		return map[string]string{}
	}
	index := runtimeEvidenceIndex(snapshot.State)
	out := make(map[string]string, len(candidate.RequiredEvidence))
	used := make(map[string]struct{})
	priority := append([]string{}, evaluation.EvidenceRefs...)
	for id := range index {
		if !containsString(priority, id) {
			priority = append(priority, id)
		}
	}
	currentRound := snapshotReviewRound(snapshot.State)
	catalog := evidence.DefaultCatalog()
	for _, kind := range candidate.RequiredEvidence {
		// Generated evidence is supplied by catalog metadata rather than the
		// concrete slot name, so a newly declared generated slot follows the
		// same Hook path automatically.
		if generator, generated := catalog.Generator(kind); generated {
			if out[kind] == "" {
				out[kind] = generator.Reference
			}
			continue
		}
		if assignTransitionEvidenceRef(kind, priority, index, out, used, currentRound) {
			continue
		}
		assignTransitionEvidencePathAlias(kind, evaluation.EvidenceRefs, index, out, used, currentRound)
	}
	return out
}

func assignTransitionEvidenceRef(
	kind string,
	priority []string,
	index map[string]map[string]any,
	out map[string]string,
	used map[string]struct{},
	currentRound int,
) bool {
	bestID := ""
	bestScore := -1
	for _, id := range priority {
		item, ok := index[id]
		if !ok {
			continue
		}
		if _, taken := used[id]; taken {
			continue
		}
		score := evidenceAssignScore(kind, item, currentRound)
		if score < 0 {
			continue
		}
		if score > bestScore {
			bestScore = score
			bestID = id
		}
	}
	if bestID == "" {
		return false
	}
	out[kind] = bestID
	used[bestID] = struct{}{}
	return true
}

func assignTransitionEvidencePathAlias(
	kind string,
	preferred []string,
	index map[string]map[string]any,
	out map[string]string,
	used map[string]struct{},
	currentRound int,
) {
	if out[kind] != "" {
		return
	}
	bestPath := ""
	bestScore := -1
	for _, id := range preferred {
		item, ok := index[id]
		if !ok {
			continue
		}
		path := stringValue(item["path"])
		if path == "" {
			continue
		}
		if _, taken := used[path]; taken {
			continue
		}
		score := evidenceAssignScore(kind, item, currentRound)
		if score < 0 {
			continue
		}
		if score > bestScore {
			bestScore = score
			bestPath = path
		}
	}
	if bestPath == "" {
		return
	}
	out[kind] = bestPath
	used[bestPath] = struct{}{}
}

// evidenceAssignScore ranks compatible evidence for a required kind.
// Higher is better. Negative means incompatible.
// BUG-039-35: prefer primary short kinds (team_manifest) and current review
// round over alias/stale builder_report when both coexist after TR-006.
func evidenceAssignScore(requiredKind string, item map[string]any, currentRound int) int {
	actualKind := stringValue(item["kind"])
	if !evidenceKindCompatible(requiredKind, actualKind) {
		return -1
	}
	score := 1
	if evidenceKindPreferred(requiredKind, actualKind) {
		score += 10
	}
	if currentRound > 0 {
		switch rr := item["review_round"].(type) {
		case int:
			if rr == currentRound {
				score += 5
			} else if rr > 0 {
				score -= 5
			}
		case float64:
			if int(rr) == currentRound {
				score += 5
			} else if rr > 0 {
				score -= 5
			}
		}
	}
	return score
}

func evidenceKindPreferred(requiredKind, actualKind string) bool {
	return evidence.DefaultCatalog().IsPreferred(requiredKind, actualKind)
}

func snapshotReviewRound(state map[string]any) int {
	review, ok := state["review"].(map[string]any)
	if !ok {
		return 0
	}
	switch v := review["round"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func runtimeEvidenceIndex(state map[string]any) map[string]map[string]any {
	index := make(map[string]map[string]any)
	raw, _ := state["evidence"].([]any)
	for _, item := range raw {
		record, _ := item.(map[string]any)
		if record == nil {
			continue
		}
		if id := stringValue(record["id"]); id != "" {
			index[id] = record
		}
	}
	return index
}

func evidenceKindCompatible(requiredKind, actualKind string) bool {
	return evidence.DefaultCatalog().Accepts(requiredKind, actualKind)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
