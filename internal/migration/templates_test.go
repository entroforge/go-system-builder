package migration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/migration"
)

func TestHarnessTemplateMigration(t *testing.T) {
	root := filepath.Join("..", "..")
	if err := migration.ValidateTemplates(root); err != nil {
		t.Fatal(err)
	}
}

func TestHookRegistrationCoversDelegationTools(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"PreToolUse": [{"matcher":"*"`) {
		t.Fatalf("PreToolUse must listen to tool events, including reads and delegation: %s", data)
	}
	if !strings.Contains(string(data), `"Stop"`) {
		t.Fatalf("Stop must invoke the Main收工门: %s", data)
	}
	for _, event := range []string{`"PostToolUseFailure"`, `"ConfigChange"`} {
		if !strings.Contains(string(data), event) {
			t.Fatalf("%s must be registered as an audit observer: %s", event, data)
		}
	}
}

func TestMainEntryRoutesThroughReadableSpine(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "AGENTS-template.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Join(strings.Fields(string(data)), " ")
	for _, required := range []string{
		"docs/control/agent-protocol.md", ".claude/loop-state.json", "S0", "S11", "DRIVE()",
		"Describe each responsibility attempt with one status:",
		"`pending`", "`active`", "`blocked`", "`done`", "`cancelled`",
		"The five values describe a responsibility attempt only.",
		"Use `done` consistently for completion.",
		"Each progress reminder gives the exact current questionnaire path.",
		"awaiting_human_release", "Release requires separate human approval.",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("AGENTS-template.md must state the current Main workflow contract: missing %q", required)
		}
	}
}

// TestValidateTemplatesRequiresQuestionnaireGuidance ensures AGENTS carries
// the current user-facing workflow contract rather than implementation-only
// Hook policy details.
func TestValidateTemplatesRequiresQuestionnaireGuidance(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join("..", "..")
	agents, err := os.ReadFile(filepath.Join(root, "AGENTS-template.md"))
	if err != nil {
		t.Fatal(err)
	}
	withoutPath := strings.ReplaceAll(
		strings.Join(strings.Fields(string(agents)), " "),
		"Each progress reminder gives the exact current questionnaire path.",
		"Progress reminders give the questionnaire location.",
	)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS-template.md"), []byte(withoutPath), 0o644); err != nil {
		t.Fatal(err)
	}
	err = migration.ValidateTemplates(dir)
	if err == nil || !strings.Contains(err.Error(), "exact current questionnaire path") {
		t.Fatalf("ValidateTemplates must require the current questionnaire guidance, got %v", err)
	}
}

// TestValidateTemplatesRejectsForbiddenField covers the
// "contains migrated responsibility" branch.
// When a contract file still contains an obsolete team/route
// marker, ValidateTemplates must surface a clear error.
func TestValidateTemplatesRejectsForbiddenField(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join("..", "..")
	agents, err := os.ReadFile(filepath.Join(root, "AGENTS-template.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Start from a valid user-facing contract so this reaches the forbidden
	// team-marker check in ValidateTemplates.
	agents = append(agents, []byte("\nDelivery Verifier Team：yes\n")...)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS-template.md"), agents, 0o644); err != nil {
		t.Fatal(err)
	}
	err = migration.ValidateTemplates(dir)
	if err == nil {
		t.Fatal("ValidateTemplates must fail when AGENTS-template.md contains the forbidden team marker")
	}
	if !strings.Contains(err.Error(), "Delivery Verifier Team") {
		t.Fatalf("forbidden-field error did not mention the marker: %v", err)
	}
}

// TestValidateTemplatesRejectsObsoleteHookEvent covers the
// "obsolete Hook event" branch (templates.go:147-151). PostToolUse is a live
// observation event since L3-S7/L4 (PLAN_REPORT capture); the retired events
// (PermissionRequest / TaskCompleted) must still fail closed.
func TestValidateTemplatesRejectsObsoleteHookEvent(t *testing.T) {
	dir := t.TempDir()
	// A settings.json with the obsolete PermissionRequest event present.
	bad := []byte(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":".claude/bin/loop-harness hook --event PreToolUse"}]}],"SessionStart":[{"hooks":[{"type":"command","command":".claude/bin/loop-harness hook --event SessionStart"}]}],"SubagentStart":[{"hooks":[{"type":"command","command":".claude/bin/loop-harness hook --event SubagentStart"}]}],"SubagentStop":[{"hooks":[{"type":"command","command":".claude/bin/loop-harness hook --event SubagentStop"}]}],"TeammateIdle":[{"hooks":[{"type":"command","command":".claude/bin/loop-harness hook --event TeammateIdle"}]}],"PreCompact":[{"hooks":[{"type":"command","command":".claude/bin/loop-harness hook --event PreCompact"}]}],"PostToolUse":[{"hooks":[{"type":"command","command":".claude/bin/loop-harness hook --event PostToolUse"}]}],"PermissionRequest":[{"hooks":[]}]}}`)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := migration.ValidateTemplates(dir); err == nil {
		t.Fatal("ValidateTemplates must fail when settings.json registers the obsolete PermissionRequest event")
	}
}

// TestValidateTemplatesRequiresPostToolUse flips the L3-S7 requirement:
// PostToolUse(SendMessage) is the PLAN_REPORT capture path and must be
// registered.
func TestValidateTemplatesRequiresPostToolUse(t *testing.T) {
	dir := t.TempDir()
	missing := []byte(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":".claude/bin/loop-harness hook --event PreToolUse"}]}],"SessionStart":[{"hooks":[{"type":"command","command":".claude/bin/loop-harness hook --event SessionStart"}]}],"SubagentStart":[{"hooks":[{"type":"command","command":".claude/bin/loop-harness hook --event SubagentStart"}]}],"SubagentStop":[{"hooks":[{"type":"command","command":".claude/bin/loop-harness hook --event SubagentStop"}]}],"TeammateIdle":[{"hooks":[{"type":"command","command":".claude/bin/loop-harness hook --event TeammateIdle"}]}],"PreCompact":[{"hooks":[{"type":"command","command":".claude/bin/loop-harness hook --event PreCompact"}]}]}}`)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), missing, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := migration.ValidateTemplates(dir); err == nil {
		t.Fatal("ValidateTemplates must fail when settings.json lacks PostToolUse")
	}
}
