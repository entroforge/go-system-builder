// s6_status.go provides a read-only board view of the current S6 batch:
// which registered TASKs still lack a Builder Result, which have failed
// checks or scope deviations, and which lack a verified integration
// checkpoint. It is the agent's answer to "where do I stand" without
// guessing from raw JSON.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// runS6Command is the `loop-harness s6` dispatcher. Currently only
// `status` exists: a read-only board of the current S6 batch (which TASKs
// lack a Builder Result, which have failing checks or deviations, which
// lack a verified integration checkpoint).
func runS6Command(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "status" {
		fmt.Fprintln(stderr, "s6 requires <status>")
		return 2
	}
	flags := flag.NewFlagSet("s6 status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	return runS6Status(*root, stdout)
}

// runS6Status reads the current runtime state and prints the S6 batch
// board to stdout. It performs no writes.
func runS6Status(root string, stdout io.Writer) int {
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	store := runtime.NewStore(statePath, journalPath)
	snapshot, err := store.Snapshot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "read runtime: %v\n", err)
		return 1
	}
	state := snapshot.State
	generation := 0
	if baseline, ok := state["baseline"].(map[string]any); ok {
		if value, ok := baseline["generation"].(float64); ok {
			generation = int(value)
		}
	}
	runtimeID, _ := state["runtime_id"].(string)

	batch := s6BatchTasks(state)
	completed := s6Completions(state, root)
	integrated := s6VerifiedCheckpoints(root, runtimeID, generation)

	fmt.Fprintf(stdout, "S6 batch board (generation %d, %d task(s) registered)\n", generation, len(batch))
	if len(batch) == 0 {
		fmt.Fprintln(stdout, "(empty batch — run TR-003 first; see `loop-harness explain TR-003`)")
		return 0
	}
	for _, taskID := range batch {
		fmt.Fprintf(stdout, "\n%s\n", taskID)
		envelope, hasEnvelope := completed[taskID]
		if hasEnvelope {
			fmt.Fprintf(stdout, "  completion: registered as %s\n", envelope["evidence_id"])
			if failing := s6FailingChecks(envelope); len(failing) > 0 {
				fmt.Fprintf(stdout, "  checks: %s (not all pass)\n", strings.Join(failing, ", "))
			} else {
				fmt.Fprintln(stdout, "  checks: all recorded checks pass")
			}
			if devs := s6ScopeDeviations(envelope); len(devs) > 0 {
				fmt.Fprintf(stdout, "  scope_deviations: %s\n", strings.Join(devs, ", "))
			} else {
				fmt.Fprintln(stdout, "  scope_deviations: none")
			}
		} else {
			fmt.Fprintln(stdout, "  completion: no Builder Result (run `runtime task-complete`)")
		}
		if integrated[taskID] {
			fmt.Fprintln(stdout, "  integration: verified checkpoint present")
		} else {
			fmt.Fprintln(stdout, "  integration: no verified checkpoint (run `runtime task-integrate` after completion)")
		}
	}
	return 0
}

// s6BatchTasks returns the TR-003 exact execution batch: the task documents
// registered at the current baseline generation.
func s6BatchTasks(state map[string]any) []string {
	generation := 0
	if baseline, ok := state["baseline"].(map[string]any); ok {
		if value, ok := baseline["generation"].(float64); ok {
			generation = int(value)
		}
	}
	raw, _ := state["documents"].([]any)
	var batch []string
	for _, item := range raw {
		doc, _ := item.(map[string]any)
		if doc == nil || doc["kind"] != "task" {
			continue
		}
		if g, ok := doc["generation"].(float64); !ok || int(g) != generation {
			continue
		}
		if id, ok := doc["id"].(string); ok && id != "" {
			batch = append(batch, id)
		}
	}
	sort.Strings(batch)
	return batch
}

// s6Completions returns the latest registered completion envelope per task.
// The map keys task_id → envelope body. Reading from disk (rather than
// trusting the index row) is what lets the board show checks and deviations
// without duplicating the gate's decoding.
func s6Completions(state map[string]any, root string) map[string]map[string]any {
	completed := make(map[string]map[string]any)
	raw, _ := state["evidence"].([]any)
	for _, item := range raw {
		entry, _ := item.(map[string]any)
		if entry == nil || entry["kind"] != "completion_report" {
			continue
		}
		if invalidated, _ := entry["invalidated_by"].(string); invalidated != "" {
			continue
		}
		path, _ := entry["path"].(string)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			continue
		}
		var envelope map[string]any
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}
		taskID, _ := envelope["task_id"].(string)
		if taskID == "" {
			continue
		}
		completed[taskID] = envelope
	}
	return completed
}

// s6VerifiedCheckpoints scans .claude/evidence/<runtime>/g<gen>/worktree/
// for checkpoint.json files with state >= verified and returns the set of
// task ids that have one.
func s6VerifiedCheckpoints(root, runtimeID string, generation int) map[string]bool {
	verified := make(map[string]bool)
	dir := filepath.Join(root, ".claude", "evidence", runtimeID, fmt.Sprintf("g%d", generation), "worktree")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return verified
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name(), "checkpoint.json"))
		if err != nil {
			continue
		}
		var checkpoint struct {
			TaskID string `json:"task_id"`
			State  string `json:"state"`
		}
		if json.Unmarshal(data, &checkpoint) != nil || checkpoint.TaskID == "" {
			continue
		}
		switch checkpoint.State {
		case "verified", "acknowledged", "cleanup_pending", "complete":
			verified[checkpoint.TaskID] = true
		}
	}
	return verified
}

func s6FailingChecks(envelope map[string]any) []string {
	var failing []string
	checks, _ := envelope["checks"].([]any)
	for _, raw := range checks {
		check, _ := raw.(map[string]any)
		if check == nil {
			continue
		}
		if result, _ := check["result"].(string); result != "pass" {
			label, _ := check["name"].(string)
			if label == "" {
				label, _ = check["command"].(string)
			}
			failing = append(failing, fmt.Sprintf("%s=%s", label, result))
		}
	}
	return failing
}

func s6ScopeDeviations(envelope map[string]any) []string {
	var devs []string
	raw, _ := envelope["scope_deviations"].([]any)
	for _, item := range raw {
		if s, _ := item.(string); s != "" {
			devs = append(devs, s)
		}
	}
	return devs
}
