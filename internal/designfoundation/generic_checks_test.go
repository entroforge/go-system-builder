package designfoundation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustWriteTreeGeneric(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func tokensJSONForTest(t *testing.T) string {
	t.Helper()
	return mustRead(t, filepath.Join(repoRoot(t), TokensJSONRel))
}

func kernelWithVersion(version string) string {
	return "# Project Design Foundation\n\n> 状态：published\n> 版本：" + version + "\n\n## 0. Next-agent card\n\n<!-- foundation-contract:v1 constraints -->\n| ID | Status | 类型 | 可执行 Statement / Do | Don't / 反向边界 | Scope | Source | Binding | Checkability | Proof / review |\n|:--|:--|:--|:--|:--|:--|:--|:--|:--|:--|\n| LAW-01 | active | law | do | dont | global | EVD-01 | GR-01 | human | PROOF-01 |\n| INV-01 | active | invariant | do2 | dont2 | all-surfaces | LAW-01 | GR-01 | static | PROOF-01 |\n\n## 8. Surfaces in force\n<!-- foundation-contract:v1 surfaces -->\n| ID | Surface | Profile/version | 与主 Surface 的对比证明 |\n|:--|:--|:--|:--|\n| SUR-01 | consumer | surface-profiles/consumer.md@v1.0.0 | PROOF-01 |\n\n## 9. Proof Set\n<!-- foundation-contract:v1 proofs -->\n| ID | 类型 | 路径 | 证明哪些约束 |\n|:--|:--|:--|:--|\n| PROOF-01 | Anchor | proof/anchor-screens/consumer.html | LAW-01 |\n\n## 10. Open design debt\n<!-- foundation-contract:v1 debts -->\n| ID | 项 | 影响 | 复查条件 |\n|:--|:--|:--|:--|\n| DEBT-01 | Image 暂弱 | low | next review |\n"
}

func grammarNineActive() string {
	return "# Design Grammar\n\n> 编译自：DESIGN.md@v1.0.0\n> 版本：v1.0.0\n\n## 1. Dimension coverage\n\n<!-- foundation-contract:v1 dimensions -->\n| 维度 | 状态 active/inherited/debt/N/A | 依据约束 | 继承源或 DEBT | Proof |\n|:--|:--|:--|:--|:--|\n| Information | active | LAW-01 | — | PROOF-01 |\n| Composition | active | LAW-01 | — | PROOF-01 |\n| Color | active | LAW-01 | — | PROOF-01 |\n| Typography | active | LAW-01 | — | PROOF-01 |\n| Shape & Surface | active | LAW-01 | — | PROOF-01 |\n| Image & Icon | active | LAW-01 | — | PROOF-01 |\n| Interaction | active | LAW-01 | — | PROOF-01 |\n| Content | active | LAW-01 | — | PROOF-01 |\n| Motion | active | LAW-01 | — | PROOF-01 |\n\n## 3. Compilation and selection rules\n\n<!-- foundation-contract:v1 grammar-rules -->\n| ID | Dimensions | Source constraints | 条件/任务 | 优先采用 | 避免 | 允许变化 | Bindings | Proof |\n|:--|:--|:--|:--|:--|:--|:--|:--|:--|\n| GR-01 | Information, Composition, Color, Typography, Shape & Surface, Image & Icon, Interaction, Content, Motion | LAW-01 | 承诺 | 先依据后行动 | 双主行动 | 文案 | ROLE-action-promise | PROOF-01 |\n\n<!-- foundation-contract:v1 bindings -->\n| ID | 含义 | Source GR-* | Token / component / pattern | 状态 active/reserved/debt |\n|:--|:--|:--|:--|:--|\n| ROLE-action-promise | 承诺 | GR-01 | color.action.promise | active |\n"
}

func TestGenericChecks_NoFalsePositiveOnMinimalContractV1(t *testing.T) {
	// Core-minimal style but with Grammar covering 9 dims so generic checks are clean.
	root := t.TempDir()
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json":                tokensJSONForTest(t),
		"docs/design/DESIGN.md":                             kernelWithVersion("v1.0.0"),
		"docs/design/design-language.md":                    grammarNineActive(),
		"docs/design/surface-profiles/consumer.md":          "# Surface Profile — consumer\n> ID：SUR-01\n> 版本：v1.0.0\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| INV-01 | yes | visible |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | default |\n",
		"docs/design/proof/anchor-screens/consumer.html": "<html>anchor</html>",
		"docs/project-map.md":                           "| design investment | core | reuse | upgrade when: new surface |\n| design foundation | docs/design/DESIGN.md | published | v1.0.0 |\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	// grammar_missing should not appear when grammar exists; only unexpected codes fail.
	for _, f := range report.Findings {
		switch f.Code {
		case "dimension_unrouted", "active_dimension_rule_missing", "active_constraint_unbound", "constraint_ref_missing", "derivation_contract_incomplete", "surface_version_mismatch", "handoff_packet_oversize", "investment_profile_missing":
			t.Fatalf("minimal contract-v1 must not emit %s, got %#v full=%#v", f.Code, f, report.Findings)
		}
	}
}

func TestGenericChecks_ActiveConstraintUnbound(t *testing.T) {
	root := t.TempDir()
	kernel := "# Project Design Foundation\n\n> 状态：published\n> 版本：v1.0.0\n\n## 0. Next-agent card\n\n<!-- foundation-contract:v1 constraints -->\n| ID | Status | 类型 | 可执行 Statement / Do | Don't / 反向边界 | Scope | Source | Binding | Checkability | Proof / review |\n|:--|:--|:--|:--|:--|:--|:--|:--|:--|:--|\n| LAW-01 | active | law | do | dont | global | EVD-01 | — | human | PROOF-01 |\n\n## 8. Surfaces in force\n<!-- foundation-contract:v1 surfaces -->\n| ID | Surface | Profile/version | 与主 Surface 的对比证明 |\n|:--|:--|:--|:--|\n| SUR-01 | consumer | surface-profiles/consumer.md@v1.0.0 | PROOF-01 |\n\n## 9. Proof Set\n<!-- foundation-contract:v1 proofs -->\n| ID | 类型 | 路径 | 证明哪些约束 |\n|:--|:--|:--|:--|\n| PROOF-01 | Anchor | proof/anchor-screens/consumer.html | LAW-01 |\n\n## 10. Open design debt\n<!-- foundation-contract:v1 debts -->\n| ID | 项 | 影响 | 复查条件 |\n|:--|:--|:--|:--|\n| DEBT-01 | x | low | next review |\n"
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json":       tokensJSONForTest(t),
		"docs/design/DESIGN.md":                    kernel,
		"docs/design/design-language.md":           grammarNineActive(),
		"docs/design/proof/anchor-screens/consumer.html": "<html/>",
		"docs/project-map.md":                      "| design investment | core | x | upgrade when: y |\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range report.Findings {
		if f.Code == "active_constraint_unbound" && strings.Contains(f.Detail, "LAW-01") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected active_constraint_unbound for LAW-01 empty Binding, got %#v", report.Findings)
	}
	// must be warning and default exit 0
	for _, f := range report.Findings {
		if f.Code == "active_constraint_unbound" && f.Severity != SeverityWarning {
			t.Fatalf("generic checks must be warning, got %s", f.Severity)
		}
	}
}

func TestGenericChecks_DimensionRoutingAndActiveRule(t *testing.T) {
	root := t.TempDir()
	grammarMissingDim := "# Design Grammar\n\n> 编译自：DESIGN.md@v1.0.0\n> 版本：v1.0.0\n\n## 1. Dimension coverage\n\n<!-- foundation-contract:v1 dimensions -->\n| 维度 | 状态 active/inherited/debt/N/A | 依据约束 | 继承源或 DEBT | Proof |\n|:--|:--|:--|:--|:--|\n| Information | active | LAW-01 | — | PROOF-01 |\n| Color | active | LAW-01 | — | PROOF-01 |\n\n## 3. Compilation and selection rules\n\n<!-- foundation-contract:v1 grammar-rules -->\n| ID | Dimensions | Source constraints | 条件/任务 | 优先采用 | 避免 | 允许变化 | Bindings | Proof |\n|:--|:--|:--|:--|:--|:--|:--|:--|:--|\n| GR-01 | Information | LAW-01 | x | y | z | a | ROLE-action-promise | PROOF-01 |\n\n<!-- foundation-contract:v1 bindings -->\n| ID | 含义 | Source GR-* | Token / component / pattern | 状态 active/reserved/debt |\n|:--|:--|:--|:--|:--|\n| ROLE-action-promise | 承诺 | GR-01 | color.action.promise | active |\n"
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json":             tokensJSONForTest(t),
		"docs/design/DESIGN.md":                          kernelWithVersion("v1.0.0"),
		"docs/design/design-language.md":                 grammarMissingDim,
		"docs/design/surface-profiles/consumer.md":       "# Surface\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| INV-01 | yes | x |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | x |\n",
		"docs/design/proof/anchor-screens/consumer.html": "<html/>",
		"docs/project-map.md":                            "| design investment | core | x | upgrade when: y |\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	hasUnrouted := false
	hasActiveMissing := false
	for _, f := range report.Findings {
		if f.Code == "dimension_unrouted" {
			hasUnrouted = true
		}
		if f.Code == "active_dimension_rule_missing" && strings.Contains(f.Detail, "Color") {
			hasActiveMissing = true
		}
	}
	if !hasUnrouted {
		t.Fatalf("expected dimension_unrouted when 7 dims missing, got %#v", report.Findings)
	}
	if !hasActiveMissing {
		t.Fatalf("expected active_dimension_rule_missing for Color without GR, got %#v", report.Findings)
	}
}

func TestGenericChecks_ConstraintRefMissing(t *testing.T) {
	root := t.TempDir()
	grammarBadRef := "# Design Grammar\n\n> 编译自：DESIGN.md@v1.0.0\n> 版本：v1.0.0\n\n## 1. Dimension coverage\n\n<!-- foundation-contract:v1 dimensions -->\n| 维度 | 状态 active/inherited/debt/N/A | 依据约束 | 继承源或 DEBT | Proof |\n|:--|:--|:--|:--|:--|\n| Information | active | LAW-01 | — | PROOF-01 |\n| Composition | N/A | — | x | — |\n| Color | N/A | — | x | — |\n| Typography | N/A | — | x | — |\n| Shape & Surface | N/A | — | x | — |\n| Image & Icon | N/A | — | x | — |\n| Interaction | N/A | — | x | — |\n| Content | N/A | — | x | — |\n| Motion | N/A | — | x | — |\n\n## 3. Compilation and selection rules\n\n<!-- foundation-contract:v1 grammar-rules -->\n| ID | Dimensions | Source constraints | 条件/任务 | 优先采用 | 避免 | 允许变化 | Bindings | Proof |\n|:--|:--|:--|:--|:--|:--|:--|:--|:--|\n| GR-01 | Information | LAW-99 | x | y | z | a | ROLE-action-promise | PROOF-01 |\n\n<!-- foundation-contract:v1 bindings -->\n| ID | 含义 | Source GR-* | Token / component / pattern | 状态 active/reserved/debt |\n|:--|:--|:--|:--|:--|\n| ROLE-action-promise | 承诺 | GR-01 | color.action.promise | active |\n"
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json":             tokensJSONForTest(t),
		"docs/design/DESIGN.md":                          kernelWithVersion("v1.0.0"),
		"docs/design/design-language.md":                 grammarBadRef,
		"docs/design/surface-profiles/consumer.md":       "# S\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| LAW-01 | yes | x |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | x |\n",
		"docs/design/proof/anchor-screens/consumer.html": "<html/>",
		"docs/project-map.md":                            "| design investment | core | x | upgrade when: y |\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range report.Findings {
		if f.Code == "constraint_ref_missing" && strings.Contains(f.Detail, "LAW-99") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected constraint_ref_missing for LAW-99, got %#v", report.Findings)
	}
}

func TestGenericChecks_DerivationIncompleteAndHandoffAndVersion(t *testing.T) {
	root := t.TempDir()
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json":             tokensJSONForTest(t),
		"docs/design/DESIGN.md":                          kernelWithVersion("v1.0.0"),
		"docs/design/design-language.md":                 grammarNineActive(),
		"docs/design/surface-profiles/consumer.md":       "# Surface Profile — consumer\n> ID：SUR-01\n> 版本：v1.0.0\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| INV-01 | yes | x |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | x |\n",
		"docs/design/proof/anchor-screens/consumer.html": "<html/>",
		"docs/project-map.md":                            "| design investment | core | x | upgrade when: y |\n| design foundation | docs/design/DESIGN.md | published | v1.0.0 |\n",
		"docs/requirements/REQ-014.md":                   "# REQ-014\n\n> 状态：locked\n> UI impact：changed\n\n| Foundation reference | docs/design/DESIGN.md@v9.9.9 |\n| Surface | consumer |\n",
		"docs/design/derivation/REQ-014.md":               "# Design Derivation — REQ-014\n\n> Foundation：docs/design/DESIGN.md@v9.9.9\n> Surface：SUR-01@v9.9.9\n\n## Active constraints\n\n<!-- foundation-contract:v1 derivation-active -->\n| ID | 本 REQ 的具体落点 | 需要打开的 GR-* |\n|:--|:--|:--|\n| LAW-01 | x | GR-01 |\n\n## Must not\n\n<!-- foundation-contract:v1 derivation-must-not -->\n| Source ID | 本页不得出现 | 观察位置 |\n|:--|:--|:--|\n| ANTI-01 | x | y |\n\n## Bindings\n\n<!-- foundation-contract:v1 derivation-bindings -->\n| ROLE/PATTERN/组件 | 现役来源 | 本 REQ 用途 |\n|:--|:--|:--|\n| ROLE-action-promise | packages/ui | commit |\n\n## Proof\n<!-- foundation-contract:v1 derivation-proof -->\n| PROOF ID | 可解析路径 | 验证哪些约束 | 观察方法 |\n|:--|:--|:--|:--|\n| PROOF-01 | docs/design/proof/anchor-screens/missing.html | LAW-01 | visual |\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, f := range report.Findings {
		codes[f.Code] = true
	}
	if !codes["surface_version_mismatch"] {
		t.Fatalf("expected surface_version_mismatch for v9.9.9 vs v1.0.0, got %#v", report.Findings)
	}
	// missing proof path should be derivation_contract_incomplete
	foundProofPath := false
	for _, f := range report.Findings {
		if f.Code == "derivation_contract_incomplete" && strings.Contains(f.Detail, "missing.html") {
			foundProofPath = true
		}
	}
	if !foundProofPath {
		t.Fatalf("expected derivation_contract_incomplete for missing proof path, got %#v", report.Findings)
	}
	// handoff oversize: Derivation file is tiny, so may not oversize yet. Force oversize by bloating derivation.
	// Instead verify that check runs without panic and budget code is present when oversized.
	// Create a bloated derivation to trigger handoff oversize
	big := strings.Repeat("x y z line that is long enough to push over budget. ", 400)
	path := filepath.Join(root, "docs/design/derivation/REQ-014.md")
	orig, _ := os.ReadFile(path)
	_ = os.WriteFile(path, append(orig, []byte("\n"+big)...), 0o644)
	report2, _ := Check(root)
	hasOversize := false
	for _, f := range report2.Findings {
		if f.Code == "handoff_packet_oversize" {
			hasOversize = true
		}
	}
	if !hasOversize {
		t.Fatalf("expected handoff_packet_oversize after bloating, got %#v", report2.Findings)
	}
}

func TestGenericChecks_InvestmentProfileMissing(t *testing.T) {
	root := t.TempDir()
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json":             tokensJSONForTest(t),
		"docs/design/DESIGN.md":                          kernelWithVersion("v1.0.0"),
		"docs/design/design-language.md":                 grammarNineActive(),
		"docs/design/surface-profiles/consumer.md":       "# Surface\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| INV-01 | yes | x |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | x |\n",
		"docs/design/proof/anchor-screens/consumer.html": "<html/>",
		"docs/project-map.md":                            "# Project Map\n\n| baseline | foo | bar |\n",
		"docs/requirements/REQ-014.md":                   "# REQ-014\n\n> 状态：locked\n> UI impact：changed\n\n| Foundation reference | docs/design/DESIGN.md@v1.0.0 |\n",
		"docs/design/derivation/REQ-014.md": "# Design Derivation — REQ-014\n\n> Foundation：docs/design/DESIGN.md@v1.0.0\n> Surface：SUR-01@v1.0.0\n\n## Active constraints\n\n<!-- foundation-contract:v1 derivation-active -->\n| ID | 本 REQ 的具体落点 | 需要打开的 GR-* |\n|:--|:--|:--|\n| LAW-01 | x | GR-01 |\n\n## Must not\n\n<!-- foundation-contract:v1 derivation-must-not -->\n| Source ID | 本页不得出现 | 观察位置 |\n|:--|:--|:--|\n| ANTI-01 | x | y |\n\n## Bindings\n\n<!-- foundation-contract:v1 derivation-bindings -->\n| ROLE/PATTERN/组件 | 现役来源 | 本 REQ 用途 |\n|:--|:--|:--|\n| ROLE-action-promise | packages/ui | x |\n\n## Proof\n<!-- foundation-contract:v1 derivation-proof -->\n| PROOF ID | 可解析路径 | 验证哪些约束 | 观察方法 |\n|:--|:--|:--|:--|\n| PROOF-01 | docs/design/proof/anchor-screens/consumer.html | LAW-01 | visual |\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range report.Findings {
		if f.Code == "investment_profile_missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected investment_profile_missing when project-map lacks investment, got %#v", report.Findings)
	}
}

func TestGenericChecks_LegacyCompatSuppressesCascade(t *testing.T) {
	root := t.TempDir()
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json": tokensJSONForTest(t),
		"docs/design/DESIGN.md":              "# Project Design Foundation\n\n> 状态：published\n> 版本：v1.0.0\n\n## 0. Next-agent card\n\n| 项 | 可执行内容 |\n|:--|:--|\n| Laws — 必须做 | do |\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	hasLegacy := false
	hasCascade := false
	for _, f := range report.Findings {
		if f.Code == "foundation_contract_legacy" {
			hasLegacy = true
		}
		if f.Code == "active_constraint_unbound" || f.Code == "dimension_unrouted" || f.Code == "constraint_ref_missing" {
			hasCascade = true
		}
	}
	if !hasLegacy {
		t.Fatalf("legacy mode must emit foundation_contract_legacy, got %#v", report.Findings)
	}
	if hasCascade {
		t.Fatalf("legacy mode must not cascade generic warnings, got %#v", report.Findings)
	}
}

func TestGenericChecks_FactoryWithNoChangedREQStaysClean(t *testing.T) {
	root := t.TempDir()
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json": tokensJSONForTest(t),
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Warnings() {
		t.Fatalf("factory with no UI work must stay clean, got %s %#v", f.Code, report.Findings)
	}
}
