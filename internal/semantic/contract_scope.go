package semantic

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/scenario"
	"github.com/entroforge/go-system-builder/internal/sharedmodel"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// ContractsCheckBatch reads the original already-validated S3 delivery set.
// Registration, source hashes and authority stay with its original consumers.
func ContractsCheckBatch(root string, files fileview.Reader, requirement workpacket.Reference, paths []string) (ContractCheckResult, error) {
	if requirement.ID == "" || requirement.Path == "" || len(paths) < 2 {
		return ContractCheckResult{}, fmt.Errorf("current contract check requires its original requirement and delivery paths")
	}
	return contractsCheck(root, files, requirement, paths)
}

func contractRequirement(root, requested string) (workpacket.Reference, error) {
	ref := workpacket.Reference{ID: requested}
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err == nil {
		var state struct {
			Lifecycle struct {
				State string `json:"state"`
			} `json:"lifecycle"`
			Bound workpacket.Reference `json:"bound_req"`
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return ref, fmt.Errorf("read original contracts scope: %w", err)
		}
		if requested != "" && requested == state.Bound.ID || requested == "" && state.Lifecycle.State != "" && state.Lifecycle.State != "inactive" {
			if state.Bound.ID == "" || state.Bound.Path == "" {
				return ref, fmt.Errorf("active contracts scope lacks its original bound REQ id/path; Main must restore the binding in .claude/loop-state.json")
			}
			ref = state.Bound
		}
	} else if !os.IsNotExist(err) {
		return ref, err
	}
	if ref.ID != "" && ref.Path == "" {
		ref.Path = filepath.ToSlash(filepath.Join(projectlayout.Requirements, ref.ID+".md"))
	}
	return ref, nil
}

// Diagnostics select the original index declarations. Normal S3 reception
// supplies its actual delivery paths directly and never re-enumerates a batch.
func contractBatchPaths(root string, files fileview.Reader, reqID string) ([]string, error) {
	entries, err := files.ReadDir(filepath.Join(root, projectlayout.Contracts))
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "CONTRACTS-") || !strings.HasSuffix(name, ".md") || strings.Contains(strings.ToLower(name), "template") {
			continue
		}
		path := filepath.ToSlash(filepath.Join(projectlayout.Contracts, name))
		data, err := files.ReadFile(filepath.Join(root, path))
		if err != nil {
			return nil, err
		}
		owner, err := sharedmodel.IndexRequirement(string(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if owner == "" && name == "CONTRACTS-"+strings.TrimPrefix(reqID, "REQ-")+".md" {
			owner = reqID
		}
		if owner != reqID {
			continue
		}
		if len(paths) > 0 {
			return nil, fmt.Errorf("REQ %s has multiple contract indexes; restore its original batch", reqID)
		}
		ids, err := runtime.ContractIndexContractIDs(string(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("%s for %s declares no contract members; restore the original batch declaration instead of scanning unrelated contracts", path, reqID)
		}
		paths = append(paths, path)
		for _, id := range ids {
			paths = append(paths, filepath.ToSlash(filepath.Join(projectlayout.Contracts, id+".md")))
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no original CONTRACTS index for %s under %s", reqID, projectlayout.Contracts)
	}
	return paths, nil
}

func contractRequirementFRs(root string, files fileview.Reader, current workpacket.Reference, owner string) (map[string]bool, error) {
	if owner == current.ID {
		if err := workpacket.CheckPath(root, current.Path); err != nil {
			return nil, err
		}
		data, err := files.ReadFile(filepath.Join(root, current.Path))
		if err != nil {
			return nil, fmt.Errorf("original %s source %s: %w", owner, current.Path, err)
		}
		return scenario.DeclaredFRIDs(string(data)), nil
	}
	// Historical REQs may retain descriptive filenames. Resolve the actual
	// declared identity in this original directory, without aliases or copies.
	entries, err := files.ReadDir(filepath.Join(root, projectlayout.Requirements))
	if err != nil {
		return nil, fmt.Errorf("original %s requirement sources: %w", owner, err)
	}
	var selected []byte
	selectedPath := ""
	for _, entry := range entries {
		name := entry.Name()
		canonical := name == owner+".md"
		if entry.IsDir() || !strings.HasSuffix(name, ".md") || !canonical && !strings.HasPrefix(name, owner+"-") {
			continue
		}
		path := filepath.ToSlash(filepath.Join(projectlayout.Requirements, name))
		if err := workpacket.CheckPath(root, path); err != nil {
			return nil, err
		}
		data, err := files.ReadFile(filepath.Join(root, path))
		if err != nil {
			return nil, fmt.Errorf("original %s source %s: %w", owner, path, err)
		}
		if !requirementHeadingNames(string(data), owner) {
			if canonical && len(requirementHeadingIDs(string(data))) > 0 {
				return nil, fmt.Errorf("original %s source %s declares another REQ identity", owner, path)
			}
			if !canonical {
				continue
			}
		}
		if selectedPath != "" {
			return nil, fmt.Errorf("original %s source is ambiguous between %s and %s; Main must restore its original source", owner, selectedPath, path)
		}
		selected, selectedPath = data, path
	}
	if selectedPath == "" {
		return nil, fmt.Errorf("original %s source is unavailable under %s", owner, projectlayout.Requirements)
	}
	return scenario.DeclaredFRIDs(string(selected)), nil
}

var requirementHeadingIDPattern = regexp.MustCompile(`\bREQ-[A-Z0-9]+(?:-[A-Z0-9]+)*\b`)

func requirementHeadingIDs(content string) []string {
	for _, line := range strings.Split(currentContractText(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return requirementHeadingIDPattern.FindAllString(line, -1)
		}
	}
	return nil
}

func requirementHeadingNames(content, owner string) bool {
	ids := requirementHeadingIDs(content)
	if len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		if id != owner {
			return false
		}
	}
	return true
}
