package review

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/evidence"
)

func TestIssuedEvidenceBindingUsesOriginalCaptureMetadataAndExecutionRoot(t *testing.T) {
	mainRoot, workerRoot := t.TempDir(), t.TempDir()
	rel := "evidence/output.txt"
	for root, body := range map[string]string{mainRoot: "Main is not the source", workerRoot: "real worker output"} {
		if err := os.MkdirAll(filepath.Join(root, "evidence"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	output := "real worker output"
	full := fmt.Sprintf("command_output:%s#sha256=%s (bytes=%d, truncated=false)", rel, sha256Of([]byte(output)), len(output))
	buffer := filepath.Join(workerRoot, "capture.jsonl")
	step := CaptureStep{Sequence: 1, Action: "actual original capture", Observed: "output", Evidence: []string{full}, Provenance: &CaptureProvenance{ExecutionRoot: workerRoot}}
	data, _ := json.Marshal(step)
	if err := os.WriteFile(buffer, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	result := Result{Findings: []Finding{{EvidenceRefs: []string{"command_output:" + rel}, Encounter: Encounter{Timeline: []TimelineStep{{EvidenceRefs: []string{"path:" + rel}}}}}}}
	if err := bindIssuedPathEvidence(mainRoot, workerRoot, nil, &result, false, buffer); err != nil {
		t.Fatal(err)
	}
	if got := result.Findings[0].EvidenceRefs[0]; got != full {
		t.Fatalf("capture metadata changed: got %q want %q", got, full)
	}
	pathRef := result.Findings[0].Encounter.Timeline[0].EvidenceRefs[0]
	if pathRef != "path:"+rel+"#sha256="+sha256Of([]byte(output)) {
		t.Fatalf("timeline borrowed Main bytes: %q", pathRef)
	}
	if err := evidence.ValidateFindingReference(workerRoot, full); err != nil {
		t.Fatal(err)
	}
	if err := validateEvidenceRefs(workerRoot, nil, []string{full}, "capture output"); err != nil {
		t.Fatalf("command output did not resolve against its original capture storage: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workerRoot, rel), []byte("changed after binding"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateEvidenceRefs(workerRoot, nil, []string{full}, "capture output"); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("source drift was refreshed: %v", err)
	}
	if err := evidence.ValidateFindingReference(workerRoot, full); err == nil {
		t.Fatal("S8 original Finding source accepted drift")
	}
}

func TestIssuedCommandOutputBindingDoesNotInventExecutionOrRefreshReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "output.txt"), []byte("output"), 0600); err != nil {
		t.Fatal(err)
	}
	result := Result{ClaimResults: []ClaimResult{{EvidenceRefs: []string{"command_output:output.txt"}}}}
	if err := bindIssuedPathEvidence(root, root, nil, &result, false); err == nil || !strings.Contains(err.Error(), "no original capture") {
		t.Fatalf("file existence fabricated command execution: %v", err)
	}
	full := "command_output:output.txt#sha256=" + sha256Of([]byte("output")) + " (bytes=6, truncated=false)"
	accepted := Result{ClaimResults: []ClaimResult{{EvidenceRefs: []string{full}}}}
	data, _ := json.Marshal(map[string]any{"submitted_answer": accepted})
	if err := os.WriteFile(filepath.Join(root, "accepted.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	row := map[string]any{"result_receipt": map[string]any{"result_ref": "accepted.json"}}
	if err := os.Remove(filepath.Join(root, "output.txt")); err != nil {
		t.Fatal(err)
	}
	if err := bindIssuedPathEvidence(root, root, row, &result, true); err != nil {
		t.Fatalf("exact replay reread mutable capture source: %v", err)
	}
	if result.ClaimResults[0].EvidenceRefs[0] != full {
		t.Fatal("replay replaced original captured bytes")
	}
	result.ClaimResults[0].EvidenceRefs[0] = "command_output:other.txt"
	if err := bindIssuedPathEvidence(root, root, row, &result, true); err == nil {
		t.Fatal("replay accepted a different source")
	}
	// A historical accepted bare path has no historical content proof. Even
	// when today's file is readable, replay must not manufacture that proof.
	legacy := Result{Findings: []Finding{{EvidenceRefs: []string{"path:output.txt"}}}}
	data, _ = json.Marshal(map[string]any{"submitted_answer": legacy})
	if err := os.WriteFile(filepath.Join(root, "accepted.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "output.txt"), []byte("today's unrelated bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := bindIssuedPathEvidence(root, root, row, &legacy, true); err == nil || !strings.Contains(err.Error(), "no original bound path") {
		t.Fatalf("historical bare path was retroactively certified: %v", err)
	}
}
