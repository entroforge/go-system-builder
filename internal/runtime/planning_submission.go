package runtime

import (
	"context"
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

// CurrentIssuedDesignPacket returns the existing current Main issuance and its
// recorded paths without constructing or comparing the current template.
func CurrentIssuedDesignPacket(state map[string]any) (workpacket.DesignPacket, bool, error) {
	binding, err := CurrentDesignBinding(state)
	if err != nil {
		return workpacket.DesignPacket{}, false, err
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	var original workpacket.DesignPacket
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s2-design-v1" {
			continue
		}
		candidate, exists, err := FindDesignPacket(state, stringValue(row["packet_id"]))
		if err != nil {
			return original, false, err
		}
		if !exists || !samePlanningResponsibility(candidate.Binding, binding) {
			continue
		}
		if original.ID != "" {
			return original, false, fmt.Errorf("multiple design questionnaires belong to the current entry")
		}
		original = candidate
	}
	return original, original.ID != "", nil
}

func RecordDesignPacket(ctx context.Context, root, statePath, journalPath string, p workpacket.DesignPacket, files fileview.Reader, validator CandidateValidator) (Snapshot, error) {
	writer := NewWriter(statePath, journalPath, root, validator).WithContext(ctx)
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
		issued, err := publishDesignPacketFiles(ctx, root, p)
		if err != nil {
			return err
		}
		data, err := json.Marshal(issued)
		if err != nil {
			return err
		}
		var row map[string]any
		if err := json.Unmarshal(data, &row); err != nil {
			return err
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

// publishDesignPacketFiles runs only within the original issuance Writer lock.
// A failed or interrupted candidate remains unknown; only the packet paths
// committed with the pending Runtime mutation are subsequently authoritative.
func publishDesignPacketFiles(ctx context.Context, root string, original workpacket.DesignPacket) (workpacket.DesignPacket, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return original, err
		}
		p := original
		if attempt > 0 {
			p.ViewPath = strings.Replace(original.ViewPath, ".claude/work-packets/", fmt.Sprintf(".claude/work-packets-alt-%d/", attempt), 1)
			p.AnswerPath = strings.Replace(original.AnswerPath, ".claude/submissions/", fmt.Sprintf(".claude/submissions-alt-%d/", attempt), 1)
			if p.ViewPath == original.ViewPath || p.AnswerPath == original.AnswerPath {
				return original, fmt.Errorf("S2 original publication roots are invalid")
			}
		}
		view, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return original, err
		}
		draft, err := json.MarshalIndent(p.Draft, "", "  ")
		if err != nil {
			return original, err
		}
		view, draft = append(view, '\n'), append(draft, '\n')
		created, err := workpacket.PublishNewBounded(root, p.ViewPath, view, int64(len(view)))
		if err != nil {
			return original, err
		}
		if !created {
			continue
		}
		if err := ctx.Err(); err != nil {
			return original, err
		}
		created, err = workpacket.PublishNewBounded(root, p.AnswerPath, draft, 256<<10)
		if err != nil {
			return original, err
		}
		if !created {
			continue
		}
		return p, nil
	}
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
	return workpacket.Reference{ID: strings.TrimSuffix(name, ".md"), Path: clean, Version: version}, nil
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
	prior, accepted, err := FindSubmission(state, p.ID)
	if err != nil {
		return SubmissionRecord{}, err
	}
	if accepted {
		return prior, errSubmissionAccepted
	}
	if err := ValidateDesignPacket(root, state, p, s.Files); err != nil {
		return SubmissionRecord{}, err
	}
	if answer.Conclusion == nil || *answer.Conclusion != "pass" || answer.ArchitecturePath == nil || *answer.ArchitecturePath == "" {
		return SubmissionRecord{}, fmt.Errorf("design remains a draft until the actual self-review and architecture are supplied")
	}
	key := "submission:" + p.ID
	return SubmissionRecord{Key: key, PacketID: p.ID, Owner: "Main", EvidenceID: req.ID, FollowupID: "followup:" + p.ID, ControlStatus: "pending", OriginState: "planning", OriginPhase: "design", OriginEntry: p.Binding.StageEntry}, nil
}
