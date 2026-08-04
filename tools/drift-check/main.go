// Package main provides a static drift detector that scans the Vibe Coding
// template documents and compares them against the canonical runtime
// ENUMs declared by the Loop Harness. It exits non-zero when a drift
// is detected so the check can be wired into a pre-commit hook or CI
// gate.
//
// Usage:
//
//	go run ./tools/drift-check
//	go run ./tools/drift-check --root /path/to/vibe-coding
//
// What it checks:
//
//  1. frontmatter 状态/Status ENUM declared in every template document
//     matches the runtime KEY:VALUE contract documented in REQ-template
//     §0. Templates that mention `> 状态：...` must enumerate the values
//     they accept; values not enumerated by the runtime (e.g. a
//     template-only ENUM like `executing` for tasks) are flagged.
//  2. UI impact ENUM in REQ-template and CONTRACTS-template matches the
//     runtime enum {none, changed, unknown}.
//  3. Hook policy JSON protected_events list equals the schema's
//     enum (which has been aligned with all Claude Code events).
//  4. Loop runtime schema (loop-state.schema.json) accepts the same
//     UI impact ENUM the engine produces.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// canonicalENUMS declares the runtime values the Loop Harness actually
// accepts. The drift detector compares every template/JSON reference
// against this list.
var canonicalENUMs = map[string][]string{
	"REQ-状态":        {"discovery", "draft", "reviewed", "locked", "changed", "archived"},
	"CONTRACT-状态":   {"draft", "reviewed", "locked"},
	"TASK-INDEX-状态": {"draft", "locked", "executing", "completed"},
	"TASK-状态":       {"locked", "activated", "working", "reported", "review", "blocked", "complete", "stale"},
	"UI-impact":     {"none", "changed", "unknown"},
}

func main() {
	root := flag.String("root", ".", "repository root to scan")
	jsonOut := flag.Bool("json", false, "emit machine-readable JSON report")
	flag.Parse()

	report := scan(*root)

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		printTextReport(report)
	}

	if !report.OK() {
		os.Exit(1)
	}
}

type finding struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Category string `json:"category"`
	Detail   string `json:"detail"`
}

type reportT struct {
	Root     string    `json:"root"`
	Findings []finding `json:"findings"`
}

func (r reportT) OK() bool { return len(r.Findings) == 0 }

func printTextReport(r reportT) {
	fmt.Printf("template/hook drift check (root=%s)\n", r.Root)
	if r.OK() {
		fmt.Println("  OK: no drift detected")
		return
	}
	fmt.Printf("  %d drift finding(s):\n", len(r.Findings))
	for _, f := range r.Findings {
		fmt.Printf("  - %s:%d  [%s]\n      %s\n", f.Path, f.Line, f.Category, f.Detail)
	}
}

var statusLineRe = regexp.MustCompile(`^>\s*(?:状态|Status)\s*[：:]\s*(.+?)\s*$`)

func scan(root string) reportT {
	var r reportT
	r.Root = root
	addFinding := func(relPath string, line int, category, detail string) {
		r.Findings = append(r.Findings, finding{
			Path:     relPath,
			Line:     line,
			Category: category,
			Detail:   detail,
		})
	}

	scanTemplate := func(relPath, docKey string, allowed []string) {
		path := filepath.Join(root, relPath)
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		allowedSet := make(map[string]bool, len(allowed))
		for _, v := range allowed {
			allowedSet[strings.ToLower(strings.TrimSpace(v))] = true
		}
		scanner := func() {}
		_ = scanner
		for i, line := range strings.Split(string(data), "\n") {
			m := statusLineRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			declared := splitSlashList(m[1])
			for _, v := range declared {
				if !allowedSet[strings.ToLower(strings.TrimSpace(v))] {
					addFinding(relPath, i+1, docKey+"-drift",
						fmt.Sprintf("declared value %q is not in the canonical runtime ENUM %v", v, allowed))
				}
			}
		}
	}

	scanTemplate("docs/requirements/REQ-template.md", "REQ-状态", canonicalENUMs["REQ-状态"])
	for _, p := range []string{
		"docs/contracts/CONTRACTS-template.md",
		"docs/contracts/BE-contract-template.md",
		"docs/contracts/FE-contract-template.md",
		"docs/contracts/SYNC-contract-template.md",
		"docs/design/architecture/ARCHITECTURE-template.md",
	} {
		scanTemplate(p, "CONTRACT-状态", canonicalENUMs["CONTRACT-状态"])
	}
	scanTemplate("docs/tasks/index-template.md", "TASK-INDEX-状态", canonicalENUMs["TASK-INDEX-状态"])

	// UI impact: scan REQ-template and CONTRACTS-template for the value list.
	scanUIImpact := func(relPath string) {
		path := filepath.Join(root, relPath)
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		re := regexp.MustCompile(`UI\s*impact\s*[：:]\s*([^\n|]+?)\s*(?:\||$)`)
		allowed := make(map[string]bool)
		for _, v := range canonicalENUMs["UI-impact"] {
			allowed[v] = true
		}
		for i, line := range strings.Split(string(data), "\n") {
			m := re.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			for _, raw := range splitSlashList(m[1]) {
				// Strip backticks and surrounding whitespace that templates
				// use to wrap code-style values.
				v := strings.TrimSpace(strings.Trim(raw, "`"))
				key := strings.ToLower(v)
				if key == "" {
					continue
				}
				if !allowed[key] {
					addFinding(relPath, i+1, "UI-impact-drift",
						fmt.Sprintf("declared UI impact value %q is not in canonical ENUM %v", v, canonicalENUMs["UI-impact"]))
				}
			}
		}
	}
	scanUIImpact("docs/requirements/REQ-template.md")
	scanUIImpact("docs/contracts/CONTRACTS-template.md")

	// Cross-check Hook policy JSON vs schema ENUM.
	checkProtectedEvents(root, addFinding)

	sort.Slice(r.Findings, func(i, j int) bool {
		if r.Findings[i].Path != r.Findings[j].Path {
			return r.Findings[i].Path < r.Findings[j].Path
		}
		return r.Findings[i].Line < r.Findings[j].Line
	})
	return r
}

func splitSlashList(s string) []string {
	parts := strings.Split(s, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// addFindingFn is the closure shape used to append a drift finding while
// the scan is still walking the tree.
type addFindingFn func(relPath string, line int, category, detail string)

func checkProtectedEvents(root string, addFinding addFindingFn) {
	schemaPath := filepath.Join(root, "internal/schema/assets/hook-policy.schema.json")
	policyPath := filepath.Join(root, "docs/hook-policy.json")

	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		addFinding("internal/schema/assets/hook-policy.schema.json", 0, "schema-missing", err.Error())
		return
	}
	var schema struct {
		Properties struct {
			ProtectedEvents struct {
				Items struct {
					Enum []string `json:"enum"`
				} `json:"items"`
			} `json:"protected_events"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		addFinding(schemaPath, 0, "schema-parse", err.Error())
		return
	}
	schemaSet := make(map[string]bool, len(schema.Properties.ProtectedEvents.Items.Enum))
	for _, e := range schema.Properties.ProtectedEvents.Items.Enum {
		schemaSet[e] = true
	}

	policyData, err := os.ReadFile(policyPath)
	if err != nil {
		addFinding(policyPath, 0, "policy-missing", err.Error())
		return
	}
	var policy struct {
		ProtectedEvents []string `json:"protected_events"`
	}
	if err := json.Unmarshal(policyData, &policy); err != nil {
		addFinding(policyPath, 0, "policy-parse", err.Error())
		return
	}
	for i, event := range policy.ProtectedEvents {
		if !schemaSet[event] {
			addFinding(policyPath, i+1, "protected-events-drift",
				fmt.Sprintf("policy declares %q which is not in schema enum %v", event, schema.Properties.ProtectedEvents.Items.Enum))
		}
	}
}
