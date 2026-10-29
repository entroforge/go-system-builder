package assignmentrecord

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/identity"
)

const (
	// AssignmentRecordContract is the explicit manifest opt-in for the S6
	// canonical Assignment Record. Runtime or manifest schema versions do not
	// enable this contract.
	AssignmentRecordContract = "s6-v1.2"
	NativeDeliveryContract   = "s6-native-v1"
	AssignmentRecordSchema   = "1.0.0"
	ClosingContractVersion   = "s6-closing-v1"
)

var (
	recordAssignmentIDRE = regexp.MustCompile(`^assignment-[A-Za-z0-9._-]+$`)
	recordTaskIDRE       = regexp.MustCompile(`^TASK-[A-Za-z0-9._-]+$`)
	recordRuntimeIDRE    = regexp.MustCompile(`^loop-[A-Za-z0-9._-]+$`)
	recordSHA256RE       = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// ContextSource identifies bytes that were an original source for this
// Assignment's frozen context. Ref may be a repository path or a fragment of
// one (for example a CheckProfile embedded in a manifest). It carries no
// permission and does not represent a Worker-authored fact.
type ContextSource struct {
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	ID      string `json:"id,omitempty"`
	Version string `json:"version,omitempty"`
	SHA256  string `json:"sha256"`
}

// SourceSnapshot identifies immutable evidence bytes captured from one
// original Assignment source. The source metadata remains distinct from the
// snapshot file path: this row does not grant authority or replace the source
// reference used by consumers.
type SourceSnapshot struct {
	SourceRef    string `json:"source_ref"`
	SourceKind   string `json:"source_kind"`
	SourceSHA256 string `json:"source_sha256"`
	Path         string `json:"path"`
	SHA256       string `json:"sha256"`
}

// RevisionIssuance binds a revised Record to the intent and exact prior
// Record it replaces. The Runtime Writer, never a caller-authored Record,
// supplies this receipt when issuing r2+.
type RevisionIssuance struct {
	IntentKey            string          `json:"intent_key"`
	RequestDigest        string          `json:"request_digest"`
	PreviousRecord       RecordReference `json:"previous_record"`
	PreviousRecordSHA256 string          `json:"previous_record_sha256"`
}

// Record is the immutable S6 dispatch charter for one Assignment revision.
// Professional plan and Result answers intentionally do not appear here.
// Their accepted source references can be projected by the original Runtime
// receipt flow without putting authored answers into this authority record.
type Record struct {
	SchemaVersion        string              `json:"schema_version"`
	Contract             string              `json:"contract"`
	RuntimeID            string              `json:"runtime_id"`
	BaselineGeneration   int                 `json:"baseline_generation"`
	RegistrationRevision int                 `json:"registration_revision"`
	AssignmentID         string              `json:"assignment_id"`
	Revision             int                 `json:"assignment_revision"`
	Task                 ContextSource       `json:"task"`
	Manifest             ContextSource       `json:"manifest"`
	AgentDefinition      ContextSource       `json:"agent_definition"`
	TeamID               string              `json:"team_id"`
	ResponsibilityID     string              `json:"responsibility_id"`
	OwnerAgentID         string              `json:"owner_agent_id"`
	RoleFamily           string              `json:"role_family"`
	DispatchMode         string              `json:"dispatch_mode"`
	ReadPaths            []string            `json:"read_paths"`
	SkillRefs            []string            `json:"skill_refs"`
	DependsOn            []string            `json:"depends_on"`
	DoneWhen             []string            `json:"done_when"`
	RequiredChecks       []string            `json:"required_checks"`
	CheckProfiles        []checkexec.Profile `json:"check_profiles"`
	IntegrationCheckMode string              `json:"integration_check_mode,omitempty"`
	ClosingContract      *ClosingContract    `json:"closing_contract,omitempty"`
	ScopeBasis           ScopeBasis          `json:"scope_basis"`
	EffectiveWritePaths  []string            `json:"effective_write_paths"`
	ContextSources       []ContextSource     `json:"context_sources"`
	SourceSnapshots      []SourceSnapshot    `json:"source_snapshots,omitempty"`
	RevisionIssuance     *RevisionIssuance   `json:"revision_issuance,omitempty"`
}

// ClosingContract freezes Main's explicit definition of the work the
// original owner must close. A nil pointer means the dispatch did not provide
// this contract; a non-nil contract with an empty WorkerChecks array means Main
// explicitly assigned no Worker runner checks.
type ClosingContract struct {
	Contract     string        `json:"contract"`
	WorkerChecks []WorkerCheck `json:"worker_checks"`
}

// WorkerCheck identifies one owner-run check and its independent Worker
// profile. The index is zero-based and contiguous within this frozen contract;
// Command and Profile repeat the executable identity so consumers can compare
// the declared check with the validated runner profile without borrowing Main
// RequiredChecks.
type WorkerCheck struct {
	Index   int               `json:"index"`
	Command string            `json:"command"`
	Profile checkexec.Profile `json:"profile"`
}

// ScopeBasis retains the original machine-readable scope inputs needed to
// recompute EffectiveWritePaths. Scope is a cap, manifest write/output paths
// are the Assignment declaration, and TaskWritePaths is the approved TASK
// boundary supplied by the caller from the current reviewed dispatch plan.
type ScopeBasis struct {
	ManifestScope       []string `json:"manifest_scope"`
	ManifestWritePaths  []string `json:"manifest_write_paths"`
	ManifestOutputPaths []string `json:"manifest_output_paths"`
	TaskWritePaths      []string `json:"task_write_paths"`
}

// RecordReference is the stable identity used by native and workspace
// projections to point back to the one Runtime entity. The content digest is
// separate so projections can bind both identity and exact bytes.
type RecordReference struct {
	RuntimeID          string `json:"runtime_id"`
	BaselineGeneration int    `json:"baseline_generation"`
	AssignmentID       string `json:"assignment_id"`
	Revision           int    `json:"assignment_revision"`
}

func (r Record) Reference() RecordReference {
	return RecordReference{
		RuntimeID:          r.RuntimeID,
		BaselineGeneration: r.BaselineGeneration,
		AssignmentID:       r.AssignmentID,
		Revision:           r.Revision,
	}
}

// Digest returns the SHA-256 of the stable JSON representation of a validated
// Record. BuildRecord sorts every set-valued field before returning.
func (r Record) Digest() (string, error) {
	if err := ValidateRecord(r); err != nil {
		return "", err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// ManifestAssignmentRecordEnabled reads only the explicit contract marker.
// Missing marker means legacy. Unknown non-empty markers fail closed. Neither
// the Runtime schema version nor this manifest's schema_version is consulted.
func ManifestAssignmentRecordEnabled(manifestBytes []byte) (bool, error) {
	var marker struct {
		Contract string `json:"assignment_record_contract"`
	}
	if err := json.Unmarshal(manifestBytes, &marker); err != nil {
		return false, fmt.Errorf("decode assignment record opt-in: %w", err)
	}
	switch marker.Contract {
	case "":
		return false, nil
	case AssignmentRecordContract, NativeDeliveryContract:
		return true, nil
	default:
		return false, fmt.Errorf("unsupported assignment_record_contract %q", marker.Contract)
	}
}

// recordManifest is a typed projection of source fields already defined by
// team-manifest.schema.json. It intentionally does not define an integration
// API or add another manifest authority; callers must pass the exact bytes
// already schema- and semantically-validated by Register.
type recordManifest struct {
	Contract           string                     `json:"assignment_record_contract"`
	ManifestID         string                     `json:"manifest_id"`
	Version            string                     `json:"version"`
	RuntimeID          string                     `json:"runtime_id"`
	BaselineGeneration int                        `json:"baseline_generation"`
	WorkgroupID        string                     `json:"workgroup_id"`
	WorkgroupKind      string                     `json:"workgroup_kind"`
	Documents          []recordManifestDocument   `json:"documents"`
	Assignments        []recordManifestAssignment `json:"assignments"`
}

// recordManifestDocument is the exact reference tuple consumed by the
// existing Team manifest. Keeping this low-level projection local avoids a
// dependency on team (which itself depends on agentcontext, a future Record
// consumer).
type recordManifestDocument struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type recordManifestAssignment struct {
	AssignmentID         string              `json:"assignment_id"`
	ResponsibilityID     string              `json:"responsibility_id"`
	RoleFamily           string              `json:"role_family"`
	AgentID              string              `json:"agent_id"`
	AgentDefinitionRef   string              `json:"agent_definition_ref"`
	SkillRefs            []string            `json:"skill_refs"`
	ReadPaths            []string            `json:"read_paths"`
	Scope                []string            `json:"scope"`
	WritePaths           []string            `json:"write_paths"`
	OutputPaths          []string            `json:"output_paths"`
	DependsOn            []string            `json:"depends_on"`
	RequiredChecks       []string            `json:"required_checks"`
	DoneWhen             []string            `json:"done_when"`
	DispatchMode         string              `json:"dispatch_mode"`
	CheckProfiles        []checkexec.Profile `json:"check_profiles"`
	IntegrationCheckMode string              `json:"integration_check_mode"`
	ClosingContract      *ClosingContract    `json:"closing_contract"`
}

// BuildRecord constructs the first canonical revision from the already
// validated manifest bytes, current Runtime state and exact TASK bytes. The
// caller supplies TASK write paths from the original reviewed dispatch plan
// and resolved role/Skill context sources from their existing catalogs.
// BuildRecord performs no I/O, creates no activation, and accepts no authored
// plan or Result content.
func BuildRecord(
	manifestBytes []byte,
	manifestRef string,
	runtimeState map[string]any,
	taskBytes []byte,
	taskID string,
	assignmentID string,
	taskWritePaths []string,
	contextSources []ContextSource,
) (Record, error) {
	enabled, err := ManifestAssignmentRecordEnabled(manifestBytes)
	if err != nil {
		return Record{}, err
	}
	if !enabled {
		return Record{}, fmt.Errorf("manifest does not explicitly opt into %s", AssignmentRecordContract)
	}
	var source recordManifest
	if err := json.Unmarshal(manifestBytes, &source); err != nil {
		return Record{}, fmt.Errorf("decode validated team manifest: %w", err)
	}
	if source.Contract != AssignmentRecordContract && source.Contract != NativeDeliveryContract {
		return Record{}, fmt.Errorf("manifest assignment record contract changed during decode")
	}
	if source.WorkgroupKind != "builder" {
		return Record{}, fmt.Errorf("canonical S6 Assignment Record requires a builder workgroup")
	}
	if !recordAssignmentIDRE.MatchString(assignmentID) {
		return Record{}, fmt.Errorf("invalid assignment id %q", assignmentID)
	}
	if err := validateRepositoryRef(manifestRef); err != nil {
		return Record{}, fmt.Errorf("manifest ref: %w", err)
	}
	if len(source.Assignments) == 0 {
		return Record{}, fmt.Errorf("manifest has no assignments")
	}
	var selected *recordManifestAssignment
	for i := range source.Assignments {
		if source.Assignments[i].AssignmentID != assignmentID {
			continue
		}
		if selected != nil {
			return Record{}, fmt.Errorf("manifest duplicates assignment %s", assignmentID)
		}
		selected = &source.Assignments[i]
	}
	if selected == nil {
		return Record{}, fmt.Errorf("assignment %s is not in the validated manifest", assignmentID)
	}
	item := *selected
	if item.RoleFamily != "backend-builder" && item.RoleFamily != "frontend-builder" && item.RoleFamily != "test-builder" {
		return Record{}, fmt.Errorf("assignment %s is not an S6 Builder role", assignmentID)
	}
	if item.AgentID == "" || item.ResponsibilityID == "" || source.WorkgroupID == "" || source.ManifestID == "" || source.Version == "" {
		return Record{}, fmt.Errorf("manifest assignment identity/source fields are incomplete")
	}
	if err := identity.ValidateAgentID(item.AgentID); err != nil {
		return Record{}, fmt.Errorf("manifest owner Agent id: %w", err)
	}
	if err := validateRepositoryRef(item.AgentDefinitionRef); err != nil {
		return Record{}, fmt.Errorf("agent definition ref: %w", err)
	}

	runtimeID, generation, currentRevision, err := currentRuntimeBinding(runtimeState)
	if err != nil {
		return Record{}, err
	}
	if runtimeID != source.RuntimeID || generation != source.BaselineGeneration {
		return Record{}, fmt.Errorf("manifest belongs to another Runtime or baseline generation")
	}
	lifecycle, _ := runtimeState["lifecycle"].(map[string]any)
	if lifecycle == nil || lifecycle["state"] != "building" {
		return Record{}, fmt.Errorf("canonical S6 Assignment Record requires the current building state")
	}

	taskDoc, err := currentTaskDocument(runtimeState, generation, source.Documents, taskID)
	if err != nil {
		return Record{}, err
	}
	if !recordTaskIDRE.MatchString(taskDoc.ID) {
		return Record{}, fmt.Errorf("invalid registered TASK id %q", taskDoc.ID)
	}
	actualTaskSHA := digest(taskBytes)
	if actualTaskSHA != taskDoc.SHA256 {
		return Record{}, fmt.Errorf("TASK bytes differ from current registered TASK %s", taskDoc.ID)
	}
	if err := validateRepositoryRef(taskDoc.Path); err != nil {
		return Record{}, fmt.Errorf("registered TASK ref: %w", err)
	}
	if err := verifyRuntimeTaskEntity(runtimeState, taskDoc, item.AgentID); err != nil {
		return Record{}, err
	}
	if err := verifyRuntimeRegistration(runtimeState, source.WorkgroupID, item.AgentID, item.RoleFamily, item.AgentDefinitionRef, item.DispatchMode, taskDoc.ID, manifestRef); err != nil {
		return Record{}, err
	}

	if item.DispatchMode == "" {
		item.DispatchMode = "plan_checkpoint"
	}
	if item.DispatchMode != "plan_checkpoint" && item.DispatchMode != "plan_approval_required" && item.DispatchMode != "one_shot" {
		return Record{}, fmt.Errorf("assignment %s has unsupported dispatch mode %q", assignmentID, item.DispatchMode)
	}
	if len(item.DoneWhen) == 0 {
		return Record{}, fmt.Errorf("assignment %s has no registered done_when contract", assignmentID)
	}
	if err := checkexec.ValidateProfiles(item.CheckProfiles, item.RequiredChecks, ""); err != nil {
		return Record{}, fmt.Errorf("assignment %s CheckProfile source: %w", assignmentID, err)
	}
	if err := ValidateClosingContract(item.ClosingContract); err != nil {
		return Record{}, fmt.Errorf("assignment %s closing contract: %w", assignmentID, err)
	}
	if item.ClosingContract != nil {
		if item.IntegrationCheckMode != "post_merge" {
			return Record{}, fmt.Errorf("assignment %s closing contract requires explicit post_merge integration checks", assignmentID)
		}
		if err := validateMainCheckProfiles(item.CheckProfiles); err != nil {
			return Record{}, fmt.Errorf("assignment %s Main CheckProfile source: %w", assignmentID, err)
		}
	}

	scopeBasis := ScopeBasis{
		ManifestScope:       append([]string(nil), item.Scope...),
		ManifestWritePaths:  append([]string(nil), item.WritePaths...),
		ManifestOutputPaths: append([]string(nil), item.OutputPaths...),
		TaskWritePaths:      append([]string(nil), taskWritePaths...),
	}
	effective, err := EffectiveScope(scopeBasis)
	if err != nil {
		return Record{}, fmt.Errorf("assignment %s effective scope: %w", assignmentID, err)
	}

	manifestSHA := digest(manifestBytes)
	manifestSource := ContextSource{Kind: "manifest", Ref: manifestRef, ID: source.ManifestID, Version: source.Version, SHA256: manifestSHA}
	taskSource := ContextSource{Kind: "task", Ref: taskDoc.Path, ID: taskDoc.ID, Version: taskDoc.Version, SHA256: taskDoc.SHA256}
	roleSource, err := findContextSource(contextSources, "agent_definition", item.AgentDefinitionRef, item.RoleFamily)
	if err != nil {
		return Record{}, err
	}
	if err := validateSHA(roleSource.SHA256); err != nil {
		return Record{}, fmt.Errorf("agent definition source: %w", err)
	}

	context := []ContextSource{manifestSource, taskSource, roleSource}
	for _, doc := range source.Documents {
		if doc.ID == "" || doc.Version == "" || doc.Path == "" {
			return Record{}, fmt.Errorf("manifest document reference is incomplete")
		}
		if err := validateSHA(doc.SHA256); err != nil {
			return Record{}, fmt.Errorf("manifest document %s: %w", doc.ID, err)
		}
		context = append(context, ContextSource{Kind: "manifest_document", Ref: doc.Path, ID: doc.ID, Version: doc.Version, SHA256: doc.SHA256})
	}
	for _, profile := range item.CheckProfiles {
		context = append(context, ContextSource{
			Kind:    "check_profile",
			Ref:     manifestRef + "#assignments/" + item.AssignmentID + "/check_profiles/" + profile.ID,
			ID:      profile.ID,
			Version: profile.Version,
			SHA256:  profile.Digest(),
		})
		for _, dependency := range profile.Dependencies {
			context = append(context, ContextSource{Kind: "check_profile_dependency", Ref: dependency.Path, SHA256: dependency.SHA256})
		}
		for _, fixture := range profile.Fixtures {
			context = append(context, ContextSource{Kind: "check_profile_fixture", Ref: fixture.Path, SHA256: fixture.SHA256})
		}
	}
	if item.ClosingContract != nil {
		for _, check := range item.ClosingContract.WorkerChecks {
			for _, dependency := range check.Profile.Dependencies {
				context = append(context, ContextSource{Kind: "worker_check_profile_dependency", Ref: dependency.Path, ID: check.Profile.ID, SHA256: dependency.SHA256})
			}
			for _, fixture := range check.Profile.Fixtures {
				context = append(context, ContextSource{Kind: "worker_check_profile_fixture", Ref: fixture.Path, ID: check.Profile.ID, SHA256: fixture.SHA256})
			}
		}
	}
	for _, extra := range contextSources {
		if extra.Kind == "agent_definition" {
			// The role source was copied above after matching its exact manifest
			// ref; do not duplicate it from the supplemental source list.
			continue
		}
		context = append(context, extra)
	}
	context, err = canonicalContextSources(context)
	if err != nil {
		return Record{}, err
	}
	if err := requireManifestSkills(item.SkillRefs, context); err != nil {
		return Record{}, err
	}

	record := Record{
		SchemaVersion:        AssignmentRecordSchema,
		Contract:             source.Contract,
		RuntimeID:            runtimeID,
		BaselineGeneration:   generation,
		RegistrationRevision: currentRevision + 1,
		AssignmentID:         assignmentID,
		Revision:             1,
		Task:                 taskSource,
		Manifest:             manifestSource,
		AgentDefinition:      roleSource,
		TeamID:               source.WorkgroupID,
		ResponsibilityID:     item.ResponsibilityID,
		OwnerAgentID:         item.AgentID,
		RoleFamily:           item.RoleFamily,
		DispatchMode:         item.DispatchMode,
		ReadPaths:            sortedCopy(item.ReadPaths),
		SkillRefs:            sortedCopy(item.SkillRefs),
		DependsOn:            sortedCopy(item.DependsOn),
		DoneWhen:             append([]string(nil), item.DoneWhen...),
		RequiredChecks:       copyStrings(item.RequiredChecks),
		CheckProfiles:        copyCheckProfiles(item.CheckProfiles),
		IntegrationCheckMode: func() string {
			if item.ClosingContract == nil {
				return ""
			}
			return item.IntegrationCheckMode
		}(),
		ClosingContract:     copyClosingContract(item.ClosingContract),
		ScopeBasis:          canonicalScopeBasis(scopeBasis),
		EffectiveWritePaths: effective,
		ContextSources:      context,
	}
	if err := ValidateRecord(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// ValidateClosingContract validates only the immutable dispatch shape. It
// does not accept or prove any check receipt, and it never treats an omitted
// contract as an explicitly empty check set.
func ValidateClosingContract(contract *ClosingContract) error {
	if contract == nil {
		return nil
	}
	if contract.Contract != ClosingContractVersion {
		return fmt.Errorf("contract must be %q", ClosingContractVersion)
	}
	if contract.WorkerChecks == nil {
		return fmt.Errorf("worker_checks must be an explicit JSON array")
	}
	if len(contract.WorkerChecks) > 64 {
		return fmt.Errorf("worker_checks exceeds the bounded 64-check contract")
	}
	profiles := make([]checkexec.Profile, len(contract.WorkerChecks))
	commands := make([]string, len(contract.WorkerChecks))
	for i, check := range contract.WorkerChecks {
		if check.Index != i {
			return fmt.Errorf("worker check indexes must be unique and contiguous from zero")
		}
		if strings.TrimSpace(check.Command) == "" || check.Profile.CheckIndex != check.Index || check.Profile.Command != check.Command || check.Profile.Location != "worker" {
			return fmt.Errorf("worker check %d command/profile binding is invalid", i)
		}
		profiles[i] = check.Profile
		commands[i] = check.Command
	}
	if len(profiles) != 0 {
		if err := checkexec.ValidateProfiles(profiles, commands, "worker"); err != nil {
			return fmt.Errorf("worker check profiles: %w", err)
		}
	}
	return nil
}

func validateMainCheckProfiles(profiles []checkexec.Profile) error {
	for _, profile := range profiles {
		if profile.Location != "main" {
			return fmt.Errorf("Main Required CheckProfile %s must have location main", profile.ID)
		}
	}
	return nil
}

func copyClosingContract(source *ClosingContract) *ClosingContract {
	if source == nil {
		return nil
	}
	copy := *source
	copy.WorkerChecks = append([]WorkerCheck{}, source.WorkerChecks...)
	for i := range copy.WorkerChecks {
		copy.WorkerChecks[i].Profile = copyCheckProfiles([]checkexec.Profile{source.WorkerChecks[i].Profile})[0]
	}
	return &copy
}

// EffectiveScope computes the manifest-declared scope intersected with the
// effective write/output declaration and the reviewed TASK write boundary.
// The result is sorted and canonical so every consumer can use the same
// persisted permission set.
func EffectiveScope(basis ScopeBasis) ([]string, error) {
	manifestScope, err := normalizeScopeSet(basis.ManifestScope)
	if err != nil {
		return nil, fmt.Errorf("manifest scope: %w", err)
	}
	writePaths, err := normalizeScopeSet(basis.ManifestWritePaths)
	if err != nil {
		return nil, fmt.Errorf("manifest write paths: %w", err)
	}
	outputPaths, err := normalizeScopeSet(basis.ManifestOutputPaths)
	if err != nil {
		return nil, fmt.Errorf("manifest output paths: %w", err)
	}
	taskScope, err := normalizeScopeSet(basis.TaskWritePaths)
	if err != nil {
		return nil, fmt.Errorf("reviewed TASK writes: %w", err)
	}
	declared := unionScopeSets(writePaths, outputPaths)
	if len(manifestScope) == 0 || len(declared) == 0 || len(taskScope) == 0 {
		return nil, fmt.Errorf("manifest scope, declared writes, and reviewed TASK writes must all be non-empty")
	}
	result := intersectScopeSets(intersectScopeSets(manifestScope, declared), taskScope)
	if len(result) == 0 {
		return nil, fmt.Errorf("original manifest and reviewed TASK scopes have no write-path intersection")
	}
	return result, nil
}

// ValidateRecord checks the serialized authority object, including that its
// effective write set is exactly reproducible from its retained source scope.
func ValidateRecord(record Record) error {
	if record.SchemaVersion != AssignmentRecordSchema || (record.Contract != AssignmentRecordContract && record.Contract != NativeDeliveryContract) {
		return fmt.Errorf("unsupported Assignment Record schema/contract")
	}
	if !recordRuntimeIDRE.MatchString(record.RuntimeID) || record.BaselineGeneration < 1 || record.RegistrationRevision < 1 {
		return fmt.Errorf("Assignment Record Runtime identity/revision is invalid")
	}
	if err := identity.ValidateAgentID(record.OwnerAgentID); err != nil {
		return fmt.Errorf("Assignment Record owner Agent id: %w", err)
	}
	if !recordAssignmentIDRE.MatchString(record.AssignmentID) || record.Revision < 1 {
		return fmt.Errorf("Assignment Record identity/revision is invalid")
	}
	if record.RevisionIssuance != nil {
		if err := validateRevisionIssuance(record, *record.RevisionIssuance); err != nil {
			return err
		}
	}
	if !recordTaskIDRE.MatchString(record.Task.ID) || record.Task.Kind != "task" || record.Task.Version == "" {
		return fmt.Errorf("Assignment Record TASK reference is invalid")
	}
	if record.Manifest.Kind != "manifest" || record.Manifest.ID == "" || record.Manifest.Version == "" {
		return fmt.Errorf("Assignment Record manifest reference is invalid")
	}
	if record.AgentDefinition.Kind != "agent_definition" || record.AgentDefinition.ID != record.RoleFamily {
		return fmt.Errorf("Assignment Record Agent definition binding is invalid")
	}
	for _, source := range []ContextSource{record.Task, record.Manifest, record.AgentDefinition} {
		if err := validateContextSource(source); err != nil {
			return err
		}
	}
	for _, source := range []ContextSource{record.Task, record.Manifest, record.AgentDefinition} {
		if err := validateRepositoryRef(source.Ref); err != nil {
			return fmt.Errorf("Assignment Record source ref: %w", err)
		}
	}
	if record.TeamID == "" || record.ResponsibilityID == "" || record.OwnerAgentID == "" || record.RoleFamily == "" {
		return fmt.Errorf("Assignment Record owner/team/responsibility is incomplete")
	}
	if record.RoleFamily != "backend-builder" && record.RoleFamily != "frontend-builder" && record.RoleFamily != "test-builder" {
		return fmt.Errorf("Assignment Record role is outside S6 Builder")
	}
	if record.DispatchMode != "plan_checkpoint" && record.DispatchMode != "plan_approval_required" && record.DispatchMode != "one_shot" {
		return fmt.Errorf("Assignment Record dispatch mode is invalid")
	}
	if record.ReadPaths == nil || record.SkillRefs == nil || record.DependsOn == nil || record.DoneWhen == nil || record.RequiredChecks == nil || record.CheckProfiles == nil || record.ContextSources == nil ||
		record.ScopeBasis.ManifestScope == nil || record.ScopeBasis.ManifestWritePaths == nil || record.ScopeBasis.ManifestOutputPaths == nil || record.ScopeBasis.TaskWritePaths == nil || record.EffectiveWritePaths == nil {
		return fmt.Errorf("Assignment Record array fields must be JSON arrays, including empty arrays")
	}
	if err := validateStrings(record.ReadPaths, false); err != nil {
		return fmt.Errorf("Assignment Record read paths: %w", err)
	}
	if !sameStrings(record.ReadPaths, canonicalStrings(record.ReadPaths)) {
		return fmt.Errorf("Assignment Record read paths are not canonical")
	}
	if err := validateStrings(record.SkillRefs, true); err != nil {
		return fmt.Errorf("Assignment Record Skill refs: %w", err)
	}
	if !sameStrings(record.SkillRefs, canonicalStrings(record.SkillRefs)) {
		return fmt.Errorf("Assignment Record Skill refs are not canonical")
	}
	if err := validateStrings(record.DependsOn, false); err != nil {
		return fmt.Errorf("Assignment Record dependencies: %w", err)
	}
	if !sameStrings(record.DependsOn, canonicalStrings(record.DependsOn)) {
		return fmt.Errorf("Assignment Record dependencies are not canonical")
	}
	if err := validateStrings(record.DoneWhen, true); err != nil {
		return fmt.Errorf("Assignment Record done_when: %w", err)
	}
	if err := validateStrings(record.RequiredChecks, false); err != nil {
		return fmt.Errorf("Assignment Record required checks: %w", err)
	}
	if len(record.CheckProfiles) > 0 {
		if err := checkexec.ValidateProfiles(record.CheckProfiles, record.RequiredChecks, ""); err != nil {
			return fmt.Errorf("Assignment Record CheckProfiles: %w", err)
		}
	}
	if err := ValidateClosingContract(record.ClosingContract); err != nil {
		return fmt.Errorf("Assignment Record closing contract: %w", err)
	}
	if record.ClosingContract != nil {
		if record.IntegrationCheckMode != "post_merge" {
			return fmt.Errorf("Assignment Record closing contract requires post_merge integration checks")
		}
		if err := validateMainCheckProfiles(record.CheckProfiles); err != nil {
			return fmt.Errorf("Assignment Record Main CheckProfiles: %w", err)
		}
	} else if record.IntegrationCheckMode != "" {
		return fmt.Errorf("Assignment Record without a closing contract must not project integration check mode")
	}
	if !sameScopeBasis(record.ScopeBasis, canonicalScopeBasis(record.ScopeBasis)) {
		return fmt.Errorf("Assignment Record source scope is not canonical")
	}
	computed, err := EffectiveScope(record.ScopeBasis)
	if err != nil {
		return err
	}
	if !sameStrings(computed, record.EffectiveWritePaths) {
		return fmt.Errorf("Assignment Record effective write paths differ from source scope intersection")
	}
	for _, scope := range record.EffectiveWritePaths {
		if _, err := normalizeScope(scope); err != nil {
			return fmt.Errorf("Assignment Record effective write path: %w", err)
		}
	}
	context, err := canonicalContextSources(record.ContextSources)
	if err != nil {
		return err
	}
	if !sameContextSources(context, record.ContextSources) {
		return fmt.Errorf("Assignment Record context sources are not canonical")
	}
	for _, source := range record.ContextSources {
		if err := validateContextSource(source); err != nil {
			return err
		}
		if err := validateRepositoryRef(source.Ref); err != nil {
			return fmt.Errorf("Assignment Record context source ref: %w", err)
		}
	}
	if record.ClosingContract != nil {
		for _, check := range record.ClosingContract.WorkerChecks {
			for _, dependency := range check.Profile.Dependencies {
				if !hasContextSourceIdentity(record.ContextSources, "worker_check_profile_dependency", dependency.Path, check.Profile.ID, dependency.SHA256) {
					return fmt.Errorf("Assignment Record omits Worker CheckProfile dependency %s", dependency.Path)
				}
			}
			for _, fixture := range check.Profile.Fixtures {
				if !hasContextSourceIdentity(record.ContextSources, "worker_check_profile_fixture", fixture.Path, check.Profile.ID, fixture.SHA256) {
					return fmt.Errorf("Assignment Record omits Worker CheckProfile fixture %s", fixture.Path)
				}
			}
		}
	}
	if !containsContextSource(record.ContextSources, record.Manifest) || !containsContextSource(record.ContextSources, record.Task) || !containsContextSource(record.ContextSources, record.AgentDefinition) {
		return fmt.Errorf("Assignment Record context source set omits manifest, TASK, or Agent definition")
	}
	if record.SourceSnapshots != nil {
		if err := validateSnapshotContextCoverage(record); err != nil {
			return err
		}
		if err := validateSourceSnapshots(record); err != nil {
			return err
		}
	}
	return nil
}

// FindRecord returns the highest revision for one Runtime/generation/
// Assignment identity. Historical revisions remain addressable through
// FindRecordRevision. Repeated revision numbers are invalid rather than
// resolved by array order.
func FindRecord(records []Record, runtimeID string, generation int, assignmentID string) (Record, bool, error) {
	if runtimeID == "" || generation < 1 || !recordAssignmentIDRE.MatchString(assignmentID) {
		return Record{}, false, fmt.Errorf("invalid Assignment Record lookup identity")
	}
	var current Record
	found := false
	revisions := map[int]bool{}
	for _, record := range records {
		if err := ValidateRecord(record); err != nil {
			return Record{}, false, fmt.Errorf("invalid stored Assignment Record: %w", err)
		}
		if record.RuntimeID != runtimeID || record.BaselineGeneration != generation || record.AssignmentID != assignmentID {
			continue
		}
		if revisions[record.Revision] {
			return Record{}, false, fmt.Errorf("duplicate Assignment Record revision %d for %s", record.Revision, assignmentID)
		}
		revisions[record.Revision] = true
		if !found || record.Revision > current.Revision {
			current, found = record, true
		}
	}
	if found {
		for revision := 1; revision <= current.Revision; revision++ {
			if !revisions[revision] {
				return Record{}, false, fmt.Errorf("Assignment Record history for %s skips revision %d", assignmentID, revision)
			}
		}
	}
	return current, found, nil
}

// FindRecordRevision resolves one exact immutable Assignment revision.
func FindRecordRevision(records []Record, ref RecordReference) (Record, bool, error) {
	if ref.RuntimeID == "" || ref.BaselineGeneration < 1 || !recordAssignmentIDRE.MatchString(ref.AssignmentID) || ref.Revision < 1 {
		return Record{}, false, fmt.Errorf("invalid Assignment Record reference")
	}
	var found *Record
	for i := range records {
		record := &records[i]
		if err := ValidateRecord(*record); err != nil {
			return Record{}, false, fmt.Errorf("invalid stored Assignment Record: %w", err)
		}
		if record.RuntimeID != ref.RuntimeID || record.BaselineGeneration != ref.BaselineGeneration || record.AssignmentID != ref.AssignmentID || record.Revision != ref.Revision {
			continue
		}
		if found != nil {
			return Record{}, false, fmt.Errorf("duplicate Assignment Record reference")
		}
		found = record
	}
	if found == nil {
		return Record{}, false, nil
	}
	return *found, true, nil
}

type registeredTaskReference struct {
	recordManifestDocument
	Kind string
}

func currentRuntimeBinding(state map[string]any) (string, int, int, error) {
	runtimeID, ok := state["runtime_id"].(string)
	if !ok || !recordRuntimeIDRE.MatchString(runtimeID) {
		return "", 0, 0, fmt.Errorf("current Runtime id is missing or invalid")
	}
	baseline, ok := state["baseline"].(map[string]any)
	if !ok {
		return "", 0, 0, fmt.Errorf("current Runtime baseline is missing")
	}
	generation, err := positiveInteger(baseline["generation"], "baseline generation")
	if err != nil {
		return "", 0, 0, err
	}
	currentRevision, err := nonNegativeInteger(state["revision"], "Runtime revision")
	if err != nil {
		return "", 0, 0, err
	}
	return runtimeID, generation, currentRevision, nil
}

func currentTaskDocument(state map[string]any, generation int, manifestDocs []recordManifestDocument, taskID string) (registeredTaskReference, error) {
	if !recordTaskIDRE.MatchString(taskID) {
		return registeredTaskReference{}, fmt.Errorf("invalid requested TASK id %q", taskID)
	}
	var manifestTask *recordManifestDocument
	for i := range manifestDocs {
		if manifestDocs[i].ID != taskID {
			continue
		}
		if manifestTask != nil {
			return registeredTaskReference{}, fmt.Errorf("manifest contains multiple TASK document subjects")
		}
		manifestTask = &manifestDocs[i]
	}
	if manifestTask == nil {
		return registeredTaskReference{}, fmt.Errorf("manifest has no formal TASK document subject %s", taskID)
	}
	docs, ok := state["documents"].([]any)
	if !ok {
		return registeredTaskReference{}, fmt.Errorf("current Runtime documents are missing")
	}
	var registered *registeredTaskReference
	for _, raw := range docs {
		row, ok := raw.(map[string]any)
		if !ok || row["kind"] != "task" || row["id"] != manifestTask.ID {
			continue
		}
		rowGeneration, err := nonNegativeInteger(row["generation"], "TASK generation")
		if err != nil {
			return registeredTaskReference{}, fmt.Errorf("registered TASK %s generation: %w", manifestTask.ID, err)
		}
		if rowGeneration != generation {
			continue
		}
		candidate := registeredTaskReference{Kind: "task", recordManifestDocument: recordManifestDocument{
			ID:      stringField(row["id"]),
			Path:    stringField(row["path"]),
			Version: stringField(row["version"]),
			SHA256:  stringField(row["sha256"]),
		}}
		if candidate.ID == "" || candidate.Path == "" || candidate.Version == "" || candidate.SHA256 == "" || row["status"] != "locked" {
			return registeredTaskReference{}, fmt.Errorf("current TASK document is not fully registered and locked")
		}
		if registered != nil {
			return registeredTaskReference{}, fmt.Errorf("current generation duplicates registered TASK document %s", manifestTask.ID)
		}
		registered = &candidate
	}
	if registered == nil {
		return registeredTaskReference{}, fmt.Errorf("current generation has no registered TASK document")
	}
	if manifestTask.ID != registered.ID || manifestTask.Path != registered.Path || manifestTask.Version != registered.Version || manifestTask.SHA256 != registered.SHA256 {
		return registeredTaskReference{}, fmt.Errorf("manifest TASK ref differs from the current Runtime TASK id/path/version/SHA")
	}
	if err := validateSHA(registered.SHA256); err != nil {
		return registeredTaskReference{}, fmt.Errorf("registered TASK SHA: %w", err)
	}
	return *registered, nil
}

func verifyRuntimeTaskEntity(state map[string]any, task registeredTaskReference, owner string) error {
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return fmt.Errorf("current Runtime entities are missing")
	}
	tasks, ok := entities["tasks"].([]any)
	if !ok {
		return fmt.Errorf("current Runtime TASK entities are missing")
	}
	var found map[string]any
	for _, raw := range tasks {
		row, ok := raw.(map[string]any)
		if ok && row["id"] == task.ID {
			if found != nil {
				return fmt.Errorf("Runtime has duplicate TASK entity %s", task.ID)
			}
			found = row
		}
	}
	if found == nil || found["path"] != task.Path || found["sha256"] != task.SHA256 {
		return fmt.Errorf("Runtime TASK entity differs from its current registered document")
	}
	owners, ok := stringValues(found["owner_agent_ids"])
	if !ok || !containsString(owners, owner) {
		return fmt.Errorf("Runtime TASK entity does not bind manifest owner %s", owner)
	}
	return nil
}

func verifyRuntimeRegistration(state map[string]any, teamID, owner, roleFamily, agentDefinitionRef, dispatchMode, taskID, manifestRef string) error {
	entities, _ := state["entities"].(map[string]any)
	teams, _ := entities["teams"].([]any)
	teamFound := false
	for _, raw := range teams {
		row, _ := raw.(map[string]any)
		if row["id"] != teamID {
			continue
		}
		if teamFound || row["manifest_ref"] != manifestRef || row["kind"] != "builder" {
			return fmt.Errorf("Runtime Team binding is duplicate or differs from the validated manifest")
		}
		teamFound = true
	}
	if !teamFound {
		return fmt.Errorf("Runtime Team %s is not registered in this Writer candidate", teamID)
	}
	agents, _ := entities["agents"].([]any)
	ownerFound := false
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row["id"] != owner {
			continue
		}
		taskIDs, ok := stringValues(row["task_ids"])
		if ownerFound || row["team_id"] != teamID || row["role"] != roleFamily || row["definition_ref"] != agentDefinitionRef || !ok || !containsString(taskIDs, taskID) {
			return fmt.Errorf("Runtime Agent owner %s is duplicate or differs from the manifest binding", owner)
		}
		if dispatchMode == "" {
			dispatchMode = "plan_checkpoint"
		}
		if row["dispatch_mode"] != dispatchMode {
			return fmt.Errorf("Runtime Agent owner %s dispatch mode differs from the manifest", owner)
		}
		ownerFound = true
	}
	if !ownerFound {
		return fmt.Errorf("Runtime Agent owner %s is not registered in this Writer candidate", owner)
	}
	return nil
}

func canonicalScopeBasis(basis ScopeBasis) ScopeBasis {
	return ScopeBasis{
		ManifestScope:       canonicalStrings(basis.ManifestScope),
		ManifestWritePaths:  canonicalStrings(basis.ManifestWritePaths),
		ManifestOutputPaths: canonicalStrings(basis.ManifestOutputPaths),
		TaskWritePaths:      canonicalStrings(basis.TaskWritePaths),
	}
}

func sameScopeBasis(left, right ScopeBasis) bool {
	return sameStrings(left.ManifestScope, right.ManifestScope) &&
		sameStrings(left.ManifestWritePaths, right.ManifestWritePaths) &&
		sameStrings(left.ManifestOutputPaths, right.ManifestOutputPaths) &&
		sameStrings(left.TaskWritePaths, right.TaskWritePaths)
}

func normalizeScopeSet(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	result := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		normalized, err := normalizeScope(path)
		if err != nil {
			return nil, err
		}
		if seen[normalized] {
			return nil, fmt.Errorf("scope contains duplicate normalized path %q", normalized)
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeScope(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\\:*?[]{}\x00") {
		return "", fmt.Errorf("scope path %q is not a repository-relative literal path", path)
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean == "" {
		return "", fmt.Errorf("scope path %q escapes or names the repository root", path)
	}
	trimmed := strings.TrimSuffix(path, "/")
	if clean != trimmed {
		return "", fmt.Errorf("scope path %q is not normalized", path)
	}
	return clean, nil
}

func unionScopeSets(left, right []string) []string {
	seen := make(map[string]bool, len(left)+len(right))
	result := make([]string, 0, len(left)+len(right))
	for _, values := range [][]string{left, right} {
		for _, value := range values {
			if seen[value] {
				continue
			}
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func intersectScopeSets(left, right []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, a := range left {
		for _, b := range right {
			if common, ok := scopeIntersection(a, b); ok && !seen[common] {
				seen[common] = true
				result = append(result, common)
			}
		}
	}
	sort.Strings(result)
	return result
}

func scopeIntersection(left, right string) (string, bool) {
	if left == right {
		return left, true
	}
	if scopeContains(left, right) {
		return right, true
	}
	if scopeContains(right, left) {
		return left, true
	}
	return "", false
}

func scopeContains(parent, child string) bool {
	if parent == child {
		return true
	}
	if strings.ContainsAny(parent, "*?[]{}") || strings.ContainsAny(child, "*?[]{}") {
		return false
	}
	last := parent[strings.LastIndex(parent, "/")+1:]
	if strings.Contains(last, ".") {
		return false
	}
	return strings.HasPrefix(child, parent+"/")
}

func validateRepositoryRef(ref string) error {
	if strings.TrimSpace(ref) == "" || filepath.IsAbs(ref) || strings.ContainsAny(ref, "\\:\x00") {
		return fmt.Errorf("%q is not a safe repository-relative ref", ref)
	}
	clean := filepath.ToSlash(filepath.Clean(ref))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%q escapes the repository", ref)
	}
	return nil
}

func canonicalContextSources(sources []ContextSource) ([]ContextSource, error) {
	result := append([]ContextSource(nil), sources...)
	seen := map[string]ContextSource{}
	for _, source := range result {
		if err := validateContextSource(source); err != nil {
			return nil, err
		}
		key := source.Kind + "\x00" + source.Ref + "\x00" + source.ID
		if old, ok := seen[key]; ok {
			if old != source {
				return nil, fmt.Errorf("conflicting context source %s", source.Ref)
			}
			return nil, fmt.Errorf("duplicate context source %s", source.Ref)
		}
		seen[key] = source
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		if result[i].Ref != result[j].Ref {
			return result[i].Ref < result[j].Ref
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func validateContextSource(source ContextSource) error {
	allowed := map[string]bool{
		"agent_definition": true, "authorized_skill": true, "check_profile": true,
		"check_profile_dependency": true, "check_profile_fixture": true,
		"worker_check_profile_dependency": true, "worker_check_profile_fixture": true,
		"frozen_input": true, "manifest": true, "manifest_document": true, "task": true,
		"embedded_schema": true, "embedded_context_profile": true,
	}
	if !allowed[source.Kind] || strings.TrimSpace(source.Ref) == "" {
		return fmt.Errorf("Assignment Record has an invalid context source kind/ref")
	}
	if err := validateSHA(source.SHA256); err != nil {
		return fmt.Errorf("context source %s: %w", source.Ref, err)
	}
	if strings.ContainsAny(source.Ref, "\x00\r\n") {
		return fmt.Errorf("context source ref contains control characters")
	}
	if source.Kind == "embedded_schema" && !strings.HasPrefix(source.Ref, "gsb-embedded/schema/") {
		return fmt.Errorf("embedded schema source has no embedded namespace")
	}
	if source.Kind == "embedded_context_profile" && source.Ref != "gsb-embedded/agent-context-profiles.json" {
		return fmt.Errorf("embedded context profile source has no embedded namespace")
	}
	if strings.HasPrefix(source.Ref, "gsb-embedded/") && source.Kind != "embedded_schema" && source.Kind != "embedded_context_profile" {
		return fmt.Errorf("embedded namespace cannot be declared as a project file source")
	}
	return nil
}

func findContextSource(sources []ContextSource, kind, ref, id string) (ContextSource, error) {
	var found *ContextSource
	for i := range sources {
		if sources[i].Kind != kind || sources[i].Ref != ref || (id != "" && sources[i].ID != id) {
			continue
		}
		if found != nil {
			return ContextSource{}, fmt.Errorf("duplicate %s context source for %s", kind, ref)
		}
		found = &sources[i]
	}
	if found == nil {
		return ContextSource{}, fmt.Errorf("missing actual %s context source for %s", kind, ref)
	}
	return *found, nil
}

func requireManifestSkills(skillRefs []string, sources []ContextSource) error {
	for _, ref := range skillRefs {
		var found *ContextSource
		for _, source := range sources {
			if source.Kind == "authorized_skill" && source.ID == ref {
				if found != nil {
					return fmt.Errorf("manifest Skill %s has multiple frozen source/hash bindings", ref)
				}
				candidate := source
				found = &candidate
			}
		}
		if found == nil {
			return fmt.Errorf("manifest Skill %s has no frozen source/hash binding", ref)
		}
	}
	return nil
}

func containsContextSource(sources []ContextSource, target ContextSource) bool {
	for _, source := range sources {
		if source == target {
			return true
		}
	}
	return false
}

func hasContextSourceIdentity(sources []ContextSource, kind, ref, id, sha string) bool {
	for _, source := range sources {
		if source.Kind == kind && source.Ref == ref && source.ID == id && source.SHA256 == sha {
			return true
		}
	}
	return false
}

func sameContextSources(left, right []ContextSource) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func sortedCopy(values []string) []string { return canonicalStrings(values) }

func canonicalStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	sort.Strings(result)
	return result
}

func copyStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func copyCheckProfiles(values []checkexec.Profile) []checkexec.Profile {
	result := make([]checkexec.Profile, len(values))
	copy(result, values)
	return result
}

func validateStrings(values []string, required bool) error {
	if required && len(values) == 0 {
		return fmt.Errorf("must be non-empty")
	}
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\x00\r\n") || seen[value] {
			return fmt.Errorf("contains an empty, invalid, or duplicate value")
		}
		seen[value] = true
	}
	return nil
}

func validateSHA(value string) error {
	if !recordSHA256RE.MatchString(value) {
		return fmt.Errorf("sha256 must be 64 lowercase hexadecimal characters")
	}
	return nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func positiveInteger(value any, name string) (int, error) {
	n, err := exactInteger(value)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return n, nil
}

func nonNegativeInteger(value any, name string) (int, error) {
	n, err := exactInteger(value)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return n, nil
}

func exactInteger(value any) (int, error) {
	switch number := value.(type) {
	case int:
		return number, nil
	case int64:
		if int64(int(number)) != number {
			return 0, fmt.Errorf("integer overflow")
		}
		return int(number), nil
	case float64:
		if number < 0 || number > float64(int(^uint(0)>>1)) || number != float64(int(number)) {
			return 0, fmt.Errorf("not an integer")
		}
		return int(number), nil
	case json.Number:
		integer, err := strconv.ParseInt(string(number), 10, 64)
		if err != nil || int64(int(integer)) != integer {
			return 0, fmt.Errorf("not an integer")
		}
		return int(integer), nil
	default:
		return 0, fmt.Errorf("not an integer")
	}
}

func stringField(value any) string {
	result, _ := value.(string)
	return result
}

func stringValues(value any) ([]string, bool) {
	switch values := value.(type) {
	case []string:
		return append([]string{}, values...), true
	case []any:
		result := make([]string, len(values))
		for i, value := range values {
			stringValue, ok := value.(string)
			if !ok {
				return nil, false
			}
			result[i] = stringValue
		}
		return result, true
	default:
		return nil, false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
