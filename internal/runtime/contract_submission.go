package runtime

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func ValidateIssuedContractPacket(root string, state map[string]any, p workpacket.ContractPacket, files fileview.Reader) error {
	return ValidateContractPacket(root, state, p, files)
}

// contractPacketHasLegacySharedModelInputs preserves packets issued before
// S3 shared-model output dependencies moved from issuance into the original
// submission checker. The upstream closure and directory manifests still
// match exactly; only persisted schema/example JSON refs from the former
// output closure may extend that source set.
func contractPacketHasLegacySharedModelInputs(p workpacket.ContractPacket, upstream workpacket.ContractInputs) bool {
	if !reflect.DeepEqual(p.Inputs.Designs, upstream.Designs) || !reflect.DeepEqual(p.Inputs.Directories, upstream.Directories) {
		return false
	}
	upstreamSources := make(map[string]workpacket.Subject, len(upstream.Sources))
	for _, source := range upstream.Sources {
		upstreamSources[source.Path] = source
	}
	issuedSources := make(map[string]workpacket.Subject, len(p.Inputs.Sources))
	for _, source := range p.Inputs.Sources {
		if _, duplicate := issuedSources[source.Path]; duplicate {
			return false
		}
		issuedSources[source.Path] = source
	}
	for path, source := range upstreamSources {
		if issuedSources[path] != source {
			return false
		}
	}
	legacyCount := 0
	for path, source := range issuedSources {
		if _, exists := upstreamSources[path]; exists {
			continue
		}
		if !safeContractDependency(path) || !strings.HasSuffix(strings.ToLower(path), ".json") || source.Version != "" {
			return false
		}
		legacyCount++
	}
	return legacyCount > 0
}

func prepareContractSubmission(root string, state map[string]any, req EvidenceRequest, data []byte) (SubmissionRecord, error) {
	s := req.Submission
	p, found, err := FindContractPacket(state, s.PacketID)
	if err != nil || !found {
		return SubmissionRecord{}, fmt.Errorf("S3 submission has no original issuance")
	}
	if req.Kind != "planning_contract" {
		return SubmissionRecord{}, fmt.Errorf("S3 submission requires planning_contract evidence")
	}
	answer, issues := workpacket.ValidateContractAnswer(p, s.AnswerJSON)
	if len(issues) > 0 {
		return SubmissionRecord{}, fmt.Errorf("contract answer %s: %s", issues[0].Pointer, issues[0].Expected)
	}
	prior, accepted, err := FindSubmission(state, p.ID)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if accepted {
		if prior.Owner != "Main" || prior.EvidenceID != req.ID {
			return prior, fmt.Errorf("S3 replay original domain differs")
		}
		rows, _ := state["evidence"].([]any)
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			if row["id"] != prior.EvidenceID || row["kind"] != req.Kind {
				continue
			}
			// Only the registered winner's professional answer can distinguish
			// a same-answer replay from an actual new authoring expression.
			original, err := workpacket.ReadFile(root, stringValue(row["path"]), workpacket.ContractEvidenceReadLimit(p))
			if err != nil {
				return prior, fmt.Errorf("read original S3 professional answer: %w", err)
			}
			previous, err := ContractAnswerFromEvidence(original)
			if err != nil {
				return prior, err
			}
			if !reflect.DeepEqual(answer, previous) {
				return prior, ErrSubmissionConflict
			}
			return prior, errSubmissionAccepted
		}
		return prior, fmt.Errorf("S3 replay original evidence is unavailable")
	}
	current, _, err := CurrentContractBinding(state)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if !samePlanningResponsibility(current, p.Binding) {
		return SubmissionRecord{}, fmt.Errorf("S3 submission does not describe the current planning responsibility")
	}
	if s.Authorize == nil {
		return SubmissionRecord{}, fmt.Errorf("需要原 consumer 执行实际合同专业检查，请保存当前准确问卷：%s", p.AnswerPath)
	}
	if err := s.Authorize(root, state); err != nil {
		return SubmissionRecord{}, err
	}
	if s.EvidenceJSON == nil {
		// An existing external report still has to contain the professional
		// answer being received. The ordinary original consumer supplies its
		// own prepared report directly to the Writer.
		previous, err := ContractAnswerFromEvidence(data)
		if err != nil || !reflect.DeepEqual(answer, previous) {
			return SubmissionRecord{}, fmt.Errorf("S3 report does not contain the original professional answer")
		}
	}
	key := "submission:" + p.ID
	return SubmissionRecord{Key: key, PacketID: p.ID, Owner: "Main", EvidenceID: req.ID, FollowupID: "followup:" + p.ID, ControlStatus: "pending", OriginState: "planning", OriginPhase: "contracts", OriginEntry: p.Binding.StageEntry}, nil
}

// ContractAnswerFromEvidence reads only the professional answer needed by an
// actual successor or by comparison with a newly saved answer. Other machine
// fields and unknown professional extensions remain untouched and unqualified.
func ContractAnswerFromEvidence(data []byte) (workpacket.ContractAnswer, error) {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return workpacket.ContractAnswer{}, err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return workpacket.ContractAnswer{}, err
	}
	raw, ok := envelope["contract_answer"]
	if !ok || string(raw) == "null" {
		return workpacket.ContractAnswer{}, fmt.Errorf("original S3 professional answer is unavailable")
	}
	var answer workpacket.ContractAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return workpacket.ContractAnswer{}, fmt.Errorf("original S3 professional answer is malformed: %w", err)
	}
	return answer, nil
}

func FindContractPacket(state map[string]any, id string) (workpacket.ContractPacket, bool, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["packet_id"] != id || row["template_id"] != "s3-contract-v1" {
			continue
		}
		data, err := json.Marshal(row)
		if err != nil {
			return workpacket.ContractPacket{}, false, err
		}
		var p workpacket.ContractPacket
		err = json.Unmarshal(data, &p)
		return p, true, err
	}
	return workpacket.ContractPacket{}, false, nil
}

func RecordContractPacket(root, statePath, journalPath string, p workpacket.ContractPacket, files fileview.Reader, validator CandidateValidator) (Snapshot, error) {
	writer := NewWriter(statePath, journalPath, root, validator)
	snap, err := writer.Snapshot()
	if err != nil {
		return Snapshot{}, err
	}
	if err := ValidateIssuedContractPacket(root, snap.State, p, files); err != nil {
		return Snapshot{}, err
	}
	if old, found, err := FindContractPacket(snap.State, p.ID); err != nil {
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
	cursor := map[string]any{"state": "planning", "phase": "contracts"}
	next, err := writer.UpdateCurrent(Mutation{EventID: "evt-contract-packet-" + p.ID, TransitionID: "PACKET-ISSUE", Event: "work_packet_issued", Actor: "orchestrator", IdempotencyKey: "packet:" + p.ID, RuntimeID: p.Binding.RuntimeID, From: cursor, To: cursor, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		if err := ValidateIssuedContractPacket(root, state, p, files); err != nil {
			return err
		}
		if _, found, err := FindContractPacket(state, p.ID); err != nil {
			return err
		} else if found {
			return errSubmissionAccepted
		}
		entities := state["entities"].(map[string]any)
		rows, _ := entities["work_packets"].([]any)
		for _, raw := range rows {
			old, _ := raw.(map[string]any)
			if old["template_id"] != "s3-contract-v1" {
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
			prior, found, readErr := FindContractPacket(latest.State, p.ID)
			if readErr == nil && found && reflect.DeepEqual(prior, p) {
				return latest, nil
			}
		}
	}
	return next, err
}
