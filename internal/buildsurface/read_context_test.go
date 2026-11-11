package buildsurface

import (
	"context"
	"errors"
	"testing"
)

func TestCurrentReadPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(WithReadCache(context.Background()))
	cancel()
	if _, err := ReadContext(ctx, t.TempDir(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadContext lost cancellation: %v", err)
	}
	if err := VerifyContext(ctx, t.TempDir(), nil, Snapshot{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifyContext lost cancellation: %v", err)
	}
}
