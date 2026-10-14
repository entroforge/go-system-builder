package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/pathscope"
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
	Module          string
	ID              string
	Title           string
	Polarity        string
	FlowRefs        []string
	Oracle          map[string]any
	Required        bool
	BrowserRequired bool
}

// A text scan is only a navigation hint. It cannot declare a selector,
// environment, collection membership, or a reusable regression asset.
type e2eCandidateCase struct {
	ModuleRef string
	CaseID    string
}

type e2eSourceCandidate struct {
	Path          string
	SHA256        string
	PossibleCases []e2eCandidateCase
}

type e2eInventory struct {
	Cases      []e2eScenario
	Candidates []e2eSourceCandidate
	// Assets is reserved for admitted runner collection records. A source
	// mention never populates it.
	Assets []E2EAsset
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

// discoverE2EInventory reads the existing S2 CASE denominator and returns
// possible source locations separately from admitted reusable assets. The
// cases.json module plus CASE id is the identity; a bare CASE id is ambiguous
// across modules. Source scanning cannot establish runner collection or PASS.
func discoverE2EInventory(root string, state map[string]any) (e2eInventory, []string) {
	inventory := e2eInventory{Assets: []E2EAsset{}}
	var diagnostics []string
	moduleFilter := boundE2EModules(root, state)
	prototypes := filepath.Join(root, "docs", "design", "prototypes")
	entries, err := os.ReadDir(prototypes)
	if os.IsNotExist(err) {
		return inventory, nil
	}
	if err != nil {
		return inventory, []string{fmt.Sprintf("read E2E prototype root: %v", err)}
	}
	for _, entry := range entries {
		if !entry.IsDir() || (len(moduleFilter) > 0 && !moduleFilter[entry.Name()]) {
			continue
		}
		path := filepath.Join(prototypes, entry.Name(), "cases.json")
		data, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("read %s: %v", filepath.ToSlash(filepath.Join("docs", "design", "prototypes", entry.Name(), "cases.json")), readErr))
			continue
		}
		var document e2eCasesDocument
		if err := json.Unmarshal(data, &document); err != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("decode %s: %v", filepath.ToSlash(filepath.Join("docs", "design", "prototypes", entry.Name(), "cases.json")), err))
			continue
		}
		if document.Module != entry.Name() {
			diagnostics = append(diagnostics, fmt.Sprintf("CASE module binding mismatch: %s declares %q", path, document.Module))
			continue
		}
		for _, item := range document.Cases {
			if !item.Required || !item.BrowserRequired || strings.TrimSpace(item.ID) == "" {
				continue
			}
			inventory.Cases = append(inventory.Cases, e2eScenario{
				Module: entry.Name(), ID: item.ID, Title: item.Title, Polarity: item.Polarity,
				FlowRefs: append([]string(nil), item.FlowRefs...), Oracle: item.Oracle,
				Required: item.Required, BrowserRequired: item.BrowserRequired,
			})
		}
	}
	if len(inventory.Cases) == 0 {
		return inventory, diagnostics
	}

	caseIDs := make(map[string][]e2eCandidateCase, len(inventory.Cases))
	for _, item := range inventory.Cases {
		caseIDs[item.ID] = append(caseIDs[item.ID], e2eCandidateCase{ModuleRef: "docs/design/prototypes/" + item.Module + "/cases.json", CaseID: item.ID})
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
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("read E2E spec %s: %v", path, readErr))
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		digest := sha256.Sum256(data)
		sha := hex.EncodeToString(digest[:])
		candidate := e2eSourceCandidate{Path: rel, SHA256: sha}
		for caseID, refs := range caseIDs {
			if strings.Contains(string(data), caseID) {
				candidate.PossibleCases = append(candidate.PossibleCases, refs...)
			}
		}
		if len(candidate.PossibleCases) > 0 {
			sort.Slice(candidate.PossibleCases, func(i, j int) bool {
				a, b := candidate.PossibleCases[i], candidate.PossibleCases[j]
				if a.ModuleRef != b.ModuleRef {
					return a.ModuleRef < b.ModuleRef
				}
				return a.CaseID < b.CaseID
			})
			inventory.Candidates = append(inventory.Candidates, candidate)
		}
		return nil
	})
	sort.Slice(inventory.Cases, func(i, j int) bool {
		a, b := inventory.Cases[i], inventory.Cases[j]
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		return a.ID < b.ID
	})
	sort.Slice(inventory.Candidates, func(i, j int) bool { return inventory.Candidates[i].Path < inventory.Candidates[j].Path })
	sort.Slice(inventory.Assets, func(i, j int) bool { return inventory.Assets[i].AssetID < inventory.Assets[j].AssetID })
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

func boundE2EModules(root string, state map[string]any) map[string]bool {
	bound, _ := state["bound_req"].(map[string]any)
	path, _ := bound["path"].(string)
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(filepath.Clean(path))))
	if err != nil {
		return nil
	}
	modules := map[string]bool{}
	for _, match := range e2eModuleRefPattern.FindAllStringSubmatch(string(data), -1) {
		if len(match) == 2 {
			modules[match[1]] = true
		}
	}
	return modules
}
