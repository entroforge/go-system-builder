package qualitygate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Project the current original reception into TR-002's document facts. The
// original save owns professional checks; this read does not replay them.
func verifiedTaskPlanningPacketFacts(input Input) ([]documentFact, bool, error) {
	state := input.Snapshot.State
	if state == nil {
		return nil, false, nil
	}
	binding, err := runtime.CurrentTaskPlanningBinding(state)
	if err != nil {
		return nil, false, fmt.Errorf("resolve current S4 record: %w", err)
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	var record runtime.SubmissionRecord
	found := false
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["origin_state"] != "planning" || row["origin_phase"] != "tasks" || intValue(row["origin_entry"]) != binding.StageEntry {
			continue
		}
		if found {
			return nil, true, fmt.Errorf("multiple current S4 reception records")
		}
		id, _ := row["packet_id"].(string)
		record, found, err = runtime.FindSubmission(state, id)
		if err != nil {
			return nil, true, fmt.Errorf("read current S4 reception: %w", err)
		}
	}
	if !found {
		return nil, false, nil
	}
	fail := func(err error) ([]documentFact, bool, error) { return nil, true, err }
	if input.Files == nil {
		return fail(fmt.Errorf("S4 recorded materials require a file view"))
	}
	packet, ok, err := runtime.FindTaskPlanningPacket(state, record.PacketID)
	if err != nil || !ok {
		return fail(fmt.Errorf("current S4 recorded questionnaire unavailable: %s", record.PacketID))
	}
	evidence, ok, err := contractEvidenceRow(state, record.EvidenceID)
	if err != nil {
		return fail(err)
	}
	if !ok {
		return fail(fmt.Errorf("current S4 reception has no recorded planning_task envelope: %s", record.EvidenceID))
	}
	path := stringValue(evidence["path"])
	if !safeTaskPlanningEvidencePath(path) {
		return fail(fmt.Errorf("current S4 envelope path is unreadable: %s", path))
	}
	data, err := input.Files.ReadFile(path)
	if err != nil {
		return fail(fmt.Errorf("read recorded S4 envelope %s: %w", path, err))
	}
	var envelope struct {
		Subjects []workpacket.Subject          `json:"subject_refs"`
		Answer   workpacket.TaskPlanningAnswer `json:"task_planning_answer"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fail(fmt.Errorf("read recorded S4 envelope %s: %w", path, err))
	}
	if envelope.Answer.IndexPath == nil {
		return fail(fmt.Errorf("recorded S4 answer has no index path"))
	}
	index := *envelope.Answer.IndexPath
	tasks := map[string]bool{}
	for _, path := range envelope.Answer.TaskPaths {
		tasks[path] = true
	}
	registered := map[string]bool{packet.Binding.Requirement.Path: true}
	for _, ref := range packet.Inputs.Contracts {
		registered[ref.Path] = true
	}
	subjects := map[string]workpacket.Subject{}
	facts := []documentFact{}
	for _, subject := range envelope.Subjects {
		subjects[subject.Path] = subject
		if subject.Path == index || tasks[subject.Path] || registered[subject.Path] {
			continue
		}
		if _, err := input.Files.ReadFile(subject.Path); err != nil {
			return fail(fmt.Errorf("read recorded S4 source %s: %w", subject.Path, err))
		}
		facts = append(facts, documentFact{ID: "task-planning-source:" + subject.Path, Kind: "source", Path: subject.Path, Version: subject.Version, SHA256: subject.SHA256, Status: "frozen", Generation: binding.Generation})
	}
	for _, path := range append([]string{index}, envelope.Answer.TaskPaths...) {
		subject, ok := subjects[path]
		if !ok {
			return fail(fmt.Errorf("recorded S4 material has no subject: %s", path))
		}
		material, err := input.Files.ReadFile(path)
		if err != nil {
			return fail(fmt.Errorf("read recorded S4 material %s: %w", path, err))
		}
		kind, status, prefix := "task", parseMarkdownStatusField(string(material)), "task-planning-output:"
		if path == index {
			kind, status, prefix = "task_index", "complete", "task-planning-index:"
		}
		id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		facts = append(facts, documentFact{ID: prefix + id, Kind: kind, Path: path, Version: subject.Version, SHA256: subject.SHA256, Status: status, Generation: binding.Generation})
	}
	return facts, true, nil
}

func safeTaskPlanningEvidencePath(value string) bool {
	clean := filepath.Clean(value)
	return value != "" && !filepath.IsAbs(value) && filepath.ToSlash(clean) == value && clean != "." && clean != ".." &&
		!strings.HasPrefix(clean, ".."+string(filepath.Separator)) && len(value) < 4096 && !strings.ContainsAny(value, "\\\x00")
}
