package qualitygate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

type verifiedContractSubmission struct {
	packet   workpacket.ContractPacket
	answer   workpacket.ContractAnswer
	subjects map[string]workpacket.Subject
}

// verifiedContractPacketFacts projects the original consumer's accepted facts.
// Professional checks ran at reception; this read does not recertify source
// bytes, directory membership, or the current complete shared-model universe.
func verifiedContractPacketFacts(input Input) ([]documentFact, bool, error) {
	verified, accepted, err := readAcceptedContractSubmission(input)
	if err != nil || !accepted {
		return nil, accepted, err
	}
	facts, err := contractPacketSourceFacts(verified.packet, verified.subjects)
	if err != nil {
		return nil, true, err
	}
	if verified.answer.IndexPath == nil || *verified.answer.IndexPath == "" {
		return nil, true, fmt.Errorf("original S3 answer has no contract index path")
	}
	indexPath := *verified.answer.IndexPath
	contractPaths := map[string]bool{}
	for _, path := range verified.answer.ContractPaths {
		contractPaths[path] = true
	}
	for _, path := range append([]string{indexPath}, verified.answer.ContractPaths...) {
		if _, ok := verified.subjects[path]; !ok {
			return nil, true, fmt.Errorf("original S3 accepted delivery is missing subject %s", path)
		}
	}
	inputs := map[string]bool{verified.packet.Binding.Requirement.Path: true}
	for _, ref := range verified.packet.Inputs.Designs {
		inputs[ref.Path] = true
	}
	for _, ref := range verified.packet.Inputs.Sources {
		inputs[ref.Path] = true
	}
	paths := make([]string, 0, len(verified.subjects))
	for path := range verified.subjects {
		if !inputs[path] {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		subject := verified.subjects[path]
		kind, status, id := "source", "frozen", "shared-model:"+path
		artifactID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		switch {
		case path == indexPath:
			kind, status, id = "contract_index", "locked", "contract-index:"+artifactID
		case contractPaths[path]:
			kind, status, id = "contract", "locked", "contract:"+artifactID
		case strings.HasPrefix(filepath.ToSlash(path), "docs/dev/contracts/"):
			return nil, true, fmt.Errorf("original S3 receipt has an undeclared contract output: %s", path)
		}
		facts = append(facts, documentFact{ID: id, Kind: kind, Path: path, Version: subject.Version, SHA256: subject.SHA256, Status: status, Generation: verified.packet.Binding.Generation})
	}
	return facts, true, nil
}

func readAcceptedContractSubmission(input Input) (*verifiedContractSubmission, bool, error) {
	state := input.Snapshot.State
	if !runtime.PacketEnabled(state) {
		return nil, false, nil
	}
	issuanceRows := currentContractIssuanceRows(state)
	if len(issuanceRows) == 0 {
		return nil, false, nil
	}
	acceptedIssuance := false
	for _, row := range issuanceRows {
		packetID := stringValue(row["packet_id"])
		if packetID != "" && countContractSubmissionRows(state, packetID) > 0 {
			acceptedIssuance = true
		}
	}
	if !acceptedIssuance {
		// A prepared packet without a received answer is the ordinary S3
		// questionnaire state. Let the existing disk fallback report missing
		// contract evidence until Main saves it.
		return nil, false, nil
	}
	if input.Root == "" || state == nil || input.Files == nil {
		return nil, true, fmt.Errorf("accepted S3 source projection requires the original root, runtime snapshot, and file view")
	}

	binding, _, err := runtime.CurrentContractBinding(state)
	if err != nil {
		return nil, true, fmt.Errorf("resolve current S3 source binding: %w", err)
	}
	var packet workpacket.ContractPacket
	packetCount := 0
	for _, row := range issuanceRows {
		id := stringValue(row["packet_id"])
		if id == "" {
			return nil, true, fmt.Errorf("current S3 issuance row has no packet identity")
		}
		candidate, found, findErr := runtime.FindContractPacket(state, id)
		if findErr != nil {
			return nil, true, fmt.Errorf("decode original S3 issuance %s: %w", id, findErr)
		}
		if !found {
			return nil, true, fmt.Errorf("original S3 issuance %s is unavailable", id)
		}
		if candidate.Binding.RuntimeID == binding.RuntimeID &&
			candidate.Binding.Generation == binding.Generation &&
			candidate.Binding.StageEntry == binding.StageEntry &&
			candidate.Binding.Stage == "S3" && candidate.Binding.Owner == "Main" {
			if candidate.Binding.Requirement != binding.Requirement {
				return nil, true, fmt.Errorf("current S3 questionnaire requirement differs from the current bound REQ")
			}
			packet = candidate
			packetCount++
		}
	}
	if packetCount != 1 {
		return nil, true, fmt.Errorf("expected exactly one original S3 issuance for the current binding; found %d", packetCount)
	}

	if countContractSubmissionRows(state, packet.ID) != 1 {
		return nil, true, fmt.Errorf("expected exactly one accepted S3 submission row")
	}
	submission, accepted, err := runtime.FindSubmission(state, packet.ID)
	if err != nil {
		return nil, true, fmt.Errorf("decode current S3 submission: %w", err)
	}
	if !accepted {
		return nil, false, nil
	}
	expectedKey := workpacket.Digest([]byte("submission:" + packet.ID))
	if submission.Key != expectedKey || submission.PacketID != packet.ID || submission.Owner != "Main" ||
		submission.EvidenceID == "" || submission.ContentHash == "" ||
		submission.FollowupID != workpacket.Digest([]byte(expectedKey+":after-domain")) ||
		submission.OriginState != "planning" || submission.OriginPhase != "contracts" ||
		submission.OriginEntry != packet.Binding.StageEntry || submission.RevisionPacketID != "" || submission.SupersededBy != "" {
		return nil, true, fmt.Errorf("current S3 submission is not the original Main planning.contracts reception")
	}

	evidence, found, err := contractEvidenceRow(state, submission.EvidenceID)
	if err != nil {
		return nil, true, fmt.Errorf("resolve original S3 evidence registration: %w", err)
	}
	if !found {
		return nil, true, fmt.Errorf("original S3 submission has no evidence registration")
	}
	if evidence["kind"] != "planning_contract" || evidence["status"] != "valid" || evidence["invalidated_by"] != nil ||
		stringValue(evidence["responsibility_id"]) != "Orchestrator" ||
		intValue(evidence["baseline_generation"]) != packet.Binding.Generation ||
		!singleProducer(evidence["produced_by"], "Main") {
		return nil, true, fmt.Errorf("original S3 evidence registration differs from the accepted Main planning_contract")
	}
	evidencePath := stringValue(evidence["path"])
	evidenceSHA := stringValue(evidence["sha256"])
	if !safeContractEvidencePath(evidencePath) || evidenceSHA == "" {
		return nil, true, fmt.Errorf("original S3 evidence registration has an invalid path or digest")
	}
	envelopeBytes, err := input.Files.ReadFile(evidencePath)
	if err != nil {
		return nil, true, fmt.Errorf("read original S3 planning_contract envelope %s: %w", evidencePath, err)
	}
	if sha256Hex(envelopeBytes) != evidenceSHA {
		return nil, true, fmt.Errorf("original S3 planning_contract envelope differs from its registered hash: %s", evidencePath)
	}
	var envelope struct {
		EvidenceID             string                    `json:"evidence_id"`
		Kind                   string                    `json:"kind"`
		RuntimeID              string                    `json:"runtime_id"`
		BaselineGeneration     int                       `json:"baseline_generation"`
		ProducerAgentID        string                    `json:"producer_agent_id"`
		ProducerResponsibility string                    `json:"producer_responsibility"`
		SubjectRefs            []workpacket.Subject      `json:"subject_refs"`
		Conclusion             string                    `json:"conclusion"`
		ContractAnswer         workpacket.ContractAnswer `json:"contract_answer"`
	}
	if err := json.Unmarshal(envelopeBytes, &envelope); err != nil {
		return nil, true, fmt.Errorf("decode original S3 planning_contract envelope %s: %w", evidencePath, err)
	}
	if envelope.EvidenceID != submission.EvidenceID || envelope.Kind != "planning_contract" ||
		envelope.RuntimeID != packet.Binding.RuntimeID || envelope.BaselineGeneration != packet.Binding.Generation ||
		envelope.ProducerAgentID != "Main" || envelope.ProducerResponsibility != "Orchestrator" || envelope.Conclusion != "pass" {
		return nil, true, fmt.Errorf("original S3 envelope header differs from the accepted Main binding: %s", evidencePath)
	}
	answerDigest, err := workpacket.ContractAnswerDigest(envelope.ContractAnswer)
	if err != nil || answerDigest != submission.ContentHash {
		return nil, true, fmt.Errorf("original S3 professional answer differs from the accepted submission hash: %s", evidencePath)
	}

	subjects := make(map[string]workpacket.Subject, len(envelope.SubjectRefs))
	for _, subject := range envelope.SubjectRefs {
		if subject.Path == "" || subject.SHA256 == "" {
			return nil, true, fmt.Errorf("original S3 envelope contains an unreadable subject reference: %s", subject.Path)
		}
		if _, duplicate := subjects[subject.Path]; duplicate {
			return nil, true, fmt.Errorf("original S3 envelope repeats subject %s", subject.Path)
		}
		subjects[subject.Path] = subject
	}
	if verifiedFileView, ok := input.Files.(interface{ Verify() error }); ok {
		if err := verifiedFileView.Verify(); err != nil {
			return nil, true, fmt.Errorf("original S3 file view moved during source projection: %w", err)
		}
	}
	return &verifiedContractSubmission{
		packet: packet, answer: envelope.ContractAnswer, subjects: subjects,
	}, true, nil
}

func contractPacketSourceFacts(packet workpacket.ContractPacket, subjects map[string]workpacket.Subject) ([]documentFact, error) {
	// Preserve the source associations recorded by the original consumer;
	// historical S2 rows stay intact and no source files are reread here.
	sources := append([]workpacket.Subject(nil), packet.Inputs.Sources...)
	for _, design := range packet.Inputs.Designs {
		sources = append(sources, workpacket.Subject{Path: design.Path, Version: design.Version, SHA256: design.SHA256})
	}
	facts := make([]documentFact, 0, len(sources))
	for _, source := range sources {
		subject, ok := subjects[source.Path]
		if !ok || subject.SHA256 != source.SHA256 {
			return nil, fmt.Errorf("original S3 envelope does not bind frozen source %s", source.Path)
		}
		facts = append(facts, documentFact{
			ID:         "contract-source:" + source.Path,
			Kind:       "source",
			Path:       source.Path,
			Version:    subject.Version,
			SHA256:     subject.SHA256,
			Status:     "frozen",
			Generation: packet.Binding.Generation,
		})
	}
	return facts, nil
}

func currentContractIssuanceRows(state map[string]any) []map[string]any {
	if state == nil {
		return nil
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	runtimeID := stringValue(state["runtime_id"])
	generation := nestedInt(state, "baseline", "generation")
	stageEntry := intValue(lifecycle["phase_revision"])
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	current := make([]map[string]any, 0, 1)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil || row["template_id"] != "s3-contract-v1" {
			continue
		}
		binding, _ := row["binding"].(map[string]any)
		if stringValue(binding["runtime_id"]) == runtimeID && intValue(binding["generation"]) == generation &&
			intValue(binding["stage_entry"]) == stageEntry && stringValue(binding["stage"]) == "S3" && stringValue(binding["owner"]) == "Main" {
			current = append(current, row)
		}
	}
	return current
}

func contractEvidenceRow(state map[string]any, id string) (map[string]any, bool, error) {
	raw, _ := state["evidence"].([]any)
	var result map[string]any
	for _, item := range raw {
		row, _ := item.(map[string]any)
		if row == nil || row["id"] != id {
			continue
		}
		if result != nil {
			return nil, false, fmt.Errorf("duplicate evidence identity %s", id)
		}
		result = row
	}
	return result, result != nil, nil
}

func countContractSubmissionRows(state map[string]any, packetID string) int {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	count := 0
	for _, item := range rows {
		row, _ := item.(map[string]any)
		if row != nil && row["packet_id"] == packetID {
			count++
		}
	}
	return count
}

func singleProducer(value any, want string) bool {
	producers, ok := value.([]any)
	return ok && len(producers) == 1 && producers[0] == want
}

func safeContractEvidencePath(value string) bool {
	clean := filepath.Clean(value)
	if value == "" || filepath.IsAbs(value) || filepath.ToSlash(clean) != value || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	return len(value) < 4096 && !strings.ContainsAny(value, "\\\x00")
}
