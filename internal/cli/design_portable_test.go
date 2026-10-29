package cli_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/designfoundation"
)

func TestDesignPortableExportsSourceFactsWithoutInventingProductChoices(t *testing.T) {
	for _, populated := range []bool{true, false} {
		name := "missing optional design sources"
		if populated {
			name = "declared custom design"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{designfoundation.TokensJSONRel: `{}`}
			if populated {
				files = map[string]string{
					designfoundation.TokensJSONRel: `{
  "color":{"primitive":{"custom":{"$type":"color","$value":"#123abc"}},"signal":{"$type":"color","$value":"{color.primitive.custom}","$description":"Only after explicit confirmation"}},
  "font":{"family":{"reading":{"$type":"fontFamily","$value":"Project Serif"}},"size":{"reading":{"$type":"dimension","$value":"19px"}}},
  "space":{"reading":{"$type":"dimension","$value":"13px"}},
  "rounded":{"panel":{"$type":"dimension","$value":"2px"}}
}`,
					designfoundation.KernelRel:  "# Foundation\n\n## 0. Next-agent card\n\nLAW-71: Two equal actions are intentional.\n\n## 3. Design Thesis\n\nDense documentary reading.\n\n## 6. Rejected direction history\n\nDFD-71: Reject dashboard decoration.\n",
					designfoundation.GrammarRel: "# Grammar\n\n## 3. Compilation and selection rules\n\nGR-71: Use visible shadows to express overlap.\n\n## 4. Variant catalog\n\nreading = roomy\n\n## 5. Semantic roles and patterns\n\nROLE-reading = font.family.reading\n\n## Typography\n\nNo display headline.\n\n## Composition\n\nDocument first.\n",
				}
			}
			for rel, body := range files {
				path := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, designfoundation.PortableRel)), 0755); err != nil {
				t.Fatal(err)
			}
			before := dryRunTree(t, root)
			out, diagnostic, code := runCLI(t, root, "design-foundation", "export-portable", "--root", root)
			if code != 0 || !strings.Contains(out, "not authority") {
				t.Fatalf("export failed: code=%d stdout=%s stderr=%s", code, out, diagnostic)
			}
			after := dryRunTree(t, root)
			data, err := os.ReadFile(filepath.Join(root, designfoundation.PortableRel))
			if err != nil {
				t.Fatal(err)
			}
			body := string(data)
			delete(after, designfoundation.PortableRel)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("export changed original sources or wrote files other than the portable snapshot")
			}
			for _, fabricated := range []string{"fontFamily: system-ui", "fontSize: 16px", "fontWeight: 400", "lineHeight: 1.5", "Emphasis is scarce", "single promise-type action color", "Metadata uses the meta size", "Use the space scale (xs–xl)", "Prefer tonal layers", "needs a Grammar exception", "at most one primary commitment"} {
				if strings.Contains(body, fabricated) {
					t.Errorf("export introduced an unapproved design choice: %q", fabricated)
				}
			}
			if populated {
				for _, fact := range []string{"color.signal", "#123abc", "Only after explicit confirmation", "font.family.reading", "Project Serif", "font.size.reading", "19px", "space.reading", "13px", "rounded.panel", "2px", "LAW-71", "Dense documentary reading", "DFD-71", "GR-71", "reading = roomy", "ROLE-reading", "No display headline", "Document first", designfoundation.KernelRel, designfoundation.GrammarRel, designfoundation.TokensJSONRel} {
					if !strings.Contains(body, fact) {
						t.Errorf("source fact or provenance lost from snapshot: %q", fact)
					}
				}
			} else if !strings.Contains(body, "未提取") || strings.Contains(body, "F6") {
				t.Error("missing sources need an honest missing-excerpt notice, not a fabricated publication step")
			}
			if strings.Contains(body, "## Components") {
				t.Fatal("export included component APIs")
			}
		})
	}
}

func TestDesignPortableReadErrorDoesNotOverwriteExistingSnapshot(t *testing.T) {
	for _, broken := range []string{designfoundation.KernelRel, designfoundation.GrammarRel} {
		t.Run(filepath.Base(broken), func(t *testing.T) {
			root := t.TempDir()
			for rel, content := range map[string]string{designfoundation.TokensJSONRel: `{}`, designfoundation.PortableRel: "existing snapshot\n"} {
				path := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Join(root, broken), 0700); err != nil {
				t.Fatal(err)
			}
			before := dryRunTree(t, root)
			out, diagnostic, code := runCLI(t, root, "design-foundation", "export-portable", "--root", root)
			if code == 0 || !strings.Contains(diagnostic, broken) {
				t.Errorf("unreadable source was treated as absent: code=%d stdout=%s stderr=%s", code, out, diagnostic)
			}
			if !reflect.DeepEqual(before, dryRunTree(t, root)) {
				t.Fatal("export overwrote old snapshot despite a real source read error")
			}
		})
	}
}
