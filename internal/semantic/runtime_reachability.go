package semantic

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	SchemaVersion string `json:"schema_version"`
	RuntimeID     string `json:"runtime_id"`
	BoundREQ      *struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"bound_req"`
	Baseline struct {
		Generation int `json:"generation"`
	} `json:"baseline"`
	Lifecycle struct {
		State         string `json:"state"`
		PhaseRevision int    `json:"phase_revision"`
	} `json:"lifecycle"`
	Documents []struct {
		ID         string `json:"id"`
		Path       string `json:"path"`
		SHA256     string `json:"sha256"`
		Kind       string `json:"kind"`
		Generation int    `json:"generation"`
	} `json:"documents"`
	Entities struct {
		Agents []struct {
			ID                 string `json:"id"`
			Role               string `json:"role"`
			TeamID             string `json:"team_id"`
			State              string `json:"state"`
			LastLifecycleEvent string `json:"last_lifecycle_event"`
			DefinitionRef      string `json:"definition_ref"`
			PromptRef          string `json:"prompt_ref"`
			ReadbackRef        string `json:"readback_ref"`
			ActivationRef      string `json:"activation_ref"`
			ActivationRevision *int   `json:"activation_revision"`
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
			Kind        string `json:"kind"`
			Status      string `json:"status"`
			Generation  int    `json:"packet_generation"`
			StageEntry  int    `json:"packet_stage_entry"`
			ManifestRef string `json:"manifest_ref"`
		} `json:"teams"`
		WorkPackets []struct {
			TemplateID string `json:"template_id"`
			Binding    struct {
				RuntimeID      string `json:"runtime_id"`
				Generation     int    `json:"generation"`
				StageEntry     int    `json:"stage_entry"`
				Owner          string `json:"owner"`
				AssignmentID   string `json:"assignment_id"`
				AssignmentSHA  string `json:"assignment_sha256"`
				Responsibility string `json:"responsibility"`
			} `json:"binding"`
		} `json:"work_packets"`
	} `json:"entities"`
	Evidence []struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"evidence"`
}

type s5ManifestIdentity struct {
	RuntimeID   string `json:"runtime_id"`
	Generation  int    `json:"baseline_generation"`
	WorkgroupID string `json:"workgroup_id"`
	Kind        string `json:"workgroup_kind"`
	Assignments []struct {
		ID             string `json:"assignment_id"`
		Owner          string `json:"agent_id"`
		Responsibility string `json:"responsibility_id"`
	} `json:"assignments"`
}

type s5ManifestStatus struct {
	Bound      bool
	Historical bool
}

// s5ManifestStatuses uses the original packet's AssignmentSHA to prove the
// manifest bytes still match their issuance. A bound manifest is historical
// only after a generation change, S5 re-entry/rework, or retirement of its
// registered team; the latest S5 input remains strict through S6/S7 execution.
func (state runtimeReachability) s5ManifestStatuses(root string) (map[string]s5ManifestStatus, error) {
	statuses := make(map[string]s5ManifestStatus)
	latestS5Entry := -1
	for _, packet := range state.Entities.WorkPackets {
		binding := packet.Binding
		if packet.TemplateID == "s5-document-review-v1" && binding.RuntimeID == state.RuntimeID && binding.Generation == state.Baseline.Generation && binding.StageEntry > latestS5Entry {
			latestS5Entry = binding.StageEntry
		}
	}
	for _, team := range state.Entities.Teams {
		manifestRef := referencePath(team.ManifestRef)
		if (team.Kind != "document_verifier" && team.Kind != "document-verifier") || manifestRef == "" {
			continue
		}
		manifestRef = normalizeReachabilityReference(manifestRef)
		var identity *s5ManifestIdentity
		var manifestData []byte
		var bound bool
		batchGeneration, batchEntry := 0, 0
		for _, packet := range state.Entities.WorkPackets {
			if packet.TemplateID != "s5-document-review-v1" {
				continue
			}
			binding := packet.Binding
			ownerFound, ownerRole, ownerTeamID, ownerPrompt := false, "", "", ""
			for i := range state.Entities.Agents {
				if state.Entities.Agents[i].ID == binding.Owner {
					ownerFound = true
					ownerRole = state.Entities.Agents[i].Role
					ownerTeamID = state.Entities.Agents[i].TeamID
					ownerPrompt = state.Entities.Agents[i].PromptRef
					break
				}
			}
			if !ownerFound || ownerTeamID != team.ID {
				continue
			}
			if team.Generation != binding.Generation || team.StageEntry != binding.StageEntry {
				return nil, fmt.Errorf("S5 packet %s does not match its registered team entry", binding.AssignmentID)
			}
			if ownerRole != "document_verifier" && ownerRole != "document-verifier" {
				return nil, fmt.Errorf("S5 packet owner %s is not registered as a document reviewer", binding.Owner)
			}
			if ownerPrompt != team.ManifestRef+"#"+binding.AssignmentID {
				return nil, fmt.Errorf("S5 packet assignment %s does not match manifest prompt %s", binding.AssignmentID, ownerPrompt)
			}
			if identity == nil {
				var err error
				absolute, resolveErr := resolveReachablePath(root, "S5 manifest", manifestRef)
				if resolveErr != nil {
					return nil, resolveErr
				}
				manifestData, err = os.ReadFile(absolute)
				if err != nil {
					return nil, fmt.Errorf("read S5 manifest %s: %w", manifestRef, err)
				}
				var decoded s5ManifestIdentity
				if err := json.Unmarshal(manifestData, &decoded); err != nil {
					return nil, fmt.Errorf("decode S5 manifest %s: %w", manifestRef, err)
				}
				identity = &decoded
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(manifestData))
			if binding.AssignmentSHA == "" || binding.AssignmentSHA != digest {
				return nil, fmt.Errorf("S5 manifest %s bytes differ from the original packet binding", manifestRef)
			}
			if binding.RuntimeID != identity.RuntimeID || binding.RuntimeID != state.RuntimeID ||
				binding.Generation != identity.Generation || team.ID != identity.WorkgroupID ||
				identity.Kind != "document_verifier" {
				return nil, fmt.Errorf("S5 manifest %s identity differs from its original packet binding", manifestRef)
			}
			matches := 0
			for _, assignment := range identity.Assignments {
				if assignment.ID == binding.AssignmentID && assignment.Owner == binding.Owner && assignment.Responsibility == binding.Responsibility {
					matches++
				}
			}
			if matches != 1 {
				return nil, fmt.Errorf("S5 manifest %s does not contain the exact issued assignment %s", manifestRef, binding.AssignmentID)
			}
			if bound && (binding.Generation != batchGeneration || binding.StageEntry != batchEntry) {
				// A single registered team manifest cannot represent multiple
				// immutable S5 batches.
				return nil, fmt.Errorf("S5 manifest %s is bound to multiple Runtime entries", manifestRef)
			}
			if !bound {
				batchGeneration, batchEntry = binding.Generation, binding.StageEntry
			}
			bound = true
		}
		status := s5ManifestStatus{Bound: bound}
		if bound {
			if batchGeneration > state.Baseline.Generation ||
				(batchGeneration == state.Baseline.Generation && batchEntry > state.Lifecycle.PhaseRevision) {
				return nil, fmt.Errorf("S5 manifest %s is bound to a future Runtime entry", manifestRef)
			}
			status.Historical = batchGeneration < state.Baseline.Generation || team.Status == "retired" ||
				(batchGeneration == state.Baseline.Generation && batchEntry < latestS5Entry) ||
				(state.Lifecycle.State == "planning" && batchEntry <= state.Lifecycle.PhaseRevision) ||
				(state.Lifecycle.State == "document_verification" && batchEntry < state.Lifecycle.PhaseRevision)
		}
		statuses[manifestRef] = status
	}
	return statuses, nil
}

func normalizeReachabilityReference(ref string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(ref)))
}

func crossManifestDependencyRefs(refs []string, statuses map[string]s5ManifestStatus) []string {
	filtered := make([]string, 0, len(refs))
	for _, ref := range refs {
		status := statuses[normalizeReachabilityReference(referencePath(ref))]
		if status.Bound && status.Historical {
			continue
		}
		filtered = append(filtered, ref)
	}
	return filtered
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
	if _, err := state.s5ManifestStatuses(root); err != nil {
		return fmt.Errorf("runtime reachability: %w", err)
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
		// Superseded generations of every kind are immutable history: after
		// an in-place amend the old entry keeps its original fingerprint
		// (and the file may have been moved to versions/), so reachability
		// and fingerprint equality only apply to the current baseline's
		// documents (L3-S3 v4.0.1: promotion from the req-only exemption).
		if doc.Generation > 0 && doc.Generation < state.Baseline.Generation {
			continue
		}
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
	if _, err := resolveReachablePath(root, label, relative); err != nil {
		return err
	}
	return nil
}

func checkReachableFingerprint(root, label, relative, expected string) error {
	if relative == "" || expected == "" {
		return nil
	}
	absolute, err := resolveReachablePath(root, label, relative)
	if err != nil {
		return err
	}
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

// Legacy agent definition refs use the source-template agents/ directory,
// while installed projects keep those same files under .claude/agents/.
// Only that documented definition alias is eligible for fallback. All reads
// remain inside the real repository root, including after symlink resolution.
func resolveReachablePath(root, label, relative string) (string, error) {
	path := strings.ReplaceAll(strings.SplitN(relative, "#", 2)[0], `\`, "/")
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, ":") {
		return "", fmt.Errorf("runtime reachability: %s path %q escapes repository root", label, relative)
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("runtime reachability: %s path %q escapes repository root", label, relative)
	}
	actualRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	actualRoot, err = filepath.Abs(actualRoot)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.EvalSymlinks(filepath.Join(actualRoot, clean))
	if errors.Is(err, os.ErrNotExist) && strings.HasSuffix(label, ".definition_ref") && strings.HasPrefix(filepath.ToSlash(clean), "agents/") {
		absolute, err = filepath.EvalSymlinks(filepath.Join(actualRoot, ".claude", clean))
	}
	if err != nil {
		return "", fmt.Errorf("runtime reachability: %s path %q is not reachable: %w", label, relative, err)
	}
	rel, err := filepath.Rel(actualRoot, absolute)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("runtime reachability: %s path %q escapes repository root", label, relative)
	}
	return absolute, nil
}
