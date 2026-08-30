package semantic_test

import (
	"crypto/sha256"
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

// When both candidate paths exist and the root one matches, doctor must use
// the root one (lookup order is root-first). Otherwise a stale .claude/bin
// copy from before a `make manual` cycle would silently pass.
func TestValidateManualAgreementPrefersRootWhenBothPresent(t *testing.T) {
	root := t.TempDir()
	// Root copy has the matching SHA.
	writeMinimalProject(t, root, "match", manualAtRoot)
	// Bin copy has a different payload → its SHA would not match.
	binDir := filepath.Join(root, ".claude", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(binDir, "loop-harness.md"),
		[]byte(manualWithSHA("deadbeef")),
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

func TestValidateManualAgreementFailsWhenDefinitionDrifts(t *testing.T) {
	root := t.TempDir()
	writeMinimalProject(t, root, "original", manualAtRoot)
	// Edit loop-definition.json AFTER the manual was generated to simulate
	// drift (e.g. someone added a transition but forgot to regen the manual).
	if err := os.WriteFile(
		filepath.Join(root, "docs", "loop-definition.json"),
		[]byte("{\"extra\":\"drift\"}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	err := semantic.ValidateManualAgreement(root)
	if err == nil {
		t.Fatal("expected drift error, got nil")
	}
	if !strings.Contains(err.Error(), "manual stale") {
		t.Fatalf("expected 'manual stale' error, got %v", err)
	}
	if !strings.Contains(err.Error(), "loop-harness manual") {
		t.Fatalf("error must mention recovery command; got %v", err)
	}
}

func TestValidateManualAgreementFailsWhenHeaderMalformed(t *testing.T) {
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
	err := semantic.ValidateManualAgreement(root)
	if err == nil {
		t.Fatal("expected header malformed error, got nil")
	}
	if !strings.Contains(err.Error(), "manual header malformed") {
		t.Fatalf("expected 'manual header malformed' error, got %v", err)
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

func TestExtractManualDefinitionSHAParsesValidHeader(t *testing.T) {
	markdown := `# loop-harness — Transition Checklist

> What ` + "`loop-harness`" + ` checks...

- **Path**: ` + "`.claude/bin/loop-harness.md`" + `
- **Harness version**: dev
- **Loop definition SHA-256**: ` + "`6c743951312a9c95d1545c6cb44519d1e7ee02ae6270cfc98fcd4a7b91c0ce7c`" + `
- **Generated**: 2026-07-06T07:33:32Z UTC
`
	got, err := semantic.ExtractManualDefinitionSHA(markdown)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	want := "6c743951312a9c95d1545c6cb44519d1e7ee02ae6270cfc98fcd4a7b91c0ce7c"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestExtractManualDefinitionSHARejectsNonHex(t *testing.T) {
	markdown := "- **Loop definition SHA-256**: `XYZnonhex\n"
	_, err := semantic.ExtractManualDefinitionSHA(markdown)
	if err == nil {
		t.Fatal("expected error for non-hex SHA, got nil")
	}
}

// manualPlacement selects where writeMinimalProject writes the Manual.
type manualPlacement int

const (
	manualAtRoot manualPlacement = iota
	manualAtBin
)

// writeMinimalProject creates the smallest project tree that
// ValidateManualAgreement can run against: a docs/loop-definition.json plus a
// matching Manual whose embedded SHA-256 is computed from the on-disk
// definition. The Manual is written at either the project root (source-repo
// placement) or .claude/bin/loop-harness.md (target-project placement),
// depending on the placement argument. Callers can then mutate either side
// to test drift, missing, or malformed scenarios.
func writeMinimalProject(t *testing.T, root, definitionPayload string, placement manualPlacement) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	defPath := filepath.Join(root, "docs", "loop-definition.json")
	defBytes := []byte(definitionPayload)
	if err := os.WriteFile(defPath, defBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256Hex(defBytes)
	manual := manualWithSHA(sum)

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

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	const hex = "0123456789abcdef"
	out := make([]byte, 64)
	for i, b := range sum {
		out[i*2] = hex[b>>4]
		out[i*2+1] = hex[b&0xF]
	}
	return string(out)
}
