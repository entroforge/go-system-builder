package runtime

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func FindDesignPacket(state map[string]any, id string) (workpacket.DesignPacket, bool, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["packet_id"] != id || row["template_id"] != "s2-design-v1" {
			continue
		}
		data, err := json.Marshal(row)
		if err != nil {
			return workpacket.DesignPacket{}, false, err
		}
		var p workpacket.DesignPacket
		err = json.Unmarshal(data, &p)
		return p, true, err
	}
	return workpacket.DesignPacket{}, false, nil
}

func RecordDesignPacket(root, statePath, journalPath string, p workpacket.DesignPacket, files fileview.Reader, validator CandidateValidator) (Snapshot, error) {
	writer := NewWriter(statePath, journalPath, root, validator)
	snap, err := writer.Snapshot()
	if err != nil {
		return Snapshot{}, err
	}
	if err := ValidateDesignPacket(root, snap.State, p, files); err != nil {
		return Snapshot{}, err
	}
	if _, found, err := FindDesignPacket(snap.State, p.ID); err != nil {
		return Snapshot{}, err
	} else if found {
		return snap, nil
	}
	data, _ := json.Marshal(p)
	var row map[string]any
	json.Unmarshal(data, &row)
	cursor := map[string]any{"state": "planning", "phase": "design"}
	next, err := writer.UpdateCurrent(Mutation{EventID: "evt-design-packet-" + p.ID, TransitionID: "PACKET-ISSUE", Event: "work_packet_issued", Actor: "orchestrator", IdempotencyKey: "packet:" + p.ID, RuntimeID: p.Binding.RuntimeID, From: cursor, To: cursor, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		if err := ValidateDesignPacket(root, state, p, files); err != nil {
			return err
		}
		if _, found, err := FindDesignPacket(state, p.ID); err != nil {
			return err
		} else if found {
			return errSubmissionAccepted
		}
		entities := state["entities"].(map[string]any)
		rows, _ := entities["work_packets"].([]any)
		for _, raw := range rows {
			old, _ := raw.(map[string]any)
			if old["template_id"] != "s2-design-v1" {
				continue
			}
			binding, _ := old["binding"].(map[string]any)
			if stringValue(binding["runtime_id"]) == p.Binding.RuntimeID && numberBindingGeneration(binding) == p.Binding.Generation && stringValue(binding["owner"]) == "Main" {
				entry, err := integerField(binding, "stage_entry")
				if err != nil {
					return err
				}
				if entry == p.Binding.StageEntry {
					return fmt.Errorf("design stage already has a different frozen issuance")
				}
			}
		}
		entities["work_packets"] = append(rows, row)
		return nil
	}})
	if err != nil {
		latest, readErr := writer.Snapshot()
		if readErr == nil {
			_, found, readErr := FindDesignPacket(latest.State, p.ID)
			if readErr == nil && found {
				return latest, nil
			}
		}
	}
	return next, err
}

// DesignArchitecture preserves the original gate's directory/name/header rules.
func DesignArchitecture(root, path string, files fileview.Reader) (workpacket.Reference, error) {
	clean := filepath.ToSlash(filepath.Clean(path))
	name := filepath.Base(clean)
	if filepath.ToSlash(filepath.Dir(clean)) != "docs/architecture" || !strings.HasPrefix(name, "ARCHITECTURE-") || !strings.HasSuffix(name, ".md") || strings.Contains(strings.ToLower(name), "template") {
		return workpacket.Reference{}, fmt.Errorf("design requires actual docs/architecture/ARCHITECTURE-*.md")
	}
	disk, err := workpacket.ReadFile(root, clean, 2<<20)
	if err != nil {
		return workpacket.Reference{}, err
	}
	view := disk
	field := func(keys ...string) string {
		for _, line := range strings.Split(string(view), "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
			for _, key := range keys {
				for _, sep := range []string{"：", ":"} {
					prefix := key + sep
					if strings.HasPrefix(line, prefix) {
						value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
						if i := strings.IndexAny(value, "（( "); i > 0 {
							value = strings.TrimSpace(value[:i])
						}
						return value
					}
				}
			}
		}
		return ""
	}
	version := field("版本", "Version")
	return workpacket.Reference{ID: strings.TrimSuffix(name, ".md"), Path: clean, Version: version, SHA256: workpacket.Digest(view)}, nil
}

func prepareDesignSubmission(root string, state map[string]any, req EvidenceRequest, data []byte) (SubmissionRecord, error) {
	s := req.Submission
	p, found, err := FindDesignPacket(state, s.PacketID)
	if err != nil || !found {
		return SubmissionRecord{}, fmt.Errorf("design questionnaire is not registered for this responsibility")
	}
	if req.Kind != "planning_design" {
		return SubmissionRecord{}, fmt.Errorf("design recording kind differs")
	}
	answer, issues := workpacket.ValidateDesignAnswer(p, s.AnswerJSON)
	if len(issues) > 0 {
		return SubmissionRecord{}, fmt.Errorf("design answer %s: %s", issues[0].Pointer, issues[0].Expected)
	}
	digest, err := workpacket.DesignAnswerDigest(answer)
	if err != nil {
		return SubmissionRecord{}, err
	}
	prior, accepted, err := FindSubmission(state, p.ID)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if accepted {
		if prior.ContentHash != digest {
			return prior, ErrSubmissionConflict
		}
		return prior, errSubmissionAccepted
	}
	if err := ValidateDesignPacket(root, state, p, s.Files); err != nil {
		return SubmissionRecord{}, err
	}
	if answer.Conclusion == nil || *answer.Conclusion != "pass" || answer.ArchitecturePath == nil || *answer.ArchitecturePath == "" {
		return SubmissionRecord{}, fmt.Errorf("design remains a draft until the actual self-review and architecture are supplied")
	}
	key := workpacket.Digest([]byte("submission:" + p.ID))
	return SubmissionRecord{Key: key, PacketID: p.ID, Owner: "Main", ContentHash: digest, EvidenceID: req.ID, FollowupID: workpacket.Digest([]byte(key + ":after-domain")), ControlStatus: "pending", OriginState: "planning", OriginPhase: "design", OriginEntry: p.Binding.StageEntry}, nil
}

func progressCanonicalJSON(v any) string {
	data, _ := json.Marshal(v)
	var value any
	json.Unmarshal(data, &value)
	data, _ = json.Marshal(value)
	return string(data)
}

func ValidateDesignEnvelope(p workpacket.DesignPacket, id string, answer workpacket.DesignAnswer, data []byte) error {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return err
	}
	// Map decoding below checks exact keys and reconstructs the original shape.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, key := range []string{"schema_version", "evidence_id", "kind", "runtime_id", "baseline_generation", "producer_agent_id", "producer_responsibility", "subject_refs", "conclusion", "created_at"} {
		if _, ok := raw[key]; !ok {
			return fmt.Errorf("planning envelope missing %s", key)
		}
	}
	if len(raw) != 10 {
		return fmt.Errorf("planning envelope has unsupported fields")
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var subjects []workpacket.Subject
	if err := json.Unmarshal(raw["subject_refs"], &subjects); err != nil || len(subjects) != 2 {
		return fmt.Errorf("planning envelope needs exact REQ and architecture subjects")
	}
	req := p.Binding.Requirement
	if subjects[0] != (workpacket.Subject{Path: req.Path, Version: req.Version, SHA256: req.SHA256}) || answer.ArchitecturePath == nil || subjects[1].Path != *answer.ArchitecturePath {
		return fmt.Errorf("planning envelope frozen subjects differ")
	}
	at, err := time.Parse(time.RFC3339Nano, stringValue(value["created_at"]))
	if err != nil {
		return err
	}
	expected, err := workpacket.DesignEvidence(p, id, workpacket.Reference{Path: subjects[1].Path, Version: subjects[1].Version, SHA256: subjects[1].SHA256}, answer, at)
	if err != nil {
		return err
	}
	expected["created_at"] = value["created_at"]
	if progressCanonicalJSON(value) != progressCanonicalJSON(expected) {
		return fmt.Errorf("planning envelope differs from validated professional answer")
	}
	return nil
}
