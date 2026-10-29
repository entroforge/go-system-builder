package review

import (
	"context"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
)

// VerifyCurrentProductBaseline rechecks the current pinned product authority
// when a persisted CleanRound is consumed. Legacy plans keep their original
// gate rules; canonical plans cannot rely on an old state-only clean label.
func VerifyCurrentProductBaseline(root string, state map[string]any, contexts ...context.Context) (err error) {
	ctx := baselineContext(contexts)
	defer func() {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	if len(contexts) > 0 && contexts[0] != nil {
		if err := contexts[0].Err(); err != nil {
			return err
		}
	}
	if !buildsurface.HasCanonical(state) {
		// A pinned Plan that declares a ProductBaseline is positive evidence
		// that this round was authored against canonical S6 authority. Do not
		// silently downgrade it to legacy behavior if the current Assignment
		// registry is missing or belongs to another generation. Legacy plans
		// still keep their state-only gate behavior, but an existing pointer
		// must be readable before we can establish that it is legacy.
		reviewState, _ := state["review"].(map[string]any)
		rawPointer, exists := reviewState["plan"]
		if !exists || rawPointer == nil {
			return nil
		}
		if PlanPointerFromState(state) == nil {
			return fmt.Errorf("ReviewPlan pointer is present but invalid")
		}
		plan, _, err := LoadPlan(root, state)
		if err != nil {
			return fmt.Errorf("load existing ReviewPlan before legacy baseline fallback: %w", err)
		}
		if plan.ProductBaseline == nil {
			return nil
		}
		return verifyProductBaselineDeclarationContext(baselineContext(contexts), root, state, plan)
	}
	plan, ptr, err := LoadPlan(root, state)
	if err != nil {
		return err
	}
	if ptr.ReviewRound != currentReviewRound(state) || plan.ReviewRound != ptr.ReviewRound || plan.BaselineGeneration != baselineGeneration(state) {
		return fmt.Errorf("CleanRound Plan is not the current round/generation")
	}
	return verifyFrozenSubjects(root, plan, state, contexts...)
}

// FrozenSubjectsForProductBaseline is the existing pure machine projection,
// shared with the original S9 seed writer. It grants no source authority.
func FrozenSubjectsForProductBaseline(snapshot *buildsurface.Snapshot) ([]FrozenSubject, error) {
	return frozenSubjectsForProductBaseline(snapshot)
}
