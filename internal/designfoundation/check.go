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

	if kernelErr != nil {
		if len(changedREQs) == 0 {
			report.Findings = append(report.Findings, Finding{
				Code:     "foundation_absent_ok",
				Severity: SeverityInfo,
				Path:     KernelRel,
				Detail:   "no DESIGN.md and no locked UI-impact=changed REQ; template factory or pre-UI project",
			})
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
		if status != "published" && len(changedREQs) > 0 {
			report.Findings = append(report.Findings, Finding{
				Code:     "foundation_unpublished",
				Severity: SeverityWarning,
				Path:     KernelRel,
				Detail:   "DESIGN.md status is " + status + " while a locked UI-impact=changed REQ exists; finish F6 publish or set pending-foundation only during F0–F6",
			})
		}
		if status == "published" {
			if _, err := os.Stat(filepath.Join(root, GrammarRel)); err != nil {
				report.Findings = append(report.Findings, Finding{
					Code:     "grammar_missing",
					Severity: SeverityWarning,
					Path:     GrammarRel,
					Detail:   "published Kernel without design-language.md",
				})
			}
		}
	}

	for _, req := range changedREQs {
		ref := req.FoundationRef
		if ref == "" || ref == "pending-foundation" {
			report.Findings = append(report.Findings, Finding{
				Code:     "req_foundation_ref",
				Severity: SeverityWarning,
				Path:     req.Path,
				Detail:   "UI impact=changed REQ should cite docs/design/DESIGN.md@vX.Y.Z after publish; pending-foundation is only valid during F0–F6",
			})
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
