package controller

import (
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/transition"
)

// DefaultQualityCycleBudget is the controller quality-evaluation deadline when
// loop-definition.json omits quality_cycle_timeout (ARCHITECTURE-039 §14.1).
// S5 document gates verify the full locked source closure plus per-reviewer
// envelopes (r99: 31 documents, dual envelopes, full SHA checks), which
// exceeds the original 2s on real projects; 15s covers the observed ~6s
// evaluations with headroom while keeping runaway evaluations bounded.
const DefaultQualityCycleBudget = 15 * time.Second

// ResolveQualityCycleBudget returns the quality-cycle deadline for one
// RunControlCycle call. An explicit request override wins; otherwise the value
// is read from docs/control/loop-definition.json via the loaded catalog; missing or
// invalid values fall back to DefaultQualityCycleBudget.
func ResolveQualityCycleBudget(catalog *transition.Catalog, override time.Duration) time.Duration {
	if override > 0 {
		return override
	}
	if catalog != nil && catalog.Definition != nil {
		if raw := strings.TrimSpace(catalog.Definition.QualityCycleTimeout); raw != "" {
			if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
				return parsed
			}
		}
	}
	return DefaultQualityCycleBudget
}
