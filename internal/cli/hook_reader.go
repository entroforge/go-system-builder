package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

type hookReaderKey struct{}
type hookReader struct {
	Kind        string
	Actor       string
	Owner       string
	References  []string
	Receipts    []string
	Retired     bool
	snapshot    runtime.Snapshot
	snapshotErr error
}

// The fallback recognizes actual top-level platform events, never a directory
// or an unsuccessful native route. It performs no reads and also serves direct
// callers which have not passed through evaluate.
func hookReaderIsMain(in policy.Input) bool {
	if reader, ok := contextForHook(in).Value(hookReaderKey{}).(*hookReader); ok {
		return reader.Kind == "Main"
	}
	return hookInputIsMain(in)
}

// Classify the platform payload without consulting an earlier provisional
// reader. A successful routing snapshot can then replace a failed observation.
func hookInputIsMain(in policy.Input) bool {
	if (in.Runtime.Agent != nil && in.Runtime.Agent.ID != "") || in.Event == "SubagentStart" || in.Event == "SubagentStop" || in.Event == "TeammateIdle" || in.AgentType != "" || in.AgentTranscriptPath != "" || in.TeammateName != "" || in.TeamName != "" {
		return false
	}
	if actor := in.EffectiveAgentID(); actor != "" {
		return actor == "Main"
	}
	switch in.Event {
	case "SessionStart", "PreToolUse", "PostToolUse", "PostToolUseFailure", "Stop", "PreCompact", "ConfigChange", "SessionEnd", "UserPromptSubmit":
		return true
	}
	return false
}

// Cache only presentation identity and original references. Original payload
// identities remain intact; domain receivers still validate their own authors.
func withHookReader(root string, in policy.Input) policy.Input {
	if _, ok := contextForHook(in).Value(hookReaderKey{}).(*hookReader); ok {
		return in
	}
	in.Context = context.WithValue(contextForHook(in), hookReaderKey{}, resolveHookReader(root, in))
	return in
}

func resolveHookReader(root string, in policy.Input, supplied ...runtime.Snapshot) *hookReader {
	reader := &hookReader{Kind: "Unknown", Actor: in.EffectiveAgentID(), Owner: in.EffectiveAgentID()}
	if err := contextForHook(in).Err(); err != nil {
		reader.snapshotErr = err
		return reader
	}
	nested := (in.Runtime.Agent != nil && in.Runtime.Agent.ID != "") || in.Event == "SubagentStart" || in.Event == "SubagentStop" || in.Event == "TeammateIdle" || in.AgentType != "" || in.AgentTranscriptPath != "" || in.TeammateName != "" || in.TeamName != ""
	if reader.Owner == "" && in.Runtime.Agent != nil {
		reader.Owner = in.Runtime.Agent.ID
	}
	associationFailed := false
	if reader.Actor == "" && !nested && in.SessionID != "" && in.CWD != "" {
		// SessionOwner first reads Runtime, although an unmarked Main root
		// cannot be a registered Worker directory. Preserve that already known
		// audience when Runtime itself is currently unreadable.
		cwd, cwdErr := workspace.Canonical(in.CWD)
		main, mainErr := workspace.Canonical(root)
		atMain := cwdErr == nil && mainErr == nil && cwd == main && workspace.RequireMain(root) == nil
		if !atMain {
			ctx, cancel := context.WithTimeout(contextForHook(in), 2*time.Second)
			owner, err := workspace.SessionOwner(ctx, root, in.CWD, in.SessionID)
			cancel()
			associationFailed = err != nil
			if err == nil && owner != "" {
				reader.Owner = owner
			}
		}
	}
	if err := contextForHook(in).Err(); err != nil {
		reader.snapshotErr = err
		return reader
	}
	// A marked Worker directory with no usable session association is unknown,
	// even for an otherwise official top-level event. The directory identifies
	// a boundary, never an owner or a new responsibility.
	if reader.Owner == "" && !nested && in.CWD != "" {
		if _, marked, err := workspace.PointerRoot(in.CWD); marked || err != nil {
			associationFailed = true
		}
	}
	if reader.Owner != "" && reader.Owner != "Main" || associationFailed {
		var snap runtime.Snapshot
		var err error
		if len(supplied) > 0 {
			snap = supplied[0]
		} else {
			snap, err = runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).WithContext(contextForHook(in)).ObservationSnapshot()
		}
		if err == nil {
			reader.snapshot = snap
			state := snap.State
			// The session resolver rejects retired execution authority. Its original
			// session/path association remains useful for read-only responsibility
			// feedback and must never turn a late Worker into a new Main.
			if associationFailed && reader.Owner == "" {
				if registry, decodeErr := workspace.Decode(state); decodeErr == nil && registry != nil {
					for _, execution := range registry.History {
						if execution.PlatformSessionID == in.SessionID && in.SessionID != "" && samePath(execution.Path, in.CWD) {
							reader.Owner, reader.Retired = execution.AgentID, true
							break
						}
					}
				}
			}
			for _, row := range objectSliceAny(mapFieldCLI(state, "entities")["work_packets"]) {
				binding := mapFieldCLI(row, "binding")
				if reader.Owner == "" || reader.Owner == "Main" || binding["owner_agent_id"] != reader.Owner && binding["owner"] != reader.Owner {
					continue
				}
				reader.Kind = "Worker"
				for _, key := range []string{"view_path", "answer_path"} {
					if ref := stringValue(row[key]); ref != "" {
						reader.References = appendUnique(reader.References, resolveRootPath(root, ref))
					}
				}
				if stringValue(row["superseded_by"]) != "" {
					reader.Retired = true
				}
			}
			for _, row := range objectSliceAny(mapFieldCLI(state, "entities")["submissions"]) {
				if row["owner_agent_id"] != reader.Owner && row["owner"] != reader.Owner {
					continue
				}
				if stringValue(row["superseded_by"]) != "" {
					reader.Retired = true
				}
				if stringValue(row["content_sha256"]) != "" && stringValue(row["packet_id"]) != "" {
					reader.Receipts = appendUnique(reader.Receipts, fmt.Sprintf("original answer %s is accepted; original control status=%s", row["packet_id"], stringValue(row["control_status"])))
				}
			}
			if agent := contextAgent(state, reader.Owner); len(agent) > 0 {
				reader.Kind = "Worker"
				reader.Retired = reader.Retired || workstate.AgentActivity(agent) == "stopped"
			}
			if registry, err := workspace.Decode(state); err == nil && registry != nil {
				executions := append([]workspace.Execution{}, registry.History...)
				for _, execution := range registry.Executions {
					executions = append(executions, execution)
				}
				for _, execution := range executions {
					if execution.AgentID != reader.Owner {
						continue
					}
					reader.Kind = "Worker"
					reader.Retired = reader.Retired || execution.Status == "retired"
					if execution.WorkPacketID != "" {
						reader.References = appendUnique(reader.References, execution.WorkPacketID)
					}
					if execution.AssignmentID != "" {
						reader.References = appendUnique(reader.References, execution.AssignmentID)
					}
				}
			}
		} else {
			reader.snapshotErr = err
		}
	}
	if reader.Kind != "Worker" {
		if nested {
			reader.Kind = "Helper"
		} else if !associationFailed && hookInputIsMain(in) && (reader.Owner == "" || reader.Owner == "Main") {
			reader.Kind = "Main"
		}
	}
	return reader
}

// Ordinary helper observations never run the Main Controller. A late Worker
// in a returned planning stage retains its own original references and facts.
// This is presentation only: saves still use the original domain receiver.
func hookReaderObservation(root string, in policy.Input, out, diagnostic io.Writer) (bool, int) {
	reader, ok := contextForHook(in).Value(hookReaderKey{}).(*hookReader)
	if !ok || reader.Kind == "Main" {
		return false, 0
	}
	if reader.Kind != "Worker" {
		return true, 0
	}
	if in.Event == "PostToolUse" {
		// A registered top-level Worker session may omit the logical owner in the
		// platform payload. Adapt a local receiver copy, retaining the cached actor.
		received := in
		if received.EffectiveAgentID() == "" {
			received.AgentID = reader.Owner
		}
		if handled, code := receiveSavedQuestionnaire(root, received, out, diagnostic); handled {
			return true, code
		}
		return true, 0
	}
	if !reader.Retired && (in.Event == "Stop" || hook.IsStopIdleEvent(in.Event)) {
		// Keep original Worker completion gates, including separately launched
		// sessions whose platform payload has no logical actor. Only this local
		// consumer copy carries the existing session association.
		checked := in
		if checked.EffectiveAgentID() == "" {
			checked.AgentID = reader.Owner
		}
		if loaded, err := hookctx.Load(root, reader.Owner); err == nil {
			checked.Runtime = loaded
		}
		feedback := func(decision policy.Decision) (bool, int) {
			if isDenyingHookDecision(decision.Decision) {
				fmt.Fprintln(diagnostic, hook.RenderStopBlockFeedback(decision, ""))
				return true, 2
			}
			return true, 0
		}
		if handled, decision := documentReviewStopPreflight(root, checked); handled {
			return feedback(decision)
		}
		if handled, decision := workspaceWorkerStopPreflight(root, checked); handled {
			return feedback(decision)
		}
		if checked.Event == "Stop" {
			checked.Event = "SubagentStop"
		}
		if decision, denied := hook.StopIdleDecision(root, checked); denied {
			return feedback(decision)
		}
		return true, 0
	}
	if in.Event != "PreToolUse" && in.Event != "SessionStart" && in.Event != "SubagentStart" && !reader.Retired {
		return false, 0
	}
	if in.Event == "PreCompact" {
		return true, 0
	}
	message, specialized := "", false
	if !reader.Retired {
		observed := in
		if observed.EffectiveAgentID() == "" {
			observed.AgentID = reader.Owner
		}
		// The original S9 producer owns current paths and reception facts,
		// including unavailable/unknown feedback. Never append stale generic
		// Plan references or receipts to an applicable professional projection.
		message, specialized = s9PacketQuestionnaireLifecycleContext(root, observed, reader.snapshot)
	}
	if !specialized {
		if len(reader.References) == 0 {
			return true, 0
		}
		message = fmt.Sprintf("Original Worker responsibility for %s: %s. Continue only this original responsibility; the global stage does not assign Main's current work to this Worker.", reader.Owner, strings.Join(reader.References, ", "))
		if reader.Retired {
			message = fmt.Sprintf("Original Worker responsibility for %s is retired/superseded: %s. Preserve its original receipt and completed work; no new Main responsibility is assigned by the current stage.", reader.Owner, strings.Join(reader.References, ", "))
		}
		if !reader.Retired && mapFieldCLI(reader.snapshot.State, "lifecycle")["state"] == "building" {
			observed := in
			if observed.EffectiveAgentID() == "" {
				observed.AgentID = reader.Owner
			}
			// Reuse the original Builder projection, including the exact private
			// PLAN/Result paths and its fixed engineering read order.
			if own := stageBuilderQuestionLifecycleContext(root, observed, reader.snapshot); own != "" {
				message = own
			}
		}
		if len(reader.Receipts) > 0 {
			message += " Recorded facts: " + strings.Join(reader.Receipts, "; ") + ". Accepted answers are not requested again."
		}
	}
	output, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": in.Event, "additionalContext": message}})
	output, commit := hook.PrepareResponsibilityContext(root, in.SessionID, reader.Owner, in.Event, "original-worker", strings.Join(reader.References, "|"), output)
	if len(output) > 0 {
		if _, err := out.Write(append(output, '\n')); err != nil {
			return true, 1
		}
	}
	commit()
	return true, 0
}
