package integration

import (
	"fmt"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"strings"
)

// AssignmentInspectConfig preserves historical manifests. New manifests opt
// into static inspection plus mandatory post-merge checks; no active manifest
// is rewritten during upgrade. Locked hints remain enforced in either mode.
func AssignmentInspectConfig(a hookctx.AssignmentContext) (InspectConfig, error) {
	cfg := InspectConfig{}
	commands, locked := splitRequiredChecks(a.RequiredChecks)
	cfg.LockedArtifacts = locked
	switch a.IntegrationCheckMode {
	case "", "legacy_pre_and_post_merge":
		cfg.RequiredChecks = commands
	case "post_merge":
	default:
		return cfg, fmt.Errorf("unknown integration_check_mode %q", a.IntegrationCheckMode)
	}
	return cfg, nil
}

// A legacy locked: entry is metadata, never executable shell text. Both the
// inspection and post-merge paths consume this same classification.
func splitRequiredChecks(checks []string) (commands, locked []string) {
	for _, check := range checks {
		if strings.HasPrefix(check, "locked:") {
			locked = append(locked, strings.TrimPrefix(check, "locked:"))
		} else {
			commands = append(commands, check)
		}
	}
	return commands, locked
}
