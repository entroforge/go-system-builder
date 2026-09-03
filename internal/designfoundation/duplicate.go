package designfoundation

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Filename tokens that mean the same control. popup.html and overlay.html
// both count as dialog so a local champion cannot evade by renaming.
var roleAlias = map[string]string{
	"button":   "button",
	"dialog":   "dialog",
	"modal":    "dialog",
	"popup":    "dialog",
	"overlay":  "dialog",
	"confirm":  "dialog",
	"alert":    "dialog",
	"drawer":   "drawer",
	"sheet":    "drawer",
	"dropdown": "dropdown",
	"popover":  "dropdown",
	"chip":     "chip",
	"toast":    "toast",
	"banner":   "banner",
}

var fileStem = regexp.MustCompile(`(?i)(button|dialog|modal|drawer|dropdown|chip|toast|banner|popup|overlay|confirm|alert|sheet|popover)`)

func LintDuplicateComponents(root string) ([]Finding, error) {
	names := map[string][]string{}
	addRole := func(rel, token string) {
		role := canonicalRoleToken(token)
		if role == "" {
			return
		}
		names[role] = append(names[role], rel)
	}

	proposalDir := filepath.Join(root, "docs", "design", "components")
	if entries, err := os.ReadDir(proposalDir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".md") || strings.Contains(name, "template") || name == "README.md" {
				continue
			}
			rel := filepath.Join("docs", "design", "components", name)
			addRole(rel, strings.TrimSuffix(name, ".md"))
			data, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				return nil, err
			}
			first := strings.SplitN(string(data), "\n", 2)[0]
			first = strings.TrimPrefix(first, "# ")
			if i := strings.Index(first, " — "); i >= 0 {
				first = first[i+len(" — "):]
			}
			addRole(rel, first)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	protoRoot := filepath.Join(root, "docs", "design", "prototypes")
	_ = filepath.Walk(protoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if !strings.HasSuffix(name, ".html") {
			return nil
		}
		if !fileStem.MatchString(name) {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		stem := strings.TrimSuffix(name, ".html")
		addRole(rel, stem)
		for _, m := range fileStem.FindAllString(strings.ToLower(stem), -1) {
			addRole(rel, m)
		}
		return nil
	})

	var findings []Finding
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		paths := uniq(names[key])
		if len(paths) < 2 {
			continue
		}
		findings = append(findings, Finding{
			Code:     "component_repeat",
			Severity: SeverityWarning,
			Detail:   "semantic " + key + " appears in " + strings.Join(paths, ", ") + "; reuse one component or file a CP-* proposal instead of a local champion",
		})
	}
	return findings, nil
}

func canonicalRoleToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	if alias, ok := roleAlias[s]; ok {
		return alias
	}
	for _, m := range fileStem.FindAllString(s, -1) {
		if alias, ok := roleAlias[strings.ToLower(m)]; ok {
			return alias
		}
	}
	return ""
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
