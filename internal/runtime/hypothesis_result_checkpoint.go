package runtime

import (
	"encoding/json"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// HypothesisResultReceipt is the original immutable Case receipt wire.
// This wire is intentionally neutral: Runtime recovery must not import the
// investigation Writer in order to verify its original immutable artifacts.
type HypothesisResultReceipt struct {
	// Result and Answer project the original Case row for current-attempt handling.
	// They are not a second persisted professional answer.
	Result                       string          `json:"-"`
	Answer                       json.RawMessage `json:"-"`
	Contract                     string          `json:"contract"`
	PacketID                     string          `json:"packet_id"`
	SubmittedAnswer              json.RawMessage `json:"submitted_answer,omitempty"`
	OwnerAgentID                 string          `json:"owner_agent_id"`
	RuntimeID                    string          `json:"runtime_id"`
	BaselineGeneration           int             `json:"baseline_generation"`
	AssignmentID                 string          `json:"assignment_id"`
	TeamID                       string          `json:"team_id"`
	TaskID                       string          `json:"task_id"`
	TaskRef                      string          `json:"task_ref"`
	ManifestRef                  string          `json:"manifest_ref"`
	ExecutionRoot                string          `json:"execution_root"`
	IssuanceEventID              string          `json:"issuance_event_id"`
	AcceptedEventID              string          `json:"accepted_event_id"`
	AcceptedIdempotencyKey       string          `json:"accepted_idempotency_key"`
	AcceptedAfterRuntimeRevision int             `json:"accepted_after_runtime_revision"`
	AcceptedSequence             int             `json:"accepted_sequence"`
	SuccessorCaseRef             string          `json:"successor_case_ref"`
	SuccessorCaseRevision        int             `json:"successor_case_revision"`
}

// DecodeOriginalHypothesisResultReceipt reads the accepted original result.
func DecodeOriginalHypothesisResultReceipt(data []byte, receipt *HypothesisResultReceipt) error {
	return json.Unmarshal(data, receipt)
}

// VerifiedHypothesisResultReceipt returns an original accepted fact without
// granting a new write or trusting a surviving authoring file. Historical
// Cases require an original pointer/history authority, never directory scans.
func VerifiedHypothesisResultReceipt(root string, state map[string]any, packetID, owner string) (HypothesisResultReceipt, bool, error) {
	p, found, err := FindHypothesisResultPacket(state, packetID)
	if err != nil || !found {
		return HypothesisResultReceipt{}, false, err
	}
	if owner == "" || p.Binding.OwnerAgentID != owner {
		return HypothesisResultReceipt{}, false, fmt.Errorf("S8 receipt owner differs from original issued owner")
	}
	if len(p.LateResults) > 0 {
		late := p.LateResults[len(p.LateResults)-1]
		var answer workpacket.HypothesisResultAnswer
		if err := json.Unmarshal(late.Answer, &answer); err != nil || answer.Result == nil {
			return HypothesisResultReceipt{}, false, fmt.Errorf("S8 late original answer is unreadable")
		}
		return HypothesisResultReceipt{Contract: "s8-hypothesis-result-receipt-v1", PacketID: p.ID, OwnerAgentID: owner,
			RuntimeID: p.Binding.RuntimeID, BaselineGeneration: p.Binding.BaselineGeneration,
			AssignmentID: p.Binding.AssignmentID, TeamID: p.Binding.TeamID, TaskID: p.Binding.TaskID, TaskRef: p.Binding.TaskRef,
			ManifestRef: p.Binding.ManifestRef, ExecutionRoot: late.ExecutionRoot, IssuanceEventID: p.IssuanceEventID,
			AcceptedEventID: late.AcceptedEventID, AcceptedIdempotencyKey: late.AcceptedIdempotencyKey,
			AcceptedAfterRuntimeRevision: late.AcceptedAfterRuntimeRevision, AcceptedSequence: late.AcceptedSequence,
			SuccessorCaseRef: p.Binding.CaseRef, SuccessorCaseRevision: p.Binding.CaseRevision,
			Result: *answer.Result, Answer: late.Answer}, true, nil
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	if pointer == nil || pointer["case_id"] != p.Binding.CaseID {
		return HypothesisResultReceipt{}, false, nil
	}
	document, err := readCheckpointHypothesisCase(root, stringValue(pointer["path"]))
	if err != nil {
		return HypothesisResultReceipt{}, false, err
	}
	rows := objectRows(document["hypothesis_results"])
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		receipt, _ := row["receipt"].(map[string]any)
		if receipt["packet_id"] != packetID {
			continue
		}
		encoded, err := json.Marshal(receipt)
		if err != nil {
			return HypothesisResultReceipt{}, false, err
		}
		var result HypothesisResultReceipt
		if err := DecodeOriginalHypothesisResultReceipt(encoded, &result); err != nil {
			return HypothesisResultReceipt{}, false, err
		}
		if result.OwnerAgentID != owner || result.AssignmentID != p.Binding.AssignmentID || result.PacketID != packetID || row["hypothesis_id"] != p.Binding.HypothesisID || row["assignment_id"] != p.Binding.AssignmentID {
			return HypothesisResultReceipt{}, false, fmt.Errorf("S8 accepted receipt differs from its original responsibility")
		}
		result.Result = stringValue(row["result"])
		var answer workpacket.HypothesisResultAnswer
		projected, _ := json.Marshal(row)
		if err := json.Unmarshal(projected, &answer); err != nil {
			return result, false, err
		}
		result.Answer, _ = json.Marshal(answer)
		return result, true, nil
	}
	return HypothesisResultReceipt{}, false, nil
}

func readCheckpointHypothesisCase(root, ref string) (map[string]any, error) {
	data, err := workpacket.ReadFile(root, ref, 20<<20)
	if err != nil {
		return nil, fmt.Errorf("S8 Case %s is unavailable: %v", ref, err)
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return nil, err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", data); err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	return document, nil
}
