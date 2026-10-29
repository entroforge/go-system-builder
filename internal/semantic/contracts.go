package semantic

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"github.com/entroforge/go-system-builder/internal/scenario"
	"github.com/entroforge/go-system-builder/internal/sharedmodel"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	contractTokenPatterns = []*regexp.Regexp{
		regexp.MustCompile(`CASE-[A-Z0-9]+(?:-[A-Z0-9]+)*`),
		regexp.MustCompile(`\bS-[0-9]{3}\b`),
		regexp.MustCompile(`\bF-[0-9]{3}\b`),
		regexp.MustCompile(`\bPATH-[A-Z0-9]+(?:-[A-Z0-9]+)*`),
		regexp.MustCompile(`\bFR-[A-Z0-9]+(?:-[A-Z0-9]+)*`),
	}
	contractClauseCellPattern  = regexp.MustCompile(`\b(FE|BE|SYNC)-[A-Z0-9]+(?:-[A-Z0-9]+)*\s*§\d+`)
	contractQualifiedFRPattern = regexp.MustCompile(`\b(REQ-[A-Z0-9]+(?:-[A-Z0-9]+)*)/(FR-[A-Z0-9]+(?:-[A-Z0-9]+)*)\b`)
)

type ContractCheckResult struct {
	Contracts    int      `json:"contracts"`
	TokenRefs    int      `json:"token_refs"`
	Clauses      int      `json:"clauses"`
	Fingerprints int      `json:"fingerprints"`
	Problems     []string `json:"problems,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	ModelRows    int      `json:"model_rows"`
}

// ContractsCheck is S3's mechanical close (L3-S3 v4.0.1). Division of labor
// with the S2 AC bridge: the bridge owns REQ-side AC↔CASE; this owns
// contract-side token existence. Non-goal: free-text cell semantics.
func ContractsCheck(root string) (ContractCheckResult, error) {
	return ContractsCheckWithFiles(root, fileview.Disk{Root: root})
}
func ContractsCheckWithFiles(root string, files fileview.Reader, reqIDs ...string) (ContractCheckResult, error) {
	requested := ""
	if len(reqIDs) > 0 {
		requested = reqIDs[0]
	}
	requirement, err := contractRequirement(root, requested)
	if err != nil {
		return ContractCheckResult{}, err
	}
	if requirement.ID != "" {
		paths, err := contractBatchPaths(root, files, requirement.ID)
		if err != nil {
			return ContractCheckResult{}, err
		}
		return ContractsCheckBatch(root, files, requirement, paths)
	}
	return contractsCheck(root, files, requirement, nil)
}

func contractsCheck(root string, files fileview.Reader, requirement workpacket.Reference, paths []string) (ContractCheckResult, error) {
	root, _ = filepath.Abs(root)
	result := ContractCheckResult{Problems: []string{}}
	var reqIDs []string
	if requirement.ID != "" {
		reqIDs = []string{requirement.ID}
	}
	models := sharedmodel.Check(root, files, reqIDs...)
	result.Problems = append(result.Problems, models.Problems...)
	result.Warnings = models.Warnings
	if requirement.ID == "" {
		result.Warnings = append(result.Warnings, "all-project authoring diagnostic: no active REQ scope; this does not certify any single contract batch")
	}
	result.ModelRows = models.Rows
	dir := filepath.Join(root, projectlayout.Contracts)
	entries, err := files.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil // no contracts directory — nothing to reconcile
		}
		return result, fmt.Errorf("read docs/dev/contracts: %w", err)
	}

	universe := map[string]bool{}
	caseUniverse := map[string]bool{}
	currentCases := map[string]bool{}
	modules, _ := files.ReadDir(filepath.Join(root, "docs", "design", "prototypes"))
	for _, module := range modules {
		if !module.IsDir() || module.Name() == "template" || module.Name() == "templates" {
			continue
		}
		mpath := filepath.Join(root, "docs", "design", "prototypes", module.Name())
		moduleGenerated := map[string]bool{}
		for _, file := range []string{"cases.json", "stories.md", "flows.md"} {
			data, err := files.ReadFile(filepath.Join(mpath, file))
			if err != nil {
				continue
			}
			if file == "cases.json" {
				ids, err := generatedCaseIDs(data)
				if err != nil {
					result.Problems = append(result.Problems, fmt.Sprintf("%s: cases.json unreadable: %v", module.Name(), err))
				}
				for id := range ids {
					moduleGenerated[id] = true
					universe[id] = true
					caseUniverse[id] = true
				}
			}
			for _, token := range contractTokens(string(data)) {
				if strings.HasPrefix(token, "CASE-") {
					continue // CASE existence comes from actual generated/model IDs.
				}
				universe[token] = true
			}
		}
		// The scenario-model's branch case_ids are the authoritative CASE
		// denominator: cases.json is a generated artifact, and a tampered
		// cases.json (delete a CASE, delete its citations) would otherwise
		// silently shrink the verification denominator.
		modelCaseIDs, scopedCases, modelErr := modelCaseSetsWithFiles(filepath.Join(mpath, "scenario-model.json"), files, requirement.ID)
		for id := range scopedCases {
			currentCases[id] = true
		}
		switch {
		case modelErr != nil:
			result.Problems = append(result.Problems, fmt.Sprintf("%s: scenario-model.json unreadable: %v — the CASE denominator cannot be verified", module.Name(), modelErr))
		case requirement.ID != "" && len(modelCaseIDs) == 0 && len(moduleGenerated) > 0:
			result.Problems = append(result.Problems, fmt.Sprintf("%s: original model sources are unavailable; cannot establish CASE obligations for %s", module.Name(), requirement.ID))
		case len(modelCaseIDs) > 0:
			for id := range modelCaseIDs {
				caseUniverse[id] = true
				universe[id] = true
			}
			if casesData, err := files.ReadFile(filepath.Join(mpath, "cases.json")); err == nil {
				generated, _ := generatedCaseIDs(casesData)
				for id := range modelCaseIDs {
					if !generated[id] {
						result.Problems = append(result.Problems, fmt.Sprintf("%s: cases.json is missing %s declared by scenario-model.json — regenerate (`scenario generate`) instead of hand-editing generated artifacts", module.Name(), id))
					}
				}
				for id := range generated {
					if !modelCaseIDs[id] {
						result.Problems = append(result.Problems, fmt.Sprintf("%s: cases.json carries %s which scenario-model.json does not declare — regenerate instead of hand-editing generated artifacts", module.Name(), id))
					}
				}
			}
		}
	}

	reqFRs := map[string]bool{}
	reqEntries, _ := files.ReadDir(filepath.Join(root, projectlayout.Requirements))
	var reqFiles []string
	for _, entry := range reqEntries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "REQ-") && strings.HasSuffix(entry.Name(), ".md") {
			reqFiles = append(reqFiles, filepath.Join(root, projectlayout.Requirements, entry.Name()))
		}
	}
	if requirement.ID == "" { // Legacy all-repository authoring diagnostic.
		for _, reqFile := range reqFiles {
			data, err := files.ReadFile(reqFile)
			if err != nil {
				continue
			}
			for id := range scenario.DeclaredFRIDs(string(data)) {
				reqFRs[id] = true
			}
		}
	}
	frByREQ := map[string]map[string]bool{}
	readFRs := func(owner string) (map[string]bool, error) {
		if ids, ok := frByREQ[owner]; ok {
			return ids, nil
		}
		ids, err := contractRequirementFRs(root, files, requirement, owner)
		if err == nil {
			frByREQ[owner] = ids
		}
		return ids, err
	}

	citedCases := map[string]bool{}
	contractIDs := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || strings.Contains(entry.Name(), "template") || entry.Name() == "README.md" {
			continue // README is directory documentation, not a contract
		}
		id := strings.TrimSuffix(entry.Name(), ".md")
		contractIDs[id] = filepath.Join(dir, entry.Name())
	}

	checkedIDs := contractIDs
	if requirement.ID != "" {
		checkedIDs = map[string]string{}
		for _, path := range paths {
			id := strings.TrimSuffix(filepath.Base(path), ".md")
			actual, ok := contractIDs[id]
			if !ok || filepath.Clean(filepath.Join(root, path)) != filepath.Clean(actual) {
				result.Problems = append(result.Problems, fmt.Sprintf("%s: current contract delivery path %s is unavailable", requirement.ID, path))
				continue
			}
			checkedIDs[id] = actual
		}
	}
	for id, path := range checkedIDs {
		result.Contracts++
		data, err := files.ReadFile(path)
		if err != nil {
			result.Problems = append(result.Problems, fmt.Sprintf("%s: unreadable: %v", id, err))
			continue
		}
		content := currentContractText(string(data))
		bareFRs := map[string]bool{}
		if requirement.ID != "" {
			for _, token := range contractTokens(contractQualifiedFRPattern.ReplaceAllString(content, "")) {
				bareFRs[token] = true
			}
			for _, ref := range contractQualifiedFRPattern.FindAllStringSubmatch(content, -1) {
				ids, err := readFRs(ref[1])
				if err != nil {
					result.Problems = append(result.Problems, fmt.Sprintf("%s: token %s: %v", id, ref[0], err))
				} else if !ids[ref[2]] {
					result.Problems = append(result.Problems, fmt.Sprintf("%s: token %s is not declared by %s", id, ref[0], ref[1]))
				}
			}
		}
		content = expandContractCaseRanges(content)
		var unknownGroups []string
		content, unknownGroups = expandContractCaseGroups(content, caseUniverse)
		for _, group := range unknownGroups {
			result.Problems = append(result.Problems, fmt.Sprintf("%s: CASE group %s matches no known CASE", id, group))
		}
		for _, token := range contractTokens(content) {
			result.TokenRefs++
			if strings.HasPrefix(token, "CASE-") {
				citedCases[token] = true
			}
			if strings.HasPrefix(token, "FR-") {
				if requirement.ID != "" {
					if bareFRs[token] {
						ids, err := readFRs(requirement.ID)
						if err != nil {
							result.Problems = append(result.Problems, fmt.Sprintf("%s: token %s: %v", id, token, err))
						} else if !ids[token] {
							result.Problems = append(result.Problems, fmt.Sprintf("%s: token %s is not declared by current %s", id, token, requirement.ID))
						}
					}
				} else if !reqFRs[token] {
					result.Problems = append(result.Problems, fmt.Sprintf("%s: token %s does not exist in any REQ's FR table", id, token))
				}
				continue
			}
			if !universe[token] {
				result.Problems = append(result.Problems, fmt.Sprintf("%s: token %s does not exist in any module package (cases/stories/flows)", id, token))
			}
		}
		for _, cell := range contractClauseCellPattern.FindAllString(content, -1) {
			result.Clauses++
			contractID := strings.TrimSpace(strings.Fields(cell)[0])
			target, ok := contractIDs[contractID]
			if !ok {
				result.Problems = append(result.Problems, fmt.Sprintf("%s: clause cell %q points at unknown contract", id, cell))
				continue
			}
			// The existing template declares obligations in 本合同条款,
			// not in prose, examples or another contract's reference column.
			// This is structural reconciliation; S3/S5 own the semantics.
			n := clauseNumberOf(cell)
			targetData, err := files.ReadFile(target)
			declared := declaredClauseNumbers(string(targetData), contractID)
			if err != nil || !declared[n] {
				result.Problems = append(result.Problems, fmt.Sprintf("%s: clause cell %q cites %s §%s but the target contract's 本合同条款 column does not declare that clause — Main must align this reference with the target's existing clause table; prose, examples and foreign contract references cannot declare its obligations", id, cell, contractID, n))
			}
		}
		for _, line := range strings.Split(content, "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "|") {
				continue
			}
			cells := strings.Split(strings.Trim(trimmed, "|"), "|")
			for i, cell := range cells {
				cell = strings.TrimSpace(cell)
				if isHex64(strings.ToLower(cell)) && strings.Contains(strings.ToLower(strings.Join(cells[:i], " ")), "fingerprint") {
					result.Fingerprints++
					if resolved, ok := resolveContractFingerprintWithFiles(root, trimmed, files); ok && resolved != cell {
						result.Problems = append(result.Problems, fmt.Sprintf("%s: fingerprint column does not match disk (recorded %s… actual %s…)", id, cell[:12], resolved[:12]))
					}
				}
			}
		}
	}
	// Reverse closure: every generated CASE is a verification-denominator
	// member; a case no contract clause cites is neither locked nor covered
	// by any TASK — the chain leaks at CASE→contract (single-denominator rule).
	if result.Contracts > 0 {
		requiredCases := caseUniverse
		if requirement.ID != "" {
			requiredCases = currentCases
		}
		for token := range requiredCases {
			if !citedCases[token] {
				result.Problems = append(result.Problems, fmt.Sprintf("reverse closure: %s exists in module packages but no contract in the checked batch %s cites it — cite it in the CONTRACTS index coverage matrix (or the FE contract case-mapping table) so it re-enters the verification denominator", token, requirement.ID))
			}
		}
	}
	return result, nil
}

// currentContractText keeps fenced examples out of current contract relations.
func currentContractText(content string) string {
	var lines []string
	fence := ""
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, string(fence[0])+" \t") == "" {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			n := 0
			for n < len(trimmed) && trimmed[n] == trimmed[0] {
				n++
			}
			fence = trimmed[:n]
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

var contractCaseRangePattern = regexp.MustCompile(`\bCASE-([0-9]{3})\s*(?:~|–|至)\s*CASE-([0-9]{3})\b`)

// Explicit numeric ranges enumerate every member of the current denominator.
func expandContractCaseRanges(content string) string {
	return contractCaseRangePattern.ReplaceAllStringFunc(content, func(ref string) string {
		parts := contractCaseRangePattern.FindStringSubmatch(ref)
		start, _ := strconv.Atoi(parts[1])
		end, _ := strconv.Atoi(parts[2])
		if end < start {
			return ref
		}
		var ids []string
		for n := start; n <= end; n++ {
			ids = append(ids, fmt.Sprintf("CASE-%03d", n))
		}
		return strings.Join(ids, " ")
	})
}

var contractCaseGroupPattern = regexp.MustCompile(`\bCASE-[A-Z0-9]+(?:-[A-Z0-9]+)*-\*`)

// A group cites only matching CASEs already known during this check.
func expandContractCaseGroups(content string, known map[string]bool) (string, []string) {
	var unknown []string
	expanded := contractCaseGroupPattern.ReplaceAllStringFunc(content, func(ref string) string {
		prefix := strings.TrimSuffix(ref, "*")
		var matched []string
		for id := range known {
			if strings.HasPrefix(id, prefix) {
				matched = append(matched, id)
			}
		}
		if len(matched) == 0 {
			unknown = append(unknown, ref)
			return ""
		}
		sort.Strings(matched)
		return strings.Join(matched, " ")
	})
	return expanded, unknown
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func resolveContractFingerprint(root, row string) (string, bool) {
	return resolveContractFingerprintWithFiles(root, row, fileview.Disk{Root: root})
}
func resolveContractFingerprintWithFiles(root, row string, files fileview.Reader) (string, bool) {
	cells := strings.Split(strings.Trim(strings.TrimSpace(row), "|"), "|")
	var fileRef string
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		for _, name := range []string{"scenario-model.json", "fixture-contract.json", "cross-matrix.json", "cases.json", "scenario-coverage.json", "index.html", "stories.md", "flows.md"} {
			if strings.HasSuffix(cell, name) {
				fileRef = cell
				break
			}
		}
		if fileRef != "" {
			break
		}
	}
	if fileRef == "" {
		return "", false
	}
	data, err := files.ReadFile(filepath.Join(root, filepath.FromSlash(fileRef)))
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), true
}

// clauseNumberOf extracts the digits of the trailing §n in a clause cell.
func clauseNumberOf(cell string) string {
	fields := strings.Fields(cell)
	return strings.TrimPrefix(fields[len(fields)-1], "§")
}

// contractTokens keeps CASE references whole: a CASE fragment inside another
// identifier (for example FACT-CATEGORY-CASE-VARIANTS) is not a CASE citation.
func contractTokens(content string) []string {
	var tokens []string
	for _, pattern := range contractTokenPatterns {
		for _, span := range pattern.FindAllStringIndex(content, -1) {
			token := content[span[0]:span[1]]
			if strings.HasPrefix(token, "CASE-") {
				startsInsideIdentifier := span[0] > 0 && contractIdentifierByte(content[span[0]-1])
				leadingDash := span[0] > 0 && content[span[0]-1] == '-' &&
					(span[0] == 1 || !contractIdentifierByte(content[span[0]-2]))
				endsInsideIdentifier := span[1] < len(content) && contractIdentifierByte(content[span[1]])
				if startsInsideIdentifier && !leadingDash || endsInsideIdentifier {
					continue
				}
			}
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func contractIdentifierByte(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}

func generatedCaseIDs(data []byte) (map[string]bool, error) {
	var document struct {
		Cases []struct {
			ID string `json:"id"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, item := range document.Cases {
		if item.ID != "" {
			ids[item.ID] = true
		}
	}
	return ids, nil
}

// modelCaseIDs extracts the branch case_id set from a module's
// scenario-model.json — the authoritative CASE denominator.
func modelCaseIDs(path string) (map[string]bool, error) {
	return modelCaseIDsWithFiles(path, fileview.Disk{Root: "."})
}
func modelCaseIDsWithFiles(path string, files fileview.Reader) (map[string]bool, error) {
	all, _, err := modelCaseSetsWithFiles(path, files, "")
	return all, err
}

func modelCaseSetsWithFiles(path string, files fileview.Reader, reqID string) (map[string]bool, map[string]bool, error) {
	data, err := files.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var model struct {
		Rules []struct {
			SourceRefs []string `json:"source_refs"`
			Branches   []struct {
				CaseID string `json:"case_id"`
			} `json:"branches"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(data, &model); err != nil {
		return nil, nil, err
	}
	ids := map[string]bool{}
	scoped := map[string]bool{}
	for _, rule := range model.Rules {
		if reqID != "" && len(rule.Branches) > 0 && len(rule.SourceRefs) == 0 {
			return nil, nil, fmt.Errorf("rule source_refs unavailable; cannot establish CASE obligations for %s", reqID)
		}
		belongs := reqID == ""
		for _, ref := range rule.SourceRefs {
			if ref == reqID || strings.HasPrefix(ref, reqID+"/") {
				belongs = true
			}
		}
		for _, branch := range rule.Branches {
			if branch.CaseID != "" {
				ids[branch.CaseID] = true
				if belongs {
					scoped[branch.CaseID] = true
				}
			}
		}
	}
	return ids, scoped, nil
}

var declaredClausePattern = regexp.MustCompile(`^(?:((?:FE|BE|SYNC)-[A-Z0-9]+(?:-[A-Z0-9]+)*)\s+)?§(\d+)$`)

// Read the original template's own declaration column. A bounded structural
// reader suffices here: pipe tables with header/separator rows and explicit
// clause tokens. This does not interpret promises or establish coverage.
func declaredClauseNumbers(content, contractID string) map[string]bool {
	out := map[string]bool{}
	column := -1
	var previous []string
	for _, line := range strings.Split(currentContractText(content), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			column, previous = -1, nil
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		separator := true
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
			if strings.Trim(cells[i], ":- ") != "" || !strings.Contains(cells[i], "-") {
				separator = false
			}
		}
		if separator {
			column = -1
			if len(previous) == len(cells) {
				for i, cell := range previous {
					if strings.Trim(strings.Trim(cell, "`* "), "§ ") == "本合同条款" {
						column = i
					}
				}
			}
		} else if column >= 0 && column < len(cells) {
			for _, token := range strings.FieldsFunc(cells[column], func(r rune) bool { return strings.ContainsRune(",，、;；/", r) }) {
				m := declaredClausePattern.FindStringSubmatch(strings.Trim(token, "`* \t"))
				if m != nil && (m[1] == "" || m[1] == contractID) {
					out[m[2]] = true
				}
			}
		}
		previous = cells
	}
	return out
}
