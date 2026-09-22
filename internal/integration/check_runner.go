package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type checkEvidenceKey struct{}
type checkEvidence struct {
	Directory string
	Receipts  *[]string
}

// CommandCheckRunner inherits the caller's environment. It does not source a
// login shell or invent project settings. Diagnostics live outside the checkout
// so checks cannot dirty their own integration baseline.
func CommandCheckRunner(ctx context.Context, root, command string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	parent := ""
	evidence, _ := ctx.Value(checkEvidenceKey{}).(checkEvidence)
	if evidence.Directory != "" {
		parent = evidence.Directory
		if err := os.MkdirAll(parent, 0700); err != nil {
			return err
		}
	}
	dir, err := os.MkdirTemp(parent, "loop-integration-check-")
	if err != nil {
		return err
	}
	logPath := filepath.Join(dir, "output.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = root
	cmd.Stdout = log
	cmd.Stderr = log
	// Bound pipe cleanup even if a command leaves descendants behind.
	cmd.WaitDelay = 2 * time.Second
	// Record the tree before execution as well as after it; a trailing HEAD
	// alone cannot identify what a command started testing.
	readHead := func() string {
		git := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
		git.Dir = root
		out, _ := git.Output()
		return strings.TrimSpace(string(out))
	}
	beforeHead := readHead()
	start := time.Now()
	err = runCheckProcess(cmd)
	closeErr := log.Close()
	status := "pass"
	kind := ""
	exitCode := 0
	if err != nil {
		status = "fail"
		kind = "execution"
		exitCode = -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
			kind = "exit"
		}
		if ctx.Err() != nil {
			kind = "cancelled_or_timeout"
		}
	} else if closeErr != nil {
		status = "fail"
		kind = "diagnostic_io"
		err = closeErr
	}
	afterHead := readHead()
	if err == nil && beforeHead != "" && afterHead != beforeHead {
		status, kind, exitCode = "fail", "target_drift", 0
		err = fmt.Errorf("check target HEAD changed during execution")
	}
	// Intentionally do not dump the environment: commands may carry credentials.
	settings := map[string]string{}
	for _, key := range []string{"E2E_WORKERS", "E2E_RUNTIME_OWNER", "E2E_RUNTIME_SERVICES", "CI"} {
		if value, ok := os.LookupEnv(key); ok {
			settings[key] = value
		}
	}
	receipt := map[string]any{"command": command, "cwd": root, "head": afterHead, "head_before": beforeHead, "head_after": afterHead, "started_at": start.UTC(), "elapsed_ms": time.Since(start).Milliseconds(), "status": status, "failure_kind": kind, "exit_code": exitCode, "caller_settings": settings, "output": logPath}
	data, _ := json.MarshalIndent(receipt, "", "  ")
	receiptPath := filepath.Join(dir, "receipt.json")
	if writeErr := os.WriteFile(receiptPath, data, 0600); writeErr != nil {
		return fmt.Errorf("persist check receipt: %w", writeErr)
	}
	if evidence.Receipts != nil {
		*evidence.Receipts = append(*evidence.Receipts, receiptPath)
	}
	if err != nil {
		return fmt.Errorf("check %q failed (%s, exit=%d): %w; receipt=%s; log=%s", command, kind, exitCode, err, receiptPath, logPath)
	}
	return nil
}
