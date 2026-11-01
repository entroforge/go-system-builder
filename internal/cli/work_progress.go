package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

const progressSourceProjectionByteLimit int64 = 4 << 20

func progressText(v any) string { s, _ := v.(string); return s }
func progressJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

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

// projectWorkProgress uses only the current Runtime responsibility. Product
// bytes, historical sources and Git identities are not proof of progress. If
// the platform actor cannot be matched unambiguously, the diagnostic is unknown.
func projectWorkProgress(root string, request policy.Input, snap runtime.Snapshot) (hook.ProgressFacts, *workspace.Execution, error) {
	f := hook.ProgressFacts{Session: request.SessionID, Sources: map[string]string{}}
	life, _ := snap.State["lifecycle"].(map[string]any)
	if life["state"] == "inactive" || life["state"] == "paused" || snap.State["bound_req"] == nil {
		return f, nil, nil
	}
	owner := request.EffectiveAgentID()
	if owner == "" {
		owner = "Main"
	}
	f.Owner, f.RecoveryOwner = owner, "Main"
	f.Binding = progressJSON([]any{snap.State["runtime_id"], life["state"], life["phase"], life["phase_revision"], owner})
	if owner == "Main" {
		f.Known = true
		f.Semantic = progressJSON([]any{life["state"], life["phase"], life["phase_revision"]})
		return f, nil, nil
	}
	entities, _ := snap.State["entities"].(map[string]any)
	rows, _ := entities["agents"].([]any)
	var found map[string]any
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] == owner {
			if found != nil {
				return f, nil, nil
			}
			found = row
		}
	}
	if found == nil {
		return f, nil, nil
	}
	f.Known = true
	f.Binding += progressJSON([]any{found["assignment_id"], found["task_ids"]})
	f.Semantic = progressJSON([]any{found["state"], found["activity"], found["completion_reported_ref"], found["work_blocked_ref"]})
	f.Complete = found["state"] == "done" || found["state"] == "cancelled"
	f.Waiting = found["state"] == "blocked" || found["state"] == "waiting"
	return f, nil, nil
}

func renderProgressNotice(stdout io.Writer, notice *hook.ProgressNotice) {
	data, _ := json.Marshal(notice)
	if len(data) > 780 {
		return
	}
	_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": "WORK PROGRESS: " + string(data)}})
}
