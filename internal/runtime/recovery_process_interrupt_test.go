package runtime_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// TestRecoveryWriterSurvivesKilledProcess interrupts the original recovery
// Writer after it has persisted its pending operation and replaced the state,
// but before it replaces the journal. The parent then uses the original
// recovery entrypoint to finish the interrupted operation.
func TestRecoveryWriterSurvivesKilledProcess(t *testing.T) {
	const childRootEnv = "GSB_RECOVERY_KILL_TEST_ROOT"
	const planID = "plan-killed-recovery-writer"
	const ready = "writer replaced state with pending operation retained"

	if root := os.Getenv(childRootEnv); root != "" {
		statePath := filepath.Join(root, ".claude", "loop-state.json")
		journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
		state, journal := recoveryCandidates(t)
		req := recoveryRequest(root, statePath, journalPath, state, journal, planID, "")
		req.FailureInjector = recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
			if step != runtime.RecoveryAfterStateReplace {
				return nil
			}
			if _, err := fmt.Fprintln(os.Stdout, ready); err != nil {
				return err
			}
			// The parent leaves stdin open while it terminates this process.
			var b [1]byte
			_, err := io.ReadFull(os.Stdin, b[:])
			return fmt.Errorf("interrupted Writer unexpectedly resumed: %w", err)
		})
		_, err := runtime.ApplyRecovery(req)
		t.Fatalf("Writer unexpectedly returned before termination: %v", err)
	}

	root, statePath, journalPath := recoveryPaths(t)
	originalState := []byte("damaged original state\n")
	originalJournal := []byte("damaged original journal\n")
	writeRecoveryFile(t, statePath, originalState)
	writeRecoveryFile(t, journalPath, originalJournal)
	candidateState, candidateJournal := recoveryCandidates(t)

	cmd := exec.Command(os.Args[0], "-test.run=^TestRecoveryWriterSurvivesKilledProcess$")
	cmd.Env = append(os.Environ(), childRootEnv+"="+root)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	type lineResult struct {
		line string
		err  error
	}
	line := make(chan lineResult, 1)
	go func() {
		value, err := bufio.NewReader(stdout).ReadString('\n')
		line <- lineResult{value, err}
	}()
	select {
	case result := <-line:
		if result.err != nil || strings.TrimSpace(result.line) != ready {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			waited = true
			t.Fatalf("Writer did not reach interruption window: %q, %v; stderr: %s", result.line, result.err, stderr.String())
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		waited = true
		t.Fatalf("Writer did not reach interruption window within 30s; stderr: %s", stderr.String())
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill recovery Writer: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		waited = true
		t.Fatal("killed Writer exited successfully")
	}
	waited = true

	marker := statePath + ".recovery-pending.json"
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("original Writer did not retain pending operation: %v", err)
	}
	if got := mustReadRecoveryFile(t, statePath); bytes.Equal(got, originalState) || !json.Valid(got) {
		t.Fatal("Writer did not reach the state-replaced interruption window")
	}
	if got := mustReadRecoveryFile(t, journalPath); !bytes.Equal(got, originalJournal) {
		t.Fatal("journal changed before the selected interruption window")
	}

	retry := recoveryRequest(root, statePath, journalPath, nil, nil, planID, "")
	result, err := runtime.ApplyRecovery(retry)
	if err != nil || !result.Applied || !result.Retried {
		t.Fatalf("original recovery entrypoint did not resume pending operation: %+v, %v", result, err)
	}
	assertRecoveredPair(t, statePath, journalPath, candidateState, candidateJournal, planID)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending marker remains after recovery: %v", err)
	}
	if got := mustReadRecoveryFile(t, filepath.Join(result.QuarantineDir, "loop-state.json")); !bytes.Equal(got, originalState) {
		t.Fatal("original state was not retained in quarantine")
	}
	if got := mustReadRecoveryFile(t, filepath.Join(result.QuarantineDir, "loop-events.jsonl")); !bytes.Equal(got, originalJournal) {
		t.Fatal("original journal was not retained in quarantine")
	}
	recoveredState := mustReadRecoveryFile(t, statePath)
	recoveredJournal := mustReadRecoveryFile(t, journalPath)
	again := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, planID, "")
	second, err := runtime.ApplyRecovery(again)
	if err != nil || !second.Applied || !second.Idempotent {
		t.Fatalf("repeat recovery was not idempotent: %+v, %v", second, err)
	}
	if !bytes.Equal(mustReadRecoveryFile(t, statePath), recoveredState) || !bytes.Equal(mustReadRecoveryFile(t, journalPath), recoveredJournal) {
		t.Fatal("idempotent recovery modified the recovered Runtime pair")
	}
}
