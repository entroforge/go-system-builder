package catalog_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/catalog"
)

func nativeRoleRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "agents"), 0755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("../../agents")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join("../../agents", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "agents", entry.Name()), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestNativeRoleCatalogChecksProfessionalOutputsWithoutLegacyActivation(t *testing.T) {
	root := nativeRoleRoot(t)
	if err := catalog.ValidateAgents(root); err != nil {
		t.Fatalf("actual native roles rejected by old execution vocabulary: %v", err)
	}
	for _, tc := range []struct{ role, remove string }{
		{"frontend-builder", "## Output Contract"},
		{"frontend-builder", "SubagentStop"},
		{"frontend-builder", "PlanReport"},
		{"frontend-builder", "RepairResult"},
		{"frontend-builder", "task-brief"},
		{"document-verifier", "## Output Contract"},
		{"document-verifier", "SubagentStop"},
		{"document-verifier", "questionnaire"},
		{"qa", "independent"},
		{"qa", "frozen"},
	} {
		t.Run(tc.role+" missing "+tc.remove, func(t *testing.T) {
			path := filepath.Join(root, "agents", tc.role+".md")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// Both capitalizations may occur in headings/prose. This fixture
			// removes the responsibility rather than adding obsolete words.
			body := regexp.MustCompile("(?i)"+regexp.QuoteMeta(tc.remove)).ReplaceAllString(string(original), "removed")
			if body == string(original) {
				t.Fatal("negative fixture did not remove an actual contract")
			}
			// Keep the original S9 link lure as well as the general destination
			// case: deleting the readable PlanReport duty must still fail.
			if tc.remove == "PlanReport" {
				body = strings.ReplaceAll(body, "#issued-s9-removed-adapter", "#issued-s9-planreport-adapter")
				if !strings.Contains(body, "#issued-s9-planreport-adapter") {
					t.Fatal("original link lure was lost")
				}
			}
			body += "\n[authoring method](../skills/method/SKILL.md#" + tc.remove + ")\n"
			if err := os.WriteFile(path, []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
			defer os.WriteFile(path, original, 0644)
			if err := catalog.ValidateAgents(root); err == nil || !strings.Contains(err.Error(), tc.role) || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.remove)) {
				t.Fatalf("missing native responsibility was accepted or diagnosed as unrelated: %v", err)
			}
		})
	}
}

func TestNativeRoleCatalogPreservesDisplayedLinkResponsibilities(t *testing.T) {
	for _, link := range []string{
		"[PlanReport](../skills/method/SKILL.md#unrelated)",
		"[PlanReport](../skills/method/SKILL.md#method(nested))",
		"[PlanReport][method]\n\n[method]: ../skills/method/SKILL.md#unrelated",
	} {
		root := nativeRoleRoot(t)
		path := filepath.Join(root, "agents", "frontend-builder.md")
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		body := regexp.MustCompile(`(?i)PlanReport`).ReplaceAllString(string(original), "removed")
		if err := os.WriteFile(path, []byte(body+"\n"+link+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := catalog.ValidateAgents(root); err != nil {
			t.Fatalf("displayed responsibility %q rejected: %v", link, err)
		}
	}
}

func TestNativeRoleCatalogRejectsLinkDestinationOnlyResponsibilities(t *testing.T) {
	for _, lure := range []string{
		"[method](../skills/method/SKILL.md#PlanReport)",
		"[method](../skills/method/SKILL.md#nested(PlanReport))",
		"[method][PlanReport]\n\n[PlanReport]: ../skills/method/SKILL.md",
		"<https://example.invalid/PlanReport>",
	} {
		root := nativeRoleRoot(t)
		path := filepath.Join(root, "agents", "frontend-builder.md")
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		body := regexp.MustCompile(`(?i)PlanReport`).ReplaceAllString(string(original), "removed")
		if err := os.WriteFile(path, []byte(body+"\n"+lure+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := catalog.ValidateAgents(root); err == nil || !strings.Contains(err.Error(), "PlanReport") {
			t.Fatalf("link destination %q supplied a responsibility: %v", lure, err)
		}
	}
}
