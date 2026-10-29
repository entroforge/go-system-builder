package mainreceipt

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestRequestGitCacheKeepsMainReceiptEvidenceLive(t *testing.T) {
	f := newReaderFixture(t)
	ctx := WithReadCache(context.Background())
	if _, err := Verify(ctx, f.input); err != nil {
		t.Fatal(err)
	}
	cache := ctx.Value(readCacheKey{}).(*immutableReads)
	if len(cache.data) == 0 {
		t.Fatal("Main fixed Git proof was not cached")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Verify(canceled, f.input); !errors.Is(err, context.Canceled) {
		t.Fatalf("Main proof cache hid cancellation: %v", err)
	}
	if err := os.WriteFile(f.log, []byte("tampered after successful proof\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(ctx, f.input); err == nil {
		t.Fatal("cached fixed Git proof waived current disk evidence verification")
	}
}
