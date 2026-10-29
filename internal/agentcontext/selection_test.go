package agentcontext_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
)

func TestShadowSelectionKeepsAuthorityAndUsesRegisteredReasons(t *testing.T) {
	original := []string{"security-review", "database-change"}
	s, err := agentcontext.Select("../..", "backend-builder", []string{"QA-SECURITY"}, original)
	if err != nil {
		t.Fatal(err)
	}
	if s.Mode != "shadow" || s.ProfileVersion != "1.0.0" || len(s.ProfileSHA256) != 64 || !reflect.DeepEqual(s.AuthorizationSkillRefs, original) {
		t.Fatalf("changed capability source: %+v", s)
	}
	names := map[string]bool{}
	for _, skill := range s.Selected {
		names[skill.Name] = true
		if skill.SHA256 == "" || skill.Source == "" || len(skill.Reasons) == 0 {
			t.Fatal("unfingerprinted source")
		}
	}
	for _, name := range []string{"agent-dispatch", "backend-engineering", "testing-strategy", "code-quality", "security-review", "database-change"} {
		if !names[name] {
			t.Fatalf("lost mandatory/registered Skill %s", name)
		}
	}
	for _, name := range []string{"gorm", "s3-object-storage", "gin", "jwt-authentication"} {
		if names[name] {
			t.Fatalf("guessed stack Skill %s", name)
		}
	}
	if !reflect.DeepEqual(original, []string{"security-review", "database-change"}) {
		t.Fatal("mutated original manifest skill_refs")
	}
	if len(s.LegacySkills) <= len(s.Selected) {
		t.Fatal("shadow baseline was silently reduced")
	}
}

func TestMissingInstalledSkillCannotFallBackToFactory(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{".claude/skills/agent-dispatch/SKILL.md", "skills/agent-dispatch/SKILL.md"} {
		path := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, []byte("factory cannot repair installed source"), 0600)
	}
	if _, err := agentcontext.Select(root, "unknown-role", nil, nil); err == nil || !strings.Contains(err.Error(), "mixed asset layout") {
		t.Fatalf("mixed installed/factory authority accepted: %v", err)
	}
	os.RemoveAll(filepath.Join(root, "skills"))
	os.Remove(filepath.Join(root, ".claude/skills/agent-dispatch/SKILL.md"))
	if _, err := agentcontext.Select(root, "unknown-role", nil, nil); err == nil {
		t.Fatal("missing installed Skill became a ready selection")
	}
}

func TestUnknownProfileIsExplicitLegacyAndUnknownSkillRejected(t *testing.T) {
	selection, err := agentcontext.Select("../..", "future-role", nil, []string{})
	if err != nil || selection.Mode != "legacy" {
		t.Fatalf("legacy fallback: %+v %v", selection, err)
	}
	if _, err := agentcontext.Select("../..", "backend-builder", nil, []string{"future-unknown-skill"}); err == nil {
		t.Fatal("unknown Skill invented knowledge/authority")
	}
}

func TestE2ESelectionUsesResponsibilityForBrowserMethods(t *testing.T) {
	for _, responsibility := range []string{"E2E-BEHAVIOR", "E2E-USER-FLOW", "E2E-CONSOLE-NETWORK"} {
		selection, err := agentcontext.Select("../..", "e2e-tester", []string{responsibility}, agentcontext.RequiredSkills(responsibility))
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, skill := range selection.Selected {
			names[skill.Name] = true
		}
		browser := responsibility != "E2E-BEHAVIOR"
		if names["playwright-e2e"] != browser || names["e2e-browser-testing"] != browser {
			t.Fatalf("%s browser selection: %+v", responsibility, selection.Selected)
		}
		if !names["testing-strategy"] {
			t.Fatal("lost general test method")
		}
	}
}
