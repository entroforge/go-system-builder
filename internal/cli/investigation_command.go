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

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/bugprojection"
	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func runRuntimeInvestigation(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "ingest" && args[0] != "status" && args[0] != "contract" && args[0] != "project" && args[0] != "hypothesis" && args[0] != "route" && args[0] != "consume" && args[0] != "dispatch") {
		fmt.Fprintln(stderr, "runtime investigation requires <ingest|status|hypothesis|route|contract> or <dispatch|consume|project>")
		return 2
	}
	switch args[0] {
	case "ingest":
		return runRuntimeInvestigationIngest(args[1:], stdout, stderr)
	case "status":
		return runRuntimeInvestigationStatus(args[1:], stdout, stderr)
	case "contract":
		if len(args) < 2 || args[1] != "approve" {
			fmt.Fprintln(stderr, "runtime investigation contract requires <approve>")
			return 2
		}
		return runRuntimeInvestigationContractApprove(args[2:], stdout, stderr)
	case "project":
		return runRuntimeInvestigationProject(args[1:], stdout, stderr)
	case "hypothesis":
		if len(args) < 2 || (args[1] != "register" && args[1] != "result") {
			fmt.Fprintln(stderr, "runtime investigation hypothesis requires <register|result>")
			return 2
		}
		if args[1] == "register" {
			return runRuntimeInvestigationHypothesisRegister(args[2:], stdout, stderr)
		}
		return runRuntimeInvestigationHypothesisResult(args[2:], stdout, stderr)
	case "route":
		return runRuntimeInvestigationRoute(args[1:], stdout, stderr)
	case "consume":
		return runRuntimeInvestigationConsume(args[1:], stdout, stderr)
	case "dispatch":
		return runRuntimeInvestigationDispatch(args[1:], stdout, stderr)
	default:
		return 2
	}
}

// runRuntimeInvestigationDispatch turns a registered Hypothesis into a real
// Investigator workgroup. Before this bridge existed, Hypothesis.assignment_id
// was only a free-form trace field: the Case could claim that a question had
// an owner while Runtime had no Team, Task, Agent, or activation envelope for
// that owner. The command deliberately reuses register-workgroup so the
// normal CAS, manifest validation, task creation, and L4 activation path stay
// authoritative.
func runRuntimeInvestigationDispatch(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime investigation dispatch", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime investigation dispatch")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
	caseID := flags.String("case-id", "", "active InvestigationCase id")
	hypothesisID := flags.String("hypothesis-id", "", "registered hypothesis id")
	agentID := flags.String("agent-id", "", "Investigator Agent id")
	assignmentID := flags.String("assignment-id", "", "optional Assignment id; defaults to Hypothesis.assignment_id")
	definitionRef := flags.String("agent-definition", "agents/investigator.md", "Investigator Agent Definition path")
	occurredAtValue := flags.String("occurred-at", "", "RFC3339 transition time")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*caseID) == "" || strings.TrimSpace(*hypothesisID) == "" || strings.TrimSpace(*agentID) == "" {
		fmt.Fprintln(stderr, "runtime investigation dispatch requires --case-id, --hypothesis-id and --agent-id")
		return 2
	}
	rootPath, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation dispatch", err))
		return 1
	}
	stateFile := resolveRootPath(rootPath, *statePath)
	journalFile := resolveRootPath(rootPath, *journalPath)
	snapshot, err := runtime.NewStore(stateFile, journalFile).Snapshot()
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation dispatch", err))
		return 1
	}
	pointer := investigationPointer(snapshot.State)
	if pointer == nil || stringValue(pointer["case_id"]) != strings.TrimSpace(*caseID) {
		fmt.Fprintf(stderr, "runtime investigation dispatch: Case %s is not the active Case; run `runtime investigation status` and use its case_id\n", *caseID)
		return 1
	}
	caseRel := stringValue(pointer["path"])
	caseBytes, err := os.ReadFile(resolveRootPath(rootPath, caseRel))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation dispatch", fmt.Errorf("read pinned Case %s: %w", caseRel, err)))
		return 1
	}
	if actual := sha256HexForArtifact(caseBytes); actual != stringValue(pointer["sha256"]) {
		fmt.Fprintf(stderr, "runtime investigation dispatch: Case %s sha256 drifted (pinned %s, disk %s); restore the Case before dispatch\n", caseRel, stringValue(pointer["sha256"]), actual)
		return 1
	}
	if err := schema.NewValidator(rootPath).ValidateBytes("review-investigation-case.schema.json", caseBytes); err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation dispatch", fmt.Errorf("Case schema: %w", err)))
		return 1
	}
	var caseDocument map[string]any
	if err := json.Unmarshal(caseBytes, &caseDocument); err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation dispatch", err))
		return 1
	}
	var hypothesis map[string]any
	for _, candidate := range objectSliceAny(caseDocument["hypotheses"]) {
		if stringValue(candidate["hypothesis_id"]) == strings.TrimSpace(*hypothesisID) {
			hypothesis = candidate
			break
		}
	}
	if hypothesis == nil {
		fmt.Fprintf(stderr, "runtime investigation dispatch: hypothesis %s is not registered on Case %s; register it before dispatch\n", *hypothesisID, *caseID)
		return 1
	}
	registeredAssignmentID := strings.TrimSpace(stringValue(hypothesis["assignment_id"]))
	if registeredAssignmentID == "" {
		fmt.Fprintf(stderr, "runtime investigation dispatch: Hypothesis %s has no assignment_id; register the falsifiable question with --assignment-id first\n", *hypothesisID)
		return 1
	}
	resolvedAssignmentID := strings.TrimSpace(*assignmentID)
	if resolvedAssignmentID != "" && resolvedAssignmentID != registeredAssignmentID {
		fmt.Fprintf(stderr, "runtime investigation dispatch: --assignment-id %q does not match the Hypothesis assignment_id %q; dispatch the registered Assignment\n", resolvedAssignmentID, registeredAssignmentID)
		return 1
	}
	resolvedAssignmentID = registeredAssignmentID
	if !strings.HasPrefix(resolvedAssignmentID, "assignment-") {
		fmt.Fprintf(stderr, "runtime investigation dispatch: assignment_id %q must use the assignment- prefix so Runtime can bind it\n", resolvedAssignmentID)
		return 1
	}
	if strings.TrimSpace(*definitionRef) == "" {
		fmt.Fprintln(stderr, "runtime investigation dispatch: --agent-definition must not be empty")
		return 2
	}
	definitionBytes, err := os.ReadFile(resolveRootPath(rootPath, *definitionRef))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation dispatch", fmt.Errorf("read Agent Definition (dispatch derives the worker capability set — allowed tools, write paths, command classes — from the definition file, so it must exist in the repository): %w", err)))
		return 1
	}
	protocolBytes, err := os.ReadFile(filepath.Join(rootPath, "docs", "agent-protocol.md"))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation dispatch", fmt.Errorf("read agent protocol: %w", err)))
		return 1
	}
	boundReq, _ := snapshot.State["bound_req"].(map[string]any)
	reqID := stringValue(boundReq["id"])
	if reqID == "" {
		fmt.Fprintln(stderr, "runtime investigation dispatch: bound_req.id is missing; bind the active REQ before dispatch")
		return 1
	}
	workgroupID := "workgroup-s8-" + dispatchSlug(*caseID) + "-" + dispatchSlug(*hypothesisID)
	manifestID := "team-manifest-s8-" + dispatchSlug(*caseID) + "-" + dispatchSlug(*hypothesisID)
	if entityExists(snapshot.State, "teams", workgroupID) {
		fmt.Fprintf(stderr, "runtime investigation dispatch: %s is already registered; inspect `runtime investigation status` and Runtime team state\n", workgroupID)
		return 1
	}
	taskID := nextInvestigationTaskID(snapshot.State)
	taskRel := filepath.ToSlash(filepath.Join(".claude", "workgroups", reqID, taskID, taskID+".json"))
	manifestRel := filepath.ToSlash(filepath.Join(".claude", "workgroups", reqID, taskID, "manifest.json"))
	review := mapFieldCLI(snapshot.State, "review")
	reviewRound := intFieldCLI(review["round"])
	var reviewRoundValue any
	if reviewRound > 0 {
		reviewRoundValue = reviewRound
	}
	baseline := mapFieldCLI(snapshot.State, "baseline")
	baselineGeneration := intFieldCLI(baseline["generation"])
	if baselineGeneration < 1 {
		fmt.Fprintln(stderr, "runtime investigation dispatch: baseline.generation is not ready; bind a baseline before dispatch")
		return 1
	}
	documents := []any{
		map[string]any{"id": "investigation-case", "path": caseRel, "version": fmt.Sprintf("r%d", intFieldCLI(pointer["revision"])), "sha256": stringValue(pointer["sha256"])},
		map[string]any{"id": "agent-protocol", "path": "docs/agent-protocol.md", "version": "current", "sha256": sha256HexForArtifact(protocolBytes)},
		map[string]any{"id": "investigator-definition", "path": filepath.ToSlash(*definitionRef), "version": "current", "sha256": sha256HexForArtifact(definitionBytes)},
	}
	manifest := map[string]any{
		"schema_version": "1.0.0", "manifest_id": manifestID, "version": "1.0.0", "runtime_id": stringValue(snapshot.State["runtime_id"]),
		"req_id": reqID, "baseline_generation": baselineGeneration, "review_round": reviewRoundValue,
		"platform_team_id": "platform-s8-investigation", "workgroup_id": workgroupID, "workgroup_kind": "investigator", "status": "planned",
		"documents": documents, "risk_tags": []any{},
		"responsibility_dispositions": []any{map[string]any{
			"responsibility_id": "S8-CAUSAL-INVESTIGATION", "disposition": "assigned", "trigger": "one falsifiable hypothesis requires an independent causal discriminator",
			"assignment_ids": []string{resolvedAssignmentID}, "na_rationale": nil, "evidence_ref": caseRel,
		}},
		"assignments": []any{map[string]any{
			"assignment_id": resolvedAssignmentID, "responsibility_id": "S8-CAUSAL-INVESTIGATION", "role_family": "investigator",
			"scope": []string{"case:" + strings.TrimSpace(*caseID), "hypothesis:" + strings.TrimSpace(*hypothesisID)}, "agent_id": strings.TrimSpace(*agentID),
			"agent_definition_ref": filepath.ToSlash(*definitionRef), "skill_refs": []string{"bug-resolution", "code-quality", "testing-strategy"},
			"read_paths": []string{caseRel, "docs/agent-protocol.md"}, "write_paths": []string{}, "output_paths": []string{".claude/review/investigation/"},
			"depends_on": []string{}, "reuse_decision": "create", "grouping_rationale": "one Investigator owns one falsifiable discriminator and returns evidence to the Case workflow", "status": "planned",
			"dispatch_mode": "plan_checkpoint",
		}},
		"separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1,
		"quantity_rationale": "Each falsifiable hypothesis gets an independently traceable Investigator assignment; no fixed upper cap is imposed.",
		"validation":         map[string]any{"result": "pass", "missing_responsibilities": []any{}, "unresolved_conflicts": []any{}, "warnings": []any{}, "validated_at": time.Now().UTC().Format(time.RFC3339Nano)},
	}
	nextAction := fmt.Sprintf("send one generic PLAN_REPORT while still running, then submit `runtime investigation hypothesis result --case-id %s --hypothesis-id %s --assignment-id %s --method <...> --observed <...> --result <supported|refuted|inconclusive> --explains <finding> --source-boundary <ref> --evidence <ref> --counterfactual <...>`", *caseID, *hypothesisID, resolvedAssignmentID)
	task := map[string]any{
		"task_id": taskID, "case_id": strings.TrimSpace(*caseID), "hypothesis_id": strings.TrimSpace(*hypothesisID), "assignment_id": resolvedAssignmentID,
		"state": "reviewed", "objective": stringValue(hypothesis["discriminator"]),
		"instruction": "Answer the discriminator with read-only inspection or tests; do not modify product or locked specification files.",
		"next_action": nextAction,
	}
	manifestBytes, err := marshalInvestigationDispatch(manifest)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation dispatch", err))
		return 1
	}
	taskBytes, err := marshalInvestigationDispatch(task)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation dispatch", err))
		return 1
	}
	manifestPath := resolveRootPath(rootPath, manifestRel)
	taskPath := resolveRootPath(rootPath, taskRel)
	if err := writeInvestigationDispatchFile(manifestPath, manifestBytes); err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation dispatch", err))
		return 1
	}
	if err := writeInvestigationDispatchFile(taskPath, taskBytes); err != nil {
		_ = os.Remove(manifestPath)
		fmt.Fprintln(stderr, formatFailure("runtime investigation dispatch", err))
		return 1
	}
	resolvedRevision, err := resolveExpectedRevision(rootPath, stateFile, *expectedRevision)
	if err != nil {
		_ = os.Remove(manifestPath)
		_ = os.Remove(taskPath)
		fmt.Fprintln(stderr, formatFailure("runtime investigation dispatch", err))
		return 1
	}
	var occurredAt time.Time
	if strings.TrimSpace(*occurredAtValue) != "" {
		occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
		if err != nil {
			_ = os.Remove(manifestPath)
			_ = os.Remove(taskPath)
			fmt.Fprintf(stderr, "runtime investigation dispatch: invalid --occurred-at: %v\n", err)
			return 2
		}
	}
	next, err := assignment.Register(rootPath, stateFile, journalFile, assignment.Request{ExpectedRevision: resolvedRevision, ManifestPath: manifestPath, TaskID: taskID, TaskPath: taskPath, OccurredAt: occurredAt})
	if err != nil {
		_ = os.Remove(manifestPath)
		_ = os.Remove(taskPath)
		fmt.Fprintln(stderr, formatFailure("runtime investigation dispatch", err))
		return 1
	}
	fmt.Fprintf(stderr, "Investigator %s registered for Hypothesis %s; next: %s\n", *agentID, *hypothesisID, nextAction)
	return encodeJSON(stdout, map[string]any{"case_id": *caseID, "hypothesis_id": *hypothesisID, "assignment_id": resolvedAssignmentID, "agent_id": *agentID, "workgroup_id": workgroupID, "task_id": taskID, "manifest_path": manifestRel, "task_path": taskRel, "revision": next.Revision, "next_action": nextAction})
}

func marshalInvestigationDispatch(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func writeInvestigationDispatchFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dispatch directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.ToSlash(path), err)
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", filepath.ToSlash(path), err)
	}
	return nil
}

func dispatchSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func nextInvestigationTaskID(state map[string]any) string {
	entities, _ := state["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	for index := 1; ; index++ {
		candidate := fmt.Sprintf("TASK-008-%03d", index)
		found := false
		for _, raw := range tasks {
			row, _ := raw.(map[string]any)
			if stringValue(row["id"]) == candidate {
				found = true
				break
			}
		}
		if !found {
			return candidate
		}
	}
}

func entityExists(state map[string]any, collection, id string) bool {
	entities, _ := state["entities"].(map[string]any)
	values, _ := entities[collection].([]any)
	for _, raw := range values {
		row, _ := raw.(map[string]any)
		if stringValue(row["id"]) == id {
			return true
		}
	}
	return false
}

func mapFieldCLI(state map[string]any, key string) map[string]any {
	value, _ := state[key].(map[string]any)
	return value
}

// runRuntimeInvestigationHypothesisRegister wires the Case-workflow API the
// S8 round actually needs: without it an ingested Case can never record a
// falsifiable hypothesis, unexplained_finding_ids never empties, and contract
// approve rejects forever (verified live in the S7~S8 round-8 review).
func runRuntimeInvestigationHypothesisRegister(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime investigation hypothesis register", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime investigation hypothesis register")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
	caseID := flags.String("case-id", "", "active InvestigationCase id")
	hypothesisID := flags.String("id", "", "hypothesis id")
	expectedCaseRevision := flags.Int("expected-case-revision", -1, "expected Case revision (read it with `runtime investigation status`)")
	expectedCaseSHA := flags.String("expected-case-sha256", "", "expected Case sha256 (read it with `runtime investigation status`)")
	assignmentID := flags.String("assignment-id", "", "dispatched Assignment answering this hypothesis")
	statement := flags.String("statement", "", "falsifiable causal statement")
	invariant := flags.String("invariant", "", "invariant the hypothesis claims is violated")
	discriminator := flags.String("discriminator", "", "observation that distinguishes this hypothesis from competitors")
	support := flags.String("support", "", "expected outcome if the hypothesis holds")
	refute := flags.String("refute", "", "expected outcome if the hypothesis is false")
	var sourceFindings stringListFlag
	flags.Var(&sourceFindings, "source-finding", "source Finding id; repeatable or comma-separated")
	var evidenceRefs stringListFlag
	flags.Var(&evidenceRefs, "evidence", "evidence ref backing the hypothesis; repeatable or comma-separated")
	occurredAtValue := flags.String("occurred-at", "", "RFC3339 event time")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*caseID) == "" || strings.TrimSpace(*hypothesisID) == "" {
		fmt.Fprintln(stderr, "runtime investigation hypothesis register requires --case-id and --id")
		return 2
	}
	resolvedRevision, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation hypothesis register", err))
		return 1
	}
	var occurredAt time.Time
	if strings.TrimSpace(*occurredAtValue) != "" {
		occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
		if err != nil {
			fmt.Fprintf(stderr, "runtime investigation hypothesis register: invalid --occurred-at: %v\n", err)
			return 2
		}
	}
	snapshot, err := investigation.RegisterHypothesis(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), investigation.HypothesisRequest{
		ExpectedRevision:     resolvedRevision,
		ExpectedCaseRevision: *expectedCaseRevision,
		ExpectedCaseSHA256:   strings.TrimSpace(*expectedCaseSHA),
		CaseID:               strings.TrimSpace(*caseID),
		HypothesisID:         strings.TrimSpace(*hypothesisID),
		AssignmentID:         strings.TrimSpace(*assignmentID),
		Statement:            *statement,
		Invariant:            *invariant,
		Discriminator:        *discriminator,
		ExpectedOutcomes:     map[string]any{"support": *support, "refute": *refute},
		SourceFindingIDs:     splitRepeatableValues(sourceFindings),
		EvidenceRefs:         splitRepeatableValues(evidenceRefs),
		OccurredAt:           occurredAt,
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation hypothesis register", err))
		return 1
	}
	pointer := investigationPointer(snapshot.State)
	fmt.Fprintf(stderr, "hypothesis registered on Case %s; next: dispatch the discriminator question, then submit the result\n", pointer["case_id"])
	return encodeJSON(stdout, map[string]any{
		"case_id":     pointer["case_id"],
		"case_path":   pointer["path"],
		"case_sha256": pointer["sha256"],
		"revision":    snapshot.Revision,
	})
}

// runRuntimeInvestigationHypothesisResult records the read-only evidence of
// one discriminator question; it never routes by itself.
func runRuntimeInvestigationHypothesisResult(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime investigation hypothesis result", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime investigation hypothesis result")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
	caseID := flags.String("case-id", "", "active InvestigationCase id")
	hypothesisID := flags.String("hypothesis-id", "", "registered hypothesis id")
	expectedCaseRevision := flags.Int("expected-case-revision", -1, "expected Case revision (read it with `runtime investigation status`)")
	expectedCaseSHA := flags.String("expected-case-sha256", "", "expected Case sha256 (read it with `runtime investigation status`)")
	assignmentID := flags.String("assignment-id", "", "Assignment that produced this result")
	method := flags.String("method", "", "how the observation was made")
	observed := flags.String("observed", "", "what was observed at the discriminator")
	result := flags.String("result", "", "supported | refuted | inconclusive")
	counterfactual := flags.String("counterfactual", "", "what the competitor hypothesis predicted")
	var sourceBoundary stringListFlag
	flags.Var(&sourceBoundary, "source-boundary", "failure-boundary ref from the Finding encounter; repeatable or comma-separated")
	var explains stringListFlag
	flags.Var(&explains, "explains", "Finding id this result explains; repeatable or comma-separated")
	var doesNotExplain stringListFlag
	flags.Var(&doesNotExplain, "does-not-explain", "Finding id this result does not explain; repeatable or comma-separated")
	var evidenceRefs stringListFlag
	flags.Var(&evidenceRefs, "evidence", "evidence ref; repeatable or comma-separated")
	occurredAtValue := flags.String("occurred-at", "", "RFC3339 event time")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*caseID) == "" || strings.TrimSpace(*hypothesisID) == "" {
		fmt.Fprintln(stderr, "runtime investigation hypothesis result requires --case-id and --hypothesis-id")
		return 2
	}
	resolvedRevision, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation hypothesis result", err))
		return 1
	}
	var occurredAt time.Time
	if strings.TrimSpace(*occurredAtValue) != "" {
		occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
		if err != nil {
			fmt.Fprintf(stderr, "runtime investigation hypothesis result: invalid --occurred-at: %v\n", err)
			return 2
		}
	}
	snapshot, err := investigation.SubmitHypothesisResult(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), investigation.HypothesisResultRequest{
		ExpectedRevision:     resolvedRevision,
		ExpectedCaseRevision: *expectedCaseRevision,
		ExpectedCaseSHA256:   strings.TrimSpace(*expectedCaseSHA),
		CaseID:               strings.TrimSpace(*caseID),
		HypothesisID:         strings.TrimSpace(*hypothesisID),
		AssignmentID:         strings.TrimSpace(*assignmentID),
		Method:               *method,
		EvidenceRefs:         splitRepeatableValues(evidenceRefs),
		SourceBoundaryRefs:   splitRepeatableValues(sourceBoundary),
		Observed:             *observed,
		Counterfactual:       *counterfactual,
		Result:               strings.TrimSpace(*result),
		ExplainsFindingIDs:   splitRepeatableValues(explains),
		DoesNotExplain:       splitRepeatableValues(doesNotExplain),
		OccurredAt:           occurredAt,
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation hypothesis result", err))
		return 1
	}
	pointer := investigationPointer(snapshot.State)
	fmt.Fprintf(stderr, "hypothesis result recorded on Case %s; when every source Finding is explained, route the Case\n", pointer["case_id"])
	return encodeJSON(stdout, map[string]any{
		"case_id":     pointer["case_id"],
		"case_path":   pointer["path"],
		"case_sha256": pointer["sha256"],
		"revision":    snapshot.Revision,
	})
}

// runRuntimeInvestigationRoute records the one Case-level disposition. The
// route must be deterministic: s9_repair requires every source Finding
// explained by supported results plus causal closure.
func runRuntimeInvestigationRoute(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime investigation route", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime investigation route")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
	caseID := flags.String("case-id", "", "active InvestigationCase id")
	route := flags.String("route", "", "investigate_more | s9_repair | duplicate | s2_spec_rework | human_req_change | s7_no_change")
	expectedCaseRevision := flags.Int("expected-case-revision", -1, "expected Case revision (read it with `runtime investigation status`)")
	expectedCaseSHA := flags.String("expected-case-sha256", "", "expected Case sha256 (read it with `runtime investigation status`)")
	routeReason := flags.String("reason", "", "why this disposition")
	primaryRootCause := flags.String("primary-root-cause", "", "s9_repair: the one-sentence root cause")
	causalModelFile := flags.String("causal-model-file", "", "s9_repair: JSON file with the causal model")
	blastRadiusFile := flags.String("blast-radius-file", "", "s9_repair: JSON file with the blast radius")
	detectionGapFile := flags.String("detection-gap-file", "", "s9_repair: JSON file with the detection gap")
	canonicalCaseID := flags.String("canonical-case-id", "", "duplicate: the canonical Case id")
	reassessmentEvidence := flags.String("reassessment-evidence", "", "S9 targeted-failure artifact path(s), comma-separated; required when reopening an approved Case")
	noCompetingHypothesis := flags.String("no-competing-hypothesis", "", "explicit declaration that no competing hypothesis was credible; substitutes for a refuted result in causal closure (S8-4)")
	occurredAtValue := flags.String("occurred-at", "", "RFC3339 event time")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*caseID) == "" || strings.TrimSpace(*route) == "" {
		fmt.Fprintln(stderr, "runtime investigation route requires --case-id and --route")
		return 2
	}
	resolvedRevision, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation route", err))
		return 1
	}
	var occurredAt time.Time
	if strings.TrimSpace(*occurredAtValue) != "" {
		occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
		if err != nil {
			fmt.Fprintf(stderr, "runtime investigation route: invalid --occurred-at: %v\n", err)
			return 2
		}
	}
	loadObject := func(flagName, path string) (map[string]any, error) {
		if strings.TrimSpace(path) == "" {
			return nil, nil
		}
		data, err := os.ReadFile(resolveRootPath(*root, path))
		if err != nil {
			return nil, fmt.Errorf("read --%s %q: %w; create the JSON file or correct the path, then rerun `runtime investigation route`", flagName, path, err)
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("decode --%s %q: %w; provide one JSON object and rerun `runtime investigation route`", flagName, path, err)
		}
		if value == nil {
			return nil, fmt.Errorf("decode --%s %q: input must be a JSON object, not null; provide one JSON object and rerun `runtime investigation route`", flagName, path)
		}
		return value, nil
	}
	causalModel, err := loadObject("causal-model-file", *causalModelFile)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation route", err))
		return 1
	}
	blastRadius, err := loadObject("blast-radius-file", *blastRadiusFile)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation route", err))
		return 1
	}
	detectionGap, err := loadObject("detection-gap-file", *detectionGapFile)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation route", err))
		return 1
	}
	reassessmentRefs, err := loadCausalReassessmentEvidence(*root, *reassessmentEvidence)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation route", err))
		return 1
	}
	snapshot, err := investigation.UpdateCaseRoute(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), investigation.RouteRequest{
		ExpectedRevision:               resolvedRevision,
		ExpectedCaseRevision:           *expectedCaseRevision,
		ExpectedCaseSHA256:             strings.TrimSpace(*expectedCaseSHA),
		CaseID:                         strings.TrimSpace(*caseID),
		Route:                          strings.TrimSpace(*route),
		RouteReason:                    *routeReason,
		PrimaryRootCause:               *primaryRootCause,
		CausalModel:                    causalModel,
		BlastRadius:                    blastRadius,
		DetectionGap:                   detectionGap,
		CanonicalCaseID:                strings.TrimSpace(*canonicalCaseID),
		CausalReassessmentEvidenceRefs: reassessmentRefs,
		NoCompetingHypothesis:          strings.TrimSpace(*noCompetingHypothesis),
		OccurredAt:                     occurredAt,
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation route", err))
		return 1
	}
	pointer := investigationPointer(snapshot.State)
	// The state pointer does not carry the route; read the durable Case file.
	routed := ""
	if path, _ := pointer["path"].(string); path != "" {
		if data, err := os.ReadFile(resolveRootPath(*root, path)); err == nil {
			var document map[string]any
			if json.Unmarshal(data, &document) == nil {
				routed, _ = document["route"].(string)
			}
		}
	}
	next := "next: record remaining discriminator results or dispatch follow-up questions"
	if action := investigationRouteNextAction(routed, pointer); action != "" {
		next = "next: " + action
	}
	fmt.Fprintf(stderr, "case routed to %s; %s\n", routed, next)
	return encodeJSON(stdout, map[string]any{
		"case_id":     pointer["case_id"],
		"route":       routed,
		"case_path":   pointer["path"],
		"case_sha256": pointer["sha256"],
		"revision":    snapshot.Revision,
	})
}

func investigationRouteNextAction(route string, pointer map[string]any) string {
	if action := stringValue(pointer["next_action"]); action != "" {
		return action
	}
	switch route {
	case "s9_repair":
		caseID := stringValue(pointer["case_id"])
		return fmt.Sprintf("draft the RepairContract for Case %s, record a matching human_decision evidence item, then run `runtime investigation contract approve --case-id %s --file <draft> --approved-by <actor> --approval-hash <sha256> --approval-evidence-id <evidence-id>`", caseID, caseID)
	case "investigate_more":
		return "register a new falsifiable hypothesis or submit its result before routing again"
	case "duplicate":
		return fmt.Sprintf("inspect canonical Case %s at %s and continue that Case; do not investigate this duplicate", stringValue(pointer["canonical_case_id"]), stringValue(pointer["canonical_case_ref"]))
	case "s2_spec_rework":
		return "consume the route with `runtime investigation consume --case-id <case>` to return to planning.design"
	case "s7_no_change":
		return "consume the route with `runtime investigation consume --case-id <case>` to open a fresh S7 round"
	case "human_req_change":
		return "pause at the human boundary with `runtime pause --reason <reason> --approved-by <human>`, then run `req amend`"
	default:
		return ""
	}
}

func runRuntimeInvestigationConsume(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime investigation consume", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime investigation consume")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
	caseID := flags.String("case-id", "", "active InvestigationCase id")
	actor := flags.String("actor", "orchestrator", "route consumer identity")
	occurredAtValue := flags.String("occurred-at", "", "RFC3339 transition time")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*caseID) == "" {
		fmt.Fprintln(stderr, "runtime investigation consume requires --case-id")
		return 2
	}
	expected, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation consume", err))
		return 1
	}
	var occurredAt time.Time
	if strings.TrimSpace(*occurredAtValue) != "" {
		occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
		if err != nil {
			fmt.Fprintf(stderr, "runtime investigation consume: invalid --occurred-at: %v\n", err)
			return 2
		}
	}
	snapshot, err := investigation.ConsumeCaseRoute(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), investigation.ConsumeRouteRequest{
		ExpectedRevision: expected,
		CaseID:           strings.TrimSpace(*caseID),
		Actor:            strings.TrimSpace(*actor),
		OccurredAt:       occurredAt,
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation consume", err))
		return 1
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	fmt.Fprintf(stderr, "investigation route consumed for Case %s; next: continue at %s.%v\n", *caseID, lifecycle["state"], lifecycle["phase"])
	return encodeJSON(stdout, map[string]any{"case_id": *caseID, "revision": snapshot.Revision, "lifecycle": lifecycle})
}

// splitRepeatable accepts both "a,b,c" and repeated flag use accumulated by
// the stringListFlag convention.
func splitRepeatable(value string) []string {
	parts := strings.Split(value, ",")
	result := []string{}
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func splitRepeatableValues(values []string) []string {
	result := []string{}
	for _, value := range values {
		result = append(result, splitRepeatable(value)...)
	}
	return result
}

func loadCausalReassessmentEvidence(root, value string) ([]investigation.EvidenceReference, error) {
	paths := splitRepeatable(value)
	if len(paths) == 0 {
		return nil, nil
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	refs := make([]investigation.EvidenceReference, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, requested := range paths {
		absolute, err := filepath.Abs(resolveRootPath(rootAbs, requested))
		if err != nil {
			return nil, fmt.Errorf("resolve reassessment evidence %q: %w", requested, err)
		}
		relative, err := filepath.Rel(rootAbs, absolute)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("reassessment evidence %q must be inside the repository", requested)
		}
		relative = filepath.ToSlash(relative)
		if _, ok := seen[relative]; ok {
			return nil, fmt.Errorf("reassessment evidence contains duplicate path %q", relative)
		}
		seen[relative] = struct{}{}
		data, err := os.ReadFile(absolute)
		if err != nil {
			return nil, fmt.Errorf("read reassessment evidence %q: %w", relative, err)
		}
		refs = append(refs, investigation.EvidenceReference{Path: relative, SHA256: sha256HexForArtifact(data)})
	}
	return refs, nil
}

func runRuntimeInvestigationProject(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime investigation project", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime investigation project")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	bugID := flags.String("bug-id", "", "canonical BUG compatibility id")
	reviewedBy := flags.String("reviewed-by", "", "projection reviewer identity")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*bugID) == "" {
		fmt.Fprintln(stderr, "runtime investigation project requires --bug-id; projection is compatibility only and does not authorize S9")
		return 2
	}
	snapshot, err := runtime.NewStore(resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath)).Snapshot()
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation project", err))
		return 1
	}
	review, _ := snapshot.State["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	if pointer == nil || stringValue(pointer["status"]) != "contract_approved" {
		fmt.Fprintln(stderr, "runtime investigation project: active InvestigationCase must be contract_approved; approve the Contract first")
		return 1
	}
	entities, _ := snapshot.State["entities"].(map[string]any)
	rawFindings, _ := entities["findings"].([]any)
	wanted := map[string]bool{}
	for _, raw := range stringSliceAny(pointer["source_finding_ids"]) {
		wanted[raw] = true
	}
	findingRefs := make([]bugprojection.FindingRef, 0, len(wanted))
	for _, raw := range rawFindings {
		row, _ := raw.(map[string]any)
		id := stringValue(row["finding_id"])
		if !wanted[id] {
			continue
		}
		path := stringValue(row["path"])
		sha := stringValue(row["sha256"])
		if path == "" || sha == "" {
			fmt.Fprintf(stderr, "runtime investigation project: Finding %s lacks path/sha256; refresh S7 finding projection\n", id)
			return 1
		}
		findingRefs = append(findingRefs, bugprojection.FindingRef{ID: id, Path: path, SHA256: sha})
	}
	if len(findingRefs) != len(wanted) {
		fmt.Fprintln(stderr, "runtime investigation project: runtime.entities.findings does not contain the exact Case Finding set; do not fabricate a BUG projection")
		return 1
	}
	bound, _ := snapshot.State["bound_req"].(map[string]any)
	runtimeID := stringValue(snapshot.State["runtime_id"])
	reqID := stringValue(bound["id"])
	result, err := bugprojection.ProjectApprovedContract(*root, bugprojection.Request{Case: bugprojection.ArtifactRef{Path: stringValue(pointer["path"]), SHA256: stringValue(pointer["sha256"])}, Contract: bugprojection.ArtifactRef{Path: stringValue(pointer["repair_contract_ref"]), SHA256: stringValue(pointer["repair_contract_sha256"])}, BugID: *bugID, RuntimeID: runtimeID, ReqID: reqID, FindingRefs: findingRefs, ReviewedBy: *reviewedBy})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation project", err))
		return 1
	}
	fmt.Fprintf(stderr, "BUG compatibility projection %s written; authority remains InvestigationCase/RepairContract; next: runtime repair session open\n", result.BugID)
	return encodeJSON(stdout, result)
}

func stringSliceAny(value any) []string {
	raw, _ := value.([]any)
	result := []string{}
	for _, item := range raw {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	if values, ok := value.([]string); ok {
		return values
	}
	return result
}

func runRuntimeInvestigationIngest(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime investigation ingest", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime investigation ingest")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
	caseID := flags.String("case-id", "", "optional InvestigationCase id")
	groupingRationale := flags.String("grouping-rationale", "", "why this sealed Finding set is a provisional grouping")
	// RC-12 (S8 intake scaffold): --emit-template writes a dry-run scaffold —
	// Case shape, RouteRequest draft, RepairContract placeholder — from the
	// batch that Ingest is about to consume. It never writes Runtime state;
	// the real Case is still created by the CAS in investigation.Ingest.
	emitTemplate := flags.String("emit-template", "", "write a case-template.json scaffold (Case + RouteRequest draft + RepairContract placeholder) to this path, or `-` for stdout; dry-run only")
	occurredAtValue := flags.String("occurred-at", "", "RFC3339 transition time")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*groupingRationale) == "" {
		fmt.Fprintln(stderr, "runtime investigation ingest requires --grouping-rationale; intake must record why the exact Finding set is provisionally grouped")
		return 2
	}
	resolvedRevision, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation ingest", err))
		return 1
	}
	var occurredAt time.Time
	if strings.TrimSpace(*occurredAtValue) != "" {
		occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
		if err != nil {
			fmt.Fprintf(stderr, "runtime investigation ingest: invalid --occurred-at: %v\n", err)
			return 2
		}
	}
	snapshot, err := investigation.Ingest(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), investigation.IngestRequest{
		ExpectedRevision:  resolvedRevision,
		CaseID:            strings.TrimSpace(*caseID),
		GroupingRationale: *groupingRationale,
		OccurredAt:        occurredAt,
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation ingest", err))
		return 1
	}
	pointer := investigationPointer(snapshot.State)
	// RC-12 (S8 intake scaffold): after the Case exists, render the scaffold
	// from the same facts. A template write failure must not undo the ingest
	// (the CAS already committed) and must not masquerade as an ingest
	// failure either (RC-18): the ingest result JSON still streams and the
	// ErrAlreadyIngested idempotent-retry path is self-explaining, so the
	// failure degrades to a warning on stderr with exit 0 — the caller
	// regenerates the scaffold after `runtime investigation status`.
	if strings.TrimSpace(*emitTemplate) != "" {
		if err := writeInvestigationCaseTemplate(*root, pointer, strings.TrimSpace(*emitTemplate), stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "investigation ingest: warning: --emit-template failed (%v); the Case ingest itself committed — re-run the scaffold after `runtime investigation status`\n", err)
		}
	}
	fmt.Fprintf(stderr, "investigation ingest: Case %s is investigating; consume S7 batch %s; next: dispatch hypothesis questions, do not create a BUG\n", pointer["case_id"], pointer["observation_batch_id"])
	return encodeJSON(stdout, map[string]any{
		"case_id":              pointer["case_id"],
		"path":                 pointer["path"],
		"sha256":               pointer["sha256"],
		"status":               pointer["status"],
		"source_finding_ids":   pointer["source_finding_ids"],
		"observation_batch_id": pointer["observation_batch_id"],
		"revision":             snapshot.Revision,
	})
}

// writeInvestigationCaseTemplate renders the S8 intake scaffold (RC-12 Step
// C) to --emit-template's path, or stdout when the path is "-". The scaffold
// is a dry-run artifact only: it mirrors the just-ingested Case facts and
// pre-fills the RouteRequest / RepairContract placeholders the S8 round must
// fill in, but it never writes Runtime state and must not be submitted
// directly (route/contract verbs validate against the live Case revision).
func writeInvestigationCaseTemplate(root string, pointer map[string]any, target string, stdout, stderr io.Writer) error {
	caseRel := stringValue(pointer["path"])
	caseBytes, err := os.ReadFile(resolveRootPath(root, caseRel))
	if err != nil {
		return fmt.Errorf("read ingested Case %s: %w", caseRel, err)
	}
	var caseDocument map[string]any
	if err := json.Unmarshal(caseBytes, &caseDocument); err != nil {
		return fmt.Errorf("decode ingested Case %s: %w", caseRel, err)
	}
	caseID := stringValue(pointer["case_id"])
	sourceFindings := stringSliceAny(pointer["source_finding_ids"])
	if len(sourceFindings) == 0 {
		sourceFindings = stringSliceAny(caseDocument["source_finding_ids"])
	}
	revision := intFieldCLI(pointer["revision"])
	if revision == 0 {
		revision = intFieldCLI(caseDocument["revision"])
	}
	if revision < 1 {
		return fmt.Errorf("Case %s revision is not readable; regenerate the scaffold after `runtime investigation status` confirms the Case", caseID)
	}

	// RouteRequest draft: the fields Route() validates are pre-named; the
	// agent replaces every TODO placeholder with real causal facts.
	routeDraft := map[string]any{
		"template":                "route-request-draft",
		"case_id":                 caseID,
		"expected_case_sha256":    stringValue(pointer["sha256"]),
		"route":                   "TODO(s9_repair|investigate_more|duplicate|s2_spec_rework|human_req_change|s7_no_change)",
		"route_reason":            "TODO(why this disposition)",
		"primary_root_cause":      "TODO(one-sentence root cause; required for s9_repair)",
		"causal_model_file":       "TODO(JSON file path with the causal model; see docs/examples/s7-s9/causal-model.json)",
		"blast_radius_file":       "TODO(JSON file path; see docs/examples/s7-s9/blast-radius.json)",
		"detection_gap_file":      "TODO(JSON file path; see docs/examples/s7-s9/detection-gap.json)",
		"unexplained_finding_ids": stringSliceAny(caseDocument["unexplained_finding_ids"]),
		"next_verb":               fmt.Sprintf("runtime investigation route --case-id %s --route <route> --reason <...> --expected-case-revision %d --expected-case-sha256 <sha> [--causal-model-file <...> --blast-radius-file <...> --detection-gap-file <...>]", caseID, revision),
	}

	// RepairContract placeholder: mirrors repair-contract.schema.json's
	// required fields (status stays draft; approval happens only through
	// `runtime investigation contract approve`).
	contractPlaceholder := map[string]any{
		"template":             "repair-contract-placeholder",
		"schema_version":       "1.0.0",
		"repair_contract_id":   fmt.Sprintf("repair-contract-%s", dispatchSlug(caseID)),
		"case_id":              caseID,
		"revision":             revision,
		"status":               "draft",
		"source_finding_ids":   sourceFindings,
		"root_cause_statement": "TODO(fill after the route settles on s9_repair)",
		"violated_invariant":   "TODO(the invariant the defect violated)",
		"causal_model_ref":     fmt.Sprintf("case://%s/causal-model", caseID),
		"architecture_intent":  "TODO(what the repaired architecture restores)",
		"repair_units": []any{map[string]any{
			"id":            "repair-unit-1",
			"description":   "TODO(one bounded repair unit; every unit needs scope + assertion_ids before approval)",
			"scope":         []string{},
			"assertion_ids": []string{},
		}},
		"prospective_scope":          []string{},
		"forbidden_scope":            []string{},
		"symptom_assertions":         []string{"TODO(symptom-1: ...)"},
		"root_invariant_assertions":  []string{"TODO(root-1: ...)"},
		"detection_gap_assertions":   []string{"TODO(gap-1: ...)"},
		"stop_escalation_conditions": []string{"TODO(when the repair must stop and escalate)"},
		"next_verb":                  fmt.Sprintf("runtime investigation contract approve --case-id %s --file <draft> --approved-by <actor> --approval-hash <sha256> --approval-evidence-id <evidence-id> --expected-case-revision %d --expected-case-sha256 <sha>", caseID, revision),
	}

	document := map[string]any{
		"template":                    "case-template",
		"case_id":                     caseID,
		"case_path":                   caseRel,
		"case_sha256":                 stringValue(pointer["sha256"]),
		"case_revision":               revision,
		"status":                      stringValue(pointer["status"]),
		"observation_batch_id":        stringValue(pointer["observation_batch_id"]),
		"source_finding_ids":          sourceFindings,
		"grouping_rationale":          caseDocument["grouping_rationale"],
		"unexplained_finding_ids":     stringSliceAny(caseDocument["unexplained_finding_ids"]),
		"route_request_draft":         routeDraft,
		"repair_contract_placeholder": contractPlaceholder,
		"disclosure":                  "dry-run scaffold only — do not submit this file; fill the TODO markers and run the named next verbs against the live Case",
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode case template: %w", err)
	}
	data = append(data, '\n')
	if target == "-" {
		fmt.Fprint(stderr, "investigation ingest --emit-template: scaffold below (stdout; dry-run, not written to disk)\n")
		_, err = stdout.Write(data)
		return err
	}
	absolute := resolveRootPath(root, target)
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return fmt.Errorf("create template directory: %w", err)
	}
	if err := os.WriteFile(absolute, data, 0o644); err != nil {
		return fmt.Errorf("write case template %s: %w", target, err)
	}
	return nil
}

func runRuntimeInvestigationContractApprove(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime investigation contract approve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime investigation contract approve")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
	caseID := flags.String("case-id", "", "active InvestigationCase id")
	contractPath := flags.String("file", "", "draft RepairContract path")
	approvedBy := flags.String("approved-by", "", "approving human or orchestrator identity")
	approvalHash := flags.String("approval-hash", "", "sha256 of the exact draft reviewed by the approver")
	approvalEvidenceID := flags.String("approval-evidence-id", "", "valid human_decision evidence id scoped to this S8 approval")
	occurredAtValue := flags.String("occurred-at", "", "RFC3339 transition time")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*caseID) == "" || strings.TrimSpace(*contractPath) == "" || strings.TrimSpace(*approvedBy) == "" || strings.TrimSpace(*approvalHash) == "" || strings.TrimSpace(*approvalEvidenceID) == "" {
		fmt.Fprintln(stderr, "runtime investigation contract approve requires --case-id, --file, --approved-by, --approval-hash and --approval-evidence-id; approval is the S8→S9 authority transaction")
		return 2
	}
	resolvedRevision, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation contract approve", err))
		return 1
	}
	var occurredAt time.Time
	if strings.TrimSpace(*occurredAtValue) != "" {
		occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
		if err != nil {
			fmt.Fprintf(stderr, "runtime investigation contract approve: invalid --occurred-at: %v\n", err)
			return 2
		}
	}
	snapshot, err := investigation.ApproveContract(resolveRootPath(*root, "."), resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), investigation.ContractRequest{
		ExpectedRevision:   resolvedRevision,
		CaseID:             strings.TrimSpace(*caseID),
		ContractPath:       *contractPath,
		ApprovedBy:         *approvedBy,
		ApprovalHash:       strings.TrimSpace(*approvalHash),
		ApprovalEvidenceID: strings.TrimSpace(*approvalEvidenceID),
		OccurredAt:         occurredAt,
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation contract approve", err))
		return 1
	}
	pointer := investigationPointer(snapshot.State)
	fmt.Fprintf(stderr, "repair contract approve: %s is approved for Case %s; next: S9 consume the approved Contract; do not create a BUG as authority\n", pointer["repair_contract_ref"], pointer["case_id"])
	return encodeJSON(stdout, map[string]any{
		"case_id":                pointer["case_id"],
		"case_path":              pointer["path"],
		"case_sha256":            pointer["sha256"],
		"case_revision":          pointer["revision"],
		"status":                 pointer["status"],
		"repair_contract_ref":    pointer["repair_contract_ref"],
		"repair_contract_sha256": pointer["repair_contract_sha256"],
		"revision":               snapshot.Revision,
	})
}

func runRuntimeInvestigationStatus(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime investigation status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime investigation status")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	caseID := flags.String("case-id", "", "optional InvestigationCase id")
	allCases := flags.Bool("all", false, "show the read-only aggregate of all InvestigationCases")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	snapshot, err := runtime.NewStore(resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath)).Snapshot()
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation status", err))
		return 1
	}
	if *allCases {
		aggregate, aggregateErr := investigationCaseAggregate(*root, snapshot.Revision)
		if aggregateErr != nil {
			fmt.Fprintln(stderr, formatFailure("runtime investigation status --all", aggregateErr))
			return 1
		}
		return encodeJSON(stdout, aggregate)
	}
	pointer := investigationPointer(snapshot.State)
	if pointer == nil {
		return encodeJSON(stdout, map[string]any{
			"status":   "not_ingested",
			"revision": snapshot.Revision,
			"next":     "runtime investigation ingest --root <root> --grouping-rationale <reason>",
		})
	}
	if requested := strings.TrimSpace(*caseID); requested != "" && requested != stringValue(pointer["case_id"]) {
		fmt.Fprintf(stderr, "runtime investigation status: Case %s is not the active Case; next: inspect Case %s\n", requested, stringValue(pointer["case_id"]))
		return 1
	}
	board, err := investigationStatusBoard(*root, pointer, snapshot.State)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime investigation status", err))
		return 1
	}
	next := stringValue(board["next_action"])
	var repairRecovery map[string]any
	review := mapFieldCLI(snapshot.State, "review")
	if repair := mapFieldCLI(review, "repair"); stringValue(repair["status"]) == "blocked" && stringValue(repair["next_action"]) != "" {
		next = stringValue(repair["next_action"])
		repairRecovery = map[string]any{
			"status":           repair["status"],
			"failure_route":    repair["failure_route"],
			"next_action":      repair["next_action"],
			"artifact_refs":    repair["targeted_reverification_refs"],
			"artifact_records": repair["targeted_reverification_artifacts"],
		}
	}
	switch stringValue(pointer["status"]) {
	case "contract_approved":
		if repairRecovery == nil {
			next = fmt.Sprintf("open S9 with `runtime repair session open --root %s --session-id <session> --created-by <agent>`; it consumes approved RepairContract (repair_contract_ref=%s)", *root, stringValue(pointer["repair_contract_ref"]))
		}
	case "blocked":
		next = "resolve the Case blocker, then re-read the Case board before continuing"
	}
	// RC-06 (S8-8): the former `case "contract_review":` branch here was a
	// ghost phase — no code path ever sets an InvestigationCase to
	// contract_review (contract.go transitions investigating →
	// contract_approved directly), so the branch was unreachable. The
	// contract_review value remains legal in the loop-state schema enums
	// (reserved for the future human review boundary) but has no CLI
	// behavior.
	return encodeJSON(stdout, map[string]any{
		"case":            pointer,
		"board":           board,
		"revision":        snapshot.Revision,
		"next":            next,
		"repair_recovery": repairRecovery,
	})
}

func investigationCaseAggregate(root string, runtimeRevision int) (map[string]any, error) {
	caseDir := resolveRootPath(root, ".claude/review/investigation/cases")
	entries, err := os.ReadDir(caseDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read InvestigationCase directory: %w", err)
	}
	latest := map[string]map[string]any{}
	latestRevision := map[string]int{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		relPath := filepath.ToSlash(filepath.Join(".claude/review/investigation/cases", entry.Name()))
		data, readErr := os.ReadFile(filepath.Join(caseDir, entry.Name()))
		if readErr != nil {
			return nil, fmt.Errorf("read InvestigationCase %s: %w", relPath, readErr)
		}
		var document map[string]any
		if decodeErr := json.Unmarshal(data, &document); decodeErr != nil {
			return nil, fmt.Errorf("decode InvestigationCase %s: %w", relPath, decodeErr)
		}
		id := stringValue(document["case_id"])
		if id == "" {
			continue
		}
		revision := intFieldCLI(document["revision"])
		if prior, ok := latestRevision[id]; ok && revision < prior {
			continue
		}
		latestRevision[id] = revision
		board, boardErr := investigationStatusBoard(root, map[string]any{
			"case_id":  id,
			"path":     relPath,
			"sha256":   sha256HexForArtifact(data),
			"revision": revision,
		}, nil)
		if boardErr != nil {
			return nil, boardErr
		}
		board["path"] = relPath
		board["revision"] = revision
		latest[id] = board
	}
	ids := make([]string, 0, len(latest))
	for id := range latest {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	cases := make([]any, 0, len(ids))
	routeCounts := map[string]int{}
	statusCounts := map[string]int{}
	for _, id := range ids {
		board := latest[id]
		cases = append(cases, board)
		routeCounts[stringValue(board["route"])]++
		statusCounts[stringValue(board["status"])]++
	}
	return map[string]any{
		"status":   "aggregate",
		"revision": runtimeRevision,
		"cases":    cases,
		"summary":  map[string]any{"case_count": len(cases), "route_counts": routeCounts, "status_counts": statusCounts},
		"next":     "choose a Case from cases, then run runtime investigation status --case-id <case> and follow its board.next_action",
	}, nil
}

func intFieldCLI(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	default:
		return 0
	}
}

// investigationStatusBoard is a read-only projection for the agent. The Case
// artifact remains the authority; this projection only answers the questions
// an investigator otherwise has to answer by opening several immutable
// revisions: what is unexplained, which hypotheses still lack a result, and
// what exact verb is safe next.
func investigationStatusBoard(root string, pointer map[string]any, state map[string]any) (map[string]any, error) {
	path := stringValue(pointer["path"])
	if path == "" {
		return nil, fmt.Errorf("active InvestigationCase pointer has no artifact path")
	}
	data, err := os.ReadFile(resolveRootPath(root, path))
	if err != nil {
		return nil, fmt.Errorf("read active InvestigationCase %s: %w", path, err)
	}
	expectedSHA := stringValue(pointer["sha256"])
	if expectedSHA == "" {
		return nil, fmt.Errorf("active InvestigationCase pointer has no sha256 for %s; reconcile the Runtime pointer before continuing", path)
	}
	actualSHA := sha256HexForArtifact(data)
	if actualSHA != expectedSHA {
		return nil, fmt.Errorf("active InvestigationCase %s sha256 drifted: Runtime pins %s but disk is %s; restore the pinned artifact or run runtime reconcile", path, expectedSHA, actualSHA)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode active InvestigationCase %s: %w", path, err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", data); err != nil {
		return nil, fmt.Errorf("active InvestigationCase %s schema is invalid: %v; restore the pinned artifact or run runtime reconcile", path, err)
	}
	if expectedID := stringValue(pointer["case_id"]); expectedID != "" && stringValue(document["case_id"]) != expectedID {
		return nil, fmt.Errorf("active InvestigationCase identity drifted: Runtime points to %q but artifact declares %q; reconcile before continuing", expectedID, stringValue(document["case_id"]))
	}
	if expectedRevision := intFieldCLI(pointer["revision"]); expectedRevision > 0 && intFieldCLI(document["revision"]) != expectedRevision {
		return nil, fmt.Errorf("active InvestigationCase revision drifted: Runtime points to %d but artifact declares %d; reconcile before continuing", expectedRevision, intFieldCLI(document["revision"]))
	}
	sourceFindingIDs := stringSliceAny(document["source_finding_ids"])
	unexplained := stringSliceAny(document["unexplained_finding_ids"])
	hypotheses := objectSliceAny(document["hypotheses"])
	results := objectSliceAny(document["hypothesis_results"])
	resultByHypothesis := map[string]map[string]any{}
	for _, result := range results {
		if id := stringValue(result["hypothesis_id"]); id != "" {
			resultByHypothesis[id] = result
		}
	}
	pendingHypotheses := []string{}
	awaitingResultHypotheses := []string{}
	completedHypotheses := []string{}
	unknownDispatchHypotheses := []string{}
	for _, hypothesis := range hypotheses {
		id := stringValue(hypothesis["hypothesis_id"])
		if id == "" {
			continue
		}
		if _, ok := resultByHypothesis[id]; ok {
			completedHypotheses = append(completedHypotheses, id)
		} else if state == nil {
			// S8-10: the --all aggregate reads Case artifacts without the
			// Runtime state, so dispatch cannot be verified. Mark the
			// dispatch state unknown instead of guessing "pending".
			unknownDispatchHypotheses = append(unknownDispatchHypotheses, id)
		} else if investigationAssignmentDispatched(root, state, stringValue(hypothesis["assignment_id"])) {
			awaitingResultHypotheses = append(awaitingResultHypotheses, id)
		} else {
			pendingHypotheses = append(pendingHypotheses, id)
		}
	}
	nextAction := "register one falsifiable hypothesis with `runtime investigation hypothesis register --case-id <case> --id <hypothesis> --assignment-id <assignment> --statement <...> --invariant <...> --discriminator <...> --support <...> --refute <...> --source-finding <finding>`"
	if len(pendingHypotheses) > 0 {
		nextAction = "dispatch it with `runtime investigation dispatch --case-id <case> --hypothesis-id <hypothesis> --agent-id <agent>`, then submit runtime investigation hypothesis result"
	} else if len(awaitingResultHypotheses) > 0 {
		nextAction = "submit it with `runtime investigation hypothesis result --case-id <case> --hypothesis-id <hypothesis> --assignment-id <assignment> --method <...> --observed <...> --result <supported|refuted|inconclusive> --explains <finding> --source-boundary <ref> --evidence <ref> --counterfactual <...>`"
	} else if len(unexplained) == 0 && len(hypotheses) > 0 {
		nextAction = "record the Case disposition with `runtime investigation route --case-id <case> --route <s9_repair|investigate_more|duplicate|s2_spec_rework|human_req_change|s7_no_change> --reason <...>`; for s9_repair also provide the root-cause and three evidence files"
	}
	if route := stringValue(document["route"]); route != "" {
		nextAction = investigationRouteNextAction(route, pointer)
	}
	// RC-18 S8-H3: surface the baseline-drift warning on the status board so
	// the investigator sees the stale-baseline fact at read time, not only in
	// the journal message recorded by the last Case revision.
	var baselineDrift string
	if reviewMap := mapFieldCLI(state, "review"); reviewMap != nil {
		baselineDrift = stringValue(reviewMap["investigation_baseline_drift"])
	}
	return map[string]any{
		"case_id":                 stringValue(pointer["case_id"]),
		"status":                  stringValue(document["status"]),
		"route":                   stringValue(document["route"]),
		"source_finding_ids":      sourceFindingIDs,
		"unexplained_finding_ids": unexplained,
		"baseline_digest":         stringValue(document["baseline_digest"]),
		"baseline_drift_warning":  baselineDrift,
		"hypothesis_summary": map[string]any{
			"total":            len(hypotheses),
			"pending":          pendingHypotheses,
			"awaiting_result":  awaitingResultHypotheses,
			"completed":        completedHypotheses,
			"dispatch_unknown": unknownDispatchHypotheses,
		},
		"ready_for_contract": len(unexplained) == 0 && stringValue(document["route"]) == "s9_repair",
		"next_action":        nextAction,
	}, nil
}

func sha256HexForArtifact(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func objectSliceAny(value any) []map[string]any {
	raw, _ := value.([]any)
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if object, ok := item.(map[string]any); ok {
			result = append(result, object)
		}
	}
	return result
}

// investigationAssignmentDispatched derives dispatch state from the existing
// Runtime task projection. It deliberately does not add another Case state:
// the generated task is the registration boundary, while the Case remains
// the authority for hypothesis/result facts.
func investigationAssignmentDispatched(root string, state map[string]any, assignmentID string) bool {
	assignmentID = strings.TrimSpace(assignmentID)
	if assignmentID == "" {
		return false
	}
	entities, _ := state["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	for _, raw := range tasks {
		task, _ := raw.(map[string]any)
		path := stringValue(task["path"])
		if path == "" {
			continue
		}
		data, err := os.ReadFile(resolveRootPath(root, path))
		if err != nil {
			continue
		}
		var document map[string]any
		if json.Unmarshal(data, &document) == nil && stringValue(document["assignment_id"]) == assignmentID {
			return true
		}
	}
	return false
}

func investigationPointer(state map[string]any) map[string]any {
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	return pointer
}
