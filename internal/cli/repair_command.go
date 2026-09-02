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

	assignmentpkg "github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
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
		if len(args) < 2 || (args[1] != "commit" && args[1] != "create") {
			fmt.Fprintln(stderr, "runtime repair impact requires <create|commit>")
			return 2
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
		if len(args) < 2 || (args[1] != "commit" && args[1] != "create") {
			fmt.Fprintln(stderr, "runtime repair handoff requires <create|commit>")
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
	definitionRef := fs.String("agent-definition", "agents/backend-builder.md", "Builder Agent Definition path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*assignmentID) == "" || strings.TrimSpace(*agentID) == "" {
		fmt.Fprintln(stderr, "runtime repair dispatch requires --assignment-id and --agent-id")
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
	if status != "planning" && status != "reproducing" {
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
	owners := stringMapCLI(pointer["assignment_owners"])
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
	definitionBytes, err := os.ReadFile(resolveRootPath(root, *definitionRef))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", fmt.Errorf("read Agent Definition (dispatch derives the worker capability set — allowed tools, write paths, command classes — from the definition file, so it must exist in the repository): %w", err)))
		return 1
	}
	protocolBytes, err := os.ReadFile(filepath.Join(root, "docs", "agent-protocol.md"))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", fmt.Errorf("read agent protocol: %w", err)))
		return 1
	}
	contractPath := stringValue(pointer["contract_ref"])
	contractSHA := stringValue(pointer["contract_sha256"])
	sessionPath := stringValue(pointer["path"])
	sessionSHA := stringValue(pointer["sha256"])
	workgroupID := "workgroup-s9-" + dispatchSlug(stringValue(pointer["session_id"])) + "-" + dispatchSlug(target.AssignmentID)
	manifestID := "team-manifest-s9-" + dispatchSlug(stringValue(pointer["session_id"])) + "-" + dispatchSlug(target.AssignmentID)
	if entityExists(snapshot.State, "teams", workgroupID) {
		fmt.Fprintf(stderr, "runtime repair dispatch: %s is already registered; inspect `runtime repair status` and Runtime team state\n", workgroupID)
		return 1
	}
	taskID := s9RepairTaskID(stringValue(pointer["session_id"]), target.AssignmentID)
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
	definitionPath := filepath.ToSlash(*definitionRef)
	manifestAssignmentID := "assignment-s9-" + dispatchSlug(strings.TrimPrefix(target.AssignmentID, "repair-assignment-"))
	manifest := map[string]any{
		"schema_version": "1.0.0", "manifest_id": manifestID, "version": "1.0.0", "runtime_id": stringValue(snapshot.State["runtime_id"]),
		"req_id": reqID, "baseline_generation": baselineGeneration, "review_round": reviewRoundValue,
		"platform_team_id": "platform-s9-repair", "workgroup_id": workgroupID, "workgroup_kind": "builder", "status": "planned",
		"documents": []any{
			map[string]any{"id": "repair-contract", "path": contractPath, "version": "approved", "sha256": contractSHA},
			map[string]any{"id": "repair-session", "path": sessionPath, "version": "planned", "sha256": sessionSHA},
			map[string]any{"id": "repair-plan", "path": planRef.Path, "version": "compiled", "sha256": planRef.SHA256},
			map[string]any{"id": "agent-protocol", "path": "docs/agent-protocol.md", "version": "current", "sha256": sha256HexForArtifact(protocolBytes)},
			map[string]any{"id": "builder-definition", "path": definitionPath, "version": "current", "sha256": sha256HexForArtifact(definitionBytes)},
		},
		"risk_tags":                   []any{},
		"responsibility_dispositions": []any{map[string]any{"responsibility_id": "BUILD-WORK-PACKAGE", "disposition": "assigned", "trigger": "one approved RepairAssignment owns this bounded root-cause unit", "assignment_ids": []string{manifestAssignmentID}, "na_rationale": nil, "evidence_ref": planRef.Path}},
		"assignments": []any{map[string]any{
			"assignment_id": manifestAssignmentID, "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": *roleFamily,
			"scope": append([]string(nil), target.Scope...), "agent_id": *agentID, "agent_definition_ref": definitionPath,
			"skill_refs": []string{"code-quality", "testing-strategy", "api-contracts", "state-machine-design", "integration-verification"},
			"read_paths": []string{contractPath, sessionPath, planRef.Path, "docs/agent-protocol.md"}, "write_paths": append([]string(nil), target.Scope...),
			"output_paths": []string{".claude/review/repair/", ".claude/evidence/"}, "depends_on": []string{}, "reuse_decision": "create",
			"grouping_rationale": "one Builder owns one immutable RepairAssignment; cross-assignment dependencies and locks are consumed by S9 Runtime", "status": "planned", "dispatch_mode": "plan_checkpoint",
		}},
		"separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1,
		"quantity_rationale": "One platform-dispatched Builder per immutable RepairAssignment; no quality-layer token cap is applied.",
		"validation":         map[string]any{"result": "pass", "missing_responsibilities": []any{}, "unresolved_conflicts": []any{}, "warnings": []any{}, "validated_at": time.Now().UTC().Format(time.RFC3339Nano)},
	}
	task := map[string]any{
		"task_id": taskID, "session_id": stringValue(pointer["session_id"]), "plan_id": plan.PlanID, "assignment_id": target.AssignmentID,
		"unit_ids": target.UnitIDs, "scope": target.Scope, "depends_on": target.DependsOn, "resource_locks": target.ResourceLocks,
		"state": "reviewed", "objective": "execute the approved RepairAssignment and restore its mapped assertions",
		"instruction": "Read the approved RepairContract and PlanReport contract; send one generic PLAN_REPORT, submit the S9 domain PlanReport, wait for execution begin, then implement only this Assignment scope.",
		"next_action": "send one PLAN_REPORT with plan_ref, submit runtime repair plan-report, and continue when execution begins",
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
		RepairAssignmentID: target.AssignmentID, RepairOwnerAgentID: *agentID, OccurredAt: at,
	})
	if err != nil {
		_ = os.Remove(manifestPath)
		_ = os.Remove(taskPath)
		fmt.Fprintln(stderr, formatFailure("runtime repair dispatch", err))
		return 1
	}
	fmt.Fprintf(stderr, "RepairAssignment %s dispatched to %s; manifest/task were generated internally (do not pass --manifest); next: (optional) send the generic PLAN_REPORT so the platform auto-chain records the checkpoint — the S9 authority is the domain `runtime repair plan-report` below and does not require it\n", target.AssignmentID, *agentID)
	return encodeJSON(stdout, map[string]any{"assignment_id": target.AssignmentID, "agent_id": *agentID, "workgroup_id": workgroupID, "task_id": taskID, "manifest_path": manifestRel, "task_path": taskRel, "revision": next.Revision, "next_action": "send one generic PLAN_REPORT with plan_ref, then submit the S9 domain runtime repair plan-report"})
}

func runRuntimeRepairSessionOpen(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair session open", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair session open")
	common := repairFlags(fs)
	sessionID := fs.String("session-id", "", "RepairSession id")
	createdBy := fs.String("created-by", "", "session creator")
	reqID := fs.String("req-id", "", "bound REQ id")
	if err := fs.Parse(args); err != nil {
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
	if err := fs.Parse(args); err != nil {
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
	fmt.Fprintf(stderr, "S9 RepairPlan %s compiled; next: dispatch each RepairAssignment, submit one domain PlanReport per Builder, then begin execution\n", plan.PlanID)
	return encodeJSON(stdout, map[string]any{"plan": plan, "artifact_ref": ref, "revision": snapshot.Revision})
}

func runRuntimeRepairPlanReportSubmit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair plan-report submit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair plan-report submit")
	common := repairFlags(fs)
	file := fs.String("file", "", "PlanReport request JSON path")
	if err := fs.Parse(args); err != nil {
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
	fmt.Fprintln(stderr, "PlanReport accepted; next: runtime repair execution begin")
	return encodeJSON(stdout, map[string]any{"report": report, "artifact_ref": ref, "revision": snapshot.Revision})
}

func runRuntimeRepairExecutionBegin(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair execution begin", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair execution begin")
	common := repairFlags(fs)
	if err := fs.Parse(args); err != nil {
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
	if err := fs.Parse(args); err != nil {
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
	if err := fs.Parse(args); err != nil {
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
	if err := fs.Parse(args); err != nil {
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
	if err := fs.Parse(args); err != nil {
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
	if err := fs.Parse(args); err != nil {
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
	if err := fs.Parse(args); err != nil {
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
	if err := fs.Parse(args); err != nil {
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
	if err := fs.Parse(args); err != nil {
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
	if err := fs.Parse(args); err != nil {
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
	if err := fs.Parse(args); err != nil {
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
		board, boardErr := repairAssignmentBoard(*root, pointer)
		if boardErr != nil {
			fmt.Fprintln(stderr, formatFailure("runtime repair status", boardErr))
			return 1
		}
		response["board"] = board
		response["next"] = board["next_action"]
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

// repairAssignmentBoard is a read-only projection of the complete S9 batch.
// The RepairPlan remains the static authority; report/result refs and the
// Runtime phase provide progress. Keeping this projection derived means
// recovery can show every Assignment, dependency, lock and missing artifact
// without adding a second mutable scheduler state.
func repairAssignmentBoard(root string, pointer map[string]any) (map[string]any, error) {
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
			row["status"] = "completed"
			if result.Result != "pass" {
				row["status"] = "blocked"
				blocked = append(blocked, assignment.AssignmentID)
			}
		} else {
			missingResults = append(missingResults, assignment.AssignmentID)
			status := "awaiting_plan_report"
			if _, ok := reports[assignment.AssignmentID]; ok {
				status = "reported"
				if phase == "repairing" || stringValue(pointer["status"]) == "repairing" {
					status = "executing"
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
				status = "queued"
				row["queue_reason"] = dependencyBlock
				queued = append(queued, assignment.AssignmentID)
			} else if lockBlock != "" {
				status = "queued"
				row["queue_reason"] = lockBlock
				queued = append(queued, assignment.AssignmentID)
			} else if status == "reported" || status == "executing" {
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
		if len(missingReports) > 0 {
			next = "submit one S9 PlanReport for each missing Assignment: " + strings.Join(missingReports, ", ")
		} else {
			next = "all S9 PlanReports are present; run `runtime repair execution begin` to release the first-write barrier"
		}
	case "repairing":
		if len(blocked) > 0 {
			next = "inspect blocked RepairResults " + strings.Join(blocked, ", ") + "; route the causal failure to S8"
		} else if len(ready) > 0 {
			next = "submit the exact-unit RepairResult for ready Assignment " + ready[0]
		} else if len(queued) > 0 {
			next = "wait for queued Assignment dependencies/locks; inspect each queue_reason before retrying"
		} else if len(missingResults) > 0 {
			next = "submit one exact-unit RepairResult for each remaining Assignment"
		}
	case "impact_reconciliation":
		next = "compute the session-wide Changeset, then commit ChangeImpact"
	case "targeted_reverification":
		next = "submit the independent targeted reverification"
	case "ready_for_full_review":
		next = "create and commit the complete RepairHandoff; S7 then registers a fresh full ReviewPlan"
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
