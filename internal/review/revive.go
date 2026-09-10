package review

import (
	"fmt"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// RevivePlan re-admits a round that staleReviewPlanAfterDrift marked stale
// when the drift was transient — e.g. a control-plane scratch file that
// existed only for the moment of one dual-source scan and has since
// disappeared. Revive re-runs BOTH baseline checks (frozen-subject hashes
// and the undeclared-product-drift scan) against the UNCHANGED plan
// revision; it never touches claims, assignments, dispositions, consumed
// results or the revision number, because nothing in the pinned review
// actually changed. If either check still fails, the plan stays stale and
// the drift error is returned (fail-closed). A stale round caused by real
// product drift cannot be revived — that path is revise (L3-S7 §5.3), which
// re-pins the baseline and invalidates the touched claims' evidence.
func RevivePlan(
	root, statePath, journalPath string,
	expectedRevision int,
) (loopruntime.Snapshot, error) {
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	current, err := store.Snapshot()
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("review-plan revive: snapshot: %w", err)
	}
	ptr := PlanPointerFromState(current.State)
	if ptr == nil {
		return loopruntime.Snapshot{}, fmt.Errorf("review-plan revive: review plan pointer missing")
	}
	if ptr.Status != "stale" {
		return loopruntime.Snapshot{}, fmt.Errorf("review-plan revive: ReviewPlan %s is %s; only a stale round can be revived", ptr.PlanID, ptr.Status)
	}
	plan, _, err := LoadPlan(root, current.State)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("review-plan revive: load plan: %w", err)
	}
	if err := verifyFrozenSubjects(root, plan, current.State); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("review-plan revive: baseline still drifted; the round stays stale (use revise for real drift): %w", err)
	}
	lifecycle, _ := current.State["lifecycle"].(map[string]any)
	cursor := map[string]any{}
	if lifecycle != nil {
		cursor = map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	}
	runtimeID, _ := current.State["runtime_id"].(string)
	commitRevision := currentCommitRevision(-1, current.State)
	snapshot, err := updateRuntime(store, expectedRevision, loopruntime.Mutation{
		EventID:        fmt.Sprintf("evt-review-plan-revive-%s-r%d", ptr.PlanID, commitRevision+1),
		TransitionID:   "REVIEW-PLAN-REVIVE",
		Event:          "review_plan_revived",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:review-plan-revive:%s:%d", ptr.PlanID, commitRevision),
		RuntimeID:      runtimeID,
		From:           cursor,
		To:             cursor,
		Message:        fmt.Sprintf("ReviewPlan %s revived from stale after drift re-check passed (revision %d unchanged; no claim, assignment or result touched)", ptr.PlanID, ptr.Revision),
		OccurredAt:     time.Now().UTC(),
		Apply: func(state map[string]any) error {
			reviewMap, ok := state["review"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime review section must be an object")
			}
			planMap, _ := reviewMap["plan"].(map[string]any)
			if planMap == nil {
				return fmt.Errorf("review plan pointer missing")
			}
			status, _ := planMap["status"].(string)
			if status != "stale" {
				return fmt.Errorf("ReviewPlan %s is %s; only a stale round can be revived", ptr.PlanID, status)
			}
			lifecycleMap, _ := state["lifecycle"].(map[string]any)
			setPlanStatus(reviewMap, lifecycleMap, "running")
			return nil
		},
	})
	if err != nil {
		return snapshot, fmt.Errorf("review-plan revive: %w", err)
	}
	return snapshot, nil
}
