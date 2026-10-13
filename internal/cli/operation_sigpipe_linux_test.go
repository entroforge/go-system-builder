//go:build linux

package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRuntimeOperationRecoversAfterActualSIGPIPE(t *testing.T) {
	binary := os.Getenv("FRAMEWORK_REPAIR_BINARY")
	if binary == "" {
		t.Skip("requires the actual built binary")
	}
	root := worktreeProjectRoot(t)
	plan := minimalReviewPlan(t, t.TempDir(), "review-plan-sigpipe")
	args := []string{"runtime", "review-plan", "--root", root, "--file", plan, "--operation-id", "sigpipe-plan-1", "--expected-revision", "1"}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, args...)
	var diagnostic bytes.Buffer
	command.Stdout, command.Stderr = writer, &diagnostic
	err = command.Run()
	_ = writer.Close()
	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		t.Fatalf("closed stdout did not fail: %v %s", err, diagnostic.String())
	}
	status, ok := exited.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGPIPE {
		t.Fatalf("expected actual SIGPIPE, got %v %s", err, diagnostic.String())
	}
	sp, jp := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	beforeState, _ := os.ReadFile(sp)
	beforeJournal, _ := os.ReadFile(jp)
	code, queried, msg := operationCLI(t, "runtime", "operation", "--root", root, "--id", "sigpipe-plan-1")
	if code != 0 {
		t.Fatalf("query after SIGPIPE: %s", msg)
	}
	var query map[string]any
	if err := json.Unmarshal(queried, &query); err != nil {
		t.Fatal(err)
	}
	if query["status"] != "committed" || query["receipt"] == nil {
		t.Fatalf("commit lost: %s", queried)
	}
	code, replayed, msg := operationCLI(t, args...)
	if code != 0 {
		t.Fatalf("replay after SIGPIPE: %s", msg)
	}
	var replay map[string]any
	if err := json.Unmarshal(replayed, &replay); err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(query["receipt"])
	retried, _ := json.Marshal(replay["operation_receipt"])
	if !bytes.Equal(original, retried) {
		t.Fatal("replay changed durable receipt")
	}
	afterState, _ := os.ReadFile(sp)
	afterJournal, _ := os.ReadFile(jp)
	if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
		t.Fatal("retry consumed state/journal twice")
	}
	t.Logf("actual_signal=%s queried_status=%s state_and_journal_unchanged_after_retry=true receipt=%s", status.Signal(), query["status"], original)
}
