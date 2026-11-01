package workpacket

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestS5PacketUsesResponsibilityWithoutHash(t *testing.T) {
	b := testBinding()
	inputs := []Reference{{ID: "REQ-039", Path: "docs/requirements/REQ-039.md", Version: "v1"}}
	p, err := BuildS5(b, inputs)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sha256") {
		t.Fatalf("new packet contains digest: %s", raw)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("work-packet.schema.json", raw); err != nil {
		t.Fatal(err)
	}
	for _, component := range strings.Split(p.ViewPath, "/") {
		if len(component) > 125 {
			t.Fatalf("view path component too long: %d", len(component))
		}
	}
	bound, err := BindTaskAnalysis(p, &TaskAnalysisRef{Path: ".claude/work-packets/analysis/current.json", ParserVersion: "1.0.0"})
	if err != nil || bound.ID != p.ID {
		t.Fatalf("analysis must keep responsibility identity: %v", err)
	}
	raw, _ = json.Marshal(bound)
	if err := schema.NewEmbeddedValidator().ValidateBytes("work-packet.schema.json", raw); err != nil {
		t.Fatal(err)
	}
}

// Presentation extras and legacy metadata must not become new qualifications;
// duplicate keys, trailing data, and the professional conclusion remain checked.
func TestDocumentReviewDecodeKeepsProfessionalBoundary(t *testing.T) {
	pass := "pass"
	p := Packet{Binding: Binding{RuntimeID: "runtime", Generation: 1, Owner: "reviewer", Responsibility: "DV-SPEC-CONSISTENCY"}, Inputs: []Reference{{Path: "docs/source.md", Version: "v1"}}}
	answer := Answer{Conclusion: &pass, Findings: []Finding{}}
	envelope := DocumentReview(p, "evidence", answer, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	extra := bytes.Replace(data, []byte(`"schema_version":`), []byte(`"unrelated_metadata":true,"schema_version":`), 1)
	extra = bytes.Replace(extra, []byte(`"path":"docs/source.md"`), []byte(`"path":"docs/source.md","sha256":"historical","note":"extra"`), 1)
	if err := ValidateDocumentReview(p, "evidence", answer, extra); err != nil {
		t.Fatalf("unrelated fields narrowed reception: %v", err)
	}
	duplicate := bytes.Replace(data, []byte(`"conclusion":"pass"`), []byte(`"conclusion":"fix_required","conclusion":"pass"`), 1)
	if err := ValidateDocumentReview(p, "evidence", answer, duplicate); err == nil {
		t.Fatal("duplicate conclusion accepted")
	}
	if err := ValidateDocumentReview(p, "evidence", answer, append(append([]byte(nil), data...), []byte(` {}`)...)); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	wrong := bytes.Replace(data, []byte(`"conclusion":"pass"`), []byte(`"conclusion":"fix_required"`), 1)
	if err := ValidateDocumentReview(p, "evidence", answer, wrong); err == nil {
		t.Fatal("different professional conclusion accepted")
	}
}
