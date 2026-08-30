package cli

import (
	"strings"
	"testing"
)

func TestStatusProjectionAtHumanReleaseUsesEmptyOpenItemsArray(t *testing.T) {
	projection := buildStatusProjection(map[string]any{
		"runtime_id": "loop-REQ-001",
		"revision":   52,
		"lifecycle":  map[string]any{"state": "awaiting_human_release"},
	}, "S11", "awaiting_human_release", nil, ".")
	if len(projection.OpenItems) != 1 || projection.OpenItems[0] == "" {
		t.Fatalf("S11 status projection must expose one actionable human decision, got %v", projection.OpenItems)
	}
	gateway, ok := projection.HumanGateway.(map[string]any)
	if !ok {
		t.Fatalf("S11 status projection must expose a human gateway object, got %T", projection.HumanGateway)
	}
	if gateway["type"] != "human_release_gateway" {
		t.Fatalf("unexpected S11 gateway type: %#v", gateway["type"])
	}
	dispositions, ok := gateway["dispositions"].([]string)
	if !ok || len(dispositions) != 6 {
		t.Fatalf("S11 gateway must expose six finite dispositions, got %#v", gateway["dispositions"])
	}
}

func TestTerminalProjectionsDoNotFallBackToCrossStage(t *testing.T) {
	for _, tc := range []struct {
		state string
		stage string
		want  string
	}{
		{state: "release_authorized", stage: "S11", want: "human-authorized terminal"},
		{state: "aborted", stage: "aborted", want: "aborted terminal"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			state := map[string]any{
				"runtime_id": "loop-REQ-001",
				"revision":   53,
				"lifecycle":  map[string]any{"state": tc.state},
			}
			stage, _, action := projectNext(tc.state, "", ".")
			if stage != tc.stage {
				t.Fatalf("state %s projected as stage %q, want %q", tc.state, stage, tc.stage)
			}
			if !strings.Contains(action, tc.want) {
				t.Fatalf("state %s action %q does not contain %q", tc.state, action, tc.want)
			}
			status := buildStatusProjection(state, stage, tc.state, nil, ".")
			gateway, ok := status.HumanGateway.(map[string]any)
			if !ok || gateway["type"] == "" {
				t.Fatalf("state %s status must expose terminal gateway projection, got %#v", tc.state, status.HumanGateway)
			}
		})
	}
}

func TestS10ProjectionKeepsTheAuditSequenceVisible(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  []string
	}{
		{state: "acceptance", want: []string{"coverage_inventory", "counterevidence", "s10 manifest validate", "do not modify product code"}},
		{state: "release_audit", want: []string{"8 release-audit areas", "counterevidence", "s10 manifest validate", "S7"}},
	} {
		t.Run(tc.state, func(t *testing.T) {
			stage, _, action := projectNext(tc.state, "", ".")
			if stage != "S10" {
				t.Fatalf("state %s projected as stage %q, want S10", tc.state, stage)
			}
			for _, want := range tc.want {
				if !strings.Contains(action, want) {
					t.Fatalf("S10 action %q does not contain %q", action, want)
				}
			}
		})
	}
}
