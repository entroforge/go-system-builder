package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHookAuditUsesOnlyOriginalCompletionInterval(t *testing.T) {
	for _, mode := range []string{"work expired", "completion expired", "explicit cancellation"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			elapsed := 100 * time.Millisecond
			if mode == "completion expired" {
				elapsed = 2 * time.Second
			}
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-elapsed))
			defer cancel()
			if mode == "explicit cancellation" {
				parent, stop := context.WithCancel(context.Background())
				stop()
				ctx = parent
			}
			err := appendHookAudit(ctx, root, map[string]any{"decision_id": "original-hook-decision"})
			path := filepath.Join(root, ".claude", "hook-decisions.jsonl")
			if mode == "work expired" {
				if err != nil {
					t.Fatalf("received work lost audit in its original completion interval: %v", err)
				}
				if _, err := os.Stat(path); err != nil {
					t.Fatal(err)
				}
				return
			}
			want := context.DeadlineExceeded
			if mode == "explicit cancellation" {
				want = context.Canceled
			}
			if !errors.Is(err, want) {
				t.Fatalf("audit renewed/detached original deadline: %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("audit appended after original completion/cancellation: %v", err)
			}
		})
	}
}
