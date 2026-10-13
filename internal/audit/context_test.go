package audit_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/audit"
	"github.com/entroforge/go-system-builder/internal/filelock"
)

func TestOutboxUsesRemainingParentBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.jsonl")
	release, err := filelock.Acquire(context.Background(), path+".lock.process")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = audit.NewOutbox(path).AppendContext(ctx, map[string]any{"decision_id": "hook-decision-timeout"})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("unbounded outbox: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("appended after budget expired")
	}
}
