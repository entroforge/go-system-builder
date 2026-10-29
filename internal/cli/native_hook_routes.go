package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type nativeRoutingKey struct{}

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
				handled, code := nativeReviewHook(root, in, out, diagnostic)
				if !handled {
					refreshRecoveredHookReader(root, in, snap)
				}
				return handled, code
			}
			if runtime.PacketEnabled(snap.State) && nativeS9Available(root, snap.State) {
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
	if handled, code := nativeMainReturnHook(root, in, out); handled {
		return true, code
	}
	if handled, code := runSavedS9RepairResultHook(root, in, out, diagnostic); handled {
		return true, code
	}
	if handled, code := nativeS6Hook(root, in, out, diagnostic); handled {
		return true, code
	}
	if handled, code := nativeS9Hook(root, in, out, diagnostic); handled {
		return true, code
	}
	return nativeReviewHook(root, in, out, diagnostic)
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
		_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": message}})
	} else {
		_ = json.NewEncoder(out).Encode(map[string]any{"systemMessage": message})
	}
	return true, 0
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
