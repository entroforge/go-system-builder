// Package investigation — RC-14 evidence attestation wrapper.
//
// ValidateEvidenceRefs is the S8/S9/S10-facing entry point that re-exports
// the unified evidence semantic gate (internal/evidence.ValidateRefs) and
// adds the RC-14 specific knobs the plan called out:
//
//   - RequireSHA         when true (and Root is non-empty), every ref must
//     pass the on-disk SHA256 verification of the
//     referenced evidence artifact.
//   - RequireKind        whitelist of accepted evidence kinds.
//   - RequireReviewRound, when > 0, every ref must carry that exact
//     review_round value.
//   - RequireExecution, when true, only execution anchors (refs containing
//     "://") are accepted — runtime evidence ids are
//     rejected. This is the inverse of the default and
//     is used by S9 pass / plan_report red-check content
//     gates.
//
// The wrapper sits in package investigation so case_workflow and the S9
// TargetedReverification validation can call it without importing
// internal/evidence directly. internal/evidence remains the source of
// truth for the index-driven checks (status / generation / SHA drift);
// this file only adds a thin policy layer.
package investigation

import (
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/evidence"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

// EvidenceAttestationOptions configures ValidateEvidenceRefs.
//
// State is the runtime evidence index map (state["evidence"]). When nil or
// empty, the validator skips index-driven checks (no kind / no round
// verification) — useful for artifact-boundary callers that operate on a
// stored envelope without the live Runtime state.
type EvidenceAttestationOptions struct {
	State              map[string]any
	Root               string
	RequireSHA         bool
	RequireKind        []string
	RequireReviewRound int
	RequireExecution   bool
	AllowPaths         bool
	ForbidSelfID       string
}

// ValidateEvidenceRefs runs the RC-14 unified evidence attestation over refs.
// It is the single entry point for hypothesis/result, TargetedReverification,
// and coverage inventory validation. Under the hood it delegates to
// evidence.ValidateRefs (which performs the index, generation, kind, and
// on-disk SHA checks) and adds the RC-14-specific RequireExecution knob
// required by S9 pass and plan_report red-check content gates.
//
// When opts.RequireExecution is true the function accepts only execution
// anchors (refs containing "://"). Runtime evidence ids are rejected even
// when the index entry exists, because the gate requires an external
// execution anchor at this boundary.
func ValidateEvidenceRefs(refs []string, opts EvidenceAttestationOptions) error {
	if opts.RequireExecution {
		for _, ref := range refs {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				return fmt.Errorf("evidence_ref is empty")
			}
			if strings.HasPrefix(ref, "runtime:") || !strings.Contains(ref, "://") {
				return fmt.Errorf("evidence_ref %q is not an execution anchor (expected a scheme:// form)", ref)
			}
		}
		return nil
	}
	// Default path: delegate to the shared evidence catalog gate. SHA
	// verification is gated by opts.Root (mirroring evidence.ValidateRefs);
	// when RequireSHA is requested but no Root is supplied, we return an
	// explicit error rather than silently downgrading to a metadata check.
	if opts.RequireSHA && strings.TrimSpace(opts.Root) == "" {
		return fmt.Errorf("ValidateEvidenceRefs RequireSHA is set but no Root is supplied; provide a repository root to enable on-disk SHA verification")
	}
	if opts.AllowPaths {
		remaining := make([]string, 0, len(refs))
		for _, ref := range refs {
			if strings.HasPrefix(ref, "path:") {
				if len(opts.RequireKind) > 0 {
					return fmt.Errorf("path evidence cannot satisfy indexed kind constraints")
				}
				if err := validateS8CapturedPathScope(ref, opts); err != nil {
					return err
				}
				if err := evidence.ValidateFindingReference(opts.Root, ref); err != nil {
					return err
				}
			} else {
				remaining = append(remaining, ref)
			}
		}
		if len(remaining) == 0 && len(refs) > 0 {
			return nil
		}
		refs = remaining
	}
	return evidence.ValidateRefs(opts.State, refs, evidence.RefsOptions{
		Root:               opts.Root,
		RequireKinds:       opts.RequireKind,
		RequireReviewRound: opts.RequireReviewRound,
		ForbidSelfID:       opts.ForbidSelfID,
	})
}

func validateS8CapturedPathScope(ref string, opts EvidenceAttestationOptions) error {
	source, _, err := evidence.ParseFindingReference(ref)
	if err != nil {
		return err
	}
	prefix := ".claude/evidence/s8/"
	if !strings.HasPrefix(source.Path, prefix) {
		return fmt.Errorf("new S8 path evidence must use its original captured source")
	}
	packetID := strings.Split(strings.TrimPrefix(source.Path, prefix), "/")[0]
	packets, err := loopruntime.HypothesisResultPackets(opts.State)
	if err != nil {
		return err
	}
	for _, packet := range packets {
		if packet.ID != packetID {
			continue
		}
		if packet.Binding.RuntimeID != stringField(opts.State["runtime_id"]) || packet.Binding.BaselineGeneration != baselineGenerationValue(opts.State) || (opts.RequireReviewRound > 0 && packet.Binding.ReviewRound != opts.RequireReviewRound) {
			break
		}
		return nil
	}
	return fmt.Errorf("S8 captured path does not belong to the current Runtime generation/round")
}
