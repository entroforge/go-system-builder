package hook

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestS6ContextPreservesGateFactsAndDeliversChangedErrors(t *testing.T) {
	root := t.TempDir()
	d := policy.Decision{Decision: "allow", AdditionalContext: "TASK one: read /task.md and return /result.json", Guidance: &policy.Guidance{
		Stage: "S6", LifecycleState: "building", Action: "TASK one: read /task.md and return /result.json", ProtocolRef: "docs/control/agent-protocol.md#s6",
	}}
	r := controller.ControlResult{QualityGate: controller.QualityGateResult{Status: controller.StatusNotReady, GateID: "GATE-BUILDER-BATCH-READY", ObservedRevision: 10, Fingerprint: "first", Missing: []string{"evidence:completion_report:TASK-one"}}}
	first, code, err := PreToolUseS6Context(d, r)
	if err != nil || code != 0 {
		t.Fatalf("render: %d %v", code, err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(first, &envelope); err != nil {
		t.Fatal(err)
	}
	context := envelope["hookSpecificOutput"].(map[string]any)["additionalContext"].(string)
	if strings.Count(context, "/task.md") != 1 || !strings.Contains(context, "completion_report:TASK-one") {
		t.Fatalf("lost gate facts or repeated the responsibility: %s", context)
	}
	_, commit := PrepareResponsibilityContext(root, "session", "Main", "PreToolUse", "S6", "task-r1", first)
	commit()
	r.QualityGate.ObservedRevision++
	r.QualityGate.Fingerprint = "later"
	next, _, _ := PreToolUseS6Context(d, r)
	unchanged, _ := PrepareResponsibilityContext(root, "session", "Main", "PreToolUse", "S6", "task-r1", next)
	if !strings.Contains(string(unchanged), "guidance is unchanged") {
		t.Fatalf("metadata churn repeated guidance: %s", unchanged)
	}
	r.QualityGate.Missing = append(r.QualityGate.Missing, "integration_checkpoint:TASK-two")
	next, _, _ = PreToolUseS6Context(d, r)
	changed, _ := PrepareResponsibilityContext(root, "session", "Main", "PreToolUse", "S6", "task-r1", next)
	if strings.Contains(string(changed), "guidance is unchanged") || !strings.Contains(string(changed), "TASK-two") {
		t.Fatalf("new obligation hidden: %s", changed)
	}
	r.QualityGate.Status = controller.StatusUnknown
	r.QualityGate.Conflicts = []string{"original source is unavailable at /missing-source"}
	failed, _, _ := PreToolUseS6Context(d, r)
	if !strings.Contains(string(failed), "/missing-source") || !strings.Contains(string(failed), "QUALITY GATE UNKNOWN") {
		t.Fatalf("error recovery was compacted away: %s", failed)
	}
}
