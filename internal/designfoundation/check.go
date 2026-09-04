package designfoundation

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
)

type Finding struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path,omitempty"`
	Detail   string   `json:"detail"`
}

type Report struct {
	Root     string    `json:"root"`
	Findings []Finding `json:"findings"`
	Skipped  bool      `json:"skipped,omitempty"`
}

func (r Report) Warnings() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Severity == SeverityWarning {
			out = append(out, f)
		}
	}
	return out
}

// Check is advisory (D3). It never invents aesthetic quality. Missing
// Foundation on a template factory or a non-UI product is information, not
// failure. --strict at the CLI turns warnings into exit 1.
func Check(root string) (Report, error) {
	report := Report{Root: root}
	if na, err := foundationNA(root); err != nil {
		return report, err
	} else if na {
		report.Skipped = true
		report.Findings = append(report.Findings, Finding{
			Code:     "foundation_na",
			Severity: SeverityInfo,
			Path:     "docs/project-map.md",
			Detail:   "project map marks Design Foundation N/A; skip UI language checks",
		})
		return report, nil
	}

	kernelPath := filepath.Join(root, KernelRel)
	kernel, kernelErr := os.ReadFile(kernelPath)
	changedREQs, err := listChangedREQs(root)
	if err != nil {
		return report, err
	}

	isAllLocal := len(changedREQs) > 0
	for _, r := range changedREQs {
		if r.FoundationRef != "local" {
			isAllLocal = false
			break
		}
	}
	if kernelErr != nil {
		if len(changedREQs) == 0 {
			report.Findings = append(report.Findings, Finding{
				Code:     "foundation_absent_ok",
				Severity: SeverityInfo,
				Path:     KernelRel,
				Detail:   "no DESIGN.md and no locked UI-impact=changed REQ; template factory or pre-UI project",
			})
		} else if isAllLocal {
			for _, r := range changedREQs {
				derivation := filepath.Join("docs", "design", "derivation", "REQ-"+r.ID+".md")
				if _, err := os.Stat(filepath.Join(root, derivation)); err != nil {
					report.Findings = append(report.Findings, Finding{
						Code:     "local_contract_incomplete",
						Severity: SeverityWarning,
						Path:     derivation,
						Detail:   "local REQ-" + r.ID + " must have a module Derivation with LOCAL decisions and upgrade triggers; missing " + derivation,
					})
				} else {
					if data, err := os.ReadFile(filepath.Join(root, derivation)); err == nil {
						if !containsLocalUpgradeTrigger(string(data)) {
							report.Findings = append(report.Findings, Finding{
								Code:     "local_contract_incomplete",
								Severity: SeverityWarning,
								Path:     derivation,
								Detail:   "local REQ-" + r.ID + " derivation must describe upgrade triggers and non-promotion scope (不得晋升)",
							})
						}
					}
				}
			}
			if findings := checkLocalUpgradeSuspected(root, changedREQs); len(findings) > 0 {
				report.Findings = append(report.Findings, findings...)
			}
		} else {
			report.Findings = append(report.Findings, Finding{
				Code:     "foundation_missing",
				Severity: SeverityWarning,
				Path:     KernelRel,
				Detail:   "UI impact=changed REQ exists but DESIGN.md is missing; run skills/design-foundation F0–F6 before locking further UI REQs",
			})
		}
	} else {
		status := kernelStatus(string(kernel))
		pending := kernelConfirmationPending(string(kernel))
		if status == "published" && pending {
			report.Findings = append(report.Findings, Finding{
				Code:     "foundation_fake_lock",
				Severity: SeverityWarning,
				Path:     KernelRel,
				Detail:   "DESIGN.md is published while confirmation records still say PENDING; use provisional until a human date is written",
			})
		}
		if status == "provisional" && len(changedREQs) > 0 && !isAllLocal {
			report.Findings = append(report.Findings, Finding{
				Code:     "foundation_provisional",
				Severity: SeverityWarning,
				Path:     KernelRel,
				Detail:   "DESIGN.md is provisional; later REQs must not treat §0 as a published lock",
			})
		} else if status != "published" && status != "provisional" && len(changedREQs) > 0 {
			report.Findings = append(report.Findings, Finding{
				Code:     "foundation_unpublished",
				Severity: SeverityWarning,
				Path:     KernelRel,
				Detail:   "DESIGN.md status is " + status + " while a locked UI-impact=changed REQ exists; finish F6 publish, stay local, or keep provisional until a human confirms",
			})
		}
		// Core+thin may leave Grammar as debt. Only Extended still owes design-language.md
		// after a real publish (not a fake lock).
		if status == "published" && !pending && designInvestment(root) == "extended" {
			if _, err := os.Stat(filepath.Join(root, GrammarRel)); err != nil {
				report.Findings = append(report.Findings, Finding{
					Code:     "grammar_missing",
					Severity: SeverityWarning,
					Path:     GrammarRel,
					Detail:   "extended published Kernel without design-language.md",
				})
			}
		}
	}

	for _, req := range changedREQs {
		// Local investment: REQ cites "local" — do not require published Foundation ref.
		if isAllLocal && req.FoundationRef == "local" {
			continue
		}
		if req.FoundationRef == "" || req.FoundationRef == "pending-foundation" {
			report.Findings = append(report.Findings, Finding{
				Code:     "req_foundation_ref",
				Severity: SeverityWarning,
				Path:     req.Path,
				Detail:   "UI impact=changed REQ should cite docs/design/DESIGN.md@vX.Y.Z after publish; pending-foundation is only valid during F0–F6",
			})
		}
		// For pure-local roots, derivation existence already handled as local_contract_incomplete above.
		if isAllLocal {
			continue
		}
		derivation := filepath.Join("docs", "design", "derivation", "REQ-"+req.ID+".md")
		if _, err := os.Stat(filepath.Join(root, derivation)); err != nil {
			report.Findings = append(report.Findings, Finding{
				Code:     "derivation_missing",
				Severity: SeverityWarning,
				Path:     derivation,
				Detail:   "S2 should write a Design Derivation Note before expanding the module package",
			})
		}
	}

	hexFindings, err := LintUnregisteredHex(root)
	if err != nil {
		return report, err
	}
	report.Findings = append(report.Findings, hexFindings...)

	dupFindings, err := LintDuplicateComponents(root)
	if err != nil {
		return report, err
	}
	report.Findings = append(report.Findings, dupFindings...)

	// Case rules (promise quota / green ban etc.) are not global aesthetics.
	// They must be declared via docs/design/design-checks.json (restricted DSL
	// 5 types, source → LAW/ANTI/INV) and are enforced via generic checks
	// (checkProjectRules). The generic engine never guesses color/role.

	if _, err := os.Stat(filepath.Join(root, KernelRel)); err == nil {
		if idx, idxFindings, idxErr := BuildContractIndex(root); idxErr == nil {
			// ContractIndex mode decides what cascades.
			switch idx.Mode {
			case "legacy-v1.0":
				// Compatibility: only the single legacy info, plus parse-level marker version complaints.
				for _, f := range idxFindings {
					if f.Code == "foundation_contract_legacy" || f.Code == "contract_version_unknown" {
						report.Findings = append(report.Findings, f)
					}
				}
			case "contract-v1":
				report.Findings = append(report.Findings, idxFindings...)
				report.Findings = append(report.Findings, runGenericChecks(root, idx)...)
			case "factory":
				for _, f := range idxFindings {
					if f.Code == "contract_version_unknown" {
						report.Findings = append(report.Findings, f)
					}
				}
			}
			_ = idx
		}
	} else {
		// No kernel: still surface contract-version complaints from deriv/surface probes if any marker exists elsewhere.
		if idx, idxFindings, idxErr := BuildContractIndex(root); idxErr == nil && idx.Mode == "contract-v1" {
			report.Findings = append(report.Findings, idxFindings...)
			report.Findings = append(report.Findings, runGenericChecks(root, idx)...)
			_ = idx
		}
	}
	return report, nil
}

func foundationNA(root string) (bool, error) {
	path := filepath.Join(root, "docs", "project-map.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		lower := strings.ToLower(line)
		if strings.Contains(lower, "design foundation") && strings.Contains(lower, "| n/a") {
			return true, nil
		}
		if strings.Contains(lower, "product surfaces") && strings.Contains(lower, "| none") {
			return true, nil
		}
	}
	return false, scanner.Err()
}

func designInvestment(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "docs", "project-map.md"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(strings.ToLower(line), "design investment") {
			continue
		}
		cells := splitTableLine(line)
		if len(cells) < 2 {
			continue
		}
		val := strings.ToLower(strings.Trim(strings.TrimSpace(cells[1]), "`"))
		if i := strings.IndexAny(val, " /|"); i >= 0 {
			val = val[:i]
		}
		return val
	}
	return ""
}

func kernelConfirmationPending(body string) bool {
	for i, line := range strings.Split(body, "\n") {
		if i > 20 {
			break
		}
		lower := strings.ToLower(line)
		if !strings.Contains(line, "确认") && !strings.Contains(lower, "confirm") {
			continue
		}
		return strings.Contains(strings.ToUpper(line), "PENDING")
	}
	return false
}

func kernelStatus(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "> 状态：") || strings.HasPrefix(strings.ToLower(line), "> status:") {
			rest := line
			if i := strings.Index(rest, "："); i >= 0 {
				rest = rest[i+len("："):]
			} else if i := strings.Index(rest, ":"); i >= 0 {
				rest = rest[i+1:]
			}
			rest = strings.TrimSpace(rest)
			if i := strings.IndexAny(rest, " /"); i >= 0 {
				rest = rest[:i]
			}
			return strings.ToLower(rest)
		}
	}
	return "unknown"
}

type changedREQ struct {
	ID            string
	Path          string
	FoundationRef string
}

var reqFile = regexp.MustCompile(`^REQ-([A-Za-z0-9]+)\.md$`)

func listChangedREQs(root string) ([]changedREQ, error) {
	dir := filepath.Join(root, "docs", "requirements")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []changedREQ
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		m := reqFile.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		rel := filepath.Join("docs", "requirements", entry.Name())
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return nil, err
		}
		body := string(data)
		if !reqTopField(body, "状态", "locked") && !reqTopField(body, "status", "locked") {
			continue
		}
		if !reqUIImpactChanged(body) {
			continue
		}
		out = append(out, changedREQ{
			ID:            m[1],
			Path:          rel,
			FoundationRef: reqTableField(body, "Foundation reference"),
		})
	}
	return out, nil
}

func reqTopField(body, key, want string) bool {
	prefix := "> " + key + "："
	alt := "> " + key + ":"
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		rest := ""
		switch {
		case strings.HasPrefix(trim, prefix):
			rest = strings.TrimSpace(strings.TrimPrefix(trim, prefix))
		case strings.HasPrefix(strings.ToLower(trim), strings.ToLower(alt)):
			rest = strings.TrimSpace(trim[len(alt):])
		default:
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return false
		}
		return strings.EqualFold(fields[0], want)
	}
	return false
}

func reqUIImpactChanged(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "> UI impact：") || strings.HasPrefix(strings.ToLower(trim), "> ui impact:") {
			return strings.Contains(strings.ToLower(trim), "changed")
		}
	}
	return false
}

func reqTableField(body, field string) string {
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, field) {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		if strings.Contains(parts[1], field) {
			return strings.TrimSpace(parts[2])
		}
	}
	return ""
}

func containsLocalUpgradeTrigger(body string) bool {
	lower := strings.ToLower(body)
	hasUpgrade := strings.Contains(lower, "upgrade") || strings.Contains(body, "升级") || strings.Contains(lower, "trigger")
	hasNonPromote := strings.Contains(body, "不得晋升") || strings.Contains(body, "不得传播") || strings.Contains(strings.ToLower(body), "not promote")
	// Local derivation must mention when to upgrade and that local decisions must not be promoted.
	return hasUpgrade && hasNonPromote
}

func checkLocalUpgradeSuspected(root string, changed []changedREQ) []Finding {
	if len(changed) <= 1 {
		// Also consider surface profile appearance as second surface signal.
		surfaceDir := filepath.Join(root, "docs", "design", "surface-profiles")
		if entries, err := os.ReadDir(surfaceDir); err == nil {
			count := 0
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				if !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				low := strings.ToLower(e.Name())
				if strings.Contains(low, "template") || low == "readme.md" {
					continue
				}
				count++
			}
			if count > 0 {
				return []Finding{{
					Code:     "local_upgrade_suspected",
					Severity: SeverityInfo,
					Path:     "docs/design/surface-profiles",
					Detail:   "local investment but a Surface profile exists; re-evaluate F0 and upgrade to Core if this is a second consumer/surface",
				}}
			}
		}
		return nil
	}
	return []Finding{{
		Code:     "local_upgrade_suspected",
		Severity: SeverityInfo,
		Path:     changed[1].Path,
		Detail:   "second UI impact=changed REQ (" + changed[1].ID + ") while Foundation is still local; local conditions likely no longer hold — re-run F0 and upgrade to Core without data loss",
	}}
}
