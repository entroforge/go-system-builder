package controller

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestAutomaticCandidatesExcludeHumanGatewayAndTerminalStates(t *testing.T) {
	catalog := &transition.Catalog{
		Transitions: map[string]transition.TransitionSpec{
			"TR-025": {
				ID:   "TR-025",
				From: "awaiting_human_release",
				AutoTrigger: &transition.AutoTriggerSpec{
					Enabled: true,
				},
			},
			"TR-031": {
				ID:   "TR-031",
				From: "release_authorized",
				AutoTrigger: &transition.AutoTriggerSpec{
					Enabled: true,
				},
			},
			"TR-032": {
				ID:   "TR-032",
				From: "aborted",
				AutoTrigger: &transition.AutoTriggerSpec{
					Enabled: true,
				},
			},
		},
	}

	for _, state := range []string{"awaiting_human_release", "release_authorized", "aborted"} {
		t.Run(state, func(t *testing.T) {
			if candidates := automaticCandidatesFor(catalog, transition.Cursor{State: state}); len(candidates) != 0 {
				t.Fatalf("automatic candidates from %s = %#v, want none", state, candidates)
			}
		})
	}
}
