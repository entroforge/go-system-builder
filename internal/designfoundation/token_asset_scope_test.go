package designfoundation

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func designAssetTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			tree[rel] = "directory"
			return nil
		}
		data, err := os.ReadFile(path)
		tree[rel] = string(data)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestExistingGeneratedTokenCSSIsComparedWithoutWritingOrCountingDefinitions(t *testing.T) {
	root := baseContractRoot(t)
	path, err := EmitCSSFile(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, drift := range []bool{false, true} {
		if drift {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, []byte("/* actual generated-file drift */\n")...), 0644); err != nil {
				t.Fatal(err)
			}
		}
		before := designAssetTree(t, root)
		report, err := Check(root)
		if err != nil {
			t.Fatal(err)
		}
		match, mismatch, scope := false, false, false
		for _, f := range report.Findings {
			match = match || f.Code == "generated_asset_match" && f.Path == TokensCSSRel
			mismatch = mismatch || f.Code == "generated_asset_unverifiable" && f.Path == TokensCSSRel
			scope = scope || f.Code == "design_token_scope" && strings.Contains(f.Detail, "docs/design") && strings.Contains(f.Detail, "src/")
			if f.Code == "primitive_consumption" && f.Path == TokensCSSRel {
				t.Error("generated primitive definitions were treated as product consumption")
			}
		}
		if match == drift || mismatch != drift || !scope {
			t.Errorf("existing CSS not compared honestly: drift=%v match=%v mismatch=%v scope=%v", drift, match, mismatch, scope)
		}
		if !reflect.DeepEqual(before, designAssetTree(t, root)) {
			t.Fatal("check regenerated an asset or wrote other project state")
		}
	}
}

func TestGeneratedSourceHintIsNotAnAuthenticatedDigestAndDoesNotRequireCSS(t *testing.T) {
	root := baseContractRoot(t)
	const path = "docs/design/prototypes/current.html"
	mustWriteTreeGeneric(t, root, map[string]string{path: "<!-- Generated from docs/design/tokens/tokens.json --><p style=\"color:var(--color-action-promise)\">hint</p>"})
	before := designAssetTree(t, root)
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	hint, unmeasured := false, false
	for _, f := range report.Findings {
		hint = hint || f.Code == "generated_asset_observation" && f.Path == path && strings.Contains(f.Detail, "source hint") && strings.Contains(f.Detail, "not")
		unmeasured = unmeasured || f.Code == "generated_asset_unmeasured" && f.Path == TokensCSSRel
		if f.Code == "generated_asset_unverifiable" && f.Path == TokensCSSRel {
			t.Errorf("absent optional generated asset became mandatory work: %+v", f)
		}
	}
	if !hint || !unmeasured {
		t.Errorf("a comment was silently promoted to source verification: hint=%v comparisonNotPerformed=%v", hint, unmeasured)
	}
	if !reflect.DeepEqual(before, designAssetTree(t, root)) {
		t.Fatal("read-only check created a missing generated file or other state")
	}
}

func TestPrimitiveTextReferencesStayWithinDocumentedDesignScope(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		reference  bool
	}{
		{"definition only", ":root { --color-primitive-blue-600: #2563eb; }", false},
		{"different longer name", "p { color:var(--color-primitive-blue-600-extra); }", false},
		{"actual var reference", "p { color:var( --color-primitive-blue-600 ); }", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := baseContractRoot(t)
			const current = "docs/design/prototypes/current.css"
			outside := "color:var(--color-primitive-blue-600); color:#01abfe;"
			mustWriteTreeGeneric(t, root, map[string]string{current: tc.text, "src/Product.tsx": outside, "docs/design/proof/style-tiles/choice.html": outside, "docs/design/proof/portable/DESIGN.md": outside})
			before := designAssetTree(t, root)
			report, err := Check(root)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, f := range report.Findings {
				if f.Code == "primitive_consumption" {
					found = true
					if f.Path != current || !strings.Contains(f.Detail, "text") {
						t.Errorf("primitive scan overstated execution or scan scope: %+v", f)
					}
				}
				if strings.HasPrefix(f.Code, "unregistered_") && f.Path != current {
					t.Errorf("excluded material entered the color scan: %+v", f)
				}
			}
			if found != tc.reference {
				t.Errorf("a definition/name was confused with a var reference: found=%v want=%v", found, tc.reference)
			}
			if !reflect.DeepEqual(before, designAssetTree(t, root)) {
				t.Fatal("bounded text checks wrote project state")
			}
		})
	}
}
