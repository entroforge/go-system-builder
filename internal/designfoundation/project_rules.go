package designfoundation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const DesignChecksRel = "docs/design/design-checks.json"

var allowedRuleTypes = map[string]bool{
	"max_role_count":  true,
	"forbid_binding":  true,
	"token_scope":     true,
	"required_import": true,
	"forbid_literal":  true,
}

var dchkIDRe = regexp.MustCompile(`^DCHK-[A-Za-z0-9_\-]+$`)

// DesignChecks is the restricted DSL projection (L5 §5.9).
type DesignChecks struct {
	Version    int              `json:"version"`
	Foundation string           `json:"foundation"`
	Rules      []DesignCheckRule `json:"rules"`
}

type DesignCheckRule struct {
	ID        string   `json:"id"`
	Source    string   `json:"source"`
	Type      string   `json:"type"`
	Role      string   `json:"role"`
	Scope     string   `json:"scope"`
	Max       *int     `json:"max"`
	Targets   []string `json:"targets"`
	Subject   string   `json:"subject"`
	Forbidden []string `json:"forbidden"`
	Token     string   `json:"token"`
	Allow     []string `json:"allow"`
	// required_import: asset path or import marker
	Import string `json:"import"`
	Asset  string `json:"asset"`
	// forbid_literal
	Literals []string `json:"literals"`
	Values   []string `json:"values"`
}

func loadDesignChecks(root string) (*DesignChecks, []Finding, error) {
	path := filepath.Join(root, DesignChecksRel)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, []Finding{{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: "cannot read " + DesignChecksRel + ": " + err.Error()}}, nil
	}
	var dc DesignChecks
	if err := json.Unmarshal(data, &dc); err != nil {
		return nil, []Finding{{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: "invalid JSON in " + DesignChecksRel + ": " + err.Error()}}, nil
	}
	return &dc, nil, nil
}

func checkProjectRules(root string, idx *ContractIndex) []Finding {
	var out []Finding
	if idx.Mode != "contract-v1" {
		return out
	}
	dc, loadFindings, _ := loadDesignChecks(root)
	if loadFindings != nil {
		return loadFindings
	}
	if dc == nil {
		return out // optional file
	}
	// version must be 1
	if dc.Version != 1 {
		out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: "version must be 1, got " + itoa(dc.Version)})
		return out
	}
	if strings.TrimSpace(dc.Foundation) == "" {
		out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: "foundation must be docs/design/DESIGN.md@vX.Y.Z"})
	}
	seen := map[string]bool{}
	for i, r := range dc.Rules {
		prefix := DesignChecksRel + " rules[" + itoa(i) + "]"
		if !dchkIDRe.MatchString(r.ID) {
			out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: prefix + " id must be DCHK-*, got " + r.ID})
			continue
		}
		if seen[r.ID] {
			out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: prefix + " duplicate id " + r.ID})
			continue
		}
		seen[r.ID] = true
		if strings.TrimSpace(r.Source) == "" {
			out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " missing source LAW/ANTI/INV"})
			continue
		}
		if _, ok := idx.Constraints[r.Source]; !ok {
			// also allow INV/ANTI that may be in constraints map only
			out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " source " + r.Source + " not found in DESIGN.md constraints"})
			continue
		}
		if !allowedRuleTypes[r.Type] {
			out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " unknown type " + r.Type + " (allowed: max_role_count/forbid_binding/token_scope/required_import/forbid_literal)"})
			continue
		}
		switch r.Type {
		case "max_role_count":
			if strings.TrimSpace(r.Role) == "" {
				out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " max_role_count requires role"})
				continue
			}
			if r.Max == nil {
				out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " max_role_count requires max"})
				continue
			}
			if *r.Max < 0 {
				out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " max must be >=0"})
				continue
			}
			if len(r.Targets) == 0 {
				out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " max_role_count requires targets globs"})
				continue
			}
			files, expFinding := expandTargets(root, r.Targets)
			if expFinding != nil {
				expFinding.Detail = r.ID + " " + expFinding.Detail
				out = append(out, *expFinding)
				continue
			}
			if len(files) == 0 {
				out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " targets matched no files: " + strings.Join(r.Targets, ",")})
				continue
			}
			// need marker data-design-role
			hasMarker := false
			total := 0
			perFile := map[string]int{}
			roleRe := regexp.MustCompile(`data-design-role\s*=\s*["']([^"']*)["']`)
			for _, rel := range files {
				data, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil {
					continue
				}
				matches := roleRe.FindAllStringSubmatch(string(data), -1)
				c := 0
				for _, m := range matches {
					hasMarker = true
					// value may be space-separated list
					for _, tok := range strings.Fields(m[1]) {
						// support dot or slash normalized; compare exact or suffix
						if tok == r.Role || strings.HasSuffix(tok, "."+r.Role) || strings.HasSuffix(tok, "/"+r.Role) || tok == strings.ReplaceAll(r.Role, ".", "-") {
							c++
						} else if strings.Contains(m[1], r.Role) {
							// fallback substring for color.action.promise matching "action.promise"
							if strings.Contains(tok, r.Role) {
								c++
							}
						}
					}
					// also count comma-separated
					if c == 0 && strings.Contains(m[1], ",") {
						for _, part := range strings.Split(m[1], ",") {
							if strings.TrimSpace(part) == r.Role || strings.Contains(strings.TrimSpace(part), r.Role) {
								c++
							}
						}
					}
				}
				// If no structured split matched but marker exists, try simple contains
				if c == 0 && hasMarker {
					// fallback: count occurrences of role string inside data-design-role values
					for _, m := range matches {
						if strings.Contains(m[1], r.Role) {
							c++
						}
					}
				}
				perFile[rel] = c
				total += c
			}
			if !hasMarker {
				out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " max_role_count requires HTML data-design-role markers in targets but none found — add data-design-role or UI Lab role mapping"})
				continue
			}
			scope := strings.ToLower(strings.TrimSpace(r.Scope))
			if scope == "" {
				scope = "file"
			}
			violated := false
			if scope == "viewport" || scope == "file" {
				for rel, c := range perFile {
					if c > *r.Max {
						out = append(out, Finding{Code: "project_rule_violation", Severity: SeverityWarning, Path: rel, Detail: r.ID + " (" + r.Source + ") max_role_count role=" + r.Role + " scope=" + scope + " max=" + itoa(*r.Max) + " got " + itoa(c) + " in " + rel})
						violated = true
					}
				}
			} else {
				if total > *r.Max {
					out = append(out, Finding{Code: "project_rule_violation", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " (" + r.Source + ") max_role_count role=" + r.Role + " total " + itoa(total) + " exceeds max " + itoa(*r.Max)})
					violated = true
				}
			}
			_ = violated

		case "forbid_binding":
			subj := strings.TrimSpace(r.Subject)
			if subj == "" {
				subj = strings.TrimSpace(r.Token)
			}
			if subj == "" {
				out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " forbid_binding requires subject"})
				continue
			}
			if len(r.Forbidden) == 0 {
				out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " forbid_binding requires forbidden list"})
				continue
			}
			targets := r.Targets
			if len(targets) == 0 {
				targets = []string{"docs/design/**/*.html", "docs/design/**/*.css"}
			}
			files, expFinding := expandTargets(root, targets)
			if expFinding != nil {
				expFinding.Detail = r.ID + " " + expFinding.Detail
				out = append(out, *expFinding)
				continue
			}
			// subject is semantic token path like color.status.verified or color.primitive.green.*
			// We check if forbidden tokens are bound to subject via alias graph or literal presence.
			// Simplified: if any forbidden glob matches a token leaf that is alias of subject, violation is structural.
			// For runtime check, scan files for forbidden literal vars when subject var also present? Instead scan for forbidden var presence as proxy for binding violation.
			for _, rel := range files {
				data, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil {
					continue
				}
				body := string(data)
				// if file contains subject var, check forbidden vars
				subjVar := tokenPathToVar(subj)
				hasSubj := subj != "" && (strings.Contains(body, subj) || strings.Contains(body, subjVar))
				// Even if subject not directly present, forbidden binding itself is violation per rule (e.g., ban green globally)
				for _, forb := range r.Forbidden {
					forbVar := tokenPathToVar(forb)
					// glob support: "color.primitive.green.*" -> var prefix
					isGlob := strings.HasSuffix(forb, ".*")
					matched := false
					if isGlob {
						prefix := tokenPathToVar(strings.TrimSuffix(forb, ".*"))
						if strings.Contains(body, prefix) {
							matched = true
						}
					} else {
						if strings.Contains(body, forb) || (forbVar != "" && strings.Contains(body, forbVar)) {
							matched = true
						}
					}
					if matched {
						// If subject scope is defined, only report when subject also present or rule is global ban
						if hasSubj || subj == "" || strings.Contains(forb, "green") || strings.Contains(forb, "primitive") {
							out = append(out, Finding{Code: "project_rule_violation", Severity: SeverityWarning, Path: rel, Detail: r.ID + " (" + r.Source + ") forbid_binding subject=" + subj + " forbids " + forb + " found in " + rel})
						}
					}
				}
			}

		case "token_scope":
			tok := strings.TrimSpace(r.Token)
			if tok == "" {
				out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " token_scope requires token"})
				continue
			}
			if len(r.Allow) == 0 {
				out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " token_scope requires allow selectors"})
				continue
			}
			targets := r.Targets
			if len(targets) == 0 {
				targets = []string{"docs/design/**/*.html", "docs/design/**/*.css"}
			}
			files, expFinding := expandTargets(root, targets)
			if expFinding != nil {
				expFinding.Detail = r.ID + " " + expFinding.Detail
				out = append(out, *expFinding)
				continue
			}
			tokVar := tokenPathToVar(tok)
			for _, rel := range files {
				data, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil {
					continue
				}
				body := string(data)
				if tokVar != "" && !strings.Contains(body, tokVar) && !strings.Contains(body, tok) {
					continue
				}
				allowedFound := false
				for _, sel := range r.Allow {
					if selectorMatches(body, strings.TrimSpace(sel)) {
						allowedFound = true
						break
					}
				}
				if !allowedFound {
					out = append(out, Finding{Code: "project_rule_violation", Severity: SeverityWarning, Path: rel, Detail: r.ID + " (" + r.Source + ") token_scope token=" + tok + " used outside allow " + strings.Join(r.Allow, ",") + " in " + rel})
				}
			}

		case "required_import":
			asset := strings.TrimSpace(r.Import)
			if asset == "" {
				asset = strings.TrimSpace(r.Asset)
			}
			if asset == "" {
				asset = strings.TrimSpace(r.Subject)
			}
			if asset == "" {
				out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " required_import requires import/asset path"})
				continue
			}
			targets := r.Targets
			if len(targets) == 0 {
				targets = []string{"docs/design/**/*.html"}
			}
			files, expFinding := expandTargets(root, targets)
			if expFinding != nil {
				expFinding.Detail = r.ID + " " + expFinding.Detail
				out = append(out, *expFinding)
				continue
			}
			for _, rel := range files {
				data, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil {
					continue
				}
				body := string(data)
				if !strings.Contains(body, asset) {
					out = append(out, Finding{Code: "project_rule_violation", Severity: SeverityWarning, Path: rel, Detail: r.ID + " (" + r.Source + ") required_import missing " + asset + " in " + rel})
				}
			}

		case "forbid_literal":
			lits := r.Literals
			if len(lits) == 0 {
				lits = r.Values
			}
			if len(lits) == 0 {
				lits = r.Forbidden
			}
			if len(lits) == 0 {
				out = append(out, Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: r.ID + " forbid_literal requires literals/values list"})
				continue
			}
			targets := r.Targets
			if len(targets) == 0 {
				targets = []string{"docs/design/**/*.html", "docs/design/**/*.css"}
			}
			files, expFinding := expandTargets(root, targets)
			if expFinding != nil {
				expFinding.Detail = r.ID + " " + expFinding.Detail
				out = append(out, *expFinding)
				continue
			}
			for _, rel := range files {
				data, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil {
					continue
				}
				body := string(data)
				for _, lit := range lits {
					if lit != "" && strings.Contains(body, lit) {
						out = append(out, Finding{Code: "project_rule_violation", Severity: SeverityWarning, Path: rel, Detail: r.ID + " (" + r.Source + ") forbid_literal " + lit + " found in " + rel})
					}
				}
			}
		}
	}
	return out
}

func tokenPathToVar(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, "`\"' ")
	if p == "" || strings.Contains(p, "*") && !strings.HasSuffix(p, ".*") {
		return ""
	}
	if strings.HasSuffix(p, ".*") {
		p = strings.TrimSuffix(p, ".*")
	}
	// already a var?
	if strings.HasPrefix(p, "--") {
		return p
	}
	if strings.HasPrefix(p, "var(") {
		return p
	}
	// DTCG path like color.action.promise -> --color-action-promise
	if strings.Contains(p, ".") {
		return "--" + strings.ReplaceAll(p, ".", "-")
	}
	return ""
}

func expandTargets(root string, patterns []string) ([]string, *Finding) {
	seen := map[string]bool{}
	var out []string
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		// reject regex-like patterns: forbid natural language aesthetic scoring, regex execution is banned
		if strings.Contains(pat, "[") && strings.Contains(pat, "]") && strings.Contains(pat, "(") {
			// heuristic: looks like regex, reject
			return nil, &Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: "targets must be glob paths, not regex: " + pat}
		}
		// Normalize: ensure forward slashes
		patSlash := filepath.ToSlash(pat)
		matches, err := expandGlob(root, patSlash)
		if err != nil {
			return nil, &Finding{Code: "project_rule_unverifiable", Severity: SeverityWarning, Path: DesignChecksRel, Detail: "unparseable target glob " + pat + ": " + err.Error()}
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out, nil
}

func selectorMatches(body, sel string) bool {
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return false
	}
	if strings.HasPrefix(sel, "[") && strings.HasSuffix(sel, "]") {
		inner := strings.Trim(sel, "[]")
		for i, ch := range inner {
			if ch == '=' || ch == '~' || ch == '|' || ch == '^' || ch == '$' || ch == '*' {
				inner = inner[:i]
				break
			}
		}
		inner = strings.TrimSpace(inner)
		if inner == "" {
			return false
		}
		return strings.Contains(body, inner)
	}
	if strings.HasPrefix(sel, ".") {
		cls := strings.TrimPrefix(sel, ".")
		cls = strings.TrimSpace(cls)
		if cls == "" {
			return false
		}
		return strings.Contains(body, cls)
	}
	if strings.HasPrefix(sel, "#") {
		id := strings.TrimPrefix(sel, "#")
		id = strings.TrimSpace(id)
		if id == "" {
			return false
		}
		return strings.Contains(body, id)
	}
	if strings.Contains(sel, "[") {
		start := strings.Index(sel, "[")
		end := strings.Index(sel, "]")
		if start >= 0 && end > start {
			attr := sel[start+1 : end]
			for i, ch := range attr {
				if ch == '=' {
					attr = attr[:i]
					break
				}
			}
			attr = strings.TrimSpace(attr)
			if attr != "" && strings.Contains(body, attr) {
				tag := strings.TrimSpace(sel[:start])
				if tag != "" && tag != "*" {
					if strings.Contains(strings.ToLower(body), "<"+strings.ToLower(tag)) {
						return true
					}
					return true
				}
				return true
			}
			return false
		}
	}
	return strings.Contains(body, sel)
}

func expandGlob(root, pattern string) ([]string, error) {
	// Support ** via walk
	if strings.Contains(pattern, "**") {
		// Split into prefix before **
		parts := strings.Split(pattern, "**")
		prefix := parts[0]
		suffix := ""
		if len(parts) > 1 {
			suffix = parts[1]
		}
		prefix = strings.TrimSuffix(prefix, "/")
		suffix = strings.TrimPrefix(suffix, "/")
		baseDir := filepath.Join(root, filepath.FromSlash(prefix))
		if prefix == "" {
			baseDir = root
		}
		var out []string
		err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			relSlash := filepath.ToSlash(rel)
			if suffix == "" {
				// match all under prefix
				out = append(out, relSlash)
				return nil
			}
			// suffix may contain * etc.
			// Check if rel matches pattern via path.Match after expanding ** as *
			// Simplified: check if rel has suffix pattern match
			matched, _ := filepath.Match(filepath.ToSlash(suffix), filepath.Base(relSlash))
			if matched {
				// also ensure prefix containment
				if strings.HasPrefix(relSlash, filepath.ToSlash(prefix)) {
					out = append(out, relSlash)
				}
			} else {
				// try full pattern with ** replaced by *
				starPat := strings.ReplaceAll(pattern, "**", "*")
				if ok, _ := filepath.Match(starPat, relSlash); ok {
					out = append(out, relSlash)
				} else if strings.Contains(relSlash, strings.Trim(suffix, "*")) {
					// fallback contains
					if suffix != "" && strings.Contains(relSlash, strings.Trim(suffix, "*/")) {
						out = append(out, relSlash)
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	// Single * glob: use filepath.Glob
	absPat := filepath.Join(root, filepath.FromSlash(pattern))
	matches, err := filepath.Glob(absPat)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range matches {
		rel, _ := filepath.Rel(root, m)
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out, nil
}
