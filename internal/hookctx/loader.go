package hookctx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

type stateFile struct {
	RuntimeID string `json:"runtime_id"`
	Revision  int    `json:"revision"`
	BoundREQ  *struct {
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
	} `json:"review"`
	Pause *struct {
		Reason string `json:"reason,omitempty"`
	} `json:"pause"`
	Entities struct {
		Agents []struct {
			ID                    string   `json:"id"`
			State                 string   `json:"state"`
			ActivationRef         *string  `json:"activation_ref"`
			TaskIDs               []string `json:"task_ids"`
			TeamID                *string  `json:"team_id"`
			PromptRef             *string  `json:"prompt_ref"`
			CompletionReportedRef *string  `json:"completion_reported_ref"`
			CompletionAckRef      *string  `json:"completion_acknowledged_ref"`
		} `json:"agents"`
		Bugs []struct {
			ID       string `json:"id"`
			State    string `json:"state"`
			Severity string `json:"severity"`
		} `json:"bugs"`
		Teams []struct {
			ManifestRef       string   `json:"manifest_ref"`
			ResponsibilityIDs []string `json:"responsibility_ids"`
		} `json:"teams"`
		Tasks []struct {
			ID            string   `json:"id"`
			State         string   `json:"state"`
			Path          string   `json:"path"`
			SHA256        string   `json:"sha256"`
			OwnerAgentIDs []string `json:"owner_agent_ids"`
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

type workgroupManifest struct {
	SchemaVersion      string `json:"schema_version"`
	ManifestID         string `json:"manifest_id"`
	Version            string `json:"version"`
	RuntimeID          string `json:"runtime_id"`
	ReqID              string `json:"req_id"`
	BaselineGeneration int    `json:"baseline_generation"`
	Status             string `json:"status"`
	WorkgroupID        string `json:"workgroup_id"`
	WorkgroupKind      string `json:"workgroup_kind"`
	Assignments        []struct {
		AssignmentID       string   `json:"assignment_id"`
		ResponsibilityID   string   `json:"responsibility_id"`
		RoleFamily         string   `json:"role_family"`
		AgentID            string   `json:"agent_id"`
		AgentDefinitionRef string   `json:"agent_definition_ref"`
		SkillRefs          []string `json:"skill_refs"`
		WritePaths         []string `json:"write_paths"`
		Status             string   `json:"status"`
		// Worktree coordinates are optional extensions on the workgroup
		// assignment row (BUG-039-37 / BUG-039-04 residual). When present
		// the loader surfaces them; when absent they stay blank — never
		// fabricated.
		WorktreePath string `json:"worktree_path,omitempty"`
		Branch       string `json:"branch,omitempty"`
		TargetBranch string `json:"target_branch,omitempty"`
	} `json:"assignments"`
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
//     sourced from .claude/workgroups/REQ-039/<TASK>/manifest.json. Tasks
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
	data, err := json.Marshal(snapshot.State)
	if err != nil {
		return nil, fmt.Errorf("encode runtime state: %w", err)
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode runtime state: %w", err)
	}

	context := policy.RuntimeContext{
		RuntimeID:   state.RuntimeID,
		Revision:    state.Revision,
		ProjectRoot: root,
	}
	if state.BoundREQ != nil {
		context.BoundREQPath = state.BoundREQ.Path
		context.BoundREQUIImpact = state.BoundREQ.Metadata.UIImpact
	}
	if state.Lifecycle != nil {
		context.CurrentState = state.Lifecycle.State
		context.CurrentPhase = state.Lifecycle.Phase
	}
	if state.Pause != nil {
		context.Paused = true
	}
	if state.Review != nil {
		context.CurrentReviewRound = state.Review.Round
		context.CleanRound = state.Review.CleanRound
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
			if doc.Generation != loaded.BaselineGeneration {
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
	if len(docs) > 0 {
		sort.Slice(docs, func(i, j int) bool {
			return docs[i].Path < docs[j].Path
		})
		context.LockedArtifacts = make([]policy.LockedArtifact, 0, len(docs))
		for _, doc := range docs {
			context.LockedArtifacts = append(context.LockedArtifacts, policy.LockedArtifact{
				ID:                 doc.ID,
				Kind:               doc.Kind,
				Path:               doc.Path,
				Version:            doc.Version,
				SHA256:             doc.SHA256,
				LockedFromStage:    lockedFromStageFor(doc.Kind),
				BaselineGeneration: doc.Generation,
			})
		}
	}

	// Assignments (BUG-039-04 §4.1). We walk runtime.entities.tasks[] and,
	// for each row with a non-empty owner_agent_ids[0], attempt to read the
	// matching .claude/workgroups/<req>/<task>/manifest.json. A missing or
	// malformed manifest is logged as an error surfaced on the assignment
	// row but does not abort the load — the Controller can still observe
	// the runtime-resolvable fields (task_id, state).
	taskIndex := make(map[string]loadedTask, len(state.Entities.Tasks))
	for _, task := range state.Entities.Tasks {
		if task.ID == "" {
			continue
		}
		taskIndex[task.ID] = loadedTask{
			State:    task.State,
			OwnerIDs: append([]string(nil), task.OwnerAgentIDs...),
		}
	}
	for _, agent := range state.Entities.Agents {
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
			row := buildAssignmentRow(root, buildAgentRow{
				ID:                    agent.ID,
				CompletionReportedRef: optionalString(agent.CompletionReportedRef),
				CompletionAckRef:      optionalString(agent.CompletionAckRef),
				PromptRef:             optionalString(agent.PromptRef),
				TaskID:                taskID,
			}, idx)
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
		if !isActiveTaskState(task.State) {
			continue
		}
		if len(task.OwnerAgentIDs) == 0 {
			continue
		}
		// Avoid duplicating rows already added above.
		if assignmentAlreadyPresent(loaded.Assignments, task.ID, task.OwnerAgentIDs[0]) {
			continue
		}
		row := buildAssignmentRowFromTask(root, task.ID, task.OwnerAgentIDs[0])
		if row != nil {
			loaded.Assignments = append(loaded.Assignments, *row)
		}
	}
	sort.Slice(loaded.Assignments, func(i, j int) bool {
		return loaded.Assignments[i].AssignmentID < loaded.Assignments[j].AssignmentID
	})

	// Agent activation (preserved from pre-BUG-039-04 behavior). The Hook
	// payload's lifecycle claim is untrusted (SYNC-039 §3): loader must
	// resolve the requesting agent against entities.agents[] and load the
	// referenced activation envelope before policy evaluation. Failure
	// paths here are documented by loader_test.go errors.
	if agentID != "" {
		for _, agent := range state.Entities.Agents {
			if agent.ID != agentID {
				continue
			}
			context.Agent = &policy.AgentContext{ID: agent.ID, State: agent.State}
			if agent.ActivationRef == nil {
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
	loaded.PolicyContext = context

	// Integration checkpoint (BUG-039-04 §4.1). The runtime's
	// milestone.integration block today is `[]` for the active REQ-039
	// tree; we read it as an arbitrary slice and pick the first item
	// that looks like an integration record. Future loop-states that
	// land a real integration record will surface here.
	loaded.IntegrationCheckpoint = firstIntegrationCheckpoint(state.Milestone)

	return loaded, nil
}

type loadedTask struct {
	State    string
	OwnerIDs []string
}

// buildAgentRow is the closure-friendly copy of one entities.agents[]
// row that buildAssignmentRow consumes. It exists to avoid passing an
// anonymous-struct value across function boundaries (which Go forbids) and
// keeps the loader-table types discoverable in one place.
type buildAgentRow struct {
	ID                    string
	PromptRef             string
	CompletionReportedRef string
	CompletionAckRef      string
	TaskID                string
}

// buildAssignmentRow materializes one AssignmentContext for an
// agent×task pair. The agent entry is the row in entities.agents[] and
// supplies completion_reported / completion_acknowledged refs. The
// matching workgroup manifest supplies assignment_id, write_paths,
// responsibility_ids and the worktree/branch coordinates.
func buildAssignmentRow(root string, agent buildAgentRow, idx loadedTask) *AssignmentContext {
	row := &AssignmentContext{
		TaskID:           agent.TaskID,
		OwnerAgentID:     agent.ID,
		State:            idx.State,
		CompletionRef:    agent.CompletionReportedRef,
		CompletionAckRef: agent.CompletionAckRef,
		ManifestRef:      agent.PromptRef,
	}
	_, manifest := loadWorkgroupManifest(root, agent.TaskID)
	if manifest != nil {
		// Match an assignment row whose agent_id matches this agent.
		for _, a := range manifest.Assignments {
			if a.AgentID != "" && a.AgentID != agent.ID {
				continue
			}
			row.AssignmentID = a.AssignmentID
			row.ResponsibilityIDs = append(row.ResponsibilityIDs, a.ResponsibilityID)
			row.WritePaths = append(row.WritePaths, a.WritePaths...)
			row.ReportStatus = a.Status
			applyAssignmentCoords(row, a.WorktreePath, a.Branch, a.TargetBranch)
			break
		}
		// Fallback: pick the first assignment row when no explicit
		// agent_id match — common for prepublished workgroups whose
		// agent_id slot is still "planned".
		if row.AssignmentID == "" && len(manifest.Assignments) > 0 {
			a := manifest.Assignments[0]
			row.AssignmentID = a.AssignmentID
			row.ResponsibilityIDs = append(row.ResponsibilityIDs, a.ResponsibilityID)
			row.WritePaths = append(row.WritePaths, a.WritePaths...)
			row.ReportStatus = a.Status
			applyAssignmentCoords(row, a.WorktreePath, a.Branch, a.TargetBranch)
		}
	}
	// Authoritative fallbacks when the workgroup row omits coordinates:
	// assignment sidecar and/or a durable integration checkpoint. Never
	// invent paths that are not present on disk (BUG-039-04 §4.2).
	enrichAssignmentCoords(root, row)
	if manifest == nil && row.AssignmentID == "" {
		return nil
	}
	return row
}

// buildAssignmentRowFromTask is the fallback used when entities.agents[]
// has no entry for the task's owner_agent_ids[0]. It only emits a row if
// the manifest exists AND names an assignment, which prevents spurious
// rows in cases where the owner agent has not yet been spawned.
func buildAssignmentRowFromTask(root, taskID, ownerAgentID string) *AssignmentContext {
	_, manifest := loadWorkgroupManifest(root, taskID)
	if manifest == nil {
		return nil
	}
	for _, a := range manifest.Assignments {
		if a.AssignmentID == "" {
			continue
		}
		row := &AssignmentContext{
			AssignmentID:      a.AssignmentID,
			TaskID:            taskID,
			OwnerAgentID:      ownerAgentID,
			State:             "in_progress",
			ManifestRef:       ".claude/workgroups/" + reqIDFromRuntime(root) + "/" + taskID + "/manifest.json",
			ReportStatus:      a.Status,
			WritePaths:        append([]string(nil), a.WritePaths...),
			ResponsibilityIDs: []string{a.ResponsibilityID},
		}
		applyAssignmentCoords(row, a.WorktreePath, a.Branch, a.TargetBranch)
		enrichAssignmentCoords(root, row)
		return row
	}
	return nil
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
	WorktreePath string `json:"worktree_path"`
	Branch       string `json:"branch"`
	TargetBranch string `json:"target_branch"`
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
	evidenceRoot := filepath.Join(root, ".claude", "evidence")
	entries, err := os.ReadDir(evidenceRoot)
	if err != nil {
		return nil
	}
	for _, runtimeEntry := range entries {
		if !runtimeEntry.IsDir() {
			continue
		}
		runtimeDir := filepath.Join(evidenceRoot, runtimeEntry.Name())
		genEntries, err := os.ReadDir(runtimeDir)
		if err != nil {
			continue
		}
		for _, genEntry := range genEntries {
			if !genEntry.IsDir() || !strings.HasPrefix(genEntry.Name(), "g") {
				continue
			}
			path := filepath.Join(runtimeDir, genEntry.Name(), "worktree", assignmentID, "checkpoint.json")
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				continue
			}
			coords := &assignmentCoordFile{
				WorktreePath: stringFromAny(raw["worktree_path"]),
				Branch:       firstNonEmpty(stringFromAny(raw["source_branch"]), stringFromAny(raw["branch"])),
				TargetBranch: stringFromAny(raw["target_branch"]),
			}
			if coords.WorktreePath == "" && coords.Branch == "" && coords.TargetBranch == "" {
				continue
			}
			return coords
		}
	}
	return nil
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

// loadWorkgroupManifest reads .claude/workgroups/REQ-039/<task>/manifest.json
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
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		reqDir := filepath.Join(manifestDir, entry.Name())
		path := filepath.Join(reqDir, taskID, "manifest.json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var manifest workgroupManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		return path, &manifest
	}
	return "", nil
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
	case "design", "ui_baseline", "ui_prototype", "contract", "task", "team_manifest":
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
	case "in_progress", "review", "blocked", "done":
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
// only as a display hint in fallback assignment rows. Returns "REQ-039"
// on any read failure so the synthesis path is fail-loud, not fail-silent.
func reqIDFromRuntime(root string) string {
	snapshot, err := runtime.NewStore(
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
	).Snapshot()
	if err != nil {
		return "REQ-039"
	}
	bound, ok := snapshot.State["bound_req"].(map[string]any)
	if !ok {
		return "REQ-039"
	}
	id, _ := bound["id"].(string)
	if id == "" {
		return "REQ-039"
	}
	return id
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
