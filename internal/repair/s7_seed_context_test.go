package repair

import (
	"context"
	"errors"
	"testing"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
)

func TestS7HandoffSeedHonorsOriginalRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := createS7ReviewPlanSeed(t.TempDir(), 2, 1, ChangeImpact{}, ContractRef{}, nil, "", nil, &buildsurface.Snapshot{}, ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("S9 successor seed restarted with Background: %v", err)
	}
}
