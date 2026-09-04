package designfoundation

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var markerRe = regexp.MustCompile(`<!--\s*foundation-contract:v1\s+([a-z0-9\-]+)\s*-->`)
var markerAnyRe = regexp.MustCompile(`<!--\s*foundation-contract:[^\s>]+\s*[^>]*-->`)

// ParsedTable is the first table after a foundation-contract marker.
// Headers are raw header cell texts; Rows map header -> cell value (trimmed).
type ParsedTable struct {
	Marker string
	File   string
	Line   int // 1-indexed line of the marker
	Headers []string
	Rows    []map[string]string
	RowLines []int // line number per data row
}

func parseFileTables(root, rel string) ([]ParsedTable, []Finding, error) {
	path := filepath.Join(root, rel)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	content := string(data)
	return parseContentTables(content, rel)
}

func parseContentTables(content, rel string) ([]ParsedTable, []Finding, error) {
	lines := strings.Split(content, "\n")
	var tables []ParsedTable
	var findings []Finding
	for i, line := range lines {
		m := markerRe.FindStringSubmatch(line)
		if m == nil {
			// detect unsupported marker version like v2
			if markerAnyRe.MatchString(line) && !strings.Contains(line, "foundation-contract:v1") {
				findings = append(findings, Finding{
					Code:     "contract_version_unknown",
					Severity: SeverityWarning,
					Path:     rel,
					Detail:   "unsupported foundation-contract marker version at line " + itoa(i+1) + "; v1 parser cannot validate this file until migrated",
				})
			}
			continue
		}
		marker := m[1]
		table, headerLine, endLine, ok := extractNextTable(lines, i)
		if !ok {
			findings = append(findings, Finding{
				Code:     "contract_table_missing",
				Severity: SeverityWarning,
				Path:     rel,
				Detail:   "marker foundation-contract:v1 " + marker + " at line " + itoa(i+1) + " has no following markdown table",
			})
			continue
		}
		pt := ParsedTable{
			Marker: marker,
			File:   rel,
			Line:   i + 1,
			Headers: table.Headers,
			Rows:    table.Rows,
			RowLines: table.RowLines,
		}
		_ = headerLine
		_ = endLine
		// header completeness check
		if req := requiredHeaders(marker); len(req) > 0 {
			for _, alt := range req {
				if !headerHasAny(table.Headers, alt) {
					findings = append(findings, Finding{
						Code:     "contract_header_missing",
						Severity: SeverityWarning,
						Path:     rel,
						Detail:   "marker " + marker + " at line " + itoa(i+1) + " missing required column " + strings.Join(alt, "/") + "; headers=" + strings.Join(table.Headers, "|"),
					})
				}
			}
		}
		tables = append(tables, pt)
	}
	return tables, findings, nil
}

type tableData struct {
	Headers  []string
	Rows     []map[string]string
	RowLines []int
}

func extractNextTable(lines []string, markerIdx int) (tableData, int, int, bool) {
	// scan forward for header row + separator
	for idx := markerIdx + 1; idx < len(lines); idx++ {
		line := lines[idx]
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		// heading or other text without pipe -> skip but allow up to a few lines
		// we look for a line containing | and next line being separator
		if !strings.Contains(line, "|") {
			continue
		}
		if idx+1 >= len(lines) {
			return tableData{}, 0, 0, false
		}
		next := lines[idx+1]
		if !isSeparatorRow(next) {
			continue
		}
		headers := splitTableLine(line)
		if len(headers) == 0 {
			continue
		}
		var rows []map[string]string
		var rowLines []int
		for r := idx + 2; r < len(lines); r++ {
			rl := lines[r]
			if strings.TrimSpace(rl) == "" {
				break
			}
			if !strings.Contains(rl, "|") {
				break
			}
			// separator-like row should not be treated as data; but data rows contain pipes
			// If line is purely separator (---|---) skip
			if isSeparatorRow(rl) {
				continue
			}
			cells := splitTableLine(rl)
			row := map[string]string{}
			for i, h := range headers {
				val := ""
				if i < len(cells) {
					val = strings.TrimSpace(cells[i])
				}
				row[h] = val
				// also store normalized lower key for convenience
				row[strings.ToLower(h)] = val
			}
			// keep raw row for debugging
			rows = append(rows, row)
			rowLines = append(rowLines, r+1)
		}
		return tableData{Headers: headers, Rows: rows, RowLines: rowLines}, idx + 1, idx + 2 + len(rows), true
	}
	return tableData{}, 0, 0, false
}

func isSeparatorRow(line string) bool {
	trim := strings.TrimSpace(line)
	if !strings.Contains(trim, "|") {
		return false
	}
	// must contain --- or :-- pattern
	if strings.Contains(trim, "---") || strings.Contains(trim, ":--") || strings.Contains(trim, "--:") {
		// ensure only | - : and spaces
		for _, ch := range trim {
			if ch == '|' || ch == '-' || ch == ':' || ch == ' ' || ch == '\t' {
				continue
			}
			return false
		}
		return true
	}
	return false
}

func splitTableLine(line string) []string {
	// split by |, trim, drop leading/trailing empty due to outer pipes
	parts := strings.Split(line, "|")
	// remove first if line starts with |
	if strings.HasPrefix(strings.TrimSpace(line), "|") && len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	// remove last if line ends with |
	if strings.HasSuffix(strings.TrimSpace(line), "|") && len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func headerHasAny(headers []string, aliases []string) bool {
	for _, h := range headers {
		hl := strings.ToLower(h)
		for _, a := range aliases {
			al := strings.ToLower(a)
			if strings.Contains(hl, al) {
				return true
			}
		}
	}
	return false
}

func requiredHeaders(marker string) [][]string {
	switch marker {
	case "constraints":
		return [][]string{{"id"}, {"status", "状态"}, {"scope"}, {"source"}, {"binding"}, {"checkability"}, {"proof", "证明"}}
	case "surfaces":
		return [][]string{{"id"}, {"surface"}, {"profile", "版本", "version"}, {"proof", "证明"}}
	case "proofs":
		return [][]string{{"id"}, {"路径", "path"}, {"证明", "proof"}}
	case "debts":
		return [][]string{{"id"}, {"复查", "review"}}
	case "dimensions":
		return [][]string{{"维度", "dimension"}, {"状态", "status"}, {"proof", "证明"}}
	case "grammar-rules":
		return [][]string{{"id"}, {"dimensions", "维度"}, {"source"}, {"binding"}, {"proof", "证明"}}
	case "bindings":
		return [][]string{{"id"}, {"source"}, {"token", "component", "pattern"}, {"状态", "status"}}
	case "surface-inherits":
		return [][]string{{"id"}, {"保留"}}
	case "surface-variants":
		return [][]string{{"variant"}, {"选择"}}
	case "derivation-active":
		return [][]string{{"id"}, {"gr", "gr-"}}
	case "derivation-must-not":
		return [][]string{{"source"}, {"观察"}}
	case "derivation-bindings":
		return [][]string{{"role", "pattern", "组件"}}
	case "derivation-proof":
		return [][]string{{"proof", "proof id"}, {"路径", "path"}}
	case "evidence-product", "evidence-relationship", "evidence-business", "evidence-ritual", "evidence-category", "evidence-constraint":
		return [][]string{{"id"}}
	default:
		return nil
	}
}

// helpers

func containsMarker(content, marker string) bool {
	return strings.Contains(content, "foundation-contract:v1 "+marker)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 4)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

// scan file for any foundation-contract:v1 marker
func fileHasAnyContractMarker(root, rel string) (bool, error) {
	path := filepath.Join(root, rel)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return strings.Contains(string(data), "foundation-contract:v1"), nil
}

func countContractMarkersInFile(root, rel string) (int, error) {
	path := filepath.Join(root, rel)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return strings.Count(string(data), "foundation-contract:v1"), nil
}

// collect all contract tables across known locations
func collectAllTables(root string) ([]ParsedTable, []Finding, error) {
	candidates := []string{
		KernelRel,
		GrammarRel,
		"docs/design/research/evidence-field.md",
	}
	// surface profiles (skip templates and README — they are not live contracts)
	surfaceDir := filepath.Join(root, "docs", "design", "surface-profiles")
	if entries, err := os.ReadDir(surfaceDir); err == nil {
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
			candidates = append(candidates, filepath.Join("docs", "design", "surface-profiles", e.Name()))
		}
	}
	// derivations (skip template)
	derivDir := filepath.Join(root, "docs", "design", "derivation")
	if entries, err := os.ReadDir(derivDir); err == nil {
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
			candidates = append(candidates, filepath.Join("docs", "design", "derivation", e.Name()))
		}
	}
	var all []ParsedTable
	var findings []Finding
	for _, rel := range candidates {
		tables, f, err := parseFileTables(root, rel)
		if err != nil {
			return nil, nil, err
		}
		all = append(all, tables...)
		findings = append(findings, f...)
	}
	return all, findings, nil
}
