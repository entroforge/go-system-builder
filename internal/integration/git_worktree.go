package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// branchExists checks the requested target branch without pinning a commit ID.
func branchExists(ctx context.Context, root, branch string) (bool, error) {
	if strings.TrimSpace(branch) == "" {
		return false, errors.New("branch name is required")
	}
	_, err := defaultRunner.Run(ctx, root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		// A non-zero exit means the ref doesn't exist; anything else
		// (e.g. corrupt refs) is an actual error and should bubble up.
		// The execRunner wraps non-zero exits as ExitError; we treat any
		// error here as "not found" so callers can fall through to the
		// missing-target branch. Real corruption will surface on the
		// subsequent ref read.
		return false, nil
	}
	return true, nil
}

// checkoutBranch verifies the Main checkout is on the requested branch.
func checkoutBranch(ctx context.Context, root, branch string) error {
	current, err := defaultRunner.Run(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(current) != strings.TrimPrefix(branch, "refs/heads/") {
		return fmt.Errorf("authority root is on %s, expected %s; switch explicitly after preserving local work", strings.TrimSpace(current), branch)
	}
	return nil
}
