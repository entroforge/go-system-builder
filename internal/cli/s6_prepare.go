package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/identity"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

type s6StringList []string

func (values *s6StringList) String() string { return strings.Join(*values, ", ") }
func (values *s6StringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type s6ManifestReference struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type s6ManifestDisposition struct {
	ResponsibilityID string   `json:"responsibility_id"`
	Disposition      string   `json:"disposition"`
	Trigger          string   `json:"trigger"`
	AssignmentIDs    []string `json:"assignment_ids"`
	NARationale      any      `json:"na_rationale"`
	EvidenceRef      any      `json:"evidence_ref"`
}

type s6ManifestClosingContract struct {
	Contract     string `json:"contract"`
	WorkerChecks []any  `json:"worker_checks"`
}

type s6ManifestAssignment struct {
	AssignmentID       string                    `json:"assignment_id"`
	ResponsibilityID   string                    `json:"responsibility_id"`
	RoleFamily         string                    `json:"role_family"`
	Scope              []string                  `json:"scope"`
	AgentID            string                    `json:"agent_id"`
	AgentDefinitionRef string                    `json:"agent_definition_ref"`
	SkillRefs          []string                  `json:"skill_refs"`
	ReadPaths          []string                  `json:"read_paths"`
	WritePaths         []string                  `json:"write_paths"`
	OutputPaths        []string                  `json:"output_paths"`
	DependsOn          []string                  `json:"depends_on"`
	RequiredChecks     []string                  `json:"required_checks"`
	DoneWhen           []string                  `json:"done_when,omitempty"`
	DispatchMode       string                    `json:"dispatch_mode"`
	CheckProfiles      []checkexec.Profile       `json:"check_profiles"`
	IntegrationMode    string                    `json:"integration_check_mode"`
	ClosingContract    s6ManifestClosingContract `json:"closing_contract"`
	ReuseDecision      string                    `json:"reuse_decision"`
	GroupingRationale  string                    `json:"grouping_rationale"`
	Status             string                    `json:"status"`
}

type s6ManifestValidation struct {
	Result                  string   `json:"result"`
	MissingResponsibilities []string `json:"missing_responsibilities"`
	UnresolvedConflicts     []string `json:"unresolved_conflicts"`
	Warnings                []string `json:"warnings"`
	ValidatedAt             string   `json:"validated_at"`
}

type s6ManifestDraft struct {
	SchemaVersion              string                  `json:"schema_version"`
	ManifestID                 string                  `json:"manifest_id"`
	Version                    string                  `json:"version"`
	RuntimeID                  string                  `json:"runtime_id"`
	REQID                      string                  `json:"req_id"`
	BaselineGeneration         int                     `json:"baseline_generation"`
	ReviewRound                any                     `json:"review_round"`
	PlatformTeamID             string                  `json:"platform_team_id"`
	WorkgroupID                string                  `json:"workgroup_id"`
	WorkgroupKind              string                  `json:"workgroup_kind"`
	Status                     string                  `json:"status"`
	Documents                  []s6ManifestReference   `json:"documents"`
	RiskTags                   []any                   `json:"risk_tags"`
	ResponsibilityDispositions []s6ManifestDisposition `json:"responsibility_dispositions"`
	Assignments                []s6ManifestAssignment  `json:"assignments"`
	SeparationEdges            []any                   `json:"separation_edges"`
	PlannedAgentCount          int                     `json:"planned_agent_count"`
	MaxParallelAgents          int                     `json:"max_parallel_agents"`
	QuantityRationale          string                  `json:"quantity_rationale"`
	Validation                 s6ManifestValidation    `json:"validation"`
	AssignmentRecordContract   string                  `json:"assignment_record_contract"`
}

func runS6PrepareCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("s6 prepare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "s6 prepare")
	root := flags.String("root", ".", "authority repository root")
	taskID := flags.String("task", "", "exact registered TASK ID")
	role := flags.String("role", "", "Builder role: backend-builder, frontend-builder, or test-builder")
	agentID := flags.String("agent-id", "", "legacy only: actual platform Agent ID selected by Main")
	platformTeamID := flags.String("platform-team-id", "", "legacy only: actual platform Team ID selected by Main")
	reviseAssignment := flags.String("revise-assignment", "", "prepare a fresh manifest draft for the current canonical S6 Assignment revision")
	revisionIntentKey := flags.String("revision-intent-key", "", "Main-chosen stable key for the original Assignment revision Writer")
	out := flags.String("out", "", "draft output path relative to the authority root")
	var doneWhen s6StringList
	flags.Var(&doneWhen, "done-when", "explicit Main override for Worker-owned closing predicates; repeat as needed")
	if wantsHelp(args) {
		flags.SetOutput(stdout)
		flags.Usage()
		fmt.Fprintln(stdout, "Default: register the reviewed TASK delivery and print its exact brief for Claude Code native Agent with worktree isolation. No platform IDs or framework launcher. Explicit legacy identity options retain the frozen workspace route.")
		fmt.Fprintln(stdout, "For explicit frozen legacy workspace preparation only: `loop-harness runtime workspace bind --root <authority-root>` binds its original Main checkout.")
		return 0
	}
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return 2
	}
	nativeDelivery := *agentID == "" && *platformTeamID == ""
	if nativeDelivery {
		*agentID = "native-" + *taskID
		*platformTeamID = "engineering-" + *taskID
		if *revisionIntentKey != "" {
			suffix := "-" + workpacket.Digest([]byte(*revisionIntentKey))[:12]
			*agentID += suffix
			*platformTeamID += suffix
		}
	}
	if *taskID == "" || *role == "" || *agentID == "" || *platformTeamID == "" {
		fmt.Fprintln(stderr, "s6 prepare requires --task and --role; legacy identity options must be supplied together")
		return 2
	}
	if (*reviseAssignment == "") != (*revisionIntentKey == "") {
		fmt.Fprintln(stderr, "s6 prepare revision requires both --revise-assignment and --revision-intent-key")
		return 2
	}
	if *role != "backend-builder" && *role != "frontend-builder" && *role != "test-builder" {
		fmt.Fprintln(stderr, "s6 prepare --role must be backend-builder, frontend-builder or test-builder")
		return 2
	}
	if err := identity.ValidateAgentID(*agentID); err != nil {
		fmt.Fprintf(stderr, "s6 prepare: %v\n", err)
		return 2
	}
	if strings.TrimSpace(*platformTeamID) != *platformTeamID || *platformTeamID == "" || strings.ContainsAny(*platformTeamID, " \t\r\n") || strings.Contains(*platformTeamID, "<") || strings.Contains(*platformTeamID, "TODO") || strings.Contains(*platformTeamID, "replace-with") {
		fmt.Fprintln(stderr, "s6 prepare --platform-team-id must be the actual platform Team ID, not a placeholder")
		return 2
	}
	for _, predicate := range doneWhen {
		if strings.TrimSpace(predicate) == "" {
			fmt.Fprintln(stderr, "s6 prepare --done-when values must not be empty")
			return 2
		}
	}

	rootAbs, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(stderr, "s6 prepare root: %v\n", err)
		return 1
	}
	statePath := filepath.Join(rootAbs, ".claude", "loop-state.json")
	snapshot, err := runtime.NewStore(statePath, filepath.Join(rootAbs, ".claude", "loop-events.jsonl")).Snapshot()
	if err != nil {
		fmt.Fprintf(stderr, "s6 prepare read Runtime: %v\n", err)
		return 1
	}
	if err := fileview.ValidateAuthority(rootAbs, snapshot.State); err != nil {
		fmt.Fprintf(stderr, "s6 prepare: %v\n", err)
		return 1
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle == nil || lifecycle["state"] != "building" {
		fmt.Fprintln(stderr, "s6 prepare requires the original Runtime to be in building")
		return 1
	}
	boundREQ, _ := snapshot.State["bound_req"].(map[string]any)
	reqID, _ := boundREQ["id"].(string)
	if reqID == "" {
		fmt.Fprintln(stderr, "s6 prepare requires the current bound REQ")
		return 1
	}
	if nativeDelivery {
		if existing, found, err := assignmentrecord.ForOwner(snapshot.State, *agentID); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		} else if found {
			if existing.Contract != assignmentrecord.NativeDeliveryContract || existing.Task.ID != *taskID || existing.RoleFamily != *role || (*reviseAssignment != "" && existing.AssignmentID != *reviseAssignment) || len(doneWhen) > 0 || *out != "" {
				fmt.Fprintln(stderr, "existing native TASK/role contract differs; Main must inspect the original TASK and role. This replay cannot replace frozen contract fields; execution correction retains the reviewed TASK, while formal specification changes belong to planning/document review")
				return 1
			}
			files, err := dispatch.View(rootAbs, snapshot.State)
			if err != nil {
				fmt.Fprintf(stderr, "Main must restore the current TASK source view before preparation recovery: %v\n", err)
				return 1
			}
			if err := s6RequireOriginalTaskForReplay(snapshot.State, existing, files); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			coordinate, err := workpacket.ReadFile(rootAbs, fmt.Sprintf(".claude/assignments/native/%s/r%d/source.json", existing.AssignmentID, existing.Revision), 4096)
			var binding struct {
				Base string `json:"base_commit"`
			}
			if err != nil || json.Unmarshal(coordinate, &binding) != nil || binding.Base == "" {
				fmt.Fprintln(stderr, "existing native TASK has no original source commit; Main must recover its original preparation, never recapture HEAD")
				return 1
			}
			if err := prepareNativeS6Brief(rootAbs, snapshot.State, existing, binding.Base, stdout); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			return 0
		}
	}
	if nativeDelivery && *reviseAssignment == "" {
		if handled, code := resumeNativeS6Preparation(rootAbs, reqID, *taskID, *role, *agentID, *out, stdout, stderr); handled {
			return code
		}
	}
	files, err := dispatch.View(rootAbs, snapshot.State)
	if err != nil {
		fmt.Fprintf(stderr, "s6 prepare current source view: %v\n", err)
		return 1
	}
	board, err := dispatch.LoadWithFiles(rootAbs, snapshot.State, files, 1<<20)
	if err != nil {
		fmt.Fprintf(stderr, "s6 prepare current dispatch: %v\n", err)
		return 1
	}
	if board.Legacy || board.Plan == nil {
		fmt.Fprintln(stderr, "s6 prepare requires the current registered waves-v1 S4 dispatch plan; legacy workgroups remain on their existing route")
		return 1
	}
	var selected *semantic.DispatchTask
	var row *semantic.DispatchRow
	for i := range board.Plan.Tasks {
		if board.Plan.Tasks[i].ID == *taskID {
			selected = &board.Plan.Tasks[i]
			break
		}
	}
	for i := range board.Rows {
		if board.Rows[i].Task.ID == *taskID {
			row = &board.Rows[i]
			break
		}
	}
	if selected == nil || row == nil {
		fmt.Fprintf(stderr, "s6 prepare TASK %s is not in the current bound REQ %s dispatch plan\n", *taskID, reqID)
		return 1
	}
	if *reviseAssignment != "" {
		return runS6PrepareRevision(s6PrepareRevisionInput{
			Root: rootAbs, State: snapshot.State, StateRevision: snapshot.Revision, Files: files, IntentKey: *revisionIntentKey, NativeDelivery: nativeDelivery, BaseCommit: files.Commit,
			AssignmentID: *reviseAssignment, TaskID: *taskID, Role: *role,
			AgentID: *agentID, PlatformTeamID: *platformTeamID, Output: *out,
			DoneWhen: append([]string(nil), doneWhen...), REQID: reqID,
		}, stdout, stderr)
	}
	if row.State != workstate.Pending || !row.Dispatchable {
		reason := row.Reason
		if reason == "" {
			reason = "the original dispatch projection has not made this TASK available"
		}
		fmt.Fprintf(stderr, "s6 prepare TASK %s is not ready: %s\n", *taskID, reason)
		if strings.Contains(reason, "Builder owner identity unresolved") {
			fmt.Fprintln(stderr, "This is an unresolved existing TASK/Assignment registry fact, not rejection of the new --agent-id. Main must restore the original ownership source; choosing another Agent ID will not repair it.")
		}
		return 1
	}

	analysis, err := semantic.TasksCheckWithFiles(rootAbs, files, reqID)
	if err != nil {
		fmt.Fprintf(stderr, "s6 prepare TASK context: %v\n", err)
		return 1
	}
	var taskContext *semantic.TaskContextSummary
	for i := range analysis.TaskContexts {
		if analysis.TaskContexts[i].TaskID == *taskID {
			taskContext = &analysis.TaskContexts[i]
			break
		}
	}
	if taskContext == nil {
		fmt.Fprintf(stderr, "s6 prepare cannot project the current TASK context for %s\n", *taskID)
		return 1
	}
	if len(selected.Writes) == 0 {
		fmt.Fprintf(stderr, "s6 prepare TASK %s has no locked write scope; return to original S4 planning\n", *taskID)
		return 1
	}

	baseline, _ := snapshot.State["baseline"].(map[string]any)
	generation := dispatch.Number(baseline["generation"])
	runtimeID, _ := snapshot.State["runtime_id"].(string)
	if generation < 1 || runtimeID == "" {
		fmt.Fprintln(stderr, "s6 prepare Runtime identity or baseline generation is not ready")
		return 1
	}
	documents, err := s6ManifestDocuments(rootAbs, files, snapshot.State, reqID, generation, *selected, *taskContext, board.Plan.Path)
	if err != nil {
		fmt.Fprintf(stderr, "s6 prepare locked documents: %v\n", err)
		return 1
	}
	readPaths := s6TaskReadPaths(*selected, *taskContext, boundREQ, board.Plan.Path, documents)
	checks := s6TaskCommands(*taskContext)
	profiles := s6MainCheckProfiles(*taskID, checks)
	if err := checkexec.ValidateProfiles(profiles, checks, "main"); err != nil {
		fmt.Fprintf(stderr, "s6 prepare Main check profile projection: %v\n", err)
		return 1
	}
	taskBytes, err := files.ReadFile(filepath.Join(rootAbs, filepath.FromSlash(selected.Path)))
	if err != nil {
		fmt.Fprintf(stderr, "s6 prepare locked TASK %s: %v\n", selected.Path, err)
		return 1
	}
	projectedDoneWhen := s6TaskDoneWhen(taskBytes)
	if len(doneWhen) > 0 {
		projectedDoneWhen = cleanS6Strings(doneWhen)
	}

	assignmentID := fmt.Sprintf("assignment-build-%s-g%d", *taskID, generation)
	workgroupID := fmt.Sprintf("workgroup-build-%s-g%d", *taskID, generation)
	manifestID := fmt.Sprintf("team-manifest-build-%s-g%d", *taskID, generation)
	roleRef := filepath.ToSlash(filepath.Join(".claude", "agents", *role+".md"))
	if _, err := os.Stat(filepath.Join(rootAbs, filepath.FromSlash(roleRef))); err != nil {
		fmt.Fprintf(stderr, "s6 prepare role definition %s is unavailable: %v\n", roleRef, err)
		return 1
	}
	skillRefs := agentcontext.CoreSkills(*role)
	if len(skillRefs) == 0 {
		fmt.Fprintf(stderr, "s6 prepare has no installed core Skills for role %s\n", *role)
		return 1
	}
	if _, err := agentcontext.Select(rootAbs, *role, []string{"BUILD-WORK-PACKAGE"}, skillRefs); err != nil {
		fmt.Fprintf(stderr, "s6 prepare role Skills: %v\n", err)
		return 1
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	warnings := []string{"Non-authoritative draft only; the original register-workgroup Writer rechecks current Runtime, TASK, document fingerprints and registration gates."}
	draft := s6ManifestDraft{
		SchemaVersion: "1.0.0", ManifestID: manifestID, Version: "v1.0.0",
		RuntimeID: runtimeID, REQID: reqID, BaselineGeneration: generation,
		ReviewRound: nil, PlatformTeamID: *platformTeamID, WorkgroupID: workgroupID,
		WorkgroupKind: "builder", Status: "planned", Documents: documents,
		RiskTags: []any{}, ResponsibilityDispositions: []s6ManifestDisposition{{
			ResponsibilityID: "BUILD-WORK-PACKAGE", Disposition: "assigned",
			Trigger:       "One current, dispatchable locked TASK has one Builder owner.",
			AssignmentIDs: []string{assignmentID}, NARationale: nil, EvidenceRef: nil,
		}},
		Assignments: []s6ManifestAssignment{{
			AssignmentID: assignmentID, ResponsibilityID: "BUILD-WORK-PACKAGE",
			RoleFamily: *role, Scope: append([]string{}, selected.Writes...), AgentID: *agentID,
			AgentDefinitionRef: roleRef, SkillRefs: skillRefs, ReadPaths: readPaths,
			WritePaths: append([]string{}, selected.Writes...), OutputPaths: []string{},
			// TASK dependencies are already satisfied by the original dispatch
			// projection. Manifest depends_on is workgroup-local, so it remains empty.
			DependsOn: []string{}, RequiredChecks: checks, DoneWhen: projectedDoneWhen,
			DispatchMode: "plan_checkpoint", CheckProfiles: profiles,
			IntegrationMode: "post_merge",
			ClosingContract: s6ManifestClosingContract{Contract: "s6-closing-v1", WorkerChecks: []any{}},
			ReuseDecision:   "create", GroupingRationale: "One Builder owns the exact reviewed TASK write scope.", Status: "planned",
		}}, SeparationEdges: []any{}, PlannedAgentCount: 1, MaxParallelAgents: 1,
		QuantityRationale:        "One Builder owns the single selected TASK.",
		Validation:               s6ManifestValidation{Result: "warn", MissingResponsibilities: []string{}, UnresolvedConflicts: []string{}, Warnings: warnings, ValidatedAt: now},
		AssignmentRecordContract: "s6-v1.2",
	}
	if nativeDelivery {
		draft.AssignmentRecordContract = assignmentrecord.NativeDeliveryContract
	}
	manifestBytes, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "s6 prepare encode manifest: %v\n", err)
		return 1
	}
	if err := team.ValidateBytes(manifestBytes); err != nil {
		fmt.Fprintf(stderr, "s6 prepare manifest structure: %v\n", err)
		return 1
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("team-manifest.schema.json", manifestBytes); err != nil {
		fmt.Fprintf(stderr, "s6 prepare manifest schema: %v\n", err)
		return 1
	}
	if nativeDelivery {
		if err := pinNativeS6Source(rootAbs, draft.Assignments[0].AssignmentID, 1, files.Commit); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	manifestRef, err := s6WriteDraft(rootAbs, reqID, *taskID, *out, manifestBytes)
	if err != nil {
		fmt.Fprintf(stderr, "s6 prepare write draft: %v\n", err)
		return 1
	}
	if err := files.Verify(); err != nil {
		_ = os.Remove(filepath.Join(rootAbs, filepath.FromSlash(manifestRef)))
		fmt.Fprintf(stderr, "s6 prepare source view changed before draft output: %v\n", err)
		return 1
	}

	if nativeDelivery {
		return registerNativeS6Delivery(rootAbs, manifestRef, *taskID, selected.Path, files.Commit, stdout, stderr)
	}
	fmt.Fprintf(stdout, "S6 manifest draft written (not registered): %s\n", manifestRef)
	fmt.Fprintf(stdout, "TASK: %s (%s); dependencies already satisfied by the original dispatch: %s\n", *taskID, selected.Path, s6DisplayList(selected.Dependencies))
	fmt.Fprintf(stdout, "Role: %s; platform Agent: %s; platform Team: %s\n", *role, *agentID, *platformTeamID)
	fmt.Fprintf(stdout, "Generated assignment: %s; exact write scope: %s\n", assignmentID, s6DisplayList(selected.Writes))
	if len(checks) > 0 {
		fmt.Fprintf(stdout, "TASK checks projected to Main required_checks and Main CheckProfiles only: %s. The Builder must run them in its workspace and cite real outputs in the Result; Main reruns them during original integration. No PASS is recorded here.\n", s6DisplayList(checks))
	} else {
		fmt.Fprintln(stdout, "No executable TASK check entrance was projected. Non-command assertions were not turned into commands; Main must preserve their professional meaning in the Result evidence.")
	}
	if len(projectedDoneWhen) == 0 {
		fmt.Fprintf(stdout, "Main question: the locked TASK %s has no reliably projectable Worker-owned Closing Contract predicate. Add a concrete `done_when` to this draft before registration; do not substitute a generic completion condition.\n", selected.Path)
	}
	for _, line := range taskContext.UnresolvedAssertions {
		if !s6MachineOwnedAssertion(line) {
			fmt.Fprintf(stdout, "TASK assertion kept non-executable and unchanged: %s\n", line)
		}
	}
	register := fmt.Sprintf("loop-harness runtime register-workgroup --root %q --manifest %q --task-id %q --task %q", rootAbs, manifestRef, *taskID, selected.Path)
	if len(projectedDoneWhen) == 0 {
		fmt.Fprintf(stdout, "After answering the listed Main question, register through the original consumer: %s\n", register)
	} else {
		fmt.Fprintf(stdout, "Next: register through the original consumer: %s\n", register)
	}
	_, mainWorkspaceBound := snapshot.State["workspace"].(map[string]any)
	if !mainWorkspaceBound {
		fmt.Fprintf(stdout, "After registration, bind the Main workspace with the existing command: loop-harness runtime workspace bind --root %q\n", rootAbs)
		fmt.Fprintf(stdout, "Then prepare the original bound workspace with: loop-harness runtime workspace prepare --root %q --assignment %q --agent %q\n", rootAbs, assignmentID, *agentID)
	} else {
		fmt.Fprintf(stdout, "After registration, prepare the original bound workspace with: loop-harness runtime workspace prepare --root %q --assignment %q --agent %q\n", rootAbs, assignmentID, *agentID)
	}
	return 0
}

// Replaying a native preparation retains its original source commit. Check
// only its TASK against current locked facts before returning that old brief;
// a different specification is not an execution-recovery authorization.
func s6RequireOriginalTaskForReplay(state map[string]any, record assignmentrecord.Record, files *fileview.View) error {
	fail := func(reason string) error {
		return fmt.Errorf("TASK %s (%s) cannot reuse its original native preparation: %s. Main must distinguish source recovery from a formal specification change. Assignment revision retains the exact reviewed TASK; formal changes return to original planning/document review. Same-generation TASK replacement is not supported by this recovery path; do not refresh fingerprints, repeat revision or reuse the old brief as a new specification", record.Task.ID, record.Task.Ref, reason)
	}
	baseline, _ := state["baseline"].(map[string]any)
	if state["runtime_id"] != record.RuntimeID || dispatch.Number(baseline["generation"]) != record.BaselineGeneration {
		return fail("the original Runtime/generation is no longer current")
	}
	rows, _ := state["documents"].([]any)
	matches := 0
	for _, raw := range rows {
		doc, _ := raw.(map[string]any)
		if doc["kind"] != "task" || doc["id"] != record.Task.ID || dispatch.Number(doc["generation"]) != record.BaselineGeneration {
			continue
		}
		matches++
		if doc["status"] != "locked" || doc["path"] != record.Task.Ref || doc["version"] != record.Task.Version || doc["sha256"] != record.Task.SHA256 {
			return fail("the current locked TASK identity/version/content differs from the original Record")
		}
	}
	if matches != 1 {
		return fail("the current locked batch does not uniquely contain this original TASK")
	}
	data, err := files.ReadFile(record.Task.Ref)
	if err != nil {
		return fmt.Errorf("Main must recover locked TASK %s source at %s before reusing its preparation: %w", record.Task.ID, record.Task.Ref, err)
	}
	if workpacket.Digest(data) != record.Task.SHA256 {
		return fail("the current Main TASK bytes changed")
	}
	if err := files.Verify(); err != nil {
		return fmt.Errorf("Main TASK source view changed during preparation recovery; preserve the original brief: %w", err)
	}
	return nil
}

func s6ManifestDocuments(root string, files fileview.Reader, state map[string]any, reqID string, generation int, task semantic.DispatchTask, taskContext semantic.TaskContextSummary, planPath string) ([]s6ManifestReference, error) {
	bound, _ := state["bound_req"].(map[string]any)
	reqPath := s6String(bound["path"])
	neededPaths := map[string]bool{reqPath: true, task.Path: true, planPath: true}
	neededIDs := map[string]bool{reqID: true, task.ID: true}
	for _, reading := range taskContext.Reading {
		if reading.Path != "" && reading.Status == "readable" {
			neededPaths[reading.Path] = true
		}
	}
	for _, clause := range taskContext.Clauses {
		if id, _, ok := strings.Cut(strings.TrimSpace(clause), " "); ok && id != "" {
			neededIDs[id] = true
		}
	}
	rows, _ := state["documents"].([]any)
	refs := make([]s6ManifestReference, 0, len(rows))
	seenID := map[string]bool{}
	seenPath := map[string]bool{}
	seenREQ, seenTask, seenPlan := false, false, false
	for _, raw := range rows {
		doc, _ := raw.(map[string]any)
		if doc == nil || dispatch.Number(doc["generation"]) != generation {
			continue
		}
		status, _ := doc["status"].(string)
		if status != "locked" && status != "complete" && status != "registered" {
			continue
		}
		ref := s6ManifestReference{ID: s6String(doc["id"]), Path: s6String(doc["path"]), Version: s6String(doc["version"]), SHA256: s6String(doc["sha256"])}
		if ref.ID == task.ID && ref.Path != task.Path {
			return nil, fmt.Errorf("current-generation TASK %s Runtime path %s differs from dispatch path %s", task.ID, ref.Path, task.Path)
		}
		if ref.ID == reqID && reqPath != "" && ref.Path != reqPath {
			return nil, fmt.Errorf("current-generation REQ %s Runtime path %s differs from bound path %s", reqID, ref.Path, reqPath)
		}
		if !neededPaths[ref.Path] && !neededIDs[ref.ID] {
			continue
		}
		if ref.ID == "" || ref.Path == "" || ref.Version == "" || !s6ValidSHA256(ref.SHA256) {
			return nil, fmt.Errorf("current-generation document %q lacks a complete locked identity/path/version/hash", ref.ID)
		}
		if seenID[ref.ID] || seenPath[ref.Path] {
			return nil, fmt.Errorf("current-generation document identity or path is duplicated: %s (%s)", ref.ID, ref.Path)
		}
		data, err := files.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
		if err != nil {
			return nil, fmt.Errorf("read exact bound source %s: %w", ref.Path, err)
		}
		actual := fmt.Sprintf("%x", sha256.Sum256(data))
		if actual != ref.SHA256 {
			return nil, fmt.Errorf("current-generation locked source differs from Runtime fingerprint: %s", ref.Path)
		}
		seenID[ref.ID], seenPath[ref.Path] = true, true
		refs = append(refs, ref)
		seenREQ = seenREQ || ref.ID == reqID && ref.Path == reqPath
		seenTask = seenTask || ref.ID == task.ID && ref.Path == task.Path
		seenPlan = seenPlan || ref.Path == planPath
	}
	if !seenREQ {
		ref := s6ManifestReference{ID: reqID, Path: reqPath, Version: s6String(bound["version"]), SHA256: s6String(bound["sha256"])}
		if ref.Path == "" || ref.Version == "" || !s6ValidSHA256(ref.SHA256) {
			return nil, fmt.Errorf("bound REQ %s lacks an exact path/version/fingerprint", reqID)
		}
		data, err := files.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != ref.SHA256 {
			return nil, fmt.Errorf("bound REQ source differs from its Runtime fingerprint: %s", ref.Path)
		}
		refs = append(refs, ref)
		seenID[ref.ID], seenPath[ref.Path] = true, true
	}
	if !seenTask {
		return nil, fmt.Errorf("locked current-generation TASK %s is missing from Runtime documents", task.ID)
	}
	for _, clause := range taskContext.Clauses {
		id, _, ok := strings.Cut(strings.TrimSpace(clause), " ")
		if ok && id != "" && !seenID[id] {
			return nil, fmt.Errorf("TASK clause %s has no current-generation locked document reference", clause)
		}
	}
	if !seenPlan {
		return nil, fmt.Errorf("registered dispatch plan %s is missing from current-generation Runtime documents", planPath)
	}
	sort.SliceStable(refs, func(i, j int) bool {
		iIsREQ, jIsREQ := refs[i].ID == reqID, refs[j].ID == reqID
		if iIsREQ != jIsREQ {
			return iIsREQ
		}
		return refs[i].Path < refs[j].Path
	})
	return refs, nil
}

func s6TaskReadPaths(task semantic.DispatchTask, context semantic.TaskContextSummary, boundREQ map[string]any, planPath string, documents []s6ManifestReference) []string {
	values := []string{task.Path, planPath, s6String(boundREQ["path"])}
	for _, reading := range context.Reading {
		if reading.Path != "" && reading.Status == "readable" {
			values = append(values, reading.Path)
		}
	}
	for _, document := range documents {
		values = append(values, document.Path)
	}
	return cleanS6Strings(values)
}

func s6TaskCommands(context semantic.TaskContextSummary) []string {
	values := make([]string, 0, len(context.CheckEntrances))
	for _, entrance := range context.CheckEntrances {
		values = append(values, entrance.Command)
	}
	return cleanS6Strings(values)
}

func s6MainCheckProfiles(taskID string, commands []string) []checkexec.Profile {
	profiles := make([]checkexec.Profile, 0, len(commands))
	for index, command := range commands {
		identity := fmt.Sprintf("s6-main-task-check-v1\x00%s\x00%d\x00%s", taskID, index, command)
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
		profiles = append(profiles, checkexec.Profile{
			ID: "s6-main-task-" + digest[:20], Version: "v1", CheckIndex: index,
			Command: command, Location: "main", TimeoutSeconds: 300,
			EnvironmentPolicy: "inherited", Environment: []checkexec.EnvFact{},
			Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{},
			InputClosure: "whole_tree", SideEffects: "unknown", ReusePolicy: "never",
		})
	}
	return profiles
}

func s6TaskDoneWhen(body []byte) []string {
	section := false
	values := []string{}
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if strings.HasPrefix(line, "## ") {
			if section {
				break
			}
			section = s6ClosingContractHeading(strings.TrimSpace(strings.TrimPrefix(line, "## ")))
			continue
		}
		if !section || line == "" || strings.HasPrefix(line, "<!--") || strings.HasPrefix(line, "```") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if !strings.HasPrefix(line, "assert ") {
			continue
		}
		predicate := strings.TrimSpace(strings.TrimPrefix(line, "assert "))
		if strings.ContainsAny(predicate, "{}") || s6MachineOwnedAssertion(line) {
			continue
		}
		values = append(values, predicate)
	}
	return cleanS6Strings(values)
}

func s6ClosingContractHeading(title string) bool {
	title = strings.TrimSpace(title)
	if number, remainder, ok := strings.Cut(title, "."); ok && number != "" {
		numeric := true
		for _, r := range number {
			if r < '0' || r > '9' {
				numeric = false
				break
			}
		}
		if numeric {
			title = strings.TrimSpace(remainder)
		}
	}
	return strings.EqualFold(title, "Closing Contract")
}

func s6MachineOwnedAssertion(line string) bool {
	predicate := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "assert "))
	switch predicate {
	case "changed_paths subset_of activated_write_paths", "scope_deviations == []":
		return true
	default:
		return false
	}
}

func s6WriteDraft(root, reqID, taskID, requested string, data []byte) (string, error) {
	ref := requested
	if ref == "" {
		ref = filepath.Join(".claude", "workgroups", reqID, taskID, "manifest.draft.json")
	}
	if !filepath.IsAbs(ref) {
		ref = filepath.Join(root, ref)
	}
	ref, err := filepath.Abs(ref)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, ref)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("draft output must remain inside the authority root")
	}
	for parent := filepath.Dir(ref); parent != root; parent = filepath.Dir(parent) {
		info, statErr := os.Lstat(parent)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("draft output parent may not be a symlink: %s", parent)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", statErr
		}
	}
	if err := os.MkdirAll(filepath.Dir(ref), 0o755); err != nil {
		return "", err
	}
	file, err := os.OpenFile(ref, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("refusing to overwrite existing draft %s", filepath.ToSlash(rel))
		}
		return "", err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		_ = os.Remove(ref)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(ref)
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func cleanS6Strings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func s6DisplayList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func s6String(value any) string {
	text, _ := value.(string)
	return text
}

func s6ValidSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}
