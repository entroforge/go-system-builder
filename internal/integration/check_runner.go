package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type checkEvidenceKey struct{}
type checkEvidence struct {
	Directory    string
	Receipts     *[]string
	ToolReceipts *[]checkexec.ToolReceipt
}

// CommandCheckRunner inherits the caller's environment. It does not source a
// login shell or invent project settings. Diagnostics live outside the checkout
// so checks cannot dirty their own integration baseline.
func CommandCheckRunner(ctx context.Context, root, command string) error {
	ctx, cancel := checkexec.CheckTimeout(ctx)
	defer cancel()
	parent := ""
	evidence, _ := ctx.Value(checkEvidenceKey{}).(checkEvidence)
	if evidence.Directory != "" {
		parent = evidence.Directory
		if err := os.MkdirAll(parent, 0700); err != nil {
			return checkexec.New("diagnostic_io", "main", command, err)
		}
	}
	dir, err := os.MkdirTemp(parent, "loop-integration-check-")
	if err != nil {
		return checkexec.New("diagnostic_io", "main", command, err)
	}
	logPath := filepath.Join(dir, "output.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return checkexec.New("diagnostic_io", "main", command, err)
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = root
	if p, ok := checkexec.CurrentProfile(ctx); ok {
		env, err := p.CommandEnvironment()
		if err != nil {
			log.Close()
			return checkexec.New("capability_missing", "environment owner", command, err)
		}
		cmd.Env = env
	}
	cmd.Stdout = log
	cmd.Stderr = log
	// Bound pipe cleanup even if a command leaves descendants behind.
	cmd.WaitDelay = 2 * time.Second
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
	// Intentionally do not dump the environment: commands may carry credentials.
	settings := map[string]string{}
	for _, key := range []string{"E2E_WORKERS", "E2E_RUNTIME_OWNER", "E2E_RUNTIME_SERVICES", "CI"} {
		if value, ok := os.LookupEnv(key); ok {
			settings[key] = value
		}
	}
	failure := checkexec.Classify(ctx, "main", command, err)
	if kind == "diagnostic_io" {
		failure = checkexec.New("diagnostic_io", "main", command, err)
	}
	if kind == "target_drift" {
		failure = checkexec.New("input_drift", "main", command, err)
	}
	receiptPath := filepath.Join(dir, "receipt.json")
	if failure != nil {
		failure.ReceiptRef = receiptPath
		failure.LogRef = logPath
	}
	binding, _ := checkexec.CurrentBinding(ctx)
	receipt := map[string]any{"check_binding": binding, "failure": failure, "command": command, "cwd": root, "started_at": start.UTC(), "elapsed_ms": time.Since(start).Milliseconds(), "status": status, "failure_kind": kind, "exit_code": exitCode, "caller_settings": settings, "output": logPath}
	data, _ := json.MarshalIndent(receipt, "", "  ")
	if writeErr := publishCheckReceipt(receiptPath, data); writeErr != nil {
		return checkexec.New("diagnostic_io", "main", command, fmt.Errorf("persist check receipt: %w", writeErr))
	}
	if evidence.Receipts != nil {
		*evidence.Receipts = append(*evidence.Receipts, receiptPath)
	}
	if err != nil {
		failure.ReceiptRef = receiptPath
		failure.LogRef = logPath
		return fmt.Errorf("check %q failed (%s, exit=%d): %w; receipt=%s; log=%s", command, kind, exitCode, failure, receiptPath, logPath)
	}
	return nil
}

func publishCheckReceipt(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".receipt-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
