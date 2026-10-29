package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// These are professional input/output references, not platform execution slots.
func nativeS9Paths(p workpacket.RepairPlanReportPacket) (brief, source, result string) {
	dir := filepath.ToSlash(filepath.Join(filepath.Dir(p.ViewPath), p.ID, "native"))
	return dir + "/brief.md", dir + "/source.json", ".claude/submissions/native-s9/" + p.ID + "/result.json"
}

// nativeS9AuthorPaths follows the immutable preparation contract. Existing
// preparations retain their original author locations.
func nativeS9AuthorPaths(root string, p workpacket.RepairPlanReportPacket) (plan, result string, err error) {
	_, pin, result := nativeS9Paths(p)
	raw, err := workpacket.ReadFile(root, pin, 4096)
	if err != nil {
		return "", "", err
	}
	var saved struct {
		Version int `json:"private_submission_version"`
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		return "", "", err
	}
	if saved.Version == 1 {
		return repair.NativeS9PrivatePaths(p.ID)
	}
	if saved.Version != 0 {
		return "", "", fmt.Errorf("unsupported native private submission version")
	}
	return p.AnswerPath, result, nil
}

func nativeS9PlanPackets(root string, state map[string]any) []workpacket.RepairPlanReportPacket {
	if stringValue(mapFieldCLI(state, "lifecycle")["state"]) != "bug_resolution" || !s9ProfessionalPhase(stringValue(mapFieldCLI(state, "lifecycle")["phase"])) {
		return nil
	}
	packets, err := runtime.CurrentRepairPlanReportPackets(root, state, "")
	if err != nil {
		return nil
	}
	pointer := repairPointerCLI(state)
	var current []workpacket.RepairPlanReportPacket
	for _, p := range packets {
		b := p.Binding
		if b.RuntimeID != state["runtime_id"] || b.BaselineGeneration != workspace.Generation(state) || b.SessionID != pointer["session_id"] || b.PlanRef != pointer["plan_ref"] || stringMapCLI(pointer["assignment_owners"])[b.RepairAssignmentID] != b.OwnerAgentID {
			continue
		}
		brief, _, _ := nativeS9Paths(p)
		if _, err := os.Stat(filepath.Join(root, brief)); err == nil {
			current = append(current, p)
		}
	}
	return current
}

func prepareNativeS9Brief(ctx context.Context, root string, p workpacket.RepairPlanReportPacket) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var assignment repair.RepairAssignment
	if err := json.Unmarshal(p.Inputs.RepairAssignment, &assignment); err != nil {
		return "", fmt.Errorf("read original native S9 RepairAssignment: %w", err)
	}
	assertionIDs, err := json.Marshal(assignment.AssertionIDs)
	if err != nil {
		return "", err
	}
	brief, pin, result := nativeS9Paths(p)
	base := ""
	if data, err := workpacket.ReadFile(root, pin, 4096); err == nil {
		var saved struct {
			Base string `json:"base_commit"`
		}
		if err := json.Unmarshal(data, &saved); err != nil || saved.Base == "" {
			return "", fmt.Errorf("original native S9 source commit is unavailable")
		}
		base = saved.Base
	} else if !os.IsNotExist(err) {
		return "", err
	} else {
		base, err = workspace.Git(ctx, root, "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		body, _ := json.Marshal(map[string]any{"base_commit": base, "private_submission_version": 1})
		published, err := workpacket.PublishBounded(root, pin, body, 4096)
		if err != nil {
			return "", err
		}
		if !bytes.Equal(body, published) {
			return "", fmt.Errorf("original native S9 baseline differs; preserve it")
		}
	}
	planPath, result, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		return "", err
	}
	for _, path := range []string{planPath, result} {
		if planPath == p.AnswerPath {
			break
		}
		if _, err := workspace.Git(ctx, root, "check-ignore", "--no-index", "--", path); err == nil {
			return "", fmt.Errorf("native private submission path is ignored: %s; original ignore rules must be preserved", path)
		}
	}
	text := fmt.Sprintf(`# S9 repair — %s

Read the original professional question %s and its actual source materials.
This brief supersedes any legacy workspace/activation instructions in that question;
use it only for professional questions, assigned units, assertions and source materials.
The private outputs below use top-level professional fields, without an answer envelope;
this brief defines the current output locations and format.
Use Claude Code native worktree isolation from committed Main HEAD containing prepared
baseline %s. Respect the actual dependency order. Before implementation,
perform actual prechecks and personally save the professional PlanReport at %s relative
to your actual native worktree. Write only plan, red_checks and proposed_paths in this
private JSON; the original Main-root question is read-only input, not a Worker write
location. red_checks records actual attempted prechecks with honest pass, fail or
blocked results; passing checks and unavailable reproductions do not require invented
RED. The selected prechecks may cover a subset; they do not define or reduce the
Result assertion set below, including when Main selects a RED experiment subset.
This workflow requires isolated Workers to keep edits in their actual
native worktree; Main owns changes to the shared Main checkout.
Continue your repair and real self-checks without waiting for a save receipt or activation.
Commit only product deliverables. Keep professional files out of Git.
Save your professional Result at %s relative to your actual native worktree:

`+"```json"+`
{"verdict":"","before_fix_results":[],"unit_results":[],"detection_gap_artifacts":[],"evidence_refs":[],"scope_deviations":[],"residual_risks":[],"migration_ref":"","rollback_ref":""}
`+"```"+`

Use verdict completed, blocked, stale or contract_conflict truthfully. before_fix_results
items have assertion_id, observed_red, evidence_refs. Include one item for every
assertion_id in this original RepairAssignment, in its original order:
%s
These IDs come directly from the issued packet's original RepairAssignment; consult
assignment %s in the original RepairPlan %s for that responsibility and the original
Contract in the question for assertion text. Include assertions already satisfied
before repair, as well as failed, unreproduced or blocked observations. observed_red
is a historical field name: write the truthful pre-repair observation and evidence,
without manufacturing a failure or omitting an already satisfied assertion.
unit_results items have unit_id,
status, evidence_refs, implementation_summary, restored_invariant_refs. Use the exact
assigned units/assertions from the question and actual evidence, never invented PASS.
unit_results.status is pass, fail or blocked; implementation_summary and observed_red
are strings. Every evidence_refs and restored_invariant_refs is an array of strings.
detection_gap_artifacts, top-level evidence_refs, scope_deviations and residual_risks
are arrays of strings, never artifact objects. migration_ref and rollback_ref are strings;
use "" if not applicable. Keep every shown field; use [] for an empty array. Do not add
machine metadata or extra nested fields. Record commands/observations in the professional
text fields and reference actual files as strings.
Local files in Result evidence_refs (top-level and per-observation) are relative
to your registered Worker root (or absolute within it); prefer path:logs/check.txt,
without authoring a hash.
Use path:check.log or ./check.log for a local bare filename; bare tokens without
path: or a slash remain opaque legacy IDs.
Reception captures these actual bytes for Main. Referenced local check outputs such as
logs/check.txt need no extra evidence commit; product deliverables keep their original
commit requirement. URI references remain unchanged;
missing local source files must be restored for reception.
Ordinary PreToolUse events and successful saves observed at PostToolUse automatically
attempt reception of these exact professional originals; SubagentStop is a fallback.
A complete Plan can be received while implementation continues and Result is unfinished.
Keep working without a submit command, done marker, or extra path-return action.
Saving a private Plan/Result does not itself establish reception.
Only the original consumer receipt confirms acceptance; Main can query
loop-harness runtime repair status --root <Main-root> for missing_reports and
missing_results. Keep the original responsibility and artifacts that still exist while
reception is pending. Do not recreate a cleaned source. Continuation after platform
cleanup requires complete source retention already registered by the system; failed
retention or unavailable material remains an actual source gap. This adds no Worker
seal, submission, or worktree-preservation action.
The system selects the original responsibility from this exact brief %s in the native
task and the actual Worker source. Reception feedback preserves the original responsibility,
exact artifact paths and consumer diagnostics; only its accepted receipt proves reception.
Main owns integration and necessary checks. No workspace
prepare/launch, session reservation, Team, generic readback, begin or activation.
`, p.Binding.RepairAssignmentID, filepath.Join(root, p.ViewPath), base, planPath, result, assertionIDs, assignment.AssignmentID, filepath.Join(root, p.Binding.PlanRef), filepath.Join(root, brief))
	if planPath != p.AnswerPath {
		text = strings.Replace(text, "\n\n", "\n\nPrivate submission contract version: 1. Both exact professional originals below must remain untracked and unignored until the original system cleanup.\n\n", 1)
		text = strings.Replace(text, "Continuation after platform\ncleanup requires complete source retention already registered by the system; failed\nretention or unavailable material remains an actual source gap.", "The system preserves the actual untracked professional originals until reception and\nnecessary successors finish. Ignored, missing or changed originals remain an actual source gap.", 1)
	}
	published, err := workpacket.PublishBounded(root, brief, []byte(text), 1<<20)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(published, []byte(text)) {
		return "", fmt.Errorf("original native S9 brief differs; preserve it")
	}
	return fmt.Sprintf("Main: use Claude Code native Agent with worktree isolation and carry this exact S9 brief: %s. Read-only professional question: %s; private PlanReport output relative to the actual native worktree: %s. Builder owns its professional plan, repair and Result; natural PreToolUse and successful save PostToolUse events receive exact artifacts automatically, with SubagentStop fallback. No submit, done marker or additional path-return action is required. Never ask the isolated Worker to write Main. No workspace prepare/launch or platform ID is required. ", filepath.Join(root, brief), filepath.Join(root, p.ViewPath), planPath), nil
}

func nativeS9Available(root string, state map[string]any) bool {
	if len(nativeS9PlanPackets(root, state)) > 0 {
		return true
	}
	pointer := repairPointerCLI(state)
	status := stringValue(pointer["status"])
	return stringValue(mapFieldCLI(state, "lifecycle")["state"]) == "bug_resolution" && s9ProfessionalPhase(stringValue(mapFieldCLI(state, "lifecycle")["phase"])) && (status == "targeted_reverification" || status == "blocked" && pointer["failure_route"] == "blocked" && len(repairArtifactRefs(pointer, "targeted_reverification_artifacts", "", "")) > 0)
}

func nativeS9Hook(root string, in policy.Input, out, diagnostic io.Writer) (bool, int) {
	snap, err := nativeRoutingSnapshot(root, in)
	if err != nil || !runtime.PacketEnabled(snap.State) || !nativeS9Available(root, snap.State) {
		return false, 0
	}
	natural := in.Event == "PreToolUse" || in.Event == "PostToolUse"
	// Ordinary child events identify the actor but point transcript_path at the
	// parent session. Resolve only that actor's actual native child transcript.
	if in.AgentID == "" || in.AgentID == "Main" {
		if in.Event == "PostToolUse" && (in.ToolName == "Agent" || in.ToolName == "Task") {
			return true, 0
		}
		return false, 0
	}
	if natural {
		child, ok := nativeChildTranscript(in)
		if !ok {
			return true, 0
		}
		in.AgentTranscriptPath = child
		feedback := &nativeS9WorkerFeedback{}
		in.Context = context.WithValue(contextForHook(in), nativeS9WorkerFeedbackKey{}, feedback)
		defer func() {
			if len(feedback.messages) > 0 {
				_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": strings.Join(feedback.messages, "\n")}})
			}
		}()
	}
	// Previously installed independent sessions keep their frozen recovery path.
	for _, raw := range mapFieldCLI(mapFieldCLI(snap.State, "workspace"), "executions") {
		e, _ := raw.(map[string]any)
		if e["agent_id"] == in.AgentID && stringValue(e["bootstrap_sha256"]) != "" {
			return false, 0
		}
	}
	if natural && (in.Error != "" || in.ToolResponse["is_error"] == true) {
		return true, 0
	}
	if in.Event != "SubagentStart" && in.Event != "SubagentStop" && !natural {
		return true, 0
	}
	if engine, err := policy.Load(filepath.Join(root, "docs/control/hook-policy.json")); err == nil {
		observed := policy.Decision{Decision: "audit", RuleID: "native_s9_lifecycle_observed", Reason: "native S9 lifecycle observed", Retry: "not_applicable"}
		auditStarted := time.Now()
		defer func() {
			observed.ElapsedMS = time.Since(auditStarted).Milliseconds()
			if err := appendHookAudit(contextForHook(in), root, engine.Envelope(in, observed, time.Now())); err != nil {
				fmt.Fprintln(diagnostic, err)
			}
		}()
	}
	if in.Event == "SubagentStart" {
		context := "Follow the exact S9 professional brief or independent Targeted answer reference in your native prompt. Save professional artifacts at their exact assigned paths and continue without activation or per-tool receipts. Natural PreToolUse and successful save PostToolUse events attempt reception, with SubagentStop fallback; no submit, done marker or additional path-return action is required. Feedback identifies the original responsibility, exact paths and original consumer outcome. Main owns integration/checks; actual native execution and resume belong to Claude Code. Unrelated native work keeps its own task."
		context += nativeS9TargetedStartContext(root, snap.State, in)
		_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": context}})
		return true, 0
	}
	packets := nativeS9PlanPackets(root, snap.State)
	var refs, targetedRefs []string
	targetedOwners := map[string]string{}
	for _, p := range packets {
		brief, _, _ := nativeS9Paths(p)
		refs = append(refs, filepath.Join(root, brief))
	}
	status := stringValue(repairPointerCLI(snap.State)["status"])
	if status == "targeted_reverification" || status == "blocked" {
		if selections, err := repairTargetedSelections(root, snap.State); err == nil {
			for _, selection := range selections {
				if ref, err := repairTargetedQuestionnairePath(root, snap.State, selection.performingAssignmentID); err == nil {
					targetedRefs = append(targetedRefs, ref)
					targetedOwners[ref] = selection.verifierOwner
				}
			}
		}
	}
	refs = append(refs, targetedRefs...)
	text := nativeReviewTaskText(in, refs)
	var selected *workpacket.RepairPlanReportPacket
	for _, p := range packets {
		brief, _, _ := nativeS9Paths(p)
		if nativeS6ExactReference(text, filepath.Join(root, brief)) {
			if selected != nil {
				fmt.Fprintln(diagnostic, "S9 return has multiple exact briefs; Main must resolve it")
				return true, 0
			}
			p := p
			selected = &p
		}
	}
	// A verifier may quote an earlier Builder's brief as background material.
	// Multiple exact responsibilities are ambiguous even across S9 branches;
	// never consume the Builder merely because it was checked first.
	if selected != nil {
		for _, ref := range targetedRefs {
			if nativeS6ExactReference(text, ref) {
				fmt.Fprintln(diagnostic, "S9 return has both Builder and Targeted responsibility references; Main must resolve the handoff")
				return true, 0
			}
		}
	}

	key, owner, paths := "", "", []string{}
	var capture *nativeS9AttemptCapture
	if selected != nil {
		brief, pin, _ := nativeS9Paths(*selected)
		key, owner = selected.ID, selected.Binding.OwnerAgentID
		paths = []string{filepath.Join(root, brief), filepath.Join(root, pin)}
		capture = newNativeS9AttemptCapture(contextForHook(in), root, snap, key, owner, "Builder")
		capture.SetResponsibilityRef(filepath.Join(root, brief))
		if natural {
			err = receiveNativeS9BuilderEvent(root, snap.State, in, *selected, text, diagnostic, capture)
		} else {
			err = receiveNativeS9Builder(root, snap.State, in, *selected, text, diagnostic, capture)
		}
	} else {
		for _, ref := range targetedRefs {
			if nativeS6ExactReference(text, ref) {
				key, owner, paths = ref, targetedOwners[ref], []string{ref}
				break
			}
		}
		if key != "" {
			capture = newNativeS9AttemptCapture(contextForHook(in), root, snap, key, owner, "Targeted")
			capture.SetResponsibilityRef(key)
		}
		if key == "" {
			return true, 0
		}

		err = guardNativeS9TargetedSource(root, snap.State, in, text)
		if natural && err == nil {
			// A draft is an ordinary saved artifact, not a completed return.
			raw, readErr := os.ReadFile(key)
			var answer repair.TargetedReverificationRequest
			if readErr != nil || json.Unmarshal(raw, &answer) != nil || strings.TrimSpace(answer.Result) == "" {
				return true, 0
			}
		}
		if err == nil {
			err = receiveNativeS9Targeted(contextForHook(in), root, snap, text, diagnostic, capture)
		}
	}
	if err == nativeS9ProgressPending {
		return true, 0
	}
	if capture != nil {
		if publishErr := capture.Publish(contextForHook(in), err); publishErr != nil {
			fmt.Fprintln(diagnostic, "S9 reception diagnostic could not be delivered:", publishErr)
		}
	}
	if err != nil {
		// The original read already captured these coordinates. Preserve them
		// in the Stop channel too, without resolving the source a second time.
		if capture != nil {
			known := map[string]bool{}
			for _, path := range paths {
				known[path] = true
			}
			var captured []string
			for path := range capture.value.Files {
				captured = append(captured, path)
			}
			captured = append(captured, capture.value.Attempt.InputRef, capture.value.Attempt.TypedInputRef)
			sort.Strings(captured)
			for _, path := range captured {
				if path != "" && !known[path] {
					paths = append(paths, path)
					known[path] = true
				}
			}
		}
		if natural {
			var completion *repairExitFailure
			if !errors.As(err, &completion) {
				nativeS9WorkerMessage(contextForHook(in), fmt.Sprintf("S9 reception for %s is incomplete. Original artifacts: %s. Original consumer diagnostic: %s. Preserve the originals; correct any identified professional fields within your assigned responsibility. System continuation remains with Main.", owner, strings.Join(paths, "; "), nativeFailureDetail(err)))
			}
		}
		return true, nativeS9HandoffFailure(owner, strings.Join(paths, "; "), err, diagnostic)
	}
	return true, 0
}

type nativeS9WorkerFeedbackKey struct{}
type nativeS9WorkerFeedback struct{ messages []string }

func nativeS9WorkerMessage(ctx context.Context, message string) {
	if feedback, ok := ctx.Value(nativeS9WorkerFeedbackKey{}).(*nativeS9WorkerFeedback); ok {
		feedback.messages = append(feedback.messages, message)
	}
}

var nativeS9ProgressPending = fmt.Errorf("native S9 professional work remains in progress")

// Natural tool events observe the actual checkout before the next tool runs.
// They never use a quoted Result in another worktree as source authority.
func receiveNativeS9BuilderEvent(root string, state map[string]any, in policy.Input, p workpacket.RepairPlanReportPacket, text string, diagnostic io.Writer, capture *nativeS9AttemptCapture) error {
	ctx := contextForHook(in)
	if in.CWD == "" {
		return nativeS9ProgressPending
	}
	source, err := workspace.Git(ctx, in.CWD, "rev-parse", "--show-toplevel")
	if err != nil {
		return nativeS9ProgressPending
	}
	source, err = workspace.Canonical(source)
	if err != nil {
		return err
	}
	main, err := workspace.Canonical(root)
	if err != nil {
		return err
	}
	if source == main {
		return nativeS9ProgressPending
	}
	if err := workspace.ValidateCheckout(root, source); err != nil {
		return err
	}
	plan, result, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		return err
	}
	// An explicit returned coordinate must agree with the actual event CWD.
	selectedSource, err := nativeS9BuilderSource(root, in, text, result)
	if err != nil {
		return err
	}
	cwd, err := workspace.CanonicalProspective(in.CWD)
	if err != nil {
		return err
	}
	if selectedSource != source && selectedSource != cwd {
		return fmt.Errorf("S9 event CWD and exact Result reference identify different sources")
	}
	originalExecution := false
	if registry, err := workspace.Decode(state); err != nil {
		return err
	} else if registry != nil {
		execution, exists := registry.Executions[p.Binding.ManifestAssignmentID]
		originalExecution = exists && execution.Purpose == workspace.NativeDeliveryPurpose && execution.BootstrapSHA256 == "" && execution.PlatformSessionID == ""
		for id, execution := range registry.Executions {
			if execution.Path == source && (id != p.Binding.ManifestAssignmentID || execution.AgentID != p.Binding.OwnerAgentID) {
				return fmt.Errorf("actual S9 checkout belongs to another responsibility")
			}
		}
	}
	// Receive the Plan independently; no Result, Git commit, or execution slot
	// is fabricated while the Builder continues its implementation.
	service := (&submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}}).WithContext(ctx)
	// Select the original receipt/typed attempt before reading mutable author
	// files. Accepted Candidates/Finals only project verified facts; an unfinished
	// typed attempt resumes its original Writer and must not be adapted again.
	// This is recovery, not a read-only probe: retained historical sources may
	// publish their original typed input through ReadRepairResultInput.
	if originalExecution {
		if handled, err := recoverNativeS9TypedResult(ctx, service, state, p, source, true, true, diagnostic, capture); handled || err != nil {
			return err
		}
	}
	receipt, err := service.RepairPlanReportReceipt(p.Binding.OwnerAgentID, p.ID)
	if err != nil {
		return err
	}
	if !receipt.Accepted {
		raw, readErr := workpacket.ReadFile(source, plan, 20<<20)
		if os.IsNotExist(readErr) || readErr == nil && !json.Valid(raw) {
			return nativeS9ProgressPending
		}
		if readErr != nil {
			return readErr
		}
		if plan != p.AnswerPath {
			status, err := workspace.GitBytes(ctx, source, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", plan)
			if err != nil || string(status) != "?? "+plan+"\x00" {
				return fmt.Errorf("native private Plan original must remain untracked and unignored")
			}
		}
		if err := importNativeS9PlanReportBytes(ctx, root, source, p, raw, diagnostic, capture); err != nil {
			return err
		}
		receipt, err = service.SaveRepairPlanReportFrom(ctx, p.Binding.OwnerAgentID, filepath.Join(root, p.AnswerPath), root, nil)
		if err != nil {
			return err
		}
		if !receipt.Accepted {
			return fmt.Errorf("original PlanReport was not accepted: %s", receipt.Error)
		}
		nativeS9WorkerMessage(ctx, fmt.Sprintf("S9 PlanReport for %s was accepted by the original consumer from %s. Continue your assigned repair and self-checks; save Result at %s when ready.", p.Binding.OwnerAgentID, filepath.Join(source, plan), filepath.Join(source, result)))
	}
	raw, err := workpacket.ReadFile(source, result, 20<<20)
	var answer workpacket.RepairResultProfessionalAnswer
	if os.IsNotExist(err) || err == nil && (json.Unmarshal(raw, &answer) != nil || strings.TrimSpace(answer.Verdict) == "") {
		return nativeS9ProgressPending
	}
	if err != nil {
		return err
	}
	return receiveNativeS9BuilderFrom(ctx, root, state, p, source, false, diagnostic, capture)
}

// A positively identified isolated checkout cannot borrow the new shared
// Targeted original. CWD absence is not source proof and retains legacy routing.
func guardNativeS9TargetedSource(root string, state map[string]any, in policy.Input, text string) error {
	if in.CWD == "" {
		return nil
	}
	selections, err := repairTargetedSelections(root, state)
	if err != nil {
		return err
	}
	for _, selection := range selections {
		path, err := repairTargetedQuestionnairePath(root, state, selection.performingAssignmentID)
		if err != nil {
			return err
		}
		if !nativeS6ExactReference(text, path) {
			continue
		}
		shared, err := repair.UsesSharedTargetedAuthoring(root, state, selection.originalAssignmentID, selection.performingAssignmentID, selection.verifierOwner)
		if err != nil {
			return err
		}
		if !shared {
			continue
		}
		if err := nativeSharedCheckoutSourceError(contextForHook(in), root, root, in.CWD, "S9 Targeted"); err != nil {
			return err
		}
	}
	return nil
}

func nativeS9TargetedStartContext(root string, state map[string]any, in policy.Input) string {
	status := stringValue(repairPointerCLI(state)["status"])
	if status != "targeted_reverification" && status != "blocked" {
		return ""
	}
	selections, err := repairTargetedSelections(root, state)
	if err != nil {
		return ""
	}
	var paths []string
	for _, selection := range selections {
		if path, err := repairTargetedQuestionnairePath(root, state, selection.performingAssignmentID); err == nil {
			paths = append(paths, path)
		}
	}
	text := nativeReviewTaskText(in, paths)
	for _, path := range paths {
		if nativeS6ExactReference(text, path) {
			return repairStopConditionsContext(root, state)
		}
	}
	return ""
}

// Resolve only the actual returned Result reference or platform CWD.
func nativeS9BuilderSource(root string, in policy.Input, text, result string) (string, error) {
	source := in.CWD
	listing, err := workspace.Git(contextForHook(in), root, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	matched := ""
	for _, line := range strings.Split(listing, "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		candidate := strings.TrimPrefix(line, "worktree ")
		if nativeS6ExactReference(text, filepath.Join(candidate, result)) {
			if matched != "" && matched != candidate {
				return "", fmt.Errorf("multiple actual Result roots; Main must resolve the handoff")
			}
			matched, source = candidate, candidate
		}
	}
	source, err = workspace.CanonicalProspective(source)
	if err != nil {
		return "", err
	}
	return source, nil
}

func receiveNativeS9Builder(root string, state map[string]any, in policy.Input, p workpacket.RepairPlanReportPacket, text string, diagnostic io.Writer, captures ...*nativeS9AttemptCapture) error {
	_, result, pathErr := nativeS9AuthorPaths(root, p)
	if pathErr != nil {
		return pathErr
	}
	// A cleaned accepted source no longer appears in git worktree list. Its
	// exact returned reference may still select the original frozen receipt.
	if registry, err := workspace.Decode(state); err == nil && registry != nil {
		if execution, ok := registry.Executions[p.Binding.ManifestAssignmentID]; ok && execution.Purpose == workspace.NativeDeliveryPurpose && (execution.Status == "complete" || repair.NativeS9SourceReference(execution).Path != "") && nativeS6ExactReference(text, filepath.Join(execution.Path, result)) {
			return receiveNativeS9BuilderFrom(contextForHook(in), root, state, p, execution.Path, false, diagnostic, nativeS9Capture(captures))
		}
	}
	source, err := nativeS9BuilderSource(root, in, text, result)
	if err != nil {
		return err
	}
	ctx := contextForHook(in)
	if in.Event == "SubagentStop" && in.AgentID != "" && in.AgentID != "Main" {
		ctx = context.WithValue(ctx, nativeS9StopActorKey{}, in.AgentID)
	}
	return receiveNativeS9BuilderFrom(ctx, root, state, p, source, false, diagnostic, nativeS9Capture(captures))
}

// Stop and explicit Main recovery share the original adapter and consumers.
func receiveNativeS9BuilderFrom(ctx context.Context, root string, state map[string]any, p workpacket.RepairPlanReportPacket, source string, recovery bool, diagnostic io.Writer, capture *nativeS9AttemptCapture) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	originalExecution := false
	if registry, err := workspace.Decode(state); err == nil && registry != nil {
		if execution, exists := registry.Executions[p.Binding.ManifestAssignmentID]; exists {
			originalExecution = execution.Purpose == workspace.NativeDeliveryPurpose && execution.BootstrapSHA256 == "" && execution.PlatformSessionID == ""
		}
		if execution, exists := registry.Executions[p.Binding.ManifestAssignmentID]; exists && execution.Purpose == workspace.NativeDeliveryPurpose && execution.Status == "complete" {
			if execution.Path != source || execution.AgentID != p.Binding.OwnerAgentID || execution.RuntimeID != p.Binding.RuntimeID || execution.BaselineGeneration != p.Binding.BaselineGeneration {
				return fmt.Errorf("completed native S9 source differs from the original responsibility")
			}
			plan, _, err := nativeS9AuthorPaths(root, p)
			if err != nil {
				return err
			}
			if plan != p.AnswerPath {
				if err := validateCompletedNativeS9Private(ctx, root, state, p, execution); err != nil {
					return err
				}
			}
			fmt.Fprintln(diagnostic, "Native S9 responsibility already integrated and cleaned; preserve the original receipt.")
			return nil
		}
	}
	_, pin, _ := nativeS9Paths(p)
	_, result, pathErr := nativeS9AuthorPaths(root, p)
	if pathErr != nil {
		return pathErr
	}
	if capture != nil {
		capture.SetInput(filepath.Join(source, result))
		capture.SetStage("source_selected")
	}
	if source == root {
		return fmt.Errorf("return the actual native worktree Result, not Main")
	}
	service := (&submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}}).WithContext(ctx)
	if recovery || originalExecution {
		// Accepted originals belong to their receipt, including after cleanup
		// removed the private authoring copies. The recovery consumer validates
		// any remaining originals before projecting that existing receipt. Stop
		// resumes an existing native attempt here; first Stop still captures its
		// source/actor through the original full reception below.
		if handled, err := recoverNativeS9TypedResult(ctx, service, state, p, source, true, !recovery, diagnostic, capture); handled || err != nil {
			return err
		}
	}
	planPath, _, pathErr := nativeS9AuthorPaths(root, p)
	if pathErr != nil {
		return pathErr
	}
	if planPath != p.AnswerPath {
		for _, path := range []string{planPath, result} {
			status, err := workspace.GitBytes(ctx, source, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", path)
			if err != nil || string(status) != "?? "+path+"\x00" {
				return fmt.Errorf("native private original must be exactly untracked, without ignore or staged changes: %s", filepath.Join(source, path))
			}
		}
	}

	retained, err := nativeS9RetainedSource(ctx, root, state, p, source)
	if err != nil {
		return err
	}
	if retained == nil {
		if err := workspace.ValidateCheckout(root, source); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var professional []byte
	if retained != nil {
		professional = retained.Result
	} else {
		professional, err = workpacket.ReadFile(source, result, 20<<20)
	}
	if err != nil {
		if capture != nil {
			capture.ObserveInputError(filepath.Join(source, result), err)
		}
		return nativeArtifactReadError("read professional Result", err)
	}
	if capture != nil {
		capture.ObserveInput(filepath.Join(source, result), professional)
		capture.SetStage("flat_result_read")
	}
	if capture != nil {
		capture.EnterConsumer("flat_result_consumer")
	}
	var verdict workpacket.RepairResultProfessionalAnswer
	if err := workpacket.ValidateUniqueJSON(professional); err != nil {
		return nativeS9ArtifactAuthorError("Result", filepath.Join(source, result), err)
	}
	if err := json.Unmarshal(professional, &verdict); err != nil {
		return nativeS9ArtifactAuthorError("Result", filepath.Join(source, result), err)
	}
	if capture != nil {
		capture.ConsumerSucceeded()
	}
	sourceBase := ""
	prepareSource := func(allowRetention bool) error {
		var saved struct {
			Base             string `json:"base_commit"`
			RetentionVersion int    `json:"source_retention_version"`
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := workpacket.ReadFile(root, pin, 4096)
		if err != nil {
			if capture != nil {
				capture.ObserveInputError(filepath.Join(root, pin), err)
			}
			return fmt.Errorf("read original native source pin %s: %w", filepath.Join(root, pin), err)
		}
		if capture != nil {
			capture.ObserveInput(filepath.Join(root, pin), data)
		}
		if err := json.Unmarshal(data, &saved); err != nil || saved.Base == "" {
			return fmt.Errorf("original native source pin missing")
		}
		base := saved.Base
		// A dependency may advance Main between professional preparation and the
		// native worktree's actual creation. Observe its common Git base at first
		// return, then preserve that base for every retry/integration of this source.
		if registry, err := workspace.Decode(state); err != nil {
			return err
		} else if registry != nil && registry.Executions[p.Binding.ManifestAssignmentID].Purpose == workspace.NativeDeliveryPurpose {
			base = registry.Executions[p.Binding.ManifestAssignmentID].BaseCommit
		} else {
			mainHead, err := workspace.Git(ctx, root, "rev-parse", "HEAD")
			if err != nil {
				return err
			}
			base, err = workspace.Git(ctx, source, "merge-base", "HEAD", mainHead)
			if err != nil {
				return err
			}
		}
		if _, err := workspace.Git(ctx, source, "merge-base", "--is-ancestor", saved.Base, base); err != nil {
			return fmt.Errorf("native Git source does not contain the prepared baseline")
		}
		var retainedRef repair.ArtifactRef
		if allowRetention && retained == nil && saved.RetentionVersion == 1 {
			actor, _ := ctx.Value(nativeS9StopActorKey{}).(string)
			if actor != "" && actor != "Main" {
				retainedRef, err = nativeS9CaptureSource(ctx, root, state, p, source, base, actor, saved.Base, result, professional)
				if err != nil {
					return err
				}
			}
		}
		sourceBase = base
		if retainedRef.Path != "" {
			if err := recordNativeS9Source(ctx, root, source, base, p, retainedRef); err != nil {
				return err
			}
		}
		return nil
	}
	sourcePrepared := false
	if retained == nil {
		beforePlan, err := nativeS9PrePlanRetentionEligible(ctx, root, state, p, source, verdict.Verdict)
		if err != nil {
			return err
		}
		if beforePlan {
			if err := prepareSource(true); err != nil {
				return err
			}
			sourcePrepared = true
		}
	}
	// Source registration precedes Plan acceptance. Refresh so all later
	// adapters use the same durable source anchor rather than the old snapshot.
	latestSource, err := runtime.NewStore(service.StatePath, service.JournalPath).WithContext(ctx).Snapshot()
	if err != nil {
		return err
	}
	state = latestSource.State
	retained, err = nativeS9RetainedSource(ctx, root, state, p, source)
	if err != nil {
		return err
	}
	planAccepted := false
	if recovery || retained != nil {
		planReceipt, err := service.RepairPlanReportReceipt(p.Binding.OwnerAgentID, p.ID)
		if err != nil {
			return err
		}
		planAccepted = planReceipt.Accepted
	}
	if !planAccepted {
		var planErr error
		if retained != nil {
			planErr = importNativeS9PlanReportBytes(ctx, root, source, p, retained.Plan, diagnostic, capture)
		} else {
			planErr = importNativeS9PlanReport(ctx, root, source, p, service, diagnostic, capture)
		}
		if err := planErr; err != nil {
			return err
		}
		if capture != nil {
			capture.EnterConsumer("plan_report_consumer")
		}
		if receipt, err := service.SaveRepairPlanReportFrom(ctx, p.Binding.OwnerAgentID, filepath.Join(root, p.AnswerPath), root, nil); err != nil {
			return err
		} else if !receipt.Accepted {
			return fmt.Errorf("original PlanReport was not accepted: %s", receipt.Error)
		}
	}
	if capture != nil {
		capture.ConsumerSucceeded()
		capture.SetStage("plan_received")
	}
	if retained == nil {
		if !sourcePrepared {
			if err := prepareSource(false); err != nil {
				return err
			}
		}
		if err := recordNativeS9Source(ctx, root, source, sourceBase, p); err != nil {
			return err
		}
	}
	// Keep the first typed adaptation and the original consumer in the same
	// workspace transaction. The lock is reentrant for the original issuer and
	// Result consumer through this context.
	ctx, unlock, err := integration.LockWorkspace(ctx, root)
	if err != nil {
		return err
	}
	defer unlock()
	beforeIssue, err := runtime.NewStore(service.StatePath, service.JournalPath).WithContext(ctx).Snapshot()
	if err != nil {
		return err
	}
	// A concurrent event may have issued or received this Result after this
	// event's routing snapshot. Reuse that exact attempt before any adaptation.
	if handled, err := recoverNativeS9TypedResult(ctx, service, beforeIssue.State, p, source, true, !recovery, diagnostic, capture); handled || err != nil {
		return err
	}
	packets, err := service.IssueRepairResults(ctx, p.Binding.OwnerAgentID)
	if err != nil {
		return err
	}
	for _, packet := range packets {
		if packet.Binding.Sources.RepairAssignmentID != p.Binding.RepairAssignmentID {
			continue
		}
		_, previouslyIssued, err := runtime.FindRepairResultPacketContext(runtime.WithPacketBodyRoot(ctx, root), beforeIssue.State, packet.ID)
		if err != nil {
			return err
		}
		// Only author-owned professional fields are transferred into the original
		// questionnaire. No platform identity or result metadata is model-authored.
		var answer workpacket.RepairResultProfessionalAnswer
		decoder := json.NewDecoder(bytes.NewReader(professional))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&answer); err != nil {
			return nativeS9ArtifactAuthorError("Result", filepath.Join(source, result), err)
		}
		draft := packet.Draft
		// Preserve explicit null/missing values for the original professional
		// validator; decoding into Go strings must not turn unknown into "".
		draft.Answer = append(json.RawMessage(nil), professional...)
		typedDiagnosticPath := filepath.Join(source, packet.AnswerPath)
		var originalDraft []byte
		if retained != nil {
			typedDiagnosticPath = filepath.Join(root, repair.NativeS9TypedInputPath(retained, packet))
		}
		if capture != nil {
			capture.SetTypedInput(typedDiagnosticPath)
			capture.SetStage("typed_result_prepared")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		typedBytes, err := json.MarshalIndent(draft, "", "  ")
		if err != nil {
			return err
		}
		typedBytes = append(typedBytes, '\n')
		if retained != nil {
			registry, err := workspace.Decode(state)
			if err != nil {
				return err
			}
			fixed, err := repair.ReadRepairResultInput(ctx, root, state, packet, registry.Executions[p.Binding.ManifestAssignmentID])
			if err != nil {
				return err
			}
			if !bytes.Equal(fixed, typedBytes) {
				return runtime.ErrSubmissionConflict
			}
		} else {
			originalDraft, err = workpacket.ReadFile(source, packet.AnswerPath, 20<<20)
			if err != nil {
				return err
			}
			// The original issued draft is the only input this adaptation may
			// replace. Saved or damaged typed inputs retain their own recovery.
			if nativeS9TypedInputSaved(packet, originalDraft, root) {
				return fmt.Errorf("original saved typed S9 Result must be resumed before adapting the professional original")
			}
			if err := os.WriteFile(filepath.Join(source, packet.AnswerPath), typedBytes, 0o644); err != nil {
				return err
			}
		}
		if capture != nil {
			capture.ObserveInput(typedDiagnosticPath, typedBytes)
			capture.SetStage("typed_result_saved")
		}
		if capture != nil {
			capture.EnterConsumer("typed_result_consumer")
		}
		var previousReceipt submission.Receipt
		var previousReceiptErr error
		if _, naturalFeedback := ctx.Value(nativeS9WorkerFeedbackKey{}).(*nativeS9WorkerFeedback); naturalFeedback {
			previousReceipt, previousReceiptErr = service.RepairResultReceipt(p.Binding.OwnerAgentID, packet.ID)
		}
		receipt, err := service.SaveRepairResultFrom(ctx, p.Binding.OwnerAgentID, filepath.Join(source, packet.AnswerPath), source, nil)
		if err != nil {
			var rejected *repair.RepairResultBeforeCandidateError
			// Only this newly issued input can be proved to have no earlier
			// staged Candidate. Preserve every unknown or interrupted attempt.
			if retained == nil && !previouslyIssued && errors.As(err, &rejected) && ctx.Err() == nil {
				current, readErr := workpacket.ReadFile(source, packet.AnswerPath, 20<<20)
				if readErr != nil || !bytes.Equal(current, typedBytes) {
					return fmt.Errorf("%w; original typed Result changed before restoring its draft: %v", err, readErr)
				}
				if writeErr := os.WriteFile(filepath.Join(source, packet.AnswerPath), originalDraft, 0o644); writeErr != nil {
					return fmt.Errorf("%w; could not restore the original unaccepted typed draft: %v", err, writeErr)
				}
				if capture != nil {
					delete(capture.value.Files, typedDiagnosticPath)
					capture.SetTypedInput("")
					capture.SetStage("flat_result_consumer")
				}
			}
			return nativeS9FlatArtifactError("Result", filepath.Join(source, result), err)
		}
		if !receipt.Accepted {
			return fmt.Errorf("original RepairResult was not accepted: %s", receipt.Error)
		}
		if previousReceiptErr == nil && !previousReceipt.Accepted {
			nativeS9WorkerMessage(ctx, fmt.Sprintf("S9 Result for %s was accepted by the original consumer from %s. Your original submission is received. Main owns the subsequent integration and checks; keep the original artifacts available.", p.Binding.OwnerAgentID, filepath.Join(source, result)))
		}
		if capture != nil {
			capture.ConsumerSucceeded()
			capture.SetStage("result_received")
		}
		fmt.Fprintln(diagnostic, "Native S9 professional Result received; Main owns original integration and checks. "+receipt.NextAction)
		return nil
	}
	return fmt.Errorf("original Result consumer has no current responsibility for this Builder")
}

// Completed D replay retains the original Candidate coordinate. It never
// selects another current responsibility or upgrades a frozen C execution.
func validateCompletedNativeS9Private(ctx context.Context, root string, state map[string]any, plan workpacket.RepairPlanReportPacket, e workspace.Execution) error {
	if e.DeliveryRef == "" || e.DeliverySHA256 == "" {
		return fmt.Errorf("completed private source lacks its original Candidate identity")
	}
	raw, err := workpacket.ReadFile(root, e.DeliveryRef, 20<<20)
	if err != nil {
		return err
	}
	if workpacket.Digest(raw) != e.DeliverySHA256 {
		return fmt.Errorf("completed private source Candidate bytes differ from original reception")
	}
	var candidate repair.WorkspaceCandidate
	if err := json.Unmarshal(raw, &candidate); err != nil {
		return err
	}
	if candidate.Provenance == nil || candidate.AssignmentID != e.AssignmentID || candidate.RuntimeID != e.RuntimeID || candidate.BaselineGeneration != e.BaselineGeneration || candidate.ExecutionGeneration != e.Generation || candidate.BaseCommit != e.BaseCommit {
		return fmt.Errorf("completed private source Candidate responsibility differs")
	}
	packet, found, err := runtime.FindRepairResultPacketContext(runtime.WithPacketBodyRoot(ctx, root), state, candidate.Provenance.PacketID)
	if err != nil {
		return err
	}
	if !found || packet.Binding.Sources != plan.Binding {
		return fmt.Errorf("completed private source lacks its exact original Result packet")
	}
	_, err = repair.NativeS9PrivateProjection(ctx, root, packet, e, candidate, true)
	return err
}

// Import only this returned worktree's original professional payload. The
// shared questionnaire is a receiver input, never a Worker write location.
func importNativeS9PlanReport(ctx context.Context, root, source string, p workpacket.RepairPlanReportPacket, service *submission.Service, diagnostic io.Writer, captures ...*nativeS9AttemptCapture) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	planPath, _, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		return err
	}
	raw, err := workpacket.ReadFile(source, planPath, 20<<20)
	if err != nil {
		if capture := nativeS9Capture(captures); capture != nil {
			capture.ObserveInputError(filepath.Join(source, planPath), err)
		}
	}
	if os.IsNotExist(err) {
		receipt, receiptErr := service.RepairPlanReportReceipt(p.Binding.OwnerAgentID, p.ID)
		if receiptErr == nil && receipt.Accepted {
			return nil
		}
		return &nativeAuthorError{fmt.Errorf("actual native worktree PlanReport is missing at %s; Main's draft cannot substitute for the Worker artifact", filepath.Join(source, planPath))}
	}
	if err != nil {
		return fmt.Errorf("read actual native PlanReport at %s: %w", filepath.Join(source, planPath), err)
	}
	return importNativeS9PlanReportBytes(ctx, root, source, p, raw, diagnostic, captures...)
}

func importNativeS9PlanReportBytes(ctx context.Context, root, source string, p workpacket.RepairPlanReportPacket, raw []byte, diagnostic io.Writer, captures ...*nativeS9AttemptCapture) error {
	planPath, _, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		return err
	}
	if capture := nativeS9Capture(captures); capture != nil {
		capture.ObserveInput(filepath.Join(source, planPath), raw)
		capture.EnterConsumer("plan_report_consumer")
	}
	if err := workpacket.ValidateUniqueJSON(raw); err != nil {
		return &nativeAuthorError{fmt.Errorf("actual native PlanReport at %s: %w", filepath.Join(source, planPath), err)}
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return &nativeAuthorError{fmt.Errorf("actual native PlanReport at %s must be a JSON object: %v", filepath.Join(source, planPath), err)}
	}
	professional := json.RawMessage(raw)
	if answer, exists := object["answer"]; exists {
		for _, key := range []string{"plan", "red_checks", "proposed_paths"} {
			if _, mixed := object[key]; mixed {
				return &nativeAuthorError{fmt.Errorf("actual native PlanReport at %s mixes answer and professional fields", filepath.Join(source, planPath))}
			}
		}
		professional = answer
	}
	draft := p.Draft
	draft.Answer = professional
	data, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	answer, fields := workpacket.ValidateRepairPlanReportAnswer(p, data)
	if len(fields) != 0 {
		err := &workpacket.AnswerError{Label: "actual native PlanReport at " + filepath.Join(source, planPath), Fields: fields}
		if _, wrapped := object["answer"]; !wrapped {
			return nativeS9FlatArtifactError("PlanReport", filepath.Join(source, planPath), err)
		}
		return err
	}
	draft.Answer = answer
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := persistRepairLeadQuestionnaire(filepath.Join(root, p.AnswerPath), draft); err != nil {
		return err
	}
	fmt.Fprintf(diagnostic, "Imported original professional PlanReport from actual native worktree: %s\n", filepath.Join(source, planPath))
	return nil
}

// This records a returned Git source for the existing Result/integration consumers.
// It never creates a worktree, bootstrap, Claude session, launch lease or process.
func recordNativeS9Source(parent context.Context, root, source, base string, p workpacket.RepairPlanReportPacket, retainedRefs ...repair.ArtifactRef) error {
	ctx, unlock, err := integration.LockWorkspace(parent, root)
	if err != nil {
		return err
	}
	defer unlock()
	store := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{}).WithContext(ctx)
	snap, err := store.Snapshot()
	if err != nil {
		return err
	}
	registry, err := workspace.Decode(snap.State)
	if err != nil {
		return err
	}
	if registry == nil {
		registry, err = workspace.New(ctx, root)
		if err != nil {
			return err
		}
	}
	if err := registry.Validate(ctx, root); err != nil {
		return err
	}
	b := p.Binding
	hasRetention := len(retainedRefs) > 0 && retainedRefs[0].Path != ""
	if old, ok := registry.Executions[b.ManifestAssignmentID]; ok && repair.NativeS9SourceReference(old).Path != "" {
		hasRetention = true
	}
	privatePlan, _, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		return err
	}
	hasPrivate := privatePlan != p.AnswerPath
	if hasRetention || hasPrivate {
		if err := runtime.VerifyRepairPlanReportPacketIssueContext(ctx, root, snap.State, p); err != nil {
			return err
		}
		if err := runtime.VerifyRepairPlanReportCurrent(root, snap.State, p, ctx); err != nil {
			return err
		}
	}

	if stringMapCLI(repairPointerCLI(snap.State)["assignment_owners"])[b.RepairAssignmentID] != b.OwnerAgentID || snap.State["runtime_id"] != b.RuntimeID || workspace.Generation(snap.State) != b.BaselineGeneration {
		return fmt.Errorf("native S9 responsibility changed before reception")
	}
	if old, exists := registry.Executions[b.ManifestAssignmentID]; exists {
		if old.AgentID == b.OwnerAgentID && old.RuntimeID == b.RuntimeID && old.BaselineGeneration == b.BaselineGeneration && old.Path == source && old.BaseCommit == base && old.BootstrapSHA256 == "" && old.PlatformSessionID == "" {
			if len(retainedRefs) > 0 && retainedRefs[0].Path != "" && (repair.NativeS9SourceReference(old).Path != retainedRefs[0].Path || repair.NativeS9SourceReference(old).SHA256 != retainedRefs[0].SHA256) {
				return runtime.ErrSubmissionConflict
			}
			if fixed, err := repair.RetainedNativeS9Source(ctx, root, old, b); err != nil {
				return err
			} else if fixed != nil {
				return nil
			}
			return registry.ValidateExecution(ctx, old, false)
		}
		return fmt.Errorf("an existing frozen source differs; Main must preserve and resolve it")
	}
	branch, err := workspace.Git(ctx, source, "branch", "--show-current")
	if err != nil || branch == "" {
		return fmt.Errorf("native source needs its actual Git branch")
	}
	plan, err := repairDispatchPlan(root, snap.State)
	if err != nil {
		return err
	}
	var scope []string
	for _, a := range plan.Assignments {
		if a.AssignmentID == b.RepairAssignmentID {
			scope = a.Scope
		}
	}
	if len(scope) == 0 {
		return fmt.Errorf("current repair scope missing")
	}
	e := workspace.Execution{Purpose: workspace.NativeDeliveryPurpose, AssignmentID: b.ManifestAssignmentID, RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, Generation: 1, AgentID: b.OwnerAgentID, Path: source, Branch: branch, TargetBranch: registry.Branch, BaseCommit: base, Status: "ready", WritePaths: scope, Checks: []string{}, Inputs: map[string]string{}, CheckLocation: "main"}
	if len(retainedRefs) > 0 && retainedRefs[0].Path != "" {
		e.Inputs[retainedRefs[0].Path] = retainedRefs[0].SHA256
	} else {
		_, pin, _ := nativeS9Paths(p)
		raw, err := workpacket.ReadFile(root, pin, 4096)
		if err != nil {
			return err
		}
		var saved struct {
			Version int `json:"private_submission_version"`
		}
		if err := json.Unmarshal(raw, &saved); err != nil {
			return err
		}
		if saved.Version == 1 {
			e.Inputs[pin] = workpacket.Digest(raw)
		}
	}
	if err := registry.ValidateExecution(ctx, e, false); err != nil {
		return err
	}
	if _, err := repair.RetainedNativeS9Source(ctx, root, e, b); err != nil {
		return err
	}
	registry.Executions[e.AssignmentID] = e
	key := "native-s9-source-" + p.ID
	_, err = store.Update(snap.Revision, runtime.Mutation{EventID: key, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "orchestrator", RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, IdempotencyKey: key, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Message: "record returned native S9 Git source", Apply: func(state map[string]any) error {
		if hasRetention || hasPrivate {
			if err := runtime.VerifyRepairPlanReportCurrent(root, state, p, ctx); err != nil {
				return err
			}
		}
		if _, err := repair.RetainedNativeS9Source(ctx, root, e, b); err != nil {
			return err
		}
		state["workspace"] = workspace.Encode(registry)
		return nil
	}})
	return err
}

func receiveNativeS9Targeted(ctx context.Context, root string, snap runtime.Snapshot, text string, diagnostic io.Writer, captures ...*nativeS9AttemptCapture) error {
	status := stringValue(repairPointerCLI(snap.State)["status"])
	if status != "targeted_reverification" && status != "blocked" {
		return nil
	}
	selections, err := repairTargetedSelections(root, snap.State)
	if err != nil {
		return err
	}
	var chosen *repairTargetedSelection
	var path string
	for _, selection := range selections {
		ref, err := repairTargetedQuestionnairePath(root, snap.State, selection.performingAssignmentID)
		if err != nil {
			return err
		}
		if nativeS6ExactReference(text, ref) {
			if chosen != nil {
				return fmt.Errorf("multiple Targeted responsibility references; Main must resolve it")
			}
			selection := selection
			chosen, path = &selection, ref
		}
	}
	if chosen == nil {
		return nil
	}
	// Keep the original independent-verification rules; native IDs are observed
	// in the audit envelope, never substituted for the engineering verifier owner.
	answer, err := readRepairTargetedAnswerContext(ctx, path, nativeS9Capture(captures))
	if err != nil {
		return err
	}
	var feedback bytes.Buffer
	if capture := nativeS9Capture(captures); capture != nil {
		capture.EnterConsumer("targeted_consumer")
	}
	_, code, exitFailure := saveRepairTargetedAnswerContext(ctx, root, snap, path, *chosen, answer, &feedback, nativeS9Capture(captures))
	var message struct {
		Output struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	_ = json.Unmarshal(feedback.Bytes(), &message)
	fmt.Fprintln(diagnostic, message.Output.Context)
	if code != 0 {
		return fmt.Errorf("original Targeted consumer: %s", feedback.String())
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	latest, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).WithContext(ctx).Snapshot()
	if err != nil {
		return err
	}
	pointer := repairPointerCLI(latest.State)
	for _, ref := range repairArtifactRefs(pointer, "targeted_reverification_artifacts", "", "") {
		var value repair.TargetedReverification
		if err := repairLeadIndexedSource(root, ref, &value); err != nil {
			continue
		}
		if value.ImpactID == chosen.impact.ImpactID && value.OriginalAssignmentID == chosen.originalAssignmentID && value.PerformingAssignmentID == chosen.performingAssignmentID {
			if err := repairLeadTargetedReplay(root, path, ref); err == nil {
				previouslyReceived := false
				for _, previous := range repairArtifactRefs(repairPointerCLI(snap.State), "targeted_reverification_artifacts", "", "") {
					if previous.Path == ref.Path && previous.SHA256 == ref.SHA256 {
						previouslyReceived = true
					}
				}
				if !previouslyReceived {
					nativeS9WorkerMessage(ctx, fmt.Sprintf("S9 independent Targeted observations for %s were accepted by the original consumer from %s. Your original verification is received. Main owns subsequent handoff and review continuation; preserve the original observations.", chosen.verifierOwner, path))
				}
				if capture := nativeS9Capture(captures); capture != nil {
					capture.SetStage("targeted_received")
				}
				return exitFailure
			}
		}
	}
	return fmt.Errorf("Targeted answer was not recorded: %s", message.Output.Context)
}
