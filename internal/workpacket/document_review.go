package workpacket

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"
)

// Subject preserves the existing S5 gate envelope shape. Document IDs remain
// in the issued packet; the gate signs the exact path/version/content set.
type Subject struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type DocumentReviewEnvelope struct {
	SchemaVersion          string    `json:"schema_version"`
	EvidenceID             string    `json:"evidence_id"`
	Kind                   string    `json:"kind"`
	RuntimeID              string    `json:"runtime_id"`
	BaselineGeneration     int       `json:"baseline_generation"`
	ProducerAgentID        string    `json:"producer_agent_id"`
	ProducerResponsibility string    `json:"producer_responsibility"`
	SubjectRefs            []Subject `json:"subject_refs"`
	Conclusion             string    `json:"conclusion"`
	Findings               []Finding `json:"findings"`
	RequestedEvent         string    `json:"requested_event"`
	CreatedAt              string    `json:"created_at"`
}

// DocumentReview is the named S5 adapter's deterministic domain projection.
// Professional judgment is supplied as an answer already validated against p.
func DocumentReview(p Packet, id string, answer Answer, at time.Time) DocumentReviewEnvelope {
	subjects := make([]Subject, len(p.Inputs))
	for i, ref := range p.Inputs {
		subjects[i] = Subject{Path: ref.Path, Version: ref.Version, SHA256: ref.SHA256}
	}
	sort.Slice(subjects, func(i, j int) bool { return subjects[i].Path < subjects[j].Path })
	event := ""
	if *answer.Conclusion == "fix_required" {
		event = "document_fix_required"
	}
	return DocumentReviewEnvelope{
		SchemaVersion: "1.0.0", EvidenceID: id, Kind: "document_review",
		RuntimeID: p.Binding.RuntimeID, BaselineGeneration: p.Binding.Generation,
		ProducerAgentID: p.Binding.Owner, ProducerResponsibility: p.Binding.Responsibility,
		SubjectRefs: subjects, Conclusion: *answer.Conclusion, Findings: answer.Findings,
		RequestedEvent: event, CreatedAt: at.UTC().Format(time.RFC3339Nano),
	}
}

// ValidateDocumentReview rejects any envelope that would record a judgment or
// source set different from the frozen, validated questionnaire.
func ValidateDocumentReview(p Packet, id string, answer Answer, data []byte) error {
	if err := rejectDuplicateKeys(data); err != nil {
		return fmt.Errorf("document review JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope DocumentReviewEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("document review envelope: %w", err)
	}
	at, err := time.Parse(time.RFC3339Nano, envelope.CreatedAt)
	if err != nil {
		return fmt.Errorf("document review created_at: %w", err)
	}
	// Compare sets without letting reordered or duplicate subjects hide gaps.
	sort.Slice(envelope.SubjectRefs, func(i, j int) bool { return envelope.SubjectRefs[i].Path < envelope.SubjectRefs[j].Path })
	expected := DocumentReview(p, id, answer, at)
	expected.CreatedAt = envelope.CreatedAt // equivalent timestamp spelling is archival metadata
	if !reflect.DeepEqual(envelope, expected) {
		return fmt.Errorf("document review does not match validated answer and exact frozen subjects")
	}
	return nil
}
