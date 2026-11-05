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
//
// The barrier is live again: G01-A put an ordinary Git branch resolution back
// into the reception chain (internal/cli/native_s9_original.go, the recorded
// authoring location), so the shim below does trigger and the original request
// deadline really expires inside the Result consumer. The earlier G01-C note
// ("the reception performs no Git call any more") described the topology
// before G01-A landed and is no longer true.
//
// The published attempt is judged by its structural facts, not by one stage
// name: the attempt stopped at the original Result consumer boundary, so the
// consumer boundary marker, the failure kind and the exact input coordinates
// carry the meaning (internal/cli/native_s9_attempt.go:30-39 "The stage name
// stays a diagnostic label and never decides the consumer boundary").
// L4-hook-acceptance-checklist.md:158 G26 — "真实原接收已收 Plan 后中断,在不重读
// 业务来源的发布尾段保留已捕获的准确失败;全新 Main 请求在 additionalContext 获得该
// 原因和实际恢复操作".
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
		t.Fatalf("original committed Plan never triggered the Git barrier: %v; %s (the reception performs no Git call any more; see the G01-C note above)", err, &stopDiagnostic)
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
	if !ok || record.Attempt == nil || record.Attempt.FailureKind != "deadline" || record.Attempt.InputRef != flat {
		t.Fatalf("original attempt was not published after work expiry: %#v ok=%v stderr=%s", record, ok, &stopDiagnostic)
	}
	// The attempt stopped at the original Result consumer boundary. The
	// boundary, the failure kind and the exact input coordinates are the
	// structural facts; the stage name is a diagnostic label and is not pinned
	// to one topology.
	if record.Attempt.ConsumerBoundary != nativeS9ConsumerBoundaryMark {
		t.Fatalf("original attempt did not stop at the consumer boundary: %#v", record.Attempt)
	}
	if !nativeS9ConsumerAttempt(record.Attempt) {
		t.Fatalf("original attempt is not a consumer attempt: %#v", record.Attempt)
	}
	if record.Attempt.Stage == "" || record.Attempt.Stage == "selected" || record.Attempt.Stage == "unknown" {
		t.Fatalf("original attempt kept an undetermined stage label: %#v", record.Attempt)
	}
	if record.Attempt.TypedInputRef != "" {
		t.Fatalf("original attempt named a typed input copy: %#v", record.Attempt)
	}
	if !strings.Contains(record.Message, "context deadline exceeded") {
		t.Fatalf("specific timeout cause was swallowed: %s", record.Message)
	}
	// The published reason shows the unfinished consumer and its exact input.
	if !strings.Contains(record.Message, "reached "+record.Attempt.Stage) || !strings.Contains(record.Message, "Consumer diagnostic:") {
		t.Fatalf("published attempt does not show the unfinished consumer: %s", record.Message)
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
