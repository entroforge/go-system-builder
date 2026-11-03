package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// RepairResultBlockerArtifact preserves one issued S9 professional answer
// alongside the exact packet, execution and immutable message that produced
// the original work_blocked transition. It is a data transfer type only: it
// contains neither accepted Runtime coordinates nor an artifact self hash.
type RepairResultBlockerArtifact struct {
	Contract      string `json:"contract"`
	SchemaVersion string `json:"schema_version"`
	PacketID      string `json:"packet_id"`

	IssuanceEventID       string                         `json:"issuance_event_id"`
	Binding               workpacket.RepairResultBinding `json:"binding"`
	ExecutionAssignmentID string                         `json:"execution_assignment_id"`
	ExecutionGeneration   int                            `json:"execution_generation"`
	ExecutionRoot         string                         `json:"execution_root"`
	PlatformSessionID     string                         `json:"platform_session_id"`

	AnswerPath      string          `json:"answer_path"`
	SubmittedAnswer json.RawMessage `json:"submitted_answer"`

	MessageRef string `json:"message_ref"`
}

// UnmarshalJSON rejects duplicate and unknown fields and canonicalizes the
// complete professional answer. The embedded schema also closes and validates
// the nested binding and nine-field answer without granting authority.
func (artifact *RepairResultBlockerArtifact) UnmarshalJSON(data []byte) error {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return fmt.Errorf("S9 blocker artifact JSON: %w", err)
	}
	var decoded repairResultBlockerArtifactWire
	if err := decodeRepairResultBlockerJSON(data, &decoded); err != nil {
		return fmt.Errorf("decode S9 blocker artifact: %w", err)
	}
	answer, err := canonicalRuntimeJSON(decoded.SubmittedAnswer)
	if err != nil {
		return fmt.Errorf("S9 blocker submitted answer: %w", err)
	}
	if err := validateCompleteRepairResultBlockerAnswer(answer); err != nil {
		return err
	}
	decoded.SubmittedAnswer = answer
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return fmt.Errorf("encode S9 blocker artifact: %w", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-result-blocker.schema.json", canonical); err != nil {
		return fmt.Errorf("validate S9 blocker artifact: %w", err)
	}
	*artifact = RepairResultBlockerArtifact(decoded)
	return nil
}

type repairResultBlockerArtifactWire RepairResultBlockerArtifact

func validateCompleteRepairResultBlockerAnswer(answer []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(answer, &fields); err != nil || fields == nil {
		return errors.New("S9 blocker submitted answer must be a complete object")
	}
	value := bytes.TrimSpace(fields["verdict"])
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return errors.New("S9 blocker submitted answer must express a verdict")
	}
	return nil
}

// RepairResultBlockerReceipt is a read-only projection of the original
// blocker artifact and its accepted AGENT-LIFECYCLE event. These coordinates
// are deliberately absent from RepairResultBlockerArtifact.
type RepairResultBlockerReceipt struct {
	Contract    string `json:"contract"`
	ArtifactRef string `json:"artifact_ref"`

	Artifact                     RepairResultBlockerArtifact `json:"artifact"`
	AcceptedEventID              string                      `json:"accepted_event_id"`
	AcceptedIdempotencyKey       string                      `json:"accepted_idempotency_key"`
	AcceptedAfterRuntimeRevision int                         `json:"accepted_after_runtime_revision"`
	AcceptedSequence             int                         `json:"accepted_sequence"`
}

// UnmarshalJSON keeps the read-only receipt closed and rejects duplicate keys.
// The nested artifact performs its own strict decoding and schema validation.
func (receipt *RepairResultBlockerReceipt) UnmarshalJSON(data []byte) error {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return fmt.Errorf("S9 blocker receipt JSON: %w", err)
	}
	var decoded repairResultBlockerReceiptWire
	if err := decodeRepairResultBlockerJSON(data, &decoded); err != nil {
		return fmt.Errorf("decode S9 blocker receipt: %w", err)
	}
	if decoded.Contract != "s9-repair-result-blocker-receipt-v1" || decoded.Artifact.Contract != "s9-repair-result-blocker-v1" || decoded.ArtifactRef == "" || decoded.AcceptedEventID == "" || decoded.AcceptedIdempotencyKey == "" || decoded.AcceptedSequence < 1 {
		return errors.New("S9 blocker receipt has incomplete original acceptance coordinates")
	}
	*receipt = RepairResultBlockerReceipt(decoded)
	return nil
}

type repairResultBlockerReceiptWire RepairResultBlockerReceipt

func decodeRepairResultBlockerJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("expected one JSON value")
		}
		return err
	}
	return nil
}
