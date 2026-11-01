package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS5AcceptedReplayUsesActualAnswerNotHistoricalFingerprints(t *testing.T) {
	conclusion := "pass"
	packet := workpacket.Packet{Binding: workpacket.Binding{RuntimeID: "runtime", Owner: "reviewer", Responsibility: "document-review"}, Inputs: []workpacket.Reference{{Path: "spec.md"}}}
	answer := workpacket.Answer{Conclusion: &conclusion, Findings: []workpacket.Finding{}}
	envelope := workpacket.DocumentReviewEnvelope{EvidenceID: "evidence", Kind: "document_review", RuntimeID: "runtime", ProducerAgentID: "reviewer", ProducerResponsibility: "document-review", Conclusion: conclusion, SubjectRefs: []workpacket.Subject{{Path: "spec.md"}}}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"path":"spec.md"`), []byte(`"path":"spec.md","sha256":"different-old-value"`), 1)
	if err := validateS5AcceptedAnswer(packet, "evidence", answer, data); err != nil {
		t.Fatalf("historical digest became a replay gate: %v", err)
	}
	changed := "fix_required"
	answer.Conclusion = &changed
	if err := validateS5AcceptedAnswer(packet, "evidence", answer, data); !errors.Is(err, ErrSubmissionConflict) {
		t.Fatalf("changed professional result did not follow original revision: %v", err)
	}
}
