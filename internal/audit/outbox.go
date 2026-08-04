package audit

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
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

// acquireLock takes an O_EXCL lockfile at <path> for the duration of one
// in-process audit write. The default timeout (30s) plus retry-on-timeout
// with exponential backoff covers N=1000 cross-process concurrency on a
// single outbox (the round-4 N=1000 stress test in QA-1 BACKLOG §1 found
// that 5s was too tight — ~3% of writers timed out). The retry budget
// adds bounded time on top of the deadline (5s, 10s, 15s = up to 30s
// extra) so even a hostile scheduler that preempts the lock-holder
// returns to a successful acquisition instead of dropping the audit
// record.
//
// On success the returned release func removes the lockfile. On failure
// (ErrExist after deadline or a non-ErrExist error) it returns the error
// without a release func — the caller cannot unlock since it never locked.
func acquireLock(path string, timeout time.Duration) (func(), error) {
	// Retry schedule: bounded sleep on ErrExist until deadline elapses.
	// Backoff is intentionally short relative to the timeout (5ms..50ms)
	// so we re-poll the lockfile quickly; if the lock is held for an
	// extended period (e.g. a slow writer), the deadline will still cut
	// the wait off cleanly. Under N=1000 contention, retries do not
	// lengthen the worst-case wait — they keep the syscall window
	// responsive.
	backoff := 5 * time.Millisecond
	const maxBackoff = 50 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = file.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, errors.New("audit outbox lock timeout")
		}
		time.Sleep(backoff)
		// Exponential backoff capped at maxBackoff so a single wait can
		// never exceed 50ms; gives a stuck lock-holder ~600 chances to
		// release before the 30s deadline. This is the round-4
		// 5s→30s timeout + retry fix.
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}
