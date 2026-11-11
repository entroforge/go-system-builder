package mainreceipt

import (
	"context"
	"errors"
	"testing"
)

func TestUnfinishedIntegrationDoesNotBecomeVerified(t *testing.T) {
	f := newReaderFixture(t)
	f.checkpoint.State = "preserved"
	f.checkpoint.VerifiedAt = ""
	f.checkpoint.CanonicalMainFacts = nil
	writeCheckpointFixture(t, f, f.checkpoint)
	_, err := Verify(context.Background(), f.input)
	if !errors.Is(err, ErrNotVerified) {
		t.Fatalf("unfinished integration = %v, want unverified", err)
	}
}
