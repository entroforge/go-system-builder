package repair

import (
	"context"
	"errors"
	"testing"
)

func TestS7HandoffSeedHonorsOriginalRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := createS7ReviewPlanSeedForEntry(t.TempDir(), "review-plan", 1, ChangeImpact{}, ContractRef{}, nil, "", "", ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("S9 successor seed restarted with Background: %v", err)
	}
}
