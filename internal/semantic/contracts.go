package semantic

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	contractClauseCellPattern = regexp.MustCompile(`\b(FE|BE|SYNC)-[A-Z0-9]+(?:-[A-Z0-9]+)*\s*§\d+`)
)

type ContractCheckResult struct {
	Contracts    int      `json:"contracts"`
	TokenRefs    int      `json:"token_refs"`
	Clauses      int      `json:"clauses"`
	Fingerprints int      `json:"fingerprints"`
	Problems     []string `json:"problems,omitempty"`
}

// ContractsCheck is S3's mechanical close (L3-S3 v4.0.1). Division of labor
// with the S2 AC bridge: the bridge owns REQ-side AC↔CASE; this owns
// contract-side token existence. Non-goal: free-text cell semantics.
func ContractsCheck(root string) (ContractCheckResult, error) {
	result := ContractCheckResult{Problems: []string{}}
	dir := filepath.Join(root, "docs", "contracts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil // no contracts directory — nothing to reconcile
		}
		return result, fmt.Errorf("read docs/contracts: %w", err)
	}

	universe := map[string]bool{}
	caseUniverse := map[string]bool{}
	modules, _ := os.ReadDir(filepath.Join(root, "docs", "design", "prototypes"))
	for _, module := range modules {
		if !module.IsDir() || module.Name() == "template" || module.Name() == "templates" {
			continue
		}
		mpath := filepath.Join(root, "docs", "design", "prototypes", module.Name())
		for _, file := range []string{"cases.json", "stories.md", "flows.md"} {
			data, err := os.ReadFile(filepath.Join(mpath, file))
			if err != nil {
				continue
			}
			for _, pattern := range contractTokenPatterns {
				for _, token := range pattern.FindAllString(string(data), -1) {
					universe[token] = true
					if file == "cases.json" && strings.HasPrefix(token, "CASE-") {
						caseUniverse[token] = true
					}
				}
			}
		}
		// The scenario-model's branch case_ids are the authoritative CASE
		// denominator: cases.json is a generated artifact, and a tampered
		// cases.json (delete a CASE, delete its citations) would otherwise
		// silently shrink the verification denominator.
		modelCaseIDs, modelErr := modelCaseIDs(filepath.Join(mpath, "scenario-model.json"))
		switch {
		case modelErr != nil:
			result.Problems = append(result.Problems, fmt.Sprintf("%s: scenario-model.json unreadable: %v — the CASE denominator cannot be verified", module.Name(), modelErr))
		case len(modelCaseIDs) > 0:
			for id := range modelCaseIDs {
				caseUniverse[id] = true
				universe[id] = true
			}
			if casesData, err := os.ReadFile(filepath.Join(mpath, "cases.json")); err == nil {
				generated := map[string]bool{}
				for _, pattern := range contractTokenPatterns {
					for _, token := range pattern.FindAllString(string(casesData), -1) {
						if strings.HasPrefix(token, "CASE-") {
							generated[token] = true
						}
					}
				}
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
	reqFiles, _ := filepath.Glob(filepath.Join(root, "docs", "requirements", "REQ-*.md"))
	for _, reqFile := range reqFiles {
		data, err := os.ReadFile(reqFile)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "| FR-") {
				cells := strings.Split(strings.Trim(trimmed, "|"), "|")
				if len(cells) > 0 {
					reqFRs[strings.TrimSpace(cells[0])] = true
				}
			}
		}
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

	for id, path := range contractIDs {
		result.Contracts++
		data, err := os.ReadFile(path)
		if err != nil {
			result.Problems = append(result.Problems, fmt.Sprintf("%s: unreadable: %v", id, err))
			continue
		}
		content := string(data)
		for _, pattern := range contractTokenPatterns {
			for _, token := range pattern.FindAllString(content, -1) {
				result.TokenRefs++
				if strings.HasPrefix(token, "CASE-") {
					citedCases[token] = true
				}
				if strings.HasPrefix(token, "FR-") {
					if !reqFRs[token] {
						result.Problems = append(result.Problems, fmt.Sprintf("%s: token %s does not exist in any REQ's FR table", id, token))
					}
					continue
				}
				if !universe[token] {
					result.Problems = append(result.Problems, fmt.Sprintf("%s: token %s does not exist in any module package (cases/stories/flows)", id, token))
				}
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
			// Cheap anti-drift check: the §n cited in an index
			// cell must exist in the target contract's own clause map —
			// otherwise the two sides number clauses independently. The
			// numbers are compared as a set, so §1 cannot satisfy §10
			// (substring comparison would be a false negative).
			n := clauseNumberOf(cell)
			targetData, err := os.ReadFile(target)
			declared := declaredClauseNumbers(string(targetData))
			if err != nil || !declared[n] {
				result.Problems = append(result.Problems, fmt.Sprintf("%s: clause cell %q cites %s §%s but the target contract never declares that clause number — align the index cell with the contract's own clause map", id, cell, contractID, n))
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
					if resolved, ok := resolveContractFingerprint(root, trimmed); ok && resolved != cell {
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
		for token := range caseUniverse {
			if !citedCases[token] {
				result.Problems = append(result.Problems, fmt.Sprintf("reverse closure: %s exists in module packages but no contract cites it — cite it in the CONTRACTS index coverage matrix (or the FE contract case-mapping table) so it re-enters the verification denominator", token))
			}
		}
	}
	return result, nil
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
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(fileRef)))
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

// modelCaseIDs extracts the branch case_id set from a module's
// scenario-model.json — the authoritative CASE denominator.
func modelCaseIDs(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var model struct {
		Rules []struct {
			Branches []struct {
				CaseID string `json:"case_id"`
			} `json:"branches"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(data, &model); err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, rule := range model.Rules {
		for _, branch := range rule.Branches {
			if branch.CaseID != "" {
				ids[branch.CaseID] = true
			}
		}
	}
	return ids, nil
}

// clauseNumbersPattern extracts every §n token (digits only) so clause
// numbers compare as a set — "§1" must not satisfy "§10".
var clauseNumbersPattern = regexp.MustCompile(`§(\d+)`)

func declaredClauseNumbers(content string) map[string]bool {
	out := map[string]bool{}
	for _, m := range clauseNumbersPattern.FindAllStringSubmatch(content, -1) {
		out[m[1]] = true
	}
	return out
}
