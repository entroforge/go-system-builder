package releasegraph_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/releasegraph"
)

func TestValidateStagedReleaseRequiresHarnessBinary(t *testing.T) {
	root := minimalStage(t)
	err := releasegraph.ValidateStagedRelease(root)
	if err == nil || !strings.Contains(err.Error(), ".claude/bin/loop-harness") {
		t.Fatalf("missing shipped Harness must fail validation: %v", err)
	}
}

func TestValidateStagedReleaseRejectsInstanceArtifacts(t *testing.T) {
	root := minimalStage(t)
	writeStageFile(t, root, ".claude/bin/loop-harness", "binary")
	if err := os.Chmod(filepath.Join(root, ".claude/bin/loop-harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeStageFile(t, root, "docs/tasks/TASK-017.md", "instance")
	err := releasegraph.ValidateStagedRelease(root)
	if err == nil || !strings.Contains(err.Error(), "instance artifact") {
		t.Fatalf("instance TASK must fail validation: %v", err)
	}
}

func TestValidateStagedReleaseDoesNotGateBareOutputFilenames(t *testing.T) {
	root := minimalStage(t)
	writeStageFile(t, root, ".claude/bin/loop-harness", "binary")
	if err := os.Chmod(filepath.Join(root, ".claude/bin/loop-harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeStageFile(t, root, "skills/example/SKILL.md", "---\nname: example\n---\nProduce `flows.md` and `docs/reports/bugs/BUG-NNN.md`.\n")
	if err := releasegraph.ValidateStagedRelease(root); err != nil {
		t.Fatalf("a generated bare filename is not a release dependency: %v", err)
	}
}

func TestValidateStagedReleaseStillRejectsDanglingQualifiedPath(t *testing.T) {
	root := minimalStage(t)
	writeStageFile(t, root, ".claude/bin/loop-harness", "binary")
	if err := os.Chmod(filepath.Join(root, ".claude/bin/loop-harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeStageFile(t, root, "skills/example/SKILL.md", "---\nname: example\n---\nRead `docs/missing/file.md`.\n")
	err := releasegraph.ValidateStagedRelease(root)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("qualified dangling path must still fail: %v", err)
	}
}

func TestValidateStagedReleaseAllowsInstanceDesignFoundationLiveFiles(t *testing.T) {
	root := minimalStage(t)
	writeStageFile(t, root, ".claude/bin/loop-harness", "binary")
	if err := os.Chmod(filepath.Join(root, ".claude/bin/loop-harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeStageFile(t, root, "skills/example/SKILL.md", "---\nname: example\n---\nRead `docs/design/DESIGN.md` and `docs/design/research/evidence-field.md`.\n")
	if err := releasegraph.ValidateStagedRelease(root); err != nil {
		t.Fatalf("instance Foundation live files are not release dependencies: %v", err)
	}
}

func TestValidateStagedReleaseAllowsInstallerCreatedProjectMap(t *testing.T) {
	root := minimalStage(t)
	writeStageFile(t, root, ".claude/bin/loop-harness", "binary")
	if err := os.Chmod(filepath.Join(root, ".claude/bin/loop-harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeStageFile(t, root, "skills/example/SKILL.md", "---\nname: example\n---\nRead the installed project facts from `docs/project-map.md`.\n")
	if err := releasegraph.ValidateStagedRelease(root); err != nil {
		t.Fatalf("installer-created project map is not a release dependency: %v", err)
	}
}

func TestValidateStagedReleaseResolvesSkillLocalReferences(t *testing.T) {
	root := minimalStage(t)
	writeStageFile(t, root, ".claude/bin/loop-harness", "binary")
	if err := os.Chmod(filepath.Join(root, ".claude/bin/loop-harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeStageFile(t, root, "skills/example/references/local.md", "# local authority\n")
	writeStageFile(t, root, "skills/example/SKILL.md", "---\nname: example\n---\nRead `references/local.md`.\n")
	if err := releasegraph.ValidateStagedRelease(root); err != nil {
		t.Fatalf("Skill-local reference should resolve beside the Skill: %v", err)
	}
}

func minimalStage(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeStageFile(t, root, "AGENTS-template.md", "# AGENTS\n")
	writeStageFile(t, root, "prelude.md", "# Prelude\n")
	writeStageFile(t, root, "Makefile", "verify:\n\t@true\n")
	writeStageFile(t, root, "skills/example/SKILL.md", "---\nname: example\n---\n# Example\n")
	return root
}

func writeStageFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
