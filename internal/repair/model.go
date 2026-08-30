package repair

import "time"

// ArtifactRef is the immutable repository reference shared by all S9
// artifacts. ContractRef remains a distinct name at the API boundary because
// an approved Contract is the authority for the repair session.
type ArtifactRef struct {
	ID     string `json:"id,omitempty"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Status string `json:"status,omitempty"`
}

type ArtifactReference = ArtifactRef

type ContractRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type RepairUnit struct {
	ID            string   `json:"id"`
	Description   string   `json:"description"`
	Scope         []string `json:"scope,omitempty"`
	AssertionIDs  []string `json:"assertion_ids,omitempty"`
	DependsOn     []string `json:"depends_on,omitempty"`
	ResourceLocks []string `json:"resource_locks,omitempty"`
}

type ApprovedContract struct {
	ContractID             string
	CaseID                 string
	Revision               int
	Status                 string
	SourceFindingIDs       []string
	Units                  []RepairUnit
	ProspectiveScope       []string
	ForbiddenScope         []string
	CompatibilityMigration string
	Ref                    ContractRef
}

type SessionRequest struct {
	Contract           ContractRef
	SessionID          string
	RuntimeID          string
	ReqID              string
	BaselineGeneration int
	CreatedBy          string
	OccurredAt         time.Time
}

type RepairSession struct {
	SchemaVersion      string        `json:"schema_version"`
	RecordType         string        `json:"record_type"`
	SessionID          string        `json:"session_id"`
	ContractRef        string        `json:"contract_ref"`
	ContractSHA256     string        `json:"contract_sha256"`
	RuntimeID          string        `json:"runtime_id"`
	ReqID              string        `json:"req_id"`
	BaselineGeneration int           `json:"baseline_generation"`
	BaselineArtifacts  []ArtifactRef `json:"baseline_artifacts"`
	// RC-15 (S9-H7/T2 shadow-field convergence): BaselineDigest and Status are
	// shadow projections. The Runtime pointer under state.review.repair is the
	// authority for the session's live status and the implementation baseline
	// digest written by CommitRepairHandoff; these artifact fields are
	// captured at session creation and are NOT consulted by any commit gate.
	BaselineDigest string    `json:"baseline_digest"`
	Status         string    `json:"status"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"-"`
	CreatedAtText  string    `json:"created_at"`
}

type PlanRequest struct {
	Contract   ContractRef
	Session    ArtifactRef
	PlanID     string
	CreatedBy  string
	OccurredAt time.Time
}

type RepairPlan struct {
	SchemaVersion    string             `json:"schema_version"`
	RecordType       string             `json:"record_type"`
	PlanID           string             `json:"plan_id"`
	SessionID        string             `json:"session_id"`
	ContractID       string             `json:"contract_id"`
	ContractRef      string             `json:"contract_ref"`
	ContractSHA256   string             `json:"contract_sha256"`
	Units            []RepairUnit       `json:"units"`
	Assignments      []RepairAssignment `json:"assignments"`
	ExecutionPolicy  string             `json:"execution_policy"`
	ProspectiveScope []string           `json:"prospective_scope"`
	ForbiddenScope   []string           `json:"forbidden_scope"`
	CreatedBy        string             `json:"created_by"`
	CreatedAt        string             `json:"created_at"`
}

// RepairAssignment is the smallest dispatchable S9 work item. A unit has one
// owner, one Contract binding and one explicit execution state; orchestration
// must not infer these facts from a free-form agent message.
type RepairAssignment struct {
	AssignmentID  string   `json:"assignment_id"`
	UnitIDs       []string `json:"unit_ids"`
	AssertionIDs  []string `json:"assertion_ids,omitempty"`
	DependsOn     []string `json:"depends_on,omitempty"`
	ResourceLocks []string `json:"resource_locks,omitempty"`
	OwnerAgentID  string   `json:"owner_agent_id,omitempty"`
	Status        string   `json:"status"`
	Scope         []string `json:"scope"`
	ContractRef   string   `json:"contract_ref"`
}

// PlanReport is the pre-execution checkpoint. It records the builder's plan
// and the red checks that prove the repair has a failing pre-fix signal before
// any implementation write is accepted.
type PlanReport struct {
	SchemaVersion string        `json:"schema_version"`
	RecordType    string        `json:"record_type"`
	ReportID      string        `json:"report_id"`
	SessionID     string        `json:"session_id"`
	PlanID        string        `json:"plan_id"`
	AssignmentID  string        `json:"assignment_id"`
	AssertionIDs  []string      `json:"assertion_ids"`
	AgentID       string        `json:"agent_id"`
	Plan          string        `json:"plan"`
	RedChecks     []RepairCheck `json:"red_checks"`
	ProposedPaths []string      `json:"proposed_paths"`
	Status        string        `json:"status"`
	ReportedAt    string        `json:"reported_at"`
}

type RepairCheck struct {
	Name         string   `json:"name"`
	Command      string   `json:"command"`
	Result       string   `json:"result"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type ChangedArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Status string `json:"status"`
}

type RepairUnitResult struct {
	UnitID       string   `json:"unit_id"`
	Status       string   `json:"status"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type RepairResultRequest struct {
	Contract         ContractRef        `json:"contract"`
	Session          ArtifactRef        `json:"session"`
	Plan             ArtifactRef        `json:"plan"`
	ResultID         string             `json:"result_id"`
	ProducerAgentID  string             `json:"producer_agent_id"`
	UnitResults      []RepairUnitResult `json:"unit_results"`
	ChangedArtifacts []ChangedArtifact  `json:"changed_artifacts"`
	Result           string             `json:"result"`
	AssignmentID     string             `json:"assignment_id"`
	PlanReport       ArtifactRef        `json:"plan_report"`
	BeforeFixChecks  []RepairCheck      `json:"before_fix_checks"`
	Checks           []RepairCheck      `json:"checks"`
	ScopeDeviations  []string           `json:"scope_deviations"`
	MigrationRef     string             `json:"migration_ref"`
	RollbackRef      string             `json:"rollback_ref"`
	ResidualRisks    []string           `json:"residual_risks"`
	OccurredAt       time.Time          `json:"occurred_at"`
}

type RepairResult struct {
	SchemaVersion      string             `json:"schema_version"`
	RecordType         string             `json:"record_type"`
	ResultID           string             `json:"result_id"`
	SessionID          string             `json:"session_id"`
	PlanID             string             `json:"plan_id"`
	ContractID         string             `json:"contract_id"`
	BaselineGeneration int                `json:"baseline_generation"`
	ProducerAgentID    string             `json:"producer_agent_id"`
	AssignmentID       string             `json:"assignment_id"`
	PlanReportRef      *ArtifactRef       `json:"plan_report_ref,omitempty"`
	BeforeFixChecks    []RepairCheck      `json:"before_fix_checks"`
	Checks             []RepairCheck      `json:"checks"`
	UnitResults        []RepairUnitResult `json:"unit_results"`
	ChangedArtifacts   []ChangedArtifact  `json:"changed_artifacts"`
	ScopeDeviations    []string           `json:"scope_deviations"`
	MigrationRef       string             `json:"migration_ref,omitempty"`
	RollbackRef        string             `json:"rollback_ref,omitempty"`
	ResidualRisks      []string           `json:"residual_risks"`
	Result             string             `json:"result"`
	SubmittedAt        string             `json:"submitted_at"`
}

type ChangesetRequest struct {
	SessionID     string
	BaseRef       string
	HeadRef       string
	ExplicitPaths []string
	OccurredAt    time.Time
}

type Changeset struct {
	SchemaVersion string        `json:"schema_version"`
	RecordType    string        `json:"record_type"`
	ChangesetID   string        `json:"changeset_id"`
	SessionID     string        `json:"session_id"`
	Source        string        `json:"source"`
	BaseRef       string        `json:"base_ref,omitempty"`
	HeadRef       string        `json:"head_ref,omitempty"`
	Artifacts     []ArtifactRef `json:"artifacts"`
	Digest        string        `json:"digest"`
	ComputedAt    string        `json:"computed_at"`
}

type ImpactDecision struct {
	SourceID         string   `json:"source_id"`
	TargetID         string   `json:"target_id"`
	Relation         string   `json:"relation"`
	RuleID           string   `json:"rule_id"`
	Decision         string   `json:"decision"`
	ResponsibilityID *string  `json:"responsibility_id"`
	Scope            []string `json:"scope"`
	Rationale        string   `json:"rationale"`
	// RC-15 (S9-H7/T2 shadow-field convergence): RecoveryEvidence is an
	// audit-only declaration; the consumed evidence gate is the Runtime
	// evidence index validated in CommitChangeImpact / CommitTargetedReverification.
	RecoveryEvidence []string `json:"recovery_evidence"`
}

type ChangeImpactRequest struct {
	ImpactID                  string           `json:"impact_id"`
	RuntimeID                 string           `json:"runtime_id"`
	ReqID                     string           `json:"req_id"`
	BaselineGeneration        int              `json:"baseline_generation"`
	SourceBugIDs              []string         `json:"source_bug_ids"`
	SourceCaseIDs             []string         `json:"source_case_ids"`
	ChangeTypes               []string         `json:"change_types"`
	ChangedArtifacts          []ArtifactRef    `json:"changed_artifacts"`
	Decisions                 []ImpactDecision `json:"decisions"`
	EscalationLevel           string           `json:"escalation_level"`
	InvalidatedEvidenceIDs    []string         `json:"invalidated_evidence_ids"`
	SupersededEvidenceIDs     []string         `json:"superseded_evidence_ids"`
	RetainedEvidenceIDs       []string         `json:"retained_evidence_ids"`
	RequiredReverificationIDs []string         `json:"required_reverification_ids"`
	AnalyzedBy                string           `json:"analyzed_by"`
	AnalyzedAt                time.Time        `json:"analyzed_at"`
	OccurredAt                time.Time        `json:"occurred_at"`
}

type ChangeImpact struct {
	SchemaVersion          string           `json:"schema_version"`
	RecordType             string           `json:"record_type"`
	ImpactID               string           `json:"impact_id"`
	RuntimeID              string           `json:"runtime_id"`
	ReqID                  string           `json:"req_id"`
	BaselineGeneration     int              `json:"baseline_generation"`
	SourceBugIDs           []string         `json:"source_bug_ids"`
	SourceCaseIDs          []string         `json:"source_case_ids,omitempty"`
	ChangeTypes            []string         `json:"change_types"`
	ChangedArtifacts       []ArtifactRef    `json:"changed_artifacts"`
	Decisions              []ImpactDecision `json:"decisions"`
	EscalationLevel        string           `json:"escalation_level"`
	InvalidatedEvidenceIDs []string         `json:"invalidated_evidence_ids"`
	SupersededEvidenceIDs  []string         `json:"superseded_evidence_ids"`
	// RC-15 (S9-H7/T2 shadow-field convergence): RetainedEvidenceIDs and the
	// per-decision RecoveryEvidence entries are shadow declarations recorded
	// at impact creation. The consumed gates are the Runtime evidence index
	// (invalidate/supersede applied in CommitChangeImpact) and
	// required_reverification_ids; these lists are audit-only and no commit
	// gate reads them.
	RetainedEvidenceIDs       []string `json:"retained_evidence_ids"`
	RequiredReverificationIDs []string `json:"required_reverification_ids"`
	AnalyzedBy                string   `json:"analyzed_by"`
	AnalyzedAt                string   `json:"analyzed_at"`
}

type AssertionResult struct {
	AssertionID  string   `json:"assertion_id"`
	Result       string   `json:"result"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type TargetedReverificationRequest struct {
	ReverificationID       string            `json:"reverification_id"`
	RuntimeID              string            `json:"runtime_id"`
	BugID                  string            `json:"bug_id,omitempty"`
	CaseID                 string            `json:"case_id,omitempty"`
	BaselineGeneration     int               `json:"baseline_generation"`
	OriginalAssignmentID   string            `json:"original_assignment_id"`
	PerformingAssignmentID string            `json:"performing_assignment_id"`
	ContinuityReason       string            `json:"continuity_reason"`
	ImpactID               string            `json:"impact_id"`
	AssertionResults       []AssertionResult `json:"assertion_results"`
	ScopeCompliance        string            `json:"scope_compliance"`
	Result                 string            `json:"result"`
	FailureClass           string            `json:"failure_class"`
	PerformedAt            time.Time         `json:"performed_at"`
}

type TargetedReverification struct {
	SchemaVersion          string            `json:"schema_version"`
	RecordType             string            `json:"record_type"`
	ReverificationID       string            `json:"reverification_id"`
	RuntimeID              string            `json:"runtime_id"`
	BugID                  string            `json:"bug_id,omitempty"`
	CaseID                 string            `json:"case_id,omitempty"`
	BaselineGeneration     int               `json:"baseline_generation"`
	OriginalAssignmentID   string            `json:"original_assignment_id"`
	PerformingAssignmentID string            `json:"performing_assignment_id"`
	ContinuityReason       string            `json:"continuity_reason"`
	ImpactID               string            `json:"impact_id"`
	AssertionResults       []AssertionResult `json:"assertion_results"`
	ScopeCompliance        string            `json:"scope_compliance"`
	Result                 string            `json:"result"`
	FailureClass           string            `json:"failure_class,omitempty"`
	PerformedAt            string            `json:"performed_at"`
}

type HandoffRequest struct {
	HandoffID               string        `json:"handoff_id"`
	Session                 ArtifactRef   `json:"session"`
	Plan                    ArtifactRef   `json:"plan"`
	Contract                ContractRef   `json:"contract"`
	Result                  ArtifactRef   `json:"result"`
	Changeset               ArtifactRef   `json:"changeset"`
	ChangeImpact            ArtifactRef   `json:"change_impact"`
	TargetedReverifications []ArtifactRef `json:"targeted_reverifications"`
	HandedOffBy             string        `json:"handed_off_by"`
	NextAction              string        `json:"next_action"`
	OccurredAt              time.Time     `json:"occurred_at"`
}

type RepairHandoff struct {
	SchemaVersion              string        `json:"schema_version"`
	RecordType                 string        `json:"record_type"`
	HandoffID                  string        `json:"handoff_id"`
	SessionRef                 ArtifactRef   `json:"session_ref"`
	PlanRef                    ArtifactRef   `json:"plan_ref"`
	ContractRef                ArtifactRef   `json:"contract_ref"`
	ResultRef                  ArtifactRef   `json:"result_ref"`
	ChangesetRef               ArtifactRef   `json:"changeset_ref"`
	ChangeImpactRef            ArtifactRef   `json:"change_impact_ref"`
	TargetedReverificationRefs []ArtifactRef `json:"targeted_reverification_refs"`
	NextAction                 string        `json:"next_action"`
	HandedOffBy                string        `json:"handed_off_by"`
	HandedOffAt                string        `json:"handed_off_at"`
}

type HandoffCompleteness struct {
	Complete bool     `json:"complete"`
	Missing  []string `json:"missing,omitempty"`
	Invalid  []string `json:"invalid,omitempty"`
}
