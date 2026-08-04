package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CheckpointStore persists durable integration checkpoints. The store
// implements an optimistic-lock CAS so concurrent Integrator callers
// cannot lose a merge record. The on-disk format is a single JSON object
// with the Revision field as the optimistic lock — see CompareAndSwap.
//
// The store is local to the package (no external persistence dependency)
// because we are explicitly forbidden from mutating internal/runtime
// checkpoint stores in this BUG's scope. The runtime Store remains the
// authoritative Milestone/journal source; the checkpoint sidecar is the
// minimum durable record that satisfies BE-039 §8 / CT-039-17.
type CheckpointStore struct {
	// Clock is used to populate UpdatedAt; tests can override it.
	Clock func() time.Time
	// NowString formats UpdatedAt. Default is RFC3339Nano UTC.
	NowString func(time.Time) string
}

// DefaultCheckpointStore returns the process-wide CheckpointStore. The
// shape is intentional — it lets the public helpers (CheckpointPath,
// LoadCheckpoint, SaveCheckpoint, CASCheckpoint) work without the caller
// instantiating the type, while still allowing tests to substitute a
// custom store via the package-level variables below.
func DefaultCheckpointStore() *CheckpointStore {
	return defaultStore
}

var (
	defaultStore = &CheckpointStore{
		Clock:     func() time.Time { return time.Now().UTC() },
		NowString: func(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) },
	}

	storeMu sync.Mutex
)

// withStore temporarily swaps the package-level CheckpointStore. Tests use
// this to inject a deterministic clock without mutating the global at the
// top of each test.
func withStore(s *CheckpointStore) func() {
	storeMu.Lock()
	previous := defaultStore
	defaultStore = s
	storeMu.Unlock()
	return func() {
		storeMu.Lock()
		defaultStore = previous
		storeMu.Unlock()
	}
}

// Path returns the canonical on-disk path for a checkpoint. The layout
// mirrors the completion-evidence directory used elsewhere in REQ-039:
//
//	<root>/.claude/evidence/<runtime_id>/g<gen>/worktree/<assignment_id>/checkpoint.json
func (s *CheckpointStore) Path(root, runtimeID string, baselineGeneration int, assignmentID string) string {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	if strings.TrimSpace(runtimeID) == "" {
		runtimeID = "loop-unknown"
	}
	if strings.TrimSpace(assignmentID) == "" {
		assignmentID = "unknown"
	}
	return filepath.Join(
		root,
		".claude",
		"evidence",
		runtimeID,
		fmt.Sprintf("g%d", baselineGeneration),
		"worktree",
		assignmentID,
		"checkpoint.json",
	)
}

// IdempotencyKey builds the canonical idempotency key per BUG-039-05 §4.1:
// assignment_id + source_head + target_branch + baseline_generation. The
// key is hash-truncated to keep it small in journal records and is
// deterministic for the same inputs.
func IdempotencyKey(assignmentID, sourceHead, targetBranch string, baselineGeneration int) string {
	raw := strings.Join([]string{
		strings.TrimSpace(assignmentID),
		strings.TrimSpace(sourceHead),
		strings.TrimSpace(targetBranch),
		strconv.Itoa(baselineGeneration),
	}, "|")
	sum := sha256.Sum256([]byte(raw))
	return "ckpt:" + hex.EncodeToString(sum[:8])
}

// Load reads a checkpoint from disk. If no file exists yet the function
// returns (Checkpoint{State: StatePending}, false, nil) so the caller can
// decide to insert a fresh record.
func (s *CheckpointStore) Load(path string) (Checkpoint, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Checkpoint{State: StatePending}, false, nil
		}
		return Checkpoint{}, false, fmt.Errorf("read checkpoint: %w", err)
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return Checkpoint{}, false, fmt.Errorf("decode checkpoint: %w", err)
	}
	return cp, true, nil
}

// CompareAndSwap writes `next` iff `next.Revision == current.Revision` (or
// `current.Revision == 0` and no file exists yet). The new Revision is
// bumped by one and the write is atomic (temp file + rename + fsync).
//
// On a stale revision CompareAndSwap returns ErrCASStale together with the
// existing record so the caller can decide whether to retry.
//
// The function takes a process-wide file lock (`.lock` next to the
// checkpoint path) so concurrent callers cannot both observe the same
// pre-CAS revision. This is the same optimistic-lock pattern the runtime
// store uses for the journal envelope (runtime.Snapshot/Update).
func (s *CheckpointStore) CompareAndSwap(path string, current Checkpoint, next Checkpoint) (Checkpoint, error) {
	lockPath := path + ".lock"
	release, err := acquireFileLock(lockPath)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("acquire checkpoint lock: %w", err)
	}
	defer release()

	existing, found, err := s.Load(path)
	if err != nil {
		return existing, err
	}
	if found && existing.Revision != current.Revision {
		return existing, ErrCASStale
	}
	if !found && current.Revision != 0 {
		return Checkpoint{}, ErrCASStale
	}
	next.Revision = current.Revision + 1
	if s.Clock != nil {
		next.UpdatedAt = s.NowString(s.Clock())
	} else {
		next.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if next.State == "" {
		next.State = StatePending
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return next, fmt.Errorf("encode checkpoint: %w", err)
	}
	if err := atomicWriteFile(path, data); err != nil {
		return next, err
	}
	return next, nil
}

// ForceWrite is used by the failure / preserve paths to record a terminal
// blocker state without going through CAS — the merge response loss recovery
// contract (BE-039 §8) explicitly says cleanup_response_loss reconciles
// "only from durable checkpoints", and we want the durable record present
// even if the optimistic-lock owner has gone away.
//
// ForceWrite bumps Revision from whatever is on disk (or 0 if absent) and
// fsyncs the new value. It does NOT skip the atomic write — durable means
// durable. ForceWrite also takes the same file lock as CompareAndSwap so
// concurrent CAS writers cannot interleave with a forced preserve write.
func (s *CheckpointStore) ForceWrite(path string, next Checkpoint) (Checkpoint, error) {
	lockPath := path + ".lock"
	release, err := acquireFileLock(lockPath)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("acquire checkpoint lock: %w", err)
	}
	defer release()

	existing, _, err := s.Load(path)
	if err != nil {
		return existing, err
	}
	next.Revision = existing.Revision + 1
	if s.Clock != nil {
		next.UpdatedAt = s.NowString(s.Clock())
	} else {
		next.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if next.State == "" {
		next.State = StatePending
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return next, fmt.Errorf("encode checkpoint: %w", err)
	}
	if err := atomicWriteFile(path, data); err != nil {
		return next, err
	}
	return next, nil
}

// atomicWriteFile writes data atomically: temp file → fsync → rename →
// fsync(parent dir). The temp file is created in the same directory so the
// final rename is on the same filesystem.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir checkpoint dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "checkpoint-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp: %w", err)
	}
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}
