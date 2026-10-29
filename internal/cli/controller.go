package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const loopManualRef = "loop-harness.md"

// loopManualFallbackRef is the alternate on-disk location for the agent-facing
// Manual (some harness generations install it under .claude/bin/). Guidance
// always names the primary path first and mentions the fallback only in
// recovery packets, so a stale install location can still be found.
const loopManualFallbackRef = ".claude/bin/loop-harness.md"

// buildGuidance is the positive side of the Hook control plane. It deliberately
// consumes projectNext/buildNextProjection, the same projection used by
// `status` and `next`, so a compacted Agent cannot be handed a second,
// slightly-different lifecycle interpretation.
// graphControlForHook observes current packet-mode professional work. S7
// committed handoffs retain their original transition cycle. S8 Case routing
// and S9 repair transitions remain with their original named consumers.
func graphControlForHook(root string, request policy.Input) (controller.ControlResult, bool) {
	switch request.Event {
	case "PreToolUse", "SessionStart", "SubagentStart", "PreCompact", "Stop", "SubagentStop", "TeammateIdle":
	default:
		return controller.ControlResult{}, false
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return controller.ControlResult{}, false
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if !runtime.PacketEnabled(snapshot.State) && lifecycle["state"] != "awaiting_human_release" && lifecycle["state"] != "inactive" {
		return controller.ControlResult{}, false
	}
	switch lifecycle["state"] {
	case "document_verification":
		if !mainQuestionnaireViewer(request) {
			return controller.ControlResult{}, false
		}
	case "planning":
		if !mainQuestionnaireViewer(request) {
			return controller.ControlResult{}, false
		}
		if lifecycle["phase"] != "design" && lifecycle["phase"] != "contracts" && lifecycle["phase"] != "tasks" {
			return controller.ControlResult{}, false
		}
	case "verification":
		if ptr := review.PlanPointerFromState(snapshot.State); ptr != nil {
			switch ptr.Status {
			case "clean", "observation_sealed", "paused":
				return controller.ControlResult{}, false
			}
		}
	case "inactive", "acceptance", "release_audit", "awaiting_human_release":
		// S10 reports and the S11 human wait only project current facts.
	case "bug_resolution":
		if lifecycle["phase"] != "investigation" && !s9ProfessionalPhase(stringValue(lifecycle["phase"])) {
			return controller.ControlResult{}, false
		}
	default:
		return controller.ControlResult{}, false
	}
	// PreToolUse guidance reads graph facts; completion proof checks belong
	// to the existing Stop/Idle cases, never to this ordinary observation.
	guidance := graphGuidance(root, snapshot.State, request)
	return controller.ControlResult{
		Snapshot: snapshot,
		Decision: policy.Decision{Decision: "allow", Guidance: &guidance},
	}, true
}

// Stop/Idle projections describe the round; the original stop handler checks
// the owner's responsibility separately, without a second proof in guidance.
func graphGuidance(root string, state map[string]any, request policy.Input) policy.Guidance {
	guidance := BuildGuidanceForState(root, state, "PreToolUse", request)
	guidance.Event = request.Event
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] == "document_verification" {
		applyS5ObservedGuidance(&guidance)
	} else if lifecycle["state"] == "verification" {
		applyS7RecoveryProjection(root, state, &guidance)
	} else {
		if runtime.PacketEnabled(state) && lifecycle["state"] == "bug_resolution" && lifecycle["phase"] == "investigation" {
			guidance.ReadOrder = s8QuestionnaireReadOrder(root, state, request)
			guidance.Read = append([]string(nil), guidance.ReadOrder...)
			guidance.Recovery = []string{"only if blocked: inspect the recorded source/preparation failure with Main", "optional recovery: .claude/loop-state.json", "optional recovery: " + guidance.ProtocolRef, "optional recovery: " + loopManualRef}
		}
		if runtime.PacketEnabled(state) && lifecycle["state"] == "bug_resolution" && s9ProfessionalPhase(stringValue(lifecycle["phase"])) && !s7BudgetGateRequired(state, root) {
			guidance.Missing = nil // Actual missing fields/owners come with the current exact questionnaire.
			guidance.ReadOrder = s9QuestionnaireReadOrder(root, state, request)
			guidance.Read = append([]string(nil), guidance.ReadOrder...)
			guidance.Recovery = []string{"only if blocked: inspect the recorded source/preparation failure with Main", "optional recovery: .claude/loop-state.json", "optional recovery: " + guidance.ProtocolRef, "optional recovery: " + loopManualRef}
			if owner := request.EffectiveAgentID(); owner != "" && owner != "Main" {
				guidance.Action = "continue the original S9 responsibility using its exact professional questionnaire delivered below"
				status := stringValue(repairPointerCLI(state)["status"])
				if lifecycle["phase"] == "planning" || lifecycle["phase"] == "reproducing" || lifecycle["phase"] == "fixing" && (status == "repairing" || status == "blocked") {
					guidance.Action += "; " + s9ListenerKnownPaths(root, state, owner, lifecycle["phase"] == "fixing")
				}
			} else if !mainQuestionnaireViewer(request) || owner == "" && (request.AgentType != "" || request.AgentTranscriptPath != "" || request.Event == "SubagentStop" || request.Event == "TeammateIdle") {
				guidance.Action = "Main must supply this native Worker's exact current S9 questionnaire path in its actual task prompt; this event did not identify its registered owner"
			}
		}
		if lifecycle["state"] == "bug_resolution" && lifecycle["phase"] == "investigation" && (request.Event == "SessionStart" || request.Event == "PreCompact") {
			applyS8EntryProjection(state, &guidance)
		}
	}
	return guidance
}

// Normal S8 reading starts with the current professional questions and actual
// source references. This projection neither prepares work nor identifies an
// unknown native owner from its role.
func s8QuestionnaireReadOrder(root string, state map[string]any, request policy.Input) []string {
	var order []string
	appendPath := func(ref string) {
		if ref != "" {
			order = appendUnique(order, resolveRootPath(root, ref))
		}
	}
	owner := request.EffectiveAgentID()
	main := mainQuestionnaireViewer(request)
	entities := mapFieldCLI(state, "entities")
	var packetInputs []string
	if main {
		for _, path := range investigationLeadQuestionnairePaths(root, state) {
			appendPath(path)
		}
	} else if owner != "" && s8RegisteredInvestigator(state, owner, "") {
		for _, row := range objectSliceAny(entities["work_packets"]) {
			binding := mapFieldCLI(row, "binding")
			if row["template_id"] != "s8-hypothesis-result-v1" || binding["owner_agent_id"] != owner || binding["runtime_id"] != state["runtime_id"] || binding["case_id"] != investigationPointer(state)["case_id"] ||
				tolerantInt(binding["baseline_generation"]) != tolerantInt(mapFieldCLI(state, "baseline")["generation"]) || tolerantInt(binding["review_round"]) != tolerantInt(mapFieldCLI(state, "review")["round"]) {
				continue
			}
			source, err := questionnaireSourceRoot(root, state, stringValue(binding["assignment_id"]), owner, stringValue(binding["runtime_id"]), tolerantInt(binding["baseline_generation"]))
			if err != nil {
				continue
			}
			for _, ref := range []string{stringValue(row["view_path"]), stringValue(row["answer_path"])} {
				if ref != "" {
					order = appendUnique(order, filepath.Join(source, filepath.FromSlash(ref)))
				}
			}
			for _, input := range objectSliceAny(mapFieldCLI(row, "draft")["read"]) {
				packetInputs = append(packetInputs, stringValue(input["path"]))
			}
		}
	}
	if len(order) == 0 {
		if main {
			order = append(order, "Main/system must prepare the exact current Main questionnaire path shown by the listener")
		} else {
			order = append(order, "use only the exact professional questionnaire paths supplied by Main; an unresolved native owner is not another registered owner")
		}
	}
	batchRef, _ := investigationLeadBatchReference(state)
	appendPath(batchRef)
	batch := mapFieldCLI(mapFieldCLI(state, "review"), "observation_batch")
	for _, id := range stringSliceAny(batch["finding_ids"]) {
		for _, finding := range objectSliceAny(entities["findings"]) {
			if finding["finding_id"] == id {
				appendPath(stringValue(finding["path"]))
			}
		}
	}
	appendPath(stringValue(investigationPointer(state)["path"]))
	appendPath(stringValue(mapFieldCLI(state, "bound_req")["path"]))
	for _, document := range objectSliceAny(state["documents"]) {
		if document["status"] == "locked" && tolerantInt(document["generation"]) == tolerantInt(mapFieldCLI(state, "baseline")["generation"]) {
			appendPath(stringValue(document["path"]))
		}
	}
	for _, ref := range packetInputs {
		appendPath(ref)
	}
	appendPath("docs/control/agent-protocol.md#s8")
	appendPath(".claude/skills/bug-resolution/SKILL.md")
	return order
}

func buildGuidance(root string, state map[string]any, event string, input policy.Input) policy.Guidance {
	if input.Event == "" {
		input.Event = event
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	lifecycleState, _ := lifecycle["state"].(string)
	lifecyclePhase, _ := lifecycle["phase"].(string)
	stage, skill, action := projectNext(lifecycleState, lifecyclePhase, root)
	next := buildNextProjection(state, stage, skill, action, root, isMainPlanningViewer(input))
	if lifecycleState == "bug_resolution" && lifecyclePhase == "investigation" {
		next.Action = investigationNextAction(root, state)
		if runtime.PacketEnabled(state) {
			// Professional missing fields and waiting owners are delivered with
			// the current questionnaires, without legacy BUG gate tokens.
			next.Missing = nil
			if owner := input.EffectiveAgentID(); owner != "" && owner != "Main" {
				next.Action = "continue the original Investigator responsibility; " + s8ListenerDiagnosticPaths(root, state, owner)
			}
		}
	}
	if lifecycleState == "bug_resolution" && lifecyclePhase != "investigation" {
		next.Action = repairNextAction(state, lifecyclePhase, root)
	}
	applyS7BudgetGateway(&next, state, root)

	guidance := policy.Guidance{
		RuntimeID:      stringValue(state["runtime_id"]),
		Revision:       integerValue(state["revision"]),
		Event:          event,
		Stage:          next.Stage,
		LifecycleState: lifecycleState,
		LifecyclePhase: lifecyclePhase,
		Objective:      next.Objective,
		Action:         next.Action,
		ProtocolRef:    next.ProtocolRef,
		ManualRef:      loopManualRef,
		PrimarySkill:   next.PrimarySkill,
		Read:           nonNilStrings(next.Read),
		ReadOrder:      recoveryReadOrder(root, next),
		Missing:        nonNilStrings(next.Missing),
		DoneWhen:       nonNilStrings(next.DoneWhen),
		Automation: []string{
			"do not repeat professional submissions or run a CLI as per-tool permission",
			"treat this Hook packet as the Controller checkpoint",
			"Main uses the exact preparation, dispatch, integration or recovery command when its current responsibility names one; read-only status is optional diagnostics, not an opening ritual",
		},
		HumanRequired: next.HumanRequired,
		Recovery:      []string{"continue from this packet's Stage and Next", "read " + next.ProtocolRef, "if blocked or unclear read " + loopManualRef},
	}
	if runtime.PacketEnabled(state) && lifecycleState == "planning" && (lifecyclePhase == "contracts" || lifecyclePhase == "tasks") {
		if row, err := registeredPlanningRow(state); err == nil && row != nil {
			guidance.Read = []string{stringValue(row["view_path"]), stringValue(row["answer_path"])}
			guidance.ReadOrder = append([]string{"LOOP RECOVERY packet (this message)"}, guidance.Read...)
			guidance.Recovery = []string{"continue the exact registered view/answer; read the linked professional method as needed; Runtime and repository-wide reading are for named diagnostics"}
		}
	}
	if lifecycleState == "inactive" {
		guidance.Missing = nil // Exact unanswered choices come from the current request.
		if next.Stage == "S0" {
			guidance.DoneWhen = []string{"the current requirement records the real user's goals, scope, constraints and acceptance criteria, resolves blocking decisions and faithfully records the human's actual lock of that version"}
			guidance.Automation = []string{"SessionStart supplies the actual requirement draft; ordinary Pre/Stop only read current work", "complete the current requirement and unresolved professional questions; save a draft normally and record locked only after the human's actual lock decision", "saving the actual locked requirement hands off to the original S1 preparation; no binding answers, machine metadata or startup choice are required to complete S0"}
			guidance.Recovery = []string{"read the exact current requirement draft below; report a missing draft or template as an entry preparation gap, without searching for internal binding files or editing Runtime"}
			guidance.ReadOrder = []string{"LOOP RECOVERY packet (this message)", next.ProtocolRef, "docs/requirements/", ".claude/skills/requirement-funnel/SKILL.md"}
		} else {
			guidance.DoneWhen = []string{"the actual locked/committed requirement and explicit startup choice are recorded by original TR-001, with current S2 paths supplied"}
			guidance.Automation = []string{"SessionStart or the actual requirement lock save prepares the original binding questions once", "ordinary Pre/Stop only read current work; complete the actual four binding answers and save for automatic original TR-001", "ordinary Git commits and branch choices remain Main's real work; no Hook commit, branch creation, pushing or publication"}
			guidance.Recovery = []string{"read the exact current binding-request path below; report missing preparation/materials to Main, without hand-editing Runtime or issuing a binding command"}
		}
	}
	if len(nativeS6Records(state)) > 0 {
		guidance.Automation = []string{
			"Main prepares reviewed TASK briefs with s6 prepare and delegates through Claude Code native Agent/worktree; Claude Code owns foreground, background and resume",
			"Builder saves its own professional plan and Result; native Start/Stop provide engineering context and receive delivery without activation or per-tool receipts",
			"Main explicitly uses original runtime task-integrate for checks, merge, ACK and cleanup",
		}
	}
	if lifecycleState == "bug_resolution" && lifecyclePhase != "investigation" {
		guidance.Automation = []string{
			"Main saves its own division/impact answers; native Workers save their original Plan/Result for PreToolUse observation, with successful PostToolUse and Stop/SubagentStop fallback through the original consumers",
			"Main saves the original Builder division and uses its exact prepared brief with Claude Code native Agent/worktree; a missing professional question is recovered through that same division save, not a workspace or session launcher",
			"Existing frozen registered executions retain their exact original recovery coordinates; native work never falls back to that startup protocol",
			"Main uses the exact current `runtime workspace integrate --root <Main-root> --assignment <execution-assignment-id> --agent <owner-agent-id>` command supplied with the S9 candidate, adding --check-command for each actual necessary Main check; do not substitute generic runtime task-integrate",
		}
	}
	applyMainPlanningAutomation(&guidance, next.mainPlanning)

	if lifecycleState == "paused" || lifecycleState == "awaiting_human_release" || lifecycleState == "aborted" {
		guidance.HumanRequired = true
		guidance.Blocked = true
		guidance.Blocker = pauseReason(state, lifecycleState)
		if lifecycleState == "awaiting_human_release" {
			guidance.Action = "present the current release materials and exact application to the human; after an actual explicit choice, faithfully fill disposition and optional reason in that application and save normally; the original listener records and follows its fixed exit automatically; empty disposition remains a waiting draft"
			guidance.Automation = []string{"ordinary S11 Pre/Stop only read current materials and the exact decision application", "after the human's actual choice, save disposition and optional reason normally; the original listener records and executes its fixed exit without a command", "empty disposition remains a draft; do not invent the human choice or publish/deploy"}
			guidance.Recovery = []string{"use the exact current application path shown below; missing material or application is a system preparation gap, not a manual registration step"}
		} else {
			guidance.Action = "stop automation and surface the human Gateway"
		}
	} else if lifecycleState == "release_authorized" {
		guidance.Action = "S11 human-authorized terminal; Harness performs no squash merge, publication, deployment, or formal release"
	}
	switch event {
	case "SubagentStart":
		addDelegationPreflight(&guidance, input)
		if input.Runtime.Agent != nil {
			approvalRequired := input.Runtime.Agent.DispatchMode == "plan_approval_required"
			questionnaire := input.Runtime.Agent.DocumentReviewAssignment || input.Runtime.Agent.ReviewAssignment || input.Runtime.Agent.HypothesisResultAssignment
			switch input.Runtime.Agent.LifecycleActivity() {
			case "spawned", "reading":
				if !approvalRequired {
					if questionnaire {
						guidance.Action = "read the supplied professional questionnaire and source paths, perform the assigned review, then fill and save that exact answer file"
					} else {
						guidance.Missing = appendUnique(guidance.Missing, "plan_report")
						guidance.Action = "read the supplied assignment and professional plan questions, fill and save the exact plan path, then continue the assigned work"
					}
					break
				}
				guidance.Blocked = true
				guidance.Blocker = "phase-one readback is not yet approved"
				guidance.Missing = appendUnique(guidance.Missing, "agent_readback")
				guidance.Action = "complete the assigned readback and wait for phase-two activation"
			case "understanding_submitted":
				if !approvalRequired {
					guidance.Automation = append(guidance.Automation, "PLAN_REPORT was submitted; the PostToolUse observer auto-activates the Agent, so continue without a second Main turn")
					break
				}
				guidance.Blocked = true
				guidance.Blocker = "phase-one readback is not yet approved"
				guidance.Missing = appendUnique(guidance.Missing, "agent_readback")
				guidance.Action = "complete the assigned readback and wait for phase-two activation"
			case "understanding_approved":
				if !approvalRequired {
					guidance.Automation = append(guidance.Automation, "plan_checkpoint does not require an approval turn; continue through the recorded checkpoint")
					break
				}
				guidance.Blocked = true
				guidance.Blocker = "phase-two activation has not been committed"
				guidance.Missing = appendUnique(guidance.Missing, "activation_envelope")
				guidance.Action = "commit or request the bounded phase-two activation before writing"
			}
		}
	case "PreToolUse":
		if input.ToolName == "Agent" || input.ToolName == "Task" {
			addDelegationPreflight(&guidance, input)
		}
	case "SubagentStop":
		if runtime.PacketEnabled(state) && ((lifecycleState == "bug_resolution" && s9ProfessionalPhase(lifecyclePhase)) || lifecycleState == "acceptance" || lifecycleState == "release_audit") {
			break // The current professional consumer and listener supply S9 facts.
		}
		guidance.Integration = []string{
			"inspect the subagent worktree and review its committed diff and required checks",
			"verify the task branch targets the REQ-bound development branch",
			"merge the reviewed worktree branch back into the REQ-bound development branch",
			"remove worktree only after the merge and checks succeed",
			"record completion_ack after integration into the bound branch; publication and release remain behind the human Gateway",
		}
		guidance.Automation = append(guidance.Automation,
			"re-wake the same Agent when its report is missing; do not silently spawn a replacement",
			"SubagentStop: Worker execution may end after its recorded Result; Main completes the worktree integration checklist before global completion",
		)
		// L4 §10.3 / §16.1: completion/blocker facts come from the
		// control plane (agent state + assignment report), not from a
		// self-injected `input.Facts["agent_report_complete"]` — the
		// official payload does not carry that flag, so reading it made
		// the Guidance block path fire on every SubagentStop and falsely
		// reported the worktree integration as blocked. The verdict is
		// fail-open: a missing/unreadable runtime defers to the platform
		// `exit 2` control (stopidle.go) for the hard block.
		reportComplete := resolveAgentReportComplete(root, input)
		if !reportComplete {
			guidance.Blocked = true
			guidance.Blocker = "subagent completion or blocker report is missing"
			guidance.Missing = appendUnique(guidance.Missing, "agent_completion_report")
			guidance.Action = "submit completion or blocker report before stopping"
		} else {
			guidance.Action = "Worker: follow your original execution close-out and await the original consumer. Main: consume the report; review and integrate a completed subagent worktree into the current development branch then acknowledge and clean it via runtime task-integrate"
		}
	case "TeammateIdle":
		if runtime.PacketEnabled(state) && ((lifecycleState == "bug_resolution" && s9ProfessionalPhase(lifecyclePhase)) || lifecycleState == "acceptance" || lifecycleState == "release_audit") {
			break // The current professional consumer and listener supply S9 facts.
		}
		guidance.Automation = append(guidance.Automation,
			"re-wake the same teammate with the current assignment envelope; do not spawn a replacement",
			"if the teammate is blocked, require a blocker report; if it reported, acknowledge it before scheduling the next legal action",
		)
		// L4 §10.2 / §16.1: idle facts come from the control plane — the
		// platform payload has no `facts.assignment_reported` field, so
		// the old code path produced a constant-true-blocked verdict on
		// every TeammateIdle. The verdict is fail-open: a missing/
		// unreadable runtime falls through to the stopidle.go real
		// platform control (which is the authoritative block path).
		reportComplete := resolveAgentReportComplete(root, input)
		if !reportComplete {
			guidance.Blocked = true
			guidance.Blocker = "the current-round assignment report is missing"
			guidance.Missing = appendUnique(guidance.Missing, "assignment_report")
			guidance.Action = "re-wake the same teammate, resume the assignment, and submit the current-round report"
		} else {
			guidance.Action = "Worker: follow your original execution close-out and await the original consumer. Main: consume the current teammate report and schedule the next legal assignment"
		}
	}

	if state["workspace"] != nil && (event == "SessionStart" || event == "PreCompact") {
		guidance.Automation = append(guidance.Automation, "recover outstanding worktree deliveries with runtime workspace pending --root <bound-main-root>; its command_argv entries derive from durable reports and checkpoints")
	}
	// L3-S7 §8: a SessionStart/PreCompact during the verification phase must
	// carry the S7-specific recovery projection (current round, assignment
	// buckets, unconsumed Results, Claim coverage gaps, single next action).
	if lifecycleState == "verification" && (event == "SessionStart" || event == "PreCompact") {
		applyS7RecoveryProjection(root, state, &guidance)
	}

	// S8 entry source line: when the lifecycle flipped into bug_resolution
	// via TR-008 (ObservationBatch handoff), the recovery packet must carry
	// the exact source fact — observation_batch id and finding count — so a
	// compacted Agent can re-bind to the right sealed batch without reading
	// the milestone log. Read from state.review.observation_batch.
	if lifecycleState == "bug_resolution" && (event == "SessionStart" || event == "PreCompact") {
		applyS8EntryProjection(state, &guidance)
		applyS9RepairProjection(root, state, &guidance)
	}

	// L3-S10 §9: acceptance and release audit are read-only audit work, not
	// an invitation to take the shortest route to S11. Keep the finite
	// denominator, counterevidence, and the S9→fresh-S7 prerequisite visible
	// on every normal recovery packet.
	if lifecycleState == "acceptance" || lifecycleState == "release_audit" {
		if mainQuestionnaireViewer(input) {
			if !s7BudgetGateRequired(state, root) {
				applyS10Guidance(&guidance)
				guidance.Action = stageAuditQuestionnaireContext(root, input, runtime.Snapshot{State: state})
			}
		} else {
			// The global cursor may advance while an original Reviewer returns.
			// Its original consumer/receipt still describes that responsibility;
			// S10's Main questionnaire creates no new Worker obligation.
			guidance.Objective = "This event assigns no S10 audit work to this Worker; Main owns acceptance and release audit. The original responsibility status remains with its original consumer and receipt"
			guidance.Action = ""
			guidance.PrimarySkill = ""
			guidance.Read = []string{}
			guidance.ReadOrder = nil
			guidance.Questions = nil
			guidance.Integration = nil
			guidance.Automation = nil
			guidance.Recovery = []string{}
			guidance.DoneWhen = []string{}
			guidance.Missing = []string{}
			// Generic protocol/manual navigation does not preload Main's
			// Manifest templates, release method or professional questionnaire.
			guidance.ProtocolRef = "docs/control/agent-protocol.md"
		}
	}

	if lifecycleState == "building" {
		applyDispatchGuidance(root, state, event, input, &guidance)
		if qualitygate.HasDispatchPlan(state) {
			guidance.Automation = append(guidance.Automation, "read the approved overall dispatch plan and `loop-harness s6 status --capacity <actual total slots>`; dispatch compatible ready tasks, integrate reported results promptly and refill slots without waiting for unrelated tasks in a prior wave")
		}
	}
	if mainQuestionnaireViewer(input) && s7BudgetGateRequired(state, root) {
		guidance.Action = stageS7BudgetDecisionQuestionnaireContext(root, input, runtime.Snapshot{State: state})
		guidance.ReadOrder = s7BudgetDecisionQuestionnairePaths(root, state)
		guidance.Automation = []string{"the original completion outlet or SessionStart prepares this exact budget application; ordinary reads do not create it", "after the human actually chooses, fill only decision, the actual increased maximum when applicable, and reason; save the same application for original Writer reception and successor", "empty choice grants nothing; do not copy Runtime metadata, register evidence or invoke a manual transition"}
		guidance.Recovery = []string{"Main/system restores missing original decision preparation or accepted sources; do not invent another approval or repeat an already received professional report"}
	}

	guidance.Instruction = formatGuidanceInstruction(guidance)
	return guidance
}

func applyMainPlanningAutomation(guidance *policy.Guidance, planning *mainPlanningNext) {
	if planning == nil {
		return
	}

	guidance.Automation = append(guidance.Automation, planning.Automation)
}

func applyS10Guidance(guidance *policy.Guidance) {
	guidance.Automation = []string{
		"Use the current exact Manifest questionnaire; reuse applicable recorded S7 observations and add only actual gaps or this step's findings.",
		"Save the professional report in that file; do not copy hashes, register another envelope or run a manual transition.",
		"Drafts and unchanged replays only report visible unfinished items; a completed report's normal save outlet performs this step's original transition.",
	}
	guidance.Missing = []string{} // Actual empty/unknown/fail/blocker rows are reported with the current questionnaire.
	guidance.Recovery = []string{"Main resolves the specific missing material or unfinished question shown with the exact current questionnaire; S11 remains a real human decision."}
	guidance.DoneWhen = []string{"The author explicitly completes this professional step with no explicitly unfinished or blocked item; the original S7 round completion/invalidation facts still apply."}
}

func investigationNextAction(root string, state map[string]any) string {
	if runtime.PacketEnabled(state) {
		if paths := investigationLeadQuestionnairePaths(root, state); len(paths) > 0 {
			return "Main questionnaire: " + strings.Join(paths, "; ") + ". Complete the current professional fields and save; follow the recorded Case results, routing and approval dependencies."
		}
		return "follow the recorded Case disposition and original routing/approval consumer; " + s8ListenerDiagnosticPaths(root, state, "")
	}
	reviewMap, _ := state["review"].(map[string]any)
	casePointer, _ := reviewMap["investigation"].(map[string]any)
	if casePointer == nil {
		return "form the InvestigationCase from its actual current source: sealed S7 observations or the human-reported problem; use the supplied current intake questionnaire, without inventing a Batch or Finding"
	}
	caseID := stringValue(casePointer["case_id"])
	if caseID == "" {
		caseID = "<case-id>"
	}
	if route := stringValue(casePointer["route"]); route != "" {
		if action := investigationRouteNextAction(route, casePointer); action != "" {
			return fmt.Sprintf("Case %s is routed to %s; next: %s", caseID, route, action)
		}
	}
	return fmt.Sprintf("continue InvestigationCase %s: run `runtime investigation status --case-id %s`, then dispatch independent hypothesis questions", caseID, caseID)
}

// repairPlanningNextAction projects the original owner and packet index. It
// does not issue a questionnaire or infer a new author from a role. The actual
// division and dispatch listeners remain responsible for their source checks.
func repairPlanningNextAction(state map[string]any, root string) string {
	division := "the original Builder division supplied by the listener"
	if root != "" {
		if paths := repairDispatchPaths(root, state); len(paths) == 1 {
			division = paths[0]
		}
	}
	selection := "Main selects and saves each unselected Builder Assignment at " + division + ". "
	pointer := repairPointerCLI(state)
	owners := stringMapCLI(pointer["assignment_owners"])
	if len(owners) == 0 {
		return selection + "The original save prepares each selected responsibility; Main then dispatches its exact prepared brief through native Agent/worktree. Builders save their own Plan with real prechecks and their Result; natural PreToolUse and successful PostToolUse observe ready answers, with Stop/SubagentStop as fallback."
	}
	packets, err := s9ListenerQuestionnaires(state, "", false)
	if err != nil {
		return selection + "The current original PlanReport index cannot be verified: " + err.Error() + ". Preserve selected owners and original sources while Main/system restores the named source."
	}
	byAssignment := map[string]s9ListenerQuestionnaire{}
	for _, packet := range packets {
		byAssignment[packet.binding.RepairAssignmentID] = packet
	}
	ids := make([]string, 0, len(owners))
	for id, owner := range owners {
		if owner != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var issued, missing []string
	for _, id := range ids {
		owner := owners[id]
		packet, ok := byAssignment[id]
		if !ok {
			missing = append(missing, id+" (owner "+owner+")")
			continue
		}
		brief, _, _ := nativeS9Paths(workpacket.RepairPlanReportPacket{ID: packet.id, ViewPath: packet.view})
		if root != "" {
			brief = resolveRootPath(root, brief)
		}
		issued = append(issued, id+" (owner "+owner+", original brief "+brief+")")
	}
	var next strings.Builder
	next.WriteString("For any other unselected Assignment, " + selection)
	if len(issued) > 0 {
		next.WriteString("For the selected and indexed responsibilities " + strings.Join(issued, "; ") + ", Main uses the exact original prepared brief for the initial native Agent/worktree dispatch, or continues the same actor when already started. If its actual original brief is unavailable, Main/system restores that original preparation. ")
	}
	if len(missing) > 0 {
		next.WriteString("No current original PlanReport is indexed for " + strings.Join(missing, "; ") + "; Main/system restores the original division preparation at " + division + ", preserving the selected owner. ")
	}
	next.WriteString("Each Builder saves its own Plan with real prechecks before implementation and its Result after work. Natural PreToolUse and successful PostToolUse observe ready answers, with Stop/SubagentStop as fallback; accepted work is not submitted again.")
	return next.String()
}

func repairNextAction(state map[string]any, phase string, roots ...string) string {
	reviewMap, _ := state["review"].(map[string]any)
	if investigation, _ := reviewMap["investigation"].(map[string]any); investigation == nil || stringValue(investigation["status"]) != "contract_approved" {
		return "complete S8 InvestigationCase and approve the RepairContract before opening S9"
	}
	pointer, _ := reviewMap["repair"].(map[string]any)
	if pointer == nil {
		return "the approved S8 RepairContract is ready; the next writable Main Hook prepares the original RepairSession and RepairPlan. Main owns Assignment dispatch, and the listeners supply each Builder's exact PlanReport questionnaire path"
	}
	switch stringValue(pointer["status"]) {
	case "planning", "reproducing":
		root := ""
		if len(roots) > 0 {
			root = roots[0]
		}
		return repairPlanningNextAction(state, root)
	case "repairing":
		return "native Builders use the supplied brief and save their private professional Plan/Result in the actual worktree. Natural PreToolUse and successful PostToolUse observe ready answers, with Stop/SubagentStop as fallback. When the candidate is recorded, Main immediately follows the supplied `runtime workspace integrate` command with the actual root, assignment and owner and repeatable --check-command for necessary real Main checks. Do not redispatch an accepted native candidate for another RepairResult questionnaire or wait for Final before Main integration. Existing frozen executions retain their exact original RepairResult questionnaire and recovery path"
	case "impact_reconciliation":
		return "Main fills and saves the exact ChangeImpact questionnaire supplied by the listener; the original consumer records its professional change analysis. The independent verifier saves its separately supplied TargetedReverification answer; natural PreToolUse and successful PostToolUse observe ready answers, with Stop/SubagentStop as fallback through the original Contract checks"
	case "targeted_reverification":
		return "the registered independent verifier saves the exact TargetedReverification answer; natural PreToolUse and successful PostToolUse observe ready answers, with Stop/SubagentStop as fallback through the original consumer and Contract assertions. Main retains integration and handoff"
	case "ready_for_full_review":
		return repairHandoffPendingInstruction
	case "closed":
		return "the original S9 Handoff consumer has seeded a fresh S7 round; use the supplied ReviewPlan questionnaire path and continue through the original S7 registration, dispatch, checks, and evaluation"
	}
	if action := stringValue(pointer["next_action"]); action != "" {
		return action
	}
	return fmt.Sprintf("continue from the current S9 %s Hook checkpoint; follow its supplied questionnaire paths and recorded recovery action", phase)
}

func applyS9RepairProjection(root string, state map[string]any, guidance *policy.Guidance) {
	reviewMap, _ := state["review"].(map[string]any)
	if reviewMap == nil {
		return
	}
	investigation, _ := reviewMap["investigation"].(map[string]any)
	if investigation == nil || stringValue(investigation["status"]) != "contract_approved" {
		return
	}
	pointer, _ := reviewMap["repair"].(map[string]any)
	if pointer == nil {
		line := "S9 approved RepairContract is ready; the next writable Main Hook prepares the original RepairSession and RepairPlan. Main owns Assignment dispatch, and the listeners supply each Builder's exact PlanReport questionnaire path"
		guidance.Automation = append(guidance.Automation, line)
		guidance.Recovery = append([]string{line}, guidance.Recovery...)
		return
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	line := fmt.Sprintf("S9 repair %s: session=%s; next=%s", stringValue(pointer["status"]), stringValue(pointer["session_id"]), repairNextAction(state, stringValue(lifecycle["phase"]), root))
	if stringValue(pointer["status"]) == "blocked" {
		if route := stringValue(pointer["failure_route"]); route != "" {
			line += "; failure_route=" + route + " — follow the recorded recovery action; do not create a symptom-only patch"
		}
	}
	guidance.Automation = append(guidance.Automation, line)
	guidance.Recovery = append([]string{line}, guidance.Recovery...)
}

// applyS7RecoveryProjection enriches the SessionStart/PreCompact recovery
// packet with the S7-specific projection L3-S7 §8 demands: the current
// review round, running/queued/blocked Assignments, unconsumed
// ReviewResults, Claim coverage gaps (required Claims still without a
// disposition) and the single next action. Every fact is computed from the
// shared control plane (state.review: plan pointer, claim dispositions,
// assignment rows, finding entities) — no new state file is introduced.
// Other lifecycle phases and events are untouched.
func applyS7RecoveryProjection(root string, state map[string]any, guidance *policy.Guidance) {
	reviewMap, _ := state["review"].(map[string]any)
	if reviewMap == nil {
		return
	}
	if repairSeedAwaitingPlan(state) {
		guidance.Action = stageReviewPlanQuestionnaireContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root}, runtime.Snapshot{State: state})
		guidance.Missing = []string{"Main: refine the original TR-012 seed through its S7 ReviewPlan questionnaire"}
		guidance.ReadOrder = []string{reviewPlanQuestionnairePath(root, state), reviewPlanGuidePath(reviewPlanQuestionnairePath(root, state))}
		guidance.Read = append([]string(nil), guidance.ReadOrder...)
		return
	}
	// The buildNextProjection layer still emits the legacy `claim_results`
	// open-items token for the S7 stage contract; the S7 recovery packet
	// supersedes it with the precise `claim:<id>` matrix (L3-S7 §8), so
	// drop the bare aggregate so the Agent does not see redundant noise.
	guidance.Missing = stripMissingTokens(guidance.Missing, "claim_results")
	round := integerValue(reviewMap["round"])
	ptr := review.PlanPointerFromState(state)

	planDesc := "no ReviewPlan registered"
	planStatus := "planned"
	if ptr != nil {
		planDesc = fmt.Sprintf("%s status=%s revision=%d e2e_coverage=%s", ptr.PlanID, ptr.Status, ptr.Revision, ptr.E2ECoverageState)
		planStatus = ptr.Status
	}
	guidance.Automation = append(guidance.Automation,
		fmt.Sprintf("S7 review round %d: plan %s", round, planDesc),
	)
	if planStatus == "stale" {
		guidance.Action = s7RecoveryNextAction(root, state, planStatus, round, nil, nil, nil, nil, nil)
		guidance.Missing = []string{"Main: recover the frozen baseline and original stale ReviewPlan"}
		guidance.Recovery = append([]string{fmt.Sprintf("S7 recovery: round %d, plan %s; next: %s", round, planDesc, guidance.Action)}, guidance.Recovery...)
		return
	}
	if maxRounds := s7MaxRounds(state); maxRounds > 0 && round >= maxRounds {
		guidance.Automation = append(guidance.Automation,
			fmt.Sprintf("S7 budget: current round %d of %d may drain. Only an actual request for another full round requires a human choice in the system-prepared budget application; ordinary acceptance does not require another approval", round, maxRounds),
		)
	}

	blockedAgents := blockedAgentIDs(state)
	var running, queued, blocked, unconsumed []string
	assignments, _ := reviewMap["assignments"].(map[string]any)
	assignmentIDs := make([]string, 0, len(assignments))
	for id := range assignments {
		assignmentIDs = append(assignmentIDs, id)
	}
	sort.Strings(assignmentIDs)
	for _, id := range assignmentIDs {
		row, _ := assignments[id].(map[string]any)
		if row == nil {
			continue
		}
		status := workstate.ReviewAssignmentActivity(row)
		agent := stringValue(row["agent_id"])
		label := id
		if agent != "" {
			label = id + "(" + agent + ")"
		}
		switch status {
		case "planned":
			// Not yet dispatched: platform capacity may queue work but never
			// drops required coverage (L3-S7 §4.5).
			queued = append(queued, id)
		case "blocked":
			blocked = append(blocked, label)
			unconsumed = append(unconsumed, id)
		case "dispatched":
			if blockedAgents[agent] {
				blocked = append(blocked, label)
			} else {
				running = append(running, label)
			}
			// A dispatched Assignment's Canonical ReviewResult is pending
			// until `runtime review-result submit` consumes it.
			unconsumed = append(unconsumed, id)
		}
	}
	guidance.Automation = append(guidance.Automation,
		"S7 assignments running: "+s7Bucket(running),
		"S7 assignments queued: "+s7Bucket(queued),
		"S7 assignments blocked: "+s7Bucket(blocked),
		"S7 unconsumed ReviewResults (dispatched, professional answer not yet received by the original consumer): "+s7Bucket(unconsumed),
	)

	// Claim coverage gaps: required Claims with no final disposition yet.
	gaps := review.UndispositionedRequired(state)
	for _, claimID := range gaps {
		guidance.Missing = appendUnique(guidance.Missing, "claim:"+claimID)
	}

	// Ordinary Findings make the round non-clean before the original consumer
	// seals its Batch. A missing Batch pointer during draining is normal.
	if planStatus == "cannot_clean" || planStatus == "discovery_draining" {
		guidance.Automation = append(guidance.Automation,
			"S7 round status="+planStatus+": confirmed Findings retain safe remaining required Claims; cannot_clean/discovery_draining ≠ end — finish the remaining required Claims listed in Missing",
			s7ObservationBatchLine(reviewMap))
	}
	for _, label := range blocked {
		guidance.Automation = append(guidance.Automation, s7BlockedAssignmentGuidance(root, state, firstAssignmentID(label)))
	}

	next := s7RecoveryNextAction(root, state, planStatus, round, running, queued, blocked, unconsumed, gaps)
	guidance.Action = next
	guidance.Recovery = append([]string{
		fmt.Sprintf("S7 recovery: round %d, plan %s; coverage gaps=%d; next: %s", round, planDesc, len(gaps), next),
	}, guidance.Recovery...)
}

// applyS8EntryProjection adds the S8 entry source line that tells a
// compacted Agent which ObservationBatch is currently recorded for S8.
// It does not infer the transition event from a Batch pointer. The pointer is read from
// state.review.observation_batch (the same path the sealed handoff
// document writes — L3-S7 §3.7). When no batch is present (defensive),
// the projection is skipped: S8 entry without a sealed batch would be a
// control-plane contradiction the rest of the harness must surface, not
// the recovery packet.
//
// The line is appended to the Automation block (positive guidance), and
// mirrored as the first Recovery line so a PreCompact that drops
// Automation still preserves the source fact.
func applyS8EntryProjection(state map[string]any, guidance *policy.Guidance) {
	reviewMap, _ := state["review"].(map[string]any)
	if reviewMap == nil {
		return
	}
	if investigationPointer, _ := reviewMap["investigation"].(map[string]any); investigationPointer == nil {
		line := "S8 intake pending: run `runtime investigation ingest --grouping-rationale <reason>`; do not create a BUG or reproduce the sealed symptom"
		if runtime.PacketEnabled(state) {
			line = "S8 intake pending: " + guidance.Action
		}
		guidance.Automation = append(guidance.Automation, line)
		guidance.Recovery = append([]string{line}, guidance.Recovery...)
	}
	batch, _ := reviewMap["observation_batch"].(map[string]any)
	if batch == nil {
		return
	}
	id := stringValue(batch["batch_id"])
	if id == "" {
		return
	}
	drain := stringValue(batch["drain_policy"])
	findingIDs, _ := batch["finding_ids"].([]any)
	count := len(findingIDs)
	line := fmt.Sprintf("S8 current ObservationBatch %s (%d findings, drain_policy=%s)",
		id, count, drain)
	guidance.Automation = append(guidance.Automation, line)
	guidance.Recovery = append([]string{line}, guidance.Recovery...)
}

// s7ObservationBatchLine renders the current ObservationBatch pointer (id,
// drain_policy, finding count) as one compact recovery line. Absence is
// normal until the original consumer seals the round's findings.
func s7ObservationBatchLine(reviewMap map[string]any) string {
	batch, _ := reviewMap["observation_batch"].(map[string]any)
	if batch == nil {
		return "S7 ObservationBatch: not sealed yet; no Batch pointer is required while remaining required Claims drain"
	}
	id := stringValue(batch["batch_id"])
	if id == "" {
		id = "observation-batch(unknown)"
	}
	drain := stringValue(batch["drain_policy"])
	if drain == "" {
		drain = "complete_required_claims"
	}
	findingIDs, _ := batch["finding_ids"].([]any)
	return fmt.Sprintf("S7 ObservationBatch %s: drain_policy=%s; %d finding(s) sealed", id, drain, len(findingIDs))
}

// s7RecoveryNextAction computes the single next action for a verification
// recovery packet. The order is deterministic: finish in-flight Results
// first, then unblock, then dispatch queued coverage, then close the round.
func s7RecoveryNextAction(root string, state map[string]any, planStatus string, round int, running, queued, blocked, unconsumed, gaps []string) string {
	switch planStatus {
	case "stale":
		return "Main must resolve the original baseline drift feedback and recover the original ReviewPlan through its existing revive/revise path; queued dispatch and old answers await a valid plan" + ". " + s7PinnedSourceRecovery(root)
	case "observation_sealed":
		return "the sealed ObservationBatch hands off to S8 automatically: the next PreToolUse auto-commits TR-008 — do not invoke the transition CLI"
	case "clean":
		return "the original final ReviewResult exit continues Gate/TR-009 and prepares S10 acceptance; resume a pending original exit with loop-harness runtime review-continue --root <root>"
	case "paused":
		return "resolve the recorded pause verdict checkpoint; the round resumes only through the human gateway (TR-010/TR-011)"
	case "planned", "":
		return fmt.Sprintf("complete and save the current round %d ReviewPlan questionnaire at the exact path supplied by the listener; resolve the professional TODOs and required coverage", round)
	}
	// running / cannot_clean / discovery_draining: coverage continues even
	// after an ordinary finding (drain_policy=complete_required_claims).
	if len(running) > 0 {
		return fmt.Sprintf("Reviewer %s uses the exact ReviewResult answer: ordinarily finish required safe checks; a real P0 stops dangerous continuation and records actual capture gaps; a real incomplete return names the missing observation/source and returns to Main for the same responsibility. Save and return that original reference for SubagentStop and the original consumer", running[0])
	}
	if len(blocked) > 0 {
		return strings.TrimSpace(s7BlockedAssignmentGuidance(root, state, firstAssignmentID(blocked[0])))
	}
	if len(queued) > 0 {
		return strings.TrimSpace(s7PendingReviewGuidance(root, state))
	}
	if len(unconsumed) > 0 {
		return fmt.Sprintf("Main checks the native return and original receipt for %s; report the exact missing reference or consumer feedback, without authoring or sweeping the Worker answer", unconsumed[0])
	}
	if len(gaps) > 0 {
		return "claim coverage gaps remain without an assignment (" + strings.Join(gaps, ", ") + "); inspect `loop-harness s7 status` and repair the dispatch"
	}
	return "all required Claims are dispositioned; the original round consumer evaluates sealing and the listener reports the next stage"
}

// s7Bucket renders one assignment bucket for the recovery packet, keeping
// empty buckets explicit so a compacted Agent sees the full picture.
func s7Bucket(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

// firstAssignmentID strips the optional "(agent)" suffix from a bucket label.
func firstAssignmentID(label string) string {
	if idx := strings.Index(label, "("); idx > 0 {
		return label[:idx]
	}
	return label
}

// blockedAgentIDs indexes agent entities that are explicitly blocked so a
// dispatched review Assignment can be bucketed as blocked instead of
// running.
func blockedAgentIDs(state map[string]any) map[string]bool {
	out := map[string]bool{}
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		id := stringValue(row["id"])
		if id == "" {
			continue
		}
		if v, ok := row["blocked"].(bool); ok && v {
			out[id] = true
			continue
		}
		if stringValue(row["state"]) == "blocked" {
			out[id] = true
		}
	}
	return out
}

func addDelegationPreflight(guidance *policy.Guidance, input policy.Input) {
	guidance.Questions = []string{
		"What professional work and checks does this assignment require?",
		"Which supplied source and answer paths belong to this assignment?",
	}
	if subType, _ := input.ToolInput["subagent_type"].(string); subType != "" {
		guidance.ReadOrder = insertReadOrder(guidance.ReadOrder, ".claude/agents/"+subType+".md", 2)
	}
	guidance.Automation = append(guidance.Automation,
		"Main actually dispatches native Agents with exact professional references; no platform Team or preselected actor identity is required",
		"isolate execution in a worktree before writing",
		"save and return the supplied professional answer for SubagentStop; the original consumer reports actual missing duties; explicit plan_approval_required work retains its real approval",
	)
}

func insertReadOrder(values []string, value string, index int) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	if index < 0 || index >= len(values) {
		return appendUnique(values, value)
	}
	result := make([]string, 0, len(values)+1)
	result = append(result, values[:index]...)
	result = append(result, value)
	result = append(result, values[index:]...)
	return result
}

func formatGuidanceInstruction(g policy.Guidance) string {
	var b strings.Builder
	b.WriteString("LOOP RECOVERY: you are at ")
	b.WriteString(g.Stage)
	b.WriteString(" (runtime ")
	b.WriteString(g.RuntimeID)
	fmt.Fprintf(&b, ", rev=%d). ", g.Revision)
	b.WriteString("Objective: ")
	b.WriteString(g.Objective)
	b.WriteString(". ")
	if strings.TrimSpace(g.Action) != "" {
		b.WriteString("Next: ")
		b.WriteString(g.Action)
		b.WriteString(". ")
	}
	if g.Stage == "S8" && g.LifecyclePhase == "investigation" {
		b.WriteString("Follow the professional questionnaire and source read order below. Only if blocked, consult ")
		b.WriteString(g.ProtocolRef)
		b.WriteString(" or ")
		b.WriteString(g.ManualRef)
		b.WriteString(" as optional recovery references.")
	} else {
		b.WriteString("Read ")
		b.WriteString(g.ProtocolRef)
		b.WriteString(" for the next protocol step and ")
		b.WriteString(g.ManualRef)
		b.WriteString(" for the harness recovery procedure.")
	}
	if len(g.ReadOrder) > 0 {
		b.WriteString(" Read in order: ")
		b.WriteString(strings.Join(g.ReadOrder, " -> "))
		b.WriteString(".")
	}
	if len(g.Questions) > 0 {
		b.WriteString(" Preflight questions: ")
		b.WriteString(strings.Join(g.Questions, " | "))
		b.WriteString(".")
	}
	if len(g.Automation) > 0 {
		b.WriteString(" Automation: ")
		b.WriteString(strings.Join(g.Automation, " | "))
		b.WriteString(".")
	}
	if len(g.Integration) > 0 {
		b.WriteString(" Integration: ")
		b.WriteString(strings.Join(g.Integration, " | "))
		b.WriteString(".")
	}
	if g.Blocked {
		b.WriteString(" BLOCKED: ")
		b.WriteString(g.Blocker)
	}
	if g.HumanRequired {
		if g.LifecycleState == "awaiting_human_release" {
			b.WriteString(" Await the actual human choice; then save disposition/reason in the exact application for its automatic original exit. Empty disposition remains a draft.")
		} else {
			b.WriteString(" Human Gateway required; do not continue automation.")
		}
	}
	return b.String()
}

func recoveryReadOrder(root string, next nextProjection) []string {
	entry := "AGENTS.md"
	if _, err := os.Stat(filepath.Join(root, entry)); err != nil {
		if _, templateErr := os.Stat(filepath.Join(root, "AGENTS-template.md")); templateErr == nil {
			entry = "AGENTS-template.md"
		}
	}
	order := []string{
		"LOOP RECOVERY packet (this message)",
		entry,
		".claude/loop-state.json",
		next.ProtocolRef,
	}
	for _, item := range next.Read {
		order = appendUnique(order, item)
	}
	if next.PrimarySkill != "" {
		order = appendUnique(order, ".claude/skills/"+next.PrimarySkill+"/SKILL.md")
	}
	return order
}

// refreshMilestone commits the controller checkpoint through the same CAS
// Store used by lifecycle transitions. A semantically identical checkpoint is
// a no-op, which prevents repeated SessionStart/PreCompact hooks from burning
// revisions and journal entries.
//
// refreshMilestone is the legacy zero-gate variant kept for callers that
// have not yet been threaded with the Controller's quality_gate projection
// (BUG-039-05 / BUG-039-06). New code paths must use
// refreshMilestoneWithGate so the persisted milestone reflects the gate
// fingerprint — see BUG-039-07 §4.1.
func refreshMilestone(root, statePath, journalPath string, snapshot runtime.Snapshot, guidance policy.Guidance, event string) (runtime.Snapshot, bool, error) {
	return refreshMilestoneWithGate(root, statePath, journalPath, snapshot, guidance, event, controller.QualityGateResult{})
}

func refreshMilestoneWithGate(root, statePath, journalPath string, snapshot runtime.Snapshot, guidance policy.Guidance, event string, gate controller.QualityGateResult) (runtime.Snapshot, bool, error) {
	current, _ := snapshot.State["milestone"].(map[string]any)
	s6Observation := event == "PreToolUse" && guidance.LifecycleState == "building" && gate.Status != ""
	if milestoneMatchesWithGate(current, guidance, gate) && (!s6Observation || current["source_revision"] != nil && integerValue(current["source_revision"]) == snapshot.Revision) {
		return snapshot, false, nil
	}

	now := time.Now().UTC()
	persistedGuidance := guidance
	persistedGuidance.Revision = snapshot.Revision + 1
	persistedGuidance.Event = event
	persistedGuidance.Instruction = formatGuidanceInstruction(persistedGuidance)
	milestone := guidanceMapWithGate(persistedGuidance, controller.QualityGateResult{}, event, snapshot.Revision+1, now, gate)
	from := lifecycleCursor(snapshot.State)
	store := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	idempotencyKey := milestoneIdempotencyWithGate(guidance, gate)
	if s6Observation {
		idempotencyKey = fmt.Sprintf("%s:r%d", idempotencyKey, snapshot.Revision)
	}
	updated, err := store.Update(snapshot.Revision, runtime.Mutation{
		EventID:              fmt.Sprintf("evt-milestone-refreshed-r%d", snapshot.Revision+1),
		TransitionID:         "MILESTONE-REFRESH",
		Event:                "milestone_refreshed",
		JournalEvent:         "milestone_refreshed",
		JournalOutcome:       "refreshed",
		RetainLastTransition: true,
		Actor:                "hook",
		RuntimeID:            guidance.RuntimeID,
		From:                 from,
		To:                   from,
		EvidenceIDs:          []string{},
		IdempotencyKey:       idempotencyKey,
		Message:              "Controller refreshed the resumable lifecycle milestone.",
		OccurredAt:           now,
		Apply: func(state map[string]any) error {
			state["milestone"] = milestone
			state["updated_at"] = now.Format(time.RFC3339Nano)
			return nil
		},
	})
	if err != nil {
		_ = metrics.ObserveMilestoneRefreshFailure(root, milestoneRefreshFailureReason(err))
		return runtime.Snapshot{}, false, err
	}
	return updated, true, nil
}

// milestoneRefreshFailureReason keeps the durable metric useful without
// leaking raw error strings into a label. The refresh is a best-effort
// checkpoint, so classification is diagnostic only: it never changes the
// Controller's gate verdict or invents a revision cap.
func milestoneRefreshFailureReason(err error) string {
	switch {
	case errors.Is(err, runtime.ErrStaleRevision):
		return "stale_revision"
	case errors.Is(err, runtime.ErrPendingRuntimeOperation):
		return "pending_runtime"
	case errors.Is(err, runtime.ErrCandidateValidatorRequired),
		errors.Is(err, runtime.ErrCandidateValidatorInvalid):
		return "candidate_validation"
	default:
		return "write_or_integrity"
	}
}

// reconcileGuidance is intentionally bounded. A concurrent transition may
// win the first CAS; the controller rereads the new cursor and retries once,
// then leaves the event to the next Hook invocation rather than guessing.
//
// BUG-039-37: SubagentStop and TeammateIdle are not text-only projections.
// When the Hook evaluate path reaches reconcileGuidance for those events,
// we dispatch to HandleSubagentStopForController / HandleTeammateIdleForController
// so Inspect→Integrate and teammate scheduling actually run.
func reconcileGuidance(root, event string, input policy.Input) (policy.Guidance, runtime.Snapshot, error) {
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	// A fresh checkout (no state file) is S0, not a recovery case: there is
	// nothing to reconcile and no lock to take.
	if runtimeStateMissing(root) {
		return *freshStartGuidance(root, event), runtime.Snapshot{}, nil
	}
	// Guidance reconciliation persists a milestone, so it is an explicit
	// mutation path and owns pending-runtime recovery.
	store := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	for attempt := 0; attempt < 2; attempt++ {
		snapshot, err := store.Snapshot()
		if err != nil {
			return policy.Guidance{}, runtime.Snapshot{}, err
		}
		switch event {
		case "SubagentStop", "TeammateIdle":
			guidance, updated, herr := reconcileSpecialGuidance(root, snapshot, event, input)
			if errors.Is(herr, runtime.ErrStaleRevision) {
				continue
			}
			if herr != nil {
				return policy.Guidance{}, runtime.Snapshot{}, herr
			}
			return guidance, updated, nil
		}
		guidance := buildGuidance(root, snapshot.State, event, input)
		updated, _, err := refreshMilestone(root, statePath, journalPath, snapshot, guidance, event)
		if errors.Is(err, runtime.ErrStaleRevision) {
			continue
		}
		if err != nil {
			return policy.Guidance{}, runtime.Snapshot{}, err
		}
		// The refresh may have advanced the revision. Rebuild from the committed
		// state so the message and persisted milestone point at the same cursor.
		guidance = buildGuidance(root, updated.State, event, input)
		return guidance, updated, nil
	}
	return policy.Guidance{}, runtime.Snapshot{}, runtime.ErrStaleRevision
}

// reconcileSpecialGuidance dispatches SubagentStop / TeammateIdle to the
// Integrator and teammate scheduler (BUG-039-37). LoadFull is invoked with
// an empty agentID so a missing/broken activation envelope cannot abort
// the assignment/worktree projection the handlers need. A LoadFull
// failure falls through to the text projection so the Agent still
// receives a Recovery packet rather than a silent miss.
func reconcileSpecialGuidance(root string, snapshot runtime.Snapshot, event string, input policy.Input) (policy.Guidance, runtime.Snapshot, error) {
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if runtime.PacketEnabled(snapshot.State) && ((lifecycle["state"] == "bug_resolution" && s9ProfessionalPhase(stringValue(lifecycle["phase"]))) || lifecycle["state"] == "acceptance" || lifecycle["state"] == "release_audit") {
		input.Event = event
		return graphGuidance(root, snapshot.State, input), snapshot, nil
	}
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		guidance := buildGuidance(root, snapshot.State, event, input)
		return guidance, snapshot, nil
	}
	switch event {
	case "SubagentStop":
		guidance := buildGuidance(root, snapshot.State, event, input)
		if a := findAssignmentForInput(loaded, input); a != nil {
			if a.WorktreePath != "" && hook.HasCompletionReport(a) {
				guidance.Action = fmt.Sprintf("Worker: your report is recorded; await Main. Main: run runtime task-integrate --assignment-id %s --root %s in the main workspace", a.AssignmentID, root)
			}
			guidance.Integration = appendUniqueStrings(guidance.Integration, "integration pending; required checks run through the explicit command outside the Hook timeout")
			guidance.Instruction = formatGuidanceInstruction(guidance)
		}
		return guidance, snapshot, nil
	case "TeammateIdle":
		return HandleTeammateIdleForController(root, snapshot, loaded, event, input)
	default:
		guidance := buildGuidance(root, snapshot.State, event, input)
		return guidance, snapshot, nil
	}
}

// guidanceMap is the legacy zero-gate projection kept for callers that
// have not yet been threaded with the Controller's quality_gate projection
// (BUG-039-05 / BUG-039-06). It now delegates to guidanceMapWithGate with
// a zero-value gate so the persisted milestone produced by the legacy
// refresh path still validates against the loop-state schema
// (BUG-039-07 §4.1).
func guidanceMap(g policy.Guidance, event string, sourceRevision int, now time.Time) map[string]any {
	return guidanceMapWithGate(g, controller.QualityGateResult{}, event, sourceRevision, now, controller.QualityGateResult{})
}

// guidanceMapWithGate is the gate-aware milestone projection. When the
// caller provides a non-empty gate (Status != ""), the persisted milestone
// includes the `quality_gate` sub-object with the 9 fields required by
// SYNC-039 §6 / REQ-039 §11. The integration field remains a string array
// for compatibility with the BUG-06 worktree checkpoint workaround.
func guidanceMapWithGate(g policy.Guidance, _ controller.QualityGateResult, event string, sourceRevision int, now time.Time, gate controller.QualityGateResult) map[string]any {
	phase := any(nil)
	if g.LifecyclePhase != "" {
		phase = g.LifecyclePhase
	}
	milestone := map[string]any{
		"stage":           g.Stage,
		"lifecycle_state": g.LifecycleState,
		"lifecycle_phase": phase,
		"objective":       g.Objective,
		"action":          g.Action,
		"protocol_ref":    g.ProtocolRef,
		"manual_ref":      g.ManualRef,
		"primary_skill":   g.PrimarySkill,
		"read":            nonNilStrings(g.Read),
		"read_order":      nonNilStrings(g.ReadOrder),
		"missing":         nonNilStrings(g.Missing),
		"done_when":       nonNilStrings(g.DoneWhen),
		"questions":       nonNilStrings(g.Questions),
		"automation":      nonNilStrings(g.Automation),
		"integration":     nonNilStrings(g.Integration),
		"human_required":  g.HumanRequired,
		"blocked":         g.Blocked,
		"blocker":         nullableString(g.Blocker),
		"event":           event,
		"instruction":     g.Instruction,
		"recovery":        nonNilStrings(g.Recovery),
		"source_revision": sourceRevision,
		"updated_at":      now.Format(time.RFC3339Nano),
	}
	if gateHasIdentity(gate) {
		milestone["quality_gate"] = qualityGateMap(gate)
	}
	return milestone
}

// qualityGateMap projects a Controller.QualityGateResult into the wire
// shape required by SYNC-039 §4 (status, gate_id, candidate_transition,
// observed_revision, fingerprint, missing, evidence_refs,
// transition_committed, next_cursor). It is the single source of truth
// for the milestone's quality_gate block (BUG-039-07 §4.1).
func qualityGateMap(gate controller.QualityGateResult) map[string]any {
	missing := gate.Missing
	if missing == nil {
		missing = []string{}
	}
	evidenceRefs := gate.EvidenceRefs
	if evidenceRefs == nil {
		evidenceRefs = []string{}
	}
	return map[string]any{
		"status":               string(gate.Status),
		"gate_id":              gate.GateID,
		"candidate_transition": gate.CandidateTransition,
		"observed_revision":    gate.ObservedRevision,
		"fingerprint":          gate.Fingerprint,
		"missing":              missing,
		"evidence_refs":        evidenceRefs,
		"transition_committed": gate.TransitionCommitted,
		"next_cursor":          gate.NextCursor,
	}
}

// milestoneMatches is the legacy zero-gate comparator kept for callers
// that have not yet been threaded with the Controller's quality_gate
// projection. It now delegates to milestoneMatchesWithGate with a
// zero-value gate.
func milestoneMatches(current map[string]any, guidance policy.Guidance) bool {
	return milestoneMatchesWithGate(current, guidance, controller.QualityGateResult{})
}

// milestoneMatchesWithGate reports whether the persisted milestone is
// semantically identical to a fresh projection computed from `guidance`
// and `gate`. Volatile observations such as revision/time/event are not part
// of milestone identity; a quality-gate fingerprint or actual guidance
// change still defeats the no-op (BUG-039-07 §4.1 step 2).
func milestoneMatchesWithGate(current map[string]any, guidance policy.Guidance, gate controller.QualityGateResult) bool {
	if current == nil {
		return false
	}
	return equalJSON(stableMilestoneProjection(current), stableMilestoneProjection(
		guidanceMapWithGate(guidance, controller.QualityGateResult{}, "", guidance.Revision, time.Time{}, gate),
	))
}

// stableMilestoneProjection is the single semantic identity used by both
// the no-op comparison and the journal idempotency key. The observed runtime
// revision is diagnostic telemetry, not a state change: including it here
// makes every hook refresh its own milestone and burns an unbounded revision
// for no semantic progress.
func stableMilestoneProjection(milestone map[string]any) map[string]any {
	if milestone == nil {
		return nil
	}
	projection := make(map[string]any, len(milestone))
	for key, value := range milestone {
		switch key {
		case "source_revision", "updated_at", "event", "instruction":
			continue
		case "quality_gate":
			gate, ok := value.(map[string]any)
			if !ok {
				projection[key] = value
				continue
			}
			stableGate := make(map[string]any, len(gate))
			for gateKey, gateValue := range gate {
				if gateKey != "observed_revision" {
					stableGate[gateKey] = gateValue
				}
			}
			projection[key] = stableGate
		default:
			projection[key] = value
		}
	}
	return projection
}

func gateHasIdentity(gate controller.QualityGateResult) bool {
	return gate.Status != "" || gate.GateID != "" || gate.CandidateTransition != "" ||
		gate.Fingerprint != "" || len(gate.Missing) > 0 || len(gate.EvidenceRefs) > 0 ||
		len(gate.Conflicts) > 0 || gate.ErrorCode != "" || gate.TransitionCommitted || gate.NextCursor != ""
}

func equalJSON(left, right any) bool {
	a, errA := json.Marshal(left)
	b, errB := json.Marshal(right)
	return errA == nil && errB == nil && string(a) == string(b)
}

// milestoneIdempotency is the legacy zero-gate idempotency key kept for
// callers that have not yet been threaded with the Controller's
// quality_gate projection. It now delegates to milestoneIdempotencyWithGate
// with a zero-value gate so the existing key shape is preserved
// (BUG-039-07 §4.1 step 3).
func milestoneIdempotency(g policy.Guidance) string {
	return milestoneIdempotencyWithGate(g, controller.QualityGateResult{})
}

// milestoneIdempotencyWithGate hashes the stable milestone projection so a
// gate change produces a new Journal key while observation-only revision
// changes remain idempotent (BUG-039-07 §4.1 step 3, REQ-039 §17).
func milestoneIdempotencyWithGate(g policy.Guidance, gate controller.QualityGateResult) string {
	payload := stableMilestoneProjection(guidanceMapWithGate(
		g, controller.QualityGateResult{}, "", g.Revision, time.Time{}, gate,
	))
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "milestone:" + hex.EncodeToString(sum[:])
}

func lifecycleCursor(state map[string]any) map[string]any {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	return map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
}

// resolveAgentReportComplete computes the SubagentStop / TeammateIdle
// "report is on the wire" fact from the control plane (hookctx), not
// from a self-injected `input.Facts[...]` flag. The official Claude
// Code 2.1.218 payload does NOT carry an `agent_report_complete` or
// `assignment_reported` field, so reading those flags produced a
// constant-true verdict that mis-fired on every event (L4 §10.2 /
// §15.2 P0-2 follow-up: keep the stop/idle facts on the same source
// the `stopidle.go` real platform control reads).
//
// The helper is fail-open: a missing or unreadable runtime cannot
// invent a completion fact, so it returns false. The hard block is
// owned by the platform `exit 2` control (stopidle.go), not by the
// Guidance projection — the projection only emits a "report is
// missing" hint when the control plane truly cannot see a report.
func resolveAgentReportComplete(root string, input policy.Input) bool {
	if root == "" {
		return false
	}
	agentID := input.EffectiveAgentID()
	if agentID == "" {
		return false
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl")).Snapshot()
	if err != nil {
		return false
	}
	reviewAssignmentID, reviewBound := hook.ReviewAssignmentBinding(snapshot.State, agentID)
	hypothesisAssignmentID, hypothesisBound := hook.HypothesisResultAssignmentBinding(root, snapshot.State, agentID)
	loaded, err := hookctx.FromSnapshot(root, agentID, snapshot)
	if err != nil || loaded == nil {
		return false
	}
	assignment := hook.AssignmentForAgent(loaded.Assignments, agentID)
	if hypothesisBound || (loaded.PolicyContext.Agent != nil && loaded.PolicyContext.Agent.HypothesisResultAssignment) {
		agent := loaded.PolicyContext.Agent
		if !hypothesisBound || hypothesisAssignmentID == "" || agent == nil || !agent.HypothesisResultAssignment ||
			agent.ID != agentID || agent.AssignmentID != hypothesisAssignmentID {
			return false
		}
		assignment = nil
		for i := range loaded.Assignments {
			row := &loaded.Assignments[i]
			if row.OwnerAgentID != agentID || row.AssignmentID != hypothesisAssignmentID {
				continue
			}
			if assignment != nil {
				return false
			}
			assignment = row
		}
		if assignment == nil {
			return false
		}
		return hook.CurrentResponsibility(root, snapshot, agent, assignment, false).ExecutionMayEnd
	}
	if agent := loaded.PolicyContext.Agent; reviewBound || (agent != nil && agent.ReviewAssignment) {
		if agent == nil || !agent.ReviewAssignment || reviewAssignmentID == "" || agent.AssignmentID != reviewAssignmentID {
			return false
		}
		return hook.CurrentResponsibility(root, snapshot, agent, assignment, false).ExecutionMayEnd
	}
	repairAssignmentID, repairBound := hook.RepairResultAssignmentBinding(root, snapshot.State, agentID)
	if repairBound || (loaded.PolicyContext.Agent != nil && loaded.PolicyContext.Agent.RepairResultAssignment) {
		agent := loaded.PolicyContext.Agent
		if !repairBound || repairAssignmentID == "" || agent == nil || !agent.RepairResultAssignment ||
			agent.ID != agentID || agent.RepairAssignmentID != repairAssignmentID {
			return false
		}
		return hook.CurrentResponsibility(root, snapshot, agent, assignment, false).ExecutionMayEnd
	}
	packetReceived := loaded.PolicyContext.PacketMode && submission.CompletedOwner(root, agentID)
	facts := hook.CurrentResponsibility(root, snapshot, loaded.PolicyContext.Agent, assignment, packetReceived)
	return facts.DeliveryRecorded || facts.Status == workstate.Blocked && facts.ExecutionMayEnd

}

func pauseReason(state map[string]any, lifecycleState string) string {
	if pause, ok := state["pause"].(map[string]any); ok {
		if reason, _ := pause["reason"].(string); reason != "" {
			return reason
		}
		if action, _ := pause["required_human_action"].(string); action != "" {
			return action
		}
	}
	if lifecycleState == "awaiting_human_release" {
		return "release-ready package awaits an explicit human decision: approve, defer, reject_defect, reject_acceptance, reject_release_audit, or abort"
	}
	return "runtime is in a human-controlled terminal or paused state"
}

func appendUnique(values []string, item string) []string {
	for _, value := range values {
		if value == item {
			return values
		}
	}
	return append(values, item)
}

// stripMissingTokens returns a copy of `values` with every occurrence of any
// `drop` token removed. Used by the S7 recovery projection to drop the
// legacy open-items aggregate so the recovery packet only carries the
// precise per-Claim matrix the §8 contract demands.
func stripMissingTokens(values []string, drop ...string) []string {
	if len(values) == 0 || len(drop) == 0 {
		return values
	}
	skip := map[string]struct{}{}
	for _, token := range drop {
		skip[token] = struct{}{}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := skip[value]; ok {
			continue
		}
		out = append(out, value)
	}
	return out
}

func appendUniqueStrings(values []string, items ...string) []string {
	for _, item := range items {
		values = appendUnique(values, item)
	}
	return values
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return append([]string{}, values...)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func integerValue(value any) int {
	switch value := value.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		result, _ := value.Int64()
		return int(result)
	default:
		return 0
	}
}

// controllerRuntimeContext projects only the fields required by Hook. It
// keeps the Agent context already loaded from the caller, including its
// activation scope.
func controllerRuntimeContext(state map[string]any, root string, existing policy.RuntimeContext) policy.RuntimeContext {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	existing.RuntimeID = stringValue(state["runtime_id"])
	existing.Revision = integerValue(state["revision"])
	existing.ProjectRoot = root
	existing.CurrentState, _ = lifecycle["state"].(string)
	existing.CurrentPhase, _ = lifecycle["phase"].(string)
	bound, _ := state["bound_req"].(map[string]any)
	existing.BoundREQPath, _ = bound["path"].(string)
	metadata, _ := bound["metadata"].(map[string]any)
	existing.BoundREQUIImpact, _ = metadata["ui_impact"].(string)
	return existing
}

func isGuidanceEvent(event string) bool {
	switch event {
	case "SessionStart", "SubagentStart", "SubagentStop", "TeammateIdle", "PreCompact":
		return true
	default:
		return false
	}
}

// refreshHookControl is the request-aware variant used by evaluate; it
// returns the runtime projection because request is passed by value.
func refreshHookControl(root string, request *policy.Input, decision *policy.Decision) {
	if decision.Decision == "allow" && !isGuidanceEvent(request.Event) {
		return
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	var (
		guidance policy.Guidance
		snapshot runtime.Snapshot
		err      error
	)
	if isGuidanceEvent(request.Event) {
		guidance, snapshot, err = reconcileGuidance(root, request.Event, *request)
	} else {
		// A denied/warned tool call receives the same positive recovery packet,
		// but does not create a Runtime revision on every tool invocation. The
		// lifecycle Hook events are the checkpoint write triggers.
		store := runtime.NewStore(statePath, journalPath)
		snapshot, err = store.Snapshot()
		if err == nil {
			guidance = buildGuidance(root, snapshot.State, request.Event, *request)
		}
	}
	if err != nil {
		if runtimeStateMissing(root) {
			decision.Guidance = freshStartGuidance(root, request.Event)
			return
		}
		decision.Guidance = fallbackGuidance(request.Event)
		return
	}
	request.Runtime = controllerRuntimeContext(snapshot.State, root, request.Runtime)
	decision.Guidance = &guidance
}

// freshStartGuidance covers the state every new project starts in: no
// loop-state.json yet. That is not a blocked condition — it is S0 with the
// bind path not yet taken (the former BLOCKED + reconcile
// instruction could never succeed on a fresh checkout).
func freshStartGuidance(root, event string) *policy.Guidance {
	guidance := &policy.Guidance{
		RuntimeID:      "unbound",
		Revision:       0,
		Event:          event,
		Stage:          "S0",
		LifecycleState: "inactive",
		Objective:      "produce one human-locked requirement",
		Action:         "draft docs/requirements/REQ-<id>.md from docs/requirements/REQ-template.md (skills: requirement-funnel), resolve the real user's goals, scope, constraints and acceptance criteria, and faithfully record the human's actual lock of the current version",
		ProtocolRef:    "docs/control/agent-protocol.md#s0",
		ManualRef:      loopManualRef,
		PrimarySkill:   "requirement-funnel",
		Read:           []string{"docs/control/agent-protocol.md#s0", "docs/requirements/REQ-template.md"},
		ReadOrder:      []string{"LOOP RECOVERY packet (this message)", "docs/control/agent-protocol.md#s0", "skills/requirement-funnel/SKILL.md", "docs/requirements/REQ-template.md"},
		Missing:        []string{"human_locked_req"},
		DoneWhen:       []string{"the current requirement is complete and the human's actual lock of that version is recorded"},
		Blocked:        false,
		Blocker:        "",
		Recovery:       []string{"use the current requirement draft or installed REQ template; missing entry preparation belongs to normal installation/initialization recovery"},
		Automation:     []string{"a draft does not authorize lock or startup; use the human's actual decisions", "the original entry supplies S1 after the actual requirement lock; S0 does not require a binding command or machine metadata"},
	}
	guidance.Instruction = formatGuidanceInstruction(*guidance)
	return guidance
}

func fallbackGuidance(event string) *policy.Guidance {
	guidance := &policy.Guidance{
		RuntimeID:      "unknown",
		Revision:       0,
		Event:          event,
		Stage:          "cross-stage",
		LifecycleState: "unknown",
		Objective:      "recover a valid runtime cursor",
		Action:         "run loop-harness runtime reconcile --root .",
		ProtocolRef:    "docs/control/agent-protocol.md#cursor-mapping",
		ManualRef:      loopManualRef,
		PrimarySkill:   "loop-orchestration",
		Read:           []string{".claude/loop-state.json", projectlayout.Definition},
		ReadOrder:      []string{"LOOP RECOVERY packet (this message)", "AGENTS.md", ".claude/loop-state.json", "docs/control/agent-protocol.md#cursor-mapping", loopManualRef},
		Missing:        []string{"valid_runtime_cursor"},
		DoneWhen:       []string{"runtime and journal reconcile successfully"},
		Blocked:        true,
		Blocker:        "the Runtime snapshot could not be safely reconciled",
		Instruction:    "",
		Recovery:       []string{"read docs/control/agent-protocol.md#cursor-mapping", "read " + loopManualRef + " (fallback: " + loopManualFallbackRef + ")", "run loop-harness runtime reconcile --root ."},
		Automation:     []string{"normal continuation is suspended until the Runtime cursor is reconciled"},
	}
	guidance.Instruction = formatGuidanceInstruction(*guidance)
	return guidance
}

// BuildGuidanceForState is the exported wrapper around buildGuidance used
// by the Controller control cycle (internal/controller). It exists so the
// cycle can reuse the projection logic without forcing the cli package to
// be imported by controller.
func BuildGuidanceForState(root string, state map[string]any, event string, input policy.Input) policy.Guidance {
	return buildGuidance(root, state, event, input)
}

// RefreshMilestoneForController is the exported wrapper around
// refreshMilestone used by the Controller control cycle. It commits the
// controller checkpoint through the same CAS Store used by lifecycle
// transitions and returns the post-CAS snapshot.
func RefreshMilestoneForController(root, statePath, journalPath string, snapshot runtime.Snapshot, guidance policy.Guidance, event string) (runtime.Snapshot, bool, error) {
	return refreshMilestone(root, statePath, journalPath, snapshot, guidance, event)
}

// ReconcileGuidanceForController is the exported wrapper around
// reconcileGuidance. The cycle uses it to rebuild Guidance+Milestone after
// a successful PreToolUse transition.
func ReconcileGuidanceForController(root, event string, input policy.Input) (policy.Guidance, runtime.Snapshot, error) {
	return reconcileGuidance(root, event, input)
}

// FallbackGuidanceForController returns the recovery Guidance the cycle
// surfaces when the Runtime snapshot cannot be safely read.
func FallbackGuidanceForController(event string) *policy.Guidance {
	return fallbackGuidance(event)
}

// FreshStartGuidanceForController returns the S0 bootstrap Guidance for a
// fresh checkout (no runtime state file yet) — not a blocked condition.
func FreshStartGuidanceForController(root, event string) *policy.Guidance {
	return freshStartGuidance(root, event)
}

// HandleTeammateIdleForController projects the L4 scheduling decision into
// the Guidance packet. It does NOT mutate runtime state: the fake-wake
// `state=activated` CAS, the idle-time `next-task` allocation, and the
// close-out CAS were retired in §15.2 P0-4 / P2-5. Real platform wake is
// `stopidle.go` (exit 2); the scheduler — not the Hook — owns next-task
// allocation; close-out belongs to the team lifecycle, not TeammateIdle.
//
// Branch table (L4 §10.2 / §15.2 P0-4):
//
//  1. assignment not complete, no blocker         → re-wake same teammate (guidance only)
//  2. assignment complete but no completion report → Guidance demanding report
//  3. assignment blocked but no blocker report     → Guidance demanding blocker report
//  4. assignment complete AND reported            → idle, await consumer; scheduler allocates next
//  5. no remaining tasks                          → idle, scheduler closes the Team
//
// The Handler keeps the branch classification so the Agent still sees a
// decision-specific Action / Missing / Automation packet, but every branch
// projects to read-only Guidance now — Runtime mutation happens elsewhere
// (canonical Result consumption, scheduler dispatch, team lifecycle).
func HandleTeammateIdleForController(root string, snapshot runtime.Snapshot, loaded *hookctx.LoadedContext, event string, input policy.Input) (policy.Guidance, runtime.Snapshot, error) {
	if !isGuidanceEvent(event) {
		return policy.Guidance{}, snapshot, fmt.Errorf("HandleTeammateIdle: %q is not a guidance event", event)
	}
	teammate := findIdleTeammate(snapshot.State, input.AgentID)
	if teammate == nil {
		// No teammate match — fall through to the read-only projection so
		// the Agent still sees a Recovery packet instead of a silent miss.
		guidance := buildGuidance(root, snapshot.State, event, input)
		return guidance, snapshot, nil
	}

	assignment := findAssignmentForTeammate(loaded, teammate)
	decision := classifyTeammateDecision(teammate, assignment)
	// Idle is guidance-only: the scheduler (not the Hook) is the only
	// writer of a new assignment, so the previously "allocate next task"
	// branch is rewritten into an idle-allow packet that names the next
	// step without performing a CAS (L4 §15.1 / §15.2 P2-5). The other
	// branches stay intact (resume / demand report / demand blocker);
	// only the idle-allow kind is rewritten so the Agent sees the
	// scheduler-owned next step instead of a (now removed) Hook CAS.
	if decision.kind == teammateIdleAwaitingConsumer {
		decision = idleAfterCompletionDecision(decision)
	}

	guidance := buildGuidanceFromDecision(root, snapshot.State, event, input, decision)
	return guidance, snapshot, nil
}

// HandleSubagentStopForController is the explicit Main task-integrate handler.
// Lifecycle hooks never invoke this mutation path.
func HandleSubagentStopForController(ctx context.Context, root string, snapshot runtime.Snapshot, loaded *hookctx.LoadedContext, event string, input policy.Input) (policy.Guidance, runtime.Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	if !isGuidanceEvent(event) {
		return policy.Guidance{}, snapshot, fmt.Errorf("HandleSubagentStop: %q is not a guidance event", event)
	}
	assignment := findAssignmentForInput(loaded, input)
	if assignment == nil {
		// No matching assignment — fall through to the text projection so
		// the caller still receives a Recovery packet.
		guidance := buildGuidance(root, snapshot.State, event, input)
		return guidance, snapshot, nil
	}
	correctionAction := "re-wake the same subagent to remediate the blockers and resubmit the report"
	if assignment.AssignmentRecord != nil {
		correctionAction = fmt.Sprintf("Main: preserve the failed checkpoint. If source changes or a new report are required, author a fresh manifest and use runtime register-workgroup --revise-assignment %s --revision-intent-key <new-intent> with the original locked TASK; do not reopen this Record. For an environment-only failure, retry the original integration after resolving the cause.", assignment.AssignmentID)
	}
	if nativeS6Assignment(snapshot.State, assignment.OwnerAgentID) {
		correctionAction = fmt.Sprintf("Main: preserve the failed checkpoint. Source/report correction uses s6 prepare --task %s --role %s --revise-assignment %s --revision-intent-key <stable-key>, then Claude Code native execution; no platform Team or launcher. For an environment-only failure, retry the original integration after resolving the cause.", assignment.TaskID, assignment.RoleFamily, assignment.AssignmentID)
	}
	if packetBackedRepairResultStage(snapshot.State) {
		packet, found, err := typedRepairResultPacketForAssignment(root, snapshot.State, assignment.AssignmentID, assignment.OwnerAgentID)
		if err != nil {
			return policy.Guidance{}, snapshot, err
		}
		if found {
			if packet.Binding.Sources.OwnerAgentID != assignment.OwnerAgentID {
				return policy.Guidance{}, snapshot, fmt.Errorf("typed S9 packet owner differs from the current registered Assignment")
			}
			guidance := buildGuidance(root, snapshot.State, event, input)
			guidance.Blocked = true
			guidance.HumanRequired = false
			guidance.Missing = appendUnique(guidance.Missing, "typed_s9_repair_result_candidate")
			if err := runtime.VerifyRepairResultPacketIssue(root, snapshot.State, packet); err != nil {
				return policy.Guidance{}, snapshot, fmt.Errorf("typed S9 packet proof failed; preserve the Worker: %w", err)
			}
			candidate, accepted, err := runtime.VerifiedRepairResultCandidateReceipt(root, snapshot.State, packet.ID, assignment.OwnerAgentID)
			if err != nil {
				return policy.Guidance{}, snapshot, err
			}
			if !accepted {
				guidance.Blocker = "typed S9 answer has not been received by the original workspace_updated Writer"
				guidance.Action = "keep the Builder active and save the exact issued RepairResult through the platform PostToolUse Hook"
			} else {
				if candidate.Provenance.PacketID != packet.ID || candidate.Provenance.OwnerAgentID != assignment.OwnerAgentID {
					return policy.Guidance{}, snapshot, fmt.Errorf("typed S9 candidate receipt differs from the exact Assignment owner")
				}
				guidance.Blocker = "typed S9 candidate must pass the original owner-check and Main integration path"
				guidance.Action = fmt.Sprintf("Main: run `runtime task-integrate --root %s --assignment-id %s --agent-id %s`; it routes through the original workspace Inspect/Integrate/Result Writer", root, packet.Binding.Sources.ManifestAssignmentID, assignment.OwnerAgentID)
			}
			guidance.Instruction = formatGuidanceInstruction(guidance)
			return guidance, snapshot, nil
		}
	}
	ctx, release, lockErr := integration.LockWorkspace(ctx, root)
	if lockErr != nil {
		return policy.Guidance{}, snapshot, lockErr
	}
	defer release()
	if assignment.WorktreePath == "" || assignment.Branch == "" {
		guidance := buildGuidance(root, snapshot.State, event, input)
		guidance.Blocked = true
		guidance.Blocker = "subagent assignment lacks worktree_path or branch"
		guidance.Missing = appendUnique(guidance.Missing, "worktree_metadata")
		guidance.Action = "record worktree_path and branch in the assignment manifest before stopping"
		guidance.Instruction = formatGuidanceInstruction(guidance)
		return guidance, snapshot, nil
	}

	root = filepath.Clean(root)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	binding, bindingErr := workspace.Decode(snapshot.State)
	if bindingErr != nil {
		return policy.Guidance{}, snapshot, bindingErr
	}
	if binding != nil && (assignment.AssignmentRecord == nil || !nativeS6Assignment(snapshot.State, assignment.OwnerAgentID)) {
		if err := binding.Validate(ctx, root); err != nil {
			return policy.Guidance{}, snapshot, err
		}
		execution, ok := binding.Execution(assignment.AssignmentID, loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration)
		if !ok || execution.AgentID != assignment.OwnerAgentID || execution.Path != assignment.WorktreePath || execution.Branch != assignment.Branch || execution.TargetBranch != assignment.TargetBranch {
			return policy.Guidance{}, snapshot, fmt.Errorf("assignment execution binding mismatch")
		}
		if err := binding.CleanInputs(ctx); err != nil {
			return policy.Guidance{}, snapshot, err
		}
	}
	targetBranch, targetErr := fileview.DevelopmentRef(snapshot.State)
	if targetErr != nil {
		return policy.Guidance{}, snapshot, targetErr
	}
	targetBranch = strings.TrimPrefix(targetBranch, "refs/heads/")
	if assignment.TargetBranch != "" && strings.TrimPrefix(assignment.TargetBranch, "refs/heads/") != targetBranch {
		return policy.Guidance{}, snapshot, fmt.Errorf("assignment target %s differs from bound development branch %s; reconcile the assignment", assignment.TargetBranch, targetBranch)
	}

	// Consult the receipt before inspection: received branches have no new
	// commits beyond the merge base, and cleanup may already have removed the path.
	priorPath, err := checkpointPathForLoadedAssignment(root, loaded, *assignment)
	if err != nil {
		return policy.Guidance{}, snapshot, err
	}
	priorCheckpoint, priorFound, err := integration.DefaultCheckpointStore().Load(priorPath)
	if err != nil {
		return policy.Guidance{}, snapshot, err
	}
	currentRecord, canonicalAssignment, err := hookctx.ResolveAssignmentRecordProjection(root, snapshot.State, *assignment)
	if err != nil {
		return policy.Guidance{}, snapshot, fmt.Errorf("resolve current Assignment before checkpoint recovery: %w", err)
	}
	if priorFound && canonicalAssignment {
		if err := integration.ValidateCheckpointRecordBinding(priorCheckpoint, currentRecord); err != nil {
			return policy.Guidance{}, snapshot, fmt.Errorf("current revision checkpoint recovery: %w", err)
		}
	}
	prior := ""
	if priorFound {
		prior = priorCheckpoint.State
	}
	if priorFound && priorCheckpoint.State == integration.StateReady {
		merge, recoverErr := integration.RecoverUnrecordedMerge(ctx, root, priorCheckpoint)
		if recoverErr != nil {
			return policy.Guidance{}, snapshot, recoverErr
		}
		if merge != "" {
			recovered := priorCheckpoint
			recovered.MergeCommit, recovered.State = merge, integration.StateMerged
			recovered.FailureReason, recovered.LastErrorCode = "", ""
			written, err := integration.DefaultCheckpointStore().CompareAndSwap(priorPath, priorCheckpoint, recovered)
			if err != nil {
				return policy.Guidance{}, snapshot, err
			}
			priorCheckpoint, prior = written, written.State
		}
	}

	resumeMerged := priorFound && priorCheckpoint.MergeCommit != ""
	if resumeMerged {
		head, err := fileview.Resolve(root, "refs/heads/"+assignment.Branch)
		if err != nil || head != priorCheckpoint.SourceHead {
			return policy.Guidance{}, snapshot, fmt.Errorf("source changed after reception; preserve and reconcile")
		}
	}
	acknowledge, cleanup := assignment.CompletionAckRef != "", assignment.CompletionAckRef != ""
	if prior == integration.StateVerified ||
		prior == integration.StateAcknowledged ||
		prior == integration.StateCleanupPending || prior == integration.StateComplete {
		acknowledge, cleanup = assignment.CompletionAckRef != "", assignment.CompletionAckRef != ""
	}

	if _, err := os.Stat(assignment.WorktreePath); err == nil {
		if err := workspace.ValidateCheckout(root, assignment.WorktreePath); err != nil {
			return policy.Guidance{}, snapshot, err
		}
	}

	inspectReq := integration.InspectRequest{
		Root:               root,
		Assignment:         *assignment,
		TargetBranch:       targetBranch,
		BaselineGeneration: loaded.BaselineGeneration,
		RuntimeID:          loaded.PolicyContext.RuntimeID,
	}
	for _, artifact := range loaded.PolicyContext.LockedArtifacts {
		inspectReq.LockedPaths = append(inspectReq.LockedPaths, artifact.Path)
	}
	recoveryBase := ""
	retry := input.Facts["integration_retry"]
	if retry {
		cpPath := priorPath
		cp, found, err := integration.DefaultCheckpointStore().Load(cpPath)
		if err != nil {
			return policy.Guidance{}, snapshot, err
		}
		if !found || (cp.State != integration.StatePreserved && cp.State != integration.StateBlocked && cp.State != integration.StateMerged) {
			return policy.Guidance{}, snapshot, fmt.Errorf("retry requires an existing preserved/blocked/merged checkpoint")
		}
		if cp.AssignmentID != assignment.AssignmentID || cp.SourceBranch != assignment.Branch || cp.TargetBranch != targetBranch || cp.BaselineGeneration != loaded.BaselineGeneration || cp.WorktreePath != assignment.WorktreePath {
			return policy.Guidance{}, snapshot, fmt.Errorf("retry checkpoint coordinates or original scope missing/mismatched")
		}
		// A builder may append a corrective commit, but cannot replace the
		// historical source ancestry when reusing this assignment.
		if cp.SourceHead != "" {
			cmd := exec.CommandContext(ctx, "git", "-C", root, "merge-base", "--is-ancestor", cp.SourceHead, assignment.Branch)
			if err := cmd.Run(); err != nil {
				return policy.Guidance{}, snapshot, fmt.Errorf("retry source no longer descends from recorded source head")
			}
		} else if cp.MergeCommit != "" || cp.MergeBase != "" {
			return policy.Guidance{}, snapshot, fmt.Errorf("retry checkpoint has incomplete historical source identity")
		}
		recoveryBase = cp.MergeBase
		if cp.MergeCommit != "" {
			recoveryBase, err = integration.RecoveryBaseForCheckpoint(ctx, root, cp)
			if err != nil {
				return policy.Guidance{}, snapshot, err
			}
		}
	}
	inspectConfig, err := integration.AssignmentInspectConfig(*assignment)
	if err != nil {
		return policy.Guidance{}, snapshot, err
	}
	inspectConfig.RecoveryBase = recoveryBase
	inspectConfig.CheckRunner = integration.ProfileCheckRunner(assignment.CheckProfiles, checkexec.Binding{RuntimeID: loaded.PolicyContext.RuntimeID, Generation: workspace.Generation(snapshot.State), AssignmentID: assignment.AssignmentID, Owner: assignment.OwnerAgentID, Responsibility: "main_required_integration"})
	var inspectResult integration.Inspection
	reinspectReady := false
	if prior == integration.StateReady {
		currentTarget, resolveErr := fileview.Resolve(root, "refs/heads/"+targetBranch)
		if resolveErr != nil {
			return policy.Guidance{}, snapshot, resolveErr
		}
		reinspectReady = currentTarget != priorCheckpoint.TargetHead
		if reinspectReady {
			if priorCheckpoint.MergeBase == "" || exec.CommandContext(ctx, "git", "-C", root, "merge-base", "--is-ancestor", priorCheckpoint.TargetHead, currentTarget).Run() != nil {
				return policy.Guidance{}, snapshot, fmt.Errorf("ready target history changed; reconcile before retry")
			}
			inspectConfig.RecoveryBase = priorCheckpoint.MergeBase
		}
	}
	if (prior == integration.StateReady && !reinspectReady) || prior == integration.StateMerged || prior == integration.StateVerified || prior == integration.StateAcknowledged || prior == integration.StateCleanupPending || prior == integration.StateComplete {
		cpPath := priorPath
		cp, found, loadErr := integration.DefaultCheckpointStore().Load(cpPath)
		if loadErr != nil || !found {
			return policy.Guidance{}, snapshot, fmt.Errorf("load cleanup checkpoint: %v", loadErr)
		}
		if cp.AssignmentID != assignment.AssignmentID || cp.WorktreePath != assignment.WorktreePath || cp.SourceBranch != assignment.Branch || cp.TargetBranch != targetBranch || cp.BaselineGeneration != loaded.BaselineGeneration {
			return policy.Guidance{}, snapshot, fmt.Errorf("cleanup assignment identity mismatch")
		}
		inspectResult = integration.InspectionFromCheckpoint(cp)
		inspectResult, err = integration.RefreshCompletionBinding(root, loaded.PolicyContext.RuntimeID, assignment.CompletionRef, inspectResult)
	} else {
		inspectResult, err = integration.Inspect(ctx, inspectReq, inspectConfig)
	}
	if err != nil {
		guidance := buildGuidance(root, snapshot.State, event, input)
		guidance.Blocked = true
		guidance.Blocker = "worktree inspection failed: " + err.Error()
		guidance.Missing = appendUnique(guidance.Missing, "worktree_inspect")
		guidance.Action = "repair the worktree state and retry runtime task-integrate"
		guidance.Instruction = formatGuidanceInstruction(guidance)
		return guidance, snapshot, nil
	}

	if reinspectReady && inspectResult.Ready {
		if inspectResult.SourceHead != priorCheckpoint.SourceHead {
			return policy.Guidance{}, snapshot, fmt.Errorf("ready source changed; reconcile before retry")
		}
		refreshed := priorCheckpoint
		refreshed.TargetHead = inspectResult.TargetHead
		written, writeErr := integration.DefaultCheckpointStore().CompareAndSwap(priorPath, priorCheckpoint, refreshed)
		if writeErr != nil {
			return policy.Guidance{}, snapshot, writeErr
		}
		priorCheckpoint = written
	}
	if !inspectResult.Ready {
		if resumeMerged || prior == integration.StateVerified || prior == integration.StateAcknowledged || prior == integration.StateCleanupPending || prior == integration.StateComplete {
			// Post-merge follow-up: force Ready so Integrate resumes
			// ack/cleanup without re-running the merge gate (BUG-039-38).
			inspectResult = inspectionForAckCleanup(*assignment, inspectResult, targetBranch, loaded.BaselineGeneration)
			if inspectResult.SourceHead == "" {
				inspectResult.SourceHead = priorCheckpoint.SourceHead
			}
		} else {
			// Preserve rejected pre-merge inspections in the Integrator's
			// original durable store. Milestone text alone cannot drive rework.
			if _, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspectResult, ExpectedRevision: int64(snapshot.Revision)}, integration.IntegrateConfig{Root: root, GitRoot: root, RuntimeID: loaded.PolicyContext.RuntimeID}); err != nil {
				return policy.Guidance{}, snapshot, err
			}
			guidance := buildGuidance(root, snapshot.State, event, input)
			guidance.Integration = appendUniqueStrings(guidance.Integration, inspectResult.Blockers...)
			guidance.Blocked = true
			guidance.Blocker = "worktree integration is not ready; preserving worktree and branch"
			guidance.Missing = appendUniqueStrings(guidance.Missing, inspectResult.Blockers...)
			guidance.Action = correctionAction
			guidance.Instruction = formatGuidanceInstruction(guidance)
			// Surface the failed inspection via the Milestone so the next
			// event can reconcile from a durable record.
			updated, _, err := persistSubagentCheckpoint(root, statePath, journalPath, snapshot, &inspectResult, targetBranch, event, "preserved")
			if err != nil && !errors.Is(err, runtime.ErrStaleRevision) {
				return guidance, snapshot, err
			}
			if updated.Revision != 0 {
				snapshot = updated
			}
			refreshed, _, err := refreshMilestone(root, statePath, journalPath, snapshot, guidance, event)
			if err != nil && !errors.Is(err, runtime.ErrStaleRevision) {
				return guidance, snapshot, err
			}
			if refreshed.Revision != 0 {
				snapshot = refreshed
				guidance = buildGuidance(root, snapshot.State, event, input)
				guidance.Integration = appendUniqueStrings(guidance.Integration, inspectResult.Blockers...)
				guidance.Blocked = true
				guidance.Blocker = "worktree integration is not ready; preserving worktree and branch"
				guidance.Missing = appendUniqueStrings(guidance.Missing, inspectResult.Blockers...)
				guidance.Action = correctionAction
				guidance.Instruction = formatGuidanceInstruction(guidance)
			}
			return guidance, snapshot, nil
		}
	}

	// Ensure checkpoint identity uses assignment_id even if Inspect
	// somehow omitted it (store + loadPrior share this key).
	if inspectResult.AssignmentID == "" {
		inspectResult.AssignmentID = assignment.AssignmentID
	}

	integrateReq := integration.IntegrateRequest{
		RetryPreserved:   retry,
		Inspection:       inspectResult,
		ExpectedRevision: int64(snapshot.Revision),
		Acknowledge:      acknowledge,
		Cleanup:          cleanup,
	}
	integrateConfig := integration.IntegrateConfig{
		Root:      root,
		GitRoot:   root,
		RuntimeID: loaded.PolicyContext.RuntimeID,
		// Delivery checks always run on the merged target. The manifest mode
		// controls only whether legacy pre-merge checks also run.
		CheckRunner:    integration.ProfileCheckRunner(assignment.CheckProfiles, checkexec.Binding{RuntimeID: loaded.PolicyContext.RuntimeID, Generation: workspace.Generation(snapshot.State), AssignmentID: assignment.AssignmentID, Owner: assignment.OwnerAgentID, Responsibility: "main_required_integration"}),
		RequiredChecks: assignment.RequiredChecks,
	}
	integrationResult, err := integration.Integrate(ctx, integrateReq, integrateConfig)
	if err == nil && integrationResult.Checkpoint.State == integration.StateVerified {
		snapshot, err = acknowledgeVerifiedIntegration(root, snapshot, assignment, integrationResult.Checkpoint)
		if err == nil {
			integrateReq.Inspection = integration.InspectionFromCheckpoint(integrationResult.Checkpoint)
			integrateReq.Acknowledge, integrateReq.Cleanup = true, true
			integrateReq.RetryPreserved = false
			integrationResult, err = integration.Integrate(ctx, integrateReq, integrateConfig)
		}
	}

	if errors.Is(err, integration.ErrCleanupPending) {
		// Verification remains durable. Report cleanup separately and project
		// cleanup_pending, never overwrite it with preserved.
		updated, _, perr := persistSubagentCheckpoint(root, statePath, journalPath, snapshot, &inspectResult, targetBranch, event, integration.StateCleanupPending)
		if perr != nil {
			return policy.Guidance{}, snapshot, perr
		}
		guidance := buildGuidance(root, updated.State, event, input)
		guidance.Action = "integration verified; cleanup pending: preserve and inspect remaining worktree files before retrying cleanup"
		guidance.Integration = appendUniqueStrings(guidance.Integration, err.Error(), "checkpoint_state=cleanup_pending")
		guidance.Instruction = formatGuidanceInstruction(guidance)
		return guidance, updated, nil
	}
	if err != nil {
		// Dirty / conflict preserve paths still return a checkpoint; surface
		// them as blocked Guidance rather than claiming a successful merge
		// (BUG-039-37: untracked harness files previously tripped dirty).
		if errors.Is(err, integration.ErrDirtyWorktree) || errors.Is(err, integration.ErrMergeConflict) || errors.Is(err, integration.ErrCheckFailed) {
			guidance := buildGuidance(root, snapshot.State, event, input)
			guidance.Blocked = true
			guidance.Blocker = "worktree integration preserved: " + err.Error()
			guidance.Missing = appendUnique(guidance.Missing, "integration_preserve")
			guidance.Integration = appendUniqueStrings(guidance.Integration, err.Error())
			if integrationResult.Checkpoint.State != "" {
				guidance.Integration = appendUniqueStrings(guidance.Integration,
					fmt.Sprintf("checkpoint_state=%s", integrationResult.Checkpoint.State))
			}
			guidance.Action = "preserve the worktree and branch; remediate the conflict or dirty tree before retrying runtime task-integrate"
			if errors.Is(err, integration.ErrCheckFailed) {
				guidance.Blocker = integrationResult.Checkpoint.FailureReason
				guidance.Action = "preserve the worktree and branch; inspect the required-check receipt and log, correct the execution environment or failing check, then retry the integration; do not weaken required checks"
			}
			guidance.Instruction = formatGuidanceInstruction(guidance)
			updated, _, perr := persistSubagentCheckpoint(root, statePath, journalPath, snapshot, &inspectResult, targetBranch, event, "preserved")
			if perr == nil && updated.Revision != 0 {
				snapshot = updated
			}
			return guidance, snapshot, nil
		}
		guidance := buildGuidance(root, snapshot.State, event, input)
		guidance.Blocked = true
		guidance.Blocker = "integration failed: " + err.Error()
		guidance.Missing = appendUnique(guidance.Missing, "integration_failure")
		guidance.Action = "investigate the integration failure; the worktree is preserved"
		guidance.Instruction = formatGuidanceInstruction(guidance)
		return guidance, snapshot, nil
	}

	if integrationResult.Checkpoint.State == integration.StatePreserved || integrationResult.Checkpoint.State == integration.StateBlocked {
		guidance := buildGuidance(root, snapshot.State, event, input)
		guidance.Blocked = true
		guidance.Blocker = "existing integration checkpoint is " + integrationResult.Checkpoint.State + ": " + integrationResult.Checkpoint.FailureReason
		guidance.Action = "resolve the recorded blocker, then run runtime task-integrate --assignment-id " + assignment.AssignmentID + " --retry-preserved"
		guidance.Integration = []string{"checkpoint_state=" + integrationResult.Checkpoint.State}
		guidance.Instruction = formatGuidanceInstruction(guidance)
		return guidance, snapshot, nil
	}
	integratedState := integrationResult.Checkpoint.State
	finishedNative := integratedState == integration.StateComplete && nativeS6Assignment(snapshot.State, assignment.OwnerAgentID)
	if integrationResult.Checkpoint.State == integration.StateVerified {
		integratedState = "verified"
	} else if integrationResult.Checkpoint.State == integration.StateAcknowledged {
		integratedState = "acknowledged"
	} else if integrationResult.Checkpoint.State == integration.StateCleanupPending {
		integratedState = "cleanup_pending"
	} else if integrationResult.Checkpoint.State == integration.StateComplete {
		integratedState = "complete"
	}

	updated, _, err := persistSubagentCheckpoint(root, statePath, journalPath, snapshot, &inspectResult, targetBranch, event, integratedState)
	if err != nil && integratedState == integration.StateComplete {
		return policy.Guidance{}, snapshot, fmt.Errorf("integration cleaned up; retry to finish Runtime completion projection: %w", err)
	}
	if err != nil && !errors.Is(err, runtime.ErrStaleRevision) {
		// Merge/verify already committed in git + durable integrator
		// checkpoint. A Milestone CAS schema miss must not hide the
		// successful integrate from the Hook (BUG-039-37 / CT-039-09).
		guidance := buildGuidance(root, snapshot.State, event, input)
		guidance.Blocked = false
		guidance.Blocker = ""
		guidance.Integration = appendUniqueStrings(guidance.Integration,
			fmt.Sprintf("worktree integrated to state=%s", integratedState),
			"milestone persist deferred: "+err.Error(),
		)
		if finishedNative || prior == integration.StateVerified || prior == integration.StateAcknowledged || prior == integration.StateCleanupPending || prior == integration.StateComplete {
			guidance.Integration = appendUniqueStrings(guidance.Integration,
				"completion_ack recorded; worktree cleanup follow-up applied",
			)
			guidance.Action = fmt.Sprintf("worktree integration advanced to %s", integratedState)
		} else {
			guidance.Integration = appendUniqueStrings(guidance.Integration,
				"retry runtime task-integrate to finish acknowledgement and cleanup",
			)
			guidance.Action = fmt.Sprintf("worktree integration advanced to %s; preserve worktree until acknowledged", integratedState)
		}
		guidance.Instruction = formatGuidanceInstruction(guidance)
		return guidance, snapshot, nil
	}
	if updated.Revision != 0 {
		snapshot = updated
	}

	guidance := buildGuidance(root, snapshot.State, event, input)
	guidance.Blocked = false
	guidance.Blocker = ""
	if finishedNative || prior == integration.StateVerified || prior == integration.StateAcknowledged || prior == integration.StateCleanupPending || prior == integration.StateComplete {
		guidance.Integration = appendUniqueStrings(guidance.Integration,
			fmt.Sprintf("worktree integrated to state=%s", integratedState),
			"completion_ack recorded; worktree cleanup follow-up applied",
		)
		guidance.Action = fmt.Sprintf("worktree integration advanced to %s", integratedState)
	} else {
		guidance.Integration = appendUniqueStrings(guidance.Integration,
			fmt.Sprintf("worktree integrated to state=%s", integratedState),
			"retry runtime task-integrate to finish acknowledgement and cleanup",
		)
		guidance.Action = fmt.Sprintf("worktree integration advanced to %s; preserve worktree until acknowledged", integratedState)
	}
	guidance.Instruction = formatGuidanceInstruction(guidance)

	refreshed, _, err := refreshMilestone(root, statePath, journalPath, snapshot, guidance, event)
	if err != nil && !errors.Is(err, runtime.ErrStaleRevision) {
		return guidance, snapshot, err
	}
	if refreshed.Revision != 0 {
		snapshot = refreshed
		guidance = buildGuidance(root, snapshot.State, event, input)
		guidance.Blocked = false
		guidance.Blocker = ""
		if finishedNative || prior == integration.StateVerified || prior == integration.StateAcknowledged || prior == integration.StateCleanupPending || prior == integration.StateComplete {
			guidance.Integration = appendUniqueStrings(guidance.Integration,
				fmt.Sprintf("worktree integrated to state=%s", integratedState),
				"completion_ack recorded; worktree cleanup follow-up applied",
			)
			guidance.Action = fmt.Sprintf("worktree integration advanced to %s", integratedState)
		} else {
			guidance.Integration = appendUniqueStrings(guidance.Integration,
				fmt.Sprintf("worktree integrated to state=%s", integratedState),
				"retry runtime task-integrate to finish acknowledgement and cleanup",
			)
			guidance.Action = fmt.Sprintf("worktree integration advanced to %s; preserve worktree until acknowledged", integratedState)
		}
		guidance.Instruction = formatGuidanceInstruction(guidance)
	}
	return guidance, snapshot, nil
}

func packetBackedRepairResultStage(state map[string]any) bool {
	if !runtime.PacketEnabled(state) {
		return false
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	return lifecycle["state"] == "bug_resolution" && lifecycle["phase"] == "fixing"
}

func typedRepairResultPacketForAssignment(root string, state map[string]any, assignmentID, owner string) (workpacket.RepairResultPacket, bool, error) {
	packets, err := runtime.RepairResultPacketsContext(runtime.WithPacketBodyRoot(context.Background(), root), state)
	if err != nil {
		return workpacket.RepairResultPacket{}, false, err
	}
	reviewMap, _ := state["review"].(map[string]any)
	pointer, _ := reviewMap["repair"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	if pointer == nil {
		return workpacket.RepairResultPacket{}, false, nil
	}
	runtimeID, _ := state["runtime_id"].(string)
	sessionID, _ := pointer["session_id"].(string)
	planRef, _ := pointer["plan_ref"].(string)
	planSHA, _ := pointer["plan_sha256"].(string)
	currentGeneration := integerValue(baseline["generation"])
	currentRound := integerValue(reviewMap["round"])
	if runtimeID == "" || sessionID == "" || planRef == "" || planSHA == "" || currentGeneration <= 0 || currentRound <= 0 {
		return workpacket.RepairResultPacket{}, false, nil
	}
	allMatches := make([]workpacket.RepairResultPacket, 0, 1)
	for _, packet := range packets {
		binding := packet.Binding.Sources
		if binding.RuntimeID != runtimeID || binding.BaselineGeneration != currentGeneration || binding.ReviewRound != currentRound ||
			binding.SessionID != sessionID || binding.PlanRef != planRef || binding.PlanSHA256 != planSHA ||
			binding.ManifestAssignmentID != assignmentID && binding.RepairAssignmentID != assignmentID {
			continue
		}
		allMatches = append(allMatches, packet)
	}
	if len(allMatches) == 0 {
		return workpacket.RepairResultPacket{}, false, nil
	}
	// Prefer the packet whose frozen attempt is still current. A packet from
	// this same canonical Session/Plan still fences the legacy Result route if
	// its source or rework attempt has gone stale; it must remain on the typed
	// preservation path until a new packet is issued.
	current, err := runtime.CurrentRepairResultPackets(root, state, "")
	if err != nil {
		return workpacket.RepairResultPacket{}, false, err
	}
	var selected *workpacket.RepairResultPacket
	for _, candidate := range allMatches {
		for _, currentPacket := range current {
			if currentPacket.ID != candidate.ID {
				continue
			}
			if selected != nil {
				return workpacket.RepairResultPacket{}, false, fmt.Errorf("S9 Assignment %s has multiple current typed RepairResult packets", assignmentID)
			}
			copyPacket := candidate
			selected = &copyPacket
		}
	}
	if selected != nil {
		return *selected, true, nil
	}
	if len(allMatches) != 1 {
		return workpacket.RepairResultPacket{}, false, fmt.Errorf("S9 Assignment %s has multiple historical typed attempts and no unique current packet; preserve the original packets and use named recovery", assignmentID)
	}
	return allMatches[0], true, nil
}

func typedRepairResultExecution(registry *workspace.ExecutionRegistry, state map[string]any, packet workpacket.RepairResultPacket) (workspace.Execution, bool, error) {
	if registry == nil {
		return workspace.Execution{}, false, fmt.Errorf("workspace registry is missing")
	}
	if err := registry.ValidateAuthority(state); err != nil {
		return workspace.Execution{}, false, err
	}
	binding := packet.Binding.Sources
	var selected *workspace.Execution
	for _, id := range []string{binding.ManifestAssignmentID, binding.RepairAssignmentID} {
		if id == "" {
			continue
		}
		execution, ok := registry.Execution(id, binding.RuntimeID, binding.BaselineGeneration)
		if !ok {
			continue
		}
		if execution.AssignmentID != id || execution.AgentID != binding.OwnerAgentID || execution.Status != "ready" && execution.Status != "complete" || execution.ReworkRef != packet.Binding.ReworkRef {
			return workspace.Execution{}, false, fmt.Errorf("S9 typed packet execution owner/status/rework differs")
		}
		if selected != nil && (selected.AssignmentID != execution.AssignmentID || filepath.Clean(selected.Path) != filepath.Clean(execution.Path)) {
			return workspace.Execution{}, false, fmt.Errorf("S9 typed packet has multiple registered Worker executions")
		}
		copyExecution := execution
		selected = &copyExecution
	}
	if selected == nil {
		return workspace.Execution{}, false, nil
	}
	return *selected, true, nil
}

// inspectionForAckCleanup builds a Ready Inspection for the post-merge
// acknowledge+cleanup Integrate call when Inspect fails solely because
// the source branch no longer has commits beyond the merge base
// (BUG-039-38). Fields already populated by Inspect are preserved;
// AssignmentID / worktree coords are filled from the assignment.
func inspectionForAckCleanup(assignment hookctx.AssignmentContext, inspected integration.Inspection, targetBranch string, generation int) integration.Inspection {
	out := inspected
	out.Ready = true
	out.Blockers = nil
	if out.AssignmentID == "" {
		out.AssignmentID = assignment.AssignmentID
	}
	if out.WorktreePath == "" {
		out.WorktreePath = assignment.WorktreePath
	}
	if out.SourceBranch == "" {
		out.SourceBranch = assignment.Branch
	}
	if out.TargetBranch == "" {
		out.TargetBranch = targetBranch
	}
	if out.BaselineGeneration == 0 {
		out.BaselineGeneration = generation
	}
	out.NonSquashMode = true
	return out
}

// teammateDecision enumerates the TeammateIdle scheduling branches that
// still need a distinct Guidance projection. Idle is guidance-only
// (L4 §15.2 P0-4 / §15.2 P2-5): the resume branch no longer CAS-writes
// `state=activated` and idle never allocates the next task — that is the
// scheduler's job. The four retained kinds map one-to-one to the §10.2
// decision matrix rows that the Hook still needs to render.
type teammateDecisionKind int

const (
	teammateResume teammateDecisionKind = iota + 1
	teammateDemandCompletionReport
	teammateDemandBlockerReport
	teammateIdleAwaitingConsumer
)

type teammateDecision struct {
	kind             teammateDecisionKind
	teammateID       string
	teammateStatus   string
	assignmentID     string
	taskID           string
	reportState      string
	blocked          bool
	missingReports   []string
	nextTaskID       string
	worktreeRecovery bool
	teamCompleted    bool
	automation       []string
	missing          []string
	action           string
	blocker          string
	blockedFlag      bool
}

func classifyTeammateDecision(teammate *teammateRow, assignment *hookctx.AssignmentContext) teammateDecision {
	decision := teammateDecision{
		teammateID:     teammate.ID,
		teammateStatus: teammate.Status,
		automation: []string{
			"teammate scheduling decision is derived from assignment + task list state, not generic guidance",
			"do not spawn a replacement teammate without cause",
		},
	}
	if assignment != nil {
		decision.assignmentID = assignment.AssignmentID
		decision.taskID = assignment.TaskID
		decision.reportState = assignment.ReportStatus
	}

	complete := isAssignmentComplete(assignment)
	blocked := isAssignmentBlocked(assignment)

	switch {
	case !complete && blocked && !hasBlockerReport(assignment):
		decision.kind = teammateDemandBlockerReport
		decision.blockedFlag = true
		decision.blocker = "teammate is blocked but has not submitted a blocker report"
		decision.missingReports = []string{"blocker_report"}
		decision.missing = []string{"blocker_report"}
		decision.action = "submit blocker.json describing the unreported blocker, then re-wake the same teammate"
		return decision
	case complete && !hasCompletionReport(assignment):
		decision.kind = teammateDemandCompletionReport
		decision.blockedFlag = true
		decision.blocker = "teammate marked assignment complete but completion report is missing"
		decision.missingReports = []string{"completion_report"}
		decision.missing = []string{"completion_report"}
		decision.action = "submit completion.json to .claude/evidence/loop-REQ-039/g1/assignments/<id>/, then re-wake the same teammate"
		return decision
	case !complete && !blocked:
		decision.kind = teammateResume
		decision.action = "re-wake the same teammate with the current assignment envelope; do not spawn a replacement"
		decision.automation = append(decision.automation, "resume existing assignment instead of allocating new work")
		return decision
	case complete && hasCompletionReport(assignment):
		decision.kind = teammateIdleAwaitingConsumer
		decision.action = "acknowledge the completion report; idle is allowed — the scheduler allocates the next legal task in the same Team"
		decision.automation = append(decision.automation,
			"completion report is durable; scheduler/Main will dispatch the next assignment",
			"idle does not self-claim a Team task — wait for the next scheduler dispatch",
		)
		return decision
	default:
		// Fall-through covers the "blocked but blocker already reported"
		// case and any case where there is no remaining work. Idle is
		// allowed and the scheduler owns Team close-out; the Guidance
		// packet still names that boundary so the Agent does not invent
		// a replacement spawn.
		decision.kind = teammateIdleAwaitingConsumer
		decision.blockedFlag = false
		decision.worktreeRecovery = true
		decision.teamCompleted = true
		decision.action = "idle is allowed; the scheduler closes the Team once the durable Result is consumed — preserve worktree until the scheduler dispatches the close-out"
		return decision
	}
}

// teammateRow is the read-only projection of an agent row used by the
// TeammateIdle handler. It intentionally keeps only the fields the
// scheduler consults; full agent context lives in LoadedContext.
type teammateRow struct {
	ID            string
	Role          string
	Status        string
	TeamID        string
	WorktreePath  string
	Blocked       bool
	BlockerReason string
	TaskIDs       []string
}

func findIdleTeammate(state map[string]any, agentID string) *teammateRow {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := row["id"].(string)
		if agentID != "" && id != agentID {
			continue
		}
		projection, err := workstate.AgentProjection(row, fmt.Sprint(state["schema_version"]))
		if err != nil || projection.Status == workstate.Done || projection.Status == workstate.Cancelled {
			continue
		}
		t := &teammateRow{
			ID:     id,
			Role:   stringValue(row["role"]),
			Status: string(projection.Status),
		}
		t.TeamID = stringValue(row["team_id"])
		t.WorktreePath = stringValue(row["worktree_path"])
		t.BlockerReason = stringValue(row["blocker_reason"])
		if v, ok := row["blocked"].(bool); ok {
			t.Blocked = v
		}
		if ids, ok := row["task_ids"].([]any); ok {
			for _, id := range ids {
				if s, ok := id.(string); ok {
					t.TaskIDs = append(t.TaskIDs, s)
				}
			}
		}
		if t.Status == "idle" || t.Status == "" {
			return t
		}
		if agentID == "" && projection.Status == workstate.Active {
			// surface the first working teammate as a fallback so the
			// scheduler still emits a real decision when no explicit idle
			// row is found (legacy state may carry no explicit idle entry).
			continue
		}
		if agentID != "" {
			return t
		}
	}
	return nil
}

func findAssignmentForTeammate(loaded *hookctx.LoadedContext, teammate *teammateRow) *hookctx.AssignmentContext {
	if loaded == nil {
		return nil
	}
	for i := range loaded.Assignments {
		row := loaded.Assignments[i]
		if teammate.ID != "" && row.OwnerAgentID == teammate.ID {
			return &row
		}
	}
	for _, taskID := range teammate.TaskIDs {
		for i := range loaded.Assignments {
			row := loaded.Assignments[i]
			if row.TaskID == taskID {
				return &row
			}
		}
	}
	return nil
}

func findAssignmentForInput(loaded *hookctx.LoadedContext, input policy.Input) *hookctx.AssignmentContext {
	if loaded == nil {
		return nil
	}
	var match *hookctx.AssignmentContext
	for i := range loaded.Assignments {
		row := &loaded.Assignments[i]
		if input.TargetID != "" {
			if row.AssignmentID == input.TargetID && (input.AgentID == "" || row.OwnerAgentID == input.AgentID) {
				return row
			}
			continue
		}
		if input.AgentID != "" && row.OwnerAgentID == input.AgentID {
			if match != nil {
				return nil
			}
			match = row
		}
	}
	if match != nil {
		return match
	}
	return nil
}

func isAssignmentComplete(assignment *hookctx.AssignmentContext) bool {
	return assignment != nil && assignment.State == string(workstate.Done)
}

func isAssignmentBlocked(assignment *hookctx.AssignmentContext) bool {
	if assignment == nil {
		return false
	}
	if assignment.State == "blocked" {
		return true
	}
	if assignment.CompletionRef != "" && assignment.ReportStatus == "blocked" {
		return true
	}
	return false
}

func hasCompletionReport(assignment *hookctx.AssignmentContext) bool {
	return hook.HasCompletionReport(assignment)
}

func hasBlockerReport(assignment *hookctx.AssignmentContext) bool {
	return hook.HasBlockerReport(assignment)
}

func persistSubagentCheckpoint(root, statePath, journalPath string, snapshot runtime.Snapshot, inspection *integration.Inspection, targetBranch, event, integratedState string) (runtime.Snapshot, bool, error) {
	if snapshot.Revision == 0 {
		return snapshot, false, nil
	}
	store := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	now := time.Now().UTC()
	// The milestone schema constrains `integration` to a string array.
	// Surface the checkpoint state + blockers as compact one-line
	// strings so the durable Milestone projection stays schema-valid
	// while still preserving the worktree + branch identity.
	integrationEntries := []string{
		fmt.Sprintf("assignment_id=%s", inspection.AssignmentID),
		fmt.Sprintf("task_id=%s", inspection.TaskID),
		fmt.Sprintf("worktree=%s", inspection.WorktreePath),
		fmt.Sprintf("branch=%s", inspection.SourceBranch),
		fmt.Sprintf("target_branch=%s", targetBranch),
		fmt.Sprintf("status=%s", integratedState),
		fmt.Sprintf("source_head=%s", inspection.SourceHead),
		fmt.Sprintf("merge_base=%s", inspection.MergeBase),
	}
	if len(inspection.OutOfScopeDiff) > 0 {
		integrationEntries = append(integrationEntries, fmt.Sprintf("out_of_scope=%s", strings.Join(inspection.OutOfScopeDiff, ",")))
	}
	if len(inspection.LockedDiff) > 0 {
		integrationEntries = append(integrationEntries, fmt.Sprintf("locked_paths=%s", strings.Join(inspection.LockedDiff, ",")))
	}
	if len(inspection.Blockers) > 0 {
		integrationEntries = append(integrationEntries, fmt.Sprintf("blockers=%s", strings.Join(inspection.Blockers, "|")))
	}
	for _, check := range inspection.RequiredChecks {
		integrationEntries = append(integrationEntries, fmt.Sprintf("check=%s:%s", check.Command, check.Status))
	}
	updated, err := store.Update(snapshot.Revision, runtime.Mutation{
		EventID:        fmt.Sprintf("evt-subagent-integration-%s-r%d", integratedState, snapshot.Revision+1),
		TransitionID:   "SUBAGENT-INTEGRATION",
		Event:          "subagent_integration",
		Actor:          "hook_controller",
		RuntimeID:      stringValue(snapshot.State["runtime_id"]),
		From:           teammateFromCursor(snapshot.State),
		To:             teammateFromCursor(snapshot.State),
		EvidenceIDs:    []string{},
		IdempotencyKey: fmt.Sprintf("subagent:%s:%s:%d", inspection.WorktreePath, integratedState, snapshot.Revision),
		Message:        fmt.Sprintf("SubagentStop recorded integration checkpoint state=%s", integratedState),
		OccurredAt:     now,
		Apply: func(state map[string]any) error {
			if integratedState == integration.StateComplete {
				if _, err := completeExecutionState(root, state, inspection.AssignmentID); err != nil {
					return err
				}
			}
			milestone, _ := state["milestone"].(map[string]any)
			if milestone == nil {
				milestone = map[string]any{}
			}
			milestone["integration"] = integrationEntries
			milestone["updated_at"] = now.Format(time.RFC3339Nano)
			state["milestone"] = milestone
			return nil
		},
	})
	if err != nil {
		return runtime.Snapshot{}, false, err
	}
	return updated, true, nil
}

func teammateFromCursor(state map[string]any) map[string]any {
	return lifecycleCursor(state)
}

func decisionLabel(kind teammateDecisionKind) string {
	switch kind {
	case teammateResume:
		return "resume"
	case teammateDemandCompletionReport:
		return "demand_completion_report"
	case teammateDemandBlockerReport:
		return "demand_blocker_report"
	case teammateIdleAwaitingConsumer:
		return "idle_awaiting_consumer"
	}
	return "unknown"
}

// idleAfterCompletionDecision rewrites the previously "allocate next
// task" decision into a read-only idle packet. The Agent still gets a
// specific next-step — acknowledge the completion report and wait for the
// scheduler — but the Hook never CAS-writes a new assignment from
// TeammateIdle (L4 §15.1 / §15.2 P2-5). The original automation entries
// stay so the Agent can see why idle is allowed and who owns the next
// dispatch.
func idleAfterCompletionDecision(source teammateDecision) teammateDecision {
	next := source
	next.kind = teammateIdleAwaitingConsumer
	next.action = "Worker: idle is allowed; await the original consumer. Main: acknowledge the completion report; the scheduler allocates the next legal task in the same Team"
	next.blocker = ""
	next.blockedFlag = false
	next.automation = appendUniqueStrings(next.automation,
		"idle does not self-claim a Team task — wait for the scheduler/Main dispatch",
	)
	next.worktreeRecovery = false
	next.teamCompleted = false
	next.nextTaskID = ""
	return next
}

func buildGuidanceFromDecision(root string, state map[string]any, event string, input policy.Input, decision teammateDecision) policy.Guidance {
	guidance := buildGuidance(root, state, event, input)
	guidance.Action = decision.action
	guidance.Automation = appendUniqueStrings(guidance.Automation, decision.automation...)
	if decision.blockedFlag {
		guidance.Blocked = true
		guidance.Blocker = decision.blocker
	}
	guidance.Missing = appendUniqueStrings(guidance.Missing, decision.missing...)
	guidance.Integration = appendUniqueStrings(guidance.Integration, decision.missingReports...)
	guidance.Instruction = formatGuidanceInstruction(guidance)
	return guidance
}

// runtimeStateMissing reports whether the runtime state file does not exist
// at all (fresh checkout) — distinct from a corrupted state, which keeps the
// BLOCKED recovery path.
func runtimeStateMissing(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".claude", "loop-state.json"))
	return err != nil && os.IsNotExist(err)
}
