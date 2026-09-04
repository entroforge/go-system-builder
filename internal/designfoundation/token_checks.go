package designfoundation

import (
	"os"
	"path/filepath"
	"strings"
)

// checkSemanticRoleUnbound verifies active ROLE/PAT have a semantic token/component binding.
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
				Detail:   id + " status active requires Token/component/pattern at line " + itoa(role.Line) + " (semantic_token_only: active ROLE must bind a semantic token or a verifiable component/pattern)",
			})
			continue
		}
		// Component/pattern path heuristic: contains / or starts with packages/
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
				// PAT/ROLE pattern reference counts as bound (validated elsewhere if unknown)
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

// checkPrimitiveConsumption detects direct consumption of primitive tokens after F6.
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
			primitiveVars = append(primitiveVars, cssName(leaf.Path))
		}
	}
	if len(primitiveVars) == 0 {
		return findings
	}
	designRoot := filepath.Join(root, "docs", "design")
	if _, err := os.Stat(designRoot); err != nil {
		return findings
	}
	_ = filepath.Walk(designRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if !lintableDesignFile(rel) {
			return nil
		}
		// Only HTML/CSS carry consumption; but lintable already filters.
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		body := string(data)
		for _, pv := range primitiveVars {
			if strings.Contains(body, pv) {
				// style-tiles are already excluded via lintableDesignFile
				findings = append(findings, Finding{
					Code:     "primitive_consumption",
					Severity: SeverityWarning,
					Path:     rel,
					Detail:   "direct consumption of primitive token " + pv + " — after F6 only semantic/component tokens via tokens.css are legal (semantic_token_only)",
				})
				break // one per file is enough
			}
		}
		return nil
	})
	return findings
}

// checkGeneratedAssetUnverifiable verifies that Proof/Prototype HTML referencing
// design tokens does so via generated tokens.css or a verifiable inline digest.
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
	// Collect HTML files under proof and prototypes that are lintable (skips templates/style-tiles/portable).
	var htmlFiles []string
	designRoot := filepath.Join(root, "docs", "design")
	if _, err := os.Stat(designRoot); err != nil {
		return findings
	}
	_ = filepath.Walk(designRoot, func(path string, info os.FileInfo, err error) error {
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
		hasTokenVar := strings.Contains(body, "var(--color-") || strings.Contains(body, "var(--font-") || strings.Contains(body, "var(--space-") || strings.Contains(body, "var(--rounded-")
		if !hasTokenVar {
			continue
		}
		// Verifiable if references generated tokens.css or carries generator digest marker.
		hasLink := strings.Contains(body, TokensCSSRel) || strings.Contains(body, "tokens.css")
		hasDigest := strings.Contains(body, "Generated from "+TokensJSONRel) || strings.Contains(body, "Generated from packages/design-tokens/tokens.json")
		// Inline digest may also be via "source digest" comment from emit-css
		if !hasLink && !hasDigest {
			findings = append(findings, Finding{
				Code:     "generated_asset_unverifiable",
				Severity: SeverityWarning,
				Path:     rel,
				Detail:   "HTML consumes tokens (var(--*)) but does not reference generated " + TokensCSSRel + " nor carry a verifiable inline digest (Generated from " + TokensJSONRel + ")",
			})
		}
	}
	return findings
}
