package scenario

import (
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The AC↔CASE bridge is the single-denominator rule made mechanical
// (L3-S2 v4.0.1): every acceptance criterion of the bound REQ must reach a
// scenario case through FR→BR→CASE, or carry an endorsed N/A (an NFR id or
// an explicit negative-space pointer — free text is not an exit, it is a
// silent removal from the verification denominator).

type boundREQInfo struct {
	ID   string
	Path string
}

// readBoundREQ reads the bound REQ from the runtime state without importing
// the runtime package (decode-only; a missing or inactive runtime means the
// bridge simply has nothing to check).
func readBoundREQ(root string) (boundREQInfo, bool) {
	data, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		return boundREQInfo{}, false
	}
	var state map[string]any
	if json.Unmarshal(data, &state) != nil {
		return boundREQInfo{}, false
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if stateName, _ := lifecycle["state"].(string); stateName == "" || stateName == "inactive" {
		return boundREQInfo{}, false
	}
	bound, _ := state["bound_req"].(map[string]any)
	id, _ := bound["id"].(string)
	path, _ := bound["path"].(string)
	if id == "" || path == "" {
		return boundREQInfo{}, false
	}
	return boundREQInfo{ID: id, Path: path}, true
}

type reqTableRow struct {
	ID     string
	Target string // last non-empty cell (the 指向 column for AC rows)
}

var reqIDCellPattern = regexp.MustCompile(`^(FR|AC|NFR)-[A-Z0-9]+(-[A-Z0-9]+)*$`)

// DeclaredFRIDs shares the original current-section/fenced-example parser;
// callers retain responsibility for selecting the exact requirement source.
func DeclaredFRIDs(content string) map[string]bool {
	rows, _, _ := parseREQTables(content)
	ids := make(map[string]bool, len(rows))
	for _, row := range rows {
		ids[row.ID] = true
	}
	return ids
}

// parseREQTables extracts FR/AC/NFR table rows from the funnel REQ
// template's §C tables (pipe tables whose first cell is a typed id).
func parseREQTables(content string) (fr, ac, nfr []reqTableRow) {
	// Remove fenced examples before detecting the current §C section.
	var current []string
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
			marker := trimmed[0]
			n := 0
			for n < len(trimmed) && trimmed[n] == marker {
				n++
			}
			fence = trimmed[:n]
			continue
		}
		current = append(current, line)
	}
	// Use the same ATX heading predicate for discovery and section selection.
	heading := func(line string) (level int, sectionC bool) {
		trimmed := strings.TrimSpace(line)
		level = len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
		if level == 0 || level > 6 || len(trimmed) == level || (trimmed[level] != ' ' && trimmed[level] != '\t') {
			return 0, false
		}
		words := strings.Fields(trimmed[level:])
		return level, len(words) > 0 && words[0] == "§C"
	}
	// Heading-free legacy fixtures remain valid.
	hasSectionC := false
	for _, line := range current {
		if _, isSectionC := heading(line); isSectionC {
			hasSectionC = true
			break
		}
	}
	sectionLevel := 0
	for _, line := range current {
		trimmed := strings.TrimSpace(line)
		if level, isSectionC := heading(line); level > 0 {
			if sectionLevel > 0 && level <= sectionLevel {
				sectionLevel = 0
			}
			if isSectionC {
				sectionLevel = level
			}
		}
		if hasSectionC && sectionLevel == 0 {
			continue
		}
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) == 0 {
			continue
		}
		if id := cells[0]; reqIDCellPattern.MatchString(id) {
			target := ""
			if len(cells) >= 3 {
				target = cells[len(cells)-1]
			}
			row := reqTableRow{ID: id, Target: target}
			switch id[:strings.IndexByte(id, '-')] {
			case "FR":
				fr = append(fr, row)
			case "AC":
				ac = append(ac, row)
			case "NFR":
				nfr = append(nfr, row)
			}
		}
	}
	return fr, ac, nfr
}

// BridgeResult reports the source-stage check outcome for humans.
type BridgeResult struct {
	REQ            string
	TotalAC        int
	ReachedCases   int
	EndorsedNA     int
	IgnoredEntries []string
}

// RunBridge checks the bound REQ's acceptance criteria against every module
// package's scenario-model. requireBranches=false is the source-stage check
// (usable right after convergence-1, before cases are generated); true adds
// the BR→CASE hop (rule must carry at least one branch).
func RunBridge(root string, requireBranches bool) (BridgeResult, error) {
	return RunBridgeWithFiles(root, requireBranches, fileview.Disk{Root: root})
}
func RunBridgeWithFiles(root string, requireBranches bool, files fileview.Reader) (BridgeResult, error) {
	root, _ = filepath.Abs(root)
	result := BridgeResult{}
	bound, ok := readBoundREQ(root)
	if !ok {
		result.IgnoredEntries = append(result.IgnoredEntries, "no bound REQ — bridge skipped")
		return result, nil
	}
	result.REQ = bound.ID
	reqData, err := files.ReadFile(filepath.Join(root, filepath.FromSlash(bound.Path)))
	if err != nil {
		return result, fmt.Errorf("AC bridge: read bound REQ %s: %w", bound.Path, err)
	}
	frRows, acRows, nfrRows := parseREQTables(string(reqData))
	frIDs := map[string]bool{}
	for _, row := range frRows {
		frIDs[row.ID] = true
	}
	nfrIDs := map[string]bool{}
	for _, row := range nfrRows {
		nfrIDs[row.ID] = true
	}

	// Collect FR-level rule references across all module packages.
	frRules := map[string]bool{} // "FR-003" -> referenced by some rule (with a branch when required)
	modules, err := listModulesWithFiles(root, files)
	if err != nil {
		return result, fmt.Errorf("AC bridge: %w", err)
	}
	for _, module := range modules {
		data, err := files.ReadFile(filepath.Join(root, prototypeRoot, module, modelFile))
		if err != nil {
			continue
		}
		var model struct {
			Rules []struct {
				SourceRefs []string `json:"source_refs"`
				Branches   []struct {
					CaseID string `json:"case_id"`
				} `json:"branches"`
			} `json:"rules"`
		}
		if json.Unmarshal(data, &model) != nil {
			continue
		}
		for _, rule := range model.Rules {
			hasBranch := len(rule.Branches) > 0
			for _, ref := range rule.SourceRefs {
				parts := strings.SplitN(ref, "/", 2)
				if len(parts) != 2 || parts[0] != bound.ID {
					continue
				}
				if !requireBranches || hasBranch {
					frRules[parts[1]] = true
				}
			}
		}
	}

	for _, ac := range acRows {
		result.TotalAC++
		target := strings.TrimSpace(ac.Target)
		if target == "" {
			return result, fmt.Errorf("AC bridge: %s has no 指向 target — every acceptance criterion must reach FR/CASE or carry an endorsed N/A (NFR id or §A4 pointer); silence is not N/A", ac.ID)
		}
		targets := splitACTargets(target)
		if len(targets) == 0 {
			return result, fmt.Errorf("AC bridge: %s target %q is neither FR-/NFR-/§A4 — free-text exits are not endorsed N/A, they silently remove the criterion from the verification denominator", ac.ID, target)
		}
		hasFR := false
		for _, ref := range targets {
			switch {
			case strings.HasPrefix(ref, "FR-"):
				if !reqIDCellPattern.MatchString(ref) || !frIDs[ref] {
					return result, fmt.Errorf("AC bridge: %s target %q is not a declared FR in the current REQ table", ac.ID, ref)
				}
				if !frRules[ref] {
					return result, fmt.Errorf("AC bridge: %s points at %s but no rule in any module package cites %s/%s in source_refs — add the branch or endorse the N/A", ac.ID, ref, bound.ID, ref)
				}
				hasFR = true
			case strings.HasPrefix(ref, "NFR-"):
				if !nfrIDs[ref] {
					return result, fmt.Errorf("AC bridge: %s endorses N/A via %s but the NFR is not declared in the REQ's non-functional table", ac.ID, ref)
				}
			case strings.HasPrefix(ref, "§A4"):
			default:
				return result, fmt.Errorf("AC bridge: %s target %q is neither FR-/NFR-/§A4 — free-text exits are not endorsed N/A, they silently remove the criterion from the verification denominator", ac.ID, ref)
			}
		}
		if hasFR {
			result.ReachedCases++
		} else {
			result.EndorsedNA++
		}
	}
	return result, nil
}

func splitACTargets(target string) []string {
	return strings.FieldsFunc(target, func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == ';' || r == '；' || r == ' ' || r == '\t' || r == '\n'
	})
}

// listModules enumerates module package directories under the prototypes
// root (templates and non-directories excluded).
func listModules(root string) ([]string, error) {
	return listModulesWithFiles(root, fileview.Disk{Root: root})
}
func listModulesWithFiles(root string, files fileview.Reader) ([]string, error) {
	root, _ = filepath.Abs(root)
	entries, err := files.ReadDir(filepath.Join(root, prototypeRoot))
	if err != nil {
		return nil, err
	}
	var modules []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "template" || entry.Name() == "templates" {
			continue
		}
		modules = append(modules, entry.Name())
	}
	return modules, nil
}

// GuardBridgeChecked is the PTR-PLAN-02 mount of the AC↔CASE bridge (D2:
// the check rides the planning advance, not a voluntary command). When
// module packages exist the full bridge runs. When the prototypes root is
// absent entirely, only a REQ whose every AC is endorsed N/A (or that has
// no ACs) may pass — an AC pointing at FR- with no packages to cite it is
// a broken denominator, not a boundary case.
func GuardBridgeChecked(root string) error {
	return GuardBridgeCheckedWithFiles(root, fileview.Disk{Root: root})
}
func GuardBridgeCheckedWithFiles(root string, files fileview.Reader) error {
	root, _ = filepath.Abs(root)
	modules, err := listModulesWithFiles(root, files)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("ac bridge: enumerate module packages: %w", err)
		}
		bound, ok := readBoundREQ(root)
		if !ok {
			// No bound REQ and no packages: nothing to check (template/CI
			// contexts). A bound REQ without packages is checked below.
			return nil
		}
		data, err := files.ReadFile(filepath.Join(root, filepath.FromSlash(bound.Path)))
		if err != nil {
			return fmt.Errorf("ac bridge: read bound REQ %s: %w", bound.Path, err)
		}
		_, acRows, _ := parseREQTables(string(data))
		for _, ac := range acRows {
			for _, ref := range splitACTargets(ac.Target) {
				if !strings.HasPrefix(ref, "FR-") {
					continue
				}
				return fmt.Errorf("ac bridge: %s points at %s but no module packages exist under docs/design/prototypes — this is an S2 design-package gap (see docs/control/agent-protocol.md#s2 failure_route): build the package or endorse the N/A (an NFR id declared in the REQ, or a §A4 明确不做 entry); silence is not N/A", ac.ID, ref)
			}
		}
		return nil
	}
	if _, err := RunBridgeWithFiles(root, true, files); err != nil {
		return err
	}
	// The module source packages (scenario-model, fixtures, cross-matrix)
	// re-validate on the natural path too — a matrix edited after generate
	// must not survive to the planning advance unnoticed.
	for _, module := range modules {
		if _, err := loadSourcePackageWithFiles(root, module, files); err != nil {
			return fmt.Errorf("module %s: %w", module, err)
		}
	}
	return nil
}
