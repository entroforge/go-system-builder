package investigation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// bindS8LocalEvidence snapshots new local files into the original evidence
// tree. No index, author metadata, or execution provenance is manufactured.
// The issued Finding source closure is verified independently and unchanged.
func bindS8LocalEvidence(root, sourceRoot string, packet workpacket.HypothesisResultPacket, answer *workpacket.HypothesisResultAnswer, replay bool) error {
	frozen := map[string]bool{}
	for _, ref := range packet.Inputs.EvidenceRefs {
		if ref.Attestation == "finding_source" || ref.Attestation == "file_source" {
			frozen[ref.Ref] = true
		}
	}
	groups := [][]string{answer.EvidenceRefs}
	for _, question := range answer.NewHypotheses {
		groups = append(groups, question.EvidenceRefs)
	}
	for _, refs := range groups {
		for i, ref := range refs {
			if !strings.HasPrefix(ref, "path:") || frozen[ref] {
				continue
			}
			rel, declared, err := evidence.LocalPathReference(ref)
			if err != nil {
				return err
			}
			if err := evidence.ValidateLocalCaptureSource(rel); err != nil {
				return err
			}
			data, err := workpacket.ReadFile(sourceRoot, rel, hypothesisResultPacketReadLimit)
			if err != nil {
				return fmt.Errorf("S8 local evidence %s: %w", rel, err)
			}
			digest := workpacket.Digest(data)
			if declared != "" && declared != digest {
				return fmt.Errorf("S8 local evidence %s SHA256 differs from the authored reference", rel)
			}
			target := evidence.S8EvidencePath(packet.ID, rel, digest)
			var saved []byte
			if replay {
				saved, err = workpacket.ReadFile(root, target, hypothesisResultPacketReadLimit)
			} else {
				saved, err = workpacket.PublishBounded(root, target, data, hypothesisResultPacketReadLimit)
			}
			if err != nil || !bytes.Equal(saved, data) {
				return fmt.Errorf("S8 local evidence immutable capture missing or differs for %s: %v", rel, err)
			}
			refs[i] = "path:" + target + "#sha256=" + digest
		}
	}
	return nil
}

// Read-only receipt lookup stays independent of private author files. Actual
// resubmission must match the originally captured projection, never sign drift.
func verifyS8ReplayedLocalEvidence(root string, receipt HypothesisResultReceipt, packet workpacket.HypothesisResultPacket, answer workpacket.HypothesisResultAnswer) error {
	data, err := workpacket.ReadFile(root, receipt.SuccessorCaseRef, hypothesisResultPacketReadLimit)
	if err != nil {
		return err
	}
	var document struct {
		Results []struct {
			HypothesisID  string   `json:"hypothesis_id"`
			AssignmentID  string   `json:"assignment_id"`
			EvidenceRefs  []string `json:"evidence_refs"`
			NewHypotheses []struct {
				EvidenceRefs []string `json:"evidence_refs"`
			} `json:"new_hypotheses"`
		} `json:"hypothesis_results"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	for _, row := range document.Results {
		if row.HypothesisID != packet.Binding.HypothesisID || row.AssignmentID != packet.Binding.AssignmentID {
			continue
		}
		if len(row.EvidenceRefs) != len(answer.EvidenceRefs) {
			break
		}
		for i := range row.EvidenceRefs {
			if row.EvidenceRefs[i] != answer.EvidenceRefs[i] {
				return fmt.Errorf("S8 replay evidence differs from the accepted file binding")
			}
		}
		if len(row.NewHypotheses) != len(answer.NewHypotheses) {
			return fmt.Errorf("S8 replay new questions differ")
		}
		for i, question := range row.NewHypotheses {
			if len(question.EvidenceRefs) != len(answer.NewHypotheses[i].EvidenceRefs) {
				return fmt.Errorf("S8 replay new question evidence differs")
			}
			for j, ref := range question.EvidenceRefs {
				if ref != answer.NewHypotheses[i].EvidenceRefs[j] {
					return fmt.Errorf("S8 replay new question file evidence differs")
				}
			}
		}
		return nil
	}
	return fmt.Errorf("S8 replay has no original result evidence projection")
}
