package buildsurface

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// ReadRegistered verifies the original pinned ReviewPlan's machine baseline.
// It does not infer CleanRound or any professional Claim disposition.
func ReadRegistered(root string, state map[string]any) (Snapshot, error) {
	return ReadRegisteredContext(context.Background(), root, state)
}

func ReadRegisteredContext(ctx context.Context, root string, state map[string]any) (result Snapshot, readErr error) {
	ctx = WithReadCache(ctx)
	defer func() {
		if ctx.Err() != nil {
			result, readErr = Snapshot{}, ctx.Err()
		}
	}()

	var zero Snapshot
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !HasCanonical(state) {
		return zero, fmt.Errorf("canonical completion cannot prove original Main facts without the current Assignment registry")
	}
	ptr := reviewmeta.FromState(state)
	if ptr == nil {
		return zero, fmt.Errorf("canonical S6 requires the original pinned ReviewPlan")
	}
	data, err := workpacket.ReadFile(root, ptr.Path, 20<<20)
	if err != nil || hashBytes(data) != ptr.SHA256 {
		return zero, fmt.Errorf("original pinned ReviewPlan is missing or changed")
	}
	if err := schema.NewValidator(root).ValidateBytes("review-plan.schema.json", data); err != nil {
		return zero, fmt.Errorf("original pinned ReviewPlan shape: %w", err)
	}
	var plan struct {
		ID         string                     `json:"review_plan_id"`
		Round      int                        `json:"review_round"`
		Generation int                        `json:"baseline_generation"`
		Baseline   *Snapshot                  `json:"product_baseline"`
		Frozen     []reviewmeta.FrozenSubject `json:"frozen_subjects"`
	}
	if err := json.Unmarshal(data, &plan); err != nil {
		return zero, err
	}
	review, _ := state["review"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	if plan.Baseline == nil || plan.ID != ptr.PlanID || plan.Round != number(review["round"]) || ptr.ReviewRound != plan.Round || plan.Generation != number(baseline["generation"]) {
		return zero, fmt.Errorf("pinned ReviewPlan lacks the exact current round product baseline")
	}
	if err := VerifyContext(ctx, root, state, *plan.Baseline); err != nil {
		return zero, err
	}
	subjects, err := FrozenSubjects(plan.Baseline)
	if err != nil {
		return zero, err
	}
	expected := make(map[string][2]string, len(subjects))
	for _, subject := range subjects {
		expected[subject.Path] = [2]string{subject.SHA256, subject.Kind}
	}
	if len(plan.Frozen) != len(expected) {
		return zero, fmt.Errorf("pinned ReviewPlan does not freeze the complete product path set")
	}
	seen := map[string]bool{}
	for _, source := range plan.Frozen {
		want, found := expected[source.Path]
		if !found || seen[source.Path] || want != [2]string{source.SHA256, source.Kind} {
			return zero, fmt.Errorf("pinned ReviewPlan frozen source differs from the machine baseline")
		}
		seen[source.Path] = true
	}
	return *plan.Baseline, nil
}
