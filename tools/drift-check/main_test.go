package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanDetectsUnknownStatusInTemplate writes a synthetic template that
// declares a 状态 value outside the canonical ENUM and asserts the scan
// flags it. This is the regression test for the original bug: the
// template declared "discovery / draft / reviewed / locked / changed /
// archived" for REQ while the runtime only accepts "locked", and the
// drift went undetected.
func TestScanDetectsUnknownStatusInTemplate(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"docs/requirements",
		"docs/contracts",
		"docs/design/architecture",
		"docs/tasks",
		"internal/schema/assets",
	} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Use the actual REQ-template.md so the drift case is realistic.
	reqTpl, err := os.ReadFile("../../docs/requirements/REQ-template.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs/requirements/REQ-template.md"), reqTpl, 0o644); err != nil {
		t.Fatal(err)
	}
	// Also include the policy + schema so the protected-events cross-check
	// doesn't fail with "missing file".
	policy, err := os.ReadFile("../../docs/hook-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs/hook-policy.json"), policy, 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("../../internal/schema/assets/hook-policy.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/schema/assets/hook-policy.schema.json"), schema, 0o644); err != nil {
		t.Fatal(err)
	}

	report := scan(root)
	if !report.OK() {
		t.Fatalf("scan should be OK against canonical templates: %+v", report.Findings)
	}
}

// TestScanFlagsInjectedDrift demonstrates the detector catches a known
// drift introduced mid-scan. We mutate the REQ template to declare
// "phantom_state" as a 状态 value, and assert the scanner surfaces it.
func TestScanFlagsInjectedDrift(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs/requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	tpl := "# REQ\n> 状态：locked / phantom_state\n"
	if err := os.WriteFile(filepath.Join(root, "docs/requirements/REQ-template.md"), []byte(tpl), 0o644); err != nil {
		t.Fatal(err)
	}
	report := scan(root)
	if report.OK() {
		t.Fatal("scan must flag phantom_state as drift")
	}
	if len(report.Findings) == 0 {
		t.Fatal("expected at least one finding")
	}
	found := false
	for _, f := range report.Findings {
		if f.Category == "REQ-状态-drift" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected REQ-状态-drift finding, got %+v", report.Findings)
	}
}

// TestScanFlagsUnregisteredProtectedEvent demonstrates the detector
// catches when docs/hook-policy.json names a Claude Code event the
// schema enum does not list.
func TestScanFlagsUnregisteredProtectedEvent(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"docs/requirements",
		"docs",
		"internal/schema/assets",
	} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "docs/requirements/REQ-template.md"),
		[]byte("# REQ\n> 状态：locked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Inject a fabricated event into a minimal policy.
	policy := `{"schema_version":"1.1.0","policy_id":"x","version":"v1.0.0","mode":"enforce","available_profiles":{"enforce":{"default":true,"description":"x"},"audit":{"default":false,"description":"y"}},"protected_events":["PreToolUse","ImaginaryEvent"],"protected_paths":[],"extension_protected_commands":[],"rules":[]}`
	if err := os.WriteFile(filepath.Join(root, "docs/hook-policy.json"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("../../internal/schema/assets/hook-policy.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/schema/assets/hook-policy.schema.json"), schema, 0o644); err != nil {
		t.Fatal(err)
	}

	report := scan(root)
	if report.OK() {
		t.Fatal("scan must flag ImaginaryEvent as protected-events-drift")
	}
	found := false
	for _, f := range report.Findings {
		if f.Category == "protected-events-drift" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected protected-events-drift finding, got %+v", report.Findings)
	}
}
