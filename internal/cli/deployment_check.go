package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// deployment-check is a read-only preflight usable outside Claude Code Hooks.
// It detects common handoff errors, not every semantic Runtime invariant.
func runDeploymentCheck(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("deployment-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "project root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	issues := []string{}
	var state struct {
		RuntimeID string `json:"runtime_id"`
		Journal   struct {
			LastSequence int64 `json:"last_sequence"`
		} `json:"journal"`
	}
	data, err := os.ReadFile(filepath.Join(*root, ".claude", "loop-state.json"))
	if err != nil {
		issues = append(issues, "state unreadable: "+err.Error())
	} else if err := json.Unmarshal(data, &state); err != nil {
		issues = append(issues, "state malformed: "+err.Error())
	} else if state.RuntimeID == "" {
		issues = append(issues, "state runtime_id missing")
	}
	file, err := os.Open(filepath.Join(*root, ".claude", "loop-events.jsonl"))
	if err != nil {
		issues = append(issues, "journal unreadable: "+err.Error())
	} else {
		defer file.Close()
		decoder := json.NewDecoder(file)
		var last int64
		journalValid := true
		for {
			var event struct {
				RuntimeID string `json:"runtime_id"`
				Sequence  int64  `json:"sequence"`
			}
			if err := decoder.Decode(&event); err == io.EOF {
				break
			} else if err != nil {
				issues = append(issues, "journal malformed: "+err.Error())
				break
			}
			if event.RuntimeID != state.RuntimeID {
				journalValid = false
				issues = append(issues, "state/journal runtime_id mismatch")
				break
			}
			if event.Sequence < 1 || (last != 0 && event.Sequence != last+1) {
				issues = append(issues, "journal sequence gap")
				break
			}
			last = event.Sequence
		}
		if journalValid && last != state.Journal.LastSequence {
			issues = append(issues, fmt.Sprintf("state journal sequence %d differs from active tail %d", state.Journal.LastSequence, last))
		}
	}
	tracked, err := exec.Command("git", "-C", *root, "ls-files", "--", ".claude/loop-state.json", ".claude/loop-events.jsonl").Output()
	if err != nil {
		issues = append(issues, "Git tracking check unavailable: "+err.Error())
	} else if strings.Contains(string(tracked), ".claude/loop-state.json") && !strings.Contains(string(tracked), ".claude/loop-events.jsonl") {
		issues = append(issues, "Git tracks active state without its journal; preserve a paired backup before changing tracking")
	}
	status := "passed"
	if len(issues) > 0 {
		status = "blocked"
	}
	if code := encodeJSON(stdout, map[string]any{"status": status, "issues": issues, "scope": "handoff preflight; full validation is still required", "recovery": "runtime recover inspect --root <root> --req <locked-REQ-path>; then runtime recover plan with the same arguments"}); code != 0 {
		return code
	}
	if len(issues) > 0 {
		return 1
	}
	return 0
}
