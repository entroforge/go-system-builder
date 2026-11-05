package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

const progressSourceProjectionByteLimit int64 = 4 << 20

func progressText(v any) string { s, _ := v.(string); return s }
func progressJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// progressFileStamp is a file's own length and modification time. It states
// that an ordinary file changed; it never authenticates the bytes.
func progressFileStamp(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "missing"
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

// progressBranchRefStamp is the bound development branch's own ref record:
// its length and modification time. The commit layer records the commit
// change in that ref, so the observation needs no Git object ID, content
// digest or source summary (L3-S4-task-split.md §10.2).
func progressBranchRefStamp(root string, state map[string]any) string {
	ref, err := fileview.DevelopmentRef(state)
	if err != nil {
		return "unknown"
	}
	branch := strings.TrimPrefix(ref, "refs/heads/")
	if branch == "" {
		return "unknown"
	}
	gitDir := filepath.Join(root, ".git")
	if info, err := os.Stat(gitDir); err != nil || !info.IsDir() {
		return "unknown"
	}
	return progressFileStamp(filepath.Join(gitDir, "refs", "heads", filepath.FromSlash(branch)))
}

func applyResponsibilityProgress(f *hook.ProgressFacts, responsibility hook.ResponsibilityFacts) bool {
	if responsibility.ConsumerPending {
		f.Waiting = true
	}
	switch responsibility.Status {
	case workstate.Done:
		f.Complete = true
		return true
	case workstate.Cancelled, workstate.Blocked:
		f.Waiting = true
		return true
	default:
		return responsibility.ConsumerPending
	}
}

func addProgressSource(sources map[string]string, remaining *int64, sourceRoot, ref string) (string, error) {
	ref, _, _ = strings.Cut(ref, "#")
	ref = filepath.ToSlash(ref)
	if ref == "" {
		return "", fmt.Errorf("necessary source path is empty")
	}
	path := filepath.Clean(filepath.Join(sourceRoot, filepath.FromSlash(ref)))
	if digest, ok := sources[path]; ok {
		return digest, nil
	}
	if remaining == nil || *remaining < 0 {
		return "", fmt.Errorf("necessary source projection exceeds its %d-byte aggregate budget", progressSourceProjectionByteLimit)
	}
	data, err := workpacket.ReadFile(sourceRoot, ref, *remaining)
	if err != nil {
		return "", fmt.Errorf("read necessary source within the %d-byte aggregate projection budget: %w", progressSourceProjectionByteLimit, err)
	}
	*remaining -= int64(len(data))
	sources[path] = "present"
	return "present", nil
}

func projectProgressSourceInputs(sources map[string]string, remaining *int64, sourceRoot string, refs []workpacket.Reference) error {
	for _, ref := range refs {
		if _, err := addProgressSource(sources, remaining, sourceRoot, ref.Path); err != nil {
			return err
		}
	}
	return nil
}

// progressDirectoryRef validates a Main milestone read declaration that names
// a directory with a trailing slash. Formal packet sources remain individual
// file references and do not use this adapter.
func progressDirectoryRef(root, raw string) (string, error) {
	ref, _, _ := strings.Cut(raw, "#")
	if !strings.HasSuffix(ref, "/") {
		return "", fmt.Errorf("milestone directory source %q must end in /", raw)
	}
	dir := strings.TrimSuffix(ref, "/")
	local := filepath.FromSlash(dir)
	if dir == "" || strings.ContainsAny(dir, `\\:#*?[]`) || filepath.IsAbs(local) ||
		!filepath.IsLocal(local) || filepath.ToSlash(filepath.Clean(local)) != dir || dir == "." || dir == ".." || strings.HasPrefix(dir, "../") {
		return "", fmt.Errorf("milestone directory source %q is not a normalized local path", raw)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve progress source root: %w", err)
	}
	current := rootAbs
	for _, part := range strings.Split(dir, "/") {
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("inspect milestone directory source %q: %w", raw, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("milestone directory source %q contains a symlink", raw)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("milestone directory source %q contains a non-directory component", raw)
		}
	}
	return dir, nil
}

// projectMilestoneReadSources treats trailing-slash milestone refs as bounded
// directory scopes. It uses the existing source walker so each eligible file
// is read through workpacket.ReadFile, hashed, and charged to the shared byte
// and entry bounds. These hashes support read diagnostics only; they are not
// formal packet source authority.
func projectMilestoneReadSources(sources map[string]string, remaining *int64, root string, reads []any) error {
	if len(reads) > 256 {
		return fmt.Errorf("milestone source observation exceeds 256 entries")
	}
	var directories []string
	var files []string
	seenDirectories := make(map[string]bool)
	for _, raw := range reads {
		ref := progressText(raw)
		pathRef, _, _ := strings.Cut(ref, "#")
		// Runtime/journal observer writes cannot count as professional progress.
		// This matches the original addSource boundary in projectWorkProgress.
		if pathRef == ".claude/loop-state.json" || pathRef == ".claude/loop-events.jsonl" {
			continue
		}
		if strings.HasSuffix(pathRef, "/") {
			dir, err := progressDirectoryRef(root, ref)
			if err != nil {
				return err
			}
			pattern := dir + "/**"
			if !seenDirectories[pattern] {
				seenDirectories[pattern] = true
				directories = append(directories, pattern)
			}
			continue
		}
		files = append(files, ref)
	}
	if len(directories) > 0 {
		projected, err := progressOwnedCode(root, directories, remaining)
		if err != nil {
			return fmt.Errorf("read necessary source within the %d-byte aggregate projection budget: %w", progressSourceProjectionByteLimit, err)
		}
		for ref, digest := range projected {
			path := filepath.Clean(filepath.Join(root, filepath.FromSlash(ref)))
			if existing, ok := sources[path]; ok && existing != digest {
				return fmt.Errorf("necessary source %q changed during directory projection", ref)
			}
			sources[path] = digest
		}
	}
	for _, ref := range files {
		if _, err := addProgressSource(sources, remaining, root, ref); err != nil {
			return err
		}
	}
	return nil
}

func observeWorkProgress(root string, request policy.Input, stderr io.Writer) (*hook.ProgressNotice, bool, bool) {
	if request.Event != "PostToolUse" || request.SessionID == "" {
		return nil, false, false
	}
	snap, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).ObservationSnapshot()
	if err != nil {
		return nil, false, false
	}
	life := mapFieldCLI(snap.State, "lifecycle")
	planning := life["state"] == "planning" && (life["phase"] == "contracts" || life["phase"] == "tasks")
	switch request.ToolName {
	case "Read", "Glob", "Grep":
		// These events do not run saved-answer consumers; do not consume a
		// change observation that the next successful save/tool still needs.
		if planning {
			return nil, false, false
		}
	case "Bash", "Write", "Edit", "MultiEdit":
	default:
		if !planning {
			return nil, false, false
		}
	}
	if request.Error != "" {
		return nil, false, false
	}
	if failed, _ := request.ToolResponse["is_error"].(bool); failed {
		return nil, false, false
	}
	facts, execution, err := projectWorkProgress(root, request, snap)
	if err != nil {
		fmt.Fprintf(stderr, "work progress coverage incomplete: %v\n", err)
		return nil, false, false
	}
	event := hook.ProgressEvent{Action: request.ToolName}
	switch request.ToolName {
	case "Read":
		path := progressText(request.ToolInput["file_path"])
		if !filepath.IsAbs(path) {
			path = filepath.Join(request.CWD, path)
		}
		if filepath.Clean(path) == filepath.Join(root, ".claude/loop-state.json") || filepath.Clean(path) == filepath.Join(root, ".claude/loop-events.jsonl") {
			event.Action, event.Known = "status", true
		} else {
			event.Read = actualProgressRead(root, request, facts.Sources)
			event.Known = event.Read != nil
		}
	case "Bash":
		event.Action = "status"
		event.Known = progressStatusCommand(root, request, execution)
	case "Write", "Edit", "MultiEdit":
		event.Known = true
	}
	notice, known, changed, err := hook.ObserveWorkProgress(root, facts, event)
	if err != nil {
		fmt.Fprintf(stderr, "work progress diagnostic deferred: %v\n", err)
	}
	return notice, known && facts.Known && !facts.Waiting && planning, changed
}

// projectWorkProgress projects the original responsibility coordinates of the
// current actor: the authoritative root, the original responsibility, its
// declared source range and the actual answer. Product bytes, historical
// sources and Git identities are not proof of progress. If the platform actor
// cannot be matched unambiguously, the diagnostic is unknown.
func projectWorkProgress(root string, request policy.Input, snap runtime.Snapshot) (hook.ProgressFacts, *workspace.Execution, error) {
	f := hook.ProgressFacts{Session: request.SessionID, Sources: map[string]string{}}
	life, _ := snap.State["lifecycle"].(map[string]any)
	if life["state"] == "inactive" || life["state"] == "paused" || snap.State["bound_req"] == nil {
		return f, nil, nil
	}
	// An observer that is neither Main nor a registered Worker owns no
	// responsibility here. Its observations stay unknown.
	if reader, ok := contextForHook(request).Value(hookReaderKey{}).(*hookReader); ok && reader.Kind != "Main" && reader.Kind != "Worker" {
		return f, nil, nil
	}
	// S9 professional listeners and saved-answer consumers own the S9 progress.
	// This optional sampler has no S9 typed-packet or native-actor coverage;
	// unknown telemetry is not missing work.
	if runtime.PacketEnabled(snap.State) && life["state"] == "bug_resolution" && s9ProfessionalPhase(progressText(life["phase"])) {
		return f, nil, nil
	}
	owner := request.EffectiveAgentID()
	if owner == "" {
		owner = "Main"
	}
	entities, _ := snap.State["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	matches := 0
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] == owner {
			matches++
		}
	}
	if matches > 1 {
		// An ambiguous platform actor is unknown, never a confident projection.
		return f, nil, nil
	}
	for _, key := range []string{"agents", "tasks", "teams"} {
		if rows, ok := entities[key].([]any); ok && len(rows) > 32 {
			return f, nil, fmt.Errorf("current context coverage exceeds thirty-two %s", key)
		}
	}
	loaded, err := hookctx.FromSnapshot(root, request.EffectiveAgentID(), snap)
	if err != nil {
		// Native S7 actors need not equal logical questionnaire owners. An
		// absent actor makes this optional sampler unknown, not missing work.
		actor := request.EffectiveAgentID()
		if runtime.PacketEnabled(snap.State) && life["state"] == "verification" && actor != "" && actor != "Main" &&
			err.Error() == fmt.Sprintf("Agent %q not found in runtime", actor) {
			return f, nil, nil
		}
		return f, nil, err
	}
	if owner != "Main" && matches == 0 {
		// An unregistered actor has no original responsibility here.
		return f, nil, nil
	}
	b, err := workspace.Decode(snap.State)
	if err != nil {
		return f, nil, err
	}
	var execution *workspace.Execution
	if b != nil && owner != "Main" {
		for _, e := range b.Executions {
			if e.AgentID == owner && e.RuntimeID == workspace.RuntimeID(snap.State) && e.BaselineGeneration == workspace.Generation(snap.State) && e.Status == "ready" {
				if execution != nil {
					return f, nil, fmt.Errorf("multiple current execution responsibilities")
				}
				copy := e
				execution = &copy
			}
		}
		// TR-004 keeps the original reviewer directory in history. Observe
		// that cancelled responsibility without asking it to work again.
		if execution == nil {
			for _, e := range b.History {
				if e.Purpose == workspace.DocumentReviewPurpose && e.Status == "retired" && e.AgentID == owner && e.RuntimeID == workspace.RuntimeID(snap.State) && e.BaselineGeneration == workspace.Generation(snap.State) && filepath.Clean(request.CWD) == filepath.Clean(e.Path) {
					copy := e
					execution = &copy
					break
				}
			}
		}
	}
	actualRoot := root
	if execution != nil {
		actualRoot = execution.Path
	}
	if request.CWD == "" || filepath.Clean(request.CWD) != filepath.Clean(actualRoot) || (owner != "Main" && loaded.PolicyContext.Agent == nil) {
		return f, nil, fmt.Errorf("registered owner/execution directory unavailable")
	}
	// The responsibility is resolved. Only now may the facts stand.
	f.Owner, f.RecoveryOwner = owner, "Main"
	f.Binding = progressJSON([]any{snap.State["runtime_id"], life["state"], life["phase"], life["phase_revision"], owner})
	f.Known = true
	remaining := progressSourceProjectionByteLimit
	semantic := map[string]any{}
	if owner == "Main" && life["state"] == "planning" && (life["phase"] == "contracts" || life["phase"] == "tasks") {
		// Main observes the current planning answer and the committed source it
		// belongs to. Neither observation computes or records a digest, a content
		// hash or a Git object ID (L3-S4-task-split.md §10.2: "Hook 不计算、登记或
		// 校验来源摘要、内容散列或 Git 对象 ID"): the answer's own length and
		// modification time are its change signal, and the bound branch's own ref
		// is where the commit layer records the commit change.
		catalog, err := transition.LoadCatalog(root)
		if err != nil {
			return f, nil, err
		}
		for _, rule := range catalog.Definition.FileSources {
			if rule.Source == "disk" && (rule.Path == "." || rule.Path == "docs" || strings.HasPrefix(rule.Path, "docs/")) {
				// A disk override of the professional source is observed
				// conservatively: the original observer keeps its own contract
				// instead of adding a source qualification rule here.
				f.Known = false
				return f, nil, nil
			}
		}
		row, rowErr := registeredPlanningRow(snap.State)
		if rowErr != nil {
			return f, nil, rowErr
		}
		if row != nil {
			if answerPath := progressText(row["answer_path"]); answerPath != "" {
				semantic["answer"] = answerPath + "|" + progressFileStamp(filepath.Join(root, filepath.FromSlash(answerPath)))
			}
		}
		semantic["source"] = progressBranchRefStamp(root, snap.State)
	}
	if owner == "Main" {
		// Main observes the current milestone: the missing professional fact and
		// the declared necessary read range.
		milestone, _ := snap.State["milestone"].(map[string]any)
		if milestone == nil || milestone["lifecycle_state"] != life["state"] {
			// An insufficient read range never forces a stagnation verdict.
			f.Known = false
			return f, nil, nil
		}
		f.Gap = progressText(milestone["action"]) + ": " + progressJSON(milestone["missing"])
		if milestone["human_required"] == true {
			f.Waiting = true
		}
		semantic["checkpoint"] = []any{milestone["action"], milestone["missing"], milestone["blocker"], milestone["blocked"]}
		if reads, ok := milestone["read"].([]any); ok {
			// Old persisted milestones may still contain the S6 category labels.
			// Use the same original-document projection as newly produced Next.
			declared := make([]string, 0, len(reads))
			for _, raw := range reads {
				declared = append(declared, progressText(raw))
			}
			resolved := resolveBoundREQRead(declared, snap.State)
			projected := make([]any, len(resolved))
			for i, ref := range resolved {
				projected[i] = ref
			}
			if err := projectMilestoneReadSources(f.Sources, &remaining, root, projected); err != nil {
				return f, nil, err
			}
		}
	} else {
		// The Worker observes its own original responsibility coordinates.
		if loaded.PolicyContext.Agent != nil && loaded.PolicyContext.Agent.ID == owner {
			agent := loaded.PolicyContext.Agent
			assignment := hook.AssignmentForAgent(loaded.Assignments, owner)
			packetReceived := loaded.PolicyContext.PacketMode && submission.CompletedOwner(root, owner)
			responsibility := hook.CurrentResponsibility(root, snap, agent, assignment, packetReceived)
			f.Binding += progressJSON([]any{responsibility.CurrentResponsibility, responsibility.AssignmentID})
			f.Semantic = progressJSON([]any{responsibility.Status, responsibility.DeliveryRecorded, responsibility.ConsumerPending, responsibility.NextAction})
			if applyResponsibilityProgress(&f, responsibility) {
				return f, execution, nil
			}
		}
		// The current S5 responsibility and the range it actually owns.
		packets, _ := entities["work_packets"].([]any)
		var current *workpacket.Packet
		for _, raw := range packets {
			row, _ := raw.(map[string]any)
			// FindWorkPacket is the original S5 lookup, not a decoder for the
			// S7/S8/S9 typed packets retained in this shared history. Their
			// original consumers own those responsibilities.
			if row["template_id"] != "s5-document-review-v1" {
				continue
			}
			binding, _ := row["binding"].(map[string]any)
			if binding == nil || (binding["owner"] != owner && binding["owner_agent_id"] != owner) {
				// Another owner's issued packet is not this responsibility.
				continue
			}
			p, ok, err := runtime.FindWorkPacket(snap.State, progressText(row["packet_id"]))
			if err != nil || !ok {
				return f, execution, fmt.Errorf("issued responsibility unavailable")
			}
			if p.Binding.Generation != workspace.Generation(snap.State) || p.Binding.RuntimeID != workspace.RuntimeID(snap.State) || progressJSON(p.Binding.StageEntry) != progressJSON(life["phase_revision"]) {
				continue
			}
			reception, accepted, err := runtime.FindSubmission(snap.State, p.ID)
			if err != nil {
				return f, execution, err
			}
			if reception.RevisionPacketID != "" || life["state"] != "document_verification" {
				continue
			}
			if current != nil {
				return f, execution, fmt.Errorf("multiple current packets require a responsibility selector")
			}
			copy := p
			current = &copy
			f.Complete, f.Waiting = accepted, accepted
		}
		if current != nil {
			f.Binding += current.ID
			f.Gap = "complete original " + current.Binding.Responsibility + " answer and receive its domain receipt"
			f.RecoveryOwner = owner
			answerRoot, answerRef, sourceRoot := root, current.AnswerPath, root
			if execution != nil {
				paths, err := b.WorkerSubmissionPaths(*execution)
				if err != nil {
					return f, execution, err
				}
				answerRoot, answerRef = execution.Path, paths.Answer
				sourceRoot = filepath.Join(execution.Path, ".claude/inputs")
			}
			if data, err := workpacket.ReadFile(answerRoot, answerRef, 256<<10); err == nil {
				var draft workpacket.Draft
				if json.Unmarshal(data, &draft) == nil {
					semantic["answer"] = draft.Answer
				} else {
					semantic["answer"] = string(data)
				}
				_, issues := workpacket.ValidateAnswer(*current, data)
				if len(issues) > 0 {
					f.Gap = issues[0].Pointer + ": " + issues[0].Expected
				} else if !f.Complete {
					f.Gap = "receive the original complete answer through its issued Save adapter"
					f.RecoveryOwner = "Main"
				}
			} else if !os.IsNotExist(err) {
				return f, execution, err
			}
			if err := projectProgressSourceInputs(f.Sources, &remaining, sourceRoot, current.Inputs); err != nil {
				return f, execution, err
			}
		} else {
			// The Worker still holds its original Assignment closing contract.
			var own *hookctx.AssignmentContext
			for _, a := range loaded.Assignments {
				if a.OwnerAgentID == owner {
					if own != nil {
						return f, execution, fmt.Errorf("multiple assignments require an original selector")
					}
					copy := a
					own = &copy
				}
			}
			if own == nil {
				f.Known = false
				return f, execution, nil
			}
			f.Binding += own.AssignmentID
			f.Gap = "satisfy original Assignment closing contract and receive delivery"
			f.Waiting = loaded.PolicyContext.Agent.LifecycleActivity() == "queued" || loaded.PolicyContext.Agent.LifecycleActivity() == "blocked"
			switch own.ReportStatus {
			case "reported", "verified", "acknowledged", "cleanup_pending", "complete":
				f.Waiting = true
			}
			semantic["assignment"] = []any{own.State, own.ReportStatus, own.CompletionRef, own.CompletionAckRef, loaded.PolicyContext.Agent.State, loaded.PolicyContext.Agent.PlanReportedRef}
			for _, ref := range own.ReadPaths {
				sourceRoot := root
				if execution != nil {
					sourceRoot = filepath.Join(execution.Path, ".claude/inputs")
				}
				cleanRef, _, _ := strings.Cut(ref, "#")
				if sourceRoot == root && (cleanRef == ".claude/loop-state.json" || cleanRef == ".claude/loop-events.jsonl") {
					continue
				}
				if _, err := addProgressSource(f.Sources, &remaining, sourceRoot, ref); err != nil {
					return f, execution, err
				}
			}
		}
	}
	semantic["sources"] = f.Sources
	f.Semantic = progressJSON(semantic)
	return f, execution, nil
}

func renderProgressNotice(stdout io.Writer, notice *hook.ProgressNotice) {
	data, _ := json.Marshal(notice)
	if len(data) > 780 {
		return
	}
	_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": "WORK PROGRESS: " + string(data)}})
}
