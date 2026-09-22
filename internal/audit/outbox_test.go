package audit_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/audit"
	"github.com/entroforge/go-system-builder/internal/filelock"
)

func TestOutboxAppendIsIdempotentByDecisionID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
	outbox := audit.NewOutbox(path)
	record := map[string]any{
		"decision_id": "hook-decision-1",
		"decision":    "block",
	}
	if err := outbox.Append(record); err != nil {
		t.Fatal(err)
	}
	if err := outbox.Append(record); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(strings.TrimSpace(string(data)), "\n") + 1; lines != 1 {
		t.Fatalf("expected one record, got %d: %s", lines, data)
	}
}

// TestOutboxAcquireLockRetriesOnContention verifies that Append waits for a
// live process-owned lock and resumes after the holder releases it.
func TestOutboxAcquireLockRetriesOnContention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hook-decisions.jsonl")
	lockPath := path + ".lock.process"
	outbox := audit.NewOutbox(path)

	releaseHolder, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseHolder)
	go func() {
		time.Sleep(200 * time.Millisecond)
		releaseHolder()
	}()

	start := time.Now()
	if err := outbox.Append(map[string]any{
		"decision_id": "hook-decision-retry",
		"decision":    "audit",
	}); err != nil {
		t.Fatalf("Append must succeed via retry, got: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 100*time.Millisecond {
		t.Fatalf("Append returned too fast (%s) — did it actually wait for the lock?", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Append waited too long (%s) — retry loop not bounded", elapsed)
	}
}

func TestOutboxAppendIgnoresLegacyStaleSentinel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
	legacyLock := path + ".lock"
	if err := os.WriteFile(legacyLock, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if err := audit.NewOutbox(path).Append(map[string]any{
		"decision_id": "hook-decision-after-crash",
		"decision":    "audit",
	}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("stale legacy sentinel delayed Append: %s", elapsed)
	}
	if _, err := os.Stat(legacyLock); err != nil {
		t.Fatalf("Append must not unlink legacy lock artifacts: %v", err)
	}
}

func TestOutboxAppendReusesUnlockedProcessLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
	processLock := path + ".lock.process"
	if err := os.WriteFile(processLock, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := audit.NewOutbox(path).Append(map[string]any{
		"decision_id": "hook-decision-unlocked-file",
		"decision":    "audit",
	}); err != nil {
		t.Fatalf("an unlocked persistent lock file must not block Append: %v", err)
	}
}

func TestOutboxProcessLockReleasedAfterHolderExit(t *testing.T) {
	if os.Getenv("GO_SYSTEM_BUILDER_AUDIT_LOCK_HELPER") == "1" {
		lockPath := os.Getenv("GO_SYSTEM_BUILDER_AUDIT_LOCK_PATH")
		if _, err := filelock.Acquire(context.Background(), lockPath); err != nil {
			os.Exit(2)
		}
		// Exit without calling the release function. The OS must release the
		// process-owned lock even though the persistent lock file remains.
		os.Exit(0)
	}

	path := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
	cmd := exec.Command(os.Args[0], "-test.run=^TestOutboxProcessLockReleasedAfterHolderExit$")
	cmd.Env = append(os.Environ(),
		"GO_SYSTEM_BUILDER_AUDIT_LOCK_HELPER=1",
		"GO_SYSTEM_BUILDER_AUDIT_LOCK_PATH="+path+".lock.process",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lock-holder subprocess failed: %v: %s", err, output)
	}

	start := time.Now()
	if err := audit.NewOutbox(path).Append(map[string]any{
		"decision_id": "hook-decision-after-holder-exit",
		"decision":    "audit",
	}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("exited holder left the outbox blocked: %s", elapsed)
	}
}

// TestOutboxAppendRejectsRecordWithoutDecisionID covers outbox.go:32-34 —
// the audit envelope contract requires decision_id.
func TestOutboxAppendRejectsRecordWithoutDecisionID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
	outbox := audit.NewOutbox(path)
	err := outbox.Append(map[string]any{"decision": "audit"})
	if err == nil {
		t.Fatal("Append must reject records without decision_id")
	}
	if !strings.Contains(err.Error(), "decision_id") {
		t.Fatalf("error must mention decision_id, got: %v", err)
	}
}

// TestOutboxAppendFailsWhenOutboxIsDirectory covers outbox.go:52-55 —
// OpenFile returns an error when the path is a directory.
func TestOutboxAppendFailsWhenOutboxIsDirectory(t *testing.T) {
	dir := t.TempDir()
	// Pre-create the outbox as a directory so OpenFile(path, O_CREATE) fails.
	badPath := filepath.Join(dir, "hook-decisions.jsonl")
	if err := os.Mkdir(badPath, 0o755); err != nil {
		t.Fatal(err)
	}
	outbox := audit.NewOutbox(badPath)
	err := outbox.Append(map[string]any{"decision_id": "x", "decision": "audit"})
	if err == nil {
		t.Fatal("Append must fail when outbox path is a directory")
	}
}

// TestOutboxAppendFailsWhenMkdirAllFails covers outbox.go:36-38 — when
// the parent directory cannot be created (e.g. the outbox path lives
// under a read-only filesystem), Append must surface the MkdirAll error
// rather than silently failing later.
func TestOutboxAppendFailsWhenMkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	// Make the parent read-only so MkdirAll fails with EACCES.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	// Put a non-existent subdir under the read-only parent.
	path := filepath.Join(dir, "no-such-subdir", "hook-decisions.jsonl")
	outbox := audit.NewOutbox(path)
	err := outbox.Append(map[string]any{"decision_id": "x", "decision": "audit"})
	if err == nil {
		t.Fatal("Append must fail when MkdirAll cannot create the parent dir")
	}
	if !strings.Contains(err.Error(), "create audit directory") {
		t.Fatalf("error must mention 'create audit directory', got: %v", err)
	}
}

func TestOutboxAppendWaitsForHeldProcessLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hook-decisions.jsonl")
	lockPath := path + ".lock.process"
	releaseHolder, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseHolder)

	done := make(chan error, 1)
	outbox := audit.NewOutbox(path)
	go func() {
		done <- outbox.Append(map[string]any{
			"decision_id": "x",
			"decision":    "audit",
		})
	}()
	select {
	case err := <-done:
		t.Fatalf("Append returned prematurely: err=%v", err)
	case <-time.After(100 * time.Millisecond):
	}
	releaseHolder()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Append must succeed after lock release, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Append did not return within 2s after lock release")
	}
}
