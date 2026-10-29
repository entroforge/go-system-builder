package designfoundation

import (
	"github.com/entroforge/go-system-builder/internal/pathscope"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// checkSemanticRoleUnbound checks binding notation, known color token paths,
// and literal project-relative source files. It does not prove product use.
// L5 §9.2 semantic_role_unbound, L4 §5.6 semantic layer boundary.
func checkSemanticRoleUnbound(root string, idx *ContractIndex) []Finding {
	var findings []Finding
	if idx.Mode != "contract-v1" {
		return findings
	}
	// Load tokens once; if missing, skip token-path validation (tokens_missing already reported elsewhere).
	var tf *tokenFile
	if _, err := os.Stat(filepath.Join(root, TokensJSONRel)); err == nil {
		if loaded, lerr := LoadTokens(root); lerr == nil {
			tf = loaded
		}
	}
	// Build set of semantic token paths for fast lookup.
	semanticPaths := map[string]bool{}
	primitivePaths := map[string]bool{}
	if tf != nil {
		for _, leaf := range tf.leaves {
			if strings.HasPrefix(leaf.Path, "color.primitive.") {
				primitivePaths[leaf.Path] = true
			} else if leaf.Type == "color" {
				semanticPaths[leaf.Path] = true
			} else {
				// non-color tokens (space, font, rounded) are considered primitive scale;
				// they are allowed as component tokens but not as color semantic roles.
				// For role↔token check we only enforce color semantic paths.
			}
		}
	}
	for id, role := range idx.Roles {
		st := strings.ToLower(strings.TrimSpace(role.Status))
		if st != "active" {
			continue
		}
		raw := strings.TrimSpace(role.Token)
		raw = strings.Trim(raw, "`\"' ")
		if raw == "" || raw == "—" || raw == "-" {
			findings = append(findings, Finding{
				Code:     "semantic_role_unbound",
				Severity: SeverityWarning,
				Path:     role.File,
				Detail:   id + " status active requires Token/component/pattern at line " + itoa(role.Line) + " (semantic_token_only: static binding checks do not prove component/pattern consumption)",
			})
			continue
		}
		// Check every literal source before the reference-notation shortcuts,
		// so a valid token does not hide another explicitly missing source.
		for _, part := range strings.Split(raw, ",") {
			if path, literal := literalBindingFile(part); literal {
				info, err := os.Stat(filepath.Join(root, path))
				if err != nil || !info.Mode().IsRegular() {
					findings = append(findings, Finding{
						Code:     "semantic_role_unbound",
						Severity: SeverityWarning,
						Path:     role.File,
						Detail:   id + " at line " + itoa(role.Line) + " declares literal source " + path + " which cannot be confirmed as an existing regular file. This checks source existence only; actual use belongs to Derivation/Proof.",
					})
				}
			}
		}
		// A slash recognizes reference notation only. Extensionless imports,
		// aliases and structured references are not resolved as source files.
		if strings.Contains(raw, "/") {
			// considered bound via component/pattern; do not validate token path
			continue
		}
		// Treat comma-separated list: any one valid suffices.
		parts := strings.Split(raw, ",")
		bound := false
		for _, p := range parts {
			p = strings.TrimSpace(strings.Trim(p, "`\"' "))
			if p == "" {
				continue
			}
			if strings.Contains(p, "/") {
				bound = true
				break
			}
			if strings.HasPrefix(p, "PAT-") || strings.HasPrefix(p, "ROLE-") {
				// Recognize the existing PAT/ROLE notation without claiming a
				// registry lookup or verification of actual consumption.
				bound = true
				break
			}
			// DTCG key like color.action.promise
			if tf != nil {
				if semanticPaths[p] {
					bound = true
					break
				}
				if primitivePaths[p] {
					findings = append(findings, Finding{
						Code:     "semantic_role_unbound",
						Severity: SeverityWarning,
						Path:     role.File,
						Detail:   id + " at line " + itoa(role.Line) + " binds primitive token " + p + " — must bind a semantic token (color.surface/content/action/status/brand) or component/pattern",
					})
					bound = true
					break
				}
				if strings.Contains(p, ".") {
					findings = append(findings, Finding{
						Code:     "semantic_role_unbound",
						Severity: SeverityWarning,
						Path:     role.File,
						Detail:   id + " at line " + itoa(role.Line) + " references unknown token " + p + " not in " + TokensJSONRel,
					})
					bound = true
					break
				}
			} else {
				if strings.Contains(p, ".") {
					bound = true
					break
				}
			}
		}
		if !bound {
			if !strings.Contains(raw, ".") && !strings.Contains(raw, "/") {
				findings = append(findings, Finding{
					Code:     "semantic_role_unbound",
					Severity: SeverityWarning,
					Path:     role.File,
					Detail:   id + " at line " + itoa(role.Line) + " has non-token binding " + raw + " — active ROLE requires a semantic token (color.*) or component/pattern path",
				})
			}
		}
	}
	return findings
}

// literalBindingFile intentionally does not resolve imports, links, globs,
// aliases or missing extensions into guessed files.
func literalBindingFile(raw string) (string, bool) {
	raw = strings.Trim(strings.TrimSpace(raw), "`\"' ")
	if !strings.Contains(raw, "/") || filepath.Ext(raw) == "" || strings.ContainsAny(raw, ":@#?*[](){}\\") {
		return "", false
	}
	path := filepath.Clean(raw)
	if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", false
	}
	return path, true
}

// checkPrimitiveConsumption observes scoped primitive var-reference text after F6.
// L5 §9.2 primitive_consumption, L4 §5.6 semantic_token_only.
func checkPrimitiveConsumption(root string, idx *ContractIndex) []Finding {
	var findings []Finding
	if idx.Mode != "contract-v1" {
		return findings
	}
	// Only enforce after Foundation is published (F6).
	if data, err := os.ReadFile(filepath.Join(root, KernelRel)); err == nil {
		if kernelStatus(string(data)) != "published" {
			return findings
		}
	} else {
		return findings
	}
	// Build set of primitive CSS var names.
	tf, err := LoadTokens(root)
	if err != nil {
		return findings
	}
	var primitiveVars []string
	for _, leaf := range tf.leaves {
		if strings.HasPrefix(leaf.Path, "color.primitive.") {
			primitiveVars = append(primitiveVars, regexp.QuoteMeta(cssName(leaf.Path)))
		}
	}
	if len(primitiveVars) == 0 {
		return findings
	}
	primitiveRef := regexp.MustCompile(`var\(\s*(` + strings.Join(primitiveVars, "|") + `)\s*[,)]`)
	designRoot := filepath.Join(root, "docs", "design")
	if _, err := os.Stat(designRoot); err != nil {
		return findings
	}
	_ = pathscope.Walk(root, designRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if !lintableDesignFile(rel) || filepath.ToSlash(rel) == TokensCSSRel {
			return nil
		}
		// Only HTML/CSS carry consumption; but lintable already filters.
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		body := string(data)
		if match := primitiveRef.FindStringSubmatch(body); match != nil {
			findings = append(findings, Finding{
				Code:     "primitive_consumption",
				Severity: SeverityWarning,
				Path:     rel,
				Detail:   "file-text var reference to primitive token " + match[1] + "; after F6 use the declared semantic/component layer. This is a design-directory text check, not proof of live CSS execution or all product consumption.",
			})
		}
		return nil
	})
	return findings
}

// checkGeneratedAssetUnverifiable checks source-hint text in scoped HTML and
// compares an existing tokens.css with the current emitter. Text hints are not
// authenticated inline digests or proof that a stylesheet was loaded.
// L5 §9.2 generated_asset_unverifiable.
func checkGeneratedAssetUnverifiable(root string, idx *ContractIndex) []Finding {
	var findings []Finding
	if idx.Mode != "contract-v1" {
		return findings
	}
	if data, err := os.ReadFile(filepath.Join(root, KernelRel)); err == nil {
		if kernelStatus(string(data)) != "published" {
			return findings
		}
	} else {
		return findings
	}
	findings = append(findings, compareExistingTokenCSS(root)...)
	// Collect HTML files under proof and prototypes that are lintable (skips templates/style-tiles/portable).
	var htmlFiles []string
	designRoot := filepath.Join(root, "docs", "design")
	if _, err := os.Stat(designRoot); err != nil {
		return findings
	}
	_ = pathscope.Walk(root, designRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if !strings.HasSuffix(strings.ToLower(rel), ".html") {
			return nil
		}
		if !lintableDesignFile(rel) {
			return nil
		}
		// Only proof and prototypes are in scope for generated asset check.
		relSlash := filepath.ToSlash(rel)
		if !(strings.Contains(relSlash, "/proof/") || strings.Contains(relSlash, "/prototypes/")) {
			return nil
		}
		htmlFiles = append(htmlFiles, rel)
		return nil
	})
	for _, rel := range htmlFiles {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		body := string(data)
		// Does file consume tokens at all?
		hasTokenVar := designTokenRef.MatchString(body)
		if !hasTokenVar {
			continue
		}
		// Observe hints only; do not promote an arbitrary comment to a digest.
		hasLink := strings.Contains(body, TokensCSSRel) || strings.Contains(body, "tokens.css")
		hasSourceHint := strings.Contains(body, "Generated from "+TokensJSONRel)
		if !hasLink && !hasSourceHint {
			findings = append(findings, Finding{
				Code:     "generated_asset_unverifiable",
				Severity: SeverityWarning,
				Path:     rel,
				Detail:   "HTML file-text has a token var reference but no tokens.css/source-hint text (Generated from " + TokensJSONRel + "). This is a text precondition, not verification of stylesheet loading or an inline digest.",
			})
		} else {
			findings = append(findings, Finding{Code: "generated_asset_observation", Severity: SeverityInfo, Path: rel, Detail: "HTML file-text contains tokens.css/source hint text; this does not authenticate an inline digest or verify loaded styles, generation provenance or actual browser consumption."})
		}
	}
	return findings
}

var designTokenRef = regexp.MustCompile(`var\(\s*--(?:color|font|space|rounded)-`)

// Compare only an already present artifact; check never creates or rewrites it.
func compareExistingTokenCSS(root string) []Finding {
	css, err := os.ReadFile(filepath.Join(root, TokensCSSRel))
	finding := Finding{Path: TokensCSSRel, Severity: SeverityInfo}
	if os.IsNotExist(err) {
		finding.Code = "generated_asset_unmeasured"
		finding.Detail = "No existing tokens.css artifact; generator comparison was not performed and no file was created. Source hints are not authenticated digests."
	} else if err != nil {
		finding.Code, finding.Severity = "generated_asset_unverifiable", SeverityWarning
		finding.Detail = "Cannot read existing tokens.css for comparison with the current emitter."
	} else if tf, err := LoadTokens(root); err != nil {
		finding.Code, finding.Severity = "generated_asset_unverifiable", SeverityWarning
		finding.Detail = "Cannot resolve the current Token source for existing tokens.css comparison."
	} else if string(css) != tf.CSS() {
		finding.Code, finding.Severity = "generated_asset_unverifiable", SeverityWarning
		finding.Detail = "Existing tokens.css differs from current Token emitter output; comparison was in memory and changed no files. Inspect the source/asset and use the original emit-css command if regeneration is intended; this does not judge product design."
	} else {
		finding.Code = "generated_asset_match"
		finding.Detail = "Existing tokens.css matches the current in-memory Token emitter output. No files changed; this does not prove loaded styles, product consumption or semantic design."
	}
	return []Finding{finding}
}
