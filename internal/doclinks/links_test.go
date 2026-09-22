package doclinks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinksAndRealAnchors(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "target.md"), []byte("# Real\n```html\n<a id=\"fake\"></a>\n```\n`<a id=\"inline\"></a>`\n"), 0644)
	for _, anchor := range []string{"real", "fake", "inline", "missing"} {
		os.WriteFile(filepath.Join(root, "index.md"), []byte("[target](target.md#"+anchor+")"), 0644)
		err := Validate(root)
		if (err == nil) != (anchor == "real") {
			t.Fatalf("anchor %s: %v", anchor, err)
		}
	}
	os.WriteFile(filepath.Join(root, "index.md"), []byte("[target](missing.md)"), 0644)
	if err := Validate(root); err == nil || !strings.Contains(err.Error(), "missing.md") {
		t.Fatal(err)
	}
}
func TestRelocateSkill(t *testing.T) {
	got := Relocate([]byte("[protocol](../../docs/control/agent-protocol.md#rules)"), "skills/test/SKILL.md", ".claude/skills/test/SKILL.md", func(s string) string { return s })
	if string(got) != "[protocol](../../../docs/control/agent-protocol.md#rules)" {
		t.Fatal(string(got))
	}
}
func TestRelocatePreservesCodeExamples(t *testing.T) {
	input := "`[code](../code.md)`\n```md\n[fenced](../fenced.md)\n```\n[live](../live.md)"
	got := string(Relocate([]byte(input), "skills/a/SKILL.md", ".claude/skills/a/SKILL.md", func(s string) string { return s }))
	if !strings.Contains(got, "`[code](../code.md)`") || !strings.Contains(got, "[fenced](../fenced.md)") || !strings.Contains(got, "[live](../../../skills/live.md)") {
		t.Fatal(got)
	}
}
func TestAbsoluteRootLinksCannotTraverseOutside(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "index.md"), []byte("[outside](/docs/../../outside.md)"), 0644)
	if err := Validate(root); err == nil || !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("unexpected containment result: %v", err)
	}
}

func TestUnrelatedCodeSymlinkDoesNotBlockDocuments(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "src"), 0755)
	os.WriteFile(filepath.Join(root, "src/real.go"), []byte("package source"), 0644)
	if err := os.Symlink("real.go", filepath.Join(root, "src/alias.go")); err != nil {
		t.Skip(err)
	}
	os.WriteFile(filepath.Join(root, "README.md"), []byte("# Docs"), 0644)
	if err := Validate(root); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "external.md"), []byte("# External"), 0644)
	os.Symlink(outside, filepath.Join(root, "src/external"))
	os.WriteFile(filepath.Join(root, "README.md"), []byte("[outside](src/external/external.md)"), 0644)
	if err := Validate(root); err == nil || !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("escaped symlink target accepted: %v", err)
	}
}

func TestDocumentDirectorySymlinkCannotHideInputs(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "docs")); err != nil {
		t.Skip(err)
	}
	if err := Validate(root); err == nil {
		t.Fatal("symlinked document root silently skipped")
	}
}

func TestAngleDestinationsValidateAndRelocate(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "target file.md"), []byte("# Real"), 0644)
	for _, tc := range []struct {
		href string
		ok   bool
	}{{"<missing.md>", false}, {"<target file.md#missing>", false}, {"<target file.md#real>", true}} {
		os.WriteFile(filepath.Join(root, "index.md"), []byte("[target]("+tc.href+")"), 0644)
		if err := Validate(root); (err == nil) != tc.ok {
			t.Fatalf("%s: %v", tc.href, err)
		}
	}
	got := string(Relocate([]byte("[x](<../../docs/control/agent-protocol.md#s0>)"), "skills/x/SKILL.md", ".claude/skills/x/SKILL.md", func(s string) string { return s }))
	if got != "[x](<../../../docs/control/agent-protocol.md#s0>)" {
		t.Fatal(got)
	}
}

func TestAngleReferenceDefinitions(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "target file.md"), []byte("# Real"), 0644)
	os.WriteFile(filepath.Join(root, "index.md"), []byte("[x][target]\n[target]: <target file.md#real>"), 0644)
	if err := Validate(root); err != nil {
		t.Fatal(err)
	}
	got := string(Relocate([]byte("[target]: <../../docs/target file.md#real>"), "skills/x/SKILL.md", ".claude/skills/x/SKILL.md", func(s string) string { return s }))
	if got != "[target]: <../../../docs/target file.md#real>" {
		t.Fatal(got)
	}
}
