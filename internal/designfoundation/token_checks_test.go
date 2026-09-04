package designfoundation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func grammarWithRole(roleID, binding string) string {
	return "# Design Grammar\n\n> 编译自：DESIGN.md@v1.0.0\n> 版本：v1.0.0\n\n## 1. Dimension coverage\n\n<!-- foundation-contract:v1 dimensions -->\n| 维度 | 状态 active/inherited/debt/N/A | 依据约束 | 继承源或 DEBT | Proof |\n|:--|:--|:--|:--|:--|\n| Information | active | LAW-01 | — | PROOF-01 |\n| Composition | N/A | — | x | — |\n| Color | N/A | — | x | — |\n| Typography | N/A | — | x | — |\n| Shape & Surface | N/A | — | x | — |\n| Image & Icon | N/A | — | x | — |\n| Interaction | N/A | — | x | — |\n| Content | N/A | — | x | — |\n| Motion | N/A | — | x | — |\n\n## 3. Compilation and selection rules\n\n<!-- foundation-contract:v1 grammar-rules -->\n| ID | Dimensions | Source constraints | 条件/任务 | 优先采用 | 避免 | 允许变化 | Bindings | Proof |\n|:--|:--|:--|:--|:--|:--|:--|:--|:--|\n| GR-01 | Information | LAW-01 | x | y | z | a | " + roleID + " | PROOF-01 |\n\n<!-- foundation-contract:v1 bindings -->\n| ID | 含义 | Source GR-* | Token / component / pattern | 状态 active/reserved/debt |\n|:--|:--|:--|:--|:--|\n| " + roleID + " | 承诺 | GR-01 | " + binding + " | active |\n"
}

func TestTokenChecks_SemanticRoleUnbound_EmptyBinding(t *testing.T) {
	root := t.TempDir()
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json":             tokensJSONForTest(t),
		"docs/design/DESIGN.md":                          kernelWithVersion("v1.0.0"),
		"docs/design/design-language.md":                 grammarWithRole("ROLE-action-promise", "—"),
		"docs/design/surface-profiles/consumer.md":       "# Surface\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| LAW-01 | yes | x |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | x |\n",
		"docs/design/proof/anchor-screens/consumer.html": "<html/>",
		"docs/project-map.md":                            "| design investment | core | x | upgrade when: y |\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range report.Findings {
		if f.Code == "semantic_role_unbound" && strings.Contains(f.Detail, "ROLE-action-promise") {
			found = true
			if f.Severity != SeverityWarning {
				t.Fatalf("must be warning")
			}
		}
	}
	if !found {
		t.Fatalf("expected semantic_role_unbound for empty binding, got %#v", report.Findings)
	}
}

func TestTokenChecks_SemanticRoleUnbound_PrimitiveRejected(t *testing.T) {
	root := t.TempDir()
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json":             tokensJSONForTest(t),
		"docs/design/DESIGN.md":                          kernelWithVersion("v1.0.0"),
		"docs/design/design-language.md":                 grammarWithRole("ROLE-action-promise", "color.primitive.blue-600"),
		"docs/design/surface-profiles/consumer.md":       "# Surface\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| LAW-01 | yes | x |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | x |\n",
		"docs/design/proof/anchor-screens/consumer.html": "<html/>",
		"docs/project-map.md":                            "| design investment | core | x | upgrade when: y |\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range report.Findings {
		if f.Code == "semantic_role_unbound" && strings.Contains(f.Detail, "primitive") {
			found = true
		}
	}
	if !found {
		t.Fatalf("active ROLE bound to primitive must emit semantic_role_unbound, got %#v", report.Findings)
	}
}

func TestTokenChecks_SemanticRoleUnbound_ComponentAndSemanticPass(t *testing.T) {
	for _, binding := range []string{"color.action.promise", "packages/ui/Confirm", "color.action.promise, packages/ui/Confirm", "PAT-confirm"} {
		root := t.TempDir()
		mustWriteTreeGeneric(t, root, map[string]string{
			"packages/design-tokens/tokens.json":             tokensJSONForTest(t),
			"docs/design/DESIGN.md":                          kernelWithVersion("v1.0.0"),
			"docs/design/design-language.md":                 grammarWithRole("ROLE-action-promise", binding),
			"docs/design/surface-profiles/consumer.md":       "# Surface\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| LAW-01 | yes | x |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | x |\n",
			"docs/design/proof/anchor-screens/consumer.html": "<html/>",
			"docs/project-map.md":                            "| design investment | core | x | upgrade when: y |\n",
		})
		report, err := Check(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range report.Findings {
			if f.Code == "semantic_role_unbound" {
				t.Fatalf("binding %q should not emit semantic_role_unbound, got %#v", binding, report.Findings)
			}
		}
	}
}

func TestTokenChecks_PrimitiveConsumption(t *testing.T) {
	root := t.TempDir()
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json":             tokensJSONForTest(t),
		"docs/design/DESIGN.md":                          kernelWithVersion("v1.0.0"),
		"docs/design/design-language.md":                 grammarNineActive(),
		"docs/design/surface-profiles/consumer.md":       "# Surface Profile — consumer\n> ID：SUR-01\n> 版本：v1.0.0\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| INV-01 | yes | x |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | x |\n",
		"docs/design/proof/anchor-screens/consumer.html": "<html><div style=\"color: var(--color-primitive-blue-600)\">x</div></html>",
		"docs/design/prototypes/fund/fund-list.html":    "<html><div style=\"color: var(--color-action-promise)\">ok</div></html>",
		"docs/project-map.md":                            "| design investment | core | x | upgrade when: y |\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range report.Findings {
		if f.Code == "primitive_consumption" {
			found = true
			if !strings.Contains(f.Path, "anchor-screens/consumer.html") {
				t.Fatalf("primitive_consumption should point to file with primitive var, got path %s", f.Path)
			}
		}
	}
	if !found {
		t.Fatalf("expected primitive_consumption for --color-primitive- var, got %#v", report.Findings)
	}
	for _, f := range report.Findings {
		if f.Code == "primitive_consumption" && strings.Contains(f.Path, "fund-list.html") {
			t.Fatalf("semantic var must not trigger primitive_consumption, got %#v", report.Findings)
		}
	}
	mustWriteTreeGeneric(t, root, map[string]string{
		"docs/design/proof/style-tiles/direction-a.html": "<html><div style=\"color: var(--color-primitive-blue-600)\"></div></html>",
	})
	report2, _ := Check(root)
	for _, f := range report2.Findings {
		if f.Code == "primitive_consumption" && strings.Contains(f.Path, "style-tiles") {
			t.Fatalf("style-tiles must be excluded from primitive_consumption, got %#v", report2.Findings)
		}
	}
}

func TestTokenChecks_GeneratedAssetUnverifiable(t *testing.T) {
	root := t.TempDir()
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json":             tokensJSONForTest(t),
		"docs/design/DESIGN.md":                          kernelWithVersion("v1.0.0"),
		"docs/design/design-language.md":                 grammarNineActive(),
		"docs/design/surface-profiles/consumer.md":       "# Surface Profile — consumer\n> ID：SUR-01\n> 版本：v1.0.0\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| INV-01 | yes | x |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | x |\n",
		"docs/design/proof/anchor-screens/consumer.html": "<html><head></head><body style=\"color: var(--color-action-promise)\">no link</body></html>",
		"docs/design/prototypes/fund/fund-list.html":    "<html><head><link rel=\"stylesheet\" href=\"/packages/design-tokens/tokens.css\"></head><body style=\"color: var(--color-action-promise)\">ok</body></html>",
		"docs/project-map.md":                            "| design investment | core | x | upgrade when: y |\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	foundAnchor := false
	for _, f := range report.Findings {
		if f.Code == "generated_asset_unverifiable" && strings.Contains(f.Path, "anchor-screens/consumer.html") {
			foundAnchor = true
		}
		if f.Code == "generated_asset_unverifiable" && strings.Contains(f.Path, "fund-list.html") {
			t.Fatalf("fund-list.html with tokens.css link must not be unverifiable, got %#v", report.Findings)
		}
	}
	if !foundAnchor {
		t.Fatalf("expected generated_asset_unverifiable for anchor HTML with var but no link/digest, got %#v", report.Findings)
	}
	anchorPath := filepath.Join(root, "docs/design/proof/anchor-screens/consumer.html")
	orig, _ := os.ReadFile(anchorPath)
	_ = os.WriteFile(anchorPath, []byte(strings.Replace(string(orig), "<body", "<!-- Generated from packages/design-tokens/tokens.json --><body", 1)), 0o644)
	report2, _ := Check(root)
	for _, f := range report2.Findings {
		if f.Code == "generated_asset_unverifiable" && strings.Contains(f.Path, "anchor-screens/consumer.html") {
			t.Fatalf("with digest, should not emit unverifiable, got %#v", report2.Findings)
		}
	}
}
