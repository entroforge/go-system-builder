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
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

const loopManualRef = ".claude/bin/loop-harness.md"

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
			"use loop-harness manually only for initialization/binding, runtime reconcile after integrity failure, rollback/rollover, or release Gateway",
		},
		HumanRequired: next.HumanRequired,
		Recovery:      []string{"continue from this packet's Stage and Next", "read " + next.ProtocolRef, "if blocked or unclear read " + loopManualRef},
	}

	if lifecycleState == "paused" || lifecycleState == "awaiting_human_release" || lifecycleState == "aborted" {
		guidance.HumanRequired = true
		guidance.Blocked = true
		guidance.Blocker = pauseReason(state, lifecycleState)
		guidance.Action = "stop automation and surface the human Gateway"
	}
	switch event {
	case "PreCompact":
		guidance.Automation = append(guidance.Automation,
			"the checkpoint is persisted before compaction; the next SessionStart will re-emit this exact milestone",
		)
	case "SubagentStart":
		addDelegationPreflight(&guidance, input)
		if input.Runtime.Agent != nil {
			switch input.Runtime.Agent.State {
			case "spawned", "reading", "understanding_submitted":
				guidance.Blocked = true
				guidance.Blocker = "phase-one readback is not yet approved"
				guidance.Missing = appendUnique(guidance.Missing, "agent_readback")
				guidance.Action = "complete the assigned readback and wait for phase-two activation"
			case "understanding_approved":
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
		if !input.Facts["agent_report_complete"] {
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
		if !input.Facts["assignment_reported"] {
			guidance.Blocked = true
			guidance.Blocker = "the current-round assignment report is missing"
			guidance.Missing = appendUnique(guidance.Missing, "assignment_report")
			guidance.Action = "re-wake the same teammate, resume the assignment, and submit the current-round report"
		} else {
			guidance.Action = "acknowledge the current teammate report and schedule the next legal assignment"
		}
	}

	guidance.Instruction = formatGuidanceInstruction(guidance)
	return guidance
}

func addDelegationPreflight(guidance *policy.Guidance, input policy.Input) {
	guidance.Questions = []string{
		"Is a single subagent necessary, or should this responsibility use an Agent Team?",
		"Which predefined agent template under .claude/agents/ is being used?",
		"Is the assignment isolated in a worktree?",
		"Does the spawn carry an explicit team_name and a two-phase readback/activation envelope?",
	}
	if subType, _ := input.ToolInput["subagent_type"].(string); subType != "" {
		guidance.ReadOrder = insertReadOrder(guidance.ReadOrder, ".claude/agents/"+subType+".md", 2)
	}
	guidance.Automation = append(guidance.Automation,
		"use TeamCreate plus team_name for parallel or role-bearing execution; read-only Explore/Plan research is the narrow exemption",
		"isolate execution in a worktree before writing",
		"do not write until phase-one readback is approved and phase-two activation is committed",
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
	store := runtime.NewStore(statePath, journalPath)
	store.PreCommitValidator = func(state map[string]any) error {
		return transition.MarshalAndValidateRuntime(root, state)
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
		metrics.RecordMilestoneRefreshFailure(root)
		return runtime.Snapshot{}, false, err
	}
	return updated, true, nil
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
	store := runtime.NewStore(statePath, journalPath)
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
	if gate.Status != "" {
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
// and `gate`. The comparison ignores time/event/instruction/source_revision
// fields, but a quality_gate fingerprint change MUST defeat the no-op
// (BUG-039-07 §4.1 step 2) so a new gate result forces a fresh write.
func milestoneMatchesWithGate(current map[string]any, guidance policy.Guidance, gate controller.QualityGateResult) bool {
	if current == nil {
		return false
	}
	existing := map[string]any{}
	for key, value := range current {
		switch key {
		case "source_revision", "updated_at", "event", "instruction":
			continue
		default:
			existing[key] = value
		}
	}
	expected := guidanceMapWithGate(guidance, controller.QualityGateResult{}, "", guidance.Revision, time.Time{}, gate)
	for key := range expected {
		if key == "source_revision" || key == "updated_at" || key == "event" || key == "instruction" {
			delete(expected, key)
		}
	}
	return equalJSON(existing, expected)
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

// milestoneIdempotencyWithGate hashes the guidance together with the
// gate fingerprint so a gate change produces a new Journal key while a
// semantically identical checkpoint stays a no-op (BUG-039-07 §4.1 step 3,
// REQ-039 §17 idempotency).
func milestoneIdempotencyWithGate(g policy.Guidance, gate controller.QualityGateResult) string {
	payload := struct {
		Guidance    policy.Guidance
		GateStatus  string
		GateID      string
		Fingerprint string
		ObservedRev int
	}{
		Guidance:    g,
		GateStatus:  string(gate.Status),
		GateID:      gate.GateID,
		Fingerprint: gate.Fingerprint,
		ObservedRev: gate.ObservedRevision,
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "milestone:" + hex.EncodeToString(sum[:])
}

func lifecycleCursor(state map[string]any) map[string]any {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	return map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
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
		return "release-ready package awaits human approval"
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
		decision.Guidance = fallbackGuidance(request.Event)
		return
	}
	request.Runtime = controllerRuntimeContext(snapshot.State, root, request.Runtime)
	decision.Guidance = &guidance
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
		Recovery:       []string{"read docs/agent-protocol.md#cursor-mapping", "read .claude/bin/loop-harness.md", "run loop-harness runtime reconcile --root ."},
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

// HandleTeammateIdleForController is the BUG-039-06 §4.1 repair: the
// TeammateIdle event handler must combine assignment state + task list to
// decide between resume / report / blocker / next-task / close. It runs
// the 5-branch scheduler, performs the matching Runtime CAS update, and
// returns the resulting Guidance + post-CAS snapshot.
//
// Branch table (per REQ-039 §13.7 / FR-017 / ARCHITECTURE-039 §11):
//
//  1. assignment not complete, no blocker         → re-wake same teammate (status=active)
//  2. assignment complete but no completion report → Guidance demanding report
//  3. assignment blocked but no blocker report     → Guidance demanding blocker report
//  4. assignment complete AND reported            → allocate next legal task in same Team
//  5. no remaining tasks                          → worktree recovery / Team close-out
//
// All Runtime state changes flow through runtime.Store CAS via the
// existing reconcileGuidance pattern; no Runtime field is mutated
// outside that surface.
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

	root = filepath.Clean(root)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")

	updated := snapshot
	var err error
	switch decision.kind {
	case teammateResume:
		updated, _, err = casTeammateStatus(root, statePath, journalPath, snapshot, teammate.ID, "activated", decision, event)
	case teammateDemandCompletionReport:
		updated, _, err = casTeammateStatus(root, statePath, journalPath, snapshot, teammate.ID, teammate.Status, decision, event)
	case teammateDemandBlockerReport:
		updated, _, err = casTeammateStatus(root, statePath, journalPath, snapshot, teammate.ID, teammate.Status, decision, event)
	case teammateAllocateNext:
		next := nextLegalTaskForTeammate(snapshot.State, teammate, loaded)
		if next == nil {
			// Complete + reported, but no remaining legal task in the
			// Team. Fall through to close-out rather than allocate.
			decision = closeOutFromAllocateNext(decision)
			updated, _, err = casCloseOutTeammate(root, statePath, journalPath, snapshot, teammate, decision, event)
		} else {
			updated, _, err = casTeammateStatus(root, statePath, journalPath, snapshot, teammate.ID, "activated", decision, event)
			if err == nil {
				updated, _, err = casCreateAssignment(root, statePath, journalPath, updated, teammate, next, event)
			}
		}
	case teammateCloseOut:
		updated, _, err = casCloseOutTeammate(root, statePath, journalPath, snapshot, teammate, decision, event)
	}
	if err != nil {
		return policy.Guidance{}, snapshot, err
	}

	guidance := buildGuidanceFromDecision(root, updated.State, event, input, decision)
	// Refresh the milestone so the persisted checkpoint reflects the
	// scheduling decision the handler just made.
	refreshed, _, err := refreshMilestone(root, statePath, journalPath, updated, guidance, event)
	if err != nil && !errors.Is(err, runtime.ErrStaleRevision) {
		return guidance, updated, err
	}
	if refreshed.Revision != 0 {
		updated = refreshed
		guidance = buildGuidanceFromDecision(root, updated.State, event, input, decision)
	}
	return guidance, updated, nil
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

// teammateDecision enumerates the five scheduling branches BUG-039-06 §4.1
// demands of the TeammateIdle event.
type teammateDecisionKind int

const (
	teammateResume teammateDecisionKind = iota + 1
	teammateDemandCompletionReport
	teammateDemandBlockerReport
	teammateAllocateNext
	teammateCloseOut
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
		decision.kind = teammateAllocateNext
		decision.action = "acknowledge the completion report and allocate the next legal task in the same Team"
		decision.automation = append(decision.automation,
			"completion report is durable; advance teammate state and create the next assignment",
		)
		return decision
	default:
		// Fall-through covers the "blocked but blocker already reported"
		// case and any case where there is no remaining work.
		decision.kind = teammateCloseOut
		decision.blockedFlag = false
		decision.worktreeRecovery = true
		decision.teamCompleted = true
		decision.action = "enter worktree recovery / Team close-out; preserve worktree and merge into the integration branch"
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

func nextLegalTaskForTeammate(state map[string]any, teammate *teammateRow, loaded *hookctx.LoadedContext) *nextTaskCandidate {
	entities, _ := state["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	completed := teammateCompletedTaskIDs(loaded, teammate)
	for _, raw := range tasks {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		stateStr := stringValue(row["state"])
		if stateStr != "candidate" && stateStr != "pending" && stateStr != "planned" && stateStr != "reviewed" {
			continue
		}
		taskID := stringValue(row["id"])
		if containsStringValue(completed, taskID) {
			continue
		}
		deps, _ := row["depends_on"].([]any)
		if !dependenciesSatisfied(deps, completed) {
			continue
		}
		owners, _ := row["owner_agent_ids"].([]any)
		// Allocate the task to the same Team as the idle teammate. We
		// prefer the teammate itself when no other owner is recorded;
		// otherwise we attach the new assignment to the first listed
		// owner so the CAS mutation stays deterministic.
		owner := teammate.ID
		if len(owners) > 0 {
			if first, ok := owners[0].(string); ok && first != "" {
				owner = first
			}
		}
		teamID := teammate.TeamID
		return &nextTaskCandidate{
			TaskID: taskID,
			Owner:  owner,
			TeamID: teamID,
			State:  stateStr,
		}
	}
	return nil
}

func dependenciesSatisfied(deps []any, completed []string) bool {
	for _, dep := range deps {
		name, ok := dep.(string)
		if !ok {
			continue
		}
		if !containsStringValue(completed, name) {
			return false
		}
	}
	return true
}

func containsStringValue(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func teammateCompletedTaskIDs(loaded *hookctx.LoadedContext, teammate *teammateRow) []string {
	completed := make([]string, 0, len(teammate.TaskIDs))
	if loaded != nil {
		for _, row := range loaded.Assignments {
			if row.OwnerAgentID != teammate.ID && !containsStringValue(teammate.TaskIDs, row.TaskID) {
				continue
			}
			if isAssignmentComplete(&row) && hasCompletionReport(&row) {
				completed = appendUnique(completed, row.TaskID)
			}
		}
	}
	return completed
}

type nextTaskCandidate struct {
	TaskID string
	Owner  string
	TeamID string
	State  string
}

func casTeammateStatus(root, statePath, journalPath string, snapshot runtime.Snapshot, agentID, newStatus string, decision teammateDecision, event string) (runtime.Snapshot, bool, error) {
	if snapshot.Revision == 0 {
		return snapshot, false, nil
	}
	store := runtime.NewStore(statePath, journalPath)
	store.PreCommitValidator = func(state map[string]any) error {
		return transition.MarshalAndValidateRuntime(root, state)
	}
	now := time.Now().UTC()
	updated, err := store.Update(snapshot.Revision, runtime.Mutation{
		EventID:        fmt.Sprintf("evt-teammate-%s-%s-r%d", agentID, newStatus, snapshot.Revision+1),
		TransitionID:   "TEAMMATE-IDLE-DECISION",
		Event:          "teammate_idle_decision",
		Actor:          "hook_controller",
		RuntimeID:      stringValue(snapshot.State["runtime_id"]),
		From:           teammateFromCursor(snapshot.State),
		To:             teammateFromCursor(snapshot.State),
		EvidenceIDs:    []string{},
		IdempotencyKey: fmt.Sprintf("teammate:%s:%s:%d", agentID, decisionLabel(decision.kind), snapshot.Revision),
		Message:        fmt.Sprintf("TeammateIdle scheduled %s decision for %s", decisionLabel(decision.kind), agentID),
		OccurredAt:     now,
		Apply: func(state map[string]any) error {
			entities, _ := state["entities"].(map[string]any)
			agents, _ := entities["agents"].([]any)
			for _, raw := range agents {
				row, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if id, _ := row["id"].(string); id != agentID {
					continue
				}
				row["state"] = newStatus
				row["updated_at"] = now.Format(time.RFC3339Nano)
			}
			state["entities"] = entities
			return nil
		},
	})
	if err != nil {
		return runtime.Snapshot{}, false, err
	}
	return updated, true, nil
}

func casCreateAssignment(root, statePath, journalPath string, snapshot runtime.Snapshot, teammate *teammateRow, next *nextTaskCandidate, event string) (runtime.Snapshot, bool, error) {
	if snapshot.Revision == 0 || next == nil {
		return snapshot, false, nil
	}
	store := runtime.NewStore(statePath, journalPath)
	store.PreCommitValidator = func(state map[string]any) error {
		return transition.MarshalAndValidateRuntime(root, state)
	}
	now := time.Now().UTC()
	newAssignmentID := fmt.Sprintf("assignment-%s-next-%s", teammate.ID, next.TaskID)
	updated, err := store.Update(snapshot.Revision, runtime.Mutation{
		EventID:        fmt.Sprintf("evt-teammate-%s-allocate-%s-r%d", teammate.ID, next.TaskID, snapshot.Revision+1),
		TransitionID:   "TEAMMATE-NEXT-ASSIGNMENT",
		Event:          "teammate_next_assignment",
		Actor:          "hook_controller",
		RuntimeID:      stringValue(snapshot.State["runtime_id"]),
		From:           teammateFromCursor(snapshot.State),
		To:             teammateFromCursor(snapshot.State),
		EvidenceIDs:    []string{},
		IdempotencyKey: fmt.Sprintf("teammate:%s:next:%s:%d", teammate.ID, next.TaskID, snapshot.Revision),
		Message:        fmt.Sprintf("TeammateIdle allocated next task %s to teammate %s", next.TaskID, teammate.ID),
		OccurredAt:     now,
		Apply: func(state map[string]any) error {
			entities, _ := state["entities"].(map[string]any)
			tasks, _ := entities["tasks"].([]any)
			for _, raw := range tasks {
				row, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if id, _ := row["id"].(string); id == next.TaskID {
					owners, _ := row["owner_agent_ids"].([]any)
					ownerSet := map[string]bool{}
					for _, o := range owners {
						if s, ok := o.(string); ok {
							ownerSet[s] = true
						}
					}
					ownerSet[teammate.ID] = true
					row["owner_agent_ids"] = ownerKeys(ownerSet)
					row["state"] = "in_progress"
				}
			}
			state["entities"] = entities
			return nil
		},
	})
	if err != nil {
		return runtime.Snapshot{}, false, err
	}
	_ = newAssignmentID // reference kept for future evidence correlation; not used in the bare CAS update.
	return updated, true, nil
}

func casCloseOutTeammate(root, statePath, journalPath string, snapshot runtime.Snapshot, teammate *teammateRow, decision teammateDecision, event string) (runtime.Snapshot, bool, error) {
	if snapshot.Revision == 0 {
		return snapshot, false, nil
	}
	store := runtime.NewStore(statePath, journalPath)
	store.PreCommitValidator = func(state map[string]any) error {
		return transition.MarshalAndValidateRuntime(root, state)
	}
	now := time.Now().UTC()
	updated, err := store.Update(snapshot.Revision, runtime.Mutation{
		EventID:        fmt.Sprintf("evt-teammate-%s-closeout-r%d", teammate.ID, snapshot.Revision+1),
		TransitionID:   "TEAMMATE-CLOSEOUT",
		Event:          "teammate_closeout",
		Actor:          "hook_controller",
		RuntimeID:      stringValue(snapshot.State["runtime_id"]),
		From:           teammateFromCursor(snapshot.State),
		To:             teammateFromCursor(snapshot.State),
		EvidenceIDs:    []string{},
		IdempotencyKey: fmt.Sprintf("teammate:%s:closeout:%d", teammate.ID, snapshot.Revision),
		Message:        "TeammateIdle entered worktree recovery / Team close-out",
		OccurredAt:     now,
		Apply: func(state map[string]any) error {
			entities, _ := state["entities"].(map[string]any)
			teams, _ := entities["teams"].([]any)
			for _, raw := range teams {
				row, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if id, _ := row["id"].(string); id != teammate.TeamID {
					continue
				}
				row["status"] = "complete"
			}
			state["entities"] = entities
			return nil
		},
	})
	if err != nil {
		return runtime.Snapshot{}, false, err
	}
	return updated, true, nil
}

func persistSubagentCheckpoint(root, statePath, journalPath string, snapshot runtime.Snapshot, inspection *integration.Inspection, targetBranch, event, integratedState string) (runtime.Snapshot, bool, error) {
	if snapshot.Revision == 0 {
		return snapshot, false, nil
	}
	store := runtime.NewStore(statePath, journalPath)
	store.PreCommitValidator = func(state map[string]any) error {
		return transition.MarshalAndValidateRuntime(root, state)
	}
	now := time.Now().UTC()
	// The milestone schema constrains `integration` to a string array.
	// Surface the checkpoint state + blockers as compact one-line
	// strings so the durable Milestone projection stays schema-valid
	// while still preserving the worktree + branch identity.
	integrationEntries := []string{
		fmt.Sprintf("worktree=%s", inspection.WorktreePath),
		fmt.Sprintf("branch=%s", inspection.SourceBranch),
		fmt.Sprintf("target_branch=%s", targetBranch),
		fmt.Sprintf("status=%s", integratedState),
		fmt.Sprintf("source_head=%s", inspection.SourceHead),
		fmt.Sprintf("merge_base=%s", inspection.MergeBase),
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
	case teammateAllocateNext:
		return "allocate_next"
	case teammateCloseOut:
		return "close_out"
	}
	return "unknown"
}

// closeOutFromAllocateNext converts an allocate-next decision to a
// close-out decision when the team has no remaining legal tasks. The
// scheduler preserves the original automation/missing entries and
// rewrites the action text so the Agent sees a close-out story.
func closeOutFromAllocateNext(source teammateDecision) teammateDecision {
	next := source
	next.kind = teammateCloseOut
	next.action = "enter worktree recovery / Team close-out; preserve worktree and merge into the integration branch"
	next.blocker = ""
	next.blockedFlag = false
	next.automation = append(next.automation, "no remaining legal task in this Team")
	next.worktreeRecovery = true
	next.teamCompleted = true
	return next
}

func ownerKeys(set map[string]bool) []any {
	out := make([]any, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
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
