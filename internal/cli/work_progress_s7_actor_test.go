package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS7OptionalProgressUnknownNativeActorIsSilent(t *testing.T) {
	for _, mode := range []string{"unknown native actor", "registered actor missing cwd", "broken agent context", "other stage", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			root := initFormatRoot(t)
			state, err := inactiveRuntimeState(root, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			state["schema_version"] = "1.2.0"
			state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
			state["lifecycle"].(map[string]any)["state"] = "verification"
			state["lifecycle"].(map[string]any)["phase"] = "planned"
			state["bound_req"] = map[string]any{"id": "REQ-progress", "path": "docs/requirements/REQ-progress.md", "version": "v1.0.0"}
			entities := state["entities"].(map[string]any)
			entities["agents"] = []any{}
			input := policy.Input{AgentID: "native-internal-actor", CWD: root, SessionID: "s7-progress"}
			switch mode {
			case "registered actor missing cwd":
				entities["agents"] = []any{map[string]any{"id": input.AgentID, "state": "working"}}
				input.CWD = ""
			case "broken agent context":
				entities["agents"] = "invalid-array"
			case "other stage":
				state["lifecycle"].(map[string]any)["state"] = "building"
			case "legacy":
				state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": false}
			}
			facts, execution, err := projectWorkProgress(root, input, runtime.Snapshot{State: state})
			if mode == "unknown native actor" {
				if err != nil || facts.Known || facts.Complete || execution != nil {
					t.Fatalf("unregistered native actor must leave optional progress unknown: facts=%+v execution=%v err=%v", facts, execution, err)
				}
			} else if err == nil {
				t.Fatal("existing context errors must remain visible")
			} else if mode == "registered actor missing cwd" && !strings.Contains(err.Error(), "directory unavailable") {
				t.Fatalf("registered responsibility lost its original directory diagnostic: %v", err)
			}
		})
	}
}
