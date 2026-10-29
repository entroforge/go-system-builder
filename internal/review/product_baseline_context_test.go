package review

import (
	"context"
	"errors"
	"testing"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestBaselineConsumersPreserveRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Even legacy/no-baseline and empty-proof fast paths cannot grant a success
	// after cancellation; callers use this signal to avoid starting a Writer.
	checks := map[string]func() error{
		"issued packet source": func() error {
			return verifyCurrentResultPacketContext(ctx, "", nil, workpacket.ReviewResultPacket{}, true)
		},
		"declaration":           func() error { return verifyProductBaselineDeclarationContext(ctx, "", nil, nil) },
		"coverage":              func() error { return validateCoverageInventory("", nil, nil, ctx) },
		"verified coverage":     func() error { return validateVerifiedCoverageInventory("", nil, nil, ctx) },
		"workspace preparation": func() error { _, err := prepareVerificationWorkspace("", nil, ctx); return err },
		"projection":            func() error { return validateS7BaselineProjection("", nil, ctx) },
		"capture":               func() error { return validateCapturePassProvenance("", nil, nil, nil, nil, nil, ctx) },
		"registration artifact": func() error { return ValidatePlanArtifactForRegistration("", nil, nil, ctx) },
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			if err := check(); !errors.Is(err, context.Canceled) {
				t.Fatalf("request cancellation was lost: %v", err)
			}
		})
	}
	if snapshot, err := readCanonicalProductBaselineContext(ctx, "", nil); snapshot != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canonical fast path lost cancellation: %v %v", snapshot, err)
	}
	if paths, diagnostics := ChangedPathsForRootDetailed("", nil, ctx); len(paths) != 0 || len(diagnostics) == 0 {
		t.Fatalf("canceled projection was reported as an empty baseline: %v %v", paths, diagnostics)
	}
}

// Cancel after helper entry, when the strict canonical verifier starts, so
// gate formatting cannot mislabel an interruption as invalid authored content.
type cancelDuringProductBaseline struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *cancelDuringProductBaseline) Err() error {
	c.checks++
	if c.checks == 3 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestBaselineGatePreservesCancellationDuringCanonicalVerification(t *testing.T) {
	state := map[string]any{"runtime_id": "loop-test", "baseline": map[string]any{"generation": 1}, "entities": map[string]any{"assignments": []any{map[string]any{"runtime_id": "loop-test", "baseline_generation": 1}}}}
	for _, gate := range []string{"coverage", "projection"} {
		t.Run(gate, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &cancelDuringProductBaseline{Context: parent, cancel: cancel}
			var err error
			if gate == "coverage" {
				err = validateCoverageInventory(t.TempDir(), state, &Plan{ProductBaseline: &buildsurface.Snapshot{Digest: "fixture"}}, ctx)
			} else {
				err = validateS7BaselineProjection(t.TempDir(), state, ctx)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("mid-verification cancellation became content refusal: %v (checks %d)", err, ctx.checks)
			}
		})
	}
}
