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
	Unknown          bool
	Integrated       bool
	DeliveryRecorded bool
	Reason           string
	EvidenceID       string
	Canonical        bool
}

func HasDispatchPlan(state map[string]any) bool {
	return builderbatch.HasDispatchPlan(state)
}
func PlannedBuilderProgress(input Input) map[string]BuilderProgress {
	result := map[string]BuilderProgress{}
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
					result[id] = BuilderProgress{Unknown: true, Reason: "Builder owner identity unresolved; Main must inspect the current responsibility", Canonical: true}
					continue
				}
			}
			if !mode.HasRecord {
				ownership := taskBuilderOwnership(input.Snapshot.State, task)
				if ownership == builderOwnershipNone {
					continue
				}
				if ownership == builderOwnershipUnresolved {
					result[id] = BuilderProgress{Unknown: true, Reason: "Builder owner identity unresolved; Main must inspect the current responsibility", Canonical: true}
					continue
				}
			}
			result[id] = canonicalProgressForTask(input, task, mode, modeErr)
			continue
		}
		// The original owner has not yet established a current S6 assignment.
		// Keep the task active; do not reconstruct a separate legacy batch.
		switch taskBuilderOwnership(input.Snapshot.State, task) {
		case builderOwnershipBuilder:
			result[id] = BuilderProgress{State: workstate.Active, Reason: "current Builder responsibility is not yet available"}
		case builderOwnershipUnresolved:
			result[id] = BuilderProgress{Unknown: true, Reason: "TASK owner is unknown; Main must inspect the current responsibility"}
		}
	}
	return result
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
		if kind == builderOwnershipBuilder {
			return builderOwnershipBuilder
		}
		if kind == builderOwnershipUnresolved {
			result = builderOwnershipUnresolved
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
	role := stringValue(agent["role"])
	kind := stringValue(team["kind"])
	// A known reviewer remains a reviewer when its S5 Team is retired.
	if reviewerRole, knownReviewer := reviewerTeamRoles[kind]; knownReviewer && role == reviewerRole {
		return builderOwnershipNone
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

	if kind == "builder" && builderRoleFamilies[role] {
		return builderOwnershipBuilder
	}
	return builderOwnershipUnresolved
}
