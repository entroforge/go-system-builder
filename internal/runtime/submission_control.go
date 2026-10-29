package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

var ErrSubmissionFollowupProcessed = errors.New("submission followup already processed")
var ErrSubmissionFollowupOriginChanged = errors.New("submission followup origin changed")

// SubmissionFollowup resolves only a durable domain reception. A caller's
// event string never authorizes control work without that original record.
func SubmissionFollowup(state map[string]any, eventID string) (SubmissionRecord, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["followup_event_id"] != eventID {
			continue
		}
		record, ok, err := FindSubmission(state, stringValue(row["packet_id"]))
		if err != nil || !ok {
			return SubmissionRecord{}, fmt.Errorf("resolve submission followup: %v", err)
		}
		if p, taskPlanning, err := FindTaskPlanningPacket(state, record.PacketID); err != nil {
			return record, err
		} else if taskPlanning {
			key := workpacket.Digest([]byte("submission:" + p.ID))
			if record.Key != key || record.FollowupID != workpacket.Digest([]byte(key+":after-domain")) || record.Owner != "Main" || record.OriginState != "planning" || record.OriginPhase != "tasks" || record.OriginEntry != p.Binding.StageEntry {
				return record, fmt.Errorf("invalid task planning followup binding")
			}
			// A pre-fix S4 evaluation could have stored no_transition while
			// TR-002 was not ready. It remains retryable only at this exact
			// original tasks entry; an advanced or replaced submission is history.
			if record.ControlStatus != "pending" && record.ControlStatus != "no_transition" {
				return record, ErrSubmissionFollowupProcessed
			}
			life, _ := state["lifecycle"].(map[string]any)
			entry, err := integerField(life, "phase_revision")
			if err != nil {
				return record, err
			}
			if state["runtime_id"] != p.Binding.RuntimeID || baselineGeneration(state) != p.Binding.Generation || life["state"] != "planning" || life["phase"] != "tasks" || entry != p.Binding.StageEntry {
				return record, ErrSubmissionFollowupOriginChanged
			}
			return record, nil
		}
		if p, contract, err := FindContractPacket(state, record.PacketID); err != nil {
			return record, err
		} else if contract {
			key := workpacket.Digest([]byte("submission:" + p.ID))
			if record.Key != key || record.FollowupID != workpacket.Digest([]byte(key+":after-domain")) || record.Owner != "Main" || record.OriginState != "planning" || record.OriginPhase != "contracts" || record.OriginEntry != p.Binding.StageEntry {
				return record, fmt.Errorf("invalid contract followup binding")
			}
			// Older S3 evaluations recorded no_transition before the gate
			// was ready. That is retryable only at this exact original entry;
			// an advanced or replaced submission stays historical.
			if record.ControlStatus != "pending" && record.ControlStatus != "no_transition" {
				return record, ErrSubmissionFollowupProcessed
			}
			life, _ := state["lifecycle"].(map[string]any)
			entry, err := integerField(life, "phase_revision")
			if err != nil {
				return record, err
			}
			if state["runtime_id"] != p.Binding.RuntimeID || baselineGeneration(state) != p.Binding.Generation || life["state"] != "planning" || life["phase"] != "contracts" || entry != p.Binding.StageEntry {
				return record, ErrSubmissionFollowupOriginChanged
			}
			return record, nil
		}
		if p, design, err := FindDesignPacket(state, record.PacketID); err != nil {
			return record, err
		} else if design {
			if record.OriginState != "planning" || record.OriginPhase != "design" || record.OriginEntry != p.Binding.StageEntry {
				return record, fmt.Errorf("design followup responsibility differs")
			}
			if record.ControlStatus != "pending" && record.ControlStatus != "no_transition" {
				return record, ErrSubmissionFollowupProcessed
			}
			life, _ := state["lifecycle"].(map[string]any)
			entry, err := integerField(life, "phase_revision")
			if err != nil {
				return record, err
			}
			if state["runtime_id"] != p.Binding.RuntimeID || baselineGeneration(state) != p.Binding.Generation || life["state"] != "planning" || life["phase"] != "design" || entry != record.OriginEntry {
				return record, ErrSubmissionFollowupOriginChanged
			}
			return record, nil
		}
		packet, ok, err := FindWorkPacket(state, record.PacketID)
		if err != nil || !ok {
			return record, fmt.Errorf("followup has no issued packet")
		}
		key := workpacket.Digest([]byte("submission:" + packet.ID))
		if record.Key != key || record.FollowupID != workpacket.Digest([]byte(key+":after-domain")) || record.Owner != packet.Binding.Owner || record.OriginState != "document_verification" || record.OriginPhase != "" || record.OriginEntry != packet.Binding.StageEntry {
			return record, fmt.Errorf("invalid submission followup binding")
		}
		if record.ControlStatus != "pending" {
			return record, ErrSubmissionFollowupProcessed
		}
		life, _ := state["lifecycle"].(map[string]any)
		baseline, _ := state["baseline"].(map[string]any)
		entry, entryErr := integerField(life, "phase_revision")
		generation, generationErr := integerField(baseline, "generation")
		if entryErr != nil || generationErr != nil {
			return record, fmt.Errorf("invalid followup origin: %v %v", entryErr, generationErr)
		}
		if state["runtime_id"] != packet.Binding.RuntimeID || generation != packet.Binding.Generation || life["state"] != record.OriginState || stringValue(life["phase"]) != record.OriginPhase || entry != record.OriginEntry {
			return record, ErrSubmissionFollowupOriginChanged
		}
		return record, nil
	}
	return SubmissionRecord{}, fmt.Errorf("submission followup is not registered")
}

// StampSubmissionTransition runs inside transition.Apply's original Writer
// mutation, after that mutation checked the pending origin and ran its guards.
// The cursor and this result must reach disk in the same commit.
func StampSubmissionTransition(state map[string]any, eventID, transitionEventID string) error {
	if transitionEventID == "" {
		return fmt.Errorf("submission transition event is required")
	}
	return setSubmissionControl(state, eventID, "advanced", transitionEventID)
}

func setSubmissionControl(state map[string]any, eventID, status, transitionEventID string) error {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["followup_event_id"] == eventID {
			if row["control_status"] != "pending" {
				// Stamp runs after the original engine changed the cursor. Only
				// S2/S3/S4 no_transition origins may complete their original
				// adjacent move; other processed work remains historical.
				if row["control_status"] != "no_transition" || status != "advanced" || !validNoTransitionPlanningStamp(state, stringValue(row["packet_id"])) {
					return ErrSubmissionFollowupProcessed
				}
			}
			row["control_status"] = status
			if transitionEventID != "" {
				row["transition_event_id"] = transitionEventID
			}
			return nil
		}
	}
	return fmt.Errorf("submission followup is not registered")
}

func validNoTransitionPlanningStamp(state map[string]any, packetID string) bool {
	life, _ := state["lifecycle"].(map[string]any)
	entry, entryErr := integerField(life, "phase_revision")
	if entryErr != nil {
		return false
	}
	if packet, found, err := FindDesignPacket(state, packetID); err == nil && found {
		return state["runtime_id"] == packet.Binding.RuntimeID && baselineGeneration(state) == packet.Binding.Generation && life["state"] == "planning" && life["phase"] == "contracts" && entry == packet.Binding.StageEntry+1
	}
	if packet, found, err := FindContractPacket(state, packetID); err == nil && found {
		return state["runtime_id"] == packet.Binding.RuntimeID && baselineGeneration(state) == packet.Binding.Generation && life["state"] == "planning" && life["phase"] == "tasks" && entry == packet.Binding.StageEntry+1
	}
	if packet, found, err := FindTaskPlanningPacket(state, packetID); err == nil && found {
		return state["runtime_id"] == packet.Binding.RuntimeID && baselineGeneration(state) == packet.Binding.Generation && life["state"] == "document_verification" && life["phase"] == nil && entry == packet.Binding.StageEntry+1
	}
	return false
}

// RecordSubmissionEvaluation records a completed, non-transitioning gate
// evaluation or an obsolete origin. Unknown/failed gate evaluations call
// neither this function nor a transition and retain the pending checkpoint.
func RecordSubmissionEvaluation(root, statePath, journalPath, eventID string, validator CandidateValidator, contexts ...context.Context) (Snapshot, error) {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	store := NewWriter(statePath, journalPath, root, validator).WithContext(ctx)
	snapshot, err := store.Snapshot()
	if err != nil {
		return Snapshot{}, err
	}
	record, followupErr := SubmissionFollowup(snapshot.State, eventID)
	if errors.Is(followupErr, ErrSubmissionFollowupProcessed) {
		return snapshot, nil
	} else if followupErr != nil && !errors.Is(followupErr, ErrSubmissionFollowupOriginChanged) {
		return Snapshot{}, followupErr
	}
	if followupErr == nil && record.OriginState == "planning" && record.OriginPhase == "design" {
		// An evaluated but uncompleted S2 exit remains available to the
		// next normal save at this same responsibility, without another write.
		return snapshot, nil
	}
	if followupErr == nil && record.OriginPhase == "contracts" {
		if _, found, err := FindContractPacket(snapshot.State, record.PacketID); err != nil {
			return Snapshot{}, err
		} else if found {
			// A completed evaluation is not a completed S3 transition. Keep
			// the accepted answer available for the next listener cycle.
			return snapshot, nil
		}
	}
	if followupErr == nil && record.OriginPhase == "tasks" {
		if _, found, err := FindTaskPlanningPacket(snapshot.State, record.PacketID); err != nil {
			return Snapshot{}, err
		} else if found {
			// S4 NotReady is not consumption. Keep the original checkpoint so
			// Read/Recover can reevaluate this same answer and exact entry.
			return snapshot, nil
		}
	}
	next, err := store.UpdateCurrent(Mutation{
		EventID: "evt-submission-control-" + eventID, TransitionID: "SUBMISSION-EVALUATE",
		Event: "submission_evaluated", Actor: "hook_controller", IdempotencyKey: "submission-control:" + eventID,
		RuntimeID: stringValue(snapshot.State["runtime_id"]), RetainLastTransition: true,
		OccurredAt: time.Now().UTC(),
		Apply: func(state map[string]any) error {
			_, err := SubmissionFollowup(state, eventID)
			status := "no_transition"
			if errors.Is(err, ErrSubmissionFollowupOriginChanged) {
				status = "replaced"
			} else if err != nil {
				return err
			}
			return setSubmissionControl(state, eventID, status, "")
		},
	})
	if err != nil {
		latest, readErr := store.Snapshot()
		if readErr == nil {
			if _, checkErr := SubmissionFollowup(latest.State, eventID); errors.Is(checkErr, ErrSubmissionFollowupProcessed) {
				return latest, nil
			}
		}
	}
	return next, err
}
