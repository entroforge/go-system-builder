package review

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
)

// readCanonicalProductBaseline chooses the strict shared S6 projection before
// any legacy completion-report projection. A malformed canonical registry is
// an error from Read, never a reason to fall back to legacy evidence.
func readCanonicalProductBaseline(root string, state map[string]any) (*buildsurface.Snapshot, error) {
	return readCanonicalProductBaselineContext(context.Background(), root, state)
}

func readCanonicalProductBaselineContext(ctx context.Context, root string, state map[string]any) (*buildsurface.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !buildsurface.HasCanonical(state) {
		return nil, nil
	}
	snapshot, err := buildsurface.ReadContext(ctx, root, state)
	if err != nil {
		return nil, fmt.Errorf("canonical S6 product baseline: %w", err)
	}
	return &snapshot, nil
}

// frozenSubjectsForProductBaseline uses the shared machine projection consumed
// by registration and the original registered-baseline reader.
func frozenSubjectsForProductBaseline(snapshot *buildsurface.Snapshot) ([]FrozenSubject, error) {
	return buildsurface.FrozenSubjects(snapshot)
}

func verifyProductBaselineDeclaration(root string, state map[string]any, plan *Plan) error {
	return verifyProductBaselineDeclarationContext(context.Background(), root, state, plan)
}

func verifyProductBaselineDeclarationContext(ctx context.Context, root string, state map[string]any, plan *Plan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan == nil {
		return fmt.Errorf("product baseline verification requires a ReviewPlan")
	}
	canonical := buildsurface.HasCanonical(state)
	if !canonical {
		if plan.ProductBaseline != nil {
			return fmt.Errorf("ReviewPlan declares a canonical product baseline but Runtime has no canonical S6 Assignment registry")
		}
		return nil
	}
	if plan.ProductBaseline == nil {
		return fmt.Errorf("canonical S6 generation requires ReviewPlan.product_baseline")
	}
	if err := buildsurface.VerifyContext(ctx, root, state, *plan.ProductBaseline); err != nil {
		return fmt.Errorf("ReviewPlan product baseline is stale or invalid: %w", err)
	}
	want, err := frozenSubjectsForProductBaseline(plan.ProductBaseline)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(plan.FrozenSubjects, want) {
		return fmt.Errorf("ReviewPlan frozen_subjects do not exactly match canonical TASK sources and changed paths")
	}
	return ctx.Err()
}

// verifyCanonicalFrozenSubjectFiles complements the Git-side product baseline
// proof with the live worktree check. Deleted paths must remain absent; their
// preimage hash is verified from the ProductBaseline's pinned Git object.
func verifyCanonicalFrozenSubjectFiles(root string, plan *Plan, contexts ...context.Context) error {
	ctx := baselineContext(contexts)
	for _, subject := range plan.FrozenSubjects {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := repositoryContainedPath(root, subject.Path)
		if err != nil {
			return fmt.Errorf("frozen subject %s is outside repository: %w", subject.Path, err)
		}
		if subject.Kind == "deleted" {
			if _, statErr := os.Lstat(path); statErr == nil {
				return fmt.Errorf("frozen deleted subject %s reappeared on disk; restore its absence before registration", subject.Path)
			} else if !os.IsNotExist(statErr) {
				return fmt.Errorf("frozen deleted subject %s cannot be checked: %w", subject.Path, statErr)
			}
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("frozen subject %s is unreadable: %w", subject.Path, err)
		}
		if actual := sha256Of(data); actual != subject.SHA256 {
			return fmt.Errorf("frozen subject %s drifted: plan pins %s but disk contains %s", subject.Path, subject.SHA256, actual)
		}
	}
	return nil
}

// baselineContext chooses the request budget for legacy-compatible helper APIs.
func baselineContext(contexts []context.Context) context.Context {
	if len(contexts) > 0 && contexts[0] != nil {
		return contexts[0]
	}
	return context.Background()
}
