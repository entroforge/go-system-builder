package hook

import (
	"github.com/entroforge/go-system-builder/internal/policy"
	"testing"
)

func TestPlanningStopBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		input  policy.Input
		want   bool
	}{
		{"contracts continue", nil, policy.Input{Event: "Stop"}, true},
		{"loop guard", nil, policy.Input{Event: "Stop", StopHookActive: true}, false},
		{"other event", nil, policy.Input{Event: "SessionStart"}, false},
		{"tasks continue", func(s map[string]any) {
			s["lifecycle"].(map[string]any)["phase"] = "tasks"
			s["milestone"].(map[string]any)["lifecycle_phase"] = "tasks"
		}, policy.Input{Event: "Stop"}, true},
		{"design signoff", func(s map[string]any) { s["lifecycle"].(map[string]any)["phase"] = "design" }, policy.Input{Event: "Stop"}, false},
		{"human wait", func(s map[string]any) { s["milestone"].(map[string]any)["human_required"] = true }, policy.Input{Event: "Stop"}, false},
		{"blocked", func(s map[string]any) { s["blockers"] = []any{"external"} }, policy.Input{Event: "Stop"}, false},
		{"paused", func(s map[string]any) { s["pause"] = map[string]any{} }, policy.Input{Event: "Stop"}, false},
		{"terminal", func(s map[string]any) { s["lifecycle"].(map[string]any)["state"] = "awaiting_human_release" }, policy.Input{Event: "Stop"}, false},
		{"delegated", func(s map[string]any) {
			s["entities"] = map[string]any{"agents": []any{map[string]any{"state": "working"}}}
		}, policy.Input{Event: "Stop"}, false},
		{"missing milestone", func(s map[string]any) { delete(s, "milestone") }, policy.Input{Event: "Stop"}, false},
		{"stale milestone", func(s map[string]any) { s["milestone"].(map[string]any)["lifecycle_phase"] = "design" }, policy.Input{Event: "Stop"}, false},
		{"unbound", func(s map[string]any) { delete(s, "bound_req") }, policy.Input{Event: "Stop"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := map[string]any{"lifecycle": map[string]any{"state": "planning", "phase": "contracts"}, "bound_req": map[string]any{"id": "REQ-053", "status": "locked"}, "milestone": map[string]any{"human_required": false, "blocked": false, "lifecycle_phase": "contracts"}}
			if tc.change != nil {
				tc.change(s)
			}
			d, b := mainStopDecisionForState(s, tc.input)
			if b != tc.want {
				t.Fatalf("blocked=%v want=%v decision=%+v", b, tc.want, d)
			}
		})
	}
}
