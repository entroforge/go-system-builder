package runtime

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func ValidateIssuedTaskPlanningPacket(root string, state map[string]any, p workpacket.TaskPlanningPacket, files fileview.Reader) error {
	return ValidateTaskPlanningPacket(root, state, p, files)
}

func prepareTaskPlanningSubmission(root string, state map[string]any, req EvidenceRequest, data []byte) (SubmissionRecord, error) {
	s := req.Submission
	p, found, err := FindTaskPlanningPacket(state, s.PacketID)
	if err != nil || !found {
		return SubmissionRecord{}, fmt.Errorf("S4 submission has no original issuance")
	}
	if req.Kind != "planning_task" {
		return SubmissionRecord{}, fmt.Errorf("S4 recording kind differs")
	}
	var draft workpacket.TaskPlanningDraft
	if err := json.Unmarshal(s.AnswerJSON, &draft); err != nil {
		return SubmissionRecord{}, err
	}
	answer := draft.Answer
	if err := validateStoredTaskPlanningAnswer(answer); err != nil {
		return SubmissionRecord{}, err
	}
	digest, err := workpacket.TaskPlanningAnswerDigest(answer)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if s.ContentHash != "" && s.ContentHash != digest {
		return SubmissionRecord{}, fmt.Errorf("S4 content digest differs")
	}
	prior, accepted, err := FindSubmission(state, p.ID)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if accepted {
		if prior.ContentHash != digest {
			return prior, ErrSubmissionConflict
		}
		if prior.EvidenceID != req.ID {
			return prior, fmt.Errorf("S4 replay original domain differs")
		}
		if err := ValidateTaskPlanningEnvelope(p, req.ID, answer, data); err != nil {
			return prior, err
		}
		rows, _ := state["evidence"].([]any)
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			if row["id"] == req.ID && row["kind"] == req.Kind && row["path"] == req.Path && row["sha256"] == workpacket.Digest(data) {
				return prior, errSubmissionAccepted
			}
		}
		return prior, fmt.Errorf("S4 replay original evidence differs")
	}
	life, _ := state["lifecycle"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	entry, entryErr := integerField(life, "phase_revision")
	generation, generationErr := integerField(baseline, "generation")
	if entryErr != nil || generationErr != nil || state["runtime_id"] != p.Binding.RuntimeID || life["state"] != "planning" || life["phase"] != "tasks" || entry != p.Binding.StageEntry || generation != p.Binding.Generation {
		return SubmissionRecord{}, fmt.Errorf("S4 questionnaire is not the current responsibility")
	}
	if err := ValidateTaskPlanningEnvelope(p, req.ID, answer, data); err != nil {
		return SubmissionRecord{}, err
	}
	key := workpacket.Digest([]byte("submission:" + p.ID))
	return SubmissionRecord{Key: key, PacketID: p.ID, Owner: p.Binding.Owner, ContentHash: digest, EvidenceID: req.ID, FollowupID: workpacket.Digest([]byte(key + ":after-domain")), ControlStatus: "pending", OriginState: "planning", OriginPhase: "tasks", OriginEntry: p.Binding.StageEntry}, nil
}

// ValidateTaskPlanningEnvelope checks readable recorded facts, not historical source proof.
func ValidateTaskPlanningEnvelope(p workpacket.TaskPlanningPacket, id string, answer workpacket.TaskPlanningAnswer, data []byte) error {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return err
	}
	var value struct {
		EvidenceID string                        `json:"evidence_id"`
		Kind       string                        `json:"kind"`
		RuntimeID  string                        `json:"runtime_id"`
		Generation int                           `json:"baseline_generation"`
		Subjects   []workpacket.Subject          `json:"subject_refs"`
		Conclusion string                        `json:"conclusion"`
		CreatedAt  string                        `json:"created_at"`
		Answer     workpacket.TaskPlanningAnswer `json:"task_planning_answer"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value.EvidenceID != id || value.Kind != "planning_task" || value.RuntimeID != p.Binding.RuntimeID || value.Generation != p.Binding.Generation {
		return fmt.Errorf("S4 record differs from current questionnaire association")
	}
	if _, err := time.Parse(time.RFC3339Nano, value.CreatedAt); err != nil {
		return err
	}
	if err := validateStoredTaskPlanningAnswer(value.Answer); err != nil {
		return err
	}
	if value.Conclusion != *value.Answer.Conclusion {
		return fmt.Errorf("S4 record conclusion differs from answer")
	}
	a, err := workpacket.TaskPlanningAnswerDigest(answer)
	if err != nil {
		return err
	}
	h, err := workpacket.TaskPlanningAnswerDigest(value.Answer)
	if err != nil || a != h {
		return fmt.Errorf("S4 evidence differs from professional answer")
	}
	return nil
}

func validateStoredTaskPlanningAnswer(answer workpacket.TaskPlanningAnswer) error {
	if answer.Conclusion == nil || *answer.Conclusion != "pass" || answer.IndexPath == nil || !safeTaskPlanningPath(*answer.IndexPath) || len(answer.TaskPaths) == 0 {
		return fmt.Errorf("S4 record requires conclusion, index and TASK paths")
	}
	seen := map[string]bool{*answer.IndexPath: true}
	for _, path := range answer.TaskPaths {
		if !safeTaskPlanningPath(path) || filepath.ToSlash(filepath.Dir(path)) != taskPlanningDirectory || !strings.HasPrefix(filepath.Base(path), "TASK-") || !strings.HasSuffix(path, ".md") || seen[path] {
			return fmt.Errorf("S4 record has invalid or duplicate TASK path")
		}
		seen[path] = true
	}
	return nil
}

func FindTaskPlanningPacket(state map[string]any, id string) (workpacket.TaskPlanningPacket, bool, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["packet_id"] != id || row["template_id"] != "s4-task-planning-v1" {
			continue
		}
		data, err := json.Marshal(row)
		if err != nil {
			return workpacket.TaskPlanningPacket{}, false, err
		}
		var p workpacket.TaskPlanningPacket
		err = json.Unmarshal(data, &p)
		return p, true, err
	}
	return workpacket.TaskPlanningPacket{}, false, nil
}

func RecordTaskPlanningPacket(root, statePath, journalPath string, p workpacket.TaskPlanningPacket, files fileview.Reader, validator CandidateValidator) (Snapshot, error) {
	writer := NewWriter(statePath, journalPath, root, validator)
	snap, err := writer.Snapshot()
	if err != nil {
		return Snapshot{}, err
	}
	if err := ValidateIssuedTaskPlanningPacket(root, snap.State, p, files); err != nil {
		return Snapshot{}, err
	}
	if old, found, err := FindTaskPlanningPacket(snap.State, p.ID); err != nil {
		return Snapshot{}, err
	} else if found {
		if !reflect.DeepEqual(old, p) {
			return Snapshot{}, fmt.Errorf("contract packet identity conflict")
		}
		return snap, nil
	}
	data, _ := json.Marshal(p)
	var row map[string]any
	json.Unmarshal(data, &row)
	cursor := map[string]any{"state": "planning", "phase": "tasks"}
	next, err := writer.UpdateCurrent(Mutation{EventID: "evt-task-planning-packet-" + p.ID, TransitionID: "PACKET-ISSUE", Event: "work_packet_issued", Actor: "orchestrator", IdempotencyKey: "packet:" + p.ID, RuntimeID: p.Binding.RuntimeID, From: cursor, To: cursor, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		if err := ValidateIssuedTaskPlanningPacket(root, state, p, files); err != nil {
			return err
		}
		if _, found, err := FindTaskPlanningPacket(state, p.ID); err != nil {
			return err
		} else if found {
			return errSubmissionAccepted
		}
		entities := state["entities"].(map[string]any)
		rows, _ := entities["work_packets"].([]any)
		for _, raw := range rows {
			old, _ := raw.(map[string]any)
			if old["template_id"] != "s4-task-planning-v1" {
				continue
			}
			binding, _ := old["binding"].(map[string]any)
			if stringValue(binding["runtime_id"]) == p.Binding.RuntimeID && numberBindingGeneration(binding) == p.Binding.Generation && stringValue(binding["owner"]) == "Main" {
				entry, err := integerField(binding, "stage_entry")
				if err != nil {
					return err
				}
				if entry == p.Binding.StageEntry {
					return fmt.Errorf("contract stage already has a different frozen issuance")
				}
			}
		}
		entities["work_packets"] = append(rows, row)
		return nil
	}})
	if err != nil {
		latest, readErr := writer.Snapshot()
		if readErr == nil {
			prior, found, readErr := FindTaskPlanningPacket(latest.State, p.ID)
			if readErr == nil && found && reflect.DeepEqual(prior, p) {
				return latest, nil
			}
		}
	}
	return next, err
}
