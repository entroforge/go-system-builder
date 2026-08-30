package scenario

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CrossMatrix is the convergence-1 carrier: the fact×FR×story completeness
// hunt made fill-in (L3-S2 v4.0.1). Each entry either names the branch that
// covers the cell or records why no branch exists — the field is the
// question (D4), the reference check is the machine's part. The machine
// floor is per-fact and per-story (every declared fact and story must be
// hunted at least once); the fact×story combinations themselves remain a
// human hunting judgment, not a cartesian product requirement.
type CrossMatrix struct {
	Module  string             `json:"module"`
	Entries []CrossMatrixEntry `json:"entries"`
}

// CrossMatrixEntry is one hunted cell. Exactly one of Branch /
// NoBranchReason must be set (schema-enforced); the reference fields are
// machine-checked against the module package.
type CrossMatrixEntry struct {
	Fact           string `json:"fact"`
	ReqRef         string `json:"req_ref"`
	Story          string `json:"story"`
	Branch         string `json:"branch,omitempty"`
	NoBranchReason string `json:"no_branch_reason,omitempty"`
}

// crossMatrixReqRefPattern accepts REQ-level ("REQ-001") and FR-level
// ("REQ-001/FR-003") references — FR-level is what the AC bridge resolves.
var crossMatrixReqRefPattern = regexp.MustCompile(`^REQ-[A-Z0-9]+(-[A-Z0-9]+)*(?:/FR-[A-Z0-9]+(-[A-Z0-9]+)*)?$`)

func decodeCrossMatrix(data []byte) (CrossMatrix, error) {
	var matrix CrossMatrix
	if err := json.Unmarshal(data, &matrix); err != nil {
		return CrossMatrix{}, fmt.Errorf("decode cross-matrix.json: %w", err)
	}
	return matrix, nil
}

// validateCrossMatrix checks that every hunted cell points at real package
// facts, stories, and branches, that a reason is recorded exactly when no
// branch covers the cell, that the named branch's rule actually cites the
// cell's REQ/FR reference (the matrix joins the AC↔CASE chain instead of
// running parallel to it), and that the hunt has a completeness floor: every
// fact and every story must appear in at least one cell.
func validateCrossMatrix(source sourcePackage, root string) error {
	matrix := source.crossMatrix
	if matrix.Module != source.model.Module {
		return fmt.Errorf("cross-matrix.json module %q does not match scenario-model module %q", matrix.Module, source.model.Module)
	}
	if len(matrix.Entries) == 0 {
		return fmt.Errorf("cross-matrix.json must enumerate at least one hunted cell — an empty hunt is no hunt")
	}
	factIDs := map[string]bool{}
	for _, fact := range source.model.Facts {
		factIDs[fact.ID] = true
	}
	branchRule := map[string]Rule{}
	for _, rule := range source.model.Rules {
		for _, branch := range rule.Branches {
			branchRule[branch.ID] = rule
		}
	}
	// The bound REQ's FR table is the join target for FR-level cells; without
	// a bound REQ (template fixtures) the table join degrades to shape-only.
	frIDs := map[string]bool{}
	reqID := ""
	if bound, ok := readBoundREQ(root); ok {
		reqID = bound.ID
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(bound.Path)))
		if err != nil {
			return fmt.Errorf("cross-matrix: read bound REQ %s: %w — the FR join cannot degrade to shape-only", bound.Path, err)
		}
		rows, _, _ := parseREQTables(string(data))
		if len(rows) == 0 {
			return fmt.Errorf("cross-matrix: bound REQ %s declares no FR table rows — the FR join cannot degrade to shape-only", bound.ID)
		}
		for _, row := range rows {
			frIDs[row.ID] = true
		}
	}
	coveredFacts := map[string]bool{}
	coveredStories := map[string]bool{}
	seen := map[string]bool{}
	for i, entry := range matrix.Entries {
		cell := fmt.Sprintf("entry %d (fact=%s req_ref=%s story=%s)", i, entry.Fact, entry.ReqRef, entry.Story)
		key := entry.Fact + "|" + entry.ReqRef + "|" + entry.Story
		if seen[key] {
			return fmt.Errorf("cross-matrix %s duplicates an earlier cell", cell)
		}
		seen[key] = true
		if !factIDs[entry.Fact] {
			return fmt.Errorf("cross-matrix %s references unknown fact %q", cell, entry.Fact)
		}
		coveredFacts[entry.Fact] = true
		if !crossMatrixReqRefPattern.MatchString(entry.ReqRef) {
			return fmt.Errorf("cross-matrix %s req_ref %q must be REQ-<id> or REQ-<id>/FR-<id>", cell, entry.ReqRef)
		}
		if reqID != "" {
			if refREQ := entry.ReqRef; refREQ != reqID && !strings.HasPrefix(refREQ, reqID+"/") {
				return fmt.Errorf("cross-matrix %s req_ref %q does not reference the bound REQ %s — the matrix must join the bound requirement's denominator", cell, entry.ReqRef, reqID)
			}
			if frID, _, ok := splitFRRef(entry.ReqRef); ok && !frIDs[frID] {
				return fmt.Errorf("cross-matrix %s req_ref %q names FR %q which the bound REQ's FR table does not declare", cell, entry.ReqRef, frID)
			}
		}
		if !storyRefPattern.MatchString(entry.Story) || !markdownHeadingContainsID(source.stories, entry.Story) {
			return fmt.Errorf("cross-matrix %s references story %q missing from stories.md", cell, entry.Story)
		}
		coveredStories[entry.Story] = true
		switch {
		case entry.Branch == "" && entry.NoBranchReason == "":
			return fmt.Errorf("cross-matrix %s names neither a branch nor a no-branch reason — silence is not N/A", cell)
		case entry.Branch != "" && entry.NoBranchReason != "":
			return fmt.Errorf("cross-matrix %s sets both branch and no-branch reason", cell)
		case entry.Branch != "" && branchRule[entry.Branch].ID == "":
			return fmt.Errorf("cross-matrix %s references unknown branch %q", cell, entry.Branch)
		case entry.Branch != "":
			rule := branchRule[entry.Branch]
			if !ruleCitesReqRef(rule, entry.ReqRef) {
				return fmt.Errorf("cross-matrix %s names branch %q but its rule %q never cites %q in source_refs — the matrix must join the model, not assert alongside it", cell, entry.Branch, rule.ID, entry.ReqRef)
			}
		default:
			if reason := strings.TrimSpace(entry.NoBranchReason); utf8.RuneCountInString(reason) < 8 || !strings.ContainsFunc(reason, unicode.IsLetter) {
				return fmt.Errorf("cross-matrix %s no_branch_reason %q is not a rationale — a real reason names the why (at least 8 characters with a letter); free-word escapes are not endorsed N/A", cell, entry.NoBranchReason)
			}
		}
	}
	for _, fact := range source.model.Facts {
		if !coveredFacts[fact.ID] {
			return fmt.Errorf("cross-matrix never hunts fact %q — every declared fact must appear in at least one cell (silence is not coverage)", fact.ID)
		}
	}
	for _, story := range storyIDsFromHeadings(source.stories) {
		if !coveredStories[story] {
			return fmt.Errorf("cross-matrix never hunts story %q — every story must appear in at least one cell (silence is not coverage)", story)
		}
	}
	return nil
}

// splitFRRef splits "REQ-001/FR-003" into ("FR-003", "REQ-001", true);
// REQ-level references return ok=false.
func splitFRRef(ref string) (fr, req string, ok bool) {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[1], parts[0], true
}

// ruleCitesReqRef reports whether the rule's source_refs cite the cell's
// reference: an FR-level ref must be cited exactly; a REQ-level ref is
// satisfied by any FR of that REQ.
func ruleCitesReqRef(rule Rule, ref string) bool {
	for _, cited := range rule.SourceRefs {
		if cited == ref {
			return true
		}
		if _, req, isFR := splitFRRef(ref); !isFR && strings.HasPrefix(cited, req+"/") {
			return true
		}
	}
	return false
}

// storyIDsFromHeadings extracts S-nnn ids from stories.md heading lines,
// using the same anywhere-in-heading caliber as markdownHeadingContainsID
// so a cell can never reference a story the floor fails to count.
func storyIDsFromHeadings(data []byte) []string {
	var ids []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		hashCount := 0
		for hashCount < len(line) && line[hashCount] == '#' {
			hashCount++
		}
		if hashCount == 0 || hashCount > 6 || hashCount == len(line) || (line[hashCount] != ' ' && line[hashCount] != '\t') {
			continue
		}
		heading := line[hashCount:]
		for _, token := range storyAnywherePattern.FindAllString(heading, -1) {
			if !seen[token] {
				seen[token] = true
				ids = append(ids, token)
			}
		}
	}
	return ids
}

// storyAnywherePattern matches an S-nnn token anywhere inside a heading —
// the same caliber containsExactID uses for cell references.
var storyAnywherePattern = regexp.MustCompile(`\bS-[0-9]{3}\b`)
