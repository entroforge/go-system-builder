package workpacket

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestIssuedReviewAndRepairPacketsPreserveOriginalResponsibility(t *testing.T) {
	review, err := BuildReviewResult(reviewResultTestBinding(), reviewResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	planBinding, planInputs := repairPlanReportTestFixture(t)
	plan, err := BuildRepairPlanReport(planBinding, planInputs)
	if err != nil {
		t.Fatal(err)
	}
	resultBinding, resultInputs := repairResultTestFixture(t)
	result, err := BuildRepairResult(resultBinding, resultInputs)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, schema string
		packet       any
	}{
		{"s7", "review-result-work-packet.schema.json", review},
		{"s9-plan", "repair-plan-report-work-packet.schema.json", plan},
		{"s9-result", "repair-result-work-packet.schema.json", result},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.packet)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.NewEmbeddedValidator().ValidateBytes(tc.schema, data); err != nil {
				t.Fatal(err)
			}
			var row map[string]any
			if err := json.Unmarshal(data, &row); err != nil {
				t.Fatal(err)
			}
			if row["packet_id"] == "" || row["binding"] == nil {
				t.Fatalf("issued original identity missing: %s", data)
			}
			if _, exists := row["template_sha256"]; exists {
				t.Fatalf("obsolete digest emitted: %s", data)
			}
		})
	}
}

func TestPublishNewBoundedPreservesUnknownOccupants(t *testing.T) {
	root := t.TempDir()
	path := ".claude/work-packets/analysis/original.json"
	created, err := PublishNewBounded(root, path, []byte(`{"owner":"first"}`), 1<<20)
	if err != nil || !created {
		t.Fatalf("first publication: %v %v", created, err)
	}
	created, err = PublishNewBounded(root, path, []byte(`{"owner":"second"}`), 1<<20)
	if err != nil || created {
		t.Fatalf("occupied publication: %v %v", created, err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil || string(data) != `{"owner":"first"}` {
		t.Fatalf("unknown original changed: %s %v", data, err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	created, err = PublishNewBounded(root, "link/new.json", []byte(`{}`), 1<<20)
	if err != nil || created {
		t.Fatalf("occupied parent: %v %v", created, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.json")); !os.IsNotExist(err) {
		t.Fatal("publication followed unknown symlink")
	}
}

func TestHypothesisFindingKeepsProfessionalDocumentWithSHAField(t *testing.T) {
	data := []byte(`{"finding_id":"finding-1","ref":"docs/finding.json","sha256":{"old":true},"document":{"business":{"sha256":"customer-value"}}}`)
	var finding HypothesisResultFinding
	if err := json.Unmarshal(data, &finding); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(finding.Document, []byte(`"sha256":"customer-value"`)) {
		t.Fatalf("machine field was not ignored or professional source changed: %+v", finding)
	}
}

func TestNewReviewerPacketIgnoresHistoricalSubjectMetadata(t *testing.T) {
	for _, value := range []string{"null", `"old"`, "7", `{"old":true}`} {
		inputs := reviewResultTestInputs()
		inputs.FrozenSubjects = json.RawMessage(`[{"path":"docs/requirements/REQ-041.md","sha256":` + value + `}]`)
		original := append([]byte(nil), inputs.FrozenSubjects...)
		packet, err := BuildReviewResult(reviewResultTestBinding(), inputs)
		if err != nil {
			t.Fatalf("historical subject %s: %v", value, err)
		}
		if bytes.Contains(packet.Inputs.FrozenSubjects, []byte("sha256")) || !bytes.Equal(inputs.FrozenSubjects, original) {
			t.Fatalf("new packet copied machine metadata or changed original: %s", packet.Inputs.FrozenSubjects)
		}
	}
}

func TestNewHypothesisPacketHasNoFrameworkDigest(t *testing.T) {
	packet, err := BuildHypothesisResult(hypothesisResultTestBinding(t), hypothesisResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("sha256")) {
		t.Fatalf("new packet contains a framework digest: %s", encoded)
	}
}
