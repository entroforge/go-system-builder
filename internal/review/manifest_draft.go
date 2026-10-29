package review

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// ---------------------------------------------------------------------------
// Reviewer workgroup manifest draft (L3-S7 §13 audit known-friction: the
// reviewer team-manifest carries 20 required top-level fields and was
// handwritten). DraftManifest scaffolds a register-workgroup manifest for one
// ReviewPlan Assignment from control-plane facts; every field the control
// plane can prove is pre-filled, the rest carry explicit TODO(planner)
// markers. Read-only: nothing is written to the runtime.
// ---------------------------------------------------------------------------

// ManifestDraft mirrors team-manifest.schema.json. It is a scaffold, not a
// dispatch decision — the Planner replaces the TODO markers and registration
// validates the result normally.
type ManifestDraft struct {
	SchemaVersion              string                `json:"schema_version"`
	ManifestID                 string                `json:"manifest_id"`
	Version                    string                `json:"version"`
	RuntimeID                  string                `json:"runtime_id"`
	ReqID                      string                `json:"req_id"`
	BaselineGeneration         int                   `json:"baseline_generation"`
	ReviewRound                int                   `json:"review_round"`
	PlatformTeamID             string                `json:"platform_team_id"`
	WorkgroupID                string                `json:"workgroup_id"`
	WorkgroupKind              string                `json:"workgroup_kind"`
	Status                     string                `json:"status"`
	Documents                  []ManifestDocument    `json:"documents"`
	RiskTags                   []ManifestRiskTag     `json:"risk_tags"`
	ResponsibilityDispositions []ManifestDisposition `json:"responsibility_dispositions"`
	Assignments                []ManifestAssignment  `json:"assignments"`
	SeparationEdges            []ManifestSeparation  `json:"separation_edges"`
	PlannedAgentCount          int                   `json:"planned_agent_count"`
	MaxParallelAgents          int                   `json:"max_parallel_agents"`
	QuantityRationale          string                `json:"quantity_rationale"`
	Validation                 ManifestValidation    `json:"validation"`
}

// ManifestDocument is one fingerprinted document reference.
type ManifestDocument struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// ManifestRiskTag is a risk annotation; the draft emits none.
type ManifestRiskTag struct {
	Tag        string `json:"tag"`
	Source     string `json:"source"`
	Derivation string `json:"derivation"`
}

// ManifestDisposition is one responsibility routing row.
type ManifestDisposition struct {
	ResponsibilityID string   `json:"responsibility_id"`
	Disposition      string   `json:"disposition"`
	Trigger          string   `json:"trigger"`
	AssignmentIDs    []string `json:"assignment_ids"`
	NARationale      *string  `json:"na_rationale"`
	EvidenceRef      *string  `json:"evidence_ref"`
}

// ManifestAssignment is the single workgroup row binding the plan Assignment.
type ManifestAssignment struct {
	AssignmentID       string   `json:"assignment_id"`
	ResponsibilityID   string   `json:"responsibility_id"`
	ClaimIDs           []string `json:"claim_ids"`
	FocusKeys          []string `json:"focus_keys,omitempty"`
	NonOverlapBoundary string   `json:"non_overlap_boundary,omitempty"`
	RoleFamily         string   `json:"role_family"`
	Scope              []string `json:"scope"`
	AgentID            string   `json:"agent_id"`
	AgentDefinitionRef string   `json:"agent_definition_ref"`
	SkillRefs          []string `json:"skill_refs"`
	ReadPaths          []string `json:"read_paths"`
	WritePaths         []string `json:"write_paths"`
	OutputPaths        []string `json:"output_paths"`
	DependsOn          []string `json:"depends_on"`
	ReuseDecision      string   `json:"reuse_decision"`
	GroupingRationale  string   `json:"grouping_rationale"`
	DoneWhen           []string `json:"done_when"`
	Status             string   `json:"status"`
}

// ManifestSeparation is a logical-independence edge; a single-row workgroup
// needs none.
type ManifestSeparation struct {
	LeftAssignmentID  string `json:"left_assignment_id"`
	RightAssignmentID string `json:"right_assignment_id"`
	Reason            string `json:"reason"`
}

// ManifestValidation is the manifest self-check block.
type ManifestValidation struct {
	Result                  string   `json:"result"`
	MissingResponsibilities []string `json:"missing_responsibilities"`
	UnresolvedConflicts     []string `json:"unresolved_conflicts"`
	Warnings                []string `json:"warnings"`
	ValidatedAt             string   `json:"validated_at"`
}

// mandatoryResponsibilities mirrors internal/team/validator.go
// mandatoryByWorkgroup for the reviewer kinds (that package is the semantic
// authority; keep the two in sync when the reviewer baseline changes).
var mandatoryResponsibilities = map[string][]string{
	"delivery_verifier": {"VER-REQ-GAP", "VER-SPEC-GAP", "VER-MODULE-COMPLETE"},
	"qa":                {"QA-MODULE-CODE", "QA-REUSE-ABSTRACTION", "QA-UNIT-TEST", "QA-INTEGRATION-TEST"},
	"e2e_browser":       {"E2E-USER-FLOW", "E2E-CONSOLE-NETWORK"},
}

// lensToRoleFamily maps a plan lens to the team-manifest role family.
func lensToRoleFamily(lens string) string {
	switch lens {
	case "delivery":
		return "delivery-verifier"
	case "qa":
		return "qa"
	case "e2e":
		return "e2e-tester"
	}
	return ""
}

// roleFamilyDefinitionRef maps a reviewer role family to its agent definition
// (agents/<role>.md — the identity anchor, see docs/control/agent-protocol.md).
func roleFamilyDefinitionRef(roleFamily string) string {
	return "agents/" + roleFamily + ".md"
}

// assignmentDoneWhen turns the ReviewPlan's exact Claim set into a concrete
// closing contract for the dispatched Worker. The Planner may refine the
// wording, but the draft must make the required output and evidence boundary
// visible without asking the Worker to infer it from a role name.
func assignmentDoneWhen(kind, assignmentID string, claimIDs []string) []string {
	claims := strings.Join(claimIDs, ", ")
	result := fmt.Sprintf("fill and save the exact issued professional answer for %s for assigned Claims [%s], and return its exact reference for original-consumer reception; ending the native turn does not establish acceptance", assignmentID, claims)
	evidence := "for an ordinary complete result, each assigned Claim has a disposition and the required evidence is referenced"
	if kind == "e2e_browser" {
		evidence = "follow the original Plan's applicability decisions; for an ordinary complete result, execute assigned required behavior through its declared entry and method and reference their actual evidence, with every assigned Claim answered as pass/fail or a legitimate product-blocked Claim; unclear applicability goes to Main for original Plan recovery, not a new Claim conclusion"
	}
	return []string{result, evidence,
		"run every declared required check before reporting ordinary completion; for a real P0, stop dangerous work, preserve observed Claim conclusions and the actual P0 Finding with genuine capture gaps, and omit unanswered Claim rows; the original consumer validates the P0 and derives unobserved Claims",
		"if sources or the environment prevent a conclusion, preserve actual observations and return the original responsibility reference and obstacle to Main; leave the responsibility incomplete without inventing a verdict, product Finding or blocked Claim"}
}

// DraftManifest scaffolds a register-workgroup team manifest for one
// ReviewPlan Assignment (L3-S7 §8 dispatch path). Every fact the control
// plane proves is pre-filled: identity (runtime/REQ/baseline/round), the
// lens-derived workgroup kind and role family, the exact Claim set with
// focus keys and non-overlap boundary, the mandatory responsibility
// dispositions for the kind, the fingerprinted REQ/TASK documents, and the
// reviewer write surface. Fields no fact decides (agent identity, skill set
// beyond schema-forced minimums) carry TODO(planner) markers listed in the
// returned notes.
func DraftManifest(root string, state map[string]any, assignmentID string) (*ManifestDraft, []string, error) {
	plan, ptr, err := LoadPlan(root, state)
	if err != nil {
		return nil, nil, err
	}
	var planned *PlanAssignment
	for i := range plan.Assignments {
		if plan.Assignments[i].AssignmentID == assignmentID {
			planned = &plan.Assignments[i]
			break
		}
	}
	if planned == nil {
		known := make([]string, 0, len(plan.Assignments))
		for _, assignment := range plan.Assignments {
			known = append(known, assignment.AssignmentID)
		}
		sort.Strings(known)
		return nil, nil, fmt.Errorf("assignment %s is not part of ReviewPlan %s (known: %s)", assignmentID, ptr.PlanID, strings.Join(known, ", "))
	}
	kind := LensToWorkgroupKind(planned.Lens)
	if kind == "" {
		return nil, nil, fmt.Errorf("assignment %s has unsupported lens %q; reviewer workgroup kinds are delivery_verifier/qa/e2e_browser", assignmentID, planned.Lens)
	}

	responsibilities := mandatoryResponsibilities[kind]
	if planned.Lens == "e2e" && planned.BehaviorSurface == "non_browser" {
		responsibilities = []string{"E2E-BEHAVIOR"}
	}
	var notes []string
	// A plan Assignment dispatches exactly once; warn early when the runtime
	// projection shows it is already consumed.
	reviewMap, _ := state["review"].(map[string]any)
	if projection, _ := reviewMap["assignments"].(map[string]any); projection != nil {
		if row, _ := projection[assignmentID].(map[string]any); row != nil {
			if status := workstate.ReviewAssignmentActivity(row); status != "" && status != "planned" {
				return nil, nil, fmt.Errorf("assignment %s is already %s; one plan Assignment dispatches once", assignmentID, status)
			}
		}
	}
	if planned.ExecutionWave == "behavior" {
		note := "behavior-wave Assignment: register-workgroup rejects it until every required static Claim has a disposition (L3-S7 §5.2-5.3)"
		if remaining := RemainingStaticClaims(state, plan); remaining > 0 {
			note += fmt.Sprintf("; wave readiness: %d static-wave claim(s) still awaiting a disposition", remaining)
		}
		notes = append(notes, note)
	}

	runtimeID, _ := state["runtime_id"].(string)
	bound, _ := state["bound_req"].(map[string]any)
	reqID, _ := bound["id"].(string)
	round := ptr.ReviewRound
	if round == 0 {
		round = currentReviewRound(state)
	}
	generation := baselineGeneration(state)

	// Reuse a platform team only when registered facts name a single team.
	platformTeamID := ""
	platformTeams := map[string]bool{}
	entities, _ := state["entities"].(map[string]any)
	teams, _ := entities["teams"].([]any)
	workgroupTaken := map[string]bool{}
	for _, raw := range teams {
		team, _ := raw.(map[string]any)
		if id, _ := team["id"].(string); id != "" {
			workgroupTaken[id] = true
		}
		if id, _ := team["platform_team_id"].(string); id != "" {
			platformTeams[id] = true
			platformTeamID = id
		}
	}
	if len(platformTeams) != 1 {
		platformTeamID = fmt.Sprintf("s7-g%d-r%d-review", generation, round)
	}
	notes = append(notes, "platform_team_id is the prepared logical engineering group label; it does not create or identify a Claude Code platform Team")

	suffix := strings.TrimPrefix(assignmentID, "assignment-")
	workgroupID := "workgroup-" + suffix
	if workgroupTaken[workgroupID] {
		workgroupID = fmt.Sprintf("workgroup-%s-r%d", suffix, round)
	}
	notes = append(notes, "workgroup_id/manifest_id are prepared engineering identifiers for this original responsibility; Main does not need to invent or copy internal labels")

	// Fingerprinted documents: the bound REQ plus every current-generation
	// TASK the round freezes (the runtime verifies sha256 against disk).
	documents := []ManifestDocument{}
	if reqID != "" {
		documents = append(documents, ManifestDocument{
			ID:      reqID,
			Path:    stringField(bound["path"]),
			Version: stringField(bound["version"]),
			SHA256:  stringField(bound["sha256"]),
		})
	}
	for _, raw := range stateDocuments(state) {
		doc, _ := raw.(map[string]any)
		if doc == nil || doc["kind"] != "task" || intField(doc["generation"]) != generation {
			continue
		}
		id := stringField(doc["id"])
		if id == "" {
			continue
		}
		documents = append(documents, ManifestDocument{
			ID:      id,
			Path:    stringField(doc["path"]),
			Version: stringField(doc["version"]),
			SHA256:  stringField(doc["sha256"]),
		})
	}
	if len(documents) == 0 {
		return nil, nil, fmt.Errorf("S7 Main/system preparation gap: no original bound REQ or current-generation TASK document references; restore the original source registration, do not author IDs, versions or SHA values")
	}
	for _, document := range documents {
		if document.Path == "" || document.Version == "" || document.SHA256 == "" {
			return nil, nil, fmt.Errorf("S7 Main/system preparation gap: original document %s is missing its registered path, version or SHA; restore its original source registration, do not ask the author to supply machine metadata", document.ID)
		}
	}

	// The single manifest row binds the exact Claim set of the plan
	// Assignment; responsibility defaults to the first mandatory one for the
	// workgroup kind.
	responsibility := responsibilities[0]
	notes = append(notes, fmt.Sprintf("responsibility_id defaults to %s (first mandatory %s responsibility); all mandatory responsibilities are pre-assigned to this row — adjust the routing if the Planner splits duties", responsibility, kind))

	scope := []string{}
	seenScope := map[string]bool{}
	claims := make(map[string]Claim, len(plan.Claims))
	for _, claim := range plan.Claims {
		claims[claim.ClaimID] = claim
	}
	for _, claimID := range planned.ClaimIDs {
		target := claims[claimID].Target
		if target != "" && !seenScope[target] {
			seenScope[target] = true
			scope = append(scope, target)
		}
	}
	if len(scope) == 0 {
		scope = []string{"TODO(planner): review scope"}
	}

	notes = append(notes, `skill_refs contains the original required responsibility methods or role defaults; Main may add an actual additional professional method by its existing Skill name, not an object or digest`)
	skillRefs := agentcontext.RequiredSkills(responsibility)
	if len(skillRefs) == 0 {
		skillRefs = agentcontext.CoreSkills(lensToRoleFamily(planned.Lens))
	}

	readPaths := []string{}
	seenRead := map[string]bool{}
	for _, subject := range plan.FrozenSubjects {
		if subject.Path != "" && !seenRead[subject.Path] {
			seenRead[subject.Path] = true
			readPaths = append(readPaths, subject.Path)
		}
	}
	if len(readPaths) == 0 {
		readPaths = []string{"TODO(planner): read paths"}
	} else {
		notes = append(notes, "read_paths are the plan's frozen subjects; widen them to the spec/test surface the reviewer needs")
	}

	writePaths := []string{".claude/"}
	if plan.VerificationArtifactWorkspace != nil && strings.TrimSpace(*plan.VerificationArtifactWorkspace) != "" {
		writePaths = append(writePaths, *plan.VerificationArtifactWorkspace)
	}
	notes = append(notes, "write_paths describe the Reviewer questionnaire/evidence responsibility and the registered verification_artifact_workspace; they are not a tool ACL")

	agentID := fmt.Sprintf("s7-g%d-r%d-%s", generation, round, suffix)
	notes = append(notes, "agent_id is the prepared engineering owner label for this exact original Assignment, not an opaque platform agent_id; Main may select an actual replacement owner without changing the professional scope or independence requirements")

	roleFamily := lensToRoleFamily(planned.Lens)
	dispositions := make([]ManifestDisposition, 0, len(responsibilities))
	for _, mandatory := range responsibilities {
		dispositions = append(dispositions, ManifestDisposition{
			ResponsibilityID: mandatory,
			Disposition:      "assigned",
			Trigger:          fmt.Sprintf("mandatory %s baseline", kind),
			AssignmentIDs:    []string{assignmentID},
		})
	}

	row := ManifestAssignment{
		AssignmentID:       assignmentID,
		ResponsibilityID:   responsibility,
		ClaimIDs:           append([]string(nil), planned.ClaimIDs...),
		FocusKeys:          append([]string(nil), planned.FocusKeys...),
		NonOverlapBoundary: planned.NonOverlapBoundary,
		RoleFamily:         roleFamily,
		Scope:              scope,
		AgentID:            agentID,
		AgentDefinitionRef: roleFamilyDefinitionRef(roleFamily),
		SkillRefs:          skillRefs,
		ReadPaths:          readPaths,
		WritePaths:         writePaths,
		OutputPaths:        []string{},
		DependsOn:          []string{},
		ReuseDecision:      "create",
		GroupingRationale: fmt.Sprintf(
			"One plan Assignment = one reviewer Agent (L3-S7 §3.4): this row binds the exact Claim set of %s in ReviewPlan %s.",
			assignmentID, ptr.PlanID),
		DoneWhen: assignmentDoneWhen(kind, assignmentID, planned.ClaimIDs),
		Status:   "planned",
	}

	draft := &ManifestDraft{
		SchemaVersion:              "1.0.0",
		ManifestID:                 "team-manifest-" + suffix,
		Version:                    "v1.0.0",
		RuntimeID:                  runtimeID,
		ReqID:                      reqID,
		BaselineGeneration:         generation,
		ReviewRound:                round,
		PlatformTeamID:             platformTeamID,
		WorkgroupID:                workgroupID,
		WorkgroupKind:              kind,
		Status:                     "planned",
		Documents:                  documents,
		RiskTags:                   []ManifestRiskTag{},
		ResponsibilityDispositions: dispositions,
		Assignments:                []ManifestAssignment{row},
		SeparationEdges:            []ManifestSeparation{},
		PlannedAgentCount:          1,
		MaxParallelAgents:          1,
		QuantityRationale: fmt.Sprintf(
			"One reviewer Agent answers plan Assignment %s (%d Claims); one plan Assignment dispatches exactly once with its exact Claim set.",
			assignmentID, len(planned.ClaimIDs)),
		Validation: ManifestValidation{
			Result:                  "warn",
			MissingResponsibilities: []string{},
			UnresolvedConflicts:     []string{},
			Warnings:                []string{"Prepared from the registered ReviewPlan; the original register-workgroup validates actual responsibility and source consistency. This draft is not professional validation evidence."},
			ValidatedAt:             time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		},
	}
	notes = append(notes, "risk_tags left empty on purpose: adding a tag can force extra `assigned` responsibility dispositions at registration (internal/team validator)")
	notes = append(notes, "validation uses the existing pass|warn|block schema; warn and its timestamp describe this generated draft, not professional review or registration acceptance. The original register-workgroup validates the completed manifest; no manual validation tuple or timestamp is required.")
	return draft, notes, nil
}
