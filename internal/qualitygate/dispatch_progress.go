package qualitygate

import (
	"path"
	"strings"

	"github.com/entroforge/go-system-builder/internal/builderbatch"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// BuilderProgress is shared by planned S6 gates, the board and dispatch checks.
// Only the TASK's current report and its owner's assignment can prove completion.
type BuilderProgress struct {
	State            workstate.Status
	Integrated       bool
	DeliveryRecorded bool
	Reason           string
	EvidenceID       string
	Canonical        bool
}

// dispatchCheckpoint is the subset of the Integrator checkpoint consumed by
// dispatch projections.  Keep the report binding and merge receipt together:
// a current Result cannot release a dependent task unless the receipt for the
// same integration is still valid on the bound development branch.
type dispatchCheckpoint = builderbatch.Checkpoint

func HasDispatchPlan(state map[string]any) bool {
	return builderbatch.HasDispatchPlan(state)
}
func PlannedBuilderProgress(input Input) map[string]BuilderProgress {
	result := map[string]BuilderProgress{}
	var legacy map[string]builderbatch.TaskProgress
	entities, _ := input.Snapshot.State["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	stage, _ := currentStatePhase(input.Snapshot.State)
	for _, raw := range tasks {
		task, _ := raw.(map[string]any)
		if task == nil {
			continue
		}
		id := stringValue(task["id"])
		state := stringValue(workstate.TaskActivity(task))
		if id == "" {
			continue
		}
		// Cancellation remains visible even after the task leaves S6 or its
		// former owner has been replaced. A live blocker or active Task is
		// counted below only when the current S6 Builder binding is established.
		if state == "cancelled" {
			result[id] = BuilderProgress{State: workstate.Cancelled, Reason: "TASK cancelled"}
			continue
		}
		if stage != "building" || state == "candidate" || state == "reviewed" || state == "locked" {
			continue
		}

		mode, canonical, modeErr := canonicalModeForTask(input, task)
		if canonical {
			if mode.HasRecord {
				switch registeredRoleKind(mode.Record.RoleFamily) {
				case builderOwnershipNone:
					continue
				case builderOwnershipUnresolved:
					result[id] = BuilderProgress{State: workstate.Active, Reason: "Builder owner identity unresolved", Canonical: true}
					continue
				}
			}
			if !mode.HasRecord {
				ownership := taskBuilderOwnership(input.Snapshot.State, task)
				if ownership == builderOwnershipNone {
					continue
				}
				if ownership == builderOwnershipUnresolved {
					result[id] = BuilderProgress{State: workstate.Active, Reason: "Builder owner identity unresolved", Canonical: true}
					continue
				}
			}
			result[id] = canonicalProgressForTask(input, task, mode, modeErr)
			continue
		}
		if legacy == nil {
			legacy = builderbatch.ProjectLegacyTasks(input.Root, input.Snapshot.State, input.Files)
		}
		if progress, tracked := legacy[id]; tracked {
			result[id] = BuilderProgress{
				State: progress.State, Integrated: progress.Integrated,
				DeliveryRecorded: progress.DeliveryRecorded, Reason: progress.Reason,
				EvidenceID: progress.EvidenceID,
			}
		}
	}
	return result
}

// checkpointMergeValid keeps existing quality-gate callers on the same
// checkpoint rules used by the shared legacy batch projection.
func checkpointMergeValid(input Input, cp dispatchCheckpoint) bool {
	return builderbatch.CheckpointMergeValid(input.Root, input.Snapshot.State, cp)
}

func boundDevelopmentBranch(state map[string]any) string {
	bound, _ := state["bound_req"].(map[string]any)
	workspace, _ := bound["workspace"].(map[string]any)
	return strings.TrimSpace(stringValue(workspace["dev_branch"]))
}

type builderOwnership uint8

const (
	builderOwnershipUnresolved builderOwnership = iota
	builderOwnershipNone
	builderOwnershipBuilder
)

var builderRoleFamilies = map[string]bool{
	"backend-builder":  true,
	"frontend-builder": true,
	"test-builder":     true,
}

var reviewerTeamRoles = map[string]string{
	"document_verifier": "document-verifier",
	"delivery_verifier": "delivery-verifier",
	"qa":                "qa",
	"e2e_browser":       "e2e-tester",
	"investigator":      "investigator",
}

func registeredRoleKind(role string) builderOwnership {
	if builderRoleFamilies[role] {
		return builderOwnershipBuilder
	}
	for _, reviewerRole := range reviewerTeamRoles {
		if role == reviewerRole {
			return builderOwnershipNone
		}
	}
	return builderOwnershipUnresolved
}

// taskBuilderOwnership recognizes only a live, bidirectionally registered
// Builder owner or a fully identified non-Builder workgroup. Missing or
// conflicting links remain active in the projection so they cannot release
// capacity or be mistaken for an unowned TASK.
func taskBuilderOwnership(state, task map[string]any) builderOwnership {
	taskID := stringValue(task["id"])
	owners := canonicalStringValues(task["owner_agent_ids"])
	if taskID == "" || len(owners) == 0 {
		return builderOwnershipUnresolved
	}
	result := builderOwnershipNone
	for _, owner := range owners {
		kind := registeredBuilderOwner(state, task, owner)
		if kind == builderOwnershipUnresolved {
			return builderOwnershipUnresolved
		}
		if kind == builderOwnershipBuilder {
			result = builderOwnershipBuilder
		}
	}
	return result
}

func registeredBuilderOwner(state, task map[string]any, owner string) builderOwnership {
	taskID := stringValue(task["id"])
	if owner == "" || taskID == "" || !containsString(canonicalStringValues(task["owner_agent_ids"]), owner) {
		return builderOwnershipUnresolved
	}
	agent, found, err := uniqueEntityByID(state, "agents", owner)
	if err != nil || !found || !containsString(canonicalStringValues(agent["task_ids"]), taskID) {
		return builderOwnershipUnresolved
	}
	teamID := stringValue(agent["team_id"])
	if teamID == "" {
		return builderOwnershipUnresolved
	}
	team, found, err := uniqueEntityByID(state, "teams", teamID)
	if err != nil || !found || !containsString(canonicalStringValues(team["agent_ids"]), owner) {
		return builderOwnershipUnresolved
	}
	teamStatus := stringValue(team["status"])
	if teamStatus != "planned" && teamStatus != "active" {
		return builderOwnershipUnresolved
	}
	manifestRef := stringValue(team["manifest_ref"])
	definitionRef := stringValue(agent["definition_ref"])
	promptRef := stringValue(agent["prompt_ref"])
	promptManifest, assignmentID, hasAssignment := strings.Cut(promptRef, "#")
	if manifestRef == "" || definitionRef == "" || !hasAssignment || promptManifest != manifestRef || assignmentID == "" || path.Base(assignmentID) != assignmentID {
		return builderOwnershipUnresolved
	}

	role := stringValue(agent["role"])
	kind := stringValue(team["kind"])
	if kind == "builder" && builderRoleFamilies[role] {
		return builderOwnershipBuilder
	}
	if reviewerRole, knownReviewer := reviewerTeamRoles[kind]; knownReviewer && role == reviewerRole {
		return builderOwnershipNone
	}
	return builderOwnershipUnresolved
}
