package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StageFor projects a machine lifecycle state + phase to the Main Spine
// S-cursor and a short dotted label. The mapping is the single source of
// truth shared by the CLI `status`/`next` projection and the Hook stage
// banner; docs/agent-protocol.md "#cursor-mapping" pins the contract.
//
// Planning phases are formal runtime state. The root argument remains in the
// signature for callers that also load Runtime from disk, but StageFor never
// scans artifacts to infer a cursor. Legacy artifact inference belongs to an
// explicit reconcile or migration path only.
//
// The formal planning mapping is design=S2, contracts=S3 and tasks=S4.
// Returns ("cross-stage", "cross-stage") for any combination the table does
// not recognise so callers can render a recovery hint rather than an empty
// string.
func StageFor(state, phase, root string) (cursor, label string) {
	switch state {
	case "inactive":
		return "S0", "inactive"
	case "planning":
		return planningStageFor(phase)
	case "document_verification":
		return "S5", "document_verification"
	case "building":
		return "S6", "building"
	case "verification":
		return "S7", "verification." + phase
	case "bug_resolution":
		switch phase {
		case "investigation", "bug_report_review":
			return "S8", "bug_resolution." + phase
		}
		return "S9", "bug_resolution." + phase
	case "acceptance", "release_audit":
		return "S10", state
	case "awaiting_human_release":
		return "S11", "human_release_gateway"
	case "release_authorized":
		return "S11", "release_authorized"
	case "aborted":
		return "aborted", "aborted"
	case "paused":
		return "paused", "paused"
	default:
		return "cross-stage", fmt.Sprintf("unknown:%s", state)
	}
}

// planningStageFor maps the formal planning phase directly to the Main Spine.
func planningStageFor(phase string) (cursor, label string) {
	switch phase {
	case "design":
		return "S2", "planning.design"
	case "contracts":
		return "S3", "planning.contracts"
	case "tasks":
		return "S4", "planning.tasks"
	default:
		return "cross-stage", "unknown:planning." + phase
	}
}

// LegacyPlanningPhaseForArtifacts is the pure mapping used by an explicit
// reconcile/migration path. It is intentionally not called by StageFor.
func LegacyPlanningPhaseForArtifacts(contractsExists, tasksExists bool) string {
	switch {
	case contractsExists && tasksExists:
		return "tasks"
	case contractsExists:
		return "contracts"
	default:
		return "design"
	}
}

// ReconcileLegacyPlanningPhase is the only planning artifact inspection seam.
// It supports generation-1 Runtime migration; normal projection remains
// authoritative on the committed lifecycle phase.
func ReconcileLegacyPlanningPhase(root string) (string, error) {
	contractsExists, err := legacyArtifactExists(root, "docs/contracts", "CONTRACTS-*.md")
	if err != nil {
		return "", err
	}
	tasksExists, err := legacyArtifactExists(root, "docs/tasks", "TASK-*.md")
	if err != nil {
		return "", err
	}
	return LegacyPlanningPhaseForArtifacts(contractsExists, tasksExists), nil
}

func legacyArtifactExists(root, dir, pattern string) (bool, error) {
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), "-template.md") {
			continue
		}
		matched, err := filepath.Match(pattern, entry.Name())
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}
