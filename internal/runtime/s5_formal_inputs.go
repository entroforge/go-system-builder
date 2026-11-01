package runtime

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// S5FormalFact is a read-only projection of a source proven by the accepted
// S4 packet. It is deliberately not written to Runtime.documents.
type S5FormalFact struct {
	Reference workpacket.Reference
	Kind      string
	Status    string
}

// S5FormalInputSet contains the exact inputs an S5 packet freezes and the
// additional source facts the quality gate may recognize under the same S4
// proof. Legacy runtimes without a typed S4 packet retain the old document
// projection.
type S5FormalInputSet struct {
	References []workpacket.Reference
	Facts      []S5FormalFact
}

// S5FormalInputs reconstructs the current S5 input closure. For modern S4 it
// derives unregistered source files and S4 task outputs only from the unique
// accepted Main planning_task tuple, after validating that tuple against the
// original journal. It never guesses sources from arbitrary files on disk.
func S5FormalInputs(root string, state map[string]any, files fileview.Reader) (S5FormalInputSet, error) {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "document_verification" || !PacketEnabled(state) {
		return S5FormalInputSet{}, fmt.Errorf("S5 formal inputs require the document_verification cursor")
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return S5FormalInputSet{}, err
	}
	refs, err := currentS5RegisteredReferences(state, generation, false)
	if err != nil {
		return S5FormalInputSet{}, err
	}
	byPath := map[string]workpacket.Reference{}
	for _, ref := range refs {
		byPath[ref.Path] = ref
	}
	facts := []S5FormalFact{}
	if hasTaskPlanningPacket(state) {
		if files == nil {
			return S5FormalInputSet{}, fmt.Errorf("S5 requires the accepted S4 professional material")
		}
		packet, answer, _, err := acceptedCurrentS4(root, state, files)
		if err != nil {
			return S5FormalInputSet{}, err
		}
		// The accepted S4 answer names its actual outputs. Sources in its
		// original issued packet provide the corresponding reading guidance.
		// No content fingerprints are recomputed or promoted to qualifications.
		add := func(ref workpacket.Reference, kind, status string) {
			if ref.Path == "" {
				return
			}
			if _, exists := byPath[ref.Path]; exists {
				return
			}
			if ref.ID == "" {
				ref.ID = "s4-source:" + ref.Path
			}
			if ref.Version == "" {
				ref.Version = "unversioned"
			}
			byPath[ref.Path] = ref
			facts = append(facts, S5FormalFact{Reference: ref, Kind: kind, Status: status})
		}
		for _, source := range packet.Inputs.Sources {
			add(workpacket.Reference{Path: source.Path, Version: source.Version}, "source", "available")
		}
		paths := append([]string(nil), answer.TaskPaths...)
		if answer.IndexPath != nil {
			paths = append(paths, *answer.IndexPath)
		}
		for _, path := range paths {
			ref, kind := workpacket.Reference{Path: path}, "task"
			if answer.IndexPath != nil && path == *answer.IndexPath {
				kind = "dispatch_plan"
			}
			for _, row := range stateDocuments(state) {
				g, _ := integerField(row, "generation")
				if g == generation && stringValue(row["path"]) == path {
					ref.ID, ref.Version = stringValue(row["id"]), stringValue(row["version"])
					if rowKind := stringValue(row["kind"]); rowKind != "" {
						kind = rowKind
					}
					break
				}
			}
			add(ref, kind, "complete")
		}
	}
	refs = make([]workpacket.Reference, 0, len(byPath))
	for _, ref := range byPath {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	sort.Slice(facts, func(i, j int) bool { return facts[i].Reference.Path < facts[j].Reference.Path })
	return S5FormalInputSet{References: refs, Facts: facts}, nil
}

func hasTaskPlanningPacket(state map[string]any) bool {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["template_id"] == "s4-task-planning-v1" {
			return true
		}
	}
	return false
}

func currentS5RegisteredReferences(state map[string]any, generation int, typedS4 bool) ([]workpacket.Reference, error) {
	documents, err := currentRegisteredFormalInputDocuments(state, generation)
	if err != nil {
		return nil, err
	}
	refs := []workpacket.Reference{}
	seenPath := map[string]workpacket.Reference{}
	for _, document := range documents {
		kind := document.Kind
		if typedS4 && (kind == "task" || kind == "dispatch_plan") {
			continue
		}
		ref := document.Reference
		if ref.ID == "" || ref.Path == "" || ref.Version == "" {
			return nil, fmt.Errorf("current S5 registered document has an incomplete identity")
		}
		if old, exists := seenPath[ref.Path]; exists {
			if old != ref {
				return nil, fmt.Errorf("current S5 document path has conflicting registrations: %s", ref.Path)
			}
			return nil, fmt.Errorf("current S5 document path is registered more than once: %s", ref.Path)
		}
		seenPath[ref.Path] = ref
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	return refs, nil
}

func acceptedCurrentS4(root string, state map[string]any, files fileview.Reader) (workpacket.TaskPlanningPacket, workpacket.TaskPlanningAnswer, map[string]workpacket.Subject, error) {

	baseline, _ := state["baseline"].(map[string]any)
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, err
	}
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil || bound["status"] != "locked" {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("S5 requires the current locked REQ binding")
	}
	req := workpacket.Reference{ID: stringValue(bound["id"]), Path: stringValue(bound["path"]), Version: stringValue(bound["version"])}
	entities, _ := state["entities"].(map[string]any)
	packetRows, _ := entities["work_packets"].([]any)
	latestStageEntry := -1
	type candidate struct {
		packet workpacket.TaskPlanningPacket
		record SubmissionRecord
	}
	candidates := []candidate{}
	for _, raw := range packetRows {
		row, _ := raw.(map[string]any)
		if row == nil || row["template_id"] != "s4-task-planning-v1" {
			continue
		}
		id := stringValue(row["packet_id"])
		p, found, err := FindTaskPlanningPacket(state, id)
		if err != nil {
			return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("decode original S4 issuance: %w", err)
		}
		if !found {
			return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("original S4 issuance is unavailable")
		}
		if p.Binding.RuntimeID != stringValue(state["runtime_id"]) || p.Binding.Generation != generation || (p.Binding.Requirement.ID != req.ID || p.Binding.Requirement.Path != req.Path || p.Binding.Requirement.Version != req.Version) || p.Binding.Stage != "S4" || p.Binding.Owner != "Main" {
			continue
		}
		if p.Binding.StageEntry > latestStageEntry {
			latestStageEntry = p.Binding.StageEntry
		}
		record, found, err := FindSubmission(state, p.ID)
		if err != nil {
			return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("decode original S4 submission: %w", err)
		}
		if !found || record.ControlStatus != "advanced" || record.OriginState != "planning" || record.OriginPhase != "tasks" || record.OriginEntry != p.Binding.StageEntry || record.TransitionEventID == "" || record.RevisionPacketID != "" || record.SupersededBy != "" {
			continue
		}
		candidates = append(candidates, candidate{packet: p, record: record})
	}
	if len(candidates) == 0 {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("no accepted current-generation S4 tuple for the locked REQ")
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].packet.Binding.StageEntry > candidates[j].packet.Binding.StageEntry
	})
	if len(candidates) > 1 && candidates[0].packet.Binding.StageEntry == candidates[1].packet.Binding.StageEntry {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 tuple for the locked REQ is ambiguous")
	}
	selected := candidates[0]
	if selected.packet.Binding.StageEntry != latestStageEntry {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("latest current-generation S4 packet is not the accepted S4 tuple")
	}
	if countSubmissionRows(state, selected.packet.ID) != 1 {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 submission tuple is duplicated")
	}
	evidence, found, err := currentEvidenceByID(state, selected.record.EvidenceID)
	if err != nil || !found {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 evidence is unavailable")
	}
	if evidence["status"] != "valid" || evidence["kind"] != "planning_task" || evidence["responsibility_id"] != "Orchestrator" || stringValue(evidence["path"]) == "" {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 evidence registration differs from the original Main planning_task")
	}
	gen, err := integerField(evidence, "baseline_generation")
	producers, _ := evidence["produced_by"].([]any)
	if err != nil || gen != generation || len(producers) != 1 || producers[0] != "Main" || evidence["invalidated_by"] != nil || evidence["invalidation_rule"] != nil || evidence["invalidation_reason"] != nil {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 evidence is invalidated or has a different producer/generation")
	}
	path := stringValue(evidence["path"])
	data, err := files.ReadFile(path)
	if err != nil {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("read accepted S4 envelope %s: %w", path, err)
	}
	var envelope struct {
		SubjectRefs []workpacket.Subject          `json:"subject_refs"`
		Answer      workpacket.TaskPlanningAnswer `json:"task_planning_answer"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("decode accepted S4 envelope: %w", err)
	}
	subjects := map[string]workpacket.Subject{}
	for _, subject := range envelope.SubjectRefs {
		if subject.Path == "" {
			return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 envelope contains an empty subject path")
		}
		if _, duplicate := subjects[subject.Path]; duplicate {
			return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 envelope repeats subject %s", subject.Path)
		}
		subjects[subject.Path] = subject
	}
	return selected.packet, envelope.Answer, subjects, nil
}

func stateDocuments(state map[string]any) []map[string]any {
	raw, _ := state["documents"].([]any)
	rows := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		row, _ := item.(map[string]any)
		if row != nil {
			rows = append(rows, row)
		}
	}
	return rows
}

func countSubmissionRows(state map[string]any, packetID string) int {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	count := 0
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["packet_id"] == packetID {
			count++
		}
	}
	return count
}

func currentEvidenceByID(state map[string]any, id string) (map[string]any, bool, error) {
	rows, _ := state["evidence"].([]any)
	var found map[string]any
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil || row["id"] != id {
			continue
		}
		if found != nil {
			return nil, false, fmt.Errorf("duplicate evidence id %s", id)
		}
		found = row
	}
	return found, found != nil, nil
}
