package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type nativeRoutingKey struct{}

// A pending-recovery route may have to select between S7 and a still-issued
// S9 responsibility. Retain this event's task read for the selected consumer.
type nativeRecoveredS9SourcesKey struct{}
type nativeRecoveredS9Sources struct {
	sources   nativeTaskSources
	refs      []string
	briefRefs []string
}

// Only the initial routing reads share this snapshot. Domain consumers and
// Writers retain their own current reads, including after a successful receipt.
type nativeRoutingRead struct {
	root string
	once sync.Once
	snap runtime.Snapshot
	err  error
}

func nativeHookRoutes(root string, in policy.Input, out, diagnostic io.Writer) (bool, int) {
	defer metrics.Span(contextForHook(in), "hook/native_routes")()
	// The derived input never escapes the routing chain. Professional receivers
	// return handled=true; recovered frozen responsibilities retain their original
	// downstream route after the shared Writer has restored its pending pair.
	in.Context = context.WithValue(contextForHook(in), nativeRoutingKey{}, &nativeRoutingRead{root: root})
	if contextForHook(in).Err() != nil {
		return true, 0
	}
	// These events have no native professional consumer. Keep their original
	// reader/observer audit path available even when Runtime cannot be read.
	if in.Event == "PostToolUseFailure" || in.Event == "ConfigChange" {
		return false, 0
	}
	if in.Event == "SubagentStop" && in.AgentID != "" && in.AgentID != "Main" {
		if _, err := nativeRoutingSnapshot(root, in); errors.Is(err, runtime.ErrPendingRuntimeOperation) {
			writer := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{}).WithContext(contextForHook(in))
			if _, err := writer.RecoverPendingOperations(); err != nil {
				if contextForHook(in).Err() != nil {
					return true, 0
				}
				return nativeSnapshotFailure(root, in, fmt.Errorf("Native SubagentStop could not recover the original pending Writer; professional reception remains unconfirmed: %w", err), out, diagnostic)
			}
			if contextForHook(in).Err() != nil {
				return true, 0
			}
			// Recovery changes the durable pair. Never reuse the cached pending
			// error or infer a stage from the marker or the platform actor.
			in.Context = context.WithValue(contextForHook(in), nativeRoutingKey{}, &nativeRoutingRead{root: root})
			snap, err := nativeRoutingSnapshot(root, in)
			if err != nil {
				if contextForHook(in).Err() != nil {
					return true, 0
				}
				return nativeSnapshotFailure(root, in, fmt.Errorf("Native SubagentStop recovered the original Writer, but current Runtime access and professional reception remain unconfirmed: %w", err), out, diagnostic)
			}
			fmt.Fprintln(diagnostic, "Native SubagentStop recovered the original pending Writer; this does not establish professional reception.")
			if nativeReviewStage(snap.State) == "S7" {
				// A real S9 Builder or verifier can return after a new S7 has
				// started. Select it only from its registered exact original task,
				// before S7's ordinary unmatched-Stop fallback consumes the event.
				if selected, ok := recoveredOriginalS9Sources(root, in, snap.State); ok {
					in.Context = context.WithValue(contextForHook(in), nativeRecoveredS9SourcesKey{}, selected)
					if handled, code := nativeS9Hook(root, in, out, diagnostic); handled {
						return true, code
					}
				}
				handled, code := nativeReviewHook(root, in, out, diagnostic)
				if !handled {
					refreshRecoveredHookReader(root, in, snap)
				}
				return handled, code
			}
			if runtime.PacketEnabled(snap.State) && nativeS9RoutingAvailable(contextForHook(in), root, snap.State, true) {
				handled, code := nativeS9Hook(root, in, out, diagnostic)
				if !handled {
					refreshRecoveredHookReader(root, in, snap)
				}
				return handled, code
			}
			// Completing a validated pending pair is shared infrastructure work.
			// Other stages do not gain a new professional receiver in this event.
			return true, 0
		}
	}
	if contextForHook(in).Err() != nil {
		return true, 0
	}
	if nativePendingRuntimeMainContext(root, in, out, diagnostic) {
		return true, 0
	}
	// All native routes share this actual read. A failed read cannot mean an
	// empty set of responsibilities, and must not reach a Main fallback.
	snap, err := nativeRoutingSnapshot(root, in)
	if contextForHook(in).Err() != nil {
		return true, 0
	}
	if err != nil {
		return nativeSnapshotFailure(root, in, err, out, diagnostic)
	}
	if reader, ok := contextForHook(in).Value(hookReaderKey{}).(*hookReader); ok && reader.snapshotErr != nil {
		refreshRecoveredHookReader(root, in, snap)
		if contextForHook(in).Err() != nil {
			return true, 0
		}
	}
	// Main observers reuse the snapshot already read by this event. The
	// reader is request-scoped; a later Writer must refresh it before reuse.
	if reader, ok := contextForHook(in).Value(hookReaderKey{}).(*hookReader); ok && reader.Kind == "Main" {
		reader.snapshot = snap
		reader.snapshotErr = nil
	}
	if handled, code := nativeMainReturnHook(root, in, out); handled {
		return true, code
	}
	if handled, code := runSavedS9RepairResultHook(root, in, out, diagnostic); handled {
		return true, code
	}
	// A previously issued Reviewer/Investigator can return during an earlier
	// stage. Only that actor's original task can select its registered packet;
	// unmatched S5/S6 work immediately continues through its own route.
	if in.AgentID != "" && in.AgentID != "Main" && nativeReviewStage(snap.State) != "S7" && nativeReviewStage(snap.State) != "S8" {
		for _, row := range objectSliceAny(mapFieldCLI(snap.State, "entities")["work_packets"]) {
			kind := stringValue(row["template_id"])
			if kind == "s7-review-result-v1" || kind == "s8-hypothesis-result-v1" {
				if handled, code := nativeReviewHookStage(root, in, out, diagnostic, "S8"); handled {
					return true, code
				}
				break
			}
		}
	}
	if handled, code := nativeS6Hook(root, in, out, diagnostic); handled {
		return true, code
	}
	// Start may carry an existing exact review binding but not the complete
	// task prompt. Give such a binding its own context before considering a
	// neutral fallback; a stale S9 route must not claim a new S7/S8 Worker.
	if in.Event == "SubagentStart" && (nativeReviewStage(snap.State) == "S7" || nativeReviewStage(snap.State) == "S8") {
		if handled, code := nativeReviewHook(root, in, out, diagnostic); handled {
			return true, code
		}
	}
	// An already issued S7 responsibility keeps its exact original receiver
	// after S8 has advanced to repair; unrelated work still reaches S9.
	if (nativeReviewStage(snap.State) == "S7" && lifecycleState(snap.State) != "verification") ||
		(nativeReviewStage(snap.State) == "S8" && lifecycleState(snap.State) == "bug_resolution" && s9ProfessionalPhase(stringValue(mapFieldCLI(snap.State, "lifecycle")["phase"])) && in.AgentID != "" && in.AgentID != "Main") {
		if handled, code := nativeReviewHook(root, in, out, diagnostic); handled {
			return true, code
		}
	}

	if handled, code := nativeS9Hook(root, in, out, diagnostic); handled {
		return true, code
	}
	return nativeReviewHook(root, in, out, diagnostic)
}

// recoveredOriginalS9Sources recognizes only an existing exact S9 task
// reference. No role, actor name, most-recent file or unique-candidate guess
// can switch a recovered S7 event to S9.
func recoveredOriginalS9Sources(root string, in policy.Input, state map[string]any) (nativeRecoveredS9Sources, bool) {
	var selected nativeRecoveredS9Sources
	routes, err := nativeS9Routes(contextForHook(in), state)
	if err != nil {
		return selected, false
	}
	for _, route := range routes {
		if route.BriefPath == "" {
			continue
		}
		selected.briefRefs = append(selected.briefRefs, filepath.Join(root, nativeS9RouteBrief(route)))
	}
	selected.refs = append(selected.refs, selected.briefRefs...)
	status := stringValue(repairPointerCLI(state)["status"])
	if status == "targeted_reverification" || status == "blocked" || status == "ready_for_full_review" || status == "closed" {
		if selections, err := repairTargetedSelections(root, state); err == nil {
			for _, selection := range selections {
				if ref, err := repairTargetedQuestionnairePath(root, state, selection.performingAssignmentID); err == nil {
					selected.refs = append(selected.refs, ref)
				}
			}
		}
	}
	if len(selected.refs) == 0 {
		return selected, false
	}
	selected.sources = nativeS9TaskSources(in, root, selected.refs, selected.briefRefs...)
	text := selected.sources.s9ResponsibilityText(root, selected.refs, selected.briefRefs...)
	for _, ref := range selected.refs {
		if nativeS6ExactReference(text, ref) {
			return selected, true
		}
	}
	return selected, false
}

// evaluate retains this reader pointer outside the derived native input. Update
// it in place so a frozen/activated responsibility returning handled=false can
// reach its original downstream branch in this same public Hook request.
func refreshRecoveredHookReader(root string, in policy.Input, snapshot ...runtime.Snapshot) {
	if reader, ok := contextForHook(in).Value(hookReaderKey{}).(*hookReader); ok {
		*reader = *resolveHookReader(root, in, snapshot...)
	}
}

// A successful exit keeps ordinary tools available. Only event-supported
// additionalContext reaches a model; Stop's systemMessage is a user notice.
// No failed snapshot supplies a stage, owner, receipt or recovery capability.
func nativeSnapshotFailure(root string, in policy.Input, cause error, out, diagnostic io.Writer) (bool, int) {
	if contextForHook(in).Err() != nil {
		return true, 0
	}
	modelContext := false
	userNotice := false
	if mainQuestionnaireViewer(in) {
		switch in.Event {
		case "PreToolUse", "SessionStart":
			modelContext = true
		case "PostToolUse":
			modelContext = in.Error == "" && in.ToolResponse["is_error"] != true
		case "Stop":
			userNotice = true
		}
	} else {
		switch in.Event {
		case "SubagentStart":
			modelContext = true
		case "SubagentStop":
			userNotice = true
		}
	}
	if !modelContext && !userNotice {
		return true, 0
	}
	message := fmt.Sprintf("Runtime snapshot at %s could not be verified: %v. The current stage and professional reception cannot be verified. Preserve original answers and accepted receipts. Main must restore the original Runtime source and inspect its original receipts before continuing. Ordinary tools remain available.", filepath.Join(root, ".claude/loop-state.json"), cause)
	fmt.Fprintln(diagnostic, message)
	if modelContext {
		// The renderer always names PreToolUse, so only that event may use it.
		// Every other event keeps the platform's own event name and its plain
		// diagnostic context.
		if in.Event == "PreToolUse" {
			if rendered, ok := nativePreToolUseQualityGate(root, message); ok {
				_, _ = out.Write(rendered)
				return true, 0
			}
		}
		_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": message}})
	} else {
		_ = json.NewEncoder(out).Encode(map[string]any{"systemMessage": message})
	}
	return true, 0
}

// An unreadable Runtime leaves the cursor unknowable, not absent. PreToolUse
// still renders through the single QUALITY_GATE producer, so the Agent sees
// the same quality projection, LOOP RECOVERY packet and recovery action as
// any other unknown cursor: status=unknown, no observed revision, no committed
// transition, no tool denial and no Runtime write.
func nativePreToolUseQualityGate(root, message string) ([]byte, bool) {
	// The unknowable cursor reuses the existing projection's default branch
	// instead of a second recovery wording.
	stage, skill, action := projectNext("", "", root)
	decision := policy.Decision{
		Decision:          "allow",
		AdditionalContext: message,
		Guidance: &policy.Guidance{
			Stage:        stage,
			PrimarySkill: skill,
			Action:       action,
			ProtocolRef:  protocolReference(stage),
			ManualRef:    loopManualRef,
		},
	}
	result := controller.ControlResult{
		Decision: decision,
		QualityGate: controller.QualityGateResult{
			Status:              controller.StatusUnknown,
			ErrorCode:           controller.CodeRuntimeInval,
			ObservedRevision:    0,
			TransitionCommitted: false,
		},
	}
	rendered, code, err := hook.PreToolUseWithQualityGate(decision, result)
	if err != nil || code != 0 {
		return nil, false
	}
	return rendered, true
}

func nativeRoutingSnapshot(root string, in policy.Input) (runtime.Snapshot, error) {
	ctx := contextForHook(in)
	if err := ctx.Err(); err != nil {
		return runtime.Snapshot{}, err
	}
	read := func() (runtime.Snapshot, error) {
		defer metrics.Span(ctx, "hook/native_routes/initial_snapshot")()
		return runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).WithContext(ctx).Snapshot()
	}
	if cached, ok := ctx.Value(nativeRoutingKey{}).(*nativeRoutingRead); ok && cached.root == root {
		cached.once.Do(func() { cached.snap, cached.err = read() })
		if err := ctx.Err(); err != nil {
			return runtime.Snapshot{}, err
		}
		return cached.snap, cached.err
	}
	return read()
}

// Pending is a typed Runtime boundary, not a stage/owner/completion fact.
// Recover only the original paired write, then retain the actual event for its
// existing receiver. A restored pair alone is never proof of delivery.
func nativePendingRuntimeMainContext(root string, in policy.Input, out io.Writer, diagnostics ...io.Writer) bool {
	if contextForHook(in).Err() != nil {
		return true
	}
	if !mainQuestionnaireViewer(in) {
		return false
	}
	switch in.Event {
	case "PreToolUse", "SessionStart":
	case "PostToolUse":
		if in.Error != "" {
			return false
		}
		if failed, _ := in.ToolResponse["is_error"].(bool); failed {
			return false
		}
	default:
		return false
	}
	if workspace.RequireMain(root) != nil {
		return false
	}
	if contextForHook(in).Err() != nil {
		return true
	}
	if in.CWD != "" {
		current, err := workspace.Canonical(in.CWD)
		if err != nil {
			return false
		}
		main, err := workspace.Canonical(root)
		if err != nil || current != main {
			return false
		}
	}
	if contextForHook(in).Err() != nil {
		return true
	}
	if _, err := nativeRoutingSnapshot(root, in); !errors.Is(err, runtime.ErrPendingRuntimeOperation) {
		return false
	}
	writer := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{}).WithContext(contextForHook(in))
	_, recoveryErr := writer.RecoverPendingOperations()
	if contextForHook(in).Err() != nil {
		return true
	}
	if recoveryErr == nil {
		// Reset the derived routing cache in place; the caller retains this
		// pointer for the rest of the same public event.
		if cached, ok := contextForHook(in).Value(nativeRoutingKey{}).(*nativeRoutingRead); ok {
			*cached = nativeRoutingRead{root: root}
		}
		if snap, err := nativeRoutingSnapshot(root, in); err != nil {
			if contextForHook(in).Err() != nil {
				return true
			}
			recoveryErr = err
		} else {
			refreshRecoveredHookReader(root, in, snap)
			if len(diagnostics) > 0 {
				fmt.Fprintln(diagnostics[0], "Main Hook recovered the original pending Writer; continuing the same actual event. Recovery alone does not establish professional reception.")
			}
			return false
		}
	}
	command := s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " runtime stage-answer recover --root " + s7CaptureShellQuote(root)
	message := "Runtime reports a pending durable write. Automatic original Writer recovery could not complete: " + recoveryErr.Error() + ". Preserve original answers and accepted receipts; this system failure does not request author edits or another save. Original recovery diagnostic entry: " + command + ". Pending alone does not establish a professional stage, owner, or completion."
	if len(diagnostics) > 0 {
		fmt.Fprintln(diagnostics[0], message)
	}
	_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": message}})
	return true
}
