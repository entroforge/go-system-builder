package integration

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckRunnerInheritsEnvironmentAndKeepsCheckoutClean(t *testing.T) {
	root := t.TempDir()
	t.Setenv("E2E_WORKERS", "1")
	t.Setenv("E2E_RUNTIME_OWNER", "playwright")
	t.Setenv("E2E_RUNTIME_SERVICES", "web")
	err := CommandCheckRunner(context.Background(), root, `test "$E2E_WORKERS" = 1 && test "$E2E_RUNTIME_OWNER" = playwright && test "$E2E_RUNTIME_SERVICES" = web`)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("check dirtied integration checkout")
	}
}
func TestCheckRunnerExitCodeAndTimeoutDiagnostics(t *testing.T) {
	root := t.TempDir()
	err := CommandCheckRunner(context.Background(), root, "printf 'failure-detail'; exit 7")
	if err == nil || !strings.Contains(err.Error(), "exit=7") || !strings.Contains(err.Error(), "receipt=") {
		t.Fatalf("missing real failure: %v", err)
	}
	logPath := strings.Split(err.Error(), "; log=")[1]
	b, e := os.ReadFile(logPath)
	if e != nil || string(b) != "failure-detail" {
		t.Fatalf("lost log: %q %v", b, e)
	}
	if _, e := os.Stat(filepath.Join(filepath.Dir(logPath), "receipt.json")); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = CommandCheckRunner(ctx, root, "exec sleep 3")
	if err == nil || !strings.Contains(err.Error(), "cancelled_or_timeout") {
		t.Fatalf("timeout misclassified: %v", err)
	}
}
func TestMissingCheckRunnerCannotPass(t *testing.T) {
	result, err := RunCheck(context.Background(), t.TempDir(), "must run", nil)
	if err == nil || result.Status != "fail" {
		t.Fatalf("missing executor passed: %#v %v", result, err)
	}
}

func TestCheckRunnerTypedFailureMatchesActualReceipt(t *testing.T) {
	root := t.TempDir()
	err := CommandCheckRunner(context.Background(), root, "printf 'actual-error'; exit 17")
	var failure *checkexec.Failure
	if !errors.As(err, &failure) || failure.Category != "process_exit" || failure.ReceiptRef == "" || failure.LogRef == "" {
		t.Fatalf("lost real typed error: %v", err)
	}
	data, readErr := os.ReadFile(failure.ReceiptRef)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var receipt struct {
		Failure *checkexec.Failure `json:"failure"`
		Status  string             `json:"status"`
		Exit    int                `json:"exit_code"`
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "fail" || receipt.Exit != 17 || receipt.Failure.Category != "process_exit" || receipt.Failure.ReceiptRef != failure.ReceiptRef {
		t.Fatal("receipt guessed assertion or lost source", receipt)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = CommandCheckRunner(ctx, root, "exec sleep 30")
	if !errors.As(err, &failure) || failure.Category != "timeout" {
		t.Fatalf("timeout is not typed: %v", err)
	}
	// Original receipt publication failure remains a check failure, never PASS.
	parent := filepath.Join(root, "not-a-directory")
	os.WriteFile(parent, []byte("file"), 0600)
	ctx = context.WithValue(context.Background(), checkEvidenceKey{}, checkEvidence{Directory: parent})
	err = CommandCheckRunner(ctx, root, "true")
	if !errors.As(err, &failure) || failure.Category != "diagnostic_io" {
		t.Fatalf("diagnostic error misclassified: %v", err)
	}
}
