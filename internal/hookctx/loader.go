package hookctx

import (
	"bytes"
	contextpkg "context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/agentauth"
	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/fileview"

	"github.com/entroforge/go-system-builder/internal/pathscope"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

type stateFile struct {
	SchemaVersion string `json:"schema_version"`
	RuntimeID     string `json:"runtime_id"`
	Revision      int    `json:"revision"`
	BoundREQ      *struct {
		Path     string `json:"path"`
		Metadata struct {
			UIImpact string `json:"ui_impact"`
		} `json:"metadata"`
	} `json:"bound_req"`
	Lifecycle *struct {
		State string `json:"state"`
		Phase string `json:"phase"`
	} `json:"lifecycle"`
	Baseline *struct {
		Generation int `json:"generation"`
	} `json:"baseline"`
	// Hook v1 dropped WarningCounters (REQ-004 §4.5). Hooks are stateless
	// and never promote warn → block, so the hook_control block is
	// intentionally absent from this loader. Other packages (semantic,
	// cli) carry their own typed HookControl struct; this one only
	// reads the fields the policy engine needs.
	Review *struct {
		Round      int `json:"round"`
		CleanRound any `json:"clean_round"`
		Document   any `json:"document_round,omitempty"`
		Build      any `json:"build_round,omitempty"`
		Verify     any `json:"verify_round,omitempty"`
		// L3-S7: the registered ReviewPlan pointer carries the verification
		// artifact workspace — the only product-adjacent write surface the
		// reviewer hard-deny rule allows during the verification stage.
		Plan *struct {
			Status                        string `json:"status"`
			VerificationArtifactWorkspace string `json:"verification_artifact_workspace"`
		} `json:"plan"`
		Repair map[string]any `json:"repair"`
	} `json:"review"`
	Pause *struct {
		Reason string `json:"reason,omitempty"`
	} `json:"pause"`
	Entities struct {
		Agents []struct {
			ID                    string            `json:"id"`
			State                 string            `json:"state"`
			LastLifecycleEvent    string            `json:"last_lifecycle_event"`
			DispatchMode          string            `json:"dispatch_mode"`
			PlanReportedRef       *string           `json:"plan_reported_ref"`
			ActivationRef         *string           `json:"activation_ref"`
			TaskIDs               []string          `json:"task_ids"`
			TeamID                *string           `json:"team_id"`
			PromptRef             *string           `json:"prompt_ref"`
			CompletionReportedRef *string           `json:"completion_reported_ref"`
			CompletionAckRef      *string           `json:"completion_acknowledged_ref"`
			ReadbackRef           *string           `json:"readback_ref"`
			WorkBlockedRef        *string           `json:"work_blocked_ref"`
			BlockerResolvedRef    *string           `json:"blocker_resolved_ref"`
			ShutdownApprovedRef   *string           `json:"shutdown_approved_ref"`
			AcknowledgedReports   map[string]string `json:"completion_acknowledged_reports"`
			// AssignmentID is not stored on the agent row; it is resolved
			// below from the workgroup manifest. Kept out of this struct —
			// the load path resolves it via loadAgentAssignmentID.
		} `json:"agents"`
		Bugs []struct {
			ID       string `json:"id"`
			State    string `json:"state"`
			Severity string `json:"severity"`
		} `json:"bugs"`
		Teams []struct {
			ID                string   `json:"id"`
			ManifestRef       string   `json:"manifest_ref"`
			ResponsibilityIDs []string `json:"responsibility_ids"`
			AgentIDs          []string `json:"agent_ids"`
		} `json:"teams"`
		Tasks []struct {
			ID                  string   `json:"id"`
			State               string   `json:"state"`
			LastLifecycleEvent  string   `json:"last_lifecycle_event"`
			Path                string   `json:"path"`
			SHA256              string   `json:"sha256"`
			OwnerAgentIDs       []string `json:"owner_agent_ids"`
			CompletionReportRef string   `json:"completion_report_ref"`
		} `json:"tasks"`
	} `json:"entities"`
	Evidence []struct {
		Status string `json:"status"`
	} `json:"evidence"`
	// Milestone is read for the optional active integration checkpoint
	// (SYNC-039 §6 / §8). The integration array is a deliberate slice of
	// unknown-shape strings/dicts in today’s runtime, so the loader reads
	// the milestone block as a free-form map and only cherry-picks the
	// fields the Integrator / Controller would consume later.
	Milestone map[string]any `json:"milestone"`
}

// lockedDocument is one element of state.documents[]. The runtime schema
// (loop-state.schema.json §documentReference) guarantees id/kind/path/version/
// sha256/status/generation, but Hook must not crash if any optional fields
// are absent — older fixtures omit version/status/generation and the loader
// skips incomplete rows rather than fabricating LockedArtifact entries.
type lockedDocument struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Path       string `json:"path"`
	Version    string `json:"version"`
	SHA256     string `json:"sha256"`
	Status     string `json:"status"`
	Generation int    `json:"generation"`
}

type activationFile struct {
	AgentID               string   `json:"agent_id"`
	AllowedTools          []string `json:"allowed_tools"`
	AllowedWritePaths     []string `json:"allowed_write_paths"`
	AllowedCommandClasses []string `json:"allowed_command_classes"`
}

type workgroupAssignment struct {
	AssignmentID       string   `json:"assignment_id"`
	ResponsibilityID   string   `json:"responsibility_id"`
	RoleFamily         string   `json:"role_family"`
	AgentID            string   `json:"agent_id"`
	AgentDefinitionRef string   `json:"agent_definition_ref"`
	DispatchMode       string   `json:"dispatch_mode,omitempty"`
	SkillRefs          []string `json:"skill_refs"`
	ReadPaths          []string `json:"read_paths"`
	DependsOn          []string `json:"depends_on"`
	// Scope is the manifest's declared write surface; WritePaths is the
	// schema-required binding (L3-S6 write-scope audit reads the real
	// diff against this). We accept both names so legacy manifests that
	// only carried `scope` still feed the audit instead of silently
	// declaring no scope.
	Scope                []string            `json:"scope"`
	WritePaths           []string            `json:"write_paths"`
	OutputPaths          []string            `json:"output_paths"`
	IntegrationCheckMode string              `json:"integration_check_mode,omitempty"`
	CheckProfiles        []checkexec.Profile `json:"check_profiles,omitempty"`
	RequiredChecks       []string            `json:"required_checks"`
	DoneWhen             []string            `json:"done_when"`
	Status               string              `json:"status"`
	// Worktree coordinates are optional extensions on the workgroup
	// assignment row (BUG-039-37 / BUG-039-04 residual). When present
	// the loader surfaces them; when absent they stay blank — never
	// fabricated.
	WorktreePath string `json:"worktree_path,omitempty"`
	Branch       string `json:"branch,omitempty"`
	TargetBranch string `json:"target_branch,omitempty"`
}

type workgroupManifest struct {
	SchemaVersion      string                `json:"schema_version"`
	ManifestID         string                `json:"manifest_id"`
	Version            string                `json:"version"`
	RuntimeID          string                `json:"runtime_id"`
	ReqID              string                `json:"req_id"`
	BaselineGeneration int                   `json:"baseline_generation"`
	Status             string                `json:"status"`
	WorkgroupID        string                `json:"workgroup_id"`
	WorkgroupKind      string                `json:"workgroup_kind"`
	Assignments        []workgroupAssignment `json:"assignments"`
	Documents          []struct {
		ID      string `json:"id"`
		Kind    string `json:"kind"`
		Path    string `json:"path"`
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
	} `json:"documents"`
	ResponsibilityDispositions []struct {
		ResponsibilityID string   `json:"responsibility_id"`
		Disposition      string   `json:"disposition"`
		AssignmentIDs    []string `json:"assignment_ids"`
		EvidenceRef      string   `json:"evidence_ref,omitempty"`
	} `json:"responsibility_dispositions"`
}

// Load returns the policy.RuntimeContext the Safety Policy already consumes,
// populated from .claude/loop-state.json + the optional activation envelope.
// It is the read-only projection Hook uses today; new code should prefer
// LoadFull, which surfaces locked artifacts and assignment context on
// LoadedContext without forcing the caller to re-read the runtime state.
//
// The function is preserved at its pre-BUG-039-04 signature so cli/run.go and
// hook/adapter.go (out-of-scope today) keep compiling. New callers — the
// Controller (BUG-02 next wave) and the Worktree Integrator (BUG-05 next
// wave) — must use LoadFull.
func Load(root, agentID string) (policy.RuntimeContext, error) {
	loaded, err := LoadFull(root, agentID)
	if err != nil {
		return policy.RuntimeContext{}, err
	}
	return loaded.PolicyContext, nil
}

// LoadFull returns the richer LoadedContext wrapper. It populates:
//
//   - PolicyContext: the existing RuntimeContext surface, now also with
//     LockedArtifacts derived from state.documents[] (BUG-039-04 §4.1).
//   - Assignments: one AssignmentContext per active task in the current
//     generation, with assignment_id/owner/worktree/branch/target_branch
//     sourced from .claude/workgroups/<REQ-ID>/<TASK>/manifest.json. Tasks
//     with state ∈ {candidate, reviewed, locked} are surfaced as
//     "structured-but-not-active" rows; tasks with state ∈
//     {in_progress, review, done, blocked} participate in the active
//     assignment set the Integrator uses for merge-back decisions.
//   - IntegrationCheckpoint: non-nil when an active worktree integration
//     exists in the current generation. It is read from milestone.integration
//     as a free-form map; absent entries yield nil. The Controller and
//     Integrator consume the non-nil pointer only.
//
// The load is strictly read-only (BUG-039-04 §4.2). No file in the runtime
// tree is mutated; if a manifest or integration block is unreadable, the
// loader drops the field rather than inventing data.
func LoadFull(root, agentID string) (*LoadedContext, error) {
	snapshot, err := runtime.NewStore(
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
	).Snapshot()
	if err != nil {
		if strings.Contains(err.Error(), "decode runtime") {
			return nil, fmt.Errorf("decode runtime state: %w", err)
		}
		return nil, fmt.Errorf("read runtime state: %w", err)
	}
	return FromSnapshot(root, agentID, snapshot)
}

// FromSnapshot projects the caller's original Writer snapshot without reading
// another Runtime revision. It grants no mutation or assignment authority.
func FromSnapshot(root, agentID string, snapshot runtime.Snapshot) (*LoadedContext, error) {
	binding, err := workspace.Decode(snapshot.State)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(snapshot.State)
	if err != nil {
		return nil, fmt.Errorf("encode runtime state: %w", err)
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode runtime state: %w", err)
	}
	manifestRefs := registeredManifestRefs(state)

	context := policy.RuntimeContext{
		Workspace:   binding,
		RuntimeID:   state.RuntimeID,
		Revision:    state.Revision,
		ProjectRoot: root,
	}
	if state.Baseline != nil {
		context.CurrentBaselineGeneration = state.Baseline.Generation
	}
	if state.BoundREQ != nil {
		context.BoundREQPath = state.BoundREQ.Path
		context.BoundREQUIImpact = state.BoundREQ.Metadata.UIImpact
	}
	if state.Lifecycle != nil {
		context.CurrentState = state.Lifecycle.State
		context.CurrentPhase = state.Lifecycle.Phase
		context.CurrentStage, _ = runtime.StageFor(context.CurrentState, context.CurrentPhase, root)
	}
	if state.Pause != nil {
		context.Paused = true
	}
	if state.Review != nil {
		context.CurrentReviewRound = state.Review.Round
		context.CleanRound = state.Review.CleanRound
		if state.Review.Plan != nil {
			context.VerificationWorkspace = state.Review.Plan.VerificationArtifactWorkspace
		}
		if state.Review.Repair != nil {
			context.RepairStatus, _ = state.Review.Repair["status"].(string)
			context.RepairSessionID, _ = state.Review.Repair["session_id"].(string)
			context.RepairPlanRef, _ = state.Review.Repair["plan_ref"].(string)
			context.RepairPlanSHA256, _ = state.Review.Repair["plan_sha256"].(string)
		}
	}
	for _, ev := range state.Evidence {
		if ev.Status == "valid" {
			context.EvidenceValidCount++
		}
	}
	const blockingSeverity = "P0"
	blockingStates := map[string]bool{
		"accepted": true, "assigned": true, "fixing": true,
		"retesting": true, "investigating": true, "pending_approval": true,
	}
	for _, bug := range state.Entities.Bugs {
		if bug.Severity == blockingSeverity && blockingStates[bug.State] {
			context.OpenBlockingBugs++
		}
	}
	for _, team := range state.Entities.Teams {
		summary := policy.TeamSummary{ManifestRef: team.ManifestRef}
		summary.ResponsibilityIDs = append(summary.ResponsibilityIDs, team.ResponsibilityIDs...)
		context.Teams = append(context.Teams, summary)
	}

	loaded := &LoadedContext{
		PolicyContext: context,
	}
	config, configErr := runtime.DispatchConfig(snapshot.State)
	if configErr != nil {
		return nil, configErr
	}
	loaded.DispatchBackpressure = config != nil

	if state.Baseline != nil {
		loaded.BaselineGeneration = state.Baseline.Generation
	}

	// Locked artifacts (BUG-039-04 §4.1).
	//
	// The runtime schema binds documents[] to a single generation via
	// documentReference.generation + .status. SYNC-039 §6 already calls out
	// that "all artifact/evidence/integration refs must belong to the same
	// generation", so the loader treats the current generation as
	// authoritative and ignores other-generations rows rather than splicing
	// them into the active manifest. Rows missing one of id/kind/path/
	// version/sha256/generation are dropped (BUG-039-04 §4.2 forbids
	// fabricating block decisions from partial data).
	var docs []lockedDocument
	if rawDocs, ok := snapshot.State["documents"].([]any); ok {
		for _, raw := range rawDocs {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			buf, err := json.Marshal(entry)
			if err != nil {
				continue
			}
			var doc lockedDocument
			if err := json.Unmarshal(buf, &doc); err != nil {
				continue
			}
			if doc.Status != "locked" && doc.Status != "active" {
				continue
			}
			// REQ baselines are immutable history: every locked generation
			// stays write-protected, not only the current baseline's entry
			// (after an amend the superseded REQ must remain locked).
			if doc.Kind != "req" && doc.Generation != loaded.BaselineGeneration {
				continue
			}
			if doc.ID == "" || doc.Kind == "" || doc.Path == "" ||
				doc.Version == "" || doc.SHA256 == "" ||
				doc.Generation == 0 {
				continue
			}
			docs = append(docs, doc)
		}
	}
	context.LockedArtifacts = LockedArtifactsFromSnapshot(snapshot)

	// Assignments (BUG-039-04 §4.1). We walk runtime.entities.tasks[] and,
	// for each row with a non-empty owner_agent_ids[0], attempt to read the
	// matching .claude/workgroups/<req>/<task>/manifest.json. A missing or
	// malformed manifest is logged as an error surfaced on the assignment
	// row but does not abort the load — the Controller can still observe
	// the runtime-resolvable fields (task_id, state).
	taskIndex := make(map[string]loadedTask, len(state.Entities.Tasks))
	recordResolver := assignmentRecordResolver{root: root, state: snapshot.State}
	for _, task := range state.Entities.Tasks {
		if task.ID == "" || task.State == "" {
			continue
		}
		version := state.SchemaVersion
		if version == "" {
			version = "1.2.0"
		}
		projected, err := workstate.TaskProjection(map[string]any{
			"state": task.State, "last_lifecycle_event": task.LastLifecycleEvent,
		}, version)
		if err != nil {
			return nil, fmt.Errorf("project TASK %s work state: %w", task.ID, err)
		}
		taskIndex[task.ID] = loadedTask{
			RuntimeID:     state.RuntimeID,
			State:         string(projected.Status),
			Activity:      projected.Activity,
			OwnerIDs:      append([]string(nil), task.OwnerAgentIDs...),
			CompletionRef: task.CompletionReportRef,
		}
	}
	supersededOwners, err := assignmentrecord.SupersededOwners(snapshot.State)
	if err != nil {
		return nil, fmt.Errorf("resolve current canonical owners: %w", err)
	}
	for owner := range supersededOwners {
		for _, task := range state.Entities.Tasks {
			if slices.Contains(task.OwnerAgentIDs, owner) {
				return nil, fmt.Errorf("superseded canonical Owner %s remains in current TASK ownership", owner)
			}
		}
	}
	if supersededOwners[agentID] {
		return nil, fmt.Errorf("canonical Agent %s no longer owns a current Assignment; use the new original dispatch", agentID)
	}
	for _, agent := range state.Entities.Agents {
		if supersededOwners[agent.ID] {
			continue
		}
		if len(agent.TaskIDs) == 0 {
			continue
		}
		// Iterate in deterministic order so snapshot diffs are stable
		// across revisions.
		taskIDs := append([]string(nil), agent.TaskIDs...)
		sort.Strings(taskIDs)
		for _, taskID := range taskIDs {
			idx, ok := taskIndex[taskID]
			if !ok {
				continue
			}
			agentInput := buildAgentRow{
				ID:                    agent.ID,
				CompletionReportedRef: optionalString(agent.CompletionReportedRef),
				CompletionAckRef:      optionalString(agent.CompletionAckRef),
				AcknowledgedReports:   agent.AcknowledgedReports,
				PromptRef:             optionalString(agent.PromptRef),
				TaskID:                taskID,
			}
			legacyRow := buildAssignmentRow(root, agentInput, idx, manifestRefs[agent.ID])
			row, canonical, err := recordResolver.project(agentInput, idx, manifestRefs[agent.ID], "", legacyRow)
			if err != nil {
				return nil, fmt.Errorf("load canonical Assignment for Agent %s / TASK %s: %w", agent.ID, taskID, err)
			}
			if !canonical {
				row = legacyRow
			}
			if row != nil {
				loaded.Assignments = append(loaded.Assignments, *row)
			}
		}
	}
	// Also surface tasks whose owner_agent_ids[] is non-empty even if no
	// matching agent row exists yet (rare — only during fresh bind before
	// agent activation). This keeps the active-assignment set complete
	// for the Controller.
	for _, task := range state.Entities.Tasks {
		if task.ID == "" || task.State == "" {
			continue
		}
		idx, ok := taskIndex[task.ID]
		if !ok || !isActiveTaskState(idx.State) {
			continue
		}
		if len(task.OwnerAgentIDs) == 0 {
			continue
		}
		// Avoid duplicating rows already added above.
		knownAgent := false
		for _, agent := range state.Entities.Agents {
			if agent.ID == task.OwnerAgentIDs[0] {
				knownAgent = true
				break
			}
		}
		if knownAgent || assignmentAlreadyPresent(loaded.Assignments, task.ID, task.OwnerAgentIDs[0]) {
			continue
		}
		owner := task.OwnerAgentIDs[0]
		legacyRow := buildAssignmentRowFromTask(root, task.ID, owner, manifestRefs[owner], idx)
		fallbackManifest := ""
		if legacyRow != nil {
			fallbackManifest = legacyRow.ManifestRef
		}
		row, canonical, err := recordResolver.project(
			buildAgentRow{ID: owner, TaskID: task.ID},
			idx,
			manifestRefs[owner], fallbackManifest, legacyRow,
		)
		if err != nil {
			return nil, fmt.Errorf("load canonical Assignment for Agent %s / TASK %s: %w", owner, task.ID, err)
		}
		if !canonical {
			row = legacyRow
		}
		if row != nil {
			row.CompletionRef = task.CompletionReportRef
			loaded.Assignments = append(loaded.Assignments, *row)
		}
	}
	sort.Slice(loaded.Assignments, func(i, j int) bool {
		return loaded.Assignments[i].AssignmentID < loaded.Assignments[j].AssignmentID
	})
	context.NativeOwnedAssignments = projectNativeOwnedAssignments(root, snapshot, loaded.Assignments, binding)

	// Agent activation (preserved from pre-BUG-039-04 behavior). The Hook
	// payload's lifecycle claim is untrusted (SYNC-039 §3): loader must
	// resolve the requesting agent against entities.agents[] and load the
	// referenced activation envelope before policy evaluation. Failure
	// paths here are documented by loader_test.go errors.
	if agentID != "" {
		var repairPointer map[string]any
		if state.Review != nil {
			repairPointer = state.Review.Repair
		}
		for _, agent := range state.Entities.Agents {
			if agent.ID != agentID {
				continue
			}
			version := state.SchemaVersion
			if version == "" {
				// A few pre-versioned Runtime fixtures and early legacy snapshots
				// use the 1.2 activity vocabulary. Canonical five-state values
				// still fail closed here because they require a matching event.
				version = "1.2.0"
			}
			projection, projectionErr := workstate.AgentProjection(map[string]any{
				"state": agent.State, "last_lifecycle_event": agent.LastLifecycleEvent,
			}, version)
			if projectionErr != nil {
				return nil, fmt.Errorf("project Agent %s work state: %w", agent.ID, projectionErr)
			}
			documentConflictRef := ""
			if projection.Status == workstate.Blocked && agent.LastLifecycleEvent == "document_conflict_reported" {
				// The original lifecycle consumer stores this evidence in readback_ref.
				documentConflictRef = optionalString(agent.ReadbackRef)
			}
			context.Agent = &policy.AgentContext{
				ID: agent.ID, State: string(projection.Status), Activity: projection.Activity,
				LifecycleEvent: agent.LastLifecycleEvent, DispatchMode: agent.DispatchMode,
				DocumentConflictRef: documentConflictRef,
				WorkBlockedRef:      optionalString(agent.WorkBlockedRef),
				BlockerResolvedRef:  optionalString(agent.BlockerResolvedRef),
				ShutdownApprovedRef: optionalString(agent.ShutdownApprovedRef),
			}
			if agent.PlanReportedRef != nil {
				context.Agent.PlanReportedRef = *agent.PlanReportedRef
			}
			// RC-04 (S7-3): surface the dispatched-Assignment facts on the
			// runtime projection itself so the L4 first-write barrier can be
			// evaluated on every PreToolUse path, including ones that carry
			// no AgentContext (e.g. the controller safety input). The
			// assignment is resolved from the same workgroup manifest the
			// Integrator reads (single deterministic owner rule); ambiguous
			// rows stay unresolved and the barrier stands down on the
			// AssignmentID fact but still sees the Agent fallback.
			// Keep the original workgroup-manifest Assignment separately from
			// the canonical S6 Assignment projection below. S9 packets bind both
			// identities, and their manifest assignment is the bridge back to
			// this registered Agent/Task dispatch.
			manifestAssignmentID := loadAgentAssignmentID(root, agent.TaskIDs, agent.ID, manifestRefs[agent.ID], state.RuntimeID)
			context.AssignmentID = manifestAssignmentID
			if record, found, recordErr := assignmentrecord.ForOwner(snapshot.State, agent.ID); recordErr != nil {
				return nil, fmt.Errorf("resolve canonical Assignment for Agent %s: %w", agent.ID, recordErr)
			} else if found {
				task, ok := taskIndex[record.Task.ID]
				if !ok || !slices.Contains(agent.TaskIDs, record.Task.ID) {
					return nil, fmt.Errorf("canonical Assignment %s TASK is absent from the registered Agent task set", record.AssignmentID)
				}
				row, canonical, projectionErr := recordResolver.project(buildAgentRow{ID: agent.ID, TaskID: record.Task.ID, PromptRef: optionalString(agent.PromptRef)}, task, manifestRefs[agent.ID], "", nil)
				if projectionErr != nil {
					return nil, fmt.Errorf("verify canonical Assignment for Agent %s: %w", agent.ID, projectionErr)
				}
				if !canonical || row == nil || row.AssignmentID != record.AssignmentID {
					return nil, fmt.Errorf("current canonical Assignment %s was not projected from the original source", record.AssignmentID)
				}
				context.AssignmentID = row.AssignmentID
			}
			if context.Agent != nil {
				context.Agent.AssignmentID = context.AssignmentID
			}
			if agent.PlanReportedRef != nil {
				context.PlanReportedRef = *agent.PlanReportedRef
			}

			if agent.DispatchMode == "plan_checkpoint" && context.Agent.PlanReportedRef != "" {
				if err := plancheckpoint.Validate(root, snapshot, agentID, context.Agent.PlanReportedRef); err != nil {
					context.Agent.PlanReportedRef = ""
					context.PlanReportedRef = ""
				} else if context.Agent.LifecycleActivity() == "activated" || context.Agent.LifecycleActivity() == "working" {
					if ref, err := plancheckpoint.ActivatedRef(root, snapshot, agentID); err != nil || ref == "" {
						context.Agent.PlanReportedRef = ""
						context.PlanReportedRef = ""
					} else {
						context.Agent.PlanReportedRef = ref
						context.PlanReportedRef = ref
					}
				}
			}
			if context.Agent.PlanReportedRef == "" && agent.DispatchMode == "plan_checkpoint" {
				// A legacy agent-begin may have committed activation but not the observer marker.
				// Invalid evidence stays unrecorded; normal plan/write gates remain closed.
				if ref, err := plancheckpoint.ActivatedRef(root, snapshot, agentID); err == nil && ref != "" {
					context.Agent.PlanReportedRef = ref
					context.PlanReportedRef = ref
				}
			}
			reviewMap, _ := snapshot.State["review"].(map[string]any)
			reviewRows, _ := reviewMap["assignments"].(map[string]any)
			reviewRow, _ := reviewRows[context.AssignmentID].(map[string]any)
			context.Agent.ReviewAssignment = reviewRow != nil && reviewRow["agent_id"] == agentID
			context.DispatchMode = agent.DispatchMode
			// L4 §15.2 P0-1: surface the dispatched task set, team and
			// registered completion ref so the TaskUpdate self-claim guard
			// and the TeammateIdle/SubagentStop control path can recognize
			// the exact teammate from runtime facts.
			context.Agent.TaskIDs = append([]string(nil), agent.TaskIDs...)
			if agent.TeamID != nil {
				context.Agent.TeamID = *agent.TeamID
			}
			if agent.CompletionReportedRef != nil {
				context.Agent.CompletionReportedRef = *agent.CompletionReportedRef
			}
			if agent.ActivationRef == nil {
				loadRepairAgentScope(root, snapshot.State, repairPointer, agent.ID, manifestAssignmentID, context.Agent)
				break
			}
			activation, err := loadActivation(root, *agent.ActivationRef)
			if err != nil {
				return nil, err
			}
			if activation.AgentID != agentID {
				return nil, fmt.Errorf("activation Agent %q does not match %q", activation.AgentID, agentID)
			}
			context.Agent.AllowedTools = activation.AllowedTools
			context.Agent.AllowedWritePaths = activation.AllowedWritePaths
			context.Agent.AllowedCommandClasses = activation.AllowedCommandClasses
			loadRepairAgentScope(root, snapshot.State, repairPointer, agent.ID, manifestAssignmentID, context.Agent)
			break
		}
		if context.Agent == nil {
			return nil, fmt.Errorf("Agent %q not found in runtime", agentID)
		}
	}
	// Mirror any Agent-context mutation back into the wrapper so that
	// downstream consumers of LoadedContext.PolicyContext observe the
	// activated Agent. Without this copy, callers reading PolicyContext
	// would still see Agent=nil (BUG-039-04 §4.1 expects the loaded
	// context to be self-consistent).
	context.PacketMode = runtime.PacketEnabled(snapshot.State)
	context.MainPlanningAnswerPaths = currentMainPlanningAnswerPaths(snapshot.State, agentID, context.Agent)
	if binding != nil && context.Agent != nil && context.Agent.ID == agentID && context.Agent.AssignmentID != "" {
		if e, ok := binding.Execution(context.Agent.AssignmentID, context.RuntimeID, context.CurrentBaselineGeneration); ok && e.AgentID == agentID && e.Status == "ready" {
			if paths, err := binding.WorkerSubmissionPaths(e); err == nil {
				context.WorkerSubmissionPaths = append(context.WorkerSubmissionPaths, paths.ControlPaths()...)
			}
		}
	}
	if context.PacketMode && agentID != "" && context.Agent != nil && context.Agent.ID == agentID {
		if packets, err := runtime.S5Packets(root, snapshot.State, agentID); err == nil {
			for _, p := range packets {
				if stored, issued, err := runtime.FindWorkPacket(snapshot.State, p.ID); err == nil && issued && reflect.DeepEqual(stored, p) {
					context.Agent.DocumentReviewAssignment = true
					if e, declared, err := workspace.ReviewExecution(snapshot.State, p); err == nil {
						if declared {
							if e.Status == "ready" {
								if binding != nil {
									if paths, err := binding.WorkerSubmissionPaths(e); err == nil {
										context.WorkPacketAnswerPaths = append(context.WorkPacketAnswerPaths, paths.Answer)
									}
								}
							}
						} else {
							context.WorkPacketAnswerPaths = append(context.WorkPacketAnswerPaths, p.AnswerPath)
						}
					}
				}
			}
		}
	}
	// S7 ReviewResult packets are a separate subtype in the original
	// work_packets registry. Project only the exact current answer path after
	// the same Runtime snapshot proves the owner and Assignment. Generic PLAN
	// authorization is not an input to this domain questionnaire path. The path
	// is only a projection of durable issuance;
	// review.SubmitIssuedResult repeats the full Plan/Assignment/journal proof
	// at the original consumer.
	if context.PacketMode && agentID != "" && context.Agent != nil &&
		context.Agent.ReviewAssignment && context.AssignmentID != "" &&
		s7ReviewResultFreshWriteStage(snapshot.State) {
		if packets, err := runtime.CurrentReviewResultPacketsContext(runtime.WithPacketBodyRoot(contextpkg.Background(), root), snapshot.State, agentID); err == nil {
			for _, packet := range packets {
				binding := packet.Binding
				if binding.OwnerAgentID != agentID || binding.AssignmentID != context.AssignmentID ||
					binding.RuntimeID != state.RuntimeID || binding.BaselineGeneration != loaded.BaselineGeneration || packet.AnswerPath == "" {
					continue
				}
				if !s7ReviewResultAssignmentWritable(snapshot.State, packet) {
					continue
				}
				stored, found, findErr := runtime.FindReviewResultPacketContext(runtime.WithPacketBodyRoot(contextpkg.Background(), root), snapshot.State, packet.ID)
				if findErr != nil || !found || !reflect.DeepEqual(stored, packet) {
					continue
				}
				if runtime.VerifyReviewResultPacketIssue(root, snapshot.State, packet) != nil {
					continue
				}
				context.ReviewResultAnswerPaths = append(context.ReviewResultAnswerPaths, packet.AnswerPath)
			}
		}
	}
	// S8 identity and fresh authoring are separate projections. An accepted
	// HypothesisResult still identifies the original registered Investigator
	// for Stop/Idle and read-only receipt recovery, but it never keeps a fresh
	// answer-file write grant. Only a still-open Case slot on the original
	// Case Assignment receives the exact answer path.
	if context.PacketMode && agentID != "" && context.Agent != nil && context.AssignmentID != "" {
		if packets, err := runtime.CurrentHypothesisResultPackets(root, snapshot.State, agentID); err == nil {
			freshStage := s8HypothesisResultFreshWriteStage(snapshot.State)
			for _, packet := range packets {
				binding := packet.Binding
				if binding.OwnerAgentID != agentID || binding.AssignmentID != context.AssignmentID ||
					binding.RuntimeID != state.RuntimeID || binding.BaselineGeneration != loaded.BaselineGeneration {
					continue
				}
				stored, found, findErr := runtime.FindHypothesisResultPacket(snapshot.State, packet.ID)
				if findErr != nil || !found || !reflect.DeepEqual(stored, packet) ||
					runtime.VerifyHypothesisResultPacketIssue(root, snapshot.State, packet) != nil {
					continue
				}
				context.Agent.HypothesisResultAssignment = true
				if freshStage && packet.AnswerPath != "" && s8HypothesisResultOpen(root, snapshot.State, packet) {
					context.HypothesisResultAnswerPaths = append(context.HypothesisResultAnswerPaths, packet.AnswerPath)
				}
			}
		}
		if !context.Agent.HypothesisResultAssignment && !s8HypothesisRouteConsumed(snapshot.State) {
			// A durable accepted receipt remains reportable if the current Case
			// question/route later moves and the current selector stops returning
			// it. This fallback grants no answer path; fresh writing still requires
			// the exact open slot above and original Case Assignment binding.
			if packets, err := runtime.HypothesisResultPackets(snapshot.State); err == nil {
				for _, packet := range packets {
					binding := packet.Binding
					if binding.OwnerAgentID != agentID || binding.AssignmentID != context.AssignmentID ||
						binding.RuntimeID != state.RuntimeID || binding.BaselineGeneration != loaded.BaselineGeneration ||
						binding.TeamID != context.Agent.TeamID || !slices.Contains(context.Agent.TaskIDs, binding.TaskID) {
						continue
					}
					if !s8LoadedAssignmentMatches(loaded.Assignments, binding.OwnerAgentID, binding.AssignmentID, binding.TaskID, binding.ManifestRef) {
						continue
					}
					if !s8RuntimeTeamMatches(snapshot.State, binding.TeamID, binding.ManifestRef) {
						continue
					}
					if runtime.VerifyHypothesisResultPacketIssue(root, snapshot.State, packet) != nil {
						continue
					}
					receipt, accepted, receiptErr := runtime.VerifiedHypothesisResultReceipt(root, snapshot.State, packet.ID, agentID)
					if receiptErr == nil && accepted && receipt.PacketID == packet.ID && receipt.OwnerAgentID == agentID {
						context.Agent.HypothesisResultAssignment = true
						break
					}
				}
			}
		}
	}
	// S9 PlanReport is an exact pre-execution control-plane answer. It is
	// projected only for a currently working Builder whose original activated
	// PLAN and repair dispatch both validate. S9 Worker check/worktree support
	// is not available in this slice, so a declared matching execution gets no
	// fallback grant from the Main checkout.
	if context.PacketMode && agentID != "" && context.Agent != nil &&
		context.Agent.ID == agentID && context.Agent.RepairAssignmentID != "" &&
		s9RepairPlanReportFreshWriteStage(snapshot.State) &&
		s9RepairPlanReportCanSubmit(root, snapshot.State, agentID) {
		if packets, packetErr := runtime.CurrentRepairPlanReportPackets(root, snapshot.State, agentID); packetErr == nil {
			for _, packet := range packets {
				binding := packet.Binding
				if binding.OwnerAgentID != agentID || binding.RepairAssignmentID != context.Agent.RepairAssignmentID ||
					binding.RuntimeID != state.RuntimeID || binding.BaselineGeneration != loaded.BaselineGeneration ||
					binding.ReviewRound != context.CurrentReviewRound || packet.AnswerPath == "" {
					continue
				}
				stored, found, findErr := runtime.FindRepairPlanReportPacketContext(runtime.WithPacketBodyRoot(contextpkg.Background(), root), snapshot.State, packet.ID)
				if findErr != nil || !found || !reflect.DeepEqual(stored, packet) ||
					runtime.VerifyRepairPlanReportPacketIssue(root, snapshot.State, packet) != nil ||
					runtime.VerifyRepairPlanReportCurrent(root, snapshot.State, packet) != nil ||
					!s9RepairPlanReportMainSourceOnly(snapshot.State, packet) {
					continue
				}
				// An accepted PlanReport remains available through the exact
				// read-only receipt/replay path, but it no longer grants a fresh
				// answer-file write. The original Writer re-reads bytes and checks
				// the saved digest before returning that receipt without mutation.
				_, accepted, receiptErr := runtime.VerifiedRepairPlanReportReceipt(root, snapshot.State, packet.ID, agentID)
				if receiptErr != nil {
					continue
				}
				if accepted && !s9RepairPlanReportCheckpointPending(snapshot.State, agentID) {
					continue
				}
				context.RepairPlanReportAnswerPaths = append(context.RepairPlanReportAnswerPaths, packet.AnswerPath)
			}
		}
	}
	// S9 responsibility is read-only current work information, independent
	// of caller authorization or source proofs. Only the fresh stage exposes
	// an unreceived answer; later phases retain the delivered responsibility.
	if context.PacketMode && agentID != "" && context.Agent != nil && context.Agent.ID == agentID {
		life, _ := snapshot.State["lifecycle"].(map[string]any)
		review, _ := snapshot.State["review"].(map[string]any)
		pointer, _ := review["repair"].(map[string]any)
		owners, _ := pointer["assignment_owners"].(map[string]any)
		assignmentID := ""
		for id, owner := range owners {
			if owner != agentID {
				continue
			}
			if assignmentID != "" {
				assignmentID = ""
				break
			}
			assignmentID = id
		}
		if life["state"] == "bug_resolution" && assignmentID != "" {
			// Only a resolved blocker retains its Result responsibility while
			// Main prepares a successor. A Plan-only owner is not yet Result work.
			if context.Agent.BlockerResolvedRef != "" {
				context.Agent.RepairAssignmentID = assignmentID
				context.Agent.RepairResultAssignment = true
			}
			raw, _ := json.Marshal(snapshot.State["workspace"])
			var registry workspace.ExecutionRegistry
			if json.Unmarshal(raw, &registry) == nil {
				if packets, err := runtime.CurrentRepairResultPackets(root, snapshot.State, agentID); err == nil {
					for _, packet := range packets {
						b := packet.Binding.Sources
						if b.OwnerAgentID != agentID || b.RepairAssignmentID != assignmentID ||
							b.RuntimeID != state.RuntimeID || b.BaselineGeneration != loaded.BaselineGeneration ||
							b.SessionID != pointer["session_id"] || b.PlanRef != pointer["plan_ref"] {
							continue
						}
						execution, ok := registry.Executions[b.ManifestAssignmentID]
						if !ok {
							execution, ok = registry.Executions[b.RepairAssignmentID]
						}
						var original workspace.Execution
						if !ok || json.Unmarshal(packet.Inputs.Execution, &original) != nil ||
							execution.AgentID != agentID || execution.RuntimeID != b.RuntimeID || execution.BaselineGeneration != b.BaselineGeneration ||
							execution.AssignmentID != original.AssignmentID || execution.Generation != original.Generation ||
							execution.Path != original.Path || execution.ReworkRef != packet.Binding.ReworkRef {
							continue
						}
						context.Agent.RepairAssignmentID = assignmentID
						context.Agent.RepairResultAssignment = true
						if s9RepairResultFreshWriteStage(snapshot.State) && execution.DeliveryRef == "" && packet.AnswerPath != "" {
							context.RepairResultAnswerPaths = append(context.RepairResultAnswerPaths, packet.AnswerPath)
						}
					}
				}
			}
		}
	}
	projectRepairResultResolution(root, snapshot, agentID, &context)
	loaded.PolicyContext = context

	// Integration checkpoint (BUG-039-04 §4.1). The runtime's
	// milestone.integration block today is `[]` for the active REQ
	// tree; we read it as an arbitrary slice and pick the first item
	// that looks like an integration record. Future loop-states that
	// land a real integration record will surface here.
	loaded.IntegrationCheckpoint = firstIntegrationCheckpoint(state.Milestone)

	return loaded, nil
}

func s8HypothesisResultFreshWriteStage(state map[string]any) bool {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	return lifecycle["state"] == "bug_resolution" && lifecycle["phase"] == "investigation"
}

func s9RepairPlanReportFreshWriteStage(state map[string]any) bool {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "bug_resolution" ||
		(lifecycle["phase"] != "planning" && lifecycle["phase"] != "reproducing") {
		return false
	}
	review, _ := state["review"].(map[string]any)
	repairPointer, _ := review["repair"].(map[string]any)
	return repairPointer["status"] == lifecycle["phase"]
}

func s9RepairPlanReportCanSubmit(root string, state map[string]any, owner string) bool {
	if agentauth.ValidateWorkingAuthorization(root, state, owner) == nil {
		return true
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	var found map[string]any
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil || row["id"] != owner {
			continue
		}
		if found != nil {
			return false
		}
		found = row
	}
	if found == nil || found["dispatch_mode"] != "plan_checkpoint" {
		return false
	}
	switch workstate.AgentActivity(found) {
	case "reading", "understanding_submitted", "activated":
		return true
	default:
		return false
	}
}

func s9RepairPlanReportCheckpointPending(state map[string]any, owner string) bool {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["id"] == owner {
			switch workstate.AgentActivity(row) {
			case "reading", "understanding_submitted", "activated":
				return true
			default:
				return false
			}
		}
	}
	return false
}

func s9RepairResultFreshWriteStage(state map[string]any) bool {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "bug_resolution" || lifecycle["phase"] != "fixing" {
		return false
	}
	review, _ := state["review"].(map[string]any)
	repairPointer, _ := review["repair"].(map[string]any)
	return repairPointer["status"] == "repairing"
}

func s9RepairResultPacketAttemptPresent(root string, state map[string]any, assignmentID, runtimeID string, generation, round int) (bool, error) {
	packets, err := runtime.RepairResultPacketsForSourcesContext(contextpkg.Background(), root, state, "")
	if err != nil {
		return false, err
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	if pointer == nil {
		return false, nil
	}
	sessionID, _ := pointer["session_id"].(string)
	planRef, _ := pointer["plan_ref"].(string)
	planSHA, _ := pointer["plan_sha256"].(string)
	for _, packet := range packets {
		binding := packet.Binding.Sources
		if binding.RepairAssignmentID == assignmentID && binding.RuntimeID == runtimeID &&
			binding.BaselineGeneration == generation && binding.ReviewRound == round &&
			binding.SessionID == sessionID && binding.PlanRef == planRef && binding.PlanSHA256 == planSHA {
			return true, nil
		}
	}
	return false, nil
}

func s9RepairPlanReportMainSourceOnly(state map[string]any, packet workpacket.RepairPlanReportPacket) bool {
	registry, err := workspace.Decode(state)
	if err != nil || registry == nil {
		return err == nil
	}
	if registry.ValidateAuthority(state) != nil {
		return false
	}
	for _, execution := range registry.AllExecutions() {
		if execution.RuntimeID == packet.Binding.RuntimeID &&
			execution.BaselineGeneration == packet.Binding.BaselineGeneration &&
			execution.AgentID == packet.Binding.OwnerAgentID &&
			(execution.AssignmentID == packet.Binding.ManifestAssignmentID || execution.AssignmentID == packet.Binding.RepairAssignmentID) {
			return false
		}
	}
	return true
}

func s8HypothesisRouteConsumed(state map[string]any) bool {
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	if pointer == nil {
		return false
	}
	consumedAt, _ := pointer["route_consumed_at"].(string)
	consumer, _ := pointer["route_consumer"].(string)
	return consumedAt != "" && consumer == "S8-ROUTE-CONSUME"
}

func s8LoadedAssignmentMatches(assignments []AssignmentContext, owner, assignmentID, taskID, manifestRef string) bool {
	count := 0
	for _, assignment := range assignments {
		if assignment.OwnerAgentID != owner || assignment.AssignmentID != assignmentID || assignment.TaskID != taskID ||
			assignment.RoleFamily != "investigator" || assignment.ManifestRef != manifestRef {
			continue
		}
		count++
	}
	return count == 1
}

func s8RuntimeTeamMatches(state map[string]any, teamID, manifestRef string) bool {
	entities, _ := state["entities"].(map[string]any)
	teams, _ := entities["teams"].([]any)
	count := 0
	for _, raw := range teams {
		team, _ := raw.(map[string]any)
		if team["id"] == teamID && team["kind"] == "investigator" && team["manifest_ref"] == manifestRef {
			count++
		}
	}
	return count == 1
}

func s8HypothesisResultOpen(root string, state map[string]any, packet workpacket.HypothesisResultPacket) bool {
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	if pointer == nil || pointer["case_id"] != packet.Binding.CaseID {
		return false
	}
	caseRef, _ := pointer["path"].(string)
	caseSHA, _ := pointer["sha256"].(string)
	data, err := workpacket.ReadFile(root, caseRef, 20<<20)
	if err != nil || caseSHA == "" || workpacket.Digest(data) != caseSHA || workpacket.ValidateUniqueJSON(data) != nil {
		return false
	}
	var document struct {
		CaseID                 string                                `json:"case_id"`
		Revision               int                                   `json:"revision"`
		SourceFindingIDs       []string                              `json:"source_finding_ids"`
		ObservationBatchID     string                                `json:"observation_batch_id"`
		ObservationBatchRef    string                                `json:"observation_batch_ref"`
		ObservationBatchSHA256 string                                `json:"observation_batch_sha256"`
		BaselineDigest         string                                `json:"baseline_digest"`
		Hypotheses             []workpacket.HypothesisResultQuestion `json:"hypotheses"`
		HypothesisResults      []struct {
			HypothesisID string `json:"hypothesis_id"`
			AssignmentID string `json:"assignment_id"`
		} `json:"hypothesis_results"`
	}
	if json.Unmarshal(data, &document) != nil || document.CaseID != packet.Binding.CaseID ||
		document.Revision != integerFact(pointer["revision"]) || document.Revision < packet.Binding.CaseRevision ||
		!stringSetEqual(document.SourceFindingIDs, packet.Binding.SourceFindingIDs) ||
		document.ObservationBatchID != packet.Binding.ObservationBatchID ||
		document.ObservationBatchRef != packet.Binding.ObservationBatchRef ||
		document.ObservationBatchSHA256 != packet.Binding.ObservationBatchSHA256 || document.BaselineDigest != packet.Binding.BaselineDigest {
		return false
	}
	var current *workpacket.HypothesisResultQuestion
	for _, hypothesis := range document.Hypotheses {
		if hypothesis.HypothesisID != packet.Binding.HypothesisID {
			continue
		}
		if current != nil {
			return false
		}
		copy := hypothesis
		current = &copy
	}
	if current == nil || current.AssignmentID != packet.Binding.AssignmentID || current.Status != "open" {
		return false
	}
	digest, err := workpacket.HypothesisQuestionDigest(*current)
	if err != nil || digest != packet.Binding.HypothesisQuestionSHA256 {
		return false
	}
	for _, result := range document.HypothesisResults {
		if result.HypothesisID == packet.Binding.HypothesisID && result.AssignmentID == packet.Binding.AssignmentID {
			// A reopened or edited Case cannot restore a consumed issuance's
			// fresh write permission. Replay is authorized only by the original
			// durable receipt path in the PostToolUse adapter.
			return false
		}
	}
	return true
}

func stringSetEqual(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	seen := make(map[string]bool, len(actual))
	for _, value := range actual {
		if value == "" || seen[value] {
			return false
		}
		seen[value] = true
	}
	for _, value := range expected {
		if value == "" || !seen[value] {
			return false
		}
	}
	return true
}

func integerFact(value any) int {
	switch n := value.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// s7ReviewResultFreshWriteStage prevents a terminal/historical packet row from
// becoming a fresh authoring grant just because its frozen Plan tuple is
// still readable. Post-consumption receipt replay uses the original receipt
// proof in the consumer and does not depend on this projection.
func s7ReviewResultFreshWriteStage(state map[string]any) bool {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "verification" {
		return false
	}
	review, _ := state["review"].(map[string]any)
	plan, _ := review["plan"].(map[string]any)
	switch plan["status"] {
	case "running", "cannot_clean", "discovery_draining":
		return true
	default:
		return false
	}
}

func s7ReviewResultAssignmentWritable(state map[string]any, packet workpacket.ReviewResultPacket) bool {
	review, _ := state["review"].(map[string]any)
	assignments, _ := review["assignments"].(map[string]any)
	row, _ := assignments[packet.Binding.AssignmentID].(map[string]any)
	if row == nil || row["agent_id"] != packet.Binding.OwnerAgentID || row["result_ref"] != nil {
		return false
	}
	version, _ := state["schema_version"].(string)
	switch workstate.ReviewAssignmentActivity(row, version) {
	case "dispatched", "blocked":
		return true
	default:
		return false
	}
}

// repairPlanHook is the deliberately small, read-only projection needed by
// the S9 Hook barrier. The authoritative artifact is still validated by the
// repair package at each Runtime command; this loader only verifies the
// pointed bytes and extracts the assignment scope before a tool call.
type repairPlanHook struct {
	Assignments []repairPlanHookAssignment `json:"assignments"`
}

type repairPlanHookAssignment struct {
	AssignmentID string   `json:"assignment_id"`
	OwnerAgentID string   `json:"owner_agent_id"`
	Scope        []string `json:"scope"`
}

type repairPlanReportHook struct {
	AgentID      string `json:"agent_id"`
	AssignmentID string `json:"assignment_id"`
}

// loadRepairAgentScope binds a Worker to the S9 assignment proven by its
// current issued packet or legacy PlanReport. In packet mode, the pointer's
// assignment_owners entry is joined to the exact registered workgroup
// manifest Assignment, Team, and Task carried by the issued packet; a
// RepairPlan row's optional owner_agent_id is never treated as authority.
func loadRepairAgentScope(root string, state map[string]any, pointer map[string]any, agentID, manifestAssignmentID string, agent *policy.AgentContext) {
	if agent == nil || pointer == nil || agentID == "" {
		return
	}
	planPath, _ := pointer["plan_ref"].(string)
	planSHA, _ := pointer["plan_sha256"].(string)
	planBytes, ok := readRepairHookArtifact(root, planPath, planSHA)
	if !ok {
		return
	}
	var plan repairPlanHook
	if json.Unmarshal(planBytes, &plan) != nil {
		return
	}
	if runtime.PacketEnabled(state) {
		loadCurrentRepairAgentScope(root, state, pointer, plan, agentID, manifestAssignmentID, agent)
		return
	}

	assignmentID := ""
	for _, assignment := range plan.Assignments {
		if assignment.OwnerAgentID == agentID {
			assignmentID = assignment.AssignmentID
			agent.RepairAllowedWritePaths = append([]string(nil), assignment.Scope...)
			break
		}
	}
	if refs, ok := pointer["plan_report_refs"].([]any); ok {
		for _, raw := range refs {
			ref, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			path, _ := ref["path"].(string)
			sha, _ := ref["sha256"].(string)
			data, valid := readRepairHookArtifact(root, path, sha)
			if !valid {
				continue
			}
			var report repairPlanReportHook
			if json.Unmarshal(data, &report) != nil || report.AgentID != agentID {
				continue
			}
			assignmentID = report.AssignmentID
			for _, assignment := range plan.Assignments {
				if assignment.AssignmentID != assignmentID {
					continue
				}
				agent.RepairAllowedWritePaths = append([]string(nil), assignment.Scope...)
				break
			}
			agent.RepairPlanReportRef = path
			break
		}
	}
	if assignmentID != "" {
		agent.RepairAssignmentID = assignmentID
	}
}

func loadCurrentRepairAgentScope(root string, state, pointer map[string]any, plan repairPlanHook, agentID, manifestAssignmentID string, agent *policy.AgentContext) {
	if agent == nil || agentID == "" || manifestAssignmentID == "" || agent.ID != agentID || agent.TeamID == "" {
		return
	}
	owners, _ := pointer["assignment_owners"].(map[string]any)
	packets, err := runtime.CurrentRepairPlanReportPackets(root, state, agentID)
	if err != nil {
		return
	}
	var selected *workpacket.RepairPlanReportPacket
	var selectedScope []string
	for _, packet := range packets {
		stored, found, findErr := runtime.FindRepairPlanReportPacketContext(runtime.WithPacketBodyRoot(contextpkg.Background(), root), state, packet.ID)
		if findErr != nil || !found || !reflect.DeepEqual(stored, packet) ||
			runtime.VerifyRepairPlanReportPacketIssue(root, state, packet) != nil {
			continue
		}
		assignment, matches := repairAssignmentForPacket(plan, owners, packet, agentID, manifestAssignmentID, agent)
		if !matches {
			continue
		}
		if selected != nil && selected.Binding.RepairAssignmentID != packet.Binding.RepairAssignmentID {
			return
		}
		copyPacket := packet
		selected = &copyPacket
		selectedScope = append([]string(nil), assignment.Scope...)
	}
	if selected == nil {
		return
	}
	agent.RepairAssignmentID = selected.Binding.RepairAssignmentID
	agent.RepairAllowedWritePaths = selectedScope
}

func repairAssignmentForPacket(plan repairPlanHook, owners map[string]any, packet workpacket.RepairPlanReportPacket, agentID, manifestAssignmentID string, agent *policy.AgentContext) (repairPlanHookAssignment, bool) {
	binding := packet.Binding
	if agent == nil || agent.ID != agentID || agentID == "" || manifestAssignmentID == "" || agent.TeamID == "" ||
		binding.OwnerAgentID != agentID || binding.ManifestAssignmentID != manifestAssignmentID ||
		binding.TeamID != agent.TeamID || !slices.Contains(agent.TaskIDs, binding.TaskID) ||
		owners[binding.RepairAssignmentID] != agentID {
		return repairPlanHookAssignment{}, false
	}
	var selected repairPlanHookAssignment
	found := false
	for _, assignment := range plan.Assignments {
		if assignment.AssignmentID != binding.RepairAssignmentID {
			continue
		}
		if found {
			return repairPlanHookAssignment{}, false
		}
		selected = assignment
		found = true
	}
	if !found || (selected.OwnerAgentID != "" && selected.OwnerAgentID != agentID) {
		return repairPlanHookAssignment{}, false
	}
	return selected, true
}

func readRepairHookArtifact(root, relative, expectedSHA string) ([]byte, bool) {
	if strings.TrimSpace(relative) == "" || strings.TrimSpace(expectedSHA) == "" {
		return nil, false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, false
	}
	abs, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(relative)))
	if err != nil {
		return nil, false
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, false
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, false
	}
	actual := sha256.Sum256(data)
	if fmt.Sprintf("%x", actual[:]) != expectedSHA {
		return nil, false
	}
	return data, true
}

type loadedTask struct {
	RuntimeID     string
	CompletionRef string
	State         string
	Activity      string
	OwnerIDs      []string
}

// assignmentRecordResolver only opens the original declared development view
// when the Runtime carries a Record or a registered manifest opts in. Legacy
// manifests keep their existing disk projection behavior.
type assignmentRecordResolver struct {
	root        string
	state       map[string]any
	view        *fileview.View
	viewAttempt bool
	viewErr     error
}

func (r *assignmentRecordResolver) originalView() (*fileview.View, error) {
	if r.viewAttempt {
		return r.view, r.viewErr
	}
	r.viewAttempt = true
	if err := fileview.ValidateAuthority(r.root, r.state); err != nil {
		r.viewErr = err
		return nil, err
	}
	branch, err := fileview.DevelopmentRef(r.state)
	if err != nil {
		r.viewErr = err
		return nil, err
	}
	definition, ok := r.state["definition"].(map[string]any)
	definitionPath, _ := definition["path"].(string)
	definitionSHA, _ := definition["sha256"].(string)
	if !ok || definitionPath != projectlayout.Definition || len(definitionSHA) != 64 {
		r.viewErr = fmt.Errorf("Runtime Loop Definition path/SHA binding is missing or noncanonical")
		return nil, r.viewErr
	}
	physicalDefinition, err := workpacket.ReadFile(r.root, definitionPath, 4<<20)
	if err != nil || workpacket.Digest(physicalDefinition) != definitionSHA {
		r.viewErr = fmt.Errorf("Runtime Loop Definition checkout SHA does not match the original source")
		return nil, r.viewErr
	}
	catalog, err := transition.LoadCatalog(r.root)
	if err != nil {
		r.viewErr = fmt.Errorf("load original Loop Definition file sources: %w", err)
		return nil, r.viewErr
	}
	r.view, r.viewErr = fileview.New(r.root, branch, catalog.Definition.FileSources)
	if r.viewErr != nil {
		return nil, r.viewErr
	}
	viewDefinition, err := r.view.ReadFile(projectlayout.Definition)
	if err != nil || workpacket.Digest(viewDefinition) != definitionSHA {
		r.viewErr = fmt.Errorf("original file-source view Loop Definition differs from the Runtime checkout SHA")
		return nil, r.viewErr
	}
	return r.view, r.viewErr
}

func (r *assignmentRecordResolver) project(agent buildAgentRow, idx loadedTask, refs registeredManifestRef, fallbackManifest string, legacy *AssignmentContext) (*AssignmentContext, bool, error) {
	record, found, err := assignmentrecord.ForOwner(r.state, agent.ID)
	if err != nil {
		return nil, false, err
	}
	if found && record.Task.ID == agent.TaskID {
		return r.projectRecord(agent, idx, refs, record, legacy)
	}

	marker, err := r.manifestOptIn(refs, fallbackManifest)
	if err != nil {
		return nil, false, err
	}
	if marker {
		if found {
			return nil, true, fmt.Errorf("current canonical Assignment for Agent %s is bound to TASK %s, not %s", agent.ID, record.Task.ID, agent.TaskID)
		}
		return nil, true, fmt.Errorf("manifest opts into %s but the current Runtime has no canonical Assignment Record for Agent %s / TASK %s", assignmentrecord.AssignmentRecordContract, agent.ID, agent.TaskID)
	}
	return nil, false, nil
}

func (r *assignmentRecordResolver) projectRecord(agent buildAgentRow, idx loadedTask, refs registeredManifestRef, record assignmentrecord.Record, legacy *AssignmentContext) (*AssignmentContext, bool, error) {
	view, err := r.originalView()
	if err != nil {
		return nil, true, fmt.Errorf("open original Assignment source view: %w", err)
	}
	manifestBytes, err := view.ReadFile(record.Manifest.Ref)
	if err != nil {
		return nil, true, fmt.Errorf("read canonical Assignment manifest %s from original view: %w", record.Manifest.Ref, err)
	}
	enabled, err := manifestRecordOptIn(manifestBytes)
	if err != nil {
		return nil, true, err
	}
	if !enabled {
		return nil, true, fmt.Errorf("canonical Assignment Record %s points to a manifest without the explicit S6 contract marker", record.AssignmentID)
	}
	if err := assignmentrecord.VerifySources(record, func(ref string) ([]byte, error) {
		if strings.HasPrefix(ref, assignmentinputs.SchemaPrefix) || ref == assignmentinputs.ContextProfileRef {
			return assignmentinputs.Read(r.root, ref)
		}
		committed, err := view.ReadFile(ref)
		if err != nil {
			return nil, err
		}
		physical, err := assignmentinputs.Read(r.root, ref)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(committed, physical) {
			return nil, fmt.Errorf("original checkout bytes differ from the declared development file view")
		}
		return committed, nil
	}); err != nil {
		return nil, true, fmt.Errorf("verify canonical Assignment sources: %w", err)
	}
	if err := view.Verify(); err != nil {
		return nil, true, fmt.Errorf("verify original Assignment source checkout: %w", err)
	}

	for _, ref := range assignmentManifestPaths(refs, "") {
		if ref != record.Manifest.Ref {
			return nil, true, fmt.Errorf("registered manifest ref %s differs from canonical Assignment manifest %s", ref, record.Manifest.Ref)
		}
	}
	var manifest workgroupManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, true, fmt.Errorf("decode canonical Assignment manifest: %w", err)
	}
	if manifest.RuntimeID != record.RuntimeID || manifest.BaselineGeneration != record.BaselineGeneration || manifest.WorkgroupID != record.TeamID || manifest.WorkgroupKind != "builder" || manifest.ManifestID != record.Manifest.ID || manifest.Version != record.Manifest.Version {
		return nil, true, fmt.Errorf("canonical Assignment manifest identity differs from its Record")
	}
	assignment := selectManifestAssignment(&manifest, agent.ID, agent.TaskID, record.AssignmentID)
	if assignment == nil || assignment.AssignmentID != record.AssignmentID || assignment.AgentID != record.OwnerAgentID || assignment.RoleFamily != record.RoleFamily || assignment.AgentDefinitionRef != record.AgentDefinition.Ref || assignment.ResponsibilityID != record.ResponsibilityID {
		return nil, true, fmt.Errorf("canonical Assignment manifest row differs from its registered Record")
	}
	dispatchMode := assignment.DispatchMode
	if dispatchMode == "" {
		dispatchMode = "plan_checkpoint"
	}
	if dispatchMode != record.DispatchMode || !sameSortedStrings(assignment.SkillRefs, record.SkillRefs) || !sameSortedStrings(assignment.ReadPaths, record.ReadPaths) || !sameSortedStrings(assignment.DependsOn, record.DependsOn) || !sameSortedStrings(assignment.DoneWhen, record.DoneWhen) || !sameSortedStrings(assignment.RequiredChecks, record.RequiredChecks) ||
		!sameSortedStrings(assignment.Scope, record.ScopeBasis.ManifestScope) || !sameSortedStrings(assignment.WritePaths, record.ScopeBasis.ManifestWritePaths) || !sameSortedStrings(assignment.OutputPaths, record.ScopeBasis.ManifestOutputPaths) || !sameCheckProfiles(assignment.CheckProfiles, record.CheckProfiles) {
		return nil, true, fmt.Errorf("canonical Assignment manifest fields differ from its registered Record")
	}
	taskDocuments := 0
	for _, document := range manifest.Documents {
		if document.ID != record.Task.ID {
			continue
		}
		taskDocuments++
		if document.Path != record.Task.Ref || document.Version != record.Task.Version || document.SHA256 != record.Task.SHA256 {
			return nil, true, fmt.Errorf("canonical Assignment manifest TASK reference differs from its registered Record")
		}
	}
	if taskDocuments != 1 {
		return nil, true, fmt.Errorf("canonical Assignment manifest must bind exactly one recorded TASK document")
	}

	row := &AssignmentContext{TaskID: agent.TaskID, OwnerAgentID: agent.ID, State: idx.State, ManifestRef: record.Manifest.Ref, BaselineGeneration: record.BaselineGeneration}
	if legacy != nil {
		row.CompletionRef = legacy.CompletionRef
		row.CompletionAckRef = legacy.CompletionAckRef
		row.AcknowledgedReports = legacy.AcknowledgedReports
	}
	if idx.CompletionRef != "" {
		for _, owner := range idx.OwnerIDs {
			if owner == agent.ID && (len(idx.OwnerIDs) == 1 || ownCompletionReport(r.root, idx.CompletionRef, agent.ID, agent.TaskID)) {
				row.CompletionRef = idx.CompletionRef
				break
			}
		}
	}
	fillAssignmentRow(row, *assignment)
	row.ManifestRef = record.Manifest.Ref
	row.BaselineGeneration = record.BaselineGeneration
	projection, err := agentcontext.ApplyRecord(agentcontext.Assignment{AssignmentID: row.AssignmentID}, record)
	if err != nil {
		return nil, true, err
	}
	row.AssignmentID = projection.AssignmentID
	row.OwnerAgentID = projection.Owner
	row.TaskID = projection.TaskID
	row.RoleFamily = projection.Role
	row.AgentDefinitionRef = projection.AgentDefinitionRef
	row.ManifestRef = projection.ManifestRef
	row.ResponsibilityIDs = append([]string(nil), projection.ResponsibilityIDs...)
	row.SkillRefs = append([]string(nil), projection.AuthorizationSkillRefs...)
	row.ReadPaths = append([]string(nil), projection.ReadPaths...)
	row.OutputPaths = append([]string(nil), projection.DeclaredOutputs...)
	row.WritePaths = append([]string(nil), projection.EffectiveWritePaths...)
	row.DependsOn = append([]string(nil), projection.DependsOn...)
	row.DoneWhen = append([]string(nil), projection.DoneWhen...)
	row.RequiredChecks = append([]string(nil), projection.RequiredChecks...)
	row.CheckProfiles = append([]checkexec.Profile(nil), record.CheckProfiles...)
	row.AssignmentRecord = projection.AssignmentRecord
	row.AssignmentRecordSHA256 = projection.AssignmentRecordSHA256
	row.AssignmentRevision = projection.AssignmentRevision
	if err := projectCanonicalExecutionCoordinates(r.state, record, row); err != nil {
		return nil, true, err
	}
	if record.Contract == assignmentrecord.NativeDeliveryContract {
		row.WorktreePath, row.Branch, row.TargetBranch = "", "", ""
		if coords, ok := loadAssignmentSidecar(r.root, row.AssignmentID); ok && coords.AssignmentRevision == record.Revision {
			applyAssignmentCoords(row, coords.WorktreePath, coords.Branch, coords.TargetBranch)
		}
	}

	return row, true, nil
}

// projectCanonicalExecutionCoordinates keeps the immutable Assignment
// manifest's planned coordinates separate from the current workspace
// execution's materialized coordinates. For a bound Runtime, only the
// current execution can authorize a worktree coordinate; a missing or
// non-ready execution clears the manifest's planned values. Legacy unbound
// projects retain the manifest projection.
func projectCanonicalExecutionCoordinates(state map[string]any, record assignmentrecord.Record, row *AssignmentContext) error {
	if row == nil || record.Contract == assignmentrecord.NativeDeliveryContract {
		return nil
	}
	binding, err := workspace.Decode(state)
	if err != nil {
		return fmt.Errorf("decode workspace for canonical Assignment coordinates: %w", err)
	}
	if binding == nil {
		return nil
	}
	clear := func() {
		row.WorktreePath, row.Branch, row.TargetBranch = "", "", ""
	}
	execution, exists := binding.Executions[record.AssignmentID]
	if !exists {
		clear()
		return nil
	}
	recordRef := record.Reference()
	recordSHA, err := record.Digest()
	if err != nil {
		return fmt.Errorf("digest current canonical Assignment coordinates: %w", err)
	}
	if execution.AssignmentID != record.AssignmentID || execution.RuntimeID != record.RuntimeID ||
		execution.BaselineGeneration != record.BaselineGeneration || execution.AgentID != record.OwnerAgentID ||
		execution.AssignmentRecord == nil || *execution.AssignmentRecord != recordRef ||
		execution.AssignmentRecordSHA256 != recordSHA {
		return fmt.Errorf("workspace execution differs from current canonical Assignment coordinates")
	}
	if execution.Status != "ready" && execution.Status != "complete" {
		clear()
		return nil
	}
	row.WorktreePath, row.Branch, row.TargetBranch = execution.Path, execution.Branch, execution.TargetBranch
	return nil
}

// manifestOptIn reads registered refs through the original file view where
// possible. If the view is unavailable, disk bytes are used only to detect an
// explicit opt-in and force a fail-closed error; they never authorize a Record.
func (r *assignmentRecordResolver) manifestOptIn(refs registeredManifestRef, fallback string) (bool, error) {
	candidates := assignmentManifestPaths(refs, fallback)
	if len(candidates) == 0 {
		return false, nil
	}
	if view, err := r.originalView(); err == nil {
		read := false
		marker := false
		for _, candidate := range candidates {
			data, readErr := view.ReadFile(candidate)
			if readErr != nil {
				continue
			}
			read = true
			enabled, markerErr := manifestRecordOptIn(data)
			if markerErr != nil {
				return false, markerErr
			}
			marker = marker || enabled
		}
		if read {
			if err := view.Verify(); err != nil {
				return false, fmt.Errorf("verify original Assignment manifest view: %w", err)
			}
			return marker, nil
		}
	}
	for _, candidate := range candidates {
		data, err := workpacket.ReadFile(r.root, candidate, 2<<20)
		if err != nil {
			continue
		}
		enabled, markerErr := manifestRecordOptIn(data)
		if markerErr != nil {
			return false, markerErr
		}
		if enabled {
			return false, fmt.Errorf("S6 manifest %s cannot be verified against the original development source view", candidate)
		}
	}
	return false, nil
}

func manifestRecordOptIn(data []byte) (bool, error) {
	enabled, err := assignmentrecord.ManifestAssignmentRecordEnabled(data)
	if err != nil {
		if bytes.Contains(data, []byte(`"assignment_record_contract"`)) {
			return false, fmt.Errorf("decode explicit Assignment Record contract marker: %w", err)
		}
		return false, nil // preserve legacy handling for malformed unmarked manifests
	}
	return enabled, nil
}

func sameSortedStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	l, r := append([]string{}, left...), append([]string{}, right...)
	sort.Strings(l)
	sort.Strings(r)
	return stringSlicesEqual(l, r)
}

func sameCheckProfiles(left, right []checkexec.Profile) bool {
	if len(left) != len(right) {
		return false
	}
	if len(left) == 0 {
		return true
	}
	l, err := json.Marshal(left)
	if err != nil {
		return false
	}
	r, err := json.Marshal(right)
	return err == nil && bytes.Equal(l, r)
}

func assignmentManifestPaths(refs registeredManifestRef, fallback string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, ref := range []string{refs.PromptRef, refs.TeamRef, fallback} {
		path, _, _ := strings.Cut(strings.TrimSpace(ref), "#")
		path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
		if path == "" || path == "." || path == ".." || strings.HasPrefix(path, "../") || filepath.IsAbs(path) || seen[path] {
			continue
		}
		seen[path] = true
		result = append(result, path)
	}
	return result
}

// ResolveAssignmentRecordProjection revalidates a Hook projection against the
// current Runtime Record and original source view before native-context use.
func ResolveAssignmentRecordProjection(root string, state map[string]any, assignment AssignmentContext) (assignmentrecord.Record, bool, error) {
	resolver := assignmentRecordResolver{root: root, state: state}
	record, found, err := assignmentrecord.ForOwner(state, assignment.OwnerAgentID)
	if err != nil {
		return assignmentrecord.Record{}, false, err
	}
	projected := assignment.AssignmentRecord != nil || assignment.AssignmentRecordSHA256 != "" || assignment.AssignmentRevision != 0
	if !found {
		if projected {
			return assignmentrecord.Record{}, false, fmt.Errorf("Assignment Record projection has no current Runtime Record")
		}
		marker, err := resolver.manifestOptIn(registeredManifestRef{}, assignment.ManifestRef)
		if err != nil {
			return assignmentrecord.Record{}, false, err
		}
		if marker {
			return assignmentrecord.Record{}, false, fmt.Errorf("S6 manifest has no current canonical Assignment Record")
		}
		return assignmentrecord.Record{}, false, nil
	}
	if record.Task.ID != assignment.TaskID {
		if projected {
			return assignmentrecord.Record{}, false, fmt.Errorf("Assignment Record TASK %s differs from projected TASK %s", record.Task.ID, assignment.TaskID)
		}
		return assignmentrecord.Record{}, false, nil
	}
	expected, canonical, err := resolver.projectRecord(buildAgentRow{ID: assignment.OwnerAgentID, TaskID: assignment.TaskID}, loadedTask{}, registeredManifestRef{}, record, &assignment)
	if err != nil {
		return assignmentrecord.Record{}, false, err
	}
	sha, err := record.Digest()
	if err != nil {
		return assignmentrecord.Record{}, false, err
	}
	if !canonical || expected == nil || !projected || assignment.AssignmentRecord == nil || *assignment.AssignmentRecord != record.Reference() || assignment.AssignmentRecordSHA256 != sha || assignment.AssignmentRevision != record.Revision ||
		assignment.AssignmentID != expected.AssignmentID || assignment.OwnerAgentID != expected.OwnerAgentID || assignment.TaskID != expected.TaskID || assignment.ManifestRef != expected.ManifestRef || assignment.BaselineGeneration != expected.BaselineGeneration ||
		assignment.RoleFamily != expected.RoleFamily || assignment.AgentDefinitionRef != expected.AgentDefinitionRef || !stringSlicesEqual(assignment.ResponsibilityIDs, expected.ResponsibilityIDs) ||
		!stringSlicesEqual(assignment.SkillRefs, expected.SkillRefs) || !stringSlicesEqual(assignment.ReadPaths, expected.ReadPaths) || !stringSlicesEqual(assignment.OutputPaths, expected.OutputPaths) ||
		!stringSlicesEqual(assignment.DependsOn, expected.DependsOn) || !stringSlicesEqual(assignment.DoneWhen, expected.DoneWhen) || !stringSlicesEqual(assignment.WritePaths, expected.WritePaths) ||
		!stringSlicesEqual(assignment.RequiredChecks, expected.RequiredChecks) || !sameCheckProfiles(assignment.CheckProfiles, expected.CheckProfiles) {
		return assignmentrecord.Record{}, false, fmt.Errorf("AssignmentContext differs from the current canonical Assignment Record")
	}
	return record, true, nil
}

func stringSlicesEqual(left, right []string) bool {
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

// registeredManifestRef carries the two control-plane pointers that can
// identify an assignment's team manifest. The Agent prompt pointer is more
// specific; the registered Team pointer is the second authoritative source.
// Empty pointers are deliberately retained as the signal to use the legacy
// canonical fallback only when no registered manifest path exists.
type registeredManifestRef struct {
	PromptRef string
	TeamRef   string
}

// buildAgentRow is the closure-friendly copy of one entities.agents[]
// row that buildAssignmentRow consumes. It exists to avoid passing an
// anonymous-struct value across function boundaries (which Go forbids) and
// keeps the loader-table types discoverable in one place.
type buildAgentRow struct {
	AcknowledgedReports   map[string]string
	ID                    string
	PromptRef             string
	CompletionReportedRef string
	CompletionAckRef      string
	TaskID                string
}

// loadAgentAssignmentID resolves the dispatched Assignment for one agent row
// (RC-04 S7-3). It reuses the workgroup manifest the Integrator reads: for
// each of the agent's task ids the manifest must name exactly one assignment
// row owned by the agent (or a single unbound row). The first deterministic
// match wins; ambiguous multi-assignment rows resolve to "" so callers never
// invent a binding the manifest does not prove.
func loadAgentAssignmentID(root string, taskIDs []string, agentID string, refs registeredManifestRef, runtimeID string) string {
	for _, taskID := range taskIDs {
		row := buildAssignmentRow(root, buildAgentRow{ID: agentID, TaskID: taskID, PromptRef: refs.PromptRef}, loadedTask{RuntimeID: runtimeID}, refs)
		if row != nil {
			return row.AssignmentID
		}
	}
	return ""
}

// buildAssignmentRow materializes one AssignmentContext for an
// agent×task pair. The agent entry is the row in entities.agents[] and
// supplies completion_reported / completion_acknowledged refs. The
// matching workgroup manifest supplies assignment_id, write_paths,
// responsibility_ids and the worktree/branch coordinates.
func buildAssignmentRow(root string, agent buildAgentRow, idx loadedTask, refs registeredManifestRef) *AssignmentContext {
	row := &AssignmentContext{
		TaskID:              agent.TaskID,
		OwnerAgentID:        agent.ID,
		State:               idx.State,
		CompletionRef:       agent.CompletionReportedRef,
		CompletionAckRef:    agent.CompletionAckRef,
		AcknowledgedReports: agent.AcknowledgedReports,
		ManifestRef:         agent.PromptRef,
	}
	// TASK-scoped canonical Results also recover pre-fix runtimes whose
	// agent pointer still names a wire message. Never borrow another owner's result.
	if idx.CompletionRef != "" {
		for _, owner := range idx.OwnerIDs {
			if owner == agent.ID {
				// A TASK may have multiple owners. Its last report cannot
				// replace another owner's original canonical report pointer.
				if len(idx.OwnerIDs) == 1 || ownCompletionReport(root, idx.CompletionRef, agent.ID, agent.TaskID) {
					row.CompletionRef = idx.CompletionRef
				}
				break
			}
		}
	}
	path, manifest, fragment := assignmentManifestForRefs(root, agent.TaskID, refs)
	if manifest == nil || (idx.RuntimeID != "" && manifest.RuntimeID != "" && manifest.RuntimeID != idx.RuntimeID) {
		return nil
	}
	assignment := selectManifestAssignment(manifest, agent.ID, agent.TaskID, fragment)
	if assignment == nil {
		return nil
	}
	row.ManifestRef = path
	row.BaselineGeneration = manifest.BaselineGeneration
	fillAssignmentRow(row, *assignment)

	// Authoritative fallbacks when the workgroup row omits coordinates:
	// assignment sidecar and/or a durable integration checkpoint. Never
	// invent paths that are not present on disk (BUG-039-04 §4.2).
	enrichAssignmentCoords(root, row)
	if row.AssignmentID == "" {
		return nil
	}
	return row
}

func ownCompletionReport(root, ref, owner, taskID string) bool {
	data, err := workpacket.ReadFile(root, ref, 1<<20)
	if err != nil {
		return false
	}
	var report struct {
		Producer string `json:"producer_agent_id"`
		Agent    string `json:"agent_id"`
		TaskID   string `json:"task_id"`
	}
	if json.Unmarshal(data, &report) != nil {
		return false
	}
	return report.TaskID == taskID && (report.Producer == owner || report.Agent == owner)
}

// buildAssignmentRowFromTask is the fallback used when entities.agents[]
// has no entry for the task's owner_agent_ids[0]. It only emits a row if
// the manifest exists AND names an assignment, which prevents spurious
// rows in cases where the owner agent has not yet been spawned.
func buildAssignmentRowFromTask(root, taskID, ownerAgentID string, refs registeredManifestRef, task loadedTask) *AssignmentContext {
	path, manifest, _ := assignmentManifestForRefs(root, taskID, refs)
	if manifest == nil {
		return nil
	}
	selected := selectManifestAssignment(manifest, ownerAgentID, taskID, "")
	if selected == nil {
		return nil
	}
	a := *selected

	row := &AssignmentContext{
		AssignmentID:         a.AssignmentID,
		TaskID:               taskID,
		OwnerAgentID:         ownerAgentID,
		RoleFamily:           a.RoleFamily,
		AgentDefinitionRef:   a.AgentDefinitionRef,
		State:                task.State,
		ManifestRef:          path,
		ReportStatus:         a.Status,
		WritePaths:           assignmentWritePaths(a.WritePaths, a.OutputPaths, a.Scope),
		RequiredChecks:       append([]string(nil), a.RequiredChecks...),
		CheckProfiles:        append([]checkexec.Profile(nil), a.CheckProfiles...),
		IntegrationCheckMode: a.IntegrationCheckMode,
		DoneWhen:             append([]string(nil), a.DoneWhen...),
		ResponsibilityIDs:    []string{a.ResponsibilityID},
		SkillRefs:            append([]string{}, a.SkillRefs...), ReadPaths: append([]string{}, a.ReadPaths...), OutputPaths: append([]string{}, a.OutputPaths...), DependsOn: append([]string{}, a.DependsOn...),
	}
	applyAssignmentCoords(row, a.WorktreePath, a.Branch, a.TargetBranch)
	enrichAssignmentCoords(root, row)
	return row
}

// applyAssignmentCoords copies non-empty worktree coordinates onto the
// assignment row. Empty source values are ignored so a later authoritative
// fallback can still fill them.
func applyAssignmentCoords(row *AssignmentContext, worktreePath, branch, targetBranch string) {
	if row == nil {
		return
	}
	if worktreePath != "" && row.WorktreePath == "" {
		row.WorktreePath = worktreePath
	}
	if branch != "" && row.Branch == "" {
		row.Branch = branch
	}
	if targetBranch != "" && row.TargetBranch == "" {
		row.TargetBranch = targetBranch
	}
}

// enrichAssignmentCoords fills blank worktree coordinates from
// `.claude/assignments/<id>.json` and/or a durable integration checkpoint
// under `.claude/evidence/.../worktree/<id>/checkpoint.json`. Missing
// files leave the fields blank.
func enrichAssignmentCoords(root string, row *AssignmentContext) {
	if row == nil || row.AssignmentID == "" {
		return
	}

	// Bound runtimes have a single coordinate authority. Missing/stale
	// execution records must not fall back to an old sidecar or checkpoint.
	if binding, state, err := workspace.Load(root); err == nil && binding != nil {
		row.WorktreePath, row.Branch, row.TargetBranch = "", "", ""
		if e, ok := binding.Execution(row.AssignmentID, workspace.RuntimeID(state), workspace.Generation(state)); ok && e.AgentID == row.OwnerAgentID && (e.Status == "ready" || e.Status == "complete") {
			row.WorktreePath, row.Branch, row.TargetBranch = e.Path, e.Branch, e.TargetBranch
		}
		return
	}
	if row.WorktreePath != "" && row.Branch != "" && row.TargetBranch != "" {
		return
	}
	if path, ok := loadAssignmentSidecar(root, row.AssignmentID); ok {
		applyAssignmentCoords(row, path.WorktreePath, path.Branch, path.TargetBranch)
	}
	if row.WorktreePath != "" && row.Branch != "" && row.TargetBranch != "" {
		return
	}
	if cp := loadAssignmentCheckpointCoords(root, row.AssignmentID); cp != nil {
		applyAssignmentCoords(row, cp.WorktreePath, cp.Branch, cp.TargetBranch)
	}
}

type assignmentCoordFile struct {
	AssignmentRevision int    `json:"assignment_revision"`
	WorktreePath       string `json:"worktree_path"`
	Branch             string `json:"branch"`
	TargetBranch       string `json:"target_branch"`
}

// assignmentWritePaths resolves the write-scope binding for a manifest
// row: schema-required write_paths first, with the legacy `scope` field as
// the declared fallback (a scope-only manifest must still feed the L3-S6
// write-scope audit instead of declaring no scope).
func assignmentWritePaths(writePaths, outputPaths, scope []string) []string {
	if len(writePaths) == 0 {
		writePaths = scope
	}
	return pathscope.EffectiveWrites(writePaths, outputPaths)
}

func loadAssignmentSidecar(root, assignmentID string) (assignmentCoordFile, bool) {
	candidates := []string{
		filepath.Join(root, ".claude", "assignments", assignmentID+".json"),
		filepath.Join(root, ".claude", "assignments", assignmentID, "manifest.json"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var coords assignmentCoordFile
		if err := json.Unmarshal(data, &coords); err != nil {
			continue
		}
		if coords.WorktreePath == "" && coords.Branch == "" && coords.TargetBranch == "" {
			continue
		}
		return coords, true
	}
	return assignmentCoordFile{}, false
}

func loadAssignmentCheckpointCoords(root, assignmentID string) *assignmentCoordFile {
	if assignmentID == "" || filepath.Base(assignmentID) != assignmentID {
		return nil
	}
	// A legacy fallback may only consult this active Runtime/generation.
	// Never search historical evidence directories for a reusable assignment ID.
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return nil
	}
	runtimeID := workspace.RuntimeID(snapshot.State)
	if runtimeID == "" || filepath.Base(runtimeID) != runtimeID {
		return nil
	}
	generation := workspace.Generation(snapshot.State)
	path := filepath.Join(root, ".claude/evidence", runtimeID, fmt.Sprintf("g%d", generation), "worktree", assignmentID, "checkpoint.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	if stringFromAny(raw["assignment_id"]) != assignmentID {
		return nil
	}
	gen, ok := raw["baseline_generation"].(float64)
	if !ok || int(gen) != generation {
		return nil
	}
	coords := &assignmentCoordFile{WorktreePath: stringFromAny(raw["worktree_path"]), Branch: firstNonEmpty(stringFromAny(raw["source_branch"]), stringFromAny(raw["branch"])), TargetBranch: stringFromAny(raw["target_branch"])}
	if coords.WorktreePath == "" || coords.Branch == "" || coords.TargetBranch == "" {
		return nil
	}
	return coords
}

func stringFromAny(v any) string {
	s, _ := v.(string)
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// registeredManifestRefs projects the registered Agent/Team pointers into a
// per-owner lookup. The Agent prompt ref is preferred; a Team manifest_ref is
// the registered fallback. A team without an id is usable only when it names
// the Agent explicitly through agent_ids; conflicting references remain
// ambiguous.
func registeredManifestRefs(state stateFile) map[string]registeredManifestRef {
	byTeamID := make(map[string]string, len(state.Entities.Teams))
	ambiguousTeam := make(map[string]bool)
	byAgentID := make(map[string]string)
	ambiguousAgent := make(map[string]bool)
	for _, team := range state.Entities.Teams {
		ref := strings.TrimSpace(team.ManifestRef)
		if ref == "" {
			continue
		}
		teamID := strings.TrimSpace(team.ID)
		if teamID != "" && !ambiguousTeam[teamID] {
			if previous, ok := byTeamID[teamID]; ok && previous != ref {
				delete(byTeamID, teamID)
				ambiguousTeam[teamID] = true
			} else {
				byTeamID[teamID] = ref
			}
		}
		for _, agentID := range team.AgentIDs {
			agentID = strings.TrimSpace(agentID)
			if agentID == "" {
				continue
			}
			if previous, ok := byAgentID[agentID]; ok && previous != ref {
				delete(byAgentID, agentID)
				ambiguousAgent[agentID] = true
				continue
			}
			if !ambiguousAgent[agentID] {
				byAgentID[agentID] = ref
			}
		}
	}
	refs := make(map[string]registeredManifestRef, len(state.Entities.Agents))
	for _, agent := range state.Entities.Agents {
		agentID := strings.TrimSpace(agent.ID)
		ref := registeredManifestRef{}
		if agent.PromptRef != nil {
			ref.PromptRef = strings.TrimSpace(*agent.PromptRef)
		}
		if agent.TeamID != nil {
			teamID := strings.TrimSpace(*agent.TeamID)
			if !ambiguousTeam[teamID] {
				ref.TeamRef = byTeamID[teamID]
			}
		} else if !ambiguousAgent[agentID] {
			// A Team without an id may still bind an Agent explicitly through
			// its agent_ids list. No positional or single-team guess is safe.
			ref.TeamRef = byAgentID[agentID]
		}
		refs[agent.ID] = ref
	}
	// A task can briefly outlive its Agent row during activation/recovery. A
	// Team's explicit agent_ids still bind that owner to the registered Team
	// manifest; this is an identity lookup, not a positional Team guess.
	for agentID, teamRef := range byAgentID {
		if _, exists := refs[agentID]; !exists {
			refs[agentID] = registeredManifestRef{TeamRef: teamRef}
		}
	}
	return refs
}

// loadWorkgroupManifestForRefs resolves a registered manifest before using
// the old task-shaped canonical path. Once a formal .json reference exists,
// a missing or malformed file is a closed lookup: guessing another caller's
// task path could cross owners and apply the wrong write scope.
func loadWorkgroupManifestForRefs(root, taskID string, refs registeredManifestRef) (string, *workgroupManifest) {
	for _, ref := range []string{refs.PromptRef, refs.TeamRef} {
		path, formal := registeredManifestPath(root, ref)
		if !formal {
			continue
		}
		if path == "" {
			return "", nil
		}
		return readWorkgroupManifest(path, root)
	}
	return loadWorkgroupManifest(root, taskID)
}

// registeredManifestPath identifies a repository manifest path embedded in a
// registered ref (the Agent form may carry `#assignment-id`). Non-manifest
// legacy prompt labels such as `manifest#assignment-id` deliberately return
// formal=false so the canonical fallback remains available.
func registeredManifestPath(root, ref string) (string, bool) {
	path := strings.TrimSpace(ref)
	if path == "" {
		return "", false
	}
	if before, _, ok := strings.Cut(path, "#"); ok {
		path = strings.TrimSpace(before)
	}
	if !strings.HasSuffix(strings.ToLower(path), ".json") {
		return "", false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", true
	}
	pathAbs := path
	if !filepath.IsAbs(pathAbs) {
		pathAbs = filepath.Join(rootAbs, filepath.FromSlash(pathAbs))
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", true
	}
	return pathAbs, true
}

func readWorkgroupManifest(path, root string) (string, *workgroupManifest) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}
	var manifest workgroupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", nil
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", nil
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", nil
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", nil
	}
	return filepath.ToSlash(filepath.Clean(rel)), &manifest
}

// loadWorkgroupManifest reads .claude/workgroups/<REQ-ID>/<task>/manifest.json
// from the project root. The function returns (path-or-empty, manifest-or-nil);
// callers distinguish "manifest not present" from "manifest present but
// unparseable" by checking only the manifest pointer.
//
// We must NOT block the load on a missing manifest: many legitimate
// pre-publication states reference tasks whose workgroup folder does not yet
// exist (e.g. TASK-039-05 … TASK-039-09 before Wave C Builder activation).
func loadWorkgroupManifest(root, taskID string) (string, *workgroupManifest) {
	if root == "" || taskID == "" {
		return "", nil
	}
	manifestDir := filepath.Join(root, ".claude", "workgroups")
	entries, err := os.ReadDir(manifestDir)
	if err != nil {
		return "", nil
	}
	activeREQ := reqIDFromRuntime(root)
	if activeREQ != "UNBOUND" {
		path := filepath.Join(manifestDir, activeREQ, taskID, "manifest.json")
		return readWorkgroupManifest(path, root)
	}
	var foundPath string
	var foundManifest *workgroupManifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		reqDir := filepath.Join(manifestDir, entry.Name())
		path := filepath.Join(reqDir, taskID, "manifest.json")
		if manifestPath, manifest := readWorkgroupManifest(path, root); manifest != nil {
			if foundManifest != nil {
				// Without a bound REQ, more than one task-shaped manifest is
				// ambiguous. Do not let directory order select another owner.
				return "", nil
			}
			foundPath, foundManifest = manifestPath, manifest
		}
	}
	return foundPath, foundManifest
}

// lockedFromStageFor maps a document kind to the lifecycle stage at which
// the canonical flat-path copy becomes immutable (BE-039 §6.1 / SYNC-039 §10).
//
// S2 = bound REQ (req) — locked at REQ bind, never auto-promotable.
// S6 = design / contract / task — locked at GATE-DOCUMENT-PASS / TR-003.
// S7 / S10 = review-grade evidence (qa, e2e, acceptance, release_audit,
// bug) — locked only after their respective PASS evidence lands.
//
// The mapping is conservative: anything not in the table is treated as
// S6 (mid-build lock). The hook only blocks once the loader actually
// surfaces the artifact, so an unmapped kind still triggers block via the
// existing lockedArtifactDecision code path.
func lockedFromStageFor(kind string) string {
	switch kind {
	case "req":
		return "S2"
	case "design", "ui_baseline", "ui_prototype", "contract", "task", "dispatch_plan", "team_manifest":
		return "S6"
	case "review", "qa", "e2e", "acceptance", "release_audit", "bug":
		return "S7"
	default:
		return "S6"
	}
}

// firstIntegrationCheckpoint cherry-picks the first non-empty integration
// record from milestone.integration. The runtime currently emits an empty
// array for REQ-039, so this returns nil until the Worktree Integration
// (BUG-05) starts persisting records.
func firstIntegrationCheckpoint(milestone map[string]any) *IntegrationCheckpoint {
	if milestone == nil {
		return nil
	}
	raw, ok := milestone["integration"].([]any)
	if !ok {
		return nil
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		buf, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		var checkpoint IntegrationCheckpoint
		if err := json.Unmarshal(buf, &checkpoint); err != nil {
			continue
		}
		if checkpoint.AssignmentID == "" && checkpoint.Status == "" {
			continue
		}
		// Normalize the status field — fixtures sometimes carry
		// strings without going through status enums. We keep whatever
		// the Runtime wrote; only nil-guard here.
		return &checkpoint
	}
	return nil
}

func isActiveTaskState(state string) bool {
	switch state {
	case string(workstate.Active), string(workstate.Blocked), string(workstate.Done):
		return true
	default:
		return false
	}
}

func assignmentAlreadyPresent(rows []AssignmentContext, taskID, ownerAgentID string) bool {
	for _, row := range rows {
		if row.TaskID == taskID && row.OwnerAgentID == ownerAgentID {
			return true
		}
	}
	return false
}

func optionalString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// reqIDFromRuntime reads the runtime and returns the bound REQ id, used
// only as a display hint in fallback assignment rows. Returns "UNBOUND"
// when the runtime is unreadable or nothing is bound — a label, never a
// plausible foreign REQ id.
func reqIDFromRuntime(root string) string {
	snapshot, err := runtime.NewStore(
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
	).Snapshot()
	if err != nil {
		return "UNBOUND"
	}
	bound, ok := snapshot.State["bound_req"].(map[string]any)
	if ok {
		id, _ := bound["id"].(string)
		if id != "" {
			return id
		}
	}
	// Older fixtures may not carry bound_req but still use the canonical
	// loop-REQ-* runtime identity. It is safe to derive this exact REQ suffix;
	// all other unbound runtimes remain ambiguous and use the unique-candidate
	// legacy fallback in loadWorkgroupManifest.
	if runtimeID, _ := snapshot.State["runtime_id"].(string); strings.HasPrefix(runtimeID, "loop-REQ-") {
		return strings.TrimPrefix(runtimeID, "loop-")
	}
	return "UNBOUND"
}

func loadActivation(root, ref string) (activationFile, error) {
	path := ref
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return activationFile{}, fmt.Errorf("read activation: %w", err)
	}
	var activation activationFile
	if err := json.Unmarshal(data, &activation); err != nil {
		return activationFile{}, fmt.Errorf("decode activation: %w", err)
	}
	return activation, nil
}

// LockedArtifactsFromState projects the runtime state's documents[] into
// the policy.LockedArtifact list using the same selection rules as the
// hook transport: status locked/active, non-req kinds only in the current
// baseline generation, every locked req generation kept (immutable
// history). Shared with the controller's final-safety input so the wire
// path and the hook transport agree on what is locked.
func LockedArtifactsFromSnapshot(snapshot runtime.Snapshot) []policy.LockedArtifact {
	generation := 0
	if baseline, ok := snapshot.State["baseline"].(map[string]any); ok {
		switch v := baseline["generation"].(type) {
		case float64:
			generation = int(v)
		case int:
			generation = v
		}
	}
	var docs []lockedDocument
	if rawDocs, ok := snapshot.State["documents"].([]any); ok {
		for _, entry := range rawDocs {
			if entry == nil {
				continue
			}
			buf, err := json.Marshal(entry)
			if err != nil {
				continue
			}
			var doc lockedDocument
			if err := json.Unmarshal(buf, &doc); err != nil {
				continue
			}
			if doc.Status != "locked" && doc.Status != "active" {
				continue
			}
			if doc.Kind != "req" && doc.Generation != generation {
				continue
			}
			if doc.ID == "" || doc.Kind == "" || doc.Path == "" ||
				doc.Version == "" || doc.SHA256 == "" ||
				doc.Generation == 0 {
				continue
			}
			docs = append(docs, doc)
		}
	}
	if len(docs) == 0 {
		return nil
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	artifacts := make([]policy.LockedArtifact, 0, len(docs))
	for _, doc := range docs {
		artifacts = append(artifacts, policy.LockedArtifact{
			ID:                 doc.ID,
			Kind:               doc.Kind,
			Path:               doc.Path,
			Version:            doc.Version,
			SHA256:             doc.SHA256,
			LockedFromStage:    lockedFromStageFor(doc.Kind),
			BaselineGeneration: doc.Generation,
		})
	}
	return artifacts
}
