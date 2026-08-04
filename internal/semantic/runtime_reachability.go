package semantic

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// runtimeReachability mirrors the subset of .claude/loop-state.json that
// declares reachable documents. It is consumed by ValidateRuntimeReachability
// to enforce that every referenced document path resolves on disk and that
// the recorded SHA-256 matches the file the path points to.
type runtimeReachability struct {
	BoundREQ *struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"bound_req"`
	Documents []struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
		Kind   string `json:"kind"`
	} `json:"documents"`
	Entities struct {
		Agents []struct {
			ID            string `json:"id"`
			DefinitionRef string `json:"definition_ref"`
			PromptRef     string `json:"prompt_ref"`
			ReadbackRef   string `json:"readback_ref"`
			ActivationRef string `json:"activation_ref"`
		} `json:"agents"`
		Tasks []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"tasks"`
		Bugs []struct {
			ID         string `json:"id"`
			ReportPath string `json:"report_path"`
		} `json:"bugs"`
		Teams []struct {
			ID          string `json:"id"`
			ManifestRef string `json:"manifest_ref"`
		} `json:"teams"`
	} `json:"entities"`
	Evidence []struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"evidence"`
}

// ValidateRuntimeReachability checks that the current runtime's referenced
// documents are all reachable on disk and that their recorded SHA-256
// fingerprints match. Per BUG-004 §4b.2(b): bound REQ path+sha256, agent
// definition_ref/prompt_ref/readback_ref+sha256/activation_ref+sha256, task
// path+sha256, team manifest_ref, bug report_path, evidence scope_refs.
//
// checkReachablePath ensures every referenced path resolves to an existing
// file (no dangling references). checkReachableFingerprint ensures the
// recorded SHA-256 matches the on-disk hash (no silent content drift).
func ValidateRuntimeReachability(root string) error {
	runtimePath := filepath.Join(root, ".claude/loop-state.json")
	data, err := os.ReadFile(runtimePath)
	if err != nil {
		return fmt.Errorf("runtime reachability: read runtime: %w", err)
	}
	var state runtimeReachability
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("runtime reachability: decode runtime: %w", err)
	}

	if state.BoundREQ != nil {
		if err := checkReachablePath(root, "bound_req", state.BoundREQ.Path); err != nil {
			return err
		}
		if err := checkReachableFingerprint(root, "bound_req",
			state.BoundREQ.Path, state.BoundREQ.SHA256); err != nil {
			return err
		}
	}

	for _, doc := range state.Documents {
		if err := checkReachablePath(root, fmt.Sprintf("documents[%s]", doc.ID), doc.Path); err != nil {
			return err
		}
		if doc.SHA256 != "" {
			if err := checkReachableFingerprint(root,
				fmt.Sprintf("documents[%s]", doc.ID), doc.Path, doc.SHA256); err != nil {
				return err
			}
		}
	}

	for _, agent := range state.Entities.Agents {
		for _, ref := range []struct {
			kind, path string
		}{
			{"definition_ref", agent.DefinitionRef},
			{"prompt_ref", agent.PromptRef},
			{"readback_ref", agent.ReadbackRef},
			{"activation_ref", agent.ActivationRef},
		} {
			if ref.path == "" {
				continue
			}
			if err := checkReachablePath(root,
				fmt.Sprintf("agents[%s].%s", agent.ID, ref.kind), ref.path); err != nil {
				return err
			}
		}
	}

	for _, task := range state.Entities.Tasks {
		if task.Path == "" {
			continue
		}
		if err := checkReachablePath(root,
			fmt.Sprintf("tasks[%s].path", task.ID), task.Path); err != nil {
			return err
		}
	}

	for _, bug := range state.Entities.Bugs {
		if bug.ReportPath == "" {
			continue
		}
		if err := checkReachablePath(root,
			fmt.Sprintf("bugs[%s].report_path", bug.ID), bug.ReportPath); err != nil {
			return err
		}
	}

	for _, team := range state.Entities.Teams {
		if team.ManifestRef == "" {
			continue
		}
		if err := checkReachablePath(root,
			fmt.Sprintf("teams[%s].manifest_ref", team.ID), team.ManifestRef); err != nil {
			return err
		}
	}
	for _, evidence := range state.Evidence {
		if err := checkReachablePath(root, fmt.Sprintf("evidence[%s]", evidence.ID), evidence.Path); err != nil {
			return err
		}
		if err := checkReachableFingerprint(root, fmt.Sprintf("evidence[%s]", evidence.ID), evidence.Path, evidence.SHA256); err != nil {
			return err
		}
	}

	return nil
}

func checkReachablePath(root, label, relative string) error {
	if relative == "" {
		return nil
	}
	clean := filepath.Clean(strings.SplitN(relative, "#", 2)[0])
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("runtime reachability: %s path %q escapes repository root", label, relative)
	}
	absolute := filepath.Join(root, clean)
	if _, err := os.Stat(absolute); err != nil {
		return fmt.Errorf("runtime reachability: %s path %q is not reachable: %w",
			label, relative, err)
	}
	return nil
}

func checkReachableFingerprint(root, label, relative, expected string) error {
	if relative == "" || expected == "" {
		return nil
	}
	clean := filepath.Clean(strings.SplitN(relative, "#", 2)[0])
	absolute := filepath.Join(root, clean)
	data, err := os.ReadFile(absolute)
	if err != nil {
		return fmt.Errorf("runtime reachability: %s fingerprint read %q: %w",
			label, relative, err)
	}
	actual := fmt.Sprintf("%x", sha256.Sum256(data))
	if actual != expected {
		return fmt.Errorf("runtime reachability: %s fingerprint mismatch at %q: runtime=%s actual=%s",
			label, relative, expected, actual)
	}
	return nil
}
