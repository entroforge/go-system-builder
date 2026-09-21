// Package projectlayout owns the finite document layout of this Harness release.
// It does not infer file sources or rewrite persisted Runtime references.
package projectlayout

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	Definition        = "docs/control/loop-definition.json"
	Policy            = "docs/control/hook-policy.json"
	Protocol          = "docs/control/agent-protocol.md"
	ProtectedCommands = "docs/control/protected-commands.json"
	Requirements      = "docs/requirements"
	Contracts         = "docs/dev/contracts"
	Tasks             = "docs/dev/tasks"
	Architecture      = "docs/architecture"
	Reports           = "docs/reports"
	ReleaseAudits     = "docs/reports/release-audits"
	GettingStarted    = "docs/guides/getting-started.md"
)

// PreviousPaths are disjoint legacy authorities. Empty older directories are
// harmless remnants, except docs/product: that retired wrapper must be absent
// in the flat requirements layout. Legacy files must never be silently ignored.
var previousPaths = []string{
	"docs/loop-definition.json", "docs/hook-policy.json", "docs/agent-protocol.md",
	"docs/product", "docs/contracts", "docs/tasks", "docs/release_audits",
	"docs/design/architecture", "docs/design/data-model", "docs/design/state", "docs/design/dataflow",
}

// Check rejects legacy/mixed installs before any CLI write. Existing projects
// stay on their matching release; this binary intentionally has no hot upgrade
// or per-file fallback. Missing new assets are diagnosed by their own loaders,
// so read-only diagnostics and small explicitly scoped fixtures still work.
func Check(root string) error {
	for _, rel := range previousPaths {
		p := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("layout: inspect %s: %w", rel, err)
		}
		if info.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				return fmt.Errorf("layout: inspect %s: %w", rel, err)
			}
			if len(entries) == 0 && rel != "docs/product" {
				continue
			}
		}
		return fmt.Errorf("layout migration required: legacy or mixed authority %s; keep the previous complete Harness release for this project; install this release in a fresh target, do not overlay docs or refresh frozen fingerprints", rel)
	}
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		return nil
	} // Runtime loader owns unreadable/corrupt state.
	return CheckRuntime(data)
}

// CheckRuntime checks the actual state bytes, including explicitly located state
// files and pending writer candidates. It never rewrites historical references.
func CheckRuntime(data []byte) error {
	var state struct {
		Definition struct {
			Path string `json:"path"`
		} `json:"definition"`
		HookControl struct {
			PolicyRef struct {
				Path string `json:"path"`
			} `json:"policy_ref"`
		} `json:"hook_control"`
		BoundREQ *struct {
			Path string `json:"path"`
		} `json:"bound_req"`
		Documents []struct {
			Path       string `json:"path"`
			Generation int    `json:"generation"`
		} `json:"documents"`
		Baseline struct {
			Generation int `json:"generation"`
		} `json:"baseline"`
	}
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	refs := []string{state.Definition.Path, state.HookControl.PolicyRef.Path}
	if state.BoundREQ != nil {
		refs = append(refs, state.BoundREQ.Path)
	}
	for _, doc := range state.Documents {
		if doc.Generation > 0 && doc.Generation < state.Baseline.Generation {
			continue
		}
		refs = append(refs, doc.Path)
	}
	for _, ref := range refs {
		ref = filepath.ToSlash(filepath.Clean(ref))
		for _, old := range previousPaths {
			if ref == old || strings.HasPrefix(ref, old+"/") {
				return fmt.Errorf("layout migration required: Runtime still references %s; restore its matching release; historical references must not be rewritten or re-signed", ref)
			}
		}
	}
	return nil
}

// IsReleaseAudit prevents a nested release report directory from inheriting
// the broader S7 and repair-preflight report write permissions.
func IsReleaseAudit(rel string) bool {
	return rel == ReleaseAudits || strings.HasPrefix(rel, ReleaseAudits+"/")
}
