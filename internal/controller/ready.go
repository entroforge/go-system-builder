package controller

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

const readyInstruction = "produce listed missing work; do not call transition CLI; next PreToolUse auto-advances when satisfied"

// ReadyReport is the read-only diagnostics projection for `loop-harness ready`.
// It dry-runs the current cursor's Quality Gate using the same candidate
// selection and evaluator as RunControlCycle, but never calls transition.Apply.
type ReadyReport struct {
	Cursor              string   `json:"cursor"`
	Stage               string   `json:"stage"`
	GateID              string   `json:"gate_id"`
	Status              string   `json:"status"`
	Missing             []string `json:"missing"`
	CandidateTransition string   `json:"candidate_transition"`
	Instruction         string   `json:"instruction"`
	HumanRequired       bool     `json:"human_required"`
	ObservedRevision    int      `json:"observed_revision"`
	ErrorCode           string   `json:"error_code,omitempty"`
	Conflicts           []string `json:"conflicts,omitempty"`
}

// EvaluateReady dry-runs the active Quality Gate at the current Runtime cursor.
// It never mutates Runtime / Journal.
func EvaluateReady(ctx context.Context, root string) (ReadyReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	store := runtime.NewStore(statePath, journalPath)
	snapshot, err := store.Snapshot()
	if err != nil {
		return ReadyReport{}, fmt.Errorf("read runtime: %w", err)
	}

	cursorState, cursorPhase := snapshotCursor(snapshot.State)
	cursor := transition.Cursor{State: cursorState, Phase: cursorPhase}
	stage, _ := runtime.StageFor(cursorState, cursorPhase, root)

	report := ReadyReport{
		Cursor:           cursorString(cursorState, cursorPhase),
		Stage:            stage,
		Missing:          []string{},
		Instruction:      readyInstruction,
		ObservedRevision: snapshot.Revision,
	}

	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		return report, fmt.Errorf("load catalog: %w", err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		return report, fmt.Errorf("build gate registry: %w", err)
	}
	evaluator := qualitygate.NewEvaluator(registry)

	candidates := automaticCandidatesFor(catalog, cursor)
	if len(candidates) == 0 {
		report.Status = string(StatusSatisfied)
		report.Missing = []string{}
		return report, nil
	}

	req := ControlRequest{Root: root, Event: "PreToolUse"}
	files := diskFiles{root: root}
	budget := ResolveQualityCycleBudget(catalog, 0)
	evalResults, timedOut := evaluateAutomaticGates(ctx, budget, evaluator, req, snapshot, candidates, nil, files)
	if timedOut {
		report.Status = string(StatusUnknown)
		report.ErrorCode = CodeGateUnknown
		return report, nil
	}

	facts := buildTriggerFacts(evaluator, req, snapshot, candidates, evalResults, nil, files)
	if conflict, conflictIDs := detectSelectorEvidenceConflict(facts, evalResults); conflict {
		report.Status = string(StatusUnknown)
		report.ErrorCode = CodeTriggerConfl
		report.Conflicts = conflictIDs
		report.Missing = mergeCandidateMissing(evalResults)
		return report, nil
	}

	resolution, err := catalog.ResolveAutomaticTransition(cursor, facts)
	if err != nil {
		var conflict *transition.TriggerConflictError
		if asConflict(err, &conflict) {
			report.Status = string(StatusUnknown)
			report.ErrorCode = CodeTriggerConfl
			report.Conflicts = append([]string{}, conflict.CandidateIDs...)
			report.Missing = mergeCandidateMissing(evalResults)
			return report, nil
		}
		return report, fmt.Errorf("resolve automatic transition: %w", err)
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
	report.Status = string(gateStatus)
	report.GateID = gateID
	report.Missing = nonNilStrings(evaluation.Missing)
	report.ErrorCode = gateError
	report.Conflicts = nonNilStrings(conflicts)
	report.HumanRequired = candidate != nil && candidate.AutoTrigger != nil && candidate.AutoTrigger.HumanRequired
	if candidate != nil {
		report.CandidateTransition = candidate.ID
	}
	return report, nil
}

func mergeCandidateMissing(results []automaticGateEval) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, item := range results {
		for _, m := range item.evaluation.Missing {
			if m == "" {
				continue
			}
			if _, ok := seen[m]; ok {
				continue
			}
			seen[m] = struct{}{}
			out = append(out, m)
		}
	}
	return out
}

func asConflict(err error, target **transition.TriggerConflictError) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*transition.TriggerConflictError); ok {
		*target = e
		return true
	}
	return false
}
