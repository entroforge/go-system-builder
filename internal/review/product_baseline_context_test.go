package review

import (
	"context"
	"errors"
	"testing"

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
