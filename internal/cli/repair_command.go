package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	assignmentpkg "github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func runRuntimeRepair(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "runtime repair requires <session|plan|dispatch|plan-report|execution|result|changeset|impact|targeted|handoff|status>")
		return 2
	}
	switch args[0] {
	case "session":
		if len(args) < 2 || args[1] != "open" {
			fmt.Fprintln(stderr, "runtime repair session requires <open>")
			return 2
		}
		return runRuntimeRepairSessionOpen(args[2:], stdout, stderr)
	case "plan":
		if len(args) < 2 || args[1] != "compile" {
			fmt.Fprintln(stderr, "runtime repair plan requires <compile>")
			return 2
		}
		return runRuntimeRepairPlanCompile(args[2:], stdout, stderr)
	case "dispatch":
		return runRuntimeRepairDispatch(args[1:], stdout, stderr)
	case "plan-report":
		if len(args) < 2 || args[1] != "submit" {
			fmt.Fprintln(stderr, "runtime repair plan-report requires <submit>")
			return 2
		}
		return runRuntimeRepairPlanReportSubmit(args[2:], stdout, stderr)
	case "execution":
		if len(args) < 2 || args[1] != "begin" {
			fmt.Fprintln(stderr, "runtime repair execution requires <begin>")
			return 2
		}
		return runRuntimeRepairExecutionBegin(args[2:], stdout, stderr)
	case "result":
		if len(args) < 2 || args[1] != "submit" {
			fmt.Fprintln(stderr, "runtime repair result requires <submit>")
			return 2
		}
		return runRuntimeRepairResultSubmit(args[2:], stdout, stderr)
	case "changeset":
		if len(args) < 2 || args[1] != "compute" {
			fmt.Fprintln(stderr, "runtime repair changeset requires <compute>")
			return 2
		}
		return runRuntimeRepairChangesetCompute(args[2:], stdout, stderr)
	case "impact":
		if len(args) < 2 || (args[1] != "commit" && args[1] != "create" && args[1] != "prepare") {
			fmt.Fprintln(stderr, "runtime repair impact requires <prepare|create|commit>")
			return 2
		}
		if args[1] == "prepare" {
			return runRuntimeRepairImpactPrepare(args[2:], stdout, stderr)
		}
		if args[1] == "create" {
			return runRuntimeRepairImpactCreate(args[2:], stdout, stderr)
		}
		return runRuntimeRepairImpactCommit(args[2:], stdout, stderr)
	case "targeted":
		if len(args) < 2 || (args[1] != "commit" && args[1] != "create" && args[1] != "resume") {
			fmt.Fprintln(stderr, "runtime repair targeted requires <create|commit|resume>")
			return 2
		}
		if args[1] == "create" {
			return runRuntimeRepairTargetedCreate(args[2:], stdout, stderr)
		}
		if args[1] == "resume" {
			return runRuntimeRepairTargetedResume(args[2:], stdout, stderr)
		}
		return runRuntimeRepairTargetedCommit(args[2:], stdout, stderr)
	case "handoff":
		if len(args) > 1 && args[1] == "complete" {
			return runRuntimeRepairHandoffComplete(args[2:], stdout, stderr)
		}
		if len(args) < 2 || (args[1] != "commit" && args[1] != "create") {
			fmt.Fprintln(stderr, "runtime repair handoff requires <create|commit|complete>")
			return 2
		}
		if args[1] == "create" {
			return runRuntimeRepairHandoffCreate(args[2:], stdout, stderr)
		}
		return runRuntimeRepairHandoffCommit(args[2:], stdout, stderr)
	case "status":
		return runRuntimeRepairStatus(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "runtime repair: unknown operation %q\n", args[0])
		return 2
	}
}

// runRuntimeRepairImpactPrepare resumes only the original preparation after
// accepted Final integration. It neither replays Builder work nor commits Impact.
func runRuntimeRepairImpactPrepare(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair impact prepare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair impact prepare")
	root := fs.String("root", ".", "repository root")
	state := fs.String("state", ".claude/loop-state.json", "runtime state path")
	journal := fs.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	snapshot, err := runtime.NewStore(resolveRootPath(*root, *state), resolveRootPath(*root, *journal)).Snapshot()
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair impact prepare", err))
		return 1
	}
	if lifecycleState(snapshot.State) != "bug_resolution" || stringValue(repairPointerCLI(snapshot.State)["status"]) != "impact_reconciliation" {
		fmt.Fprintln(stderr, "runtime repair impact prepare requires the current S9 impact_reconciliation responsibility; no questionnaire or Runtime was changed")
		return 1
	}
	path, err := prepareRepairImpactQuestionnaire(*root, snapshot.State)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair impact prepare", err))
		return 1
	}
	fmt.Fprintln(stderr, "Original ChangeImpact questionnaire prepared; existing professional answers are preserved. Complete and save this exact file for its original consumer; no Builder resubmission or hand-authored machine IDs are needed.")
	return encodeJSON(stdout, map[string]any{"questionnaire_path": path, "revision": snapshot.Revision})
}

func repairImpactPreparationRecovery(root string) string {
	return "`loop-harness runtime repair impact prepare --root " + s7CaptureShellQuote(root) + "`"
}

type repairCLIFlags struct {
	root, state, journal string
	expected             int
	actor, occurred      string
}

func repairFlags(fs *flag.FlagSet) *repairCLIFlags {
	result := &repairCLIFlags{}
	fs.StringVar(&result.root, "root", ".", "repository root")
	fs.StringVar(&result.state, "state", ".claude/loop-state.json", "runtime state path")
	fs.StringVar(&result.journal, "journal", ".claude/loop-events.jsonl", "runtime journal path")
	fs.IntVar(&result.expected, "expected-revision", -1, "expected runtime revision")
	fs.StringVar(&result.actor, "actor", "", "actor identity")
	fs.StringVar(&result.occurred, "occurred-at", "", "RFC3339 transition time")
	return result
}
func repairOccurred(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, value)
}
func repairExpected(f *repairCLIFlags) (int, error) {
	// Keep the optional explicit value intact. A normal S9 command passes -1
	// through to the Writer, which reads the Runtime under its lock; resolving
	// it here would recreate the stale read-then-CAS handoff that this flag is
	// meant to stop imposing on Agents.
	return f.expected, nil
}

// repairAgentDefinition preserves an explicit operator-supplied path. When
// omitted, use catalog's single-layout authority so an installed checkout
// resolves below .claude/agents and a factory checkout resolves below agents/.
// The catalog never falls back across layouts when an installed asset is
// missing.
func repairAgentDefinition(root, role, requested string, explicit bool) (string, []byte, error) {
	if explicit {
		path := filepath.ToSlash(requested)
		data, err := os.ReadFile(resolveRootPath(root, requested))
		if err != nil {
			return "", nil, fmt.Errorf("read explicitly selected Agent Definition %q: %w", requested, err)
		}
		return path, data, nil
	}
	data, path, err := catalog.ReadAgentAsset(root, role)
	if err != nil {
		return "", nil, fmt.Errorf("resolve Agent Definition for %s from the selected asset layout: %w", role, err)
	}
	return path, data, nil
}

// runRuntimeRepairDispatch materializes one S9 RepairAssignment as a normal
// builder workgroup. All assignments are dispatched before execution begins
// so every Builder can submit its domain PlanReport; dependency and lock
// ordering is consumed by Result submission and the read-only status board.
// This is a lifecycle bridge, not a second scheduler.
func runRuntimeRepairDispatch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair dispatch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair dispatch")
	common := repairFlags(fs)
	assignmentID := fs.String("assignment-id", "", "RepairAssignment id")
	agentID := fs.String("agent-id", "", "Builder Agent id")
	roleFamily := fs.String("role-family", "backend-builder", "Builder role family")
	definitionRef := fs.String("agent-definition", "", "Builder Agent Definition path (default follows the installed asset layout)")
	independentVerification := fs.Bool("independent-verification", false, "dispatch the independent verifier assignment for the targeted reverification of --assignment-id (identity registration only: no product writes, test-builder verifier)")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	explicitFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { explicitFlags[f.Name] = true })
	// RC-18: a verification identity is registered by the same dispatch chain
	// but has a fixed shape — an independent test-builder that writes no
	// product file — so its defaults follow the flag instead of the builder
	// defaults; an explicitly passed flag always wins.
	if *independentVerification {
		if !explicitFlags["role-family"] {
			*roleFamily = "test-builder"
		}
	}
	if strings.TrimSpace(*assignmentID) == "" || strings.TrimSpace(*agentID) == "" {
		fmt.Fprintln(stderr, "runtime repair dispatch requires --assignment-id and --agent-id")
		return 2
	}
	if *independentVerification && *roleFamily != "test-builder" {
		fmt.Fprintln(stderr, "independent verification requires role-family test-builder")
		return 2
	}
	if *roleFamily != "frontend-builder" && *roleFamily != "backend-builder" && *roleFamily != "test-builder" {
		fmt.Fprintf(stderr, "runtime repair dispatch: unsupported --role-family %q; use frontend-builder, backend-builder or test-builder\n", *roleFamily)
		return 2
	}
	root, err := filepath.Abs(common.root)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", err))
		return 1
	}
	statePath := resolveRootPath(root, common.state)
	journalPath := resolveRootPath(root, common.journal)
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", err))
		return 1
	}
	review := mapFieldCLI(snapshot.State, "review")
	pointer := mapFieldCLI(review, "repair")
	status := stringValue(pointer["status"])
	// Recovery dispatch (RC: queued coverage is never dropped): an assignment
	// the operator skipped before execution begin has no owner, and only its
	// owner may submit the unit result — refusing dispatch in repairing would
	// strand that unit permanently.
	existingOwners := stringMapCLI(pointer["assignment_owners"])
	unownedInRepairing := status == "repairing" && existingOwners[*assignmentID] == ""
	// RC-18: a verification identity is dispatched against a plan assignment
	// that already executed, so targeted_reverification — the only phase in
	// which the reverification may be committed — must accept it too. The
	// repair-assignment admission rules above are untouched.
	verificationMode := *independentVerification
	verificationStatus := status == "planning" || status == "reproducing" || status == "targeted_reverification"
	if (verificationMode && !verificationStatus) || (!verificationMode && status != "planning" && status != "reproducing" && !unownedInRepairing) {
		if verificationMode {
			fmt.Fprintf(stderr, "runtime repair dispatch: --independent-verification requires S9 status=planning, reproducing or targeted_reverification (current %s)\n", status)
			return 1
		}
		fmt.Fprintf(stderr, "runtime repair dispatch: S9 status=%s cannot accept a new Builder; compile a RepairPlan and dispatch before `runtime repair execution begin`\n", status)
		return 1
	}
	planRef := repair.ArtifactRef{Path: stringValue(pointer["plan_ref"]), SHA256: stringValue(pointer["plan_sha256"])}
	plan, err := repair.ValidateRepairPlan(root, planRef)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", err))
		return 1
	}
	var target repair.RepairAssignment
	found := false
	for _, candidate := range plan.Assignments {
		if candidate.AssignmentID == *assignmentID {
			target = candidate
			found = true
			break
		}
	}
	if !found {
		fmt.Fprintf(stderr, "runtime repair dispatch: Assignment %s is not in RepairPlan %s; use `runtime repair status` to list exact assignments\n", *assignmentID, plan.PlanID)
		return 1
	}
	owners := existingOwners
	dispatchAssignmentID := target.AssignmentID
	manifestAssignmentID := "assignment-s9-" + dispatchSlug(strings.TrimPrefix(target.AssignmentID, "repair-assignment-"))
	writePaths := append([]string(nil), target.Scope...)
	groupingRationale := "one Builder owns one immutable RepairAssignment; cross-assignment dependencies and locks are consumed by S9 Runtime"
	if verificationMode {
		// RC-18: synthesize and validate the independent verification
		// identity for this repair assignment. The repair assignment itself
		// is already owned and already carries a PlanReport/RepairResult —
		// that is this mode's precondition, not a conflict — so the builder
		// duplicate guards below are deliberately skipped for it.
		verificationAssignmentID, verificationErr := repair.ValidateVerificationDispatch(plan, target.AssignmentID, *agentID, owners)
		if verificationErr != nil {
			fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", verificationErr))
			return 1
		}
		dispatchAssignmentID = verificationAssignmentID
		manifestAssignmentID = repair.ManifestVerificationAssignmentID(target.AssignmentID)
		writePaths = []string{}
		groupingRationale = "one independent verifier owns one synthesized verification assignment; it re-executes the approved contract assertions for the repair unit and writes no product file"
	} else {
		if owner := owners[target.AssignmentID]; owner != "" {
			fmt.Fprintf(stderr, "runtime repair dispatch: Assignment %s is already owned by Agent %s; continue that Agent or recover the session, do not replace ownership\n", target.AssignmentID, owner)
			return 1
		}
		for _, ref := range repairArtifactRefs(pointer, "plan_report_refs", "plan_report_ref", "plan_report_sha256") {
			report, reportErr := repair.ValidatePlanReport(root, ref)
			if reportErr == nil && report.AssignmentID == target.AssignmentID {
				fmt.Fprintf(stderr, "runtime repair dispatch: Assignment %s already has PlanReport %s; submit the domain report or inspect `runtime repair status`\n", target.AssignmentID, report.ReportID)
				return 1
			}
		}
		for _, ref := range repairArtifactRefs(pointer, "result_refs", "result_ref", "result_sha256") {
			result, resultErr := repair.ValidateRepairResult(root, ref)
			if resultErr == nil && result.AssignmentID == target.AssignmentID {
				fmt.Fprintf(stderr, "runtime repair dispatch: Assignment %s already has RepairResult %s; do not dispatch it again\n", target.AssignmentID, result.ResultID)
				return 1
			}
		}
	}
	boundReq := mapFieldCLI(snapshot.State, "bound_req")
	reqID := stringValue(boundReq["id"])
	if reqID == "" {
		fmt.Fprintln(stderr, "runtime repair dispatch: bound_req.id is missing; bind the active REQ before dispatch")
		return 1
	}
	baseline := mapFieldCLI(snapshot.State, "baseline")
	baselineGeneration := intFieldCLI(baseline["generation"])
	if baselineGeneration < 1 {
		fmt.Fprintln(stderr, "runtime repair dispatch: baseline.generation is not ready; restore the current baseline before dispatch")
		return 1
	}
	definitionPath, definitionBytes, err := repairAgentDefinition(root, *roleFamily, *definitionRef, explicitFlags["agent-definition"])
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", err))
		return 1
	}
	protocolBytes, err := os.ReadFile(filepath.Join(root, projectlayout.Protocol))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", fmt.Errorf("read agent protocol: %w", err)))
		return 1
	}
	contractPath := stringValue(pointer["contract_ref"])
	contractSHA := stringValue(pointer["contract_sha256"])
	sessionPath := stringValue(pointer["path"])
	sessionSHA := stringValue(pointer["sha256"])
	workgroupID := "workgroup-s9-" + dispatchSlug(stringValue(pointer["session_id"])) + "-" + dispatchSlug(dispatchAssignmentID)
	manifestID := "team-manifest-s9-" + dispatchSlug(stringValue(pointer["session_id"])) + "-" + dispatchSlug(dispatchAssignmentID)
	if entityExists(snapshot.State, "teams", workgroupID) {
		fmt.Fprintf(stderr, "runtime repair dispatch: %s is already registered; inspect `runtime repair status` and Runtime team state\n", workgroupID)
		return 1
	}
	taskID := s9RepairTaskID(stringValue(pointer["session_id"]), dispatchAssignmentID)
	if entityExists(snapshot.State, "tasks", taskID) {
		fmt.Fprintf(stderr, "runtime repair dispatch: task %s already exists; inspect Runtime entities before retrying\n", taskID)
		return 1
	}
	taskRel := filepath.ToSlash(filepath.Join(".claude", "workgroups", reqID, taskID, taskID+".json"))
	manifestRel := filepath.ToSlash(filepath.Join(".claude", "workgroups", reqID, taskID, "manifest.json"))
	reviewRound := intFieldCLI(review["round"])
	var reviewRoundValue any
	if reviewRound > 0 {
		reviewRoundValue = reviewRound
	}
	definitionPath = filepath.ToSlash(definitionPath)
	manifest := map[string]any{
		"schema_version": "1.0.0", "manifest_id": manifestID, "version": "1.0.0", "runtime_id": stringValue(snapshot.State["runtime_id"]),
		"req_id": reqID, "baseline_generation": baselineGeneration, "review_round": reviewRoundValue,
		"platform_team_id": "platform-s9-repair", "workgroup_id": workgroupID, "workgroup_kind": "builder", "status": "planned",
		"documents": []any{
			map[string]any{"id": "repair-contract", "path": contractPath, "version": "approved", "sha256": contractSHA},
			map[string]any{"id": "repair-session", "path": sessionPath, "version": "planned", "sha256": sessionSHA},
			map[string]any{"id": "repair-plan", "path": planRef.Path, "version": "compiled", "sha256": planRef.SHA256},
			map[string]any{"id": "agent-protocol", "path": projectlayout.Protocol, "version": "current", "sha256": sha256HexForArtifact(protocolBytes)},
			map[string]any{"id": "builder-definition", "path": definitionPath, "version": "current", "sha256": sha256HexForArtifact(definitionBytes)},
		},
		"risk_tags":                   []any{},
		"responsibility_dispositions": []any{map[string]any{"responsibility_id": "BUILD-WORK-PACKAGE", "disposition": "assigned", "trigger": "one approved RepairAssignment owns this bounded root-cause unit", "assignment_ids": []string{manifestAssignmentID}, "na_rationale": nil, "evidence_ref": planRef.Path}},
		"assignments": []any{map[string]any{
			"assignment_id": manifestAssignmentID, "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": *roleFamily,
			"scope": append([]string(nil), target.Scope...), "agent_id": *agentID, "agent_definition_ref": definitionPath,
			"skill_refs": []string{"code-quality", "testing-strategy", "api-contracts", "state-machine-design", "integration-verification"},
			"read_paths": []string{contractPath, sessionPath, planRef.Path, projectlayout.Protocol}, "write_paths": writePaths,
			"output_paths": []string{".claude/review/repair/", ".claude/evidence/"}, "depends_on": []string{}, "reuse_decision": "create",
			"grouping_rationale": groupingRationale, "status": "planned", "dispatch_mode": "plan_checkpoint",
		}},
		"separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1,
		"quantity_rationale": "One platform-dispatched Builder per immutable RepairAssignment; no quality-layer token cap is applied.",
		"validation":         map[string]any{"result": "pass", "missing_responsibilities": []any{}, "unresolved_conflicts": []any{}, "warnings": []any{}, "validated_at": time.Now().UTC().Format(time.RFC3339Nano)},
	}
	taskObjective := "execute the approved RepairAssignment and restore its mapped assertions"
	taskInstruction := "Read the approved RepairContract and supplied materials, perform actual prechecks, and record your PlanReport. Main then prepares the actual work location; implement or test your Assignment and record its RepairResult."
	taskNextAction := "record your own PlanReport, then Main prepares the actual work location for implementation or testing"
	if verificationMode {
		taskObjective = fmt.Sprintf("independently re-execute the approved RepairContract assertions of %s and record the targeted reverification artifact", target.AssignmentID)
		taskInstruction = "Read the approved RepairContract, RepairSession and RepairPlan; do not modify any product file; re-execute the contract assertions on the post-repair tree and commit `runtime repair targeted commit` with performing_assignment_id set to this verification assignment."
		taskNextAction = "re-execute the contract assertions independently and submit the targeted reverification artifact"
	}
	if runtime.PacketEnabled(snapshot.State) {
		taskInstruction = "Follow the exact native S9 brief supplied by Main. Use Claude Code native worktree isolation; perform actual prechecks and personally save your professional PlanReport before implementation. Continue the repair and real self-checks without activation or per-tool receipt, commit products when needed and save your professional Result in the actual worktree. Natural PreToolUse and successful PostToolUse observe ready answers, with Stop/SubagentStop as fallback through the original consumers; no extra path return or submission is required. Main owns actual integration and necessary checks; no workspace prepare/launch, Team or platform ID is required."
		taskNextAction = "Main passes the exact prepared native S9 brief to Claude Code Agent; Builder saves its professional Plan and Result, natural events observe ready answers, and Main integrates an accepted candidate"
		if verificationMode {
			taskInstruction = "Read the approved RepairContract, RepairSession and RepairPlan; independently re-execute the Contract assertions on the actual post-repair tree without modifying product files. Fill and save the exact TargetedReverification questionnaire supplied at the original targeted checkpoint. Natural PreToolUse and successful PostToolUse observe a ready answer, with Stop/SubagentStop as fallback through the original consumer; no extra path return or submission is required. Main supplies the original answer and TASK/manifest to the native Worker."
			taskNextAction = "independently check the Contract assertions and save the exact TargetedReverification answer for natural observation by the original consumer"
			if answerPath, pathErr := repairTargetedQuestionnairePath(root, snapshot.State, dispatchAssignmentID); pathErr == nil {
				taskInstruction += " Original answer path: " + answerPath + "; Main/system prepares this file when that responsibility becomes current."
				taskNextAction += ": " + answerPath
			}
		}
	}
	task := map[string]any{
		"task_id": taskID, "session_id": stringValue(pointer["session_id"]), "plan_id": plan.PlanID, "assignment_id": dispatchAssignmentID,
		"unit_ids": target.UnitIDs, "scope": target.Scope, "depends_on": target.DependsOn, "resource_locks": target.ResourceLocks,
		"state": "reviewed", "objective": taskObjective,
		"instruction": taskInstruction,
		"next_action": taskNextAction,
	}
	if verificationMode && runtime.PacketEnabled(snapshot.State) {
		task["targeted_authoring"] = repair.NewTargetedAuthoring(snapshot.State, target.AssignmentID, dispatchAssignmentID, *agentID)
		// The verifier is not registered yet. Its freshly generated original TASK
		// fixes the new protocol; selectors later verify the registered TASK digest.
		if legacy, pathErr := repairLeadQuestionnairePath(root, snapshot.State, "targeted-reverification-"+dispatchSlug(dispatchAssignmentID)+".json"); pathErr == nil {
			if normal, pathErr := repair.TargetedSharedAnswerPath(root, legacy); pathErr == nil {
				task["instruction"] = "Read the approved RepairContract, RepairSession and RepairPlan. In an independent native context sharing Main's checkout, inspect the actual Final product and perform the Contract assertions. Fill and save only your exact original Targeted answer at " + normal + " when prepared. Natural PreToolUse and successful PostToolUse observe a ready answer, with Stop/SubagentStop as fallback through the original consumer. Main does not author the independent result. No Builder PlanReport/Result, product changes, extra path return, submission or commit is required."
				task["next_action"] = "Main dispatches an independent context sharing the final Main checkout with this exact Targeted answer: " + normal
			}
		}
	}
	manifestBytes, err := marshalInvestigationDispatch(manifest)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", err))
		return 1
	}
	taskBytes, err := marshalInvestigationDispatch(task)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", err))
		return 1
	}
	manifestPath := resolveRootPath(root, manifestRel)
	taskPath := resolveRootPath(root, taskRel)
	if err := writeInvestigationDispatchFile(manifestPath, manifestBytes); err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", err))
		return 1
	}
	if err := writeInvestigationDispatchFile(taskPath, taskBytes); err != nil {
		_ = os.Remove(manifestPath)
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", err))
		return 1
	}
	expected, err := repairExpected(common)
	if err != nil {
		_ = os.Remove(manifestPath)
		_ = os.Remove(taskPath)
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", err))
		return 1
	}
	at, err := repairOccurred(common.occurred)
	if err != nil {
		_ = os.Remove(manifestPath)
		_ = os.Remove(taskPath)
		fmt.Fprintln(stderr, err)
		return 2
	}
	next, err := assignmentpkg.Register(root, statePath, journalPath, assignmentpkg.Request{
		ExpectedRevision: expected, ManifestPath: manifestPath, TaskID: taskID, TaskPath: taskPath,
		RepairAssignmentID: dispatchAssignmentID, RepairOwnerAgentID: *agentID, OccurredAt: at,
	})
	if err != nil {
		_ = os.Remove(manifestPath)
		_ = os.Remove(taskPath)
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", err))
		return 1
	}
	if runtime.PacketEnabled(snapshot.State) {
		fmt.Fprintf(stderr, "RepairAssignment %s (%s) dispatched to %s; manifest/task were generated internally (do not pass --manifest); next: %s\n", dispatchAssignmentID, manifestAssignmentID, *agentID, taskNextAction)
		response := map[string]any{"assignment_id": dispatchAssignmentID, "agent_id": *agentID, "workgroup_id": workgroupID, "task_id": taskID, "manifest_path": manifestRel, "task_path": taskRel, "revision": next.Revision, "next_action": taskNextAction}
		if verificationMode {
			response["independent_verification"], response["original_assignment_id"] = true, target.AssignmentID
		}
		return encodeJSON(stdout, response)
	}
	if verificationMode {
		fmt.Fprintf(stderr, "Independent verification assignment %s (%s) dispatched to %s; manifest/task were generated internally (do not pass --manifest); next: re-execute the RepairContract assertions and commit `runtime repair targeted commit --actor %s` with performing_assignment_id=%s\n", dispatchAssignmentID, manifestAssignmentID, *agentID, *agentID, dispatchAssignmentID)
		return encodeJSON(stdout, map[string]any{"assignment_id": dispatchAssignmentID, "agent_id": *agentID, "workgroup_id": workgroupID, "task_id": taskID, "manifest_path": manifestRel, "task_path": taskRel, "revision": next.Revision, "independent_verification": true, "original_assignment_id": target.AssignmentID, "next_action": "re-execute the contract assertions independently, then commit the targeted reverification with performing_assignment_id set to this verification assignment"})
	}
	fmt.Fprintf(stderr, "RepairAssignment %s dispatched to %s; manifest/task were generated internally (do not pass --manifest); next: record the Builder PlanReport, then Main normally prepares its actual work location\n", target.AssignmentID, *agentID)
	return encodeJSON(stdout, map[string]any{"assignment_id": target.AssignmentID, "agent_id": *agentID, "workgroup_id": workgroupID, "task_id": taskID, "manifest_path": manifestRel, "task_path": taskRel, "revision": next.Revision, "next_action": "record the Builder PlanReport, then Main normally prepares its actual work location"})
}

func runRuntimeRepairSessionOpen(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair session open", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair session open")
	common := repairFlags(fs)
	sessionID := fs.String("session-id", "", "RepairSession id")
	createdBy := fs.String("created-by", "", "session creator")
	reqID := fs.String("req-id", "", "bound REQ id")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	if *sessionID == "" || *createdBy == "" {
		fmt.Fprintln(stderr, "runtime repair session open requires --session-id and --created-by")
		return 2
	}
	expected, err := repairExpected(common)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair session open", err))
		return 1
	}
	at, err := repairOccurred(common.occurred)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	snapshot, session, ref, err := repair.OpenRepairSession(common.root, resolveRootPath(common.root, common.state), resolveRootPath(common.root, common.journal), repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: expected, Actor: common.actor, OccurredAt: at}, SessionID: *sessionID, CreatedBy: *createdBy, ReqID: *reqID})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair session open", err))
		return 1
	}
	fmt.Fprintf(stderr, "S9 RepairSession %s opened; next: runtime repair plan compile --plan-id <plan> --created-by <agent>\n", session.SessionID)
	return encodeJSON(stdout, map[string]any{"session": session, "artifact_ref": ref, "revision": snapshot.Revision})
}

func runRuntimeRepairPlanCompile(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair plan compile", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair plan compile")
	common := repairFlags(fs)
	planID := fs.String("plan-id", "", "RepairPlan id")
	createdBy := fs.String("created-by", "", "plan creator")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	if *planID == "" || *createdBy == "" {
		fmt.Fprintln(stderr, "runtime repair plan compile requires --plan-id and --created-by")
		return 2
	}
	expected, err := repairExpected(common)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair plan compile", err))
		return 1
	}
	at, err := repairOccurred(common.occurred)
	if err != nil {
		return 2
	}
	snapshot, plan, ref, err := repair.CompileRepairPlan(common.root, resolveRootPath(common.root, common.state), resolveRootPath(common.root, common.journal), repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: expected, Actor: common.actor, OccurredAt: at}, PlanID: *planID, CreatedBy: *createdBy})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair plan compile", err))
		return 1
	}
	if _, err := prepareRepairEntry(common.root, resolveRootPath(common.root, common.state), resolveRootPath(common.root, common.journal), snapshot.State); err != nil {
		fmt.Fprintln(stderr, "S9 RepairPlan compiled; Main division draft preparation: "+err.Error())
		return 1
	}
	fmt.Fprintf(stderr, "S9 RepairPlan %s compiled; next: dispatch each RepairAssignment; each Builder records its own PlanReport before Main prepares its normal work location\n", plan.PlanID)
	return encodeJSON(stdout, map[string]any{"plan": plan, "artifact_ref": ref, "revision": snapshot.Revision})
}

func runRuntimeRepairPlanReportSubmit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair plan-report submit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair plan-report submit")
	common := repairFlags(fs)
	file := fs.String("file", "", "PlanReport request JSON path")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(stderr, "runtime repair plan-report submit requires --file")
		return 2
	}
	var request repair.PlanReportRequest
	if err := readJSONFile(common.root, *file, &request); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	expected, err := repairExpected(common)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair plan-report submit", err))
		return 1
	}
	at, err := repairOccurred(common.occurred)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	report, ref, err := repair.CreatePlanReport(common.root, request)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair plan-report create", err))
		return 1
	}
	snapshot, report, err := repair.SubmitRepairPlanReportToRuntime(common.root, resolveRootPath(common.root, common.state), resolveRootPath(common.root, common.journal), repair.SubmitPlanReportRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: expected, Actor: common.actor, OccurredAt: at}, Report: ref})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair plan-report submit", err))
		return 1
	}
	_ = report
	fmt.Fprintln(stderr, "PlanReport recorded; Main owns normal work startup and preparation of this Builder’s actual work location")
	return encodeJSON(stdout, map[string]any{"report": report, "artifact_ref": ref, "revision": snapshot.Revision})
}

func runRuntimeRepairExecutionBegin(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair execution begin", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair execution begin")
	common := repairFlags(fs)
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	expected, err := repairExpected(common)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair execution begin", err))
		return 1
	}
	at, err := repairOccurred(common.occurred)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	snapshot, err := repair.BeginRepairExecution(common.root, resolveRootPath(common.root, common.state), resolveRootPath(common.root, common.journal), repair.BeginRepairExecutionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: expected, Actor: common.actor, OccurredAt: at}})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair execution begin", err))
		return 1
	}
	fmt.Fprintln(stderr, "S9 repair execution started; next: submit one exact-unit RepairResult for each bound Assignment")
	return encodeJSON(stdout, map[string]any{"revision": snapshot.Revision, "lifecycle": snapshot.State["lifecycle"]})
}

func runRuntimeRepairResultSubmit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair result submit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair result submit")
	common := repairFlags(fs)
	file := fs.String("file", "", "RepairResult JSON path")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(stderr, "runtime repair result submit requires --file")
		return 2
	}
	var request repair.RepairResultRequest
	if err := readJSONFile(common.root, *file, &request); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	expected, err := repairExpected(common)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair result submit", err))
		return 1
	}
	at, err := repairOccurred(common.occurred)
	if err != nil {
		return 2
	}
	snapshot, result, ref, err := repair.SubmitRepairResultToRuntime(common.root, resolveRootPath(common.root, common.state), resolveRootPath(common.root, common.journal), repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: expected, Actor: common.actor, OccurredAt: at}, Result: request})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair result submit", err))
		return 1
	}
	next := "create and commit ChangeImpact"
	if review, ok := snapshot.State["review"].(map[string]any); ok {
		if pointer, ok := review["repair"].(map[string]any); ok {
			switch pointer["status"] {
			case "repairing":
				next = "submit the remaining Assignment RepairResults; the complete batch is required before ChangeImpact"
			case "blocked":
				next = "follow the recorded S8 recovery route; do not patch the symptom locally"
			}
		}
	}
	fmt.Fprintf(stderr, "RepairResult %s accepted; next: %s\n", result.ResultID, next)
	return encodeJSON(stdout, map[string]any{"result": result, "artifact_ref": ref, "revision": snapshot.Revision})
}

func runRuntimeRepairChangesetCompute(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair changeset compute", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair changeset compute")
	root := fs.String("root", ".", "repository root")
	sessionID := fs.String("session-id", "", "RepairSession id")
	baseRef := fs.String("base-ref", "", "git base ref")
	headRef := fs.String("head-ref", "", "git head ref")
	paths := fs.String("paths", "", "comma-separated repository-relative paths")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	if *sessionID == "" {
		fmt.Fprintln(stderr, "runtime repair changeset compute requires --session-id")
		return 2
	}
	explicit := []string{}
	for _, value := range strings.Split(*paths, ",") {
		if strings.TrimSpace(value) != "" {
			explicit = append(explicit, strings.TrimSpace(value))
		}
	}
	var changeset repair.Changeset
	var err error
	if len(explicit) == 0 && strings.TrimSpace(*baseRef) == "" && strings.TrimSpace(*headRef) == "" {
		sessionPath := ".claude/review/repair/sessions/" + *sessionID + ".json"
		sessionRef, refErr := artifactRef(*root, sessionPath, *sessionID)
		if refErr != nil {
			fmt.Fprintln(stderr, formatFailure("runtime repair changeset compute", refErr))
			return 1
		}
		session, sessionErr := repair.ValidateRepairSession(*root, sessionRef)
		if sessionErr != nil {
			fmt.Fprintln(stderr, formatFailure("runtime repair changeset compute", sessionErr))
			return 1
		}
		changeset, err = repair.ComputeSessionChangesetRecord(*root, session)
	} else {
		changeset, err = repair.ComputeChangeset(*root, repair.ChangesetRequest{SessionID: *sessionID, BaseRef: *baseRef, HeadRef: *headRef, ExplicitPaths: explicit})
	}
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair changeset compute", err))
		return 1
	}
	ref, err := repair.PersistChangeset(*root, changeset)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair changeset compute", err))
		return 1
	}
	return encodeJSON(stdout, map[string]any{"changeset": changeset, "artifact_ref": ref})
}

func runRuntimeRepairImpactCreate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair impact create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair impact create")
	root := fs.String("root", ".", "repository root")
	file := fs.String("file", "", "ChangeImpact request JSON path")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(stderr, "runtime repair impact create requires --file")
		return 2
	}
	var request repair.ChangeImpactRequest
	if err := readJSONFile(*root, *file, &request); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	impact, ref, err := repair.CreateChangeImpact(*root, request)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair impact create", err))
		return 1
	}
	return encodeJSON(stdout, map[string]any{"impact": impact, "artifact_ref": ref})
}

func runRuntimeRepairTargetedCreate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair targeted create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair targeted create")
	root := fs.String("root", ".", "repository root")
	file := fs.String("file", "", "TargetedReverification request JSON path")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(stderr, "runtime repair targeted create requires --file")
		return 2
	}
	var request repair.TargetedReverificationRequest
	if err := readJSONFile(*root, *file, &request); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	value, ref, err := repair.CreateTargetedReverification(*root, request)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair targeted create", err))
		return 1
	}
	return encodeJSON(stdout, map[string]any{"targeted_reverification": value, "artifact_ref": ref})
}

func runRuntimeRepairHandoffCreate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair handoff create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair handoff create")
	root := fs.String("root", ".", "repository root")
	file := fs.String("file", "", "RepairHandoff request JSON path")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(stderr, "runtime repair handoff create requires --file")
		return 2
	}
	var request repair.HandoffRequest
	if err := readJSONFile(*root, *file, &request); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	handoff, ref, err := repair.CreateRepairHandoff(*root, request)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair handoff create", err))
		return 1
	}
	return encodeJSON(stdout, map[string]any{"handoff": handoff, "artifact_ref": ref})
}

func runRuntimeRepairImpactCommit(args []string, stdout, stderr io.Writer) int {
	return runRepairArtifactCommit(args, stdout, stderr, "runtime repair impact commit", func(root, state, journal string, req repair.CommitImpactRequest) (runtime.Snapshot, error) {
		return repair.CommitChangeImpact(root, state, journal, req)
	})
}
func runRuntimeRepairTargetedCommit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair targeted commit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair targeted commit")
	common := repairFlags(fs)
	file := fs.String("file", "", "TargetedReverification JSON path")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(stderr, "runtime repair targeted commit requires --file")
		return 2
	}
	ref, err := artifactRef(common.root, *file, "")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	expected, err := repairExpected(common)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair targeted commit", err))
		return 1
	}
	at, err := repairOccurred(common.occurred)
	if err != nil {
		return 2
	}
	snapshot, err := repair.CommitTargetedReverification(common.root, resolveRootPath(common.root, common.state), resolveRootPath(common.root, common.journal), repair.CommitTargetedRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: expected, Actor: common.actor, OccurredAt: at}, Reverification: ref})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair targeted commit", err))
		return 1
	}
	return encodeJSON(stdout, map[string]any{"artifact_ref": ref, "revision": snapshot.Revision})
}

func runRuntimeRepairTargetedResume(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair targeted resume", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair targeted resume")
	common := repairFlags(fs)
	reason := fs.String("reason", "", "why the targeted verification blocker is resolved")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	if strings.TrimSpace(*reason) == "" {
		fmt.Fprintln(stderr, "runtime repair targeted resume requires --reason <resolution>")
		return 2
	}
	expected, err := repairExpected(common)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair targeted resume", err))
		return 1
	}
	at, err := repairOccurred(common.occurred)
	if err != nil {
		return 2
	}
	snapshot, err := repair.ResumeTargetedReverification(common.root, resolveRootPath(common.root, common.state), resolveRootPath(common.root, common.journal), repair.ResumeTargetedRequest{
		RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: expected, Actor: common.actor, OccurredAt: at}, Reason: *reason,
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair targeted resume", err))
		return 1
	}
	fmt.Fprintln(stderr, "targeted verification blocker resolved; next: create and commit a new independent targeted reverification")
	return encodeJSON(stdout, map[string]any{"revision": snapshot.Revision, "lifecycle": snapshot.State["lifecycle"], "next_action": "create and commit a new independent targeted reverification"})
}

// runRepairArtifactCommit is kept for impact; targeted and handoff use their
// typed wrappers below because their artifact pointer types differ.
func runRepairArtifactCommit(args []string, stdout, stderr io.Writer, label string, commit func(string, string, string, repair.CommitImpactRequest) (runtime.Snapshot, error)) int {
	fs := flag.NewFlagSet(label, flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, label)
	common := repairFlags(fs)
	file := fs.String("file", "", "artifact JSON path")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(stderr, label+" requires --file")
		return 2
	}
	ref, err := artifactRef(common.root, *file, "")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	expected, err := repairExpected(common)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure(label, err))
		return 1
	}
	at, err := repairOccurred(common.occurred)
	if err != nil {
		return 2
	}
	snapshot, err := commit(common.root, resolveRootPath(common.root, common.state), resolveRootPath(common.root, common.journal), repair.CommitImpactRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: expected, Actor: common.actor, OccurredAt: at}, Impact: ref})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure(label, err))
		return 1
	}
	return encodeJSON(stdout, map[string]any{"artifact_ref": ref, "revision": snapshot.Revision})
}

func runRuntimeRepairHandoffCommit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair handoff commit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair handoff commit")
	common := repairFlags(fs)
	file := fs.String("file", "", "RepairHandoff JSON path")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(stderr, "runtime repair handoff commit requires --file")
		return 2
	}
	ref, err := artifactRef(common.root, *file, "")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	expected, err := repairExpected(common)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair handoff commit", err))
		return 1
	}
	at, err := repairOccurred(common.occurred)
	if err != nil {
		return 2
	}
	snapshot, err := repair.CommitRepairHandoff(common.root, resolveRootPath(common.root, common.state), resolveRootPath(common.root, common.journal), repair.CommitHandoffRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: expected, Actor: common.actor, OccurredAt: at}, Handoff: ref})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair handoff commit", err))
		return 1
	}
	return encodeJSON(stdout, map[string]any{"artifact_ref": ref, "revision": snapshot.Revision})
}

func runRuntimeRepairStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair status")
	root := fs.String("root", ".", "repository root")
	state := fs.String("state", ".claude/loop-state.json", "runtime state path")
	journal := fs.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	snapshot, err := runtime.NewStore(resolveRootPath(*root, *state), resolveRootPath(*root, *journal)).Snapshot()
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair status", err))
		return 1
	}
	review, _ := snapshot.State["review"].(map[string]any)
	pointer := map[string]any(nil)
	if review != nil {
		pointer, _ = review["repair"].(map[string]any)
	}
	if pointer == nil {
		return encodeJSON(stdout, map[string]any{"status": "not_open", "revision": snapshot.Revision, "next": "runtime repair session open --session-id <session> --created-by <agent>"})
	}
	response := map[string]any{"repair": pointer, "revision": snapshot.Revision, "next": pointer["next_action"]}
	if stringValue(pointer["plan_ref"]) != "" {
		board, boardErr := repairAssignmentBoard(*root, snapshot.State, runtime.PacketEnabled(snapshot.State))
		if boardErr != nil {
			fmt.Fprintln(stderr, formatFailure("runtime repair status", boardErr))
			return 1
		}
		response["board"] = board
		response["next"] = board["next_action"]
		if runtime.PacketEnabled(snapshot.State) && (stringValue(pointer["status"]) == "ready_for_full_review" || stringValue(pointer["handoff_ref"]) != "") {
			if projection := repairHandoffProjectionContext(*root, snapshot.State); projection != "" {
				response["next"], board["next_action"] = projection, projection
			}
		}
		if runtime.PacketEnabled(snapshot.State) && s9RepairResultStage(snapshot.State) {
			rows, next := repairResultPacketStatus(*root, snapshot.State)
			response["repair_result_packets"] = rows
			response["next"], board["next_action"] = next, next
		}
		if runtime.PacketEnabled(snapshot.State) && s9PlanReportPreExecutionState(snapshot.State) {
			rows, _, reportErr := repairPlanReportPacketStatus(*root, snapshot.State, board)
			if reportErr != nil {
				fmt.Fprintln(stderr, formatFailure("runtime repair status", reportErr))
				return 1
			}
			response["plan_report_packets"] = rows
		}
	}
	if resultPath, ok := pointer["result_ref"].(string); ok && strings.TrimSpace(resultPath) != "" {
		summary, summaryErr := readRepairResultSummary(*root, resultPath)
		if summaryErr != nil {
			fmt.Fprintln(stderr, formatFailure("runtime repair status", summaryErr))
			return 1
		}
		response["result_summary"] = summary
	}
	return encodeJSON(stdout, response)
}

func s9RepairResultStage(state map[string]any) bool {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	return lifecycle["state"] == "bug_resolution" && lifecycle["phase"] == "fixing"
}

// Native Result packets are derived machine inputs. The author uses the
// original Plan's native brief and frozen worktree-relative professional path.
func nativeRepairResultStatusPaths(root string, state map[string]any, packet workpacket.RepairResultPacket, execution map[string]any) (string, string, error) {
	plans, err := runtime.CurrentRepairPlanReportPackets(root, state, packet.Binding.Sources.OwnerAgentID)
	if err != nil {
		return "", "", fmt.Errorf("read original native Plan packet: %w", err)
	}
	var original *workpacket.RepairPlanReportPacket
	for i := range plans {
		if plans[i].Binding == packet.Binding.Sources {
			if original != nil {
				return "", "", fmt.Errorf("multiple original native Plan packets for this Result")
			}
			original = &plans[i]
		}
	}
	if original == nil {
		return "", "", fmt.Errorf("original native Plan packet is unavailable for this Result")
	}
	brief, pin, _ := nativeS9Paths(*original)
	brief = resolveRootPath(root, brief)
	if info, err := os.Stat(brief); err != nil {
		return "", "", fmt.Errorf("original native brief %s: %w", brief, err)
	} else if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("original native brief %s is not a regular file", brief)
	}
	planPath, resultPath, err := nativeS9AuthorPaths(root, *original)
	if err != nil {
		return "", "", fmt.Errorf("original native author paths: %w", err)
	}
	// New private originals pin the source contract in the frozen execution.
	// Older native responsibilities use their previously frozen paths instead.
	if planPath != original.AnswerPath {
		expected := stringValue(mapFieldCLI(execution, "inputs")[pin])
		if expected == "" {
			return "", "", fmt.Errorf("original native author source %s is not pinned by its execution", pin)
		}
		raw, readErr := workpacket.ReadFile(root, pin, 4096)
		if readErr != nil {
			return "", "", fmt.Errorf("original native author source %s: %w", pin, readErr)
		}
		if workpacket.Digest(raw) != expected {
			return "", "", fmt.Errorf("original native author source %s differs from the frozen execution", pin)
		}
	}
	worker := stringValue(execution["worktree_path"])
	if !filepath.IsAbs(worker) || filepath.Clean(worker) != worker {
		return "", "", fmt.Errorf("original native Worker root is unavailable or noncanonical")
	}
	return brief, filepath.Join(worker, filepath.FromSlash(resultPath)), nil
}

func repairResultPacketStatus(root string, state map[string]any) ([]any, string) {
	packets, err := runtime.RepairResultPacketsContext(runtime.WithPacketBodyRoot(context.Background(), root), state)
	if err != nil {
		return []any{}, "Runtime S9 Result packet registry needs recovery before status can verify it: " + err.Error()
	}
	baseline, _ := state["baseline"].(map[string]any)
	reviewState, _ := state["review"].(map[string]any)
	round := intFieldCLI(reviewState["round"])
	generation := intFieldCLI(baseline["generation"])
	rows := []any{}
	var problems, issuedActions []string
	anyStale, anyIssued, allComplete := false, false, len(packets) > 0
	for _, packet := range packets {
		binding := packet.Binding.Sources
		if binding.RuntimeID != stringValue(state["runtime_id"]) || binding.BaselineGeneration != generation || binding.ReviewRound != round {
			continue
		}
		status := "issued"
		currentErr := runtime.VerifyRepairResultPacketIssue(root, state, packet)
		if currentErr == nil {
			currentErr = runtime.VerifyRepairResultPacketCurrent(root, state, packet)
		}
		execution, executionErr := runtime.RepairResultExecution(state, packet)
		if currentErr == nil && executionErr != nil {
			currentErr = executionErr
		}
		native := executionErr == nil && execution["purpose"] == workspace.NativeDeliveryPurpose
		viewPath, answerPath := packet.ViewPath, packet.AnswerPath
		if currentErr != nil {
			status = "stale_or_preserved"
			anyStale = true
			allComplete = false
			problems = append(problems, binding.RepairAssignmentID+": "+currentErr.Error())
		} else {
			receipt, accepted, receiptErr := runtime.VerifiedRepairResultCandidateReceipt(root, state, packet.ID, binding.OwnerAgentID)
			if receiptErr != nil {
				status = "candidate_receipt_unavailable"
				anyStale = true
				allComplete = false
				problems = append(problems, binding.RepairAssignmentID+": "+receiptErr.Error())
			} else if accepted {
				complete, completeErr := repair.RepairResultWorkComplete(root, state, packet.ID, binding.OwnerAgentID)
				if completeErr != nil {
					status = "candidate_accepted_owner_proof_unavailable"
					anyStale = true
					allComplete = false
					problems = append(problems, binding.RepairAssignmentID+": "+completeErr.Error())
				} else if complete {
					status = "worker_complete_main_pending"
				} else {
					status = "candidate_accepted_preserve_and_recover"
					anyStale = true
					allComplete = false
				}
				_ = receipt
			} else {
				allComplete = false
				if native {
					viewPath, answerPath, err = nativeRepairResultStatusPaths(root, state, packet, execution)
					if err != nil {
						status = "stale_or_preserved"
						anyStale = true
						problems = append(problems, binding.RepairAssignmentID+": "+err.Error())
					}
				}
				if status == "issued" {
					anyIssued = true
					issuedActions = append(issuedActions, fmt.Sprintf("Assignment %s owner=%s: original brief/question %s; author answer %s", binding.RepairAssignmentID, binding.OwnerAgentID, viewPath, answerPath))
				}
			}
		}
		row := map[string]any{
			"packet_id": packet.ID, "repair_assignment_id": binding.RepairAssignmentID,
			"manifest_assignment_id": binding.ManifestAssignmentID, "owner_agent_id": binding.OwnerAgentID,
			"status":          status,
			"receipt_command": fmt.Sprintf("runtime stage-answer receipt --agent %s --packet %s", binding.OwnerAgentID, packet.ID),
		}
		if !native && executionErr == nil || native && status == "issued" {
			row["view_path"], row["answer_path"] = viewPath, answerPath
		}
		rows = append(rows, row)
	}
	if anyStale {
		next := "Preserve original S9 candidate evidence; a frozen source, packet, or owner-check proof needs the original named rework/recovery path. Do not submit a legacy Result."
		if len(problems) > 0 {
			next += " Current detail: " + strings.Join(problems, "; ")
		}
		return rows, next
	}
	if len(rows) == 0 {
		if handoffs := s9KnownMainHandoffsContext(context.Background(), root, runtime.Snapshot{State: state}); handoffs != "" {
			return rows, handoffs
		}
		return rows, "Continue the original native Builder responsibility using its exact prepared brief; when the candidate is recorded Main performs the original integration and actual checks. If original material is unavailable, Main/system restores that original preparation; existing frozen executions retain their original recovery path."
	}
	if anyIssued {
		return rows, "Builders follow their original responsibilities and save their own answers: " + strings.Join(issuedActions, "; ") + ". Natural PreToolUse and successful PostToolUse observe ready answers, with Stop/SubagentStop as fallback; an accepted candidate still requires original Main integration."
	}
	if allComplete {
		commands := []string{}
		for _, row := range rows {
			packet := row.(map[string]any)
			commands = append(commands, fmt.Sprintf("runtime workspace integrate --root %q --assignment %q --agent %q --check-command <actual necessary Main check command>", root, packet["manifest_assignment_id"], packet["owner_agent_id"]))
		}
		return rows, "Worker candidates and own checks are complete; Main performs actual integration and records Final using: " + strings.Join(commands, "; ") + ". Main selects and runs checks for the actual repair; repeat --check-command for multiple checks. Use this typed S9 workspace command, not generic runtime task-integrate."
	}
	return rows, "keep the original S9 execution active and follow its named recovery action"
}

func s9PlanReportPreExecutionState(state map[string]any) bool {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	return lifecycle["state"] == "bug_resolution" &&
		(lifecycle["phase"] == "planning" || lifecycle["phase"] == "reproducing")
}

func repairPlanReportPacketStatus(root string, state map[string]any, board map[string]any) ([]any, []string, error) {
	packets, err := runtime.CurrentRepairPlanReportPackets(root, state, "")
	if err != nil {
		return nil, nil, err
	}
	missing := []string{}
	if raw, ok := board["missing_reports"].([]string); ok {
		missing = append(missing, raw...)
	}
	byAssignment := map[string]bool{}
	rows := make([]any, 0, len(packets))
	for _, packet := range packets {
		if err := runtime.VerifyRepairPlanReportPacketIssue(root, state, packet); err != nil {
			return nil, nil, fmt.Errorf("S9 packet %s original issuance proof: %w", packet.ID, err)
		}
		if err := runtime.VerifyRepairPlanReportCurrent(root, state, packet); err != nil {
			return nil, nil, fmt.Errorf("S9 packet %s current dispatch proof: %w", packet.ID, err)
		}
		status := "issued"
		if receipt, accepted, err := runtime.VerifiedRepairPlanReportReceipt(root, state, packet.ID, packet.Binding.OwnerAgentID); err != nil {
			return nil, nil, err
		} else if accepted {
			if receipt.WorkComplete {
				return nil, nil, fmt.Errorf("S9 PlanReport packet %s receipt incorrectly claims work completion", packet.ID)
			}
			status = "accepted"
		}
		byAssignment[packet.Binding.RepairAssignmentID] = true
		rows = append(rows, map[string]any{
			"packet_id": packet.ID, "assignment_id": packet.Binding.RepairAssignmentID,
			"owner_agent_id": packet.Binding.OwnerAgentID, "status": status,
			"view_path": packet.ViewPath, "answer_path": packet.AnswerPath,
			"receipt_command": fmt.Sprintf("runtime stage-answer receipt --agent %s --packet %s", packet.Binding.OwnerAgentID, packet.ID),
		})
	}
	unissued := []string{}
	for _, assignmentID := range missing {
		if !byAssignment[assignmentID] {
			unissued = append(unissued, assignmentID)
		}
	}
	sort.Strings(unissued)
	return rows, unissued, nil
}

// repairAssignmentBoard is a read-only projection of the complete S9 batch.
// The RepairPlan remains the static authority; report/result refs and the
// Runtime phase provide progress. Keeping this projection derived means
// recovery can show every Assignment, dependency, lock and missing artifact
// without adding a second mutable scheduler state.
func repairAssignmentBoard(root string, state map[string]any, packetMode bool) (map[string]any, error) {
	pointer := repairPointerCLI(state)
	planRef := repair.ArtifactRef{Path: stringValue(pointer["plan_ref"]), SHA256: stringValue(pointer["plan_sha256"])}
	plan, err := repair.ValidateRepairPlan(root, planRef)
	if err != nil {
		return nil, fmt.Errorf("current RepairPlan is invalid: %w", err)
	}
	reportRefs := repairArtifactRefs(pointer, "plan_report_refs", "plan_report_ref", "plan_report_sha256")
	resultRefs := repairArtifactRefs(pointer, "result_refs", "result_ref", "result_sha256")
	reports := map[string]repair.PlanReport{}
	reportPaths := map[string]string{}
	for index, ref := range reportRefs {
		report, reportErr := repair.ValidatePlanReport(root, ref)
		if reportErr != nil {
			return nil, fmt.Errorf("current PlanReport[%d] is invalid: %w", index, reportErr)
		}
		if prior, exists := reports[report.AssignmentID]; exists && prior.ReportID != report.ReportID {
			return nil, fmt.Errorf("current Runtime has duplicate PlanReports for Assignment %s", report.AssignmentID)
		}
		reports[report.AssignmentID] = report
		reportPaths[report.AssignmentID] = ref.Path
	}
	results := map[string]repair.RepairResult{}
	resultPaths := map[string]string{}
	for index, ref := range resultRefs {
		result, resultErr := repair.ValidateRepairResult(root, ref)
		if resultErr != nil {
			return nil, fmt.Errorf("current RepairResult[%d] is invalid: %w", index, resultErr)
		}
		if prior, exists := results[result.AssignmentID]; exists && prior.ResultID != result.ResultID {
			return nil, fmt.Errorf("current Runtime has duplicate RepairResults for Assignment %s", result.AssignmentID)
		}
		results[result.AssignmentID] = result
		resultPaths[result.AssignmentID] = ref.Path
	}
	unitOwners := map[string]string{}
	for _, assignment := range plan.Assignments {
		for _, unitID := range assignment.UnitIDs {
			unitOwners[unitID] = assignment.AssignmentID
		}
	}
	// A PlanReport reserves an Assignment's declared resource locks until its
	// Result is consumed. The first Assignment in the immutable plan order is
	// the deterministic holder when two reports mention the same lock; this is
	// the same tie-break used by Result submission and prevents a deadlock in
	// the read-only board.
	lockHolders := map[string]string{}
	for _, assignment := range plan.Assignments {
		if _, hasReport := reports[assignment.AssignmentID]; !hasReport {
			continue
		}
		if _, hasResult := results[assignment.AssignmentID]; hasResult {
			continue
		}
		for _, lock := range assignment.ResourceLocks {
			if _, held := lockHolders[lock]; !held {
				lockHolders[lock] = assignment.AssignmentID
			}
		}
	}
	owners, _ := pointer["assignment_owners"].(map[string]any)
	lifecycle, _ := pointer["_lifecycle"].(map[string]any)
	phase := stringValue(lifecycle["phase"])
	if phase == "" {
		phase = stringValue(pointer["status"])
	}
	rows := make([]any, 0, len(plan.Assignments))
	missingReports := []string{}
	missingResults := []string{}
	ready := []string{}
	queued := []string{}
	blocked := []string{}
	for _, assignment := range plan.Assignments {
		row := map[string]any{
			"assignment_id":  assignment.AssignmentID,
			"unit_ids":       assignment.UnitIDs,
			"depends_on":     assignment.DependsOn,
			"resource_locks": assignment.ResourceLocks,
			"owner_agent_id": stringValue(owners[assignment.AssignmentID]),
			"report_ref":     reportPaths[assignment.AssignmentID],
			"result_ref":     resultPaths[assignment.AssignmentID],
		}
		if assignment.OwnerAgentID != "" && stringValue(row["owner_agent_id"]) == "" {
			row["owner_agent_id"] = assignment.OwnerAgentID
		}
		if _, ok := reports[assignment.AssignmentID]; !ok {
			missingReports = append(missingReports, assignment.AssignmentID)
		}
		if result, ok := results[assignment.AssignmentID]; ok {
			row["result"] = result.Result
			row["status"] = workstate.Done
			if result.Result != "pass" {
				row["status"] = workstate.Blocked
				blocked = append(blocked, assignment.AssignmentID)
			}
		} else {
			missingResults = append(missingResults, assignment.AssignmentID)
			status := workstate.Pending
			if _, ok := reports[assignment.AssignmentID]; ok {
				status = workstate.Active
				if phase == "repairing" || stringValue(pointer["status"]) == "repairing" {
					status = workstate.Active
				}
			}
			dependencyBlock := ""
			for _, dependencyUnit := range assignment.DependsOn {
				dependencyID := unitOwners[dependencyUnit]
				dependency, dependencyDone := results[dependencyID]
				if !dependencyDone {
					dependencyBlock = fmt.Sprintf("dependency %s (Assignment %s) has no passing Result", dependencyUnit, dependencyID)
					break
				}
				if dependency.Result != "pass" {
					dependencyBlock = fmt.Sprintf("dependency %s (Assignment %s) returned %s", dependencyUnit, dependencyID, dependency.Result)
					break
				}
			}
			lockBlock := ""
			for _, lock := range assignment.ResourceLocks {
				if holder := lockHolders[lock]; holder != "" && holder != assignment.AssignmentID {
					lockBlock = fmt.Sprintf("resource lock %s is held by Assignment %s", lock, holder)
					break
				}
			}
			if dependencyBlock != "" {
				status = workstate.Pending
				row["queue_reason"] = dependencyBlock
				queued = append(queued, assignment.AssignmentID)
			} else if lockBlock != "" {
				status = workstate.Pending
				row["queue_reason"] = lockBlock
				queued = append(queued, assignment.AssignmentID)
			} else if status == workstate.Active {
				ready = append(ready, assignment.AssignmentID)
			}
			row["status"] = status
		}
		locks := make([]map[string]any, 0, len(assignment.ResourceLocks))
		for _, lock := range assignment.ResourceLocks {
			lockState := "available"
			lockRow := map[string]any{"lock": lock, "state": lockState}
			if _, done := results[assignment.AssignmentID]; done {
				lockState = "released"
			} else if holder := lockHolders[lock]; holder != "" {
				lockState = "held"
				if holder != assignment.AssignmentID {
					lockRow["held_by"] = holder
				}
			}
			lockRow["state"] = lockState
			locks = append(locks, lockRow)
		}
		row["lock_state"] = locks
		rows = append(rows, row)
	}
	sort.Strings(missingReports)
	sort.Strings(missingResults)
	sort.Strings(ready)
	sort.Strings(queued)
	sort.Strings(blocked)
	next := stringValue(pointer["next_action"])
	status := stringValue(pointer["status"])
	switch status {
	case "planning", "reproducing":
		if packetMode {
			next = repairPlanningNextAction(state, root)
		} else if len(missingReports) > 0 {
			next = "submit one S9 PlanReport for each missing Assignment: " + strings.Join(missingReports, ", ")
		} else {
			next = "recorded Builder Plans are ready for Main to prepare their actual work locations with runtime workspace prepare"
		}
	case "repairing":
		if len(blocked) > 0 {
			next = "inspect blocked RepairResults " + strings.Join(blocked, ", ") + "; route the causal failure to S8"
		} else if len(ready) > 0 {
			if packetMode {
				next = "the selected Builder continues the original responsibility for ready Assignment " + ready[0] + "; natural events observe its saved Result and Main integrates an accepted candidate"
			} else {
				next = "submit the exact-unit RepairResult for ready Assignment " + ready[0]
			}
		} else if len(queued) > 0 {
			next = "wait for queued Assignment dependencies/locks; inspect each queue_reason before retrying"
		} else if len(missingResults) > 0 {
			if packetMode {
				next = "continue each remaining original Builder responsibility and save its exact Result; natural events observe ready answers and Main integrates accepted candidates"
			} else {
				next = "submit one exact-unit RepairResult for each remaining Assignment"
			}
		}
	case "impact_reconciliation":
		next = "compute the session-wide Changeset, then commit ChangeImpact"
		if packetMode {
			next = "Main fills and saves the exact ChangeImpact questionnaire supplied by the listener; the original consumer records its professional change analysis. If missing, resume original preparation with " + repairImpactPreparationRecovery(root)
		}
	case "targeted_reverification":
		next = "submit the independent targeted reverification"
		if packetMode {
			next = "the registered independent verifier fills and saves the exact TargetedReverification questionnaire; natural PreToolUse and successful PostToolUse observe ready answers, with Stop/SubagentStop as fallback through the original consumer"
		}
	case "ready_for_full_review":
		next = "create and commit the complete RepairHandoff; S7 then registers a fresh full ReviewPlan"
		if packetMode {
			next = repairHandoffPendingInstruction
		}
	}
	return map[string]any{
		"plan_id":         plan.PlanID,
		"status":          status,
		"assignments":     rows,
		"ready":           ready,
		"queued":          queued,
		"blocked":         blocked,
		"missing_reports": missingReports,
		"missing_results": missingResults,
		"complete":        len(missingResults) == 0 && len(blocked) == 0,
		"next_action":     next,
	}, nil
}

func repairArtifactRefs(pointer map[string]any, listKey, pathKey, hashKey string) []repair.ArtifactRef {
	refs := []repair.ArtifactRef{}
	if raw, ok := pointer[listKey].([]any); ok {
		for _, value := range raw {
			row, _ := value.(map[string]any)
			if row == nil || stringValue(row["path"]) == "" {
				continue
			}
			refs = append(refs, repair.ArtifactRef{Path: stringValue(row["path"]), SHA256: stringValue(row["sha256"])})
		}
	}
	if len(refs) == 0 && stringValue(pointer[pathKey]) != "" {
		refs = append(refs, repair.ArtifactRef{Path: stringValue(pointer[pathKey]), SHA256: stringValue(pointer[hashKey])})
	}
	return refs
}

func readRepairResultSummary(root, path string) (map[string]any, error) {
	data, err := os.ReadFile(resolveRootPath(root, path))
	if err != nil {
		return nil, fmt.Errorf("read RepairResult %s: %w", path, err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode RepairResult %s: %w", path, err)
	}
	changed, _ := result["changed_artifacts"].([]any)
	risks, _ := result["residual_risks"].([]any)
	status, _ := result["result"].(string)
	summary := map[string]any{
		"result_id":              result["result_id"],
		"assignment_id":          result["assignment_id"],
		"result":                 status,
		"changed_artifact_count": len(changed),
		"residual_risks":         risks,
	}
	if status != "pass" {
		summary["recovery"] = "inspect this Result's blocker/residual_risks; route the causal question to S8 before creating another local patch"
	}
	return summary, nil
}

func readJSONFile(root, name string, target any) error {
	path, err := repoFile(root, name)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return nil
}

func stringMapCLI(value any) map[string]string {
	result := map[string]string{}
	values, _ := value.(map[string]any)
	for key, raw := range values {
		if text, ok := raw.(string); ok {
			result[key] = text
		}
	}
	return result
}

func s9RepairTaskID(sessionID, assignmentID string) string {
	sum := sha256.Sum256([]byte(sessionID + "|" + assignmentID))
	number := uint64(sum[0])<<24 | uint64(sum[1])<<16 | uint64(sum[2])<<8 | uint64(sum[3])
	return fmt.Sprintf("TASK-9%d", number)
}

func artifactRef(root, name, id string) (repair.ArtifactRef, error) {
	path, err := repoFile(root, name)
	if err != nil {
		return repair.ArtifactRef{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return repair.ArtifactRef{}, err
	}
	sum := sha256.Sum256(data)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return repair.ArtifactRef{}, err
	}
	rel, err := filepath.Rel(rootAbs, path)
	if err != nil {
		return repair.ArtifactRef{}, err
	}
	return repair.ArtifactRef{ID: id, Path: filepath.ToSlash(rel), SHA256: hex.EncodeToString(sum[:])}, nil
}
func repoFile(root, name string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(rootAbs, filepath.FromSlash(name))
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes repository root", name)
	}
	return path, nil
}
