package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Final domain checks describe the executed Main checks, not a Worker's claim
// that an arbitrary command passed. Immutable proposal bytes remain untouched.
func verifiedDomainChecks(root string, e workspace.Execution, cp integration.Checkpoint) ([]repair.RepairCheck, error) {
	canonicalRoot, err := workspace.Canonical(root)
	if err != nil {
		return nil, err
	}
	root = canonicalRoot
	if cp.TestedHead == "" {
		return nil, fmt.Errorf("domain checks require tested_head")
	}
	checks := map[string]repair.RepairCheck{}
	allowed := filepath.Dir(integration.CheckpointPath(root, e.RuntimeID, e.BaselineGeneration, e.AssignmentID))
	for _, path := range cp.CheckReceipts {
		physical, err := workspace.Canonical(path)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(allowed, physical)
		if err != nil || !filepath.IsLocal(rel) {
			return nil, fmt.Errorf("check receipt escapes assignment evidence")
		}
		data, err := os.ReadFile(physical)
		if err != nil {
			return nil, err
		}
		var r struct {
			Command string `json:"command"`
			CWD     string `json:"cwd"`
			Before  string `json:"head_before"`
			After   string `json:"head_after"`
			Status  string `json:"status"`
			Exit    int    `json:"exit_code"`
		}
		if err = json.Unmarshal(data, &r); err != nil {
			return nil, err
		}
		if r.Status != "pass" || r.Exit != 0 || r.Before != cp.TestedHead || r.After != cp.TestedHead {
			continue
		}
		cwd, err := workspace.Canonical(r.CWD)
		historical := false
		for _, previous := range cp.PreviousMainRoots {
			if r.CWD == previous {
				historical = true
			}
		}
		if (err != nil || cwd != root) && !historical {
			return nil, fmt.Errorf("check receipt belongs to another Main checkout")
		}
		sum := sha256.Sum256(data)
		relative, _ := filepath.Rel(root, physical)
		checks[r.Command] = repair.RepairCheck{Name: "verified Main integration", Command: r.Command, Result: "pass", EvidenceRefs: []string{"path:" + filepath.ToSlash(relative) + "#sha256=" + hex.EncodeToString(sum[:])}}
	}
	result := []repair.RepairCheck{}
	for _, command := range e.Checks {
		if strings.HasPrefix(command, "locked:") {
			continue
		}
		check, ok := checks[command]
		if !ok {
			return nil, fmt.Errorf("no passing receipt for declared check at tested_head: %s", command)
		}
		result = append(result, check)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("S9 final result requires executed Main checks")
	}
	return result, nil
}
