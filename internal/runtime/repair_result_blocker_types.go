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
	Contract              string                         `json:"contract"`
	SchemaVersion         string                         `json:"schema_version"`
	PacketID              string                         `json:"packet_id"`
	PacketSHA256          string                         `json:"packet_sha256"`
	IssuanceEventID       string                         `json:"issuance_event_id"`
	Binding               workpacket.RepairResultBinding `json:"binding"`
	ExecutionAssignmentID string                         `json:"execution_assignment_id"`
	ExecutionGeneration   int                            `json:"execution_generation"`
	ExecutionRoot         string                         `json:"execution_root"`
	PlatformSessionID     string                         `json:"platform_session_id"`
	BaseCommit            string                         `json:"base_commit"`
	SourceHead            string                         `json:"source_head"`
	AnswerPath            string                         `json:"answer_path"`
	SubmittedAnswer       json.RawMessage                `json:"submitted_answer"`
	AnswerSHA256          string                         `json:"answer_sha256"`
	MessageRef            string                         `json:"message_ref"`
	MessageSHA256         string                         `json:"message_sha256"`
}

// UnmarshalJSON rejects duplicate and unknown fields and canonicalizes the
// complete professional answer. The embedded schema also closes and validates
// the nested binding and nine-field answer without granting authority.
func (artifact *RepairResultBlockerArtifact) UnmarshalJSON(data []byte) error {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return fmt.Errorf("S9 blocker artifact JSON: %w", err)
	}
	var decoded repairResultBlockerArtifactWire
	if err := decodeRepairResultBlockerStrictJSON(data, &decoded); err != nil {
		return fmt.Errorf("decode S9 blocker artifact: %w", err)
	}
	answer, err := canonicalRuntimeJSON(decoded.SubmittedAnswer)
	if err != nil {
		return fmt.Errorf("S9 blocker submitted answer: %w", err)
	}
	if err := validateCompleteRepairResultBlockerAnswer(answer); err != nil {
		return err
	}
	if decoded.AnswerSHA256 != workpacket.Digest(answer) {
		return errors.New("S9 blocker submitted answer differs from its canonical answer SHA256")
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
	for _, name := range []string{
		"verdict", "before_fix_results", "unit_results", "detection_gap_artifacts", "evidence_refs",
		"scope_deviations", "residual_risks", "migration_ref", "rollback_ref",
	} {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("S9 blocker submitted answer field %q must be explicitly complete", name)
		}
	}
	return nil
}

// RepairResultBlockerReceipt is a read-only projection of the original
// blocker artifact and its accepted AGENT-LIFECYCLE event. These coordinates
// are deliberately absent from RepairResultBlockerArtifact.
type RepairResultBlockerReceipt struct {
	Contract                     string                      `json:"contract"`
	ArtifactRef                  string                      `json:"artifact_ref"`
	ArtifactSHA256               string                      `json:"artifact_sha256"`
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
	if err := decodeRepairResultBlockerStrictJSON(data, &decoded); err != nil {
		return fmt.Errorf("decode S9 blocker receipt: %w", err)
	}
	if decoded.Contract != "s9-repair-result-blocker-receipt-v1" || decoded.Artifact.Contract != "s9-repair-result-blocker-v1" || decoded.ArtifactRef == "" || !isSHA256Hex(decoded.ArtifactSHA256) || decoded.AcceptedEventID == "" || decoded.AcceptedIdempotencyKey == "" || decoded.AcceptedAfterRuntimeRevision < 1 || decoded.AcceptedSequence < 1 {
		return errors.New("S9 blocker receipt has incomplete original acceptance coordinates")
	}
	*receipt = RepairResultBlockerReceipt(decoded)
	return nil
}

type repairResultBlockerReceiptWire RepairResultBlockerReceipt

func decodeRepairResultBlockerStrictJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
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

func isSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !('0' <= c && c <= '9') && !('a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}
