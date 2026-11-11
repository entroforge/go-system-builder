package semantic_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestValidateManualAgreementPassesWhenManualAtRoot(t *testing.T) {
	root := t.TempDir()
	writeMinimalProject(t, root, "match", manualAtRoot)
	if err := semantic.ValidateManualAgreement(root); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestValidateManualAgreementPassesWhenManualInBin(t *testing.T) {
	root := t.TempDir()
	writeMinimalProject(t, root, "match", manualAtBin)
	if err := semantic.ValidateManualAgreement(root); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestValidateManualAgreementDoesNotRequireDigest(t *testing.T) {
	root := t.TempDir()
	writeMinimalProject(t, root, "{}", manualAtBin)
	manual := filepath.Join(root, ".claude", "bin", "loop-harness.md")
	for _, content := range []string{"# loop-harness\n", manualWithSHA(strings.Repeat("0", 64))} {
		if err := os.WriteFile(manual, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := semantic.ValidateManualAgreement(root); err != nil {
			t.Fatalf("manual must not require a digest: %v", err)
		}
	}
	if err := os.Remove(manual); err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateManualAgreement(root); err == nil {
		t.Fatal("missing manual must be reported")
	}
}

// When both candidate paths exist, doctor must use the root one (lookup
// order is root-first).
func TestValidateManualAgreementPrefersRootWhenBothPresent(t *testing.T) {
	root := t.TempDir()
	writeMinimalProject(t, root, `{"transitions":[{"id":"TR-006","required_evidence":["builder_report_record"]}]}`, manualAtRoot)
	if err := os.WriteFile(filepath.Join(root, "loop-harness.md"), []byte("--evidence builder_report_record=<reference>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Bin copy lacks the required evidence guidance.
	binDir := filepath.Join(root, ".claude", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(binDir, "loop-harness.md"),
		[]byte("# Older manual\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateManualAgreement(root); err != nil {
		t.Fatalf("expected pass (root wins), got %v", err)
	}
}

func TestValidateManualAgreementFailsWhenManualMissing(t *testing.T) {
	root := t.TempDir()
	writeMinimalProject(t, root, "anything", manualAtBin)
	// Remove the manual to simulate a fresh project that hasn't run init yet.
	if err := os.RemoveAll(filepath.Join(root, ".claude")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "loop-harness.md")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	err := semantic.ValidateManualAgreement(root)
	if err == nil || !strings.Contains(err.Error(), "manual missing") {
		t.Fatalf("expected 'manual missing' error, got %v", err)
	}
	if !strings.Contains(err.Error(), "loop-harness manual") {
		t.Fatalf("error must mention recovery command; got %v", err)
	}
}

func TestValidateManualAgreementIgnoresHistoricalDigestAfterDefinitionChanges(t *testing.T) {
	root := t.TempDir()
	writeMinimalProject(t, root, "original", manualAtRoot)
	// Edit loop-definition.json AFTER the manual was generated to simulate
	// drift (e.g. someone added a transition but forgot to regen the manual).
	if err := os.WriteFile(
		filepath.Join(root, "docs", "control", "loop-definition.json"),
		[]byte("{\"extra\":\"drift\"}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateManualAgreement(root); err != nil {
		t.Fatalf("historical digest must not block guidance: %v", err)
	}
}

func TestValidateManualAgreementAcceptsHeaderWithoutDigest(t *testing.T) {
	root := t.TempDir()
	writeMinimalProject(t, root, "original", manualAtRoot)
	// Overwrite the manual with content that has no SHA-256 line.
	if err := os.WriteFile(
		filepath.Join(root, "loop-harness.md"),
		[]byte("# loop-harness\n\nNo header here.\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateManualAgreement(root); err != nil {
		t.Fatalf("digest-free header must be accepted: %v", err)
	}
}

func TestValidateManualAgreementRejectsMissingEvidenceBindingGuidance(t *testing.T) {
	root := t.TempDir()
	definition := `{
  "schema_version": "1.0.0",
  "states": {},
  "phase_machines": {},
  "entity_lifecycles": {},
  "transitions": [{
    "id": "TR-006",
    "from": "building",
    "event": "builder_batch_reported",
    "to": "verification",
    "actors": ["orchestrator"],
    "guards": [],
    "actions": [],
    "required_evidence": ["builder_report_record"],
    "description": "Start a review round."
  }],
  "global_transitions": [],
  "forbidden_events": [],
  "invariants": []
}`
	writeMinimalProject(t, root, definition, manualAtRoot)

	err := semantic.ValidateManualAgreement(root)
	if err == nil || !strings.Contains(err.Error(), "--evidence builder_report_record=<reference>") {
		t.Fatalf("expected doctor to report missing evidence binding guidance, got %v", err)
	}
}

// manualPlacement selects where writeMinimalProject writes the Manual.
type manualPlacement int

const (
	manualAtRoot manualPlacement = iota
	manualAtBin
)

// writeMinimalProject creates the smallest project tree that
// ValidateManualAgreement can run against: a docs/control/loop-definition.json plus a
// Manual. The Manual is written at either the project root (source-repo
// placement) or .claude/bin/loop-harness.md (target-project placement),
// depending on the placement argument. Callers can then mutate either side
// to test drift, missing, or malformed scenarios.
func writeMinimalProject(t *testing.T, root, definitionPayload string, placement manualPlacement) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	defPath := filepath.Join(root, "docs", "control", "loop-definition.json")
	defBytes := []byte(definitionPayload)
	if err := os.WriteFile(defPath, defBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	manual := "# loop-harness — Transition Checklist\n"

	var target string
	switch placement {
	case manualAtRoot:
		target = filepath.Join(root, "loop-harness.md")
	case manualAtBin:
		target = filepath.Join(root, ".claude", "bin", "loop-harness.md")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown placement %v", placement)
	}
	if err := os.WriteFile(target, []byte(manual), 0o644); err != nil {
		t.Fatal(err)
	}
}

func manualWithSHA(sha string) string {
	return "# loop-harness — Transition Checklist\n\n" +
		"- **Path**: `loop-harness.md`\n" +
		"- **Loop definition SHA-256**: `" + sha + "`\n"
}
