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
	// A failed check keeps its receipt and log as evidence; the attempt
	// directory belongs to this test so it is reclaimed with it.
	t.Setenv("TMPDIR", t.TempDir())
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
func TestCheckRunnerTypedFailureMatchesActualReceipt(t *testing.T) {
	root := t.TempDir()
	// A failed check keeps its receipt and log as evidence; the attempt
	// directory belongs to this test so it is reclaimed with it.
	t.Setenv("TMPDIR", t.TempDir())
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

// A check attempt that no evidence owner keeps must not pile up in the
// system temporary directory, while the attempts that something does refer to
// must survive exactly as they are today.
func TestCheckRunnerRemovesOnlyUnownedPassedAttempts(t *testing.T) {
	root := t.TempDir()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	// A passed check with no evidence owner has no referent left.
	if err := CommandCheckRunner(context.Background(), root, "true"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("passed check left attempt directories behind: %v", entries)
	}

	// A failed check keeps its receipt and log, because the failure points at
	// them.
	failureErr := CommandCheckRunner(context.Background(), root, "exit 9")
	if failureErr == nil {
		t.Fatal("failed check must not pass")
	}
	entries, err = os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("failed check lost its attempt directory: %v", entries)
	}
	attempt := filepath.Join(tmp, entries[0].Name())
	if !strings.HasPrefix(entries[0].Name(), "loop-integration-check-") {
		t.Fatalf("attempt directory is not the original layout: %s", entries[0].Name())
	}
	for _, name := range []string{"receipt.json", "output.log"} {
		if _, statErr := os.Stat(filepath.Join(attempt, name)); statErr != nil {
			t.Fatalf("failed check lost its %s: %v", name, statErr)
		}
	}

	// An owned attempt directory survives even when the check passes, because
	// its receipt is the canonical Main check artifact.
	receipts := []string{}
	owned := context.WithValue(context.Background(), checkEvidenceKey{}, checkEvidence{
		Directory: filepath.Join(t.TempDir(), "checks"), Receipts: &receipts,
	})
	if err := CommandCheckRunner(owned, root, "true"); err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 {
		t.Fatalf("owned check did not publish its receipt: %v", receipts)
	}
	if _, statErr := os.Stat(receipts[0]); statErr != nil {
		t.Fatalf("owned check lost its receipt file: %v", statErr)
	}
}
