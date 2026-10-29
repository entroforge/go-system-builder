package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
)

func TestProductBaselineAndDriftHonorCanceledParent(t *testing.T) {
	root := t.TempDir()
	state := writePinnedProductGatePlan(t, root, Plan{
		SchemaVersion: "1.0.0", ReviewPlanID: "review-plan-legacy", ReviewRound: 1, BaselineGeneration: 1,
	})
	if err := VerifyCurrentProductBaseline(root, state); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := VerifyCurrentProductBaseline(root, state, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("baseline check ignored the parent's canceled budget: %v", err)
	}
	if _, err := detectUndeclaredProductDriftWithState(root, &Plan{}, state, root, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Git drift scan replaced the parent's canceled budget: %v", err)
	}
}

func TestVerifyCurrentProductBaselineDoesNotDowngradePinnedCanonicalPlan(t *testing.T) {
	root := t.TempDir()
	canonicalPlan := Plan{
		SchemaVersion:      "1.0.0",
		ReviewPlanID:       "review-plan-canonical",
		ReviewRound:        1,
		BaselineGeneration: 1,
		ProductBaseline:    &buildsurface.Snapshot{},
	}
	state := writePinnedProductGatePlan(t, root, canonicalPlan)
	state["entities"] = map[string]any{"assignments": []any{}}

	err := VerifyCurrentProductBaseline(root, state)
	if err == nil || !strings.Contains(err.Error(), "no canonical S6 Assignment registry") {
		t.Fatalf("pinned canonical plan without a current registry must fail closed, got %v", err)
	}
}

func TestVerifyCurrentProductBaselineKeepsReadableLegacyPlanBehavior(t *testing.T) {
	root := t.TempDir()
	legacyPlan := Plan{
		SchemaVersion:      "1.0.0",
		ReviewPlanID:       "review-plan-legacy",
		ReviewRound:        1,
		BaselineGeneration: 1,
	}
	state := writePinnedProductGatePlan(t, root, legacyPlan)
	state["entities"] = map[string]any{"assignments": []any{}}

	if err := VerifyCurrentProductBaseline(root, state); err != nil {
		t.Fatalf("readable legacy plan without ProductBaseline should preserve legacy behavior: %v", err)
	}
}

func TestVerifyCurrentProductBaselineRejectsUnreadableExistingPlanPointer(t *testing.T) {
	state := map[string]any{
		"baseline": map[string]any{"generation": 1},
		"review": map[string]any{
			"round": 1,
			"plan": map[string]any{
				"plan_id":      "review-plan-missing",
				"path":         "missing-plan.json",
				"sha256":       strings.Repeat("a", 64),
				"review_round": 1,
				"revision":     1,
				"status":       "clean",
			},
		},
		"entities": map[string]any{"assignments": []any{}},
	}

	err := VerifyCurrentProductBaseline(t.TempDir(), state)
	if err == nil || !strings.Contains(err.Error(), "read ReviewPlan") {
		t.Fatalf("unreadable existing ReviewPlan pointer must fail closed, got %v", err)
	}
}

func writePinnedProductGatePlan(t *testing.T, root string, plan Plan) map[string]any {
	t.Helper()

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal ReviewPlan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "plan.json"), data, 0o600); err != nil {
		t.Fatalf("write ReviewPlan: %v", err)
	}
	sum := sha256.Sum256(data)
	return map[string]any{
		"baseline": map[string]any{"generation": 1},
		"review": map[string]any{
			"round": 1,
			"plan": map[string]any{
				"plan_id":      plan.ReviewPlanID,
				"path":         "plan.json",
				"sha256":       hex.EncodeToString(sum[:]),
				"review_round": 1,
				"revision":     1,
				"status":       "clean",
			},
		},
	}
}
