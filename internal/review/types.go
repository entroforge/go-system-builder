// Package review implements the S7 verification-round control plane:
// ReviewPlan registration, Canonical ReviewResult submission, immutable
// Findings, ObservationBatch sealing and machine CleanRound computation
// (L3-S7 §3, §9, §10). The package owns the state.review projection
// (plan pointer, claim dispositions, assignment consumption, batch pointer)
// and writes every mutation through the runtime's single-writer CAS.
package review

// Plan mirrors review-plan.schema.json — the single authority for one
// review round's coverage, Claims and Assignment DAG projection.
type Plan struct {
	SchemaVersion                 string           `json:"schema_version"`
	ReviewPlanID                  string           `json:"review_plan_id"`
	ReviewRound                   int              `json:"review_round"`
	BaselineGeneration            int              `json:"baseline_generation"`
	FrozenSubjects                []FrozenSubject  `json:"frozen_subjects"`
	CoverageInventory             []CoverageItem   `json:"coverage_inventory,omitempty"`
	E2EAssets                     []E2EAsset       `json:"e2e_assets,omitempty"`
	ChangeImpact                  *ChangeImpact    `json:"change_impact"`
	Claims                        []Claim          `json:"claims"`
	Assignments                   []PlanAssignment `json:"assignments"`
	E2ECoverageState              string           `json:"e2e_coverage_state"`
	VerificationArtifactWorkspace *string          `json:"verification_artifact_workspace"`
	DispatchCapacityPolicy        string           `json:"dispatch_capacity_policy"`
	CoverageJustification         *string          `json:"coverage_justification"`
	CreatedBy                     string           `json:"created_by"`
	CreatedAt                     string           `json:"created_at"`
}

// E2EAsset is one existing CASE/PATH asset that a regression_available round
// claims it can reuse. The file digest is checked at registration and submit.
// SelectorRef/RouteRef/Environment are the S7-7 (RC-07) executability
// fingerprint: a reusable asset must declare the selector surface it drives,
// the route/entry point it covers and the environment it was recorded on —
// not merely that a spec file mentions the CASE id.
type E2EAsset struct {
	AssetID     string `json:"asset_id"`
	CaseRef     string `json:"case_ref"`
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	SelectorRef string `json:"selector_ref,omitempty"`
	RouteRef    string `json:"route_ref,omitempty"`
	Environment string `json:"environment,omitempty"`
}

// FrozenSubject is one fingerprinted product/config/test/spec surface the
// round binds.
type FrozenSubject struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Kind   string `json:"kind,omitempty"`
}

// ChangeImpact carries the change-surface summary the Planner consumed. On a
// TR-012 re-entry, SourceRefs must include the current change_impact evidence
// id; RegisterPlan binds that artifact's changed_artifacts to frozen subjects.
type ChangeImpact struct {
	Summary    string   `json:"summary,omitempty"`
	SourceRefs []string `json:"source_refs,omitempty"`
}

// Claim is one auditable proposition a Reviewer must answer (L3-S7 §3.3).
type Claim struct {
	ClaimID          string   `json:"claim_id"`
	Lens             string   `json:"lens"`
	Target           string   `json:"target"`
	Assertion        string   `json:"assertion"`
	Oracle           string   `json:"oracle"`
	Method           string   `json:"method"`
	RequiredEvidence []string `json:"required_evidence,omitempty"`
	Applicability    string   `json:"applicability"`
	NARationale      string   `json:"na_rationale,omitempty"`
	// NAChecklistID is the explicit N/A checklist the claim's not_applicable
	// disposition was proven against (S7-9/RC-07). An e2e N/A claim without
	// one is a silent dimension drop, not a conclusion.
	NAChecklistID string   `json:"na_checklist_id,omitempty"`
	SourceRefs    []string `json:"source_refs"`
	FocusKey      string   `json:"focus_key,omitempty"`
	DependsOn     []string `json:"depends_on,omitempty"`
	ResourceLocks []string `json:"resource_locks,omitempty"`
}

// PlanAssignment is the Claim responsibility grouping inside the plan
// (L3-S7 §3.4). The dispatch artifact (team manifest) binds to it by id.
type PlanAssignment struct {
	AssignmentID       string   `json:"assignment_id"`
	Lens               string   `json:"lens"`
	ClaimIDs           []string `json:"claim_ids"`
	FocusKeys          []string `json:"focus_keys,omitempty"`
	NonOverlapBoundary string   `json:"non_overlap_boundary"`
	ExecutionWave      string   `json:"execution_wave"`
	ResourceLocks      []string `json:"resource_locks,omitempty"`
}

// Result mirrors review-result.schema.json — the Canonical ReviewResult.
type Result struct {
	SchemaVersion              string                `json:"schema_version"`
	ResultID                   string                `json:"result_id"`
	AssignmentID               string                `json:"assignment_id"`
	AssignmentRevision         int                   `json:"assignment_revision"`
	ReviewPlanID               string                `json:"review_plan_id"`
	ReviewRound                int                   `json:"review_round"`
	BaselineGeneration         int                   `json:"baseline_generation"`
	ProducerAgentID            string                `json:"producer_agent_id"`
	SubjectDigest              string                `json:"subject_digest"`
	VerificationArtifactDigest *string               `json:"verification_artifact_digest"`
	ClaimResults               []ClaimResult         `json:"claim_results"`
	BlockedClaims              []BlockedClaim        `json:"blocked_claims,omitempty"`
	Checks                     []ResultCheck         `json:"checks,omitempty"`
	Findings                   []Finding             `json:"findings,omitempty"`
	Deviations                 []string              `json:"deviations,omitempty"`
	SiteLost                   []SiteLostDeclaration `json:"site_lost,omitempty"`
	Verdict                    string                `json:"verdict"`
}

// BlockedClaim is the Reviewer-facing declaration the tool projects to
// disposition=blocked (blocked_by_confirmed_finding, L3-S7 §3.5/§5.2): a
// required Claim the Reviewer objectively cannot execute because a confirmed
// product Finding of this round breaks a build/start/entry/precondition. It
// is never a pass and never satisfies the repaired round's Claim.
type BlockedClaim struct {
	ClaimID             string             `json:"claim_id"`
	BlockingFindingIDs  []string           `json:"blocking_finding_ids"`
	FailedPrecondition  FailedPrecondition `json:"failed_precondition"`
	EvidenceRefs        []string           `json:"evidence_refs"`
	AfterRepairRequired bool               `json:"after_repair_required"`
}

// FailedPrecondition names which precondition the confirmed Finding breaks.
type FailedPrecondition struct {
	Kind   string `json:"kind"` // build | start | entry | precondition
	Detail string `json:"detail"`
}

// SiteLostDeclaration is the Reviewer's explicit statement that an ordinary
// Finding's encounter scene is unrecoverable (L3-S7 §9.1 step 12): submit
// then records an Assignment BLOCKER and stays in S7 instead of rejecting
// with a bare re-capture demand or faking readiness.
type SiteLostDeclaration struct {
	FindingID string `json:"finding_id"`
	Reason    string `json:"reason"`
}

// ClaimResult is the per-Claim conclusion. not_applicable is a plan
// disposition and never appears here.
type ClaimResult struct {
	ClaimID      string   `json:"claim_id"`
	Conclusion   string   `json:"conclusion"`
	Method       string   `json:"method,omitempty"`
	Observed     string   `json:"observed"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

// ResultCheck is one executed check attached to the result.
type ResultCheck struct {
	Name         string   `json:"name,omitempty"`
	Command      string   `json:"command"`
	Result       string   `json:"result"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

// Finding mirrors finding.schema.json (L3-S7 §3.6). Findings are immutable:
// supplements append, never rewrite.
type Finding struct {
	SchemaVersion   string       `json:"schema_version"`
	FindingID       string       `json:"finding_id"`
	ClaimID         string       `json:"claim_id"`
	Lens            string       `json:"lens"`
	Severity        string       `json:"severity"`
	Blocking        *bool        `json:"blocking,omitempty"`
	Expected        string       `json:"expected"`
	AuthorityRefs   []string     `json:"authority_refs"`
	Observed        string       `json:"observed"`
	ObservationMode string       `json:"observation_mode"`
	Encounter       Encounter    `json:"encounter"`
	Reproducibility string       `json:"reproducibility"`
	EvidenceRefs    []string     `json:"evidence_refs"`
	CorrelationRefs []string     `json:"correlation_refs,omitempty"`
	VisibleImpact   string       `json:"visible_impact,omitempty"`
	NegativeFacts   []string     `json:"negative_facts,omitempty"`
	OpenQuestions   []string     `json:"open_questions,omitempty"`
	Hypotheses      []Hypothesis `json:"hypotheses,omitempty"`
}

// Encounter is the real operation scene of one observation.
type Encounter struct {
	JourneySummary      string         `json:"journey_summary"`
	Entrypoint          string         `json:"entrypoint,omitempty"`
	ScenarioRef         string         `json:"scenario_ref,omitempty"`
	RuntimeContext      string         `json:"runtime_context,omitempty"`
	ActorContext        string         `json:"actor_context,omitempty"`
	InitialStateRef     string         `json:"initial_state_ref,omitempty"`
	Timeline            []TimelineStep `json:"timeline,omitempty"`
	LastGoodCheckpoint  string         `json:"last_good_checkpoint,omitempty"`
	WallAction          string         `json:"wall_action"`
	FirstBadCheckpoint  string         `json:"first_bad_checkpoint"`
	BlockedContinuation string         `json:"blocked_continuation,omitempty"`
	TerminalState       string         `json:"terminal_state,omitempty"`
	StateDeltaRefs      []string       `json:"state_delta_refs,omitempty"`
	SideEffects         []string       `json:"side_effects,omitempty"`
	AttemptVariants     []string       `json:"attempt_variants,omitempty"`
	CaptureGaps         []string       `json:"capture_gaps,omitempty"`
	CleanupState        string         `json:"cleanup_state,omitempty"`
	RequestSummary      string         `json:"request_summary,omitempty"`
	ResponseSummary     string         `json:"response_summary,omitempty"`
	Command             string         `json:"command,omitempty"`
	ExitCode            *int           `json:"exit_code,omitempty"`
	BeforeState         string         `json:"before_state,omitempty"`
	AfterState          string         `json:"after_state,omitempty"`
	InspectionEntry     string         `json:"inspection_entry,omitempty"`
	SymbolTrail         string         `json:"symbol_trail,omitempty"`
}

// TimelineStep is one material step of the real encounter.
type TimelineStep struct {
	Sequence           int      `json:"sequence"`
	Action             string   `json:"action"`
	Target             string   `json:"target,omitempty"`
	InputRef           string   `json:"input_ref,omitempty"`
	ObservedCheckpoint string   `json:"observed_checkpoint"`
	EvidenceRefs       []string `json:"evidence_refs,omitempty"`
}

// Hypothesis is an optional unverified lead; never a routing authority.
type Hypothesis struct {
	Statement string `json:"statement"`
	Status    string `json:"status"`
}

// Supplement mirrors finding-supplement.schema.json (L3-S7 §3.6, L3-S8 §2.2).
// The original finder (or a scheduler-authorized replacement) appends new
// observation/evidence/correlation refs to an immutable Finding — typically
// answering an S8 discriminator-bound follow-up observation. Supplements never
// rewrite the Finding, never re-do the base capture S7 owed, and never carry
// root cause or repair content.
type Supplement struct {
	SchemaVersion        string   `json:"schema_version"`
	SupplementID         string   `json:"supplement_id"`
	SupplementsFindingID string   `json:"supplements_finding_id"`
	Author               string   `json:"author"`
	NewObservation       string   `json:"new_observation"`
	EvidenceRefs         []string `json:"evidence_refs,omitempty"`
	CorrelationRefs      []string `json:"correlation_refs,omitempty"`
	Discriminator        string   `json:"discriminator,omitempty"`
	HypothesisID         string   `json:"hypothesis_id,omitempty"`
	CreatedAt            string   `json:"created_at"`
	Hash                 string   `json:"hash"`
}

// LensToResponsibility maps a plan lens to the producer responsibility
// vocabulary the gate registry already uses.
func LensToResponsibility(lens string) string {
	switch lens {
	case "delivery":
		return "Delivery Verifier"
	case "qa":
		return "QA"
	case "e2e":
		return "E2E Browser"
	}
	return ""
}

// LensToWorkgroupKind maps a plan lens to the team-manifest workgroup kind.
func LensToWorkgroupKind(lens string) string {
	switch lens {
	case "delivery":
		return "delivery_verifier"
	case "qa":
		return "qa"
	case "e2e":
		return "e2e_browser"
	}
	return ""
}
