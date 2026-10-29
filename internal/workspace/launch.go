package workspace

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/processtree"
)

func NewSessionID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 15) | 64
	id[8] = (id[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}

// Launch changes only the child cwd. Identity must be persisted before calling;
// a retry must explicitly resume the same platform session, never guess latest.
func (b *ExecutionRegistry) Launch(ctx context.Context, e Execution, binary, role, prompt string, resume bool, stdout, stderr io.Writer) error {
	if err := b.Validate(ctx, b.MainRoot); err != nil {
		return err
	}
	if err := b.ValidateExecution(ctx, e, false); err != nil {
		return err
	}
	if err := b.ValidateInputs(e); err != nil {
		return err
	}
	submissionPaths, err := b.WorkerSubmissionPaths(e)
	if err != nil {
		return err
	}
	if e.PlatformSessionID == "" || e.BootstrapSHA256 == "" || !safeID.MatchString(role) {
		return fmt.Errorf("launch requires persisted session identity, bootstrap and installed role")
	}
	rolePath := filepath.Join(e.Path, ".claude/agents", role+".md")
	if info, err := os.Stat(rolePath); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("installed Worker role unavailable: %s", role)
	}
	flag := "--session-id"
	if resume {
		flag = "--resume"
	}
	state, s9, err := b.s9AuthoringState()
	if err != nil {
		return err
	}
	if HasDispatchContext(e) && !s9 {
		c, err := b.ReadDispatchContext(e)
		if err != nil {
			return err
		}
		refs := map[string]any{"assignment_context_ref": DispatchContextPath, "input_read_order": c.ReadingOrder(), "context_selection_mode": c.Selection.Mode}
		if c.PlanDraft != nil {
			refs["plan_draft_path"] = submissionPaths.Plan
			refs["plan_schema_ref"] = PlanSchemaPath
		}
		if c.ResultDraft != nil {
			refs["result_draft_path"] = c.ResultPath
			refs["result_schema_ref"] = PlanSchemaPath
		}
		refs["submission_paths"] = workerSubmissionPathProjection(c, submissionPaths)
		data, err := json.Marshal(refs)
		if err != nil {
			return err
		}
		loading := "Shadow skill selection changes no permissions or legacy loading."
		if c.Selection.Mode == "enabled" {
			if role != c.Dispatch.Role {
				return fmt.Errorf("enabled launch role differs from the original registered role")
			}
			loading = "The fixed backend core profile is enabled in this Worker role. Read its selected extra Skill paths; loading changes no original activation permissions or checks."
		}
		prompt += "\nFrozen dispatch sources: " + string(data) + "\nFirst Read assignment_context_ref, then its frozen input_read_order and installed selected Skill sources. " + loading + " Do not copy examples with another owner or guess identities. Approval-mode jobs keep their original approval; a draft cannot activate you."
		if c.PlanDraft != nil {
			prompt += " The plan draft has real machine bindings but unanswered professional fields; complete them using plan_questions and the full schema before begin."
		}
		if c.ResultDraft != nil {
			if _, hasClosing := c.ResultDraft["closing_assessment"]; hasClosing {
				prompt += " The canonical S6 Result draft has the original Record tuple. After completing the actual TASK and retaining the accepted PLAN_REPORT, add actual evidence_refs to the supplied assertion/oracle rows and author the summary, risks, scope deviations, disposition, condition judgments and rationales using result_questions and result_schema_ref. The listener supplies the Result identity and automatically calls the original report adapter when you save result_path. Do not add machine checks, changed paths, runner PASS claims, or integration conclusions."
			} else {
				prompt += " The canonical S6 Result draft has the original Record tuple. After completing the actual TASK and retaining the accepted PLAN_REPORT, add actual evidence_refs to the supplied assertion/oracle rows and author the summary, risks and scope deviations using result_questions and result_schema_ref. The listener supplies the Result identity and automatically calls the original report adapter when you save result_path. Do not add machine checks, changed paths, or PASS claims."
			}
		}
	}
	if s9 {
		var reading []string
		if HasDispatchContext(e) {
			c, err := b.ReadDispatchContext(e)
			if err != nil {
				return err
			}
			reading = c.ReadingOrder()
		}
		question := s9LaunchQuestionnaire(b.MainRoot, e, state)
		data, _ := json.Marshal(map[string]any{"responsibility": e.AssignmentID, "main_root": b.MainRoot, "worker_root": e.Path, "input_read_order": reading, "questionnaire": question, "check_commands": workspaceCheckCommands(e)})
		prompt += "\nCurrent S9 professional work: " + string(data) + "\nRead the supplied current questionnaire and actual input documents. Personally fill its professional answer and save the exact answer path through an editor, script or extension tool. Record actual work and real check results for this responsibility; known Git/task data is supplied. For actual source changes, use ordinary git add and git commit in this Worker, then save the supplied RepairResult answer. No-change tests require no new commit. No extra commit form or generic completion report is required. Independent verification remains separate work; Main owns real integration, necessary checks and subsequent stage handoff."
		if len(question) == 0 {
			prompt += " Main/system preparation gap: the current personal S9 questionnaire is not yet indexed; Main must supply its exact view/answer paths. Keep that concrete gap visible without constructing another plan or report."
		}
	} else if e.Purpose == DocumentReviewPurpose {
		reviewContext := map[string]any{"assignment": e, "work_packet_ref": ".claude/inputs/work-packet.json", "answer_path": submissionPaths.Answer, "submission_paths": workerSubmissionPathProjection(DispatchContext{AnswerPath: submissionPaths.Answer}, submissionPaths), "frozen_input_root": ".claude/inputs", "receipt_command": WorkerInvocation(e, "receipt"), "review_check_command": WorkerInvocation(e, "review-check"), "observation_commands": ObservationCommands(e), "owned_check_commands": ownedCheckCommands(e)}
		reviewPrompt := "\nRegistered S5 document review: %s\nRead the issued execution view. Each packet read[].path is authority-relative: read it below frozen_input_root. Fill only answer.conclusion/findings in the exact answer_path and save with Write/Edit; the original consumer validates and records the review. An ordinary plan_checkpoint review has no generic PLAN_REPORT or begin step. A plan_approval_required assignment still waits for its original approval. End this responsibility when work_complete=true; Main owns peer delivery, system followup and human decisions. Product writes, commit, Builder report and delivery are not granted."
		if submissionPaths.Plan != "" {
			reviewContext["plan_draft_path"] = submissionPaths.Plan
			reviewContext["begin_command"] = WorkerInvocation(e, "begin")
			reviewContext["answer_command"] = WorkerInvocation(e, "answer")
			reviewContext["submission_paths"].(map[string]string)["plan"] = submissionPaths.Plan
			reviewPrompt += " This registered execution also has an actual plan_draft_path; complete that original plan and use begin_command before saving the answer."
			reviewPrompt += " The answer command remains available for this explicit legacy execution if its normal save receipt is unavailable."
		}
		data, _ := json.Marshal(reviewContext)
		prompt += fmt.Sprintf(reviewPrompt, string(data))
	} else {
		var dispatchContext DispatchContext
		contextBound := HasDispatchContext(e)
		if contextBound {
			dispatchContext, _ = b.ReadDispatchContext(e)
		}
		var projectedPaths any = submissionPaths
		if contextBound {
			projectedPaths = workerSubmissionPathProjection(dispatchContext, submissionPaths)
		}
		contextProjection := map[string]any{"assignment": e, "observation_commands": ObservationCommands(e), "check_commands": workspaceCheckCommands(e), "owned_check_commands": ownedCheckCommands(e), "frozen_input_directory": ".claude/inputs", "submission_paths": projectedPaths}
		if !contextBound {
			contextProjection["begin_command"] = WorkerInvocation(e, "begin")
		}
		if !contextBound {
			contextProjection["report_command"] = WorkerInvocation(e, "report")
		}
		if len(e.WritePaths) > 0 || !contextBound {
			contextProjection["commit_command"] = WorkerInvocation(e, "commit")
		}
		if dispatchContext.Dispatch.Role == "backend-builder" || dispatchContext.Dispatch.Role == "frontend-builder" || dispatchContext.Dispatch.Role == "test-builder" || !contextBound {
			contextProjection["s9_delivery_command"] = WorkerInvocation(e, "deliver")
		}
		contextData, _ := json.Marshal(contextProjection)
		prompt += "\nRegistered Harness context: " + string(contextData) + "\nRead approved input documents under .claude/inputs. Keep the Main checkout unchanged. Save professional answers at their supplied submission paths."
		if dispatchContext.PlanDraft != nil {
			prompt += " Complete the supplied professional PLAN at the exact plan path. Saving it calls the original begin and activation flow automatically."
		}
		if dispatchContext.ResultDraft != nil {
			prompt += " Save the completed Result at its exact registered path; the listener calls the original report adapter automatically."
		}
		if len(e.WritePaths) > 0 {
			prompt += " Use commit only through the explicit commit adapter and supplied exact path."
		}
		if canonicalS6BuilderCommitDraft(e, dispatchContext) {
			prompt += " " + canonicalS6CommitDraftGuidance
		}
		prompt += " Use the repository agent-protocol schemas and actual Git/check commands appropriate to this work; workspace prepare lists the supplied check commands."
		if dispatchContext.ResultDraft != nil || !contextBound {
			prompt += " " + builderSubmissionOrder
		}
		prompt += " A process exit is not completion or release approval."
	}
	args := []string{"--print", flag, e.PlatformSessionID, "--agent", role, "--settings", filepath.Join(e.Path, ".claude/settings.json"), "--setting-sources", "project", "--permission-mode", "dontAsk", "--allowedTools", "Read,Glob,Grep,Write,Edit,Bash", "--", prompt}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = e.Path
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		// The new child has its own recorded identity. Do not inherit the parent's
		// nested-session marker; never copy or print credential values.
		if key == "CLAUDECODE" || key == "CLAUDE_CODE_SESSION_ID" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	return processtree.Run(cmd)
}

// s9AuthoringState reads current stage facts once. It does not validate packet
// identity, source hashes or receipts to choose the professional authoring flow.
func (b *ExecutionRegistry) s9AuthoringState() (map[string]any, bool, error) {
	data, err := os.ReadFile(filepath.Join(b.MainRoot, ".claude/loop-state.json"))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, false, err
	}
	life, _ := state["lifecycle"].(map[string]any)
	configuration, _ := state["configuration"].(map[string]any)
	optimization, _ := configuration["optimization"].(map[string]any)
	if life["state"] != "bug_resolution" || optimization["packet"] != true {
		return state, false, nil
	}
	switch life["phase"] {
	case "repair_readback", "planning", "reproducing", "fixing", "impact_reconciliation", "targeted_reverification", "ready_for_full_review":
		return state, true, nil
	}
	return state, false, nil
}

// Bindings select only the existing personal path, never permission to work.
// Missing questions remain preparation gaps within the same S9 workflow.
func s9LaunchQuestionnaire(mainRoot string, e Execution, state map[string]any) map[string]string {
	life, _ := state["lifecycle"].(map[string]any)
	var template, root string
	switch life["phase"] {
	case "repair_readback", "planning", "reproducing":
		template, root = "s9-plan-report-v1", mainRoot
	case "fixing":
		template, root = "s9-repair-result-v1", e.Path
	default:
		return nil // A later professional responsibility needs its own question.
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	var question map[string]string
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != template {
			continue
		}
		binding, _ := row["binding"].(map[string]any)
		if template == "s9-repair-result-v1" {
			binding, _ = binding["sources"].(map[string]any)
		}
		if binding["owner_agent_id"] != e.AgentID || binding["manifest_assignment_id"] != e.AssignmentID && binding["repair_assignment_id"] != e.AssignmentID {
			continue
		}
		view, _ := row["view_path"].(string)
		answer, _ := row["answer_path"].(string)
		if view != "" && answer != "" {
			question = map[string]string{"view_path": filepath.Join(root, filepath.FromSlash(view)), "answer_path": filepath.Join(root, filepath.FromSlash(answer))}
		}
	}
	return question
}

func workerSubmissionPathProjection(c DispatchContext, paths WorkerSubmissionPaths) map[string]string {
	out := map[string]string{}
	if c.PlanDraft != nil && paths.Plan != "" {
		out["plan"] = paths.Plan
	}
	if c.AnswerPath != "" && paths.Answer != "" {
		out["answer"] = paths.Answer
	}
	if c.ResultDraft != nil && paths.Completion != "" {
		out["completion"] = paths.Completion
	}
	if len(c.ProductWritePaths) > 0 && paths.Commit != "" {
		out["commit"] = paths.Commit
	}
	if (c.Dispatch.Role == "backend-builder" || c.Dispatch.Role == "frontend-builder" || c.Dispatch.Role == "test-builder") && paths.RepairResult != "" {
		out["repair_result"] = paths.RepairResult
	}
	return out
}

func workspaceCheckCommands(e Execution) []string {
	commands := []string{}
	for i, command := range e.Checks {
		if e.CheckLocation == "main" || strings.HasPrefix(strings.TrimSpace(command), "locked:") {
			continue
		}
		commands = append(commands, CheckInvocation(e, i))
	}
	return commands
}

func ownedCheckCommands(e Execution) []string {
	commands := make([]string, 0, len(e.WorkerChecks))
	for index := range e.WorkerChecks {
		commands = append(commands, OwnedCheckInvocation(e, index))
	}
	return commands
}

// SessionOwner recognizes only the registered top-level session in its own
// execution root. Native subagent IDs are validated separately by the caller.
func SessionOwner(ctx context.Context, main, cwd, session string) (string, error) {
	if session == "" {
		return "", nil
	}
	b, state, err := Load(main)
	if err != nil {
		return "", err
	}
	if b == nil {
		return "", nil
	}
	root, marked, err := PointerRoot(cwd)
	if err != nil {
		return "", err
	}
	if !marked {
		return "", nil
	}
	for _, e := range b.Executions {
		if e.Path != root || e.PlatformSessionID != session {
			continue
		}
		if e.Status != "ready" || e.RuntimeID != RuntimeID(state) || e.BaselineGeneration != Generation(state) {
			return "", fmt.Errorf("stale Worker platform session")
		}
		if err := b.ValidateExecution(ctx, e, false); err != nil {
			return "", err
		}
		return e.AgentID, nil
	}
	return "", fmt.Errorf("Worker platform session has no registered execution owner")
}
