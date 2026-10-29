package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

type planningChangeKey struct{}

// This is disposable event observation, not issuance, evidence or a stage gate.
// Only S2/S3 use this boundary; other stages retain their original listeners.
type planningToolChange struct {
	Active, Known, MaterialsChanged bool
	SavedAnswers                    map[string]bool
}

type planningToolStamp struct {
	Responsibility string            `json:"responsibility"`
	Materials      map[string]string `json:"materials"`
	Answers        map[string]string `json:"answers"`
}

func nativeFileSave(request policy.Input) bool {
	if !successfulPlanningTool(request) {
		return false
	}
	switch request.ToolName {
	case "Write", "Edit", "MultiEdit":
		return true
	}
	return false
}

func successfulPlanningTool(request policy.Input) bool {
	failed, _ := request.ToolResponse["is_error"].(bool)
	return request.Event == "PostToolUse" && request.Error == "" && !failed
}

func planningChangeFrom(request policy.Input) planningToolChange {
	change, _ := contextForHook(request).Value(planningChangeKey{}).(planningToolChange)
	return change
}

func actualPlanningAnswerSave(request policy.Input, path string) bool {
	if nativeFileSave(request) {
		return true
	}
	return successfulPlanningTool(request) && planningChangeFrom(request).SavedAnswers[filepath.Clean(path)]
}

// Observe only the current answer and planning material metadata. Directory
// walks do not parse, validate or hash the full source closure. The answer hash
// distinguishes an actual saved draft from a pre-existing unreceived PASS.
func observePlanningToolChange(root string, request policy.Input, stderr io.Writer) policy.Input {
	if request.Event != "PreToolUse" && request.Event != "PostToolUse" && request.Event != "PostToolUseFailure" {
		return request
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).ObservationSnapshot()
	if err != nil {
		return request
	}
	life := mapFieldCLI(snapshot.State, "lifecycle")
	if !runtime.PacketEnabled(snapshot.State) || life["state"] != "planning" || (life["phase"] != "design" && life["phase"] != "contracts") {
		return request
	}
	change := planningToolChange{Active: true, SavedAnswers: map[string]bool{}}
	request.Context = context.WithValue(contextForHook(request), planningChangeKey{}, change)
	if !mainQuestionnaireViewer(request) {
		return request
	}
	switch request.ToolName {
	case "Read", "Glob", "Grep":
		return request // Known read-only events need no mutation observation.
	}
	key := planningToolCacheKey(root, request)
	if key == "" {
		return request
	}
	if request.Event == "PostToolUseFailure" || (request.Event == "PostToolUse" && !successfulPlanningTool(request)) {
		_ = os.Remove(key)
		return request
	}
	stamp, err := currentPlanningToolStamp(root, request, snapshot)
	if err != nil {
		_ = os.Remove(key)
		fmt.Fprintf(stderr, "planning change observation unavailable: %v\n", err)
		return request
	}
	if request.Event == "PreToolUse" {
		if err := writePlanningToolStamp(key, stamp); err != nil {
			fmt.Fprintf(stderr, "planning change observation unavailable: %v\n", err)
		}
		return request
	}
	data, err := os.ReadFile(key)
	_ = os.Remove(key) // A callback cannot replay an earlier tool's mutation.
	var before planningToolStamp
	if err != nil || json.Unmarshal(data, &before) != nil || before.Responsibility != stamp.Responsibility {
		return request
	}
	change.Known = true
	change.MaterialsChanged = !reflect.DeepEqual(before.Materials, stamp.Materials)
	for path, digest := range stamp.Answers {
		if old, exists := before.Answers[path]; exists && old != digest && digest != "missing" {
			change.SavedAnswers[path] = true
		}
	}
	request.Context = context.WithValue(contextForHook(request), planningChangeKey{}, change)
	return request
}

func planningToolCacheKey(root string, request policy.Input) string {
	if request.SessionID == "" && request.ToolUseID == "" {
		return ""
	}
	identity, _ := json.Marshal([]any{request.SessionID, request.EffectiveAgentID(), request.CWD, request.ToolUseID, request.ToolName, request.ToolInput})
	sum := sha256.Sum256(identity)
	return filepath.Join(root, ".claude", "hook-context", "planning-tools", fmt.Sprintf("%x.json", sum))
}

func currentPlanningToolStamp(root string, request policy.Input, snapshot runtime.Snapshot) (planningToolStamp, error) {
	stamp := planningToolStamp{Materials: map[string]string{}, Answers: map[string]string{}}
	life := mapFieldCLI(snapshot.State, "lifecycle")
	row, err := registeredPlanningRow(snapshot.State)
	if err != nil {
		return stamp, err
	}
	identity, _ := json.Marshal([]any{snapshot.State["runtime_id"], snapshot.State["baseline"], life, row})
	stamp.Responsibility = fmt.Sprintf("%x", sha256.Sum256(identity))
	if row != nil {
		if path := stringValue(row["answer_path"]); path != "" {
			path = filepath.Join(root, filepath.FromSlash(path))
			data, err := os.ReadFile(path)
			switch {
			case os.IsNotExist(err):
				stamp.Answers[path] = "missing"
			case err != nil:
				return stamp, err
			default:
				stamp.Answers[path] = fmt.Sprintf("%x", sha256.Sum256(data))
			}
		}
	}
	needsPreparation := row == nil
	if row != nil {
		for _, field := range []string{"view_path", "answer_path"} {
			if _, err := os.Stat(filepath.Join(root, stringValue(row[field]))); err != nil {
				needsPreparation = true
			}
		}
	}
	if !needsPreparation {
		return stamp, nil // Normal authoring observes only the exact answer.
	}
	for _, rel := range []string{"docs/architecture", "docs/design", "docs/dev/contracts"} {
		base := filepath.Join(root, filepath.FromSlash(rel))
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if os.IsNotExist(walkErr) {
				stamp.Materials[path] = "missing"
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			stamp.Materials[path] = fmt.Sprintf("%d:%d:%d", info.Mode(), info.Size(), info.ModTime().UnixNano())
			return nil
		})
		if err != nil {
			return stamp, err
		}
	}
	// The bound commit can change after a Bash git commit without altering the
	// already-written material files. No shell text is parsed or executed here.
	if ref, err := fileview.DevelopmentRef(snapshot.State); err == nil {
		cmd := exec.CommandContext(contextForHook(request), "git", "-C", root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
		if data, err := cmd.Output(); err == nil {
			stamp.Materials["git:"+ref] = strings.TrimSpace(string(data))
		}
	}
	return stamp, nil
}

func writePlanningToolStamp(path string, stamp planningToolStamp) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".observation-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(stamp)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func planningMaterialSave(request policy.Input, root string) bool {
	if !successfulPlanningTool(request) || !mainQuestionnaireViewer(request) {
		return false
	}
	change := planningChangeFrom(request)
	if change.Known {
		return change.MaterialsChanged || len(change.SavedAnswers) > 0
	}
	// Native file saves carry an explicit operation contract even if a host
	// omitted PreToolUse. Unknown tools never inherit that contract from a path.
	if !nativeFileSave(request) {
		return false
	}
	path, ok := savedQuestionnairePath(request)
	if !ok {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return strings.HasPrefix(rel, "docs/architecture/") || strings.HasPrefix(rel, "docs/design/") || strings.HasPrefix(rel, "docs/dev/contracts/") || strings.HasPrefix(rel, ".claude/submissions/")
}

// Preserve the original receiver's envelope and add preparation to its single
// model-facing response. stderr remains diagnostics, never the delivery path.
func writePlanningPostOutput(stdout io.Writer, original []byte, preparation string) {
	if preparation == "" {
		_, _ = stdout.Write(original)
		return
	}
	envelope := map[string]any{}
	if len(original) > 0 && json.Unmarshal(original, &envelope) != nil {
		_, _ = stdout.Write(original)
		return
	}
	specific, _ := envelope["hookSpecificOutput"].(map[string]any)
	if specific == nil {
		specific = map[string]any{"hookEventName": "PostToolUse"}
		envelope["hookSpecificOutput"] = specific
	}
	specific["additionalContext"] = strings.TrimSpace(stringValue(specific["additionalContext"]) + "\n" + preparation)
	_ = json.NewEncoder(stdout).Encode(envelope)
}

func planningUnobservedSaveGuidance(root string, request policy.Input, path string) string {
	change := planningChangeFrom(request)
	if !change.Active || change.Known || !mainQuestionnaireViewer(request) {
		return ""
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).ObservationSnapshot()
	if err != nil {
		return ""
	}
	row, err := registeredPlanningRow(snapshot.State)
	if err != nil || row == nil || !samePath(path, filepath.Join(root, stringValue(row["answer_path"]))) {
		return ""
	}
	return fmt.Sprintf("This tool event has no matching before/after save observation for %s; it did not submit the answer. A read requires no action. If this was an intended completed submission, Main/system repairs the missing PreToolUse/PostToolUse observation, or uses the original explicit receiver: %s runtime stage-answer consume --root %s --path %s. This is recovery for a missing event, not a routine extra submission step.", path, s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")), s7CaptureShellQuote(root), s7CaptureShellQuote(path))
}
