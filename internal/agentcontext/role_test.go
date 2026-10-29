package agentcontext_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
)

func removeSkillBlock(data []byte) string {
	source := string(data)
	start := strings.Index(source, "skills:") + len("skills:")
	tail := strings.Index(source[start:], "\n---")
	return source[:start] + source[start+tail:]
}
func TestFirstBuilderCoreRolePreservesEveryAuthorityFieldAndBody(t *testing.T) {
	original, err := os.ReadFile("../../agents/backend-builder.md")
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the frozen legacy generator with its former broad preload;
	// newly installed roles already carry their actual core list.
	legacy := bytes.Replace(original, []byte("skills:\n"), []byte("skills:\n  - gorm\n  - gin\n"), 1)
	projected, err := agentcontext.RenderCoreRole("backend-builder", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if removeSkillBlock(original) != removeSkillBlock(projected) {
		t.Fatal("core generation changed non-Skill fields or responsibility body")
	}
	frontmatter := strings.SplitN(string(projected), "---", 3)[1]
	for _, name := range []string{"agent-dispatch", "backend-engineering", "testing-strategy", "code-quality"} {
		if !strings.Contains(frontmatter, "  - "+name+"\n") {
			t.Fatalf("missing core %s", name)
		}
	}
	for _, name := range []string{"gorm", "gin", "s3-object-storage", "jwt-authentication"} {
		if strings.Contains(frontmatter, "  - "+name+"\n") {
			t.Fatalf("undeclared stack was still preloaded: %s", name)
		}
	}
	if len(projected) >= len(legacy) || !bytes.Equal(projected, original) {
		t.Fatal("generation did not remove broad preload")
	}
	// The generation is not an activation producer and cannot mutate its source.
	reread, _ := os.ReadFile("../../agents/backend-builder.md")
	if !bytes.Equal(original, reread) {
		t.Fatal("generator modified the installed/source role")
	}
}

func TestInstalledBuilderRolesDeliverCoreAndReachableSpecialties(t *testing.T) {
	for _, role := range []string{"backend-builder", "frontend-builder", "test-builder"} {
		t.Run(role, func(t *testing.T) {
			path := filepath.Join("..", "..", "agents", role+".md")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			parts := strings.SplitN(string(data), "---", 3)
			var loaded []string
			for _, line := range strings.Split(parts[1], "\n") {
				if strings.HasPrefix(line, "  - ") {
					loaded = append(loaded, strings.TrimPrefix(line, "  - "))
				}
			}
			if !reflect.DeepEqual(loaded, agentcontext.CoreSkills(role)) {
				t.Fatalf("installed role loads %v, registration selects %v", loaded, agentcontext.CoreSkills(role))
			}
			links := regexp.MustCompile(`\]\((\.\./skills/[^)]+/SKILL\.md)\)`).FindAllStringSubmatch(parts[2], -1)
			if len(links) == 0 {
				t.Fatal("core role lost conditional specialty sources")
			}
			for _, link := range links {
				if _, err := os.Stat(filepath.Join(filepath.Dir(path), link[1])); err != nil {
					t.Fatalf("unreachable specialty %s: %v", link[1], err)
				}
			}
		})
	}
}
func TestCoreRoleRejectsAmbiguousFrontmatterAndKeepsCRLFAuthorityBytes(t *testing.T) {
	source := []byte("---\r\nname: backend-builder\r\ntools: Read, Write\r\npermissionMode: default\r\nskills:\r\n  - gorm\r\nmaxTurns: 9\r\n---\r\nExact body\r\n")
	projected, err := agentcontext.RenderCoreRole("backend-builder", source)
	if err != nil || !bytes.HasSuffix(projected, []byte("maxTurns: 9\r\n---\r\nExact body\r\n")) || !bytes.Contains(projected, []byte("tools: Read, Write\r\npermissionMode: default\r\n")) {
		t.Fatalf("CRLF authority changed: %v %s", err, projected)
	}
	for _, bad := range []string{"---\nname: backend-builder\nskills: []\n---\nbody", "---\nname: backend-builder\nskills:\n  - a\nskills:\n  - b\n---\nbody", "---\nname: foreign-role\nskills:\n  - a\n---\nbody"} {
		if _, err := agentcontext.RenderCoreRole("backend-builder", []byte(bad)); err == nil {
			t.Fatal("ambiguous/different role was generated")
		}
	}
	if _, err := agentcontext.RenderCoreRole("frontend-builder", source); err == nil {
		t.Fatal("unsupported rollout role became enabled")
	}
}
