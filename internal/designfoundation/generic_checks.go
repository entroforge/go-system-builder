package designfoundation

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var versionAtRe = regexp.MustCompile(`@v([0-9]+\.[0-9]+\.[0-9]+)`)

// runGenericChecks is advisory; only executed in contract-v1 mode.
func runGenericChecks(root string, idx *ContractIndex) []Finding {
	var out []Finding
	if idx.Mode != "contract-v1" {
		return out
	}
	// Only run grammar-dependent checks if grammar file exists.
	hasGrammar := false
	if _, err := os.Stat(filepath.Join(root, GrammarRel)); err == nil {
		hasGrammar = true
	}
	out = append(out, checkActiveConstraintUnbound(idx)...)
	if hasGrammar {
		out = append(out, checkDimensionRouting(idx)...)
		out = append(out, checkActiveDimensionRule(idx)...)
	}
	// Constraint / GR / ROLE / PROOF reference integrity must be checked even
	// on Core+thin without Grammar: referencing GR-* while Grammar is absent
	// must surface as constraint_ref_missing rather than passing silently.
	out = append(out, checkConstraintRefs(idx)...)
	out = append(out, checkDerivationCompleteness(root, idx)...)
	out = append(out, checkSurfaceVersion(root, idx)...)
	out = append(out, checkHandoffBudget(root, idx)...)
	out = append(out, checkInvestmentProfile(root, idx)...)
	out = append(out, checkSemanticRoleUnbound(root, idx)...)
	out = append(out, checkPrimitiveConsumption(root, idx)...)
	out = append(out, checkGeneratedAssetUnverifiable(root, idx)...)
	out = append(out, checkProjectRules(root, idx)...)
	return out
}

func checkActiveConstraintUnbound(idx *ContractIndex) []Finding {
	var findings []Finding
	for id, row := range idx.Constraints {
		status := strings.ToLower(strings.TrimSpace(row.Status))
		if status != "active" {
			continue
		}
		binding := strings.TrimSpace(row.Binding)
		proof := strings.TrimSpace(row.Proof)
		if binding == "" || binding == "—" || binding == "-" {
			findings = append(findings, Finding{
				Code:     "active_constraint_unbound",
				Severity: SeverityWarning,
				Path:     row.File,
				Detail:   id + " status active requires non-empty Binding at line " + itoa(row.Line),
			})
			continue
		}
		if proof == "" || proof == "—" || proof == "-" {
			findings = append(findings, Finding{
				Code:     "active_constraint_unbound",
				Severity: SeverityWarning,
				Path:     row.File,
				Detail:   id + " status active requires Proof/review at line " + itoa(row.Line),
			})
		}
		// Checkability static without binding coverage: if Checkability contains static, Binding must be verifiable.
		ch := strings.ToLower(row.Checkability)
		if strings.Contains(ch, "static") {
			b := strings.ToLower(binding)
			// human-review is not static-verifiable
			if b == "human-review" && !strings.Contains(ch, "human") {
				findings = append(findings, Finding{
					Code:     "active_constraint_unbound",
					Severity: SeverityWarning,
					Path:     row.File,
					Detail:   id + " Checkability static requires Binding with static coverage, got human-review at line " + itoa(row.Line),
				})
			}
		}
	}
	return findings
}

var allowedDimStatus = map[string]bool{"active": true, "inherited": true, "debt": true, "n/a": true, "na": true}
var expectedDimensions = []string{"Information", "Composition", "Color", "Typography", "Shape & Surface", "Image & Icon", "Interaction", "Content", "Motion"}

func checkDimensionRouting(idx *ContractIndex) []Finding {
	var findings []Finding
	// If no dimensions table at all, warn dimension_unrouted (but only if we have a grammar file)
	if len(idx.Dimensions) == 0 {
		// No dimensions rows -> all 9 unrouted
		findings = append(findings, Finding{
			Code:     "dimension_unrouted",
			Severity: SeverityWarning,
			Path:     GrammarRel,
			Detail:   "nine dimensions must be routed active/inherited/debt/N/A; no dimensions table found",
		})
		return findings
	}
	// Check each dimension present
	for _, dim := range expectedDimensions {
		row, ok := idx.Dimensions[dim]
		if !ok {
			findings = append(findings, Finding{
				Code:     "dimension_unrouted",
				Severity: SeverityWarning,
				Path:     GrammarRel,
				Detail:   "dimension " + dim + " not routed active/inherited/debt/N/A",
			})
			continue
		}
		st := strings.ToLower(strings.TrimSpace(row.Status))
		st = strings.ReplaceAll(st, " ", "")
		if !allowedDimStatus[st] {
			findings = append(findings, Finding{
				Code:     "dimension_unrouted",
				Severity: SeverityWarning,
				Path:     row.File,
				Detail:   "dimension " + dim + " at line " + itoa(row.Line) + " has invalid status " + row.Status + " (expected active/inherited/debt/N/A)",
			})
		}
	}
	// Also check any extra dimension rows with invalid status
	for name, row := range idx.Dimensions {
		st := strings.ToLower(strings.TrimSpace(row.Status))
		stNorm := strings.ReplaceAll(st, " ", "")
		if st != "" && !allowedDimStatus[stNorm] {
			// already reported if in expected list
			isExpected := false
			for _, e := range expectedDimensions {
				if e == name {
					isExpected = true
					break
				}
			}
			if !isExpected {
				findings = append(findings, Finding{
					Code:     "dimension_unrouted",
					Severity: SeverityWarning,
					Path:     row.File,
					Detail:   "dimension " + name + " at line " + itoa(row.Line) + " invalid status " + row.Status,
				})
			}
		}
	}
	return findings
}

func checkActiveDimensionRule(idx *ContractIndex) []Finding {
	var findings []Finding
	for dimName, dimRow := range idx.Dimensions {
		if strings.ToLower(strings.TrimSpace(dimRow.Status)) != "active" {
			continue
		}
		found := false
		for _, gr := range idx.GrammarRules {
			dims := strings.ToLower(gr.Dimensions)
			// dims is comma-separated
			for _, part := range strings.Split(dims, ",") {
				if strings.TrimSpace(strings.ToLower(part)) == strings.ToLower(dimName) {
					found = true
					break
				}
				// also handle Chinese dimension names? They are English.
			}
			if found {
				break
			}
		}
		if !found {
			findings = append(findings, Finding{
				Code:     "active_dimension_rule_missing",
				Severity: SeverityWarning,
				Path:     dimRow.File,
				Detail:   "active dimension " + dimName + " at line " + itoa(dimRow.Line) + " has no GR covering it",
			})
		}
	}
	return findings
}

func checkConstraintRefs(idx *ContractIndex) []Finding {
	var findings []Finding
	// Helper to check a comma list
	checkList := func(ids string, file string, line int, context string) {
		for _, id := range parseIDList(ids) {
			if id == "—" || id == "-" || id == "" {
				continue
			}
			if strings.HasPrefix(id, "LOCAL-") {
				continue
			}
			// Determine type
			known := false
			if _, ok := idx.Constraints[id]; ok {
				known = true
			}
			if _, ok := idx.GrammarRules[id]; ok {
				known = true
			}
			if _, ok := idx.Roles[id]; ok {
				known = true
			}
			if _, ok := idx.Surfaces[id]; ok {
				known = true
			}
			if _, ok := idx.Proofs[id]; ok {
				known = true
			}
			if _, ok := idx.Debts[id]; ok {
				known = true
			}
			if _, ok := idx.Evidence[id]; ok {
				known = true
			}
			// Thin paths may cite these prefixes without a separate registry.
			// Recognizing their notation does not authenticate the evidence.
			if strings.HasPrefix(id, "EVD-") || strings.HasPrefix(id, "EX-") || strings.HasPrefix(id, "DEBT-") || strings.HasPrefix(id, "DCHK-") {
				// Allow EX/DEBT/DCHK without requiring registry if not tracked
				known = true
			}
			if !known && isValidID(id) {
				findings = append(findings, Finding{
					Code:     "constraint_ref_missing",
					Severity: SeverityWarning,
					Path:     file,
					Detail:   context + " at line " + itoa(line) + " references unknown ID " + id,
				})
			}
		}
	}
	for _, gr := range idx.GrammarRules {
		checkList(gr.Source, gr.File, gr.Line, "GR "+gr.ID+" Source")
		checkList(gr.Binding, gr.File, gr.Line, "GR "+gr.ID+" Binding")
		checkList(gr.Proof, gr.File, gr.Line, "GR "+gr.ID+" Proof")
	}
	for _, role := range idx.Roles {
		checkList(role.Source, role.File, role.Line, "ROLE "+role.ID+" Source")
	}
	for _, row := range idx.Constraints {
		checkList(row.Source, row.File, row.Line, row.ID+" Source")
		checkList(row.Binding, row.File, row.Line, row.ID+" Binding")
		checkList(row.Proof, row.File, row.Line, row.ID+" Proof")
	}
	for _, proof := range idx.Proofs {
		checkList(proof.Cover, proof.File, proof.Line, "PROOF "+proof.ID+" covers")
	}
	// Derivations
	for _, pack := range idx.Derivations {
		for _, t := range pack.Active {
			for _, row := range t.Rows {
				idRaw := cellByAliases(row, []string{"id"})
				for _, id := range parseIDList(idRaw) {
					if id == "—" || strings.HasPrefix(id, "LOCAL-") {
						continue
					}
					if _, ok := idx.Constraints[id]; !ok {
						if isValidID(id) {
							findings = append(findings, Finding{
								Code:     "constraint_ref_missing",
								Severity: SeverityWarning,
								Path:     t.File,
								Detail:   "derivation-active at line " + itoa(t.Line) + " references unknown constraint " + id,
							})
						}
					}
				}
				grRaw := cellByAliases(row, []string{"gr", "gr-*", "需要打开的 gr-*"})
				for _, grID := range parseIDList(grRaw) {
					if grID == "—" || grID == "" {
						continue
					}
					if _, ok := idx.GrammarRules[grID]; !ok {
						findings = append(findings, Finding{
							Code:     "constraint_ref_missing",
							Severity: SeverityWarning,
							Path:     t.File,
							Detail:   "derivation-active at line " + itoa(t.Line) + " references unknown GR " + grID,
						})
					}
				}
			}
		}
		for _, t := range pack.MustNot {
			for _, row := range t.Rows {
				src := cellByAliases(row, []string{"source", "source id"})
				for _, id := range parseIDList(src) {
					if id == "—" || id == "" || strings.HasPrefix(id, "LOCAL-") {
						continue
					}
					if _, ok := idx.Constraints[id]; !ok {
						if isValidID(id) || strings.HasPrefix(id, "ANTI-") || strings.HasPrefix(id, "LAW-") {
							findings = append(findings, Finding{
								Code:     "constraint_ref_missing",
								Severity: SeverityWarning,
								Path:     t.File,
								Detail:   "derivation-must-not at line " + itoa(t.Line) + " references unknown source " + id,
							})
						}
					}
				}
			}
		}
	}
	return findings
}

func parseIDList(s string) []string {
	if strings.TrimSpace(s) == "" || strings.TrimSpace(s) == "—" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		// also handle Chinese comma
		if strings.Contains(t, "，") {
			for _, q := range strings.Split(t, "，") {
				qq := strings.TrimSpace(q)
				if qq != "" {
					out = append(out, qq)
				}
			}
			continue
		}
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func checkDerivationCompleteness(root string, idx *ContractIndex) []Finding {
	var findings []Finding
	changed, _ := listChangedREQs(root)
	for _, req := range changed {
		pack, ok := idx.Derivations[req.ID]
		if !ok {
			// already reported as derivation_missing by Check; skip duplicate
			continue
		}
		hasActive := false
		for _, t := range pack.Active {
			if len(t.Rows) > 0 {
				for _, row := range t.Rows {
					if strings.TrimSpace(cellByAliases(row, []string{"id"})) != "" && strings.TrimSpace(cellByAliases(row, []string{"id"})) != "—" {
						hasActive = true
						break
					}
				}
			}
		}
		if !hasActive {
			findings = append(findings, Finding{
				Code:     "derivation_contract_incomplete",
				Severity: SeverityWarning,
				Path:     pack.File,
				Detail:   "REQ-" + req.ID + " derivation-active must list at least one LAW/ANTI/INV with GR",
			})
		}
		hasMustNot := false
		for _, t := range pack.MustNot {
			if len(t.Rows) > 0 {
				hasMustNot = true
				break
			}
		}
		if !hasMustNot {
			findings = append(findings, Finding{
				Code:     "derivation_contract_incomplete",
				Severity: SeverityWarning,
				Path:     pack.File,
				Detail:   "REQ-" + req.ID + " derivation-must-not must list at least one Must not row",
			})
		}
		hasBinding := false
		for _, t := range pack.Bindings {
			if len(t.Rows) > 0 {
				hasBinding = true
				break
			}
		}
		if !hasBinding {
			findings = append(findings, Finding{
				Code:     "derivation_contract_incomplete",
				Severity: SeverityWarning,
				Path:     pack.File,
				Detail:   "REQ-" + req.ID + " derivation-bindings must list at least one ROLE/PATTERN binding",
			})
		}
		hasProof := false
		for _, t := range pack.Proofs {
			for _, row := range t.Rows {
				pathRaw := cellByAliases(row, []string{"path", "路径", "可解析路径"})
				if strings.TrimSpace(pathRaw) == "" || strings.TrimSpace(pathRaw) == "—" {
					continue
				}
				// strip backticks
				pathRaw = strings.Trim(pathRaw, "` ")
				if pathRaw == "" {
					continue
				}
				hasProof = true
				// verify file exists
				abs := filepath.Join(root, pathRaw)
				if _, err := os.Stat(abs); err != nil {
					findings = append(findings, Finding{
						Code:     "derivation_contract_incomplete",
						Severity: SeverityWarning,
						Path:     pack.File,
						Detail:   "REQ-" + req.ID + " derivation-proof path " + pathRaw + " not found on disk",
					})
				}
			}
		}
		if !hasProof {
			findings = append(findings, Finding{
				Code:     "derivation_contract_incomplete",
				Severity: SeverityWarning,
				Path:     pack.File,
				Detail:   "REQ-" + req.ID + " derivation-proof must have at least one resolvable PROOF path",
			})
		}
	}
	return findings
}

func checkSurfaceVersion(root string, idx *ContractIndex) []Finding {
	var findings []Finding
	kernelVer := readKernelVersion(root)
	// For each derivation pack, check Foundation version matches kernel
	for _, pack := range idx.Derivations {
		data, err := os.ReadFile(filepath.Join(root, pack.File))
		if err != nil {
			continue
		}
		content := string(data)
		// look for header line with Foundation: ...@v...
		for _, line := range strings.Split(content, "\n") {
			if kernelVer != "" && strings.Contains(line, "Foundation") && strings.Contains(line, "@v") {
				if m := versionAtRe.FindString(line); m != "" {
					if m != "@v"+kernelVer && m != "v"+kernelVer {
						// normalize compare
						got := strings.TrimPrefix(m, "@")
						if got != "v"+kernelVer && got != kernelVer {
							findings = append(findings, Finding{
								Code:     "surface_version_mismatch",
								Severity: SeverityWarning,
								Path:     pack.File,
								Detail:   "Foundation reference " + m + " does not match current " + KernelRel + " v" + kernelVer,
							})
						}
					}
				}
			}
		}
		if ref := designHeaderValue(content, "Surface"); ref != "" && ref != "local" {
			findings = append(findings, checkDeclaredSurfaceVersion(root, idx, pack.File, ref, kernelVer)...)
		}
	}
	// Also check REQ Foundation reference version
	changed, _ := listChangedREQs(root)
	for _, req := range changed {
		if kernelVer == "" || req.FoundationRef == "" || req.FoundationRef == "local" || req.FoundationRef == "pending-foundation" {
			continue
		}
		if m := versionAtRe.FindString(req.FoundationRef); m != "" {
			got := strings.TrimPrefix(m, "@")
			if got != "v"+kernelVer && strings.TrimPrefix(got, "v") != kernelVer {
				findings = append(findings, Finding{
					Code:     "surface_version_mismatch",
					Severity: SeverityWarning,
					Path:     req.Path,
					Detail:   "Foundation reference " + m + " does not match current " + KernelRel + " v" + kernelVer,
				})
			}
		}
	}
	return findings
}

func readKernelVersion(root string) string {
	data, err := os.ReadFile(filepath.Join(root, KernelRel))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "版本") || strings.Contains(strings.ToLower(line), "version") {
			if m := versionAtRe.FindString(line); m != "" {
				return strings.TrimPrefix(strings.TrimPrefix(m, "@"), "v")
			}
			// also look for "> 版本：vX.Y.Z"
			if strings.Contains(line, "v") {
				// extract via regex
				re := regexp.MustCompile(`v[0-9]+\.[0-9]+\.[0-9]+`)
				if mm := re.FindString(line); mm != "" {
					return strings.TrimPrefix(mm, "v")
				}
			}
		}
	}
	return ""
}

func checkHandoffBudget(root string, idx *ContractIndex) []Finding {
	var findings []Finding
	kernel, _ := os.ReadFile(filepath.Join(root, KernelRel))
	card := kernelSectionZero(string(kernel))
	changed, err := listChangedREQs(root)
	if err != nil {
		return []Finding{{Code: "handoff_packet_unmeasured", Severity: SeverityInfo, Path: "docs/requirements", Detail: "Cannot read the declared REQ set; handoff budget was not measured. Main should inspect the original inputs."}}
	}
	for _, req := range changed {
		if req.FoundationRef == "local" {
			continue
		}
		lines, bytes := 0, 0
		var sources, omitted []string
		count := func(text, source string) {
			sources = append(sources, source)
			bytes += len(text)
			if text != "" {
				lines += strings.Count(text, "\n")
				if !strings.HasSuffix(text, "\n") {
					lines++
				}
			}
		}
		if card == "" {
			omitted = append(omitted, KernelRel+" §0 (not readable/extractable)")
		} else {
			count(card, KernelRel+" §0")
		}
		pack, ok := idx.Derivations[req.ID]
		packPath := filepath.Join("docs", "design", "derivation", "REQ-"+req.ID+".md")
		if ok {
			packPath = pack.File
		}
		data, readErr := os.ReadFile(filepath.Join(root, packPath))
		if readErr != nil || !ok {
			omitted = append(omitted, packPath+" (not readable/indexed), applicable Surface not identified")
		} else {
			count(string(data), packPath)
			reference := designHeaderValue(string(data), "Surface")
			surfaceID, _, _ := strings.Cut(reference, "@")
			surface, found := idx.Surfaces[surfaceID]
			if !found {
				omitted = append(omitted, "Surface "+reference+" (no matching declared row)")
			} else if strings.Trim(surface.Profile, "` ") == "inline" {
				// A raw indexed row is an observable lower bound. Do not invent
				// an inline packet format or treat unlocated prose as zero bytes.
				text, err := os.ReadFile(filepath.Join(root, surface.File))
				rows := strings.Split(string(text), "\n")
				if err == nil && surface.Line > 0 && surface.Line <= len(rows) {
					count(rows[surface.Line-1]+"\n", surface.File+":"+itoa(surface.Line)+" ("+surfaceID+" inline row)")
				}
				omitted = append(omitted, surfaceID+" inline table context and any prose outside the indexed row")
			} else {
				path, _, _ := strings.Cut(strings.Trim(surface.Profile, "` "), "@")
				path = filepath.Clean(path)
				if path == "." || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
					omitted = append(omitted, surfaceID+" Profile (no explicit project-relative path)")
				} else {
					if !strings.HasPrefix(filepath.ToSlash(path), "docs/") {
						path = filepath.Join("docs", "design", path)
					}
					if profile, err := os.ReadFile(filepath.Join(root, path)); err != nil {
						omitted = append(omitted, path+" (not readable)")
					} else {
						count(string(profile), path)
					}
				}
			}
		}
		code := "handoff_packet_estimate"
		detail := "REQ-" + req.ID + " counted source fragments: " + itoa(lines) + " lines / " + itoa(bytes) + " bytes; counted: " + strings.Join(sources, ", ") + ". This is a source-scope estimate, not the actual transmitted packet."
		if len(omitted) > 0 {
			code = "handoff_packet_unmeasured"
			detail += " Not counted: " + strings.Join(omitted, "; ") + "."
		}
		detail += " Main checks the actual handoff; 120 lines / 12KB is a clarity target, not a stage gate or permission to remove necessary context."
		findings = append(findings, Finding{Code: code, Severity: SeverityInfo, Path: packPath, Detail: detail})
		if lines > 120 || bytes > 12*1024 {
			findings = append(findings, Finding{Code: "handoff_packet_oversize", Severity: SeverityWarning, Path: packPath, Detail: "REQ-" + req.ID + " counted source fragments alone exceed the handoff target (120 lines / 12KB). " + detail})
		}
	}
	return findings
}

func readKernelSectionZero(root string) string {
	data, err := os.ReadFile(filepath.Join(root, KernelRel))
	if err != nil {
		return ""
	}
	return kernelSectionZero(string(data))
}

func kernelSectionZero(content string) string {
	// extract from ## 0. Next-agent card until next ## heading
	re := regexp.MustCompile(`(?m)^##\s*0\.`)
	loc := re.FindStringIndex(content)
	if loc == nil {
		return ""
	}
	rest := content[loc[0]:]
	// find next heading
	nextRe := regexp.MustCompile(`(?m)^##(?:[ \t]+|$)`)
	if nl := nextRe.FindStringIndex(rest[1:]); nl != nil {
		rest = rest[:nl[0]+1]
	}
	return rest
}

func checkInvestmentProfile(root string, idx *ContractIndex) []Finding {
	// Only warn if there's UI work (locked changed REQ) but no investment record.
	changed, _ := listChangedREQs(root)
	if len(changed) == 0 {
		return nil
	}
	// Check project-map.md for design investment row
	data, err := os.ReadFile(filepath.Join(root, "docs", "project-map.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return []Finding{{
				Code:     "investment_profile_missing",
				Severity: SeverityWarning,
				Path:     "docs/project-map.md",
				Detail:   "UI impact=changed REQ exists but no docs/project-map.md investment profile (local/core/extended) found",
			}}
		}
		return nil
	}
	content := strings.ToLower(string(data))
	if strings.Contains(content, "design investment") {
		// check if row contains local/core/extended/n/a
		if strings.Contains(content, "local") || strings.Contains(content, "core") || strings.Contains(content, "extended") || strings.Contains(content, "n/a") {
			return nil
		}
		return []Finding{{
			Code:     "investment_profile_missing",
			Severity: SeverityWarning,
			Path:     "docs/project-map.md",
			Detail:   "design investment row exists but does not record local/core/extended/N/A",
		}}
	}
	return []Finding{{
		Code:     "investment_profile_missing",
		Severity: SeverityWarning,
		Path:     "docs/project-map.md",
		Detail:   "UI work exists but Baseline Index has no design investment entry (local/core/extended/N/A)",
	}}
}
