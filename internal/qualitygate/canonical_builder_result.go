package qualitygate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/mainreceipt"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const canonicalReceiptReadLimit int64 = 20 << 20

type canonicalAssignmentMode struct {
	Record     assignmentrecord.Record
	HasRecord  bool
	Manifest   string
	Assignment string
}

type canonicalMainCheckpoint struct {
	AssignmentRecord       *assignmentrecord.RecordReference `json:"assignment_record,omitempty"`
	AssignmentRecordSHA256 string                            `json:"assignment_record_sha256,omitempty"`
	TaskID                 string                            `json:"task_id"`
	AssignmentID           string                            `json:"assignment_id"`
	Generation             int                               `json:"baseline_generation"`
	State                  string                            `json:"state"`
	VerifiedAt             string                            `json:"verified_at"`
	ReportPath             string                            `json:"completion_report_path"`
	ReportSHA              string                            `json:"completion_report_sha256"`
	SourceHead             string                            `json:"source_head"`
	TargetHead             string                            `json:"target_head"`
	MergeBase              string                            `json:"merge_base"`
	TargetBranch           string                            `json:"target_branch"`
	MergeCommit            string                            `json:"merge_commit"`
	ChangedPaths           []string                          `json:"changed_paths"`
	ChangedPathsSHA256     string                            `json:"changed_paths_sha256"`
	TestedHead             string                            `json:"tested_head"`
	PreviousMainRoots      []string                          `json:"previous_main_roots,omitempty"`
	CheckReceipts          []string                          `json:"check_receipts,omitempty"`
	ToolCheckReceipts      []checkexec.ToolReceipt           `json:"tool_check_receipts,omitempty"`
	CanonicalMainFacts     *assignmentresult.MainFacts       `json:"canonical_main_facts"`
	Revision               int64                             `json:"revision"`
}

type canonicalBuilderResult struct {
	Record        assignmentrecord.Record
	RecordSHA     string
	Result        assignmentresult.AcceptedResult
	ResultRef     string
	ResultSHA     string
	Checkpoint    string
	CheckpointSHA string
	Facts         *assignmentresult.MainFacts
	MainReason    string
	Integrated    bool
	EvidenceID    string
	Err           error
}

// canonicalModeForOwner selects the S6 v1.2 path before legacy completion
// qualification. A current Record always opts into the strict path; without
// one, only the exact original Team manifest marker can select it.
func canonicalModeForOwner(input Input, owner string) (canonicalAssignmentMode, bool, error) {
	var mode canonicalAssignmentMode
	if owner == "" {
		return mode, false, nil
	}
	record, found, err := assignmentrecord.ForOwner(input.Snapshot.State, owner)
	if err != nil {
		return mode, true, fmt.Errorf("resolve current canonical Assignment: %w", err)
	}
	if found {
		mode.Record, mode.HasRecord, mode.Manifest, mode.Assignment = record, true, record.Manifest.Ref, record.AssignmentID
		return mode, true, nil
	}
	agent, ok, err := uniqueEntityByID(input.Snapshot.State, "agents", owner)
	if err != nil {
		return mode, true, err
	}
	if !ok {
		return mode, false, nil
	}
	teamID := stringValue(agent["team_id"])
	if teamID == "" {
		return mode, false, nil
	}
	teams, _ := input.Snapshot.State["entities"].(map[string]any)
	var team map[string]any
	for _, row := range anyRows(teams["teams"]) {
		if row["id"] != teamID {
			continue
		}
		if team != nil {
			return mode, true, fmt.Errorf("Team %s has duplicate Runtime rows", teamID)
		}
		team = row
	}
	if team == nil {
		return mode, false, nil
	}
	mode.Manifest = stringValue(team["manifest_ref"])
	_, mode.Assignment, _ = strings.Cut(stringValue(agent["prompt_ref"]), "#")
	if mode.Manifest == "" || input.Files == nil {
		return mode, true, fmt.Errorf("cannot resolve original Team manifest to determine the explicit S6 marker")
	}
	manifest, err := input.Files.ReadFile(mode.Manifest)
	if err != nil {
		return mode, true, fmt.Errorf("read original Team manifest %s: %w", mode.Manifest, err)
	}
	enabled, markerErr := assignmentrecord.ManifestAssignmentRecordEnabled(manifest)
	if markerErr != nil {
		return mode, true, fmt.Errorf("read explicit Assignment Record marker: %w", markerErr)
	}
	if !enabled {
		return mode, false, nil
	}
	return mode, true, fmt.Errorf("Team manifest opts into %s but the current Runtime has no canonical Assignment Record", assignmentrecord.AssignmentRecordContract)
}

func uniqueEntityByID(state map[string]any, collection, id string) (map[string]any, bool, error) {
	entities, _ := state["entities"].(map[string]any)
	var found map[string]any
	for _, row := range anyRows(entities[collection]) {
		if row["id"] != id {
			continue
		}
		if found != nil {
			return nil, false, fmt.Errorf("ambiguous %s %s", collection, id)
		}
		found = row
	}
	return found, found != nil, nil
}

func canonicalModeForTask(input Input, task map[string]any) (canonicalAssignmentMode, bool, error) {
	taskID := stringValue(task["id"])
	var selected canonicalAssignmentMode
	selectedFound := false
	for _, owner := range canonicalStringValues(task["owner_agent_ids"]) {
		agent, ok, err := uniqueEntityByID(input.Snapshot.State, "agents", owner)
		if err != nil || !ok {
			if err != nil {
				return selected, true, err
			}
			continue
		}
		mode, canonical, modeErr := canonicalModeForOwner(input, owner)
		if !canonical {
			continue
		}
		// The registered current Assignment must still name this TASK and
		// the bidirectional Runtime owner links must agree. An explicit S6
		// owner on this TASK cannot use its old legacy report.
		if modeErr == nil && mode.HasRecord && mode.Record.Task.ID != taskID {
			modeErr = fmt.Errorf("current Assignment Record is bound to TASK %s, not %s", mode.Record.Task.ID, taskID)
		}
		if modeErr == nil && (!containsAny(agent["task_ids"], taskID) || !containsAny(task["owner_agent_ids"], owner)) {
			modeErr = fmt.Errorf("current Agent/TASK ownership links differ from the canonical Assignment")
		}
		if modeErr == nil && !mode.HasRecord {
			modeErr = fmt.Errorf("S6 marker has no current canonical Assignment Record")
		}
		if selectedFound {
			return selected, true, fmt.Errorf("TASK %s has multiple canonical Builder Assignments", taskID)
		}
		selected, selectedFound = mode, true
		if modeErr != nil {
			return selected, true, modeErr
		}
	}
	return selected, selectedFound, nil
}

func canonicalStringValues(raw any) []string {
	var values []string
	switch rows := raw.(type) {
	case []string:
		values = append(values, rows...)
	case []any:
		for _, row := range rows {
			if value, ok := row.(string); ok {
				values = append(values, value)
			}
		}
	}
	return values
}

func readFrozenAssignmentSource(input Input, ref string) ([]byte, error) {
	if strings.HasPrefix(ref, assignmentinputs.SchemaPrefix) || ref == assignmentinputs.ContextProfileRef {
		data, err := assignmentinputs.Read(input.Root, ref)
		if err != nil {
			return nil, err
		}
		return data, nil
	}
	viewBytes, err := input.Files.ReadFile(ref)
	if err != nil {
		return nil, err
	}
	physical, err := assignmentinputs.Read(input.Root, ref)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(viewBytes, physical) {
		return nil, fmt.Errorf("original file view differs from the current authority checkout")
	}
	return viewBytes, nil
}

func canonicalOwnerArtifactPrefix(record assignmentrecord.Record) string {
	return fmt.Sprintf(".claude/evidence/%s/g%d/assignments/%s/", record.RuntimeID, record.BaselineGeneration, record.OwnerAgentID)
}

func safeCanonicalRef(ref string) bool {
	return ref != "" && !filepath.IsAbs(ref) && !strings.ContainsAny(ref, "\\\x00\r\n") && filepath.ToSlash(filepath.Clean(filepath.FromSlash(ref))) == ref && ref != "." && ref != ".." && !strings.HasPrefix(ref, "../")
}

func currentCanonicalReportRef(state map[string]any, owner, taskID string) (string, error) {
	task, found, err := uniqueEntityByID(state, "tasks", taskID)
	if err != nil || !found {
		return "", fmt.Errorf("current TASK is missing or ambiguous")
	}
	agent, found, err := uniqueEntityByID(state, "agents", owner)
	if err != nil || !found {
		return "", fmt.Errorf("current owner Agent is missing or ambiguous")
	}
	if !containsAny(task["owner_agent_ids"], owner) || !containsAny(agent["task_ids"], taskID) {
		return "", fmt.Errorf("current Agent/TASK ownership differs from the Assignment")
	}
	taskRef, agentRef := stringValue(task["completion_report_ref"]), stringValue(agent["completion_reported_ref"])
	if taskRef == "" || agentRef == "" || taskRef != agentRef {
		return "", fmt.Errorf("current Agent/TASK Result references are missing or differ")
	}
	return taskRef, nil
}

func canonicalCurrentPlanRef(input Input, owner string) (string, error) {
	ref, err := plancheckpoint.ActivatedRef(input.Root, input.Snapshot, owner)
	if err != nil {
		return "", fmt.Errorf("validate current accepted PLAN_REPORT: %w", err)
	}
	agent, found, err := uniqueEntityByID(input.Snapshot.State, "agents", owner)
	record, native, _ := assignmentrecord.ForOwner(input.Snapshot.State, owner)
	if err != nil || !found || ref == "" || (!(native && record.Contract == assignmentrecord.NativeDeliveryContract) && stringValue(agent["readback_ref"]) != ref) || stringValue(agent["plan_reported_ref"]) != ref || !safeCanonicalRef(ref) {
		return "", fmt.Errorf("current accepted PLAN_REPORT reference is unavailable or inconsistent")
	}
	return ref, nil
}

func readCanonicalResultArtifact(input Input, record assignmentrecord.Record, currentPlan, ref string) ([]byte, error) {
	if !safeCanonicalRef(ref) {
		return nil, fmt.Errorf("unsafe canonical Result artifact reference")
	}
	if ref != currentPlan && !strings.HasPrefix(ref, canonicalOwnerArtifactPrefix(record)) {
		return nil, fmt.Errorf("canonical Result artifact is outside the Assignment evidence namespace")
	}
	return workpacket.ReadFile(input.Root, ref, canonicalReceiptReadLimit)
}

func validateCurrentCanonicalRecord(input Input, mode canonicalAssignmentMode, owner, assignmentID, taskID string, requireDelivery bool) (assignmentrecord.Record, string, error) {
	if !mode.HasRecord {
		return assignmentrecord.Record{}, "", fmt.Errorf("explicit S6 marker has no current Assignment Record")
	}
	record := mode.Record
	if input.Root == "" || input.Files == nil {
		return assignmentrecord.Record{}, "", fmt.Errorf("canonical receipt requires the original Runtime root and FileView")
	}
	if err := fileview.ValidateAuthority(input.Root, input.Snapshot.State); err != nil {
		return assignmentrecord.Record{}, "", fmt.Errorf("validate original Runtime authority: %w", err)
	}
	if record.OwnerAgentID != owner || record.AssignmentID != assignmentID || record.Task.ID != taskID || record.RuntimeID != stringValue(input.Snapshot.State["runtime_id"]) || record.BaselineGeneration != nestedInt(input.Snapshot.State, "baseline", "generation") {
		return assignmentrecord.Record{}, "", fmt.Errorf("current Assignment Record differs from the receipt owner, TASK, generation or assignment")
	}
	agent, found, err := uniqueEntityByID(input.Snapshot.State, "agents", owner)
	if err != nil || !found {
		return assignmentrecord.Record{}, "", fmt.Errorf("current canonical owner Agent is missing or ambiguous")
	}
	if state := stringValue(workstate.AgentActivity(agent)); requireDelivery && state != "reported" && state != "done" && state != "stopped" {
		return assignmentrecord.Record{}, "", fmt.Errorf("current canonical owner has not recorded delivery")
	}
	task, found, err := uniqueEntityByID(input.Snapshot.State, "tasks", taskID)
	if err != nil || !found || workstate.TaskActivity(task) == "blocked" || workstate.TaskActivity(task) == "cancelled" {
		return assignmentrecord.Record{}, "", fmt.Errorf("current canonical TASK is missing, ambiguous, blocked or cancelled")
	}
	if !containsAny(task["owner_agent_ids"], owner) || !containsAny(agent["task_ids"], taskID) {
		return assignmentrecord.Record{}, "", fmt.Errorf("current Agent/TASK ownership differs from the canonical Assignment")
	}
	_, assignment, _ := strings.Cut(stringValue(agent["prompt_ref"]), "#")
	if assignment != record.AssignmentID || stringValue(agent["team_id"]) != record.TeamID || stringValue(agent["definition_ref"]) != record.AgentDefinition.Ref {
		return assignmentrecord.Record{}, "", fmt.Errorf("current Agent prompt/team/definition differs from the canonical Assignment")
	}
	teams, _ := input.Snapshot.State["entities"].(map[string]any)
	teamFound := 0
	for _, team := range anyRows(teams["teams"]) {
		if team["id"] == record.TeamID {
			teamFound++
			if team["manifest_ref"] != record.Manifest.Ref {
				return assignmentrecord.Record{}, "", fmt.Errorf("current Team manifest reference differs from the canonical Assignment")
			}
		}
	}
	if teamFound != 1 {
		return assignmentrecord.Record{}, "", fmt.Errorf("canonical Assignment Team is missing or ambiguous")
	}
	manifest, err := readFrozenAssignmentSource(input, record.Manifest.Ref)
	if err != nil {
		return assignmentrecord.Record{}, "", fmt.Errorf("read canonical Assignment manifest: %w", err)
	}
	enabled, err := assignmentrecord.ManifestAssignmentRecordEnabled(manifest)
	if err != nil || !enabled {
		return assignmentrecord.Record{}, "", fmt.Errorf("canonical Assignment manifest no longer has its exact S6 marker")
	}
	if err := assignmentrecord.VerifySources(record, func(ref string) ([]byte, error) { return readFrozenAssignmentSource(input, ref) }); err != nil {
		return assignmentrecord.Record{}, "", fmt.Errorf("verify frozen Assignment sources: %w", err)
	}
	digest, err := record.Digest()
	if err != nil {
		return assignmentrecord.Record{}, "", err
	}
	registered, found, err := assignmentrecord.ForOwner(input.Snapshot.State, owner)
	if err != nil || !found {
		return assignmentrecord.Record{}, "", fmt.Errorf("current Assignment Record registration is missing or invalid")
	}
	registeredDigest, digestErr := registered.Digest()
	if digestErr != nil || registered.Reference() != record.Reference() || registeredDigest != digest {
		return assignmentrecord.Record{}, "", fmt.Errorf("current Assignment Record reference or digest changed")
	}
	var taskDocs []documentFact
	for _, document := range currentDocuments(input.Snapshot.State, record.BaselineGeneration) {
		if document.Kind == "task" && document.ID == record.Task.ID {
			taskDocs = append(taskDocs, document)
		}
	}
	if len(taskDocs) != 1 || taskDocs[0].Path != record.Task.Ref || taskDocs[0].Version != record.Task.Version || taskDocs[0].SHA256 != record.Task.SHA256 {
		return assignmentrecord.Record{}, "", fmt.Errorf("current registered TASK document differs from the canonical Assignment")
	}
	taskView, err := input.Files.ReadFile(record.Task.Ref)
	if err != nil || sha256Hex(taskView) != record.Task.SHA256 {
		return assignmentrecord.Record{}, "", fmt.Errorf("current TASK source differs from its registered fingerprint")
	}
	taskPhysical, err := assignmentinputs.Read(input.Root, record.Task.Ref)
	if err != nil || !bytes.Equal(taskView, taskPhysical) {
		return assignmentrecord.Record{}, "", fmt.Errorf("current TASK checkout differs from the original FileView")
	}
	return record, digest, nil
}

func canonicalResultAwaitingSubmission(state map[string]any, owner, taskID string) bool {
	agent, agentFound, agentErr := uniqueEntityByID(state, "agents", owner)
	task, taskFound, taskErr := uniqueEntityByID(state, "tasks", taskID)
	if agentErr != nil || taskErr != nil || !agentFound || !taskFound ||
		stringValue(agent["completion_reported_ref"]) != "" || stringValue(task["completion_report_ref"]) != "" {
		return false
	}
	switch workstate.AgentActivity(agent) {
	case "spawned", "queued", "reading", "understanding_submitted", "understanding_approved", "activated", "working":
		return true
	default:
		return false
	}
}

func resolveCanonicalBuilderResult(input Input, mode canonicalAssignmentMode, owner, assignmentID, taskID string) canonicalBuilderResult {
	out := canonicalBuilderResult{MainReason: "integration_pending"}
	record, recordSHA, err := validateCurrentCanonicalRecord(input, mode, owner, assignmentID, taskID, true)
	if err != nil {
		out.Err = err
		return out
	}
	out.Record, out.RecordSHA = record, recordSHA
	ref, err := currentCanonicalReportRef(input.Snapshot.State, owner, taskID)
	if err != nil {
		out.Err = err
		return out
	}
	out.ResultRef = ref
	if !strings.HasPrefix(ref, canonicalOwnerArtifactPrefix(record)) {
		out.Err = fmt.Errorf("current Result ref is outside the canonical Assignment evidence namespace")
		return out
	}
	planRef, err := canonicalCurrentPlanRef(input, owner)
	if err != nil {
		out.Err = err
		return out
	}
	read := func(path string) ([]byte, error) { return readCanonicalResultArtifact(input, record, planRef, path) }
	accepted, err := assignmentresult.LoadAccepted(input.Snapshot.State, record, ref, read)
	if err != nil {
		out.MainReason = "result_evidence_invalid"
		out.Err = err
		return out
	}
	if accepted.PlanRef != planRef {
		out.MainReason = "result_evidence_invalid"
		out.Err = fmt.Errorf("accepted Result is not bound to the current accepted PLAN_REPORT")
		return out
	}
	history, err := assignmentresult.AcceptedHistory(input.Snapshot.State, record, read)
	if err != nil || len(history) == 0 || history[len(history)-1].EvidenceID != accepted.EvidenceID || history[len(history)-1].AnswerDigest != accepted.AnswerDigest {
		out.MainReason = "result_evidence_invalid"
		out.Err = fmt.Errorf("current Result is not the latest valid result in the original Assignment history: %v", err)
		return out
	}
	resultBytes, err := read(ref)
	if err != nil {
		out.MainReason = "result_evidence_invalid"
		out.Err = err
		return out
	}
	out.Result, out.ResultSHA, out.EvidenceID = accepted, sha256Hex(resultBytes), accepted.EvidenceID
	out.MainReason = "integration_pending"
	checkpointRel := filepath.ToSlash(assignmentrecord.IntegrationCheckpointPath("", record.Reference()))
	out.Checkpoint = checkpointRel
	cpBytes, err := workpacket.ReadFile(input.Root, checkpointRel, canonicalReceiptReadLimit)
	if err != nil {
		return out
	}
	out.CheckpointSHA = sha256Hex(cpBytes)
	var cp canonicalMainCheckpoint
	if err := json.Unmarshal(cpBytes, &cp); err != nil {
		out.MainReason = "integration_receipt_invalid"
		return out
	}
	if cp.AssignmentRecord == nil && cp.AssignmentRecordSHA256 != "" || cp.AssignmentRecord != nil && (*cp.AssignmentRecord != record.Reference() || cp.AssignmentRecordSHA256 != recordSHA) {
		out.MainReason = "integration_receipt_invalid"
		return out
	}
	facts := cp.CanonicalMainFacts
	if facts == nil || (record.Contract == assignmentrecord.NativeDeliveryContract && facts.SourceHead != accepted.SourceHead) || cp.TaskID != record.Task.ID || cp.AssignmentID != record.AssignmentID || cp.Generation != record.BaselineGeneration || cp.ReportPath != ref || cp.ReportSHA != out.ResultSHA || facts.SourceHead != cp.SourceHead || facts.MergeBase != cp.MergeBase || facts.TestedHead != cp.TestedHead || facts.TargetBranch != cp.TargetBranch || facts.MergeCommit != cp.MergeCommit || cp.ChangedPaths != nil || cp.ChangedPathsSHA256 != "" {
		out.MainReason = "integration_receipt_invalid"
		return out
	}
	mainRoot, rootErr := canonicalMainRoot(input.Root)
	if rootErr != nil || !canonicalMainRootAuthorized(mainRoot, facts.MainRoot, cp.PreviousMainRoots) || facts.TargetBranch != boundDevelopmentBranch(input.Snapshot.State) {
		out.MainReason = "integration_receipt_invalid"
		return out
	}
	switch cp.State {
	case "verified", "acknowledged", "cleanup_pending", "complete":
		// These are the only states that may prove Main integration.
	case "pending", "ready", "merged":
		out.MainReason = "integration_pending"
		return out
	case "blocked", "preserved":
		out.MainReason = "integration_receipt_invalid"
		return out
	default:
		out.MainReason = "integration_receipt_invalid"
		return out
	}
	executionGeneration, err := validateCanonicalExecutionBinding(input, record, *facts)
	if err != nil {
		out.MainReason = "integration_receipt_invalid"
		return out
	}
	finalTargetHead, err := resolveCanonicalBoundTargetHead(context.Background(), mainRoot, facts.TargetBranch)
	if err != nil {
		out.MainReason = "integration_receipt_invalid"
		return out
	}
	authorizedMainRoots := canonicalAuthorizedMainRoots(mainRoot, cp.PreviousMainRoots)
	proof, err := mainreceipt.Verify(context.Background(), mainreceipt.Input{
		AuthorityRoot: mainRoot, Record: record, Accepted: accepted, ResultRef: ref,
		ResultSHA256: out.ResultSHA, ExecutionGeneration: executionGeneration,
		AuthorizedMainRoots: authorizedMainRoots, ExpectedTargetBranch: facts.TargetBranch,
		FinalTargetHead: finalTargetHead,
	})
	if err != nil || proof.CheckpointSHA256() != out.CheckpointSHA {
		out.MainReason = "integration_receipt_invalid"
		return out
	}
	verifiedFacts := proof.MainFacts()
	out.Facts = &verifiedFacts
	out.Integrated = true
	return out
}

func canonicalAuthorizedMainRoots(currentRoot string, previous []string) []string {
	roots := []string{currentRoot}
	for _, prior := range previous {
		abs, err := filepath.Abs(prior)
		if err != nil {
			roots = append(roots, "")
			continue
		}
		roots = append(roots, filepath.Clean(abs))
	}
	return roots
}

func resolveCanonicalBoundTargetHead(ctx context.Context, root, branch string) (string, error) {
	branch = strings.TrimSpace(strings.TrimPrefix(branch, "refs/heads/"))
	if branch == "" {
		return "", fmt.Errorf("bound development branch is empty")
	}
	if _, err := workspace.Git(ctx, root, "check-ref-format", "refs/heads/"+branch); err != nil {
		return "", fmt.Errorf("bound development branch is invalid: %w", err)
	}
	head, err := workspace.Git(ctx, root, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve pinned bound development branch head: %w", err)
	}
	if len(head) != 40 && len(head) != 64 {
		return "", fmt.Errorf("bound development branch returned an invalid commit object ID")
	}
	for _, ch := range head {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return "", fmt.Errorf("bound development branch returned a noncanonical commit object ID")
		}
	}
	return head, nil
}

func canonicalMainRoot(root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("empty Main root")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func canonicalMainRootAuthorized(currentRoot, factsRoot string, previous []string) bool {
	factsAbs, err := filepath.Abs(factsRoot)
	if err != nil {
		return false
	}
	if canonical, err := filepath.EvalSymlinks(factsAbs); err == nil && canonical == currentRoot {
		return true
	}
	for _, prior := range previous {
		priorAbs, err := filepath.Abs(prior)
		if err != nil || priorAbs != factsAbs {
			continue
		}
		return true
	}
	return false
}

func validateCanonicalExecutionBinding(input Input, record assignmentrecord.Record, facts assignmentresult.MainFacts) (int, error) {
	registry, err := workspace.Decode(input.Snapshot.State)
	if err != nil {
		return 0, fmt.Errorf("decode current workspace execution registry: %w", err)
	}
	if registry == nil || record.Contract == assignmentrecord.NativeDeliveryContract {
		if facts.ExecutionGeneration != 0 {
			return 0, fmt.Errorf("Main facts claim a workspace execution that the Runtime does not register")
		}
		return 0, nil
	}
	currentRoot, err := canonicalMainRoot(input.Root)
	if err != nil {
		return 0, err
	}
	registeredRoot, err := canonicalMainRoot(registry.MainRoot)
	if err != nil || registeredRoot != currentRoot {
		return 0, fmt.Errorf("current workspace registry is bound to another Main root")
	}
	execution, found := registry.Execution(record.AssignmentID, record.RuntimeID, record.BaselineGeneration)
	if !found {
		return 0, fmt.Errorf("current workspace registry has no execution for the canonical Assignment")
	}
	recordSHA, err := record.Digest()
	if err != nil {
		return 0, err
	}
	if execution.AgentID != record.OwnerAgentID || execution.AssignmentRecord == nil || *execution.AssignmentRecord != record.Reference() || execution.AssignmentRecordSHA256 != recordSHA || execution.Generation <= 0 || facts.ExecutionGeneration != execution.Generation {
		return 0, fmt.Errorf("Main execution generation differs from the current canonical workspace execution")
	}
	return execution.Generation, nil
}

func validateCanonicalMainToolReceipts(input Input, record assignmentrecord.Record, cp canonicalMainCheckpoint, facts assignmentresult.MainFacts) error {
	if len(cp.CheckReceipts) != len(facts.Checks) || len(cp.ToolCheckReceipts) != len(record.CheckProfiles) {
		return fmt.Errorf("Main checkpoint does not preserve the complete canonical check receipt set")
	}
	checkPaths := map[string]bool{}
	for _, ref := range cp.CheckReceipts {
		if ref == "" || checkPaths[ref] {
			return fmt.Errorf("Main checkpoint duplicates or omits a check receipt reference")
		}
		checkPaths[ref] = true
	}
	if len(checkPaths) != len(facts.Checks) {
		return fmt.Errorf("Main checkpoint check receipt count differs from canonical facts")
	}
	factByIndex := make(map[int]assignmentresult.MainCheckFact, len(facts.Checks))
	for _, fact := range facts.Checks {
		if !checkPaths[fact.ReceiptRef] {
			return fmt.Errorf("Main checkpoint omits a canonical check receipt")
		}
		if _, exists := factByIndex[fact.CheckIndex]; exists {
			return fmt.Errorf("Main checkpoint duplicates canonical check indexes")
		}
		factByIndex[fact.CheckIndex] = fact
	}
	mainBinding := checkexec.Binding{RuntimeID: record.RuntimeID, Generation: record.BaselineGeneration, AssignmentID: record.AssignmentID, Owner: "Main", ExecutionGeneration: facts.ExecutionGeneration, Responsibility: "main_required_integration"}
	profileByIndex := make(map[int]checkexec.Profile, len(record.CheckProfiles))
	for _, profile := range record.CheckProfiles {
		profileByIndex[profile.CheckIndex] = profile
	}
	seenProfiles := map[int]bool{}
	seenReceiptPaths := map[string]bool{}
	for _, ref := range cp.ToolCheckReceipts {
		index := profileIndexForMainReceipt(record, ref)
		profile, found := profileByIndex[index]
		if !found || seenProfiles[profile.CheckIndex] || seenReceiptPaths[ref.Path] || ref.Binding != mainBinding || ref.Command != profile.Command || ref.ProfileDigest != profile.Digest() || ref.Checkout != facts.MainRoot {
			return fmt.Errorf("captured Main tool receipt differs from its original check profile or execution binding")
		}
		fact, found := factByIndex[profile.CheckIndex]
		if !found || fact.Command != profile.Command || fact.ProfileDigest != profile.Digest() || fact.ReceiptRef != ref.Path {
			return fmt.Errorf("captured Main tool receipt is not covered by the canonical check facts")
		}
		rel, err := canonicalMainCheckArtifactRel(input.Root, record, facts.MainRoot, cp.PreviousMainRoots, ref.Path)
		if err != nil {
			return err
		}
		body, err := workpacket.ReadFile(input.Root, rel, 4<<20)
		if err != nil || checkexec.Hash(body) != ref.SHA256 {
			return fmt.Errorf("captured Main tool receipt bytes changed: %s", profile.Command)
		}
		currentRef := ref
		currentRef.Path = filepath.Join(input.Root, filepath.FromSlash(rel))
		checkRoot := filepath.Join(filepath.Dir(assignmentrecord.IntegrationCheckpointPath(input.Root, record.Reference())), "checks")
		verified, err := checkexec.VerifyToolReceipt(checkRoot, currentRef)
		if err != nil || !verified.Passed {
			return fmt.Errorf("captured Main tool receipt is not a passing original receipt: %s", profile.Command)
		}
		seenProfiles[profile.CheckIndex] = true
		seenReceiptPaths[ref.Path] = true
	}
	if len(seenProfiles) != len(record.CheckProfiles) {
		return fmt.Errorf("Main checkpoint omits an original CheckProfile receipt")
	}
	return nil
}

func profileIndexForMainReceipt(record assignmentrecord.Record, ref checkexec.ToolReceipt) int {
	for _, profile := range record.CheckProfiles {
		if profile.Command == ref.Command && profile.Digest() == ref.ProfileDigest {
			return profile.CheckIndex
		}
	}
	return -1
}

func readCanonicalMainCheckArtifact(root string, record assignmentrecord.Record, factsRoot string, previous []string, ref string) ([]byte, error) {
	rel, err := canonicalMainCheckArtifactRel(root, record, factsRoot, previous, ref)
	if err != nil {
		return nil, err
	}
	return workpacket.ReadFile(root, rel, canonicalReceiptReadLimit)
}

func canonicalMainCheckArtifactRel(root string, record assignmentrecord.Record, factsRoot string, previous []string, ref string) (string, error) {
	if !filepath.IsAbs(ref) {
		return "", fmt.Errorf("Main check artifact reference must be the original absolute runner ref")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	refAbs, err := filepath.Abs(ref)
	if err != nil {
		return "", err
	}
	allowedRoots := []string{rootAbs}
	if factsRoot != "" {
		factsAbs, absErr := filepath.Abs(factsRoot)
		if absErr != nil {
			return "", absErr
		}
		allowedRoots = append(allowedRoots, factsAbs)
	}
	for _, prior := range previous {
		if abs, absErr := filepath.Abs(prior); absErr == nil {
			allowedRoots = append(allowedRoots, abs)
		}
	}
	var rel string
	for _, allowedRoot := range allowedRoots {
		candidate, relErr := filepath.Rel(allowedRoot, refAbs)
		if relErr == nil && !filepath.IsAbs(candidate) && candidate != ".." && !strings.HasPrefix(candidate, ".."+string(filepath.Separator)) {
			rel = candidate
			break
		}
	}
	if rel == "" {
		return "", fmt.Errorf("Main check artifact escapes the original authorized Main roots")
	}
	rel = filepath.ToSlash(rel)
	prefix := path.Join(path.Dir(filepath.ToSlash(assignmentrecord.IntegrationCheckpointPath("", record.Reference()))), "checks") + "/"
	if !strings.HasPrefix(rel, prefix) {
		return "", fmt.Errorf("Main check artifact is outside the exact Assignment check-receipt directory")
	}
	remainder := strings.TrimPrefix(rel, prefix)
	parts := strings.Split(remainder, "/")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "loop-integration-check-") || len(parts[0]) == len("loop-integration-check-") || (parts[1] != "receipt.json" && parts[1] != "output.log") {
		return "", fmt.Errorf("Main check artifact is outside the original runner receipt layout")
	}
	return rel, nil
}

func canonicalGitDiffMatches(root, targetHead string, facts assignmentresult.MainFacts) bool {
	if targetHead == "" || facts.MergeBase == "" || facts.SourceHead == "" {
		return false
	}
	mergeBase := exec.Command("git", "-C", root, "merge-base", targetHead, facts.SourceHead)
	out, err := mergeBase.Output()
	if err != nil || strings.TrimSpace(string(out)) != facts.MergeBase {
		return false
	}
	cmd := exec.Command("git", "-C", root, "diff", "--name-only", "--no-renames", "-z", facts.MergeBase, facts.SourceHead, "--")
	out, err = cmd.Output()
	if err != nil {
		return false
	}
	actual := []string{}
	for _, item := range bytes.Split(out, []byte{0}) {
		if len(item) > 0 {
			actual = append(actual, string(item))
		}
	}
	sort.Strings(actual)
	digest, err := assignmentresult.ChangedPathsDigest(actual)
	return err == nil && digest == facts.ChangedPathsSHA256 && reflect.DeepEqual(actual, facts.ChangedPaths)
}

func currentCanonicalBuilderReceipt(input Input, req BuilderReceiptRequest, mode canonicalAssignmentMode, modeErr error) BuilderReceipt {
	r := BuilderReceipt{
		SchemaVersion: "1.0.0", RuntimeID: stringValue(input.Snapshot.State["runtime_id"]),
		Generation: nestedInt(input.Snapshot.State, "baseline", "generation"), ObservedRevision: input.Snapshot.Revision,
		Owner: req.Owner, AssignmentID: req.AssignmentID, TaskID: req.TaskID, ChecksSource: "main_checkpoint",
		ResponsibilityStatus: "invalid", DomainRefs: []string{}, OwnPending: []string{}, WaitingOn: []ResponsibilityWaiting{},
		ProfessionalStatus: "not_submitted",
	}
	invalid := func(reason string) BuilderReceipt { r.OwnPending = append(r.OwnPending, reason); return r }
	if modeErr != nil {
		return invalid("canonical_assignment_invalid")
	}
	if req.Owner == "" || req.AssignmentID == "" || req.TaskID == "" || input.Files == nil {
		return invalid("registered_assignment")
	}
	awaitingResult := canonicalResultAwaitingSubmission(input.Snapshot.State, req.Owner, req.TaskID)
	record, recordSHA, err := validateCurrentCanonicalRecord(input, mode, req.Owner, req.AssignmentID, req.TaskID, !awaitingResult)
	if err != nil {
		return invalid("canonical_assignment_invalid")
	}
	if awaitingResult {
		r.ResponsibilityStatus = "not_submitted"
		r.OwnPending = append(r.OwnPending, "current_Result")
		return r
	}
	ref, err := currentCanonicalReportRef(input.Snapshot.State, req.Owner, req.TaskID)
	if err != nil {
		return invalid("current_Result")
	}
	r.DeliveryRecorded = true
	r.ResponsibilityStatus = "invalid"
	if req.CompletionRef != "" && req.CompletionRef != ref {
		return invalid("current_Result")
	}
	planRef, err := canonicalCurrentPlanRef(input, req.Owner)
	if err != nil {
		r.WaitingOn = append(r.WaitingOn, ResponsibilityWaiting{Owner: "main", Reason: "result_evidence_invalid", Ref: ref})
		return r
	}
	read := func(p string) ([]byte, error) { return readCanonicalResultArtifact(input, record, planRef, p) }
	accepted, err := assignmentresult.LoadAccepted(input.Snapshot.State, record, ref, read)
	if err != nil || accepted.PlanRef != planRef {
		r.WaitingOn = append(r.WaitingOn, ResponsibilityWaiting{Owner: "main", Reason: "result_evidence_invalid", Ref: ref})
		return r
	}
	history, err := assignmentresult.AcceptedHistory(input.Snapshot.State, record, read)
	if err != nil || len(history) == 0 || history[len(history)-1].EvidenceID != accepted.EvidenceID || history[len(history)-1].AnswerDigest != accepted.AnswerDigest {
		r.WaitingOn = append(r.WaitingOn, ResponsibilityWaiting{Owner: "main", Reason: "result_evidence_invalid", Ref: ref})
		return r
	}
	resultBytes, err := read(ref)
	if err != nil {
		r.WaitingOn = append(r.WaitingOn, ResponsibilityWaiting{Owner: "main", Reason: "result_evidence_invalid", Ref: ref})
		return r
	}
	r.Accepted, r.ResponsibilityStatus = true, "accepted_incomplete"
	r.ProfessionalStatus = "accepted_unassessed"
	reference := record.Reference()
	r.AssignmentRecord, r.AssignmentRecordSHA256 = &reference, recordSHA
	r.ResultRef, r.ResultSHA256 = ref, sha256Hex(resultBytes)
	r.ResultRevision, r.AnswerDigest = accepted.ResultRevision, accepted.AnswerDigest
	r.PlanRef, r.PlanSHA256 = accepted.PlanRef, accepted.PlanSHA256
	r.ResolvedEvidenceRefs = append([]assignmentresult.ResolvedEvidence{}, accepted.ResolvedEvidenceRefs...)
	r.WorkerCheckFacts = append([]assignmentresult.WorkerCheckFact{}, accepted.WorkerCheckFacts...)
	r.ToolClosureDigest, _ = assignmentresult.EffectiveToolClosureDigest(record, accepted)
	r.DomainRefs = append(r.DomainRefs, ref)
	for _, fact := range accepted.WorkerCheckFacts {
		if fact.ReceiptArtifact != nil {
			r.DomainRefs = append(r.DomainRefs, fact.ReceiptArtifact.Path)
		}
		if fact.LogArtifact != nil {
			r.DomainRefs = append(r.DomainRefs, fact.LogArtifact.Path)
		}
	}
	applyCanonicalProfessionalClosure(&r, record, accepted)
	projection := resolveCanonicalBuilderResult(input, mode, req.Owner, req.AssignmentID, req.TaskID)
	if projection.Err != nil && projection.MainReason == "result_evidence_invalid" {
		r.WaitingOn = append(r.WaitingOn, ResponsibilityWaiting{Owner: "main", Reason: "result_evidence_invalid", Ref: ref})
		return r
	}
	checkpoint := filepath.ToSlash(assignmentrecord.IntegrationCheckpointPath("", record.Reference()))
	if projection.CheckpointSHA != "" {
		r.MainCheckpointRef, r.MainCheckpointSHA256 = checkpoint, projection.CheckpointSHA
	}
	if projection.Integrated && projection.Facts != nil {
		r.Integrated = true
		r.ActualChangedPaths = append([]string{}, projection.Facts.ChangedPaths...)
		r.ActualChangedPathsSHA256 = projection.Facts.ChangedPathsSHA256
		r.MainCheckReceipts = append([]assignmentresult.MainCheckFact{}, projection.Facts.Checks...)
		r.DomainRefs = append(r.DomainRefs, checkpoint)
		return r
	}
	r.WaitingOn = append(r.WaitingOn, ResponsibilityWaiting{Owner: "main", Reason: projection.MainReason, Ref: checkpoint})
	return r
}

func applyCanonicalProfessionalClosure(receipt *BuilderReceipt, record assignmentrecord.Record, result assignmentresult.AcceptedResult) {
	if receipt == nil {
		return
	}
	answer := result.Answer
	if record.ClosingContract == nil {
		receipt.OwnPending = append(receipt.OwnPending, "professional_closing_contract_unproven")
		return
	}
	assessment := answer.ClosingAssessment
	if assessment == nil {
		receipt.OwnPending = append(receipt.OwnPending, "professional_closing_contract_unproven")
		return
	}
	if assessment.Disposition == "blocked" {
		receipt.ProfessionalStatus = "incomplete"
	} else if assessment.Disposition == "incomplete" {
		receipt.ProfessionalStatus = "incomplete"
	} else if assessment.Disposition == "complete" && !assignmentresult.ClosingAssessmentComplete(answer) {
		receipt.ProfessionalStatus = "incomplete"
	}
	if len(answer.RisksBlockers) > 0 || len(answer.ScopeDeviations) > 0 {
		receipt.ProfessionalStatus = "incomplete"
	}
	receipt.OwnPending = append(receipt.OwnPending, canonicalProfessionalClosurePending(record, answer, result)...)
	if len(receipt.OwnPending) == 0 {
		receipt.WorkComplete = true
		receipt.ResponsibilityStatus = "complete"
		receipt.ProfessionalStatus = "complete"
	}
}

func canonicalProfessionalClosurePending(record assignmentrecord.Record, answer assignmentresult.Answer, result assignmentresult.AcceptedResult) []string {
	if record.ClosingContract == nil || answer.ClosingAssessment == nil {
		return []string{"professional_closing_contract_unproven"}
	}
	pending := []string{}
	switch answer.ClosingAssessment.Disposition {
	case "blocked":
		pending = append(pending, "builder_blocked")
	case "incomplete":
		pending = append(pending, "closing_conditions_incomplete")
	case "complete":
		if !assignmentresult.ClosingAssessmentComplete(answer) {
			pending = append(pending, "closing_conditions_incomplete")
		}
	default:
		pending = append(pending, "professional_closing_contract_unproven")
	}
	if len(answer.RisksBlockers) > 0 {
		pending = append(pending, "declared_risks_or_blockers")
	}
	if len(answer.ScopeDeviations) > 0 {
		pending = append(pending, "unapproved_scope_deviation")
	}
	if len(record.ClosingContract.WorkerChecks) > 0 && !assignmentresult.WorkerClosureComplete(record, result) {
		found := false
		for _, fact := range result.WorkerCheckFacts {
			if fact.Status == "pass" {
				continue
			}
			found = true
			switch fact.Status {
			case "fail":
				pending = append(pending, fmt.Sprintf("owned_check_failed:%d", fact.Index))
			case "missing":
				pending = append(pending, fmt.Sprintf("owned_check_missing:%d", fact.Index))
			case "stale":
				pending = append(pending, fmt.Sprintf("owned_check_stale:%d", fact.Index))
			case "unavailable":
				pending = append(pending, fmt.Sprintf("owned_check_evidence_unavailable:%d", fact.Index))
			default:
				pending = append(pending, "owned_check_evidence_unavailable")
			}
		}
		if !found {
			pending = append(pending, "owned_check_evidence_unavailable")
		}
	}
	return pending
}

func canonicalProgressForTask(input Input, task map[string]any, mode canonicalAssignmentMode, modeErr error) BuilderProgress {
	id := stringValue(task["id"])
	progress := BuilderProgress{State: workstate.Active, Reason: "canonical result/integration not verified", Canonical: true}
	if modeErr != nil || !mode.HasRecord {
		progress.Reason = "canonical_assignment_invalid"
		return progress
	}
	_, err := currentCanonicalReportRef(input.Snapshot.State, mode.Record.OwnerAgentID, id)
	if err != nil {
		return progress
	}
	progress = BuilderProgress{State: workstate.Active, DeliveryRecorded: true, Reason: "canonical result/integration not verified", Canonical: true}
	projection := resolveCanonicalBuilderResult(input, mode, mode.Record.OwnerAgentID, mode.Record.AssignmentID, id)
	progress.EvidenceID = projection.EvidenceID
	if projection.Err != nil {
		if projection.MainReason == "result_evidence_invalid" {
			progress.Reason = "canonical_result_invalid"
		}
		return progress
	}
	if projection.Integrated {
		progress.Integrated = true
		if mode.Record.ClosingContract != nil {
			pending := canonicalProfessionalClosurePending(projection.Record, projection.Result.Answer, projection.Result)
			if len(pending) > 0 {
				progress.Reason = "professional_closure_incomplete:" + strings.Join(pending, ",")
				return progress
			}
		}
		progress.State = workstate.Done
		progress.Integrated = true
		progress.Reason = "current canonical Result and Main checkpoint verified"
	}
	return progress
}
