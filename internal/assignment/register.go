package assignment

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/transition"
)

type Request struct {
	ExpectedRevision int
	ManifestPath     string
	TaskID           string
	TaskPath         string
	OccurredAt       time.Time
}

type manifest struct {
	ManifestID                 string                 `json:"manifest_id"`
	PlatformTeamID             string                 `json:"platform_team_id"`
	WorkgroupID                string                 `json:"workgroup_id"`
	WorkgroupKind              string                 `json:"workgroup_kind"`
	ReviewRound                *int                   `json:"review_round"`
	ResponsibilityDispositions []responsibilityStatus `json:"responsibility_dispositions"`
	Assignments                []assignment           `json:"assignments"`
}

type responsibilityStatus struct {
	ResponsibilityID string `json:"responsibility_id"`
	Disposition      string `json:"disposition"`
}

type assignment struct {
	AssignmentID       string `json:"assignment_id"`
	ResponsibilityID   string `json:"responsibility_id"`
	RoleFamily         string `json:"role_family"`
	AgentID            string `json:"agent_id"`
	AgentDefinitionRef string `json:"agent_definition_ref"`
}

func Register(root, statePath, journalPath string, request Request) (loopruntime.Snapshot, error) {
	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateData, &current); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode runtime: %w", err)
	}
	lifecycle, ok := current["lifecycle"].(map[string]any)
	if !ok {
		return loopruntime.Snapshot{}, fmt.Errorf("runtime lifecycle must be an object")
	}
	cursor := map[string]any{
		"state": lifecycle["state"],
		"phase": lifecycle["phase"],
	}
	runtimeID, _ := current["runtime_id"].(string)

	manifestData, err := os.ReadFile(request.ManifestPath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read team manifest: %w", err)
	}
	if err := schema.NewValidator(root).ValidateBytes(
		"team-manifest.schema.json",
		manifestData,
	); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("team manifest schema: %w", err)
	}
	if err := team.ValidateBytes(manifestData); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("team manifest semantics: %w", err)
	}
	var value manifest
	if err := json.Unmarshal(manifestData, &value); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode team manifest: %w", err)
	}
	taskData, err := os.ReadFile(request.TaskPath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read task: %w", err)
	}
	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	manifestRef := repositoryPath(root, request.ManifestPath)
	taskRef := repositoryPath(root, request.TaskPath)
	agentIDs := make([]string, 0, len(value.Assignments))
	for _, item := range value.Assignments {
		agentIDs = append(agentIDs, item.AgentID)
	}
	responsibilityIDs := assignedResponsibilities(value)

	store := loopruntime.NewStore(statePath, journalPath)
	return store.Update(request.ExpectedRevision, loopruntime.Mutation{
		EventID:        fmt.Sprintf("evt-register-%s-r%d", value.WorkgroupID, request.ExpectedRevision+1),
		TransitionID:   "ENTITY-REGISTER",
		Event:          "workgroup_registered",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:register:%s:%d", value.ManifestID, request.ExpectedRevision),
		EvidenceIDs:    []string{manifestRef, taskRef},
		Message:        "Registered a validated workgroup, task and phase-one Agents.",
		OccurredAt:     occurredAt,
		RuntimeID:      runtimeID,
		From:           cursor,
		To:             cursor,
		Apply: func(state map[string]any) error {
			lifecycle, ok := state["lifecycle"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime lifecycle must be an object")
			}
			if err := validateWorkgroupState(value.WorkgroupKind, lifecycle); err != nil {
				return err
			}
			entities, ok := state["entities"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime entities must be an object")
			}
			if containsEntity(entities["teams"], value.WorkgroupID) {
				return fmt.Errorf("workgroup %s is already registered", value.WorkgroupID)
			}
			task := map[string]any{
				"id":              request.TaskID,
				"state":           "reviewed",
				"path":            taskRef,
				"sha256":          transition.SHA256(taskData),
				"owner_agent_ids": agentIDs,
			}
			tasks, _ := entities["tasks"].([]any)
			if !containsEntity(tasks, request.TaskID) {
				entities["tasks"] = append(tasks, task)
			}
			teams, _ := entities["teams"].([]any)
			entities["teams"] = append(teams, map[string]any{
				"id":                 value.WorkgroupID,
				"platform_team_id":   value.PlatformTeamID,
				"kind":               value.WorkgroupKind,
				"status":             "planned",
				"manifest_ref":       manifestRef,
				"responsibility_ids": responsibilityIDs,
				"agent_ids":          agentIDs,
				"review_round":       value.ReviewRound,
			})
			agents, _ := entities["agents"].([]any)
			for _, item := range value.Assignments {
				if containsEntity(agents, item.AgentID) {
					return fmt.Errorf("Agent %s is already registered", item.AgentID)
				}
				agents = append(agents, map[string]any{
					"id":                  item.AgentID,
					"role":                item.RoleFamily,
					"state":               "reading",
					"task_ids":            []string{request.TaskID},
					"team_id":             value.WorkgroupID,
					"definition_ref":      item.AgentDefinitionRef,
					"prompt_ref":          manifestRef + "#" + item.AssignmentID,
					"readback_ref":        nil,
					"activation_ref":      nil,
					"activation_revision": nil,
					"updated_at":          occurredAt.UTC().Format(time.RFC3339Nano),
				})
			}
			entities["agents"] = agents
			state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			return nil
		},
	})
}

func assignedResponsibilities(value manifest) []string {
	seen := map[string]bool{}
	var ids []string
	for _, item := range value.ResponsibilityDispositions {
		if item.Disposition != "assigned" || item.ResponsibilityID == "" || seen[item.ResponsibilityID] {
			continue
		}
		seen[item.ResponsibilityID] = true
		ids = append(ids, item.ResponsibilityID)
	}
	if len(ids) > 0 {
		return ids
	}
	for _, item := range value.Assignments {
		if item.ResponsibilityID == "" || seen[item.ResponsibilityID] {
			continue
		}
		seen[item.ResponsibilityID] = true
		ids = append(ids, item.ResponsibilityID)
	}
	return ids
}

func validateWorkgroupState(kind string, lifecycle map[string]any) error {
	state, _ := lifecycle["state"].(string)
	phase, _ := lifecycle["phase"].(string)
	switch kind {
	case "document_verifier":
		if state != "document_verification" {
			return fmt.Errorf("document verifier workgroup requires document_verification state")
		}
	case "delivery_verifier":
		if state != "verification" || phase != "delivery" {
			return fmt.Errorf("delivery verifier workgroup requires verification.delivery")
		}
	case "qa":
		if state != "verification" || phase != "qa" {
			return fmt.Errorf("QA workgroup requires verification.qa")
		}
	case "e2e_browser":
		if state != "verification" || phase != "e2e_browser" {
			return fmt.Errorf("E2E browser workgroup requires verification.e2e_browser")
		}
	case "builder":
		if state != "building" && state != "bug_resolution" {
			return fmt.Errorf("builder workgroup requires building or bug_resolution state")
		}
	default:
		return fmt.Errorf("unsupported workgroup kind %q", kind)
	}
	return nil
}

func repositoryPath(root, path string) string {
	if relative, err := filepath.Rel(root, path); err == nil && relative != ".." &&
		!filepath.IsAbs(relative) {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(path)
}

func containsEntity(value any, id string) bool {
	items, _ := value.([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["id"] == id {
			return true
		}
	}
	return false
}

// =============================================================================
// TASK-015 / BUG-003 §4b.2(a) — RegisterBug runtime operation.
//
// RegisterBug adds a canonical BUG entity to state.entities.bugs. It is the
// only sanctioned way to create a BUG entity at runtime; direct JSON edits
// are forbidden by REQ-002.
//
// Contract (BUG-003 §4b.2(a)):
//   - id matches ^BUG-[0-9]{3,}$.
//   - severity ∈ {P0, P1, P2, P3}.
//   - finding_source path exists on disk.
//   - at least one EvidenceRefs entry exists on disk; an empty evidence
//     chain is rejected.
//   - finding fingerprint sha256(finding_source + sorted(EvidenceRefs)) is
//     unique among all live (non-closed/non-rejected/non-duplicate) BUGs.
//     On collision, ErrDuplicateBug{ExistingBugID} is returned.
//   - CAS-safe append via loopruntime.Store.Update (acquireLock + revision
//     check + atomic JSON write).
// =============================================================================

// RegisterBugRequest is the wire shape for the runtime register-bug
// operation. Only the canonical fields allowed by the bug schema
// (loop-state.schema.json §bug) are persisted; the finding_source and
// evidence_refs fields are validated for existence and used to compute the
// finding fingerprint, then folded into the stored BUG's path field as
// `<finding_source>#fp=<sha256>` (the fingerprint suffix is the dedup key).
type RegisterBugRequest struct {
	ExpectedRevision     int
	BugID                string   // must match ^BUG-[0-9]{3,}$
	Severity             string   // P0..P3
	FindingSource        string   // path to the finding source document
	EvidenceRefs         []string // paths to evidence files
	RootCause            string   // free text
	Reproduction         string   // free text
	ReporterAgentID      string   // required; recorded in original_finder_agent_ids
	ClosingContractHints []string
	OccurredAt           time.Time
}

// ErrDuplicateBug is returned by RegisterBug when a BUG with the same
// finding fingerprint already exists and is still in a live state (not
// closed/rejected/duplicate). ExistingBugID identifies the prior BUG that
// the caller should link to instead.
type ErrDuplicateBug struct {
	ExistingBugID string
}

func (e *ErrDuplicateBug) Error() string {
	return fmt.Sprintf("duplicate BUG (finding fingerprint already registered as %s)", e.ExistingBugID)
}

// ErrInvalidBugSeverity is returned when req.Severity is not in {P0..P3}.
type ErrInvalidBugSeverity struct {
	Severity string
}

func (e *ErrInvalidBugSeverity) Error() string {
	return fmt.Sprintf("invalid severity %q (must be P0|P1|P2|P3)", e.Severity)
}

// ErrMissingEvidence is returned when req.EvidenceRefs is empty or any
// referenced evidence file does not exist on disk.
type ErrMissingEvidence struct {
	Missing []string
}

func (e *ErrMissingEvidence) Error() string {
	return fmt.Sprintf("missing or non-existent evidence: %v", e.Missing)
}

var (
	bugIDPattern    = regexp.MustCompile(`^BUG-[0-9]{3,}$`)
	bugSeverityEnum = map[string]bool{"P0": true, "P1": true, "P2": true, "P3": true}
)

// RegisterBug creates a canonical BUG entity using CAS-safe append.
// Validation runs before the mutation; on any validation error the runtime
// is not touched.
func RegisterBug(root, statePath, journalPath string, req RegisterBugRequest) (loopruntime.Snapshot, error) {
	if !bugIDPattern.MatchString(req.BugID) {
		return loopruntime.Snapshot{}, fmt.Errorf("BUG id %q does not match ^BUG-[0-9]{3,}$", req.BugID)
	}
	if !bugSeverityEnum[req.Severity] {
		return loopruntime.Snapshot{}, &ErrInvalidBugSeverity{Severity: req.Severity}
	}
	if req.FindingSource == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("finding_source is required")
	}
	if req.ReporterAgentID == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("reporter_agent_id is required")
	}
	if len(req.EvidenceRefs) == 0 {
		return loopruntime.Snapshot{}, &ErrMissingEvidence{Missing: []string{"<empty evidence chain>"}}
	}
	missing, err := validateEvidenceFiles(root, req.EvidenceRefs)
	if err != nil {
		return loopruntime.Snapshot{}, &ErrMissingEvidence{Missing: missing}
	}
	absSource := filepath.Join(root, req.FindingSource)
	if _, err := os.Stat(absSource); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("finding_source %q not readable: %w", req.FindingSource, err)
	}

	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateData, &current); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode runtime: %w", err)
	}
	runtimeID, _ := current["runtime_id"].(string)
	lifecycle, _ := current["lifecycle"].(map[string]any)
	cursor := map[string]any{
		"state": lifecycle["state"],
		"phase": lifecycle["phase"],
	}

	fingerprint := bugFindingFingerprint(req.FindingSource, req.EvidenceRefs)
	occurredAt := req.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	store := loopruntime.NewStore(statePath, journalPath)
	return store.Update(req.ExpectedRevision, loopruntime.Mutation{
		EventID:        fmt.Sprintf("evt-register-bug-%s-r%d", req.BugID, req.ExpectedRevision+1),
		TransitionID:   "ENTITY-REGISTER",
		Event:          "bug_registered",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:register:bug:%s:%d", req.BugID, req.ExpectedRevision),
		From:           cursor,
		To:             cursor,
		RuntimeID:      runtimeID,
		Message:        fmt.Sprintf("Registered canonical BUG %s (severity=%s)", req.BugID, req.Severity),
		OccurredAt:     occurredAt,
		EvidenceIDs:    append([]string{req.FindingSource}, req.EvidenceRefs...),
		Apply: func(state map[string]any) error {
			entities, ok := state["entities"].(map[string]any)
			if !ok {
				return fmt.Errorf("entities must be an object")
			}
			bugs, _ := entities["bugs"].([]any)
			for _, raw := range bugs {
				bug, _ := raw.(map[string]any)
				id, _ := bug["id"].(string)
				if id == req.BugID {
					return fmt.Errorf("BUG %s already registered", req.BugID)
				}
			}
			for _, raw := range bugs {
				bug, _ := raw.(map[string]any)
				path, _ := bug["path"].(string)
				if !strings.HasSuffix(path, "#fp="+fingerprint) {
					continue
				}
				stateVal, _ := bug["state"].(string)
				if stateVal == "closed" || stateVal == "rejected" || stateVal == "duplicate" {
					continue
				}
				existingID, _ := bug["id"].(string)
				return &ErrDuplicateBug{ExistingBugID: existingID}
			}
			newBug := map[string]any{
				"id":                          req.BugID,
				"state":                       "draft",
				"path":                        req.FindingSource + "#fp=" + fingerprint,
				"severity":                    req.Severity,
				"attempt_count":               0,
				"same_contract_failure_count": 0,
				"original_finder_agent_ids":   []any{req.ReporterAgentID},
			}
			entities["bugs"] = append(bugs, newBug)
			state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			return nil
		},
	})
}

// bugFindingFingerprint computes sha256(findingSource + sorted(evidenceRefs))
// per BUG-003 §4b.2(a). Sorting the evidence paths is what makes the
// fingerprint canonical regardless of input ordering. The fingerprint is
// then encoded in the BUG's path field for dedup.
func bugFindingFingerprint(findingSource string, evidenceRefs []string) string {
	refs := append([]string(nil), evidenceRefs...)
	sort.Strings(refs)
	h := sha256.New()
	h.Write([]byte(findingSource))
	h.Write([]byte{'\n'})
	for i, ref := range refs {
		if i > 0 {
			h.Write([]byte{'\n'})
		}
		h.Write([]byte(ref))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// validateEvidenceFiles verifies that every supplied evidence path is
// readable. The first return value is the list of missing paths (in input
// order); the second is the error from os.Stat (typically nil since missing
// paths return nil error from this helper).
func validateEvidenceFiles(root string, refs []string) ([]string, error) {
	var missing []string
	for _, ref := range refs {
		abs := filepath.Join(root, ref)
		if _, err := os.Stat(abs); err != nil {
			missing = append(missing, ref)
		}
	}
	if len(missing) > 0 {
		return missing, fmt.Errorf("missing evidence: %v", missing)
	}
	return nil, nil
}

// =============================================================================
// TASK-015 / BUG-003 §4b.2(b) — RegisterTask runtime operation.
// =============================================================================

// RegisterTaskRequest is the wire shape for the runtime register-task
// operation. It validates that:
//   - Path exists on disk.
//   - OwnerAgentIDs is non-empty and each owner is registered in
//     entities.agents.
//   - SourceContractID is a non-empty primary contract reference.
//   - ID is not already present in entities.tasks (duplicate id rejected).
type RegisterTaskRequest struct {
	ExpectedRevision int
	TaskID           string   // matches ^TASK-[0-9]{3,}$
	Path             string   // canonical task spec path
	OwnerAgentIDs    []string // must exist in entities.agents
	SourceContractID string   // primary contract reference (e.g. "BUG-003")
	OccurredAt       time.Time
}

// ErrMissingTaskPath is returned when req.Path is empty or unreadable.
type ErrMissingTaskPath struct {
	Path string
}

func (e *ErrMissingTaskPath) Error() string {
	return fmt.Sprintf("task path %q missing or unreadable", e.Path)
}

// ErrMissingTaskOwner is returned when req.OwnerAgentIDs is empty.
type ErrMissingTaskOwner struct{}

func (e *ErrMissingTaskOwner) Error() string { return "task owner_agent_ids is required" }

// ErrDuplicateTaskID is returned when the supplied task id already exists in
// entities.tasks.
type ErrDuplicateTaskID struct {
	TaskID string
}

func (e *ErrDuplicateTaskID) Error() string {
	return fmt.Sprintf("task %s already registered", e.TaskID)
}

// taskIDPattern matches the canonical TASK-NNN id format used in the task
// schema and the existing entities.
var taskIDPattern = regexp.MustCompile(`^TASK-[0-9]{3,}$`)

// RegisterTask adds a canonical TASK entity to state.entities.tasks in the
// "candidate" state. The operation uses CAS-safe append via
// loopruntime.Store.Update.
func RegisterTask(root, statePath, journalPath string, req RegisterTaskRequest) (loopruntime.Snapshot, error) {
	if !taskIDPattern.MatchString(req.TaskID) {
		return loopruntime.Snapshot{}, fmt.Errorf("task id %q does not match ^TASK-[0-9]{3,}$", req.TaskID)
	}
	if req.Path == "" {
		return loopruntime.Snapshot{}, &ErrMissingTaskPath{}
	}
	absPath := filepath.Join(root, req.Path)
	taskData, err := os.ReadFile(absPath)
	if err != nil {
		return loopruntime.Snapshot{}, &ErrMissingTaskPath{Path: req.Path}
	}
	if len(req.OwnerAgentIDs) == 0 {
		return loopruntime.Snapshot{}, &ErrMissingTaskOwner{}
	}
	if req.SourceContractID == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("source_contract_id is required")
	}

	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateData, &current); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode runtime: %w", err)
	}
	runtimeID, _ := current["runtime_id"].(string)
	lifecycle, _ := current["lifecycle"].(map[string]any)
	cursor := map[string]any{
		"state": lifecycle["state"],
		"phase": lifecycle["phase"],
	}
	taskRef := repositoryPath(root, absPath)
	taskSHA := transition.SHA256(taskData)
	occurredAt := req.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	store := loopruntime.NewStore(statePath, journalPath)
	return store.Update(req.ExpectedRevision, loopruntime.Mutation{
		EventID:        fmt.Sprintf("evt-register-task-%s-r%d", req.TaskID, req.ExpectedRevision+1),
		TransitionID:   "ENTITY-REGISTER",
		Event:          "task_registered",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:register:task:%s:%d", req.TaskID, req.ExpectedRevision),
		From:           cursor,
		To:             cursor,
		RuntimeID:      runtimeID,
		Message:        fmt.Sprintf("Registered canonical TASK %s", req.TaskID),
		OccurredAt:     occurredAt,
		EvidenceIDs:    []string{taskRef},
		Apply: func(state map[string]any) error {
			entities, ok := state["entities"].(map[string]any)
			if !ok {
				return fmt.Errorf("entities must be an object")
			}
			tasks, _ := entities["tasks"].([]any)
			for _, raw := range tasks {
				t, _ := raw.(map[string]any)
				id, _ := t["id"].(string)
				if id == req.TaskID {
					return &ErrDuplicateTaskID{TaskID: req.TaskID}
				}
			}
			agents, _ := entities["agents"].([]any)
			known := map[string]bool{}
			for _, raw := range agents {
				agent, _ := raw.(map[string]any)
				id, _ := agent["id"].(string)
				known[id] = true
			}
			for _, owner := range req.OwnerAgentIDs {
				if !known[owner] {
					return fmt.Errorf("task owner %q is not registered", owner)
				}
			}
			owners := make([]any, len(req.OwnerAgentIDs))
			for i, owner := range req.OwnerAgentIDs {
				owners[i] = owner
			}
			newTask := map[string]any{
				"id":              req.TaskID,
				"state":           "candidate",
				"path":            taskRef,
				"sha256":          taskSHA,
				"owner_agent_ids": owners,
			}
			entities["tasks"] = append(tasks, newTask)
			state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			return nil
		},
	})
}
