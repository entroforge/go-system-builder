package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
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
func buildGuidance(root string, state map[string]any, event string, input policy.Input) policy.Guidance {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	lifecycleState, _ := lifecycle["state"].(string)
	lifecyclePhase, _ := lifecycle["phase"].(string)
	stage, skill, action := projectNext(lifecycleState, lifecyclePhase, root)
	next := buildNextProjection(state, stage, skill, action, root)
	applyS7BudgetGateway(&next, state)
	if lifecycleState == "bug_resolution" && lifecyclePhase == "investigation" {
		next.Action = investigationNextAction(state)
	}
	if lifecycleState == "bug_resolution" && lifecyclePhase != "investigation" {
		next.Action = repairNextAction(state, lifecyclePhase)
	}

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
			"do not call loop-harness for normal continuation",
			"treat this Hook packet as the Controller checkpoint",
			"use loop-harness manually only for initialization/binding, runtime reconcile after integrity failure, rollback/rollover, release Gateway, or the explicit worktree integration follow-up (`runtime task-integrate`)",
		},
		HumanRequired: next.HumanRequired,
		Recovery:      []string{"continue from this packet's Stage and Next", "read " + next.ProtocolRef, "if blocked or unclear read " + loopManualRef},
	}

	if lifecycleState == "paused" || lifecycleState == "awaiting_human_release" || lifecycleState == "aborted" {
		guidance.HumanRequired = true
		guidance.Blocked = true
		guidance.Blocker = pauseReason(state, lifecycleState)
		if lifecycleState == "awaiting_human_release" {
			guidance.Action = "stop automation and submit one explicit runtime human-decision (approve, defer, reject_defect, reject_acceptance, reject_release_audit, or abort)"
		} else {
			guidance.Action = "stop automation and surface the human Gateway"
		}
	} else if lifecycleState == "release_authorized" {
		guidance.Action = "S11 human-authorized terminal; Harness performs no squash merge, publication, deployment, or formal release"
	}
	switch event {
	case "PreCompact":
		guidance.Automation = append(guidance.Automation,
			"the checkpoint is persisted before compaction; the next SessionStart will re-emit this exact milestone",
		)
	case "SubagentStart":
		addDelegationPreflight(&guidance, input)
		if input.Runtime.Agent != nil {
			approvalRequired := input.Runtime.Agent.DispatchMode == "plan_approval_required"
			switch input.Runtime.Agent.State {
			case "spawned", "reading":
				if !approvalRequired {
					guidance.Missing = appendUnique(guidance.Missing, "plan_report")
					guidance.Action = "read the assignment, send one PLAN_REPORT via SendMessage(message_type=plan_report, plan_ref=<path>), then continue executing immediately; do not wait for Main approval"
					guidance.Automation = append(guidance.Automation, "plan_checkpoint is the normal dispatch mode; PLAN_REPORT is a live checkpoint, not a final response or an idle barrier")
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
		guidance.Integration = []string{
			"inspect the subagent worktree and review its committed diff and required checks",
			"verify the task branch targets the current develop integration branch",
			"merge the reviewed worktree branch back into develop/current integration branch",
			"remove worktree only after the merge and checks succeed",
			"record completion_ack after integration; never merge this path into master/main or release",
		}
		guidance.Automation = append(guidance.Automation,
			"re-wake the same Agent when its report is missing; do not silently spawn a replacement",
			"SubagentStop is not completion until the worktree integration checklist is complete",
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
			guidance.Action = "review and integrate the subagent worktree into the current development branch before acknowledging stop"
		}
	case "TeammateIdle":
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
			guidance.Action = "acknowledge the current teammate report and schedule the next legal assignment"
		}
	}

	// L3-S7 §8: a SessionStart/PreCompact during the verification phase must
	// carry the S7-specific recovery projection (current round, assignment
	// buckets, unconsumed Results, Claim coverage gaps, single next action).
	if lifecycleState == "verification" && (event == "SessionStart" || event == "PreCompact") {
		applyS7RecoveryProjection(state, &guidance)
	}

	// S8 entry source line: when the lifecycle flipped into bug_resolution
	// via TR-008 (ObservationBatch handoff), the recovery packet must carry
	// the exact source fact — observation_batch id and finding count — so a
	// compacted Agent can re-bind to the right sealed batch without reading
	// the milestone log. Read from state.review.observation_batch.
	if lifecycleState == "bug_resolution" && (event == "SessionStart" || event == "PreCompact") {
		applyS8EntryProjection(state, &guidance)
		applyS9RepairProjection(state, &guidance)
	}

	// L3-S10 §9: acceptance and release audit are read-only audit work, not
	// an invitation to take the shortest route to S11. Keep the finite
	// denominator, counterevidence, and the S9→fresh-S7 prerequisite visible
	// on every normal recovery packet.
	if lifecycleState == "acceptance" || lifecycleState == "release_audit" {
		applyS10Guidance(&guidance)
	}

	guidance.Instruction = formatGuidanceInstruction(guidance)
	return guidance
}

func applyS10Guidance(guidance *policy.Guidance) {
	guidance.Automation = append(guidance.Automation,
		"S10 is read-only for product code, locked REQ, contracts, and TASKs; any product change invalidates the current S7 clean round and must return through S8→S9→fresh S7",
		"do not treat a generic clean-round or overall PASS as acceptance; consume the finite coverage_inventory and counterevidence ledger",
		"the shortest path to S11 is not a completion criterion; UNKNOWN must be resolved, not renamed N/A or non-blocking risk",
	)
	guidance.Recovery = append(guidance.Recovery,
		"if the manifest gate names a missing row, correct that row and register a new immutable evidence artifact",
		"if an audit finding changes product behavior, stop S10 and route S8→S9→fresh S7; never patch in S10",
		"only after acceptance and release audit are complete should the Controller move to S11's human gateway",
	)
	if guidance.LifecycleState == "acceptance" {
		guidance.Missing = appendUniqueStrings(guidance.Missing, "coverage_inventory", "counterevidence_ledger", "acceptance_manifest")
		guidance.DoneWhen = append(guidance.DoneWhen,
			"every coverage item has source, expected, oracle, owner, evidence, disposition, and one counterevidence result",
			"acceptance manifest is valid, current, and bound to this runtime/baseline/review round",
		)
	} else {
		guidance.Missing = appendUniqueStrings(guidance.Missing, "audit_areas:8", "counterevidence_ledger", "release_audit_manifest")
		guidance.DoneWhen = append(guidance.DoneWhen,
			"all 8 release-audit areas have an independent conclusion and evidence",
			"release-audit manifest is valid, current, and bound to this runtime/baseline/review round",
		)
	}
}

func investigationNextAction(state map[string]any) string {
	reviewMap, _ := state["review"].(map[string]any)
	casePointer, _ := reviewMap["investigation"].(map[string]any)
	if casePointer == nil {
		return "ingest the sealed ObservationBatch via `runtime investigation ingest --grouping-rationale <reason>`; do not create a BUG or reproduce the symptom"
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

func repairNextAction(state map[string]any, phase string) string {
	reviewMap, _ := state["review"].(map[string]any)
	if investigation, _ := reviewMap["investigation"].(map[string]any); investigation == nil || stringValue(investigation["status"]) != "contract_approved" {
		return "complete S8 InvestigationCase and approve the RepairContract before opening S9"
	}
	pointer, _ := reviewMap["repair"].(map[string]any)
	if pointer == nil {
		return "consume the approved S8 RepairContract: run `runtime repair session open --session-id <session> --created-by <agent>`"
	}
	switch stringValue(pointer["status"]) {
	case "planning":
		return "dispatch each RepairAssignment with `runtime repair dispatch --assignment-id <assignment> --agent-id <agent>`, then submit one S9 domain PlanReport per Builder"
	case "reproducing":
		return "submit any missing S9 domain PlanReports; when every Assignment is reported run `runtime repair execution begin`"
	}
	if action := stringValue(pointer["next_action"]); action != "" {
		return action
	}
	return fmt.Sprintf("continue S9 repair phase %s: run `runtime repair status` and follow the recorded next action", phase)
}

func applyS9RepairProjection(state map[string]any, guidance *policy.Guidance) {
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
		line := "S9 RepairSession not opened: consume the approved Contract with `runtime repair session open --session-id <session> --created-by <agent>`"
		guidance.Automation = append(guidance.Automation, line)
		guidance.Recovery = append([]string{line}, guidance.Recovery...)
		return
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	line := fmt.Sprintf("S9 repair %s: session=%s; next=%s", stringValue(pointer["status"]), stringValue(pointer["session_id"]), repairNextAction(state, stringValue(lifecycle["phase"])))
	switch stringValue(pointer["status"]) {
	case "planning":
		line += "; dispatch every RepairAssignment with `runtime repair dispatch --assignment-id <assignment> --agent-id <agent>`, then submit one domain PlanReport per Builder with a red/blocked pre-fix check"
	case "reproducing":
		line += "; all PlanReports must be present before `runtime repair execution begin`; implementation writes remain gated"
	case "repairing":
		line += "; submit one exact-unit RepairResult per Assignment; the batch stays in fixing until every Assignment is reported"
	case "impact_reconciliation":
		line += "; the complete Assignment Result batch is present; compute session-wide Changeset and commit ChangeImpact"
	case "blocked":
		if route := stringValue(pointer["failure_route"]); route != "" {
			line += "; failure_route=" + route + " — follow the recorded recovery action; do not create a symptom-only patch"
		}
	case "closed":
		if seed := stringValue(pointer["review_plan_seed_ref"]); seed != "" {
			line += "; consume the registration-ready S7 seed with `runtime review-plan --file " + seed + "` after refreshing frozen hashes"
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
func applyS7RecoveryProjection(state map[string]any, guidance *policy.Guidance) {
	reviewMap, _ := state["review"].(map[string]any)
	if reviewMap == nil {
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
	if maxRounds := s7MaxRounds(state); maxRounds > 0 && round >= maxRounds {
		guidance.Automation = append(guidance.Automation,
			fmt.Sprintf("S7 budget: current round %d of %d may drain, but opening another full round requires the human `runtime s7-budget-decision` gateway", round, maxRounds),
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
		status := stringValue(row["status"])
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
		"S7 unconsumed ReviewResults (dispatched, result not yet consumed via `runtime review-result submit`): "+s7Bucket(unconsumed),
	)

	// Claim coverage gaps: required Claims with no final disposition yet.
	gaps := review.UndispositionedRequired(state)
	for _, claimID := range gaps {
		guidance.Missing = appendUnique(guidance.Missing, "claim:"+claimID)
	}

	// cannot_clean / discovery_draining: the round is NOT closed — the
	// ObservationBatch has been opened (when present) and the round is
	// draining with drain_policy=complete_required_claims. Surface that
	// invariant so a compacted Agent treats "draining" as continuing the
	// remaining required Claims, not as the round ending.
	if planStatus == "cannot_clean" || planStatus == "discovery_draining" {
		invariant := "S7 round status=" + planStatus + ": ObservationBatch is open with drain_policy=complete_required_claims; cannot_clean/discovery_draining ≠ end — finish the remaining required Claims listed in Missing"
		batchLine := s7ObservationBatchLine(reviewMap)
		if strings.Contains(batchLine, "not yet opened") {
			// The plan status already proves a batch exists; a missing
			// pointer is a control-plane inconsistency, not a fact to
			// state — saying both lines would contradict the invariant.
			batchLine = "S7 ObservationBatch: pointer missing from state.review despite " + planStatus + " — run `loop-harness doctor` to diagnose the control plane"
		}
		guidance.Automation = append(guidance.Automation, invariant, batchLine)
	}

	next := s7RecoveryNextAction(planStatus, round, running, queued, blocked, unconsumed, gaps)
	guidance.Action = next
	guidance.Recovery = append([]string{
		fmt.Sprintf("S7 recovery: round %d, plan %s; coverage gaps=%d; next: %s", round, planDesc, len(gaps), next),
	}, guidance.Recovery...)
}

// applyS8EntryProjection adds the S8 entry source line that tells a
// compacted Agent exactly which ObservationBatch carried the lifecycle from
// S7 into bug_resolution via TR-008. The batch pointer is read from
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
	line := fmt.Sprintf("S8 entered via TR-008 with observation_batch %s (%d findings, drain_policy=%s)",
		id, count, drain)
	guidance.Automation = append(guidance.Automation, line)
	guidance.Recovery = append([]string{line}, guidance.Recovery...)
}

// s7ObservationBatchLine renders the current ObservationBatch pointer (id,
// drain_policy, finding count) as one compact recovery line. Returns a
// placeholder when no batch has been opened yet (the round will open one
// on the next seal-triggering ReviewResult when the round surfaces an
// ordinary Finding).
func s7ObservationBatchLine(reviewMap map[string]any) string {
	batch, _ := reviewMap["observation_batch"].(map[string]any)
	if batch == nil {
		return "S7 ObservationBatch: not yet opened (round continues with drain_policy=complete_required_claims)"
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
func s7RecoveryNextAction(planStatus string, round int, running, queued, blocked, unconsumed, gaps []string) string {
	switch planStatus {
	case "observation_sealed":
		return "the sealed ObservationBatch hands off to S8 automatically: the next PreToolUse auto-commits TR-008 — do not invoke the transition CLI"
	case "clean":
		return "the machine CleanRound promotes to acceptance automatically: the next PreToolUse auto-commits TR-009 — do not invoke the transition CLI"
	case "paused":
		return "resolve the recorded pause verdict checkpoint; the round resumes only through the human gateway (TR-010/TR-011)"
	case "planned", "":
		return fmt.Sprintf("register the ReviewPlan for round %d: `loop-harness s7 draft --out plan.json`, fill the TODO oracles and generated `coverage_inventory`/`e2e_assets` facts, then `runtime review-plan --file plan.json`", round)
	}
	// running / cannot_clean / discovery_draining: coverage continues even
	// after an ordinary finding (drain_policy=complete_required_claims).
	if len(running) > 0 {
		return fmt.Sprintf("consume the pending ReviewResult for %s via `runtime review-result submit --assignment-id %s --result <result.json>`", running[0], firstAssignmentID(running[0]))
	}
	if len(blocked) > 0 {
		return fmt.Sprintf("assignment %s is blocked: fix the capture conditions, record `runtime agent-event --event blocker_resolved --agent-id <id> --message <file>`, then resubmit the same review result; blocked coverage must still be consumed before the round can close", blocked[0])
	}
	if len(queued) > 0 {
		return fmt.Sprintf("dispatch queued assignment %s via `runtime register-workgroup`; queued coverage is never dropped (L3-S7 §4.5)", queued[0])
	}
	if len(unconsumed) > 0 {
		return fmt.Sprintf("consume the pending ReviewResult for %s via `runtime review-result submit`", unconsumed[0])
	}
	if len(gaps) > 0 {
		return "claim coverage gaps remain without an assignment (" + strings.Join(gaps, ", ") + "); inspect `loop-harness s7 status` and repair the dispatch"
	}
	return "all required Claims are dispositioned; the round consumer seals on the next `runtime review-result submit` — verify via `loop-harness s7 status`"
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
		"Is a single subagent necessary, or should this responsibility use an Agent Team?",
		"Which predefined agent template under .claude/agents/ is being used?",
		"Is the assignment isolated in a worktree?",
		"Does the spawn carry an explicit team_name and a dispatch envelope (plan report / activation)?",
	}
	if subType, _ := input.ToolInput["subagent_type"].(string); subType != "" {
		guidance.ReadOrder = insertReadOrder(guidance.ReadOrder, ".claude/agents/"+subType+".md", 2)
	}
	guidance.Automation = append(guidance.Automation,
		"use TeamCreate plus team_name for parallel or role-bearing execution; read-only Explore/Plan research is the narrow exemption",
		"isolate execution in a worktree before writing",
		"default: send one PLAN_REPORT through SendMessage while the Worker is running, then continue; only plan_approval_required waits for activation",
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
	b.WriteString("Next: ")
	b.WriteString(g.Action)
	b.WriteString(". Read ")
	b.WriteString(g.ProtocolRef)
	b.WriteString(" for the next protocol step and ")
	b.WriteString(g.ManualRef)
	b.WriteString(" for the harness recovery procedure.")
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
		b.WriteString(" Human Gateway required; do not continue automation.")
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
	if milestoneMatchesWithGate(current, guidance, gate) {
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
		IdempotencyKey:       milestoneIdempotencyWithGate(guidance, gate),
		Message:              "Controller refreshed the resumable lifecycle milestone.",
		OccurredAt:           now,
		Apply: func(state map[string]any) error {
			state["milestone"] = milestone
			state["updated_at"] = now.Format(time.RFC3339Nano)
			return nil
		},
	})
	if err != nil {
		_ = metrics.RecordMilestoneRefreshFailure(root, milestoneRefreshFailureReason(err))
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
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		guidance := buildGuidance(root, snapshot.State, event, input)
		return guidance, snapshot, nil
	}
	switch event {
	case "SubagentStop":
		return HandleSubagentStopForController(context.Background(), root, snapshot, loaded, event, input)
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
	loaded, err := hookctx.LoadFull(root, agentID)
	if err != nil || loaded == nil {
		return false
	}
	assignment := hook.AssignmentForAgent(loaded.Assignments, agentID)
	if hook.HasCompletionReport(assignment) || hook.HasBlockerReport(assignment) {
		return true
	}
	// Final fallback for the no-assignment case (one-shot dispatch):
	// when the agent's own state already reports completion the loop
	// considers the report consumed without a separate Assignment row.
	if loaded.PolicyContext.Agent != nil {
		switch loaded.PolicyContext.Agent.State {
		case "reported", "done", "completed", "closed":
			return true
		}
		if loaded.PolicyContext.Agent.CompletionReportedRef != "" {
			return true
		}
	}
	return false
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
		Action:         "draft docs/requirements/REQ-<id>.md from docs/requirements/REQ-template.md (skills: requirement-funnel), have the human lock it, then bind with `loop-harness req bind --approved-by <the human who locked it>` (bind auto-initializes the runtime)",
		ProtocolRef:    "docs/agent-protocol.md#s0",
		ManualRef:      loopManualRef,
		PrimarySkill:   "requirement-funnel",
		Read:           []string{"docs/agent-protocol.md#s0", "docs/requirements/REQ-template.md"},
		ReadOrder:      []string{"LOOP RECOVERY packet (this message)", "docs/agent-protocol.md#s0", "skills/requirement-funnel/SKILL.md", "docs/requirements/REQ-template.md"},
		Missing:        []string{"human_locked_req"},
		DoneWhen:       []string{"a locked REQ exists and `req bind` succeeds (the runtime is initialized by bind)"},
		Blocked:        false,
		Blocker:        "",
		Recovery:       []string{"check `req list` for bindable REQs once one is locked"},
		Automation:     []string{"req bind auto-initializes the runtime — do not run `runtime reconcile` on a fresh checkout"},
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
		ProtocolRef:    "docs/agent-protocol.md#cursor-mapping",
		ManualRef:      loopManualRef,
		PrimarySkill:   "loop-orchestration",
		Read:           []string{".claude/loop-state.json", "docs/loop-definition.json"},
		ReadOrder:      []string{"LOOP RECOVERY packet (this message)", "AGENTS.md", ".claude/loop-state.json", "docs/agent-protocol.md#cursor-mapping", loopManualRef},
		Missing:        []string{"valid_runtime_cursor"},
		DoneWhen:       []string{"runtime and journal reconcile successfully"},
		Blocked:        true,
		Blocker:        "the Runtime snapshot could not be safely reconciled",
		Instruction:    "",
		Recovery:       []string{"read docs/agent-protocol.md#cursor-mapping", "read " + loopManualRef + " (fallback: " + loopManualFallbackRef + ")", "run loop-harness runtime reconcile --root ."},
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

// HandleSubagentStopForController wires SubagentStop through the Worktree
// Integrator (BUG-039-05) per BUG-039-06 §4.1:
//
//  1. Match the loaded assignment by agent_id / assignment_id.
//  2. Call integration.Inspect. If Ready → Integrate(Acknowledge=false,
//     Cleanup=false). If !Ready → surface blockers as Guidance and
//     preserve the worktree.
//  3. Surface the checkpoint in the Milestone via refreshMilestone.
//
// Cleanup and Acknowledge happen on subsequent calls (TeammateIdle or
// another SubagentStop) — never in this handler (BUG-039-06 §4.2).
func HandleSubagentStopForController(ctx context.Context, root string, snapshot runtime.Snapshot, loaded *hookctx.LoadedContext, event string, input policy.Input) (policy.Guidance, runtime.Snapshot, error) {
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
	targetBranch := assignment.TargetBranch
	if targetBranch == "" {
		targetBranch = "develop"
	}

	// First SubagentStop: merge → verified (Acknowledge=false, Cleanup=false).
	// Subsequent SubagentStop after verified: ack + cleanup follow-up
	// (BUG-039-06 §4.2 / BUG-039-38). Consult prior BEFORE treating Inspect
	// !Ready as terminal — after merge, Inspect commonly fails with
	// ErrMissingCommits (no commits beyond merge base).
	prior := loadPriorIntegrationState(root, loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration, assignment.AssignmentID)
	acknowledge, cleanup := false, false
	if prior == integration.StateVerified ||
		prior == integration.StateAcknowledged ||
		prior == integration.StateCleanupPending {
		acknowledge, cleanup = true, true
	}

	inspectReq := integration.InspectRequest{
		Root:               root,
		Assignment:         *assignment,
		TargetBranch:       targetBranch,
		BaselineGeneration: loaded.BaselineGeneration,
		RuntimeID:          loaded.PolicyContext.RuntimeID,
	}
	inspectResult, err := integration.Inspect(ctx, inspectReq, integration.InspectConfig{
		SkipCompletionCheck: false,
		// L3-S6 §7.4: required checks come from the assignment's manifest
		// declaration and run for real via the shell runner — a `verified`
		// checkpoint without an executed check set is no longer reachable
		// from this wiring.
		CheckRunner:    integration.CommandCheckRunner,
		RequiredChecks: assignment.RequiredChecks,
	})
	if err != nil {
		guidance := buildGuidance(root, snapshot.State, event, input)
		guidance.Blocked = true
		guidance.Blocker = "worktree inspection failed: " + err.Error()
		guidance.Missing = appendUnique(guidance.Missing, "worktree_inspect")
		guidance.Action = "repair the worktree state and re-run SubagentStop"
		guidance.Instruction = formatGuidanceInstruction(guidance)
		return guidance, snapshot, nil
	}

	if !inspectResult.Ready {
		if acknowledge && cleanup {
			// Post-merge follow-up: force Ready so Integrate resumes
			// ack/cleanup without re-running the merge gate (BUG-039-38).
			inspectResult = inspectionForAckCleanup(*assignment, inspectResult, targetBranch, loaded.BaselineGeneration)
		} else {
			guidance := buildGuidance(root, snapshot.State, event, input)
			guidance.Integration = appendUniqueStrings(guidance.Integration, inspectResult.Blockers...)
			guidance.Blocked = true
			guidance.Blocker = "worktree integration is not ready; preserving worktree and branch"
			guidance.Missing = appendUniqueStrings(guidance.Missing, inspectResult.Blockers...)
			guidance.Action = "re-wake the same subagent to remediate the blockers and resubmit the report"
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
				guidance.Action = "re-wake the same subagent to remediate the blockers and resubmit the report"
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
		Inspection:       inspectResult,
		ExpectedRevision: int64(snapshot.Revision),
		Acknowledge:      acknowledge,
		Cleanup:          cleanup,
	}
	integrationResult, err := integration.Integrate(ctx, integrateReq, integration.IntegrateConfig{
		Root:      root,
		GitRoot:   root,
		RuntimeID: loaded.PolicyContext.RuntimeID,
		// Same real-check wiring as Inspect — the verified transition in
		// the checkpoint state machine runs the assignment's declared
		// checks instead of advancing on an empty list.
		CheckRunner:    integration.CommandCheckRunner,
		RequiredChecks: assignment.RequiredChecks,
	})
	if err != nil {
		// Dirty / conflict preserve paths still return a checkpoint; surface
		// them as blocked Guidance rather than claiming a successful merge
		// (BUG-039-37: untracked harness files previously tripped dirty).
		if errors.Is(err, integration.ErrDirtyWorktree) || errors.Is(err, integration.ErrMergeConflict) {
			guidance := buildGuidance(root, snapshot.State, event, input)
			guidance.Blocked = true
			guidance.Blocker = "worktree integration preserved: " + err.Error()
			guidance.Missing = appendUnique(guidance.Missing, "integration_preserve")
			guidance.Integration = appendUniqueStrings(guidance.Integration, err.Error())
			if integrationResult.Checkpoint.State != "" {
				guidance.Integration = appendUniqueStrings(guidance.Integration,
					fmt.Sprintf("checkpoint_state=%s", integrationResult.Checkpoint.State))
			}
			guidance.Action = "preserve the worktree and branch; remediate the conflict or dirty tree before retrying SubagentStop"
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

	integratedState := "merged"
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
		if acknowledge && cleanup {
			guidance.Integration = appendUniqueStrings(guidance.Integration,
				"completion_ack recorded; worktree cleanup follow-up applied",
			)
			guidance.Action = fmt.Sprintf("worktree integration advanced to %s", integratedState)
		} else {
			guidance.Integration = appendUniqueStrings(guidance.Integration,
				"acknowledge and cleanup happen on the next TeammateIdle or SubagentStop",
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
	if acknowledge && cleanup {
		guidance.Integration = appendUniqueStrings(guidance.Integration,
			fmt.Sprintf("worktree integrated to state=%s", integratedState),
			"completion_ack recorded; worktree cleanup follow-up applied",
		)
		guidance.Action = fmt.Sprintf("worktree integration advanced to %s", integratedState)
	} else {
		guidance.Integration = appendUniqueStrings(guidance.Integration,
			fmt.Sprintf("worktree integrated to state=%s", integratedState),
			"acknowledge and cleanup happen on the next TeammateIdle or SubagentStop",
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
		if acknowledge && cleanup {
			guidance.Integration = appendUniqueStrings(guidance.Integration,
				fmt.Sprintf("worktree integrated to state=%s", integratedState),
				"completion_ack recorded; worktree cleanup follow-up applied",
			)
			guidance.Action = fmt.Sprintf("worktree integration advanced to %s", integratedState)
		} else {
			guidance.Integration = appendUniqueStrings(guidance.Integration,
				fmt.Sprintf("worktree integrated to state=%s", integratedState),
				"acknowledge and cleanup happen on the next TeammateIdle or SubagentStop",
			)
			guidance.Action = fmt.Sprintf("worktree integration advanced to %s; preserve worktree until acknowledged", integratedState)
		}
		guidance.Instruction = formatGuidanceInstruction(guidance)
	}
	return guidance, snapshot, nil
}

// loadPriorIntegrationState reads the durable worktree checkpoint so a
// subsequent SubagentStop can decide whether to run the ack/cleanup
// follow-up. Missing or unreadable checkpoints return "" (first-call path).
func loadPriorIntegrationState(root, runtimeID string, generation int, assignmentID string) string {
	if assignmentID == "" {
		return ""
	}
	if runtimeID == "" {
		runtimeID = "loop-REQ-039"
	}
	if generation <= 0 {
		generation = 1
	}
	path := integration.DefaultCheckpointStore().Path(root, runtimeID, generation, assignmentID)
	cp, found, err := integration.DefaultCheckpointStore().Load(path)
	if err != nil || !found {
		return ""
	}
	return cp.State
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
		if status, _ := row["state"].(string); status == "done" || status == "closed" || status == "completed" {
			continue
		}
		t := &teammateRow{
			ID:     id,
			Role:   stringValue(row["role"]),
			Status: stringValue(row["state"]),
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
		if agentID == "" && t.Status == "working" {
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
	for i := range loaded.Assignments {
		row := loaded.Assignments[i]
		if input.AgentID != "" && row.OwnerAgentID == input.AgentID {
			return &row
		}
		if input.TargetID != "" && row.AssignmentID == input.TargetID {
			return &row
		}
	}
	return nil
}

func isAssignmentComplete(assignment *hookctx.AssignmentContext) bool {
	if assignment == nil {
		return false
	}
	switch assignment.State {
	case "complete", "completed", "done", "verified", "merged", "acknowledged":
		return true
	}
	return false
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
	if assignment == nil {
		return false
	}
	if assignment.CompletionRef != "" {
		return true
	}
	if assignment.ReportStatus == "completion_report" || assignment.ReportStatus == "complete" || assignment.ReportStatus == "completed" {
		return true
	}
	return false
}

func hasBlockerReport(assignment *hookctx.AssignmentContext) bool {
	if assignment == nil {
		return false
	}
	return assignment.ReportStatus == "blocked" || assignment.ReportStatus == "blocker_report"
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
	next.action = "acknowledge the completion report; idle is allowed — the scheduler allocates the next legal task in the same Team"
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
