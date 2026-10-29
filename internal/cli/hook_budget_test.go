package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestHookBudgetTimeoutKeepsListenerNonBlocking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Hook process")
	}
	script := filepath.Join(t.TempDir(), "hook")
	os.WriteFile(script, []byte("#!/bin/sh\nprintf '{\"decision\":\"allow\"}'\nsleep 30\n"), 0700)
	for _, tool := range []string{"Write", "Bash", "Read"} {
		t.Run(tool, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			var out, errout bytes.Buffer
			start := time.Now()
			code := runBudgetedHook(ctx, script, []string{"--root", t.TempDir()}, strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"`+tool+`"}`), &out, &errout)
			want := 0
			if code != want || out.Len() != 0 || !strings.Contains(errout.String(), "internal budget") {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &errout)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("deadline failed to bound process")
			}
		})
	}
}

func TestHookFailurePreservesOriginalDiagnosticAndDiscardsPartialSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Hook process")
	}
	script := filepath.Join(t.TempDir(), "hook")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '{\"accepted\":true}'\nprintf 'Main owns recovery: original Record source is missing\\n' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var out, diagnostic bytes.Buffer
	code := runBudgetedHook(ctx, script, []string{"--root", t.TempDir()}, strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Read"}`), &out, &diagnostic)
	if code != 0 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "original Record source is missing") || !strings.Contains(diagnostic.String(), "original receipt") || !strings.Contains(diagnostic.String(), "does not prove no commit") {
		t.Fatalf("incomplete Hook hid its cause or delivered partial success: code=%d stdout=%s stderr=%s", code, &out, &diagnostic)
	}
}

func TestHookContextCarriesCancellationWithoutAuthorBudgetFields(t *testing.T) {
	var request policy.Input
	if err := json.Unmarshal([]byte(`{"session_id":"actual-session","Context":{"timeout":999},"hook_event_name":"PreToolUse"}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.Context != nil {
		t.Fatal("platform/author payload supplied an internal Hook lifetime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	request.Context = ctx
	derived := contextForHook(request)
	want, _ := ctx.Deadline()
	if got, ok := derived.Deadline(); !ok || got != want {
		t.Fatal("adding session context discarded the Hook deadline")
	}
	data, err := json.Marshal(request)
	if err != nil || bytes.Contains(data, []byte("Context")) || bytes.Contains(data, []byte("timeout")) {
		t.Fatalf("internal budget became an author/persistence field: %s %v", data, err)
	}
	cancel()
	select {
	case <-derived.Done():
	default:
		t.Fatal("existing nested Hook calls lost parent cancellation")
	}
}

func TestHookTimeoutPreservesReceiverDiagnosticWithoutPublishingPartialSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Hook process")
	}
	for _, event := range []string{"PostToolUse", "SubagentStop"} {
		t.Run(event, func(t *testing.T) {
			dir := t.TempDir()
			script := filepath.Join(dir, "hook")
			body := "#!/bin/sh\nprintf '{\"accepted\":true}'\nprintf 'Original packet=packet-original answer=/original/answer.json; original receiver committed; successor preparation incomplete\\n' >&2\nsleep 30\n"
			if err := os.WriteFile(script, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			var out, diagnostic bytes.Buffer
			code := runBudgetedHook(ctx, script, []string{"--root", dir}, strings.NewReader(`{"hook_event_name":"`+event+`","tool_name":"Write"}`), &out, &diagnostic)
			if code != 0 || out.Len() != 0 {
				t.Fatalf("transport published partial success or changed listener control: code=%d stdout=%s stderr=%s", code, &out, &diagnostic)
			}
			for _, want := range []string{"packet-original", "/original/answer.json", "original receiver committed", "successor preparation incomplete", "internal budget", "not a professional content verdict", "Framework recovery"} {
				if !strings.Contains(diagnostic.String(), want) {
					t.Fatalf("missing %q: %s", want, &diagnostic)
				}
			}
			if degraded, err := metrics.HealthDegraded(dir); err != nil || !degraded {
				t.Fatalf("outer timeout missing from health below platform threshold: degraded=%v err=%v", degraded, err)
			}
			for _, forbidden := range []string{"Main must inspect", "save the unchanged file again", "change the artifact id"} {
				if strings.Contains(diagnostic.String(), forbidden) {
					t.Fatalf("transport assigned infrastructure work to the author: %s", &diagnostic)
				}
			}
		})
	}
}

func TestS4HookBudgetIncludesPathlessSaveAndLeavesOtherEventsBounded(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	if err := os.WriteFile(statePath, []byte(`{"lifecycle":{"state":"planning","phase":"tasks"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		payload string
		want    time.Duration
	}{
		{`{"hook_event_name":"PostToolUse","tool_name":"mcp__files__execute"}`, s4HookProcessBudget},
		{`{"hook_event_name":"PostToolUse","tool_name":"Read"}`, s4HookProcessBudget}, // pending original followup
		{`{"hook_event_name":"PreToolUse","tool_name":"Write"}`, hookProcessBudget},
		{`{"hook_event_name":"Stop"}`, hookProcessBudget},
		{`{"hook_event_name":"PostToolUseFailure","tool_name":"Write"}`, hookProcessBudget},
		{`{"hook_event_name":"PostToolUse","tool_response":{"is_error":true}}`, hookProcessBudget},
		{`{"hook_event_name":"PostToolUse","agent_id":"worker"}`, hookProcessBudget},
	} {
		var input policy.Input
		if err := json.Unmarshal([]byte(tc.payload), &input); err != nil {
			t.Fatal(err)
		}
		if got := hookBudget(root, input); got != tc.want {
			t.Fatalf("%s budget=%s want=%s", tc.payload, got, tc.want)
		}
	}
	if err := os.WriteFile(statePath, []byte(`{"lifecycle":{"state":"document_verification"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	input := policy.Input{Event: "PostToolUse", ToolName: "Write", ToolInput: map[string]any{"file_path": filepath.Join(root, ".claude/submissions/REQ-039/S4/Main/answer.json")}}
	if hookBudget(root, input) != s4HookProcessBudget {
		t.Fatal("original S4 replay lost its handoff budget after the transition")
	}
	input.ToolInput = nil
	if hookBudget(root, input) != hookProcessBudget {
		t.Fatal("unrelated S5 event inherited S4 budget")
	}
	for _, args := range [][]string{{"--root", root, "--event", "PostToolUse"}, {"--event=PostToolUse", "--root=" + root}} {
		if hookRootForBudget(args) != root {
			t.Fatal("native process and worker selected different roots")
		}
	}
}
