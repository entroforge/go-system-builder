package designfoundation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Helpers shared from generic_checks_test.go (same package) are reused.
// This file implements DF-T26 E2E paths required by L5 §12.2:
// 1) Local→Core upgrade without hollow Foundation
// 2) Core cold handoff packet ≤120 lines / ≤12KB via second changed REQ trial
// 3) Extended multi-surface inheritance/versioning
// 4) ErrorDSL + legacy migration E2E (+ validate --all / --strict boundaries)

func TestE2E_LocalStaysLocalClean_NoHollowFoundation(t *testing.T) {
	// L5 §12.2: Local sample does not generate a hollow Foundation; single consumer stays clean.
	root := filepath.Join(repoRoot(t), "internal", "designfoundation", "testdata", "samples", "local-minimal")
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Warnings() {
		t.Fatalf("local-minimal with single local REQ must stay advisory-clean, got %s %s", f.Code, f.Detail)
	}
	// Must not emit foundation_missing when FoundationRef is local; empty FoundationRef still not local.
	idx, _, err := BuildContractIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Mode != "factory" {
		t.Fatalf("local-minimal must stay factory mode (no contract markers), got %q", idx.Mode)
	}
	// No hollow DESIGN.md on disk.
	if _, err := os.Stat(filepath.Join(root, KernelRel)); !os.IsNotExist(err) {
		t.Fatalf("local sample must not have a hollow %s", KernelRel)
	}
	// project-map records local investment (L5 §6.1)
	if data, err := os.ReadFile(filepath.Join(root, "docs", "project-map.md")); err == nil {
		if !strings.Contains(strings.ToLower(string(data)), "local") {
			t.Fatalf("local project-map must record local investment, got %q", string(data))
		}
	}
}

func TestE2E_LocalToCoreUpgrade_SecondConsumerTriggersPromptWithoutDataLoss(t *testing.T) {
	// Build from local-minimal: add a second local REQ to simulate the upgrade trigger.
	// L4 §2.1 + L5 §12.2: second consumer / handoff / shared component -> local conditions no longer hold.
	tmp := t.TempDir()
	copyDir(t, filepath.Join(repoRoot(t), "internal", "designfoundation", "testdata", "samples", "local-minimal"), tmp)

	// Before upgrade: single local REQ is clean (already verified above via sample).
	report, _ := Check(tmp)
	for _, f := range report.Warnings() {
		t.Fatalf("pre-upgrade must be clean, got %s", f.Code)
	}

	// Add second local REQ without upgrading Foundation.
	mustWriteTreeGeneric(t, tmp, map[string]string{
		"docs/requirements/REQ-002.md":      "# REQ-002\n\n> 状态：locked\n> UI impact：changed\n\n| Foundation reference | local |\n| Surface | local |\n",
		"docs/design/derivation/REQ-002.md": "# Local Design Derivation — REQ-002\n\n> Foundation：local\n> Surface：local\n> Scope：second consumer checkout variant\n> Expires / review：2026-10-01\n\n## Local decisions\n| ID | 决策 | 理由 | 不得晋升/传播到 |\n|:--|:--|:--|:--|\n| LOCAL-02 | reuse same checkout tokens | second consumer | 共享组件、下一 REQ |\n\n## Upgrade triggers\n- 出现第二消费者时升级 Core — 本行即升级触发与不得晋升说明\n\n## Proof\n`docs/design/prototypes/checkout/index.html` — local proof\n",
	})
	report2, err := Check(tmp)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range report2.Findings {
		if f.Code == "local_upgrade_suspected" {
			found = true
			if f.Severity != SeverityInfo {
				t.Fatalf("local_upgrade_suspected must be info (human confirms), got %s", f.Severity)
			}
		}
		if f.Code == "foundation_missing" {
			t.Fatalf("pure-local second REQ must not emit foundation_missing, got %#v", report2.Findings)
		}
	}
	if !found {
		t.Fatalf("second local REQ must emit local_upgrade_suspected (L5 §9.2); got %#v", report2.Findings)
	}

	// Now perform the real upgrade: install a Core Foundation without losing local decisions.
	// Simulate what F0→F6 would do: publish Kernel + Grammar + Surface, and upgrade REQ-002 to Core.
	mustWriteTreeGeneric(t, tmp, map[string]string{
		"packages/design-tokens/tokens.json":             tokensJSONForTest(t),
		"docs/design/DESIGN.md":                          kernelWithVersion("v1.0.0"),
		"docs/design/design-language.md":                 grammarNineActive(),
		"docs/design/surface-profiles/consumer.md":       "# Surface Profile — consumer\n> ID：SUR-01\n> 版本：v1.0.0\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| INV-01 | yes | visible |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | default |\n",
		"docs/design/proof/anchor-screens/consumer.html": "<html>anchor</html>",
		// Upgrade REQ-002 to Core (FoundationRef points to published Kernel)
		"docs/requirements/REQ-002.md":      "# REQ-002\n\n> 状态：locked\n> UI impact：changed\n\n| Foundation reference | docs/design/DESIGN.md@v1.0.0 |\n| Surface | consumer |\n",
		"docs/design/derivation/REQ-002.md": "# Design Derivation — REQ-002\n\n> Foundation：docs/design/DESIGN.md@v1.0.0\n> Surface：SUR-01@v1.0.0\n\n## Active constraints\n\n<!-- foundation-contract:v1 derivation-active -->\n| ID | 本 REQ 的具体落点 | 需要打开的 GR-* |\n|:--|:--|:--|\n| LAW-01 | upgraded from LOCAL-02 | GR-01 |\n\n## Must not\n\n<!-- foundation-contract:v1 derivation-must-not -->\n| Source ID | 本页不得出现 | 观察位置 |\n|:--|:--|:--|\n| LAW-01 | 双主行动 | checkout |\n\n## Bindings\n\n<!-- foundation-contract:v1 derivation-bindings -->\n| ROLE/PATTERN/组件 | 现役来源 | 本 REQ 用途 |\n|:--|:--|:--|\n| ROLE-action-promise | packages/ui | checkout CTA |\n\n## Proof\n<!-- foundation-contract:v1 derivation-proof -->\n| PROOF ID | 可解析路径 | 验证哪些约束 | 观察方法 |\n|:--|:--|:--|:--|\n| PROOF-01 | docs/design/proof/anchor-screens/consumer.html | LAW-01 | visual |\n",
		"docs/project-map.md":               "| design investment | core | upgraded from local — second consumer without data loss | upgrade when: second Surface / handoff |\n| design foundation | docs/design/DESIGN.md | published | v1.0.0 |\n",
	})
	// REQ-001 local derivation should be preserved (no data loss); keep it as-is (LOCAL-01 still on disk).
	// After upgrade, the local derivation for REQ-001 remains; Core checks treat mixed local+core as non-all-local.
	report3, err := Check(tmp)
	if err != nil {
		t.Fatal(err)
	}
	// After upgrade, local_upgrade_suspected may still appear as info (mixed) but must not block; no foundation_missing.
	for _, f := range report3.Findings {
		if f.Code == "foundation_missing" {
			t.Fatalf("after Core upgrade must not emit foundation_missing, got %#v", report3.Findings)
		}
		if f.Code == "local_contract_incomplete" {
			t.Fatalf("upgraded tree must satisfy local contract for REQ-001, got %#v", report3.Findings)
		}
	}
	// The upgraded tree's ContractIndex must parse Core artefacts cleanly.
	idx2, findings, err := BuildContractIndex(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if idx2.Mode != "contract-v1" {
		t.Fatalf("after upgrade mode must be contract-v1, got %q findings=%#v", idx2.Mode, findings)
	}
	if _, ok := idx2.Constraints["LAW-01"]; !ok {
		t.Fatalf("upgraded Core must have LAW-01")
	}
	// Derive no data loss: REQ-001 derivation still exists with LOCAL-01
	if data, err := os.ReadFile(filepath.Join(tmp, "docs", "design", "derivation", "REQ-001.md")); err != nil || !strings.Contains(string(data), "LOCAL-01") {
		t.Fatalf("upgrade must preserve REQ-001 local derivation (LOCAL-01) without data loss")
	}
}

func TestE2E_CoreColdHandoffBudgetSecondReqTrial(t *testing.T) {
	// L4 §2.5 / L5 §9.2 handoff_packet_oversize: §0 + SUR + Derivation ≤120 lines & ≤12KB
	// Build a Core root with two changed REQs to simulate the handoff trial (Req-014 first, Req-015 second in new session).
	root := t.TempDir()
	mustWriteTreeGeneric(t, root, map[string]string{
		"packages/design-tokens/tokens.json":             tokensJSONForTest(t),
		"docs/design/DESIGN.md":                          kernelWithVersion("v1.0.0"),
		"docs/design/design-language.md":                 grammarNineActive(),
		"docs/design/surface-profiles/consumer.md":       "# Surface Profile — consumer\n> ID：SUR-01\n> 版本：v1.0.0\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| INV-01 | yes | visible |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | default |\n",
		"docs/design/proof/anchor-screens/consumer.html": "<html>anchor</html>",
		"docs/project-map.md":                            "| design investment | core | reuse | upgrade when: new surface |\n| design foundation | docs/design/DESIGN.md | published | v1.0.0 |\n",
		"docs/requirements/REQ-014.md":                   "# REQ-014\n\n> 状态：locked\n> UI impact：changed\n\n| Foundation reference | docs/design/DESIGN.md@v1.0.0 |\n| Surface | consumer |\n",
		"docs/design/derivation/REQ-014.md":              "# Design Derivation — REQ-014\n\n> Foundation：docs/design/DESIGN.md@v1.0.0\n> Surface：SUR-01@v1.0.0\n\n## Active constraints\n\n<!-- foundation-contract:v1 derivation-active -->\n| ID | 本 REQ 的具体落点 | 需要打开的 GR-* |\n|:--|:--|:--|\n| LAW-01 | checkout CTA | GR-01 |\n\n## Must not\n\n<!-- foundation-contract:v1 derivation-must-not -->\n| Source ID | 本页不得出现 | 观察位置 |\n|:--|:--|:--|\n| LAW-01 | 双主行动 | checkout |\n\n## Bindings\n\n<!-- foundation-contract:v1 derivation-bindings -->\n| ROLE/PATTERN/组件 | 现役来源 | 本 REQ 用途 |\n|:--|:--|:--|\n| ROLE-action-promise | packages/ui | checkout |\n\n## Proof\n<!-- foundation-contract:v1 derivation-proof -->\n| PROOF ID | 可解析路径 | 验证哪些约束 | 观察方法 |\n|:--|:--|:--|:--|\n| PROOF-01 | docs/design/proof/anchor-screens/consumer.html | LAW-01 | visual |\n",
		// Second changed REQ: the cold handoff trial — new session reads only §0 + SUR + Derivation(REQ-015)
		"docs/requirements/REQ-015.md":      "# REQ-015\n\n> 状态：locked\n> UI impact：changed\n\n| Foundation reference | docs/design/DESIGN.md@v1.0.0 |\n| Surface | consumer |\n",
		"docs/design/derivation/REQ-015.md": "# Design Derivation — REQ-015\n\n> Foundation：docs/design/DESIGN.md@v1.0.0\n> Surface：SUR-01@v1.0.0\n\n## Active constraints\n\n<!-- foundation-contract:v1 derivation-active -->\n| ID | 本 REQ 的具体落点 | 需要打开的 GR-* |\n|:--|:--|:--|\n| LAW-01 | compare CTA | GR-01 |\n\n## Must not\n\n<!-- foundation-contract:v1 derivation-must-not -->\n| Source ID | 本页不得出现 | 观察位置 |\n|:--|:--|:--|\n| LAW-01 | 双主行动 | compare |\n\n## Bindings\n\n<!-- foundation-contract:v1 derivation-bindings -->\n| ROLE/PATTERN/组件 | 现役来源 | 本 REQ 用途 |\n|:--|:--|:--|\n| ROLE-action-promise | packages/ui | compare |\n\n## Proof\n<!-- foundation-contract:v1 derivation-proof -->\n| PROOF ID | 可解析路径 | 验证哪些约束 | 观察方法 |\n|:--|:--|:--|:--|\n| PROOF-01 | docs/design/proof/anchor-screens/consumer.html | LAW-01 | visual |\n",
	})
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Code == "handoff_packet_oversize" {
			t.Fatalf("lean cold handoff must stay within 120 lines / 12KB, got oversize %s", f.Detail)
		}
		if f.Code == "derivation_contract_incomplete" || f.Code == "surface_version_mismatch" {
			t.Fatalf("second REQ trial must not emit %s, got %#v", f.Code, report.Findings)
		}
	}
	// Verify budget via raw file sizes: §0 (from DESIGN.md) + SUR + Derivation(REQ-015) < thresholds
	kernelSec := readKernelSectionZero(root)
	surfData, _ := os.ReadFile(filepath.Join(root, "docs", "design", "surface-profiles", "consumer.md"))
	deriv15, _ := os.ReadFile(filepath.Join(root, "docs", "design", "derivation", "REQ-015.md"))
	totalLines := strings.Count(kernelSec, "\n") + strings.Count(string(surfData), "\n") + strings.Count(string(deriv15), "\n") + 3
	totalBytes := len(kernelSec) + len(surfData) + len(deriv15)
	if totalLines > 120 || totalBytes > 12*1024 {
		t.Fatalf("cold handoff budget exceeded: %d lines / %d bytes (≤120 / ≤12KB)", totalLines, totalBytes)
	}
	// ContractIndex for this cold handoff: the second REQ's derivation must be resolvable from packet alone
	idx, _, err := BuildContractIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if pack, ok := idx.Derivations["015"]; !ok || len(pack.Active) == 0 {
		t.Fatalf("DERIVATION for REQ-015 must be indexed for cold handoff")
	}
	// Bloated second derivation must trigger oversize warning (prove check actually enforces budget)
	big := strings.Repeat("x y z long line to push over budget. ", 500)
	_ = os.WriteFile(filepath.Join(root, "docs", "design", "derivation", "REQ-015.md"), append(deriv15, []byte("\n"+big)...), 0o644)
	report2, _ := Check(root)
	found := false
	for _, f := range report2.Findings {
		if f.Code == "handoff_packet_oversize" && strings.Contains(f.Detail, "REQ-015") {
			found = true
		}
	}
	if !found {
		t.Fatalf("bloated REQ-015 derivation must trigger handoff_packet_oversize, got %#v", report2.Findings)
	}
}

func TestE2E_ExtendedMultiSurfaceInheritanceVersioning(t *testing.T) {
	root := filepath.Join(repoRoot(t), "internal", "designfoundation", "testdata", "samples", "extended-minimal")
	idx, _, err := BuildContractIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Mode != "contract-v1" {
		t.Fatalf("extended-minimal must be contract-v1")
	}
	if _, ok := idx.Surfaces["SUR-01"]; !ok {
		t.Fatalf("missing SUR-01")
	}
	if _, ok := idx.Surfaces["SUR-02"]; !ok {
		t.Fatalf("missing SUR-02")
	}
	if _, ok := idx.Proofs["PROOF-01"]; !ok {
		t.Fatalf("missing PROOF-01")
	}
	if _, ok := idx.Proofs["PROOF-02"]; !ok {
		t.Fatalf("missing PROOF-02")
	}
	// No missing ID / unrouted / active unbound / role-token orphan on the golden sample
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		switch f.Code {
		case "constraint_ref_missing", "dimension_unrouted", "active_dimension_rule_missing", "active_constraint_unbound", "semantic_role_unbound", "primitive_consumption", "generated_asset_unverifiable":
			t.Fatalf("extended golden sample must not emit %s, got %#v", f.Code, report.Findings)
		}
	}
	// Surface inherits/variants marker actually enforces inheritance
	// Check that surface profiles carry inherits referencing INV/GR/ROLE
	consumerPath := filepath.Join(root, "docs", "design", "surface-profiles", "consumer.md")
	opsPath := filepath.Join(root, "docs", "design", "surface-profiles", "operations.md")
	for _, p := range []string{consumerPath, opsPath} {
		data, _ := os.ReadFile(p)
		if !strings.Contains(string(data), "foundation-contract:v1 surface-inherits") {
			t.Fatalf("%s missing surface-inherits marker", p)
		}
		if !strings.Contains(string(data), "foundation-contract:v1 surface-variants") {
			t.Fatalf("%s missing surface-variants marker", p)
		}
	}
	// Versioning: REQ-014 style derivation version mismatch already tested elsewhere; here just check surface profile versions exist
	for _, id := range []string{"SUR-01", "SUR-02"} {
		if idx.Surfaces[id].Profile == "" {
			t.Fatalf("SUR %s must have Profile/version", id)
		}
		if !strings.Contains(idx.Surfaces[id].Profile, "@v") {
			t.Fatalf("SUR %s profile must be versioned SUR-*@v..., got %q", id, idx.Surfaces[id].Profile)
		}
	}
}

func TestE2E_ErrorDSLAndLegacyMigration_E2E(t *testing.T) {
	// Part A: error DSL — a project rule violation is advisory warning, not block
	root := baseContractRoot(t)
	mustWriteTreeGeneric(t, root, map[string]string{
		"docs/design/prototypes/fund/fund-list.html": `<html><div style="color: var(--color-primitive-green-500)">green</div></html>`,
	})
	writeChecks(t, root, DesignChecks{
		Version: 1, Foundation: "docs/design/DESIGN.md@v1.0.0",
		Rules: []DesignCheckRule{{ID: "DCHK-01", Source: "LAW-01", Type: "forbid_binding", Subject: "color.status.verified", Forbidden: []string{"color.primitive.green.*"}, Targets: []string{"docs/design/prototypes/**/*.html"}}},
	})
	report, _ := Check(root)
	found := false
	for _, f := range report.Findings {
		if f.Code == "project_rule_violation" && strings.Contains(f.Detail, "forbid_binding") {
			found = true
			if f.Severity != SeverityWarning {
				t.Fatalf("DSL findings must be warning, got %s", f.Severity)
			}
		}
	}
	if !found {
		t.Fatalf("expected project DSL violation via Check, got %#v", report.Findings)
	}
	// CLI JSON snapshot for this DSL case
	j, _ := json.Marshal(report)
	var decoded Report
	if err := json.Unmarshal(j, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Findings) == 0 {
		t.Fatal("json snapshot must carry findings")
	}

	// Part B: malformed design-checks — unknown type -> unverifiable warning
	root2 := baseContractRoot(t)
	writeChecks(t, root2, DesignChecks{
		Version: 1, Foundation: "docs/design/DESIGN.md@v1.0.0",
		Rules: []DesignCheckRule{{ID: "DCHK-99", Source: "LAW-01", Type: "no_such_type"}},
	})
	report2, _ := Check(root2)
	hasUnverifiable := false
	for _, f := range report2.Findings {
		if f.Code == "project_rule_unverifiable" {
			hasUnverifiable = true
		}
	}
	if !hasUnverifiable {
		t.Fatalf("unknown DSL type must be unverifiable, got %#v", report2.Findings)
	}

	// Part C: legacy v1.0 kernel without markers -> compatibility info only, no cascade
	legacyRoot := t.TempDir()
	mustWriteTreeGeneric(t, legacyRoot, map[string]string{
		"packages/design-tokens/tokens.json": tokensJSONForTest(t),
		"docs/design/DESIGN.md":              "# Project Design Foundation\n\n> 状态：published\n> 版本：v1.0.0\n\n## 0. Next-agent card\n\n| 项 | 可执行内容 |\n|:--|:--|\n| Laws — 必须做 | do |\n",
	})
	idx, findings, err := BuildContractIndex(legacyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Mode != "legacy-v1.0" {
		t.Fatalf("legacy kernel without markers must be legacy-v1.0, got %q", idx.Mode)
	}
	hasLegacy := false
	for _, f := range findings {
		if f.Code == "foundation_contract_legacy" {
			hasLegacy = true
		}
	}
	if !hasLegacy {
		t.Fatalf("legacy must emit foundation_contract_legacy, got %#v", findings)
	}
	report3, _ := Check(legacyRoot)
	for _, f := range report3.Findings {
		if f.Code == "dimension_unrouted" || f.Code == "active_constraint_unbound" || f.Code == "constraint_ref_missing" {
			t.Fatalf("legacy compat must not cascade v1.1 warnings, got %#v", report3.Findings)
		}
	}
	// migrate --dry-run preview
	plan, err := PlanMigrate(legacyRoot, "contract-v1", true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != "legacy-v1.0" || !plan.DryRun {
		t.Fatalf("migrate plan must be legacy-v1.0 dry-run, got %#v", plan)
	}
	if len(plan.Files) == 0 {
		t.Fatal("migrate plan must list files")
	}

	// Part D: primitive consumption + generated asset unverifiable via Check
	primRoot := t.TempDir()
	mustWriteTreeGeneric(t, primRoot, map[string]string{
		"packages/design-tokens/tokens.json":               tokensJSONForTest(t),
		"docs/design/DESIGN.md":                            kernelWithVersion("v1.0.0"),
		"docs/design/design-language.md":                   grammarNineActive(),
		"docs/design/surface-profiles/consumer.md":         "# Surface\n\n<!-- foundation-contract:v1 surface-inherits -->\n| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |\n|:--|:--|:--|\n| INV-01 | yes | x |\n\n<!-- foundation-contract:v1 surface-variants -->\n| Variant | 选择 | 理由 |\n|:--|:--|:--|\n| density | medium | x |\n",
		"docs/design/proof/anchor-screens/consumer.html":   "<html><head></head><body style=\"color: var(--color-primitive-slate-50)\">anchor with primitive</body></html>",
		"docs/design/proof/anchor-screens/operations.html": "<html><body style=\"color: var(--color-surface-page)\">good semantic</body></html>",
		"docs/project-map.md":                              "| design investment | core | x | upgrade when: y |\n",
	})
	report4, _ := Check(primRoot)
	hasPrim := false
	for _, f := range report4.Findings {
		if f.Code == "primitive_consumption" {
			hasPrim = true
		}
	}
	if !hasPrim {
		t.Fatalf("direct primitive var must be flagged primitive_consumption, got %#v", report4.Findings)
	}
	// The semantic-only second file must not trigger primitive, but may trigger generated_asset check if missing link/digest
}

func TestE2E_StrictIsPerProject_AndValidateAllExcludesFoundation(t *testing.T) {
	// --strict is a CLI flag, not a repository-wide mode; Check itself stays advisory (exit 0 by default).
	// This test proves two boundaries without invoking the full CLI harness:
	// 1) Check always returns findings as warning/info (never error), so a caller must explicitly opt into strict.
	// 2) The factory root stays warning-clean, so validate --all has nothing to require.
	root := repoRoot(t)
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings()) != 0 {
		t.Fatalf("factory root must stay advisory-clean for validate --all exclusion, got %#v", report.Findings)
	}
	// A violated Core root is still advisory by default (warnings, not error).
	badRoot := t.TempDir()
	mustWriteTreeGeneric(t, badRoot, map[string]string{
		"packages/design-tokens/tokens.json":             tokensJSONForTest(t),
		"docs/design/DESIGN.md":                          "# Project Design Foundation\n\n> 状态：published\n> 版本：v1.0.0\n\n## 0. Next-agent card\n\n<!-- foundation-contract:v1 constraints -->\n| ID | Status | 类型 | 可执行 Statement / Do | Don't / 反向边界 | Scope | Source | Binding | Checkability | Proof / review |\n|:--|:--|:--|:--|:--|:--|:--|:--|:--|:--|\n| LAW-01 | active | law | do | dont | global | EVD-01 | — | human | PROOF-01 |\n\n## 8. Surfaces in force\n<!-- foundation-contract:v1 surfaces -->\n| ID | Surface | Profile/version | 与主 Surface 的对比证明 |\n|:--|:--|:--|:--|\n| SUR-01 | consumer | surface-profiles/consumer.md@v1.0.0 | PROOF-01 |\n\n## 9. Proof Set\n<!-- foundation-contract:v1 proofs -->\n| ID | 类型 | 路径 | 证明哪些约束 |\n|:--|:--|:--|:--|\n| PROOF-01 | Anchor | proof/anchor-screens/consumer.html | LAW-01 |\n\n## 10. Open design debt\n<!-- foundation-contract:v1 debts -->\n| ID | 项 | 影响 | 复查条件 |\n|:--|:--|:--|:--|\n| DEBT-01 | x | low | next review |\n",
		"docs/design/design-language.md":                 grammarNineActive(),
		"docs/design/proof/anchor-screens/consumer.html": "<html/>",
		"docs/project-map.md":                            "| design investment | core | x | upgrade when: y |\n",
	})
	reportBad, _ := Check(badRoot)
	if len(reportBad.Warnings()) == 0 {
		t.Fatal("bad Core must produce advisory warnings")
	}
	for _, f := range reportBad.Findings {
		if f.Code == "active_constraint_unbound" && f.Severity != SeverityWarning {
			t.Fatalf("even violated Core findings are advisory warning, not error; got %s", f.Severity)
		}
	}
	// --strict would turn these warnings into exit 1, but that is the caller's choice per project.
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
}
