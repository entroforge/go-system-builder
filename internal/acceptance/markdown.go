package acceptance

import (
	"fmt"
	"sort"
	"strings"
)

// This file renders the 16-section ACC / release-audit Markdown directly
// from the manifest (single source). RC-11 (C-5): the Markdown report is no
// longer a second hand-maintained carrier that drifts from the JSON ledger —
// the manifest remains the machine authority, and the Markdown is a
// projection of it. Generate the body with
// `loop-harness s10 manifest render --file <manifest.json>` instead of
// writing the sections by hand.

// RenderMarkdown renders the human-readable 16-section ACC/release-audit
// report from a decoded manifest and its gate-facing Summary. The manifest
// must be validated first (acceptance.Validate / ValidateForOutcome); the
// renderer performs no validation of its own.
func RenderMarkdown(manifest Manifest, summary Summary) string {
	var b strings.Builder
	if manifest.ManifestType == ManifestReleaseAudit {
		b.WriteString("# Architecture Release Audit\n\n")
	} else {
		b.WriteString("# Acceptance Check Report (ACC)\n\n")
	}
	b.WriteString(fmt.Sprintf("> Runtime：`%s` @ baseline_generation %d, S7 review round %d\n", manifest.RuntimeID, manifest.BaselineGeneration, manifest.ReviewRound))
	b.WriteString("> 来源：本报告由 S10 manifest 渲染生成（single source）；机器完整性以 JSON manifest 为准，\n")
	b.WriteString("> 手工编辑不会改变 Gate 消费的事实。生成命令：\n")
	b.WriteString(">\n> ```text\n> loop-harness s10 manifest render --file <manifest.json>\n> ```\n\n")

	writeCoverageSection(&b, manifest)
	writeCounterevidenceSection(&b, manifest)
	if manifest.ManifestType == ManifestReleaseAudit {
		writeAuditAreaSections(&b, manifest)
	}
	writeRisksSection(&b, manifest)
	writeDebtSection(&b, manifest)
	writeBlockingSection(&b, manifest)
	writeMetricsSection(&b, summary, manifest)
	writeSignoffSection(&b, manifest)
	return b.String()
}

func writeCoverageSection(b *strings.Builder, manifest Manifest) {
	fmt.Fprintf(b, "## Coverage Inventory\n\n| Inventory ID | Category | Source | Expected | Oracle | Owner | Evidence | Disposition |\n|:---|:---|:---|:---|:---|:---|:---|:---|\n")
	for _, item := range manifest.CoverageInventory {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s | %s |\n",
			mdCell(item.ID), mdCell(item.Category), mdCell(strings.Join(item.SourceRefs, ", ")),
			mdCell(item.Expected), mdCell(item.Oracle), mdCell(item.Owner),
			mdCell(strings.Join(item.EvidenceRefs, ", ")), mdCell(dispositionCell(item)))
	}
	b.WriteString("\n")
}

func writeCounterevidenceSection(b *strings.Builder, manifest Manifest) {
	b.WriteString("## Counterevidence Ledger\n\n| Inventory ID | What would disprove PASS? | Evidence | Outcome |\n|:---|:---|:---|:---|\n")
	for _, item := range manifest.Counterevidence {
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n",
			mdCell(item.InventoryID), mdCell(item.Question),
			mdCell(strings.Join(item.EvidenceRefs, ", ")), mdCell(item.Outcome))
	}
	b.WriteString("\n")
}

// writeAuditAreaSections renders the eight release-architecture audit areas
// (state machine / transaction / concurrency / data model / call sites /
// observability / verification evidence / docs-release boundary).
func writeAuditAreaSections(b *strings.Builder, manifest Manifest) {
	areas := make(map[string]AuditArea, len(manifest.AuditAreas))
	for _, area := range manifest.AuditAreas {
		areas[area.ID] = area
	}
	for i, id := range AuditAreaIDs {
		area := areas[id]
		fmt.Fprintf(b, "## Audit Area %d: %s\n\n", i+1, id)
		fmt.Fprintf(b, "- Conclusion: %s\n- Owner: %s\n- Evidence: %s\n\n",
			mdCell(area.Conclusion), mdCell(area.Owner), mdCell(strings.Join(area.EvidenceRefs, ", ")))
	}
}

func writeRisksSection(b *strings.Builder, manifest Manifest) {
	b.WriteString("## Non-Blocking Risks\n\n| Risk ID | Severity | Impact | Owner | Tracking | Recovery point |\n|:---|:---|:---|:---|:---|:---|\n")
	if len(manifest.Risks) == 0 {
		b.WriteString("| (none) | | | | | |\n")
	}
	for _, risk := range manifest.Risks {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s |\n",
			mdCell(risk.ID), mdCell(risk.Severity), mdCell(risk.Impact),
			mdCell(risk.Owner), mdCell(risk.TrackingRef), mdCell(risk.RecoveryPoint))
	}
	b.WriteString("\n")
}

func writeDebtSection(b *strings.Builder, manifest Manifest) {
	b.WriteString("## Technical Debt\n\n| Debt ID | Impact | Owner | Tracking |\n|:---|:---|:---|:---|\n")
	if len(manifest.TechnicalDebt) == 0 {
		b.WriteString("| (none) | | | |\n")
	}
	for _, debt := range manifest.TechnicalDebt {
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n",
			mdCell(debt.ID), mdCell(debt.Impact), mdCell(debt.Owner), mdCell(debt.TrackingRef))
	}
	b.WriteString("\n")
}

func writeBlockingSection(b *strings.Builder, manifest Manifest) {
	b.WriteString("## Blocking Findings\n\n| Finding ID | Route |\n|:---|:---|\n")
	if len(manifest.BlockingFindings) == 0 {
		b.WriteString("| (none) | |\n")
	}
	for _, finding := range manifest.BlockingFindings {
		fmt.Fprintf(b, "| %s | %s |\n", mdCell(finding.ID), mdCell(finding.Route))
	}
	b.WriteString("\n")
}

func writeMetricsSection(b *strings.Builder, summary Summary, manifest Manifest) {
	b.WriteString("## Metrics\n\n")
	m := summary.Metrics
	fmt.Fprintf(b, "- requirement_coverage: %g\n- contract_coverage: %g\n- changed_path_coverage: %g\n", m.RequirementCoverage, m.ContractCoverage, m.ChangedPathCoverage)
	if manifest.ManifestType == ManifestReleaseAudit {
		fmt.Fprintf(b, "- audit_area_coverage: %g\n", m.AuditAreaCoverage)
	}
	fmt.Fprintf(b, "- inventory rows: %d (%d dispositioned)\n- counterevidence rows: %d\n", summary.InventoryCount, summary.DispositionedCount, summary.CounterevidenceCount)
	if manifest.ManifestType == ManifestReleaseAudit {
		fmt.Fprintf(b, "- audit areas: %d / %d\n", summary.AuditAreaCount, len(AuditAreaIDs))
	}
	fmt.Fprintf(b, "- unknown_count: %d\n- unsupported_pass_count: %d\n- blocking_finding_count: %d\n\n", m.UnknownCount, m.UnsupportedPassCount, m.BlockingFindingCount)
	if len(summary.EvidenceRefs) > 0 {
		refs := append([]string(nil), summary.EvidenceRefs...)
		sort.Strings(refs)
		fmt.Fprintf(b, "Evidence references: %s\n\n", strings.Join(refs, ", "))
	}
}

func writeSignoffSection(b *strings.Builder, manifest Manifest) {
	b.WriteString("## Sign-off Questions\n\n")
	for _, item := range manifest.CoverageInventory {
		if item.Disposition == "pass" && len(nonEmpty(item.EvidenceRefs)) > 0 {
			continue
		}
		if item.Disposition == "not_applicable" && strings.TrimSpace(item.NAReason) != "" {
			continue
		}
		fmt.Fprintf(b, "- [ ] %s (%s): %s\n", item.ID, item.Category, item.Expected)
	}
	for _, item := range manifest.Counterevidence {
		if item.Outcome == "pass" || item.Outcome == "not_applicable" {
			continue
		}
		fmt.Fprintf(b, "- [ ] counterevidence %s (%s) is %s — resolve before sign-off\n", item.ID, item.InventoryID, item.Outcome)
	}
	b.WriteString("\n")
}

func dispositionCell(item CoverageItem) string {
	if item.Disposition == "not_applicable" && strings.TrimSpace(item.NAReason) != "" {
		return "not_applicable (" + item.NAReason + ")"
	}
	return item.Disposition
}

// mdCell keeps user-supplied strings from breaking the Markdown table shape.
func mdCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}
