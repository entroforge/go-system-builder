package review

import (
	"fmt"
	"sort"
	"strings"
	"time"
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

// responsibilityRequiredSkills mirrors internal/team/validator.go
// responsibilitySkills for the responsibilities the draft can pick.
var responsibilityRequiredSkills = map[string][]string{
	"QA-UNIT-TEST":        {"testing-strategy"},
	"QA-INTEGRATION-TEST": {"testing-strategy"},
	"VER-INTEGRATION":     {"integration-verification"},
	"E2E-USER-FLOW":       {"e2e-browser-testing", "playwright-e2e"},
	"E2E-CONSOLE-NETWORK": {"e2e-browser-testing", "playwright-e2e"},
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
// (agents/<role>.md — the identity anchor, see docs/agent-protocol.md).
func roleFamilyDefinitionRef(roleFamily string) string {
	return "agents/" + roleFamily + ".md"
}

// assignmentDoneWhen turns the ReviewPlan's exact Claim set into a concrete
// closing contract for the dispatched Worker. The Planner may refine the
// wording, but the draft must make the required output and evidence boundary
// visible without asking the Worker to infer it from a role name.
func assignmentDoneWhen(kind, assignmentID string, claimIDs []string) []string {
	claims := strings.Join(claimIDs, ", ")
	result := fmt.Sprintf("register one complete ReviewResult for %s covering exactly Claims [%s]", assignmentID, claims)
	evidence := "each Claim has a disposition and the required evidence is referenced"
	if kind == "e2e_browser" {
		evidence = "each assigned user flow has executable evidence, or an explicit applicability and rationale"
	}
	return []string{result, evidence, "run every declared required check before reporting completion"}
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

	var notes []string
	// A plan Assignment dispatches exactly once; warn early when the runtime
	// projection shows it is already consumed.
	reviewMap, _ := state["review"].(map[string]any)
	if projection, _ := reviewMap["assignments"].(map[string]any); projection != nil {
		if row, _ := projection[assignmentID].(map[string]any); row != nil {
			if status, _ := row["status"].(string); status != "" && status != "planned" {
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

	// Identity: reuse the platform team id of an already-registered
	// workgroup when one exists; otherwise mirror the example convention.
	platformTeamID := ""
	entities, _ := state["entities"].(map[string]any)
	teams, _ := entities["teams"].([]any)
	workgroupTaken := map[string]bool{}
	for _, raw := range teams {
		team, _ := raw.(map[string]any)
		if id, _ := team["id"].(string); id != "" {
			workgroupTaken[id] = true
		}
		if platformTeamID == "" {
			platformTeamID, _ = team["platform_team_id"].(string)
		}
	}
	if platformTeamID == "" {
		platformTeamID = "claude-team-" + runtimeID
		notes = append(notes, "platform_team_id is guessed as claude-team-<runtime_id>; align it with the platform's real team id if one exists")
	}

	suffix := strings.TrimPrefix(assignmentID, "assignment-")
	workgroupID := "workgroup-" + suffix
	if workgroupTaken[workgroupID] {
		workgroupID = fmt.Sprintf("workgroup-%s-r%d", suffix, round)
	}
	notes = append(notes, "workgroup_id/manifest_id are suggestions; rename freely as long as the ^workgroup- / ^team-manifest- patterns hold")

	// Fingerprinted documents: the bound REQ plus every current-generation
	// TASK the round freezes (the runtime verifies sha256 against disk).
	generation := baselineGeneration(state)
	documents := []ManifestDocument{}
	if reqID != "" {
		documents = append(documents, ManifestDocument{
			ID:      reqID,
			Path:    stringField(bound["path"]),
			Version: documentVersion(bound["version"], reqID, &notes),
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
			Version: documentVersion(doc["version"], id, &notes),
			SHA256:  stringField(doc["sha256"]),
		})
	}
	if len(documents) == 0 {
		documents = append(documents, ManifestDocument{
			ID:      "TODO(planner): document id",
			Path:    "TODO(planner): document path",
			Version: "TODO(planner): document version",
			SHA256:  strings.Repeat("0", 64),
		})
		notes = append(notes, "no bound REQ or current-generation TASK documents found; the placeholder documents row MUST be replaced with real fingerprinted references")
	}

	// The single manifest row binds the exact Claim set of the plan
	// Assignment; responsibility defaults to the first mandatory one for the
	// workgroup kind.
	responsibility := mandatoryResponsibilities[kind][0]
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

	skillRefs := responsibilityRequiredSkills[responsibility]
	if len(skillRefs) == 0 {
		skillRefs = []string{fmt.Sprintf("TODO(planner): skill ref for %s", responsibility)}
	} else {
		skillRefs = append([]string(nil), skillRefs...)
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

	reportPath := fmt.Sprintf("docs/reports/review/%s.json", assignmentID)
	writePaths := []string{reportPath}
	if plan.VerificationArtifactWorkspace != nil && strings.TrimSpace(*plan.VerificationArtifactWorkspace) != "" {
		writePaths = append(writePaths, *plan.VerificationArtifactWorkspace)
	}
	notes = append(notes, "write_paths stay inside the reviewer write rule (.claude/, docs/reports/, the plan's verification_artifact_workspace); the runtime hard-denies product writes")

	agentID := fmt.Sprintf("TODO(planner):agent-id-for-%s", suffix)
	notes = append(notes, "agent_id is a TODO marker; name the reviewer Agent before registering")

	roleFamily := lensToRoleFamily(planned.Lens)
	dispositions := make([]ManifestDisposition, 0, len(mandatoryResponsibilities[kind]))
	for _, mandatory := range mandatoryResponsibilities[kind] {
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
		OutputPaths:        []string{reportPath},
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
			Result:                  "pass",
			MissingResponsibilities: []string{},
			UnresolvedConflicts:     []string{},
			Warnings:                []string{},
			ValidatedAt:             time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		},
	}
	notes = append(notes, "risk_tags left empty on purpose: adding a tag can force extra `assigned` responsibility dispositions at registration (internal/team validator)")
	// Disclosure for the validation five-tuple (E2E known-friction: the
	// team-manifest schema rejects empty arrays in some interpreter
	// flavours and the planner only learns the required shape on the
	// first register attempt). We pre-populate the tuple with `pass` plus
	// empty arrays and the current timestamp so the JSON validates; the
	// final values MUST be hand-filled before dispatch:
	//   result                  ∈ {"pass", "warnings", "fail"}
	//   missing_responsibilities array of responsibility_id strings
	//   unresolved_conflicts    array of conflict descriptions
	//   warnings                array of planner-visible warnings
	//   validated_at            RFC3339 timestamp of the hand review
	// (Shape authority: internal/schema/assets/team-manifest.schema.json.)
	notes = append(notes, "manifest `validation` (result/missing_responsibilities/unresolved_conflicts/warnings/validated_at) is pre-filled as pass/[]/[]/[]/<now>; hand-fill the tuple before dispatch — `result` accepts only pass|warnings|fail and the shape authority is the team-manifest schema (internal/schema/assets/)")
	return draft, notes, nil
}

// documentVersion returns the recorded version or a TODO marker.
func documentVersion(value any, id string, notes *[]string) string {
	if version, _ := value.(string); version != "" {
		return version
	}
	*notes = append(*notes, fmt.Sprintf("document %s has no recorded version in the runtime; a TODO marker is emitted", id))
	return "TODO(planner): document version"
}
