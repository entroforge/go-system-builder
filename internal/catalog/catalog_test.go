package catalog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/catalog"
)

func TestSkillCatalogMatchesApprovedArchitecture(t *testing.T) {
	root := filepath.Join("..", "..")
	if err := catalog.ValidateSkills(root); err != nil {
		t.Fatal(err)
	}
}

func TestAgentDefinitionsMatchApprovedRoles(t *testing.T) {
	root := filepath.Join("..", "..")
	if err := catalog.ValidateAgents(root); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogRejectsSkillDescriptionThatSummarizesWorkflow(t *testing.T) {
	root := t.TempDir()
	writeSkillFixture(t, root, "loop-orchestration", `---
name: loop-orchestration
description: Use when Loop is active; reads state, selects transitions and commits runtime changes
category: methodology
version: 1.0.0
---
# Loop Orchestration
## Authority
## Required Inputs
## Procedure
## Outputs
## Stop Conditions
## Non-Goals
`)
	err := catalog.ValidateSkill(root, catalog.SkillSpec{
		Name:     "loop-orchestration",
		Category: "methodology",
	})
	if err == nil || !strings.Contains(err.Error(), "description must contain triggers only") {
		t.Fatalf("expected workflow-summary rejection, got %v", err)
	}
}

func writeSkillFixture(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
