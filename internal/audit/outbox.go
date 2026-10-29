package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
)

type Outbox struct{ path string }

func NewOutbox(path string) *Outbox { return &Outbox{path: path} }

func (o *Outbox) Append(record any) error { return o.AppendContext(context.Background(), record) }

// AppendContext shares the request's cancellation and deadline with the original
// cross-process lock. The log is authoritative; its index is a rebuildable cache.
func (o *Outbox) AppendContext(parent context.Context, record any) error {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode audit record: %w", err)
	}
	var identity struct {
		DecisionID string `json:"decision_id"`
	}
	if err := json.Unmarshal(data, &identity); err != nil {
		return fmt.Errorf("decode audit identity: %w", err)
	}
	if identity.DecisionID == "" {
		return errors.New("audit record requires decision_id")
	}
	if err := os.MkdirAll(filepath.Dir(o.path), 0755); err != nil {
		return fmt.Errorf("create audit directory: %w", err)
	}
	release, err := acquireLockContext(ctx, o.path+".lock", 30*time.Second)
	if err != nil {
		return err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.OpenFile(o.path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("open audit outbox: %w", err)
	}
	defer file.Close()
	index, contentHash, err := loadDecisionIndex(ctx, o.path, file)
	if err != nil {
		return err
	}
	if _, found := index.Decisions[identity.DecisionID]; found {
		if err := ctx.Err(); err != nil {
			return err
		}
		// A cold/corrupt/stale index is repaired even on a deduplicated retry.
		if index.dirty {
			return writeDecisionIndex(ctx, o.path, index)
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	line := append(data, '\n')
	n, err := file.Write(line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return fmt.Errorf("write audit outbox: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync audit outbox after append: %w", err)
	}
	// Once the event is durable, cancellation/index failure cannot undo it.
	// A retry reconstructs from this log and never appends a duplicate.
	_, _ = contentHash.Write(line)
	index.Bytes += int64(len(line))
	index.SHA256 = digestString(contentHash)
	index.Decisions[identity.DecisionID] = true
	if err := writeDecisionIndex(ctx, o.path, index); err != nil {
		return fmt.Errorf("audit record %s appended; decision index update incomplete: %w", identity.DecisionID, err)
	}
	return nil
}

func containsDecision(path, decisionID string) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	decisions, err := readDecisions(context.Background(), file)
	return decisions[decisionID], err
}

// The lock inode is persistent; the OS releases ownership on process exit.
// Legacy callers retain their local cap; bounded callers inherit the parent.
func acquireLock(path string, timeout time.Duration) (func(), error) {
	return acquireLockContext(context.Background(), path, timeout)
}
func acquireLockContext(parent context.Context, path string, timeout time.Duration) (func(), error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	release, err := filelock.Acquire(ctx, path+".process")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("audit outbox lock timeout: %w", err)
		}
		return nil, err
	}
	return release, nil
}
