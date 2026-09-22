package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
)

type Outbox struct {
	path string
}

func NewOutbox(path string) *Outbox {
	return &Outbox{path: path}
}

func (o *Outbox) Append(record any) error {
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

	if err := os.MkdirAll(filepath.Dir(o.path), 0o755); err != nil {
		return fmt.Errorf("create audit directory: %w", err)
	}
	release, err := acquireLock(o.path+".lock", 30*time.Second)
	if err != nil {
		return err
	}
	defer release()

	found, err := containsDecision(o.path, identity.DecisionID)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	file, err := os.OpenFile(o.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open audit outbox: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write audit outbox: %w", err)
	}
	return file.Sync()
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
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var item struct {
			DecisionID string `json:"decision_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return false, fmt.Errorf("decode audit outbox: %w", err)
		}
		if item.DecisionID == decisionID {
			return true, nil
		}
	}
	return false, scanner.Err()
}

// acquireLock serializes cross-process outbox writes with a process-owned OS
// lock. The lock inode is intentionally persistent: unlinking a locked file
// can let another process lock a different inode at the same path. A separate
// .process file also prevents stale O_EXCL sentinels left by older binaries
// from blocking current hooks. The operating system releases the lock when a
// holder exits, including crash and forced-termination paths.
func acquireLock(path string, timeout time.Duration) (func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
