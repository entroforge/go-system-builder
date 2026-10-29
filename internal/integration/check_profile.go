package integration

import (
	"context"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

// ProfileCheckRunner retains every original required command and Main/fresh
// execution. It applies only the same registered command's explicit profile.
func ProfileCheckRunner(profiles []checkexec.Profile, binding checkexec.Binding) RequiredCheckRunner {
	return func(ctx context.Context, root, command string) error {
		for _, p := range profiles {
			if p.Command == command {
				ctx = checkexec.WithProfile(ctx, p)
				ctx = checkexec.WithBinding(ctx, binding)
				break
			}
		}
		return CommandCheckRunner(ctx, root, command)
	}
}
