package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/submission"
)

// Exercise the real Stop adapter/Service/Writer chain and a fresh public Main
// request. The Git barrier is enabled by the original committed Plan journal
// event, rather than by a guessed delay or a production test-only callback.
func TestS9FailureHandoffRealStopDeadlineReachesFreshPublicMain(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("requires POSIX shell")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	grep, err := exec.LookPath("grep")
	if err != nil {
		t.Skip("requires grep for the journal barrier")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("requires sleep for the canceled Git process")
	}
	root, worker, plan, input, _ := nativeS9WorkerFixture(t)
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	event := `"transition_id":"PTR-BUG-10"`
	if bytes.Contains(journal, []byte(event)) {
		t.Fatal("fixture already received Plan before the real Stop")
	}
	shimDir := t.TempDir()
	marker := filepath.Join(shimDir, "git-blocked-after-plan")
	script := "#!/bin/sh\nif " + s7CaptureShellQuote(grep) + " -Fq -- " + s7CaptureShellQuote(event) + " " + s7CaptureShellQuote(journalPath) + "; then\n  printf '%s\\n' \"$*\" > " + s7CaptureShellQuote(marker) + "\n  exec " + s7CaptureShellQuote(sleep) + " 30\nfi\nexec " + s7CaptureShellQuote(git) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(shimDir, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+originalPath)
	work, cancel := hookRequestContexts(t.Context(), time.Now().Add(8*time.Second))
	defer cancel()
	input.Context = work
	input.Event = "SubagentStop"
	var stopOut, stopDiagnostic bytes.Buffer
	handled, code := nativeS9Hook(root, input, &stopOut, &stopDiagnostic)
	// Restore the real Git immediately: the next Main request must run with a
	// fresh request context and the original unchanged project environment.
	t.Setenv("PATH", originalPath)
	if !handled || code != 0 {
		t.Fatalf("real Stop: handled=%v code=%d stdout=%s stderr=%s", handled, code, &stopOut, &stopDiagnostic)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("original committed Plan never triggered the Git barrier: %v; %s", err, &stopDiagnostic)
	}
	if !errors.Is(work.Err(), context.DeadlineExceeded) {
		t.Fatalf("real work deadline did not expire: %v; %s", work.Err(), &stopDiagnostic)
	}
	if nativeS9PublicationContext(work).Err() != nil {
		t.Fatalf("Stop exhausted the original publication tail: %s", &stopDiagnostic)
	}
	service, err := submission.NewReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.RepairPlanReportReceipt(plan.Binding.OwnerAgentID, plan.ID)
	if err != nil || !receipt.Accepted {
		t.Fatalf("original Plan receipt was lost: %#v %v", receipt, err)
	}
	snap := repairDispatchSnapshot(t, root)
	record, ok := nativeS9ReturnDiagnosticRecordContext(t.Context(), root, snap, plan.ID)
	flatRelative := nativeS9TestResultPath(t, root, plan)
	flat := filepath.Join(worker, flatRelative)
	if !ok || record.Attempt == nil || record.Attempt.FailureKind != "deadline" || record.Attempt.Stage != "plan_received" || record.Attempt.InputRef != flat {
		t.Fatalf("original attempt was not published after work expiry: %#v ok=%v stderr=%s", record, ok, &stopDiagnostic)
	}
	if !strings.Contains(record.Message, "context deadline exceeded") {
		t.Fatalf("specific timeout cause was swallowed: %s", record.Message)
	}
	before := repairDispatchRuntimeBytes(t, root)
	body, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "session_id": "fresh-main-after-stop-timeout", "agent_id": "Main", "cwd": root,
		"tool_name": "Bash", "tool_input": map[string]any{"command": "git status --short"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var mainOut, mainDiagnostic bytes.Buffer
	if code := Run([]string{"hook", "--root", root, "--event", "PreToolUse"}, bytes.NewReader(body), &mainOut, &mainDiagnostic); code != 0 {
		t.Fatalf("fresh public Main Pre: %d stdout=%s stderr=%s", code, &mainOut, &mainDiagnostic)
	}
	var output struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(mainOut.Bytes(), &output); err != nil {
		t.Fatalf("public Main output: %v %s stderr=%s", err, &mainOut, &mainDiagnostic)
	}
	text := output.HookSpecificOutput.AdditionalContext
	for _, want := range []string{"PlanReport receipt is accepted", "context deadline exceeded", s9OriginalConsumeCommand(root, flat), plan.Binding.OwnerAgentID} {
		if !strings.Contains(text, want) {
			t.Errorf("fresh public Main lost %q: %s; stderr=%s", want, text, &mainDiagnostic)
		}
	}
	if strings.Contains(text, "Neither PlanReport nor Builder Result is accepted") {
		t.Errorf("timeout erased accepted Plan: %s", text)
	}
	repairDispatchAssertUnchanged(t, root, before)
}
