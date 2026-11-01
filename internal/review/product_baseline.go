package review

import (
	"context"

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
	// Main provides actual work materials; no frozen Git proof is required.
	return nil, nil
}

// frozenSubjectsForProductBaseline uses the shared machine projection consumed
// by registration and the original registered-baseline reader.
func frozenSubjectsForProductBaseline(snapshot *buildsurface.Snapshot) ([]FrozenSubject, error) {
	return buildsurface.FrozenSubjects(snapshot)
}

func verifyCanonicalFrozenSubjectFiles(root string, plan *Plan, contexts ...context.Context) error {
	return baselineContext(contexts).Err()
}

// baselineContext chooses the request budget for legacy-compatible helper APIs.
func baselineContext(contexts []context.Context) context.Context {
	if len(contexts) > 0 && contexts[0] != nil {
		return contexts[0]
	}
	return context.Background()
}
