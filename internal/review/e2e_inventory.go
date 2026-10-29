package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/pathscope"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// e2eScenario is the small CASE projection the S7 Planner needs. The full
// cases.json remains the authority; this projection only carries the fields
// needed to name an Assignment and build a concrete oracle prompt.
type e2eScenario struct {
	CasesPath       string
	Module          string
	ID              string
	Title           string
	Polarity        string
	FlowRefs        []string
	Oracle          map[string]any
	Required        bool
	BrowserRequired bool
}

type e2eSourceCandidate struct {
	Path          string
	SHA256        string
	PossibleCases []string
}

type e2eInventory struct {
	Cases      []e2eScenario
	Candidates []e2eSourceCandidate
}

type e2eCasesDocument struct {
	Module string            `json:"module"`
	Cases  []e2eScenarioJSON `json:"cases"`
}

type e2eScenarioJSON struct {
	ID              string         `json:"id"`
	Title           string         `json:"title"`
	Polarity        string         `json:"polarity"`
	FlowRefs        []string       `json:"flow_refs"`
	Oracle          map[string]any `json:"oracle"`
	Required        bool           `json:"required"`
	BrowserRequired bool           `json:"browser_required"`
}

var e2eSpecSuffixes = []string{".spec.ts", ".spec.tsx", ".spec.js", ".spec.jsx"}

// Discovery offers source hints, never an executable-asset declaration.
// The full cases path keeps module-local CASE/PATH identifiers distinct.
func discoverE2EInventory(root string, state map[string]any, plans ...*Plan) (e2eInventory, []string) {
	var inventory e2eInventory
	var diagnostics []string
	plan := &Plan{}
	if len(plans) > 0 {
		plan = plans[0]
	}
	cases, err := knownE2ECases(context.Background(), root, state, plan)
	if err != nil {
		return inventory, []string{err.Error()}
	}
	inventory.Cases = cases
	if len(inventory.Cases) == 0 {
		return inventory, diagnostics
	}

	caseIDs := make(map[string][]string, len(inventory.Cases))
	for _, item := range inventory.Cases {
		caseIDs[item.ID] = append(caseIDs[item.ID], scenarioRef(item))
	}
	scope := pathscope.New(root)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("walk E2E assets: %v", walkErr))
			return nil
		}
		if scope.Excludes(path) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".claude", "node_modules", "vendor", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || !hasSuffix(path, e2eSpecSuffixes) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		data, readErr := workpacket.ReadFile(root, rel, 2<<20)
		if readErr != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("read E2E candidate %s: %v", rel, readErr))
			return nil
		}
		digest := sha256.Sum256(data)
		candidate := e2eSourceCandidate{Path: rel, SHA256: hex.EncodeToString(digest[:])}
		for caseID, refs := range caseIDs {
			if mentionsCaseID(string(data), caseID) {
				candidate.PossibleCases = append(candidate.PossibleCases, refs...)
			}
		}
		if len(candidate.PossibleCases) > 0 {
			sort.Strings(candidate.PossibleCases)
			inventory.Candidates = append(inventory.Candidates, candidate)
		}
		return nil
	})
	sort.Slice(inventory.Cases, func(i, j int) bool { return scenarioRef(inventory.Cases[i]) < scenarioRef(inventory.Cases[j]) })
	sort.Slice(inventory.Candidates, func(i, j int) bool { return inventory.Candidates[i].Path < inventory.Candidates[j].Path })
	return inventory, diagnostics
}

func hasSuffix(path string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(strings.ToLower(path), suffix) {
			return true
		}
	}
	return false
}

var e2eModuleRefPattern = regexp.MustCompile(`docs/design/prototypes/([a-z0-9]+(?:-[a-z0-9]+)*)/`)

func scenarioRef(s e2eScenario) string { return s.CasesPath + "#" + s.ID }

// A comment is still just a candidate, but CASE-0010 is not CASE-001.
func mentionsCaseID(text, id string) bool {
	if id == "" {
		return false
	}
	word := func(b byte) bool {
		return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-'
	}
	for offset := 0; offset < len(text); {
		i := strings.Index(text[offset:], id)
		if i < 0 {
			return false
		}
		i += offset
		end := i + len(id)
		if (i == 0 || !word(text[i-1])) && (end == len(text) || !word(text[end])) {
			return true
		}
		offset = end
	}
	return false
}
