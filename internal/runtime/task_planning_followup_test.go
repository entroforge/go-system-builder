package runtime_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// This is an in-memory compatibility fixture, not a Writer/model acceptance.
func taskPlanningFollowupState(t *testing.T) (map[string]any, map[string]any, workpacket.TaskPlanningPacket, string) {
	t.Helper()
	root, state, files := taskPlanningRuntimeFixture(t)
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := runtime.CurrentTaskPlanningBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := workpacket.BuildTaskPlanning(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var packetRow map[string]any
	if err := json.Unmarshal(data, &packetRow); err != nil {
		t.Fatal(err)
	}
	key := workpacket.Digest([]byte("submission:" + packet.ID))
	followup := workpacket.Digest([]byte(key + ":after-domain"))
	row := map[string]any{
		"submission_key": key, "packet_id": packet.ID, "owner": "Main",
		"content_sha256": workpacket.Digest([]byte("professional answer fixture")), "evidence_id": "E-S4-001", "followup_event_id": followup,
		"control_status": "no_transition", "origin_state": "planning", "origin_phase": "tasks", "origin_entry": binding.StageEntry,
	}
	entities := state["entities"].(map[string]any)
	entities["work_packets"] = append(entities["work_packets"].([]any), packetRow)
	entities["submissions"] = append(entities["submissions"].([]any), row)
	return state, row, packet, followup
}

func TestTaskPlanningLegacyNoTransitionRetriesOnlyAtOriginalEntry(t *testing.T) {
	for _, change := range []string{"none", "new_entry", "new_generation", "other_runtime", "advanced", "replaced"} {
		t.Run(change, func(t *testing.T) {
			state, row, packet, followup := taskPlanningFollowupState(t)
			var expected error
			switch change {
			case "new_entry":
				state["lifecycle"].(map[string]any)["phase_revision"] = packet.Binding.StageEntry + 4
				expected = runtime.ErrSubmissionFollowupOriginChanged
			case "new_generation":
				state["baseline"].(map[string]any)["generation"] = packet.Binding.Generation + 1
				expected = runtime.ErrSubmissionFollowupOriginChanged
			case "other_runtime":
				state["runtime_id"] = "other-runtime"
				expected = runtime.ErrSubmissionFollowupOriginChanged
			case "advanced", "replaced":
				row["control_status"] = change
				expected = runtime.ErrSubmissionFollowupProcessed
			}
			if _, err := runtime.SubmissionFollowup(state, followup); !errors.Is(err, expected) {
				t.Fatalf("followup after %s: got %v, want %v", change, err, expected)
			}
		})
	}
}

func TestTaskPlanningLegacyNoTransitionStampRequiresOriginalTR002Destination(t *testing.T) {
	for _, destination := range []string{"S5", "wrong_entry", "wrong_phase"} {
		t.Run(destination, func(t *testing.T) {
			state, row, packet, followup := taskPlanningFollowupState(t)
			life := map[string]any{"state": "document_verification", "phase_revision": packet.Binding.StageEntry + 1}
			if destination == "wrong_entry" {
				life["phase_revision"] = packet.Binding.StageEntry + 5
			} else if destination == "wrong_phase" {
				life["state"], life["phase"] = "planning", "tasks"
			}
			state["lifecycle"] = life
			err := runtime.StampSubmissionTransition(state, followup, "evt-original-TR-002")
			if destination == "S5" {
				if err != nil || row["control_status"] != "advanced" || row["transition_event_id"] != "evt-original-TR-002" {
					t.Fatalf("original adjacent stamp failed: %v; %#v", err, row)
				}
			} else if !errors.Is(err, runtime.ErrSubmissionFollowupProcessed) || row["control_status"] != "no_transition" {
				t.Fatalf("stamped an unrelated destination: %v; %#v", err, row)
			}
		})
	}
}
