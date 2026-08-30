package metrics_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
)

func TestFormatDoctorReportsHookTimingPercentiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	records := []string{
		`{"decision_id":"hook-decision-1","hook_event":"PreToolUse","elapsed_ms":2}`,
		`{"decision_id":"hook-decision-2","hook_event":"PreToolUse","elapsed_ms":10}`,
		`{"decision_id":"hook-decision-3","hook_event":"PreToolUse","elapsed_ms":20}`,
		`{"decision_id":"hook-decision-4","hook_event":"PreToolUse","elapsed_ms":40}`,
		`{"decision_id":"hook-decision-5","hook_event":"Stop","elapsed_ms":7}`,
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "hook-decisions.jsonl"), []byte(strings.Join(records, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := metrics.FormatDoctor(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`loop_hook_evaluation_duration_ms{event="PreToolUse"} count=4 p95_ms=40 max_ms=40`,
		`loop_hook_evaluation_duration_ms{event="Stop"} count=1 p95_ms=7 max_ms=7`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatDoctorWarnsWhenHookTimingApproachesTimeout(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"hook_event":"PreToolUse","elapsed_ms":4000}
{"hook_event":"PreToolUse","elapsed_ms":5000}
`
	if err := os.WriteFile(filepath.Join(root, ".claude", "hook-decisions.jsonl"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := metrics.FormatDoctor(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "WARNING") || !strings.Contains(out, "40%") {
		t.Fatalf("doctor must warn about the timeout bypass window: %s", out)
	}
}

func TestFormatDoctorIgnoresMissingHookTimingOutbox(t *testing.T) {
	if out, err := metrics.FormatDoctor(t.TempDir()); err != nil || !strings.Contains(out, "loop_hook_evaluation_duration_ms (no samples)") {
		t.Fatalf("missing hook timing outbox must be non-fatal, out=%q err=%v", out, err)
	}
}
