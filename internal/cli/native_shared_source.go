package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Only an observed cwd can establish another checkout. Missing or non-Git cwd
// grants no new source binding and leaves the original consumer responsible.
func nativeSharedCheckoutSourceError(ctx context.Context, root, source, cwd, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cwd == "" {
		return nil
	}
	main, mainErr := workspace.Canonical(root)
	bound, boundErr := workspace.Canonical(source)
	actual, actualErr := workspace.Canonical(cwd)
	if mainErr != nil || boundErr != nil || actualErr != nil || bound != main {
		return ctx.Err()
	}
	top, err := workspace.Git(ctx, actual, "rev-parse", "--show-toplevel")
	if err != nil {
		return ctx.Err()
	}
	top, err = workspace.Canonical(top)
	rel, relErr := filepath.Rel(top, actual)
	if err != nil || relErr != nil || !filepath.IsLocal(rel) || top == main {
		return ctx.Err()
	}
	mainCommon, mainErr := workspace.Git(ctx, main, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if mainErr != nil {
		return ctx.Err()
	}
	actualCommon, actualErr := workspace.Git(ctx, top, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if actualErr != nil {
		return ctx.Err()
	}
	mainCommon, mainErr = workspace.Canonical(mainCommon)
	actualCommon, actualErr = workspace.Canonical(actualCommon)
	if err := ctx.Err(); err != nil {
		return err
	}
	if mainErr == nil && actualErr == nil && mainCommon == actualCommon {
		return fmt.Errorf("%s shared-checkout original has no source binding for actual native worktree %s (Stop cwd %s); Main answer cannot substitute; no answer was inspected or received", label, top, actual)
	}
	return nil
}
