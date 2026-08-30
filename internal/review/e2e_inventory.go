package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

type e2eInventory struct {
	Cases  []e2eScenario
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

// discoverE2EInventory reads the existing S2 module package and maps each
// required browser CASE to a repository Playwright spec that mentions its
// CASE id. It intentionally does not infer selectors or environments from
// prose: those remain optional author-owned asset metadata. A CASE without a
// matching spec is still returned, which makes DraftPlan choose cold_start
// and split the work instead of silently dropping coverage.
func discoverE2EInventory(root string, state map[string]any) (e2eInventory, []string) {
	var inventory e2eInventory
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

	caseIDs := make(map[string]e2eScenario, len(inventory.Cases))
	for _, item := range inventory.Cases {
		caseIDs[item.ID] = item
	}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("walk E2E assets: %v", walkErr))
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".claude", "node_modules", "vendor", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if !hasSuffix(path, e2eSpecSuffixes) {
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
		for caseID := range caseIDs {
			if !strings.Contains(string(data), caseID) {
				continue
			}
			scenario := caseIDs[caseID]
			inventory.Assets = append(inventory.Assets, E2EAsset{
				AssetID: "e2e-asset:" + caseID + ":" + rel,
				CaseRef: caseID, Path: rel, SHA256: sha,
				// S7-7 (RC-07): a spec that merely mentions the CASE id — even
				// in a comment — is not a regression asset by itself. Record
				// the executability fingerprint the asset gate requires: the
				// test-id selector surface, the module route/flow refs the
				// CASE declares, and the module environment tag. The
				// declaration is validated, not inferred from prose, so
				// substring matching can never silently become
				// "regression available".
				SelectorRef: "testid:" + caseID,
				RouteRef:    strings.Join(append([]string{scenario.Module}, scenario.FlowRefs...), ","),
				Environment: "module=" + scenario.Module,
			})
		}
		return nil
	})
	sort.Slice(inventory.Cases, func(i, j int) bool { return inventory.Cases[i].ID < inventory.Cases[j].ID })
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
