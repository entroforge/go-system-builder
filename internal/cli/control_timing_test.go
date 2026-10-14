package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestHookPersistsMeasuredPhasesInVersionedDiagnosticEnvelope(t *testing.T) {
	root := acFixtureRoot(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-events.jsonl"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	args := []string{"hook", "--event", "PreToolUse", "--root", root}
	input := `{"hook_event_name":"PreToolUse","session_id":"timing-session","tool_name":"Read","tool_input":{"file_path":"docs/control/loop-definition.json"}}`
	code := 0
	if binary := os.Getenv("FRAMEWORK_REPAIR_BINARY"); binary != "" {
		cmd := exec.Command(binary, args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(input), &out, &errout
		if err := cmd.Run(); err != nil {
			t.Fatalf("actual Hook binary: %v %s", err, errout.String())
		}
	} else {
		code = cli.Run(args, strings.NewReader(input), &out, &errout)
	}
	if code != 0 {
		t.Fatalf("Hook failed %d: %s", code, errout.String())
	}
	record := readLastHookDecision(t, root)
	if record["schema_version"] != "1.2.0" || record["session_id"] != "timing-session" {
		t.Fatalf("missing version/correlation: %#v", record)
	}
	timing := record["timing"].(map[string]any)
	phases := timing["phases"].(map[string]any)
	for _, phase := range []string{"runtime_lock_wait", "runtime_lock_hold", "runtime_state_read", "control_cycle_total"} {
		span, ok := phases[phase].(map[string]any)
		if !ok || span["calls"].(float64) < 1 || span["duration_ns"].(float64) < 0 {
			t.Fatalf("missing measured %s: %#v", phase, phases)
		}
	}
	encoded, _ := json.Marshal(record)
	if receipt := os.Getenv("FRAMEWORK_TIMING_RECEIPT"); receipt != "" {
		if err := os.WriteFile(receipt, append(encoded, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	validator := schema.NewEmbeddedValidator()
	if err := validator.ValidateBytes("hook-decision.schema.json", encoded); err != nil {
		t.Fatalf("new envelope rejected: %v", err)
	}
	// Neither relabeling new data as legacy nor unknown phase names may pass.
	record["schema_version"] = "1.1.0"
	encoded, _ = json.Marshal(record)
	if validator.ValidateBytes("hook-decision.schema.json", encoded) == nil {
		t.Fatal("silently changed legacy schema")
	}
	record["schema_version"] = "1.2.0"
	phases["invented"] = map[string]any{"calls": 1, "duration_ns": 0}
	encoded, _ = json.Marshal(record)
	if validator.ValidateBytes("hook-decision.schema.json", encoded) == nil {
		t.Fatal("accepted unknown metric")
	}
}
