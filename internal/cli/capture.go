// capture.go — `loop-harness capture step` (L3-S7 §3.6 / §6.3): the
// transient capture buffer that execution wrappers write into while a
// Reviewer works. review-result submit merges buffered steps into findings
// whose encounter timeline is empty.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// runCapture is the `loop-harness capture` dispatcher.
func runCapture(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "capture requires <step|exec>")
		return 2
	}
	if args[0] == "exec" {
		return runCaptureExec(args[1:], stdin, stdout, stderr)
	}
	if args[0] != "step" {
		fmt.Fprintln(stderr, "capture requires <step|exec>")
		return 2
	}
	flags := flag.NewFlagSet("capture step", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	assignmentID := flags.String("assignment", "", "plan assignment the step belongs to")
	action := flags.String("action", "", "what was done (sanitized)")
	observed := flags.String("observed", "", "what was observed at the checkpoint")
	evidence := flags.String("evidence", "", "comma-separated evidence refs (redacted refs, never values)")
	findingID := flags.String("finding", "", "optional Finding id this step belongs to")
	claimID := flags.String("claim", "", "optional Claim id this step belongs to")
	sequence := flags.Int("sequence", 0, "step sequence (default: next)")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *assignmentID == "" || *action == "" || *observed == "" {
		fmt.Fprintln(stderr, "capture step requires --assignment, --action and --observed")
		return 2
	}

	var evidenceRefs []string
	if *evidence != "" {
		for _, ref := range strings.Split(*evidence, ",") {
			if ref != "" {
				evidenceRefs = append(evidenceRefs, ref)
			}
		}
	}
	step := review.CaptureStep{
		Sequence:   *sequence,
		FindingID:  *findingID,
		ClaimID:    *claimID,
		Action:     *action,
		Observed:   *observed,
		Evidence:   evidenceRefs,
		CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	// Redaction gate: the buffer never persists secret values.
	if err := review.SanitizeCapture(step); err != nil {
		fmt.Fprintf(stderr, "capture step rejected: %v\n", err)
		return 1
	}

	statePath := filepath.Join(*root, ".claude", "loop-state.json")
	journalPath := filepath.Join(*root, ".claude", "loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		fmt.Fprintf(stderr, "capture step: read runtime: %v\n", err)
		return 1
	}
	runtimeID, _ := snapshot.State["runtime_id"].(string)
	generation := 0
	if baseline, ok := snapshot.State["baseline"].(map[string]any); ok {
		if value, ok := baseline["generation"].(float64); ok {
			generation = int(value)
		}
	}
	bufferPath := review.CaptureFile(*root, runtimeID, generation, *assignmentID)
	steps, err := review.LoadCaptureStepsStrict(bufferPath)
	if err != nil {
		fmt.Fprintf(stderr, "capture step: read buffer: %v\n", err)
		return 1
	}
	if step.Sequence == 0 {
		step.Sequence = len(steps) + 1
	}
	if err := os.MkdirAll(filepath.Dir(bufferPath), 0o755); err != nil {
		fmt.Fprintf(stderr, "capture step: create buffer dir: %v\n", err)
		return 1
	}
	file, err := os.OpenFile(bufferPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(stderr, "capture step: open buffer: %v\n", err)
		return 1
	}
	defer file.Close()
	data, err := json.Marshal(step)
	if err != nil {
		fmt.Fprintf(stderr, "capture step: encode: %v\n", err)
		return 1
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		fmt.Fprintf(stderr, "capture step: append: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "captured step %d for %s (buffer: %s; pass it to `runtime review-result submit --captures`)\n", step.Sequence, *assignmentID, bufferPath)
	return 0
}
