package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// This is a projection fixture, not a fabricated source-authorization history.
// The original listener fixture supplies the same indexed facts its producer
// already tests; the public investigation tests cover real frozen sources.
func TestHookReaderS9ProfessionalProjectionReplacesGenericAndRestoresChangedFacts(t *testing.T) {
	prepared := time.Now()
	f := newS9ListenerFixture(t, true)
	t.Logf("fixture preparation wall=%s", time.Since(prepared))
	owner := f.packet.binding.OwnerAgentID
	answer := filepath.Join(f.worker, filepath.FromSlash(f.packet.answer))
	stale := filepath.Join(f.root, ".claude/stale-plan-answer.json")
	reader := &hookReader{Kind: "Worker", Owner: owner, snapshot: runtime.Snapshot{State: f.state}}
	observe := func(label, event string, full, recorded bool) string {
		t.Helper()
		before, err := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
		if err != nil {
			t.Fatal(err)
		}
		journalPath := filepath.Join(f.root, ".claude/loop-events.jsonl")
		journalBefore, journalErr := os.ReadFile(journalPath)
		timed, spans := metrics.WithCallTimings(context.Background())
		in := policy.Input{Event: event, SessionID: "reader-s9-combination", CWD: f.worker, ToolName: "Read", Context: context.WithValue(timed, hookReaderKey{}, reader)}
		var out, diagnostic bytes.Buffer
		start := time.Now()
		handled, code := hookReaderObservation(f.root, in, &out, &diagnostic)
		t.Logf("%s Hook wall=%s output_bytes=%d diagnostic_bytes=%d", label, time.Since(start), out.Len(), diagnostic.Len())
		t.Logf("%s existing spans=%v (overlapping, not summed)", label, spans.Snapshot())
		if !handled || code != 0 {
			t.Fatalf("%s: handled=%t code=%d out=%s diagnostic=%s", label, handled, code, &out, &diagnostic)
		}
		var envelope struct {
			Output struct {
				Context string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatalf("%s: %v: %s", label, err, &out)
		}
		got := envelope.Output.Context
		if full && !strings.Contains(got, answer) {
			t.Fatalf("%s lost exact Worker answer: %s", label, got)
		}
		if recorded && !strings.Contains(got, "recorded") {
			t.Fatalf("%s lost changed accepted fact: %s", label, got)
		}
		for _, forbidden := range []string{stale, "Recorded facts:", "stale-generic-receipt", filepath.Join(f.root, filepath.FromSlash(f.packet.answer))} {
			if strings.Contains(got, forbidden) {
				t.Fatalf("%s mixed stale generic facts into professional projection: %s", label, got)
			}
		}
		after, err := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("%s mutated Runtime: %v", label, err)
		}
		journalAfter, afterErr := os.ReadFile(journalPath)
		if !bytes.Equal(journalBefore, journalAfter) || os.IsNotExist(journalErr) != os.IsNotExist(afterErr) {
			t.Fatalf("%s mutated journal: before=%v after=%v", label, journalErr, afterErr)
		}
		return got
	}
	observe("empty-refs-first", "PreToolUse", true, false)
	observe("empty-refs-repeat", "PreToolUse", true, false)
	reader.References = []string{stale}
	reader.Receipts = []string{"stale-generic-receipt"}
	observe("stable-refs-first", "PreToolUse", true, false)
	if got := observe("stable-refs-repeat", "PreToolUse", false, false); !strings.Contains(got, "guidance is unchanged") {
		t.Fatalf("repeat did not use original full-context cache: %s", got)
	}
	observe("session-start", "SessionStart", true, false)
	observe("after-session-start", "PreToolUse", true, false)
	// Reuse the listener's accepted-artifact projection shape. This changes an
	// original fact, without pretending that a test-created document is a save.
	receipt := map[string]any{"packet_id": f.packet.id, "owner_agent_id": owner, "execution_root": f.worker}
	repairPointerCLI(f.state)["result_refs"] = []any{f.artifact(t, ".claude/reader-accepted-result.json", map[string]any{"receipt": map[string]any{"provenance": receipt}})}
	f.write(t)
	observe("accepted-fact-change", "PreToolUse", true, true)
	observe("subagent-start", "SubagentStart", true, true)
}
