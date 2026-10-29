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
	if err := ValidateContractPacket(root, state, p, files); err != nil {
		return err
	}
	inputs, err := ResolveContractInputs(root, state, files)
	if err != nil {
		return err
	}
	expected, err := workpacket.BuildContracts(p.Binding, inputs)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, p) && !contractPacketHasLegacySharedModelInputs(p, inputs) {
		return fmt.Errorf("S3 complete original source closure differs from issuance")
	}
	return nil
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
	digest, err := workpacket.ContractAnswerDigest(answer)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if s.ContentHash != "" && s.ContentHash != digest {
		return SubmissionRecord{}, fmt.Errorf("S3 content digest differs")
	}
	prior, accepted, err := FindSubmission(state, p.ID)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if accepted {
		if prior.ContentHash != digest {
			return prior, ErrSubmissionConflict
		}
		if prior.Owner != "Main" || prior.EvidenceID != req.ID {
			return prior, fmt.Errorf("S3 replay original domain differs")
		}
		if err := ValidateContractEnvelope(p, req.ID, answer, data); err != nil {
			return prior, err
		}
		rows, _ := state["evidence"].([]any)
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			if row["id"] == req.ID && row["kind"] == req.Kind && row["path"] == req.Path && row["sha256"] == workpacket.Digest(data) {
				return prior, errSubmissionAccepted
			}
		}
		return prior, fmt.Errorf("S3 replay original evidence differs")
	}
	current, _, err := CurrentContractBinding(state)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if current != p.Binding {
		return SubmissionRecord{}, fmt.Errorf("S3 submission does not describe the current planning responsibility")
	}
	if s.Authorize == nil {
		return SubmissionRecord{}, fmt.Errorf("需要原 consumer 执行实际合同专业检查，请保存当前准确问卷：%s", p.AnswerPath)
	}
	if err := s.Authorize(root, state); err != nil {
		return SubmissionRecord{}, err
	}
	if err := ValidateContractEnvelope(p, req.ID, answer, data); err != nil {
		return SubmissionRecord{}, err
	}
	key := workpacket.Digest([]byte("submission:" + p.ID))
	return SubmissionRecord{Key: key, PacketID: p.ID, Owner: "Main", ContentHash: digest, EvidenceID: req.ID, FollowupID: workpacket.Digest([]byte(key + ":after-domain")), ControlStatus: "pending", OriginState: "planning", OriginPhase: "contracts", OriginEntry: p.Binding.StageEntry}, nil
}

// ValidateContractEnvelope verifies historical professional facts without
// reinterpreting them against a later project cursor or changing authoring file.
func ValidateContractEnvelope(p workpacket.ContractPacket, id string, answer workpacket.ContractAnswer, data []byte) error {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, key := range []string{"schema_version", "evidence_id", "kind", "runtime_id", "baseline_generation", "producer_agent_id", "producer_responsibility", "subject_refs", "conclusion", "created_at", "contract_answer"} {
		if _, ok := raw[key]; !ok {
			return fmt.Errorf("S3 planning envelope missing %s", key)
		}
	}
	if len(raw) != 11 {
		return fmt.Errorf("S3 planning envelope has unsupported fields")
	}
	var historical workpacket.ContractAnswer
	if err := json.Unmarshal(raw["contract_answer"], &historical); err != nil {
		return err
	}
	draft := p.Draft
	draft.Answer = historical
	encoded, _ := json.Marshal(draft)
	historical, issues := workpacket.ValidateContractAnswer(p, encoded)
	if len(issues) > 0 {
		return fmt.Errorf("S3 historical answer invalid")
	}
	a, err := workpacket.ContractAnswerDigest(answer)
	if err != nil {
		return err
	}
	h, err := workpacket.ContractAnswerDigest(historical)
	if err != nil || a != h {
		return fmt.Errorf("S3 evidence differs from professional answer")
	}
	var subjects []workpacket.Subject
	if err := json.Unmarshal(raw["subject_refs"], &subjects); err != nil {
		return err
	}
	byPath := map[string]workpacket.Subject{}
	for _, ref := range subjects {
		if _, duplicate := byPath[ref.Path]; duplicate {
			return fmt.Errorf("S3 evidence duplicate subjects")
		}
		byPath[ref.Path] = ref
	}
	contractPaths := map[string]bool{*answer.IndexPath: true}
	for _, path := range answer.ContractPaths {
		contractPaths[path] = true
	}
	frozenPaths := map[string]bool{p.Binding.Requirement.Path: true}
	for _, ref := range p.Inputs.Designs {
		frozenPaths[ref.Path] = true
	}
	for _, ref := range p.Inputs.Sources {
		frozenPaths[ref.Path] = true
	}
	artifacts := []workpacket.Reference{}
	contractRefs := map[string]workpacket.Subject{}
	sharedModelRefs := []workpacket.Reference{}
	for _, ref := range subjects {
		if frozenPaths[ref.Path] {
			continue
		}
		if contractPaths[ref.Path] {
			contractRefs[ref.Path] = ref
			continue
		}
		sharedModelRefs = append(sharedModelRefs, workpacket.Reference{Path: ref.Path, Version: ref.Version, SHA256: ref.SHA256})
	}
	for path := range contractPaths {
		ref, ok := contractRefs[path]
		if !ok {
			return fmt.Errorf("S3 evidence missing delivered contract")
		}
		artifacts = append(artifacts, workpacket.Reference{Path: path, Version: ref.Version, SHA256: ref.SHA256})
	}
	artifacts = append(artifacts, sharedModelRefs...)
	var value map[string]any
	json.Unmarshal(data, &value)
	at, err := time.Parse(time.RFC3339Nano, stringValue(value["created_at"]))
	if err != nil {
		return err
	}
	expected, err := workpacket.ContractEvidence(p, id, artifacts, answer, at)
	if err != nil {
		return err
	}
	expected["created_at"] = value["created_at"]
	if progressCanonicalJSON(value) != progressCanonicalJSON(expected) {
		return fmt.Errorf("S3 envelope differs from frozen original subjects and professional answer")
	}
	return nil
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
