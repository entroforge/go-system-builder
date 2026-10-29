package hook

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// PreparePlanningContext retains the S2 entry for existing callers.
func PreparePlanningContext(root, session, agent, event, responsibility string, output []byte) ([]byte, func()) {
	return PrepareResponsibilityContext(root, session, agent, event, "S2", responsibility, output)
}

// PrepareResponsibilityContext reduces repeated model guidance only when the
// caller supplies a known current responsibility. It never owns workflow state.
// The last successfully delivered content is scoped to session and agent; new
// paths, diagnostics or responsibility facts always restore the full context.
// A missing session, cache failure or malformed output keeps the full delivery.
func PrepareResponsibilityContext(root, session, agent, event, stage, responsibility string, output []byte) ([]byte, func()) {
	noop := func() {}
	if root == "" || session == "" {
		return output, noop
	}
	identity := sha256.Sum256([]byte(session + "\x00" + agent))
	path := filepath.Join(root, ".claude", "hook-context", fmt.Sprintf("%x", identity))
	if event == "SessionStart" || event == "SubagentStart" || event == "PreCompact" {
		return output, func() { _ = os.Remove(path) }
	}
	if event != "PreToolUse" || responsibility == "" {
		return output, noop
	}
	var envelope map[string]any
	if json.Unmarshal(output, &envelope) != nil {
		return output, noop
	}
	specific, _ := envelope["hookSpecificOutput"].(map[string]any)
	context, ok := specific["additionalContext"].(string)
	if !ok || context == "" || specific["permissionDecision"] != nil {
		return output, noop
	}
	sum := sha256.Sum256([]byte(stage + "\x00" + responsibility + "\x00" + responsibilityContextStamp(stage, context)))
	stamp := fmt.Sprintf("%x", sum)
	if old, err := os.ReadFile(path); err == nil && string(old) == stamp {
		specific["additionalContext"] = stage + " guidance is unchanged; continue the current responsibility using the previously delivered paths and instructions."
		if encoded, err := json.Marshal(envelope); err == nil {
			return encoded, noop
		}
	}
	return output, func() {
		if os.MkdirAll(filepath.Dir(path), 0700) != nil {
			return
		}
		file, err := os.CreateTemp(filepath.Dir(path), ".context-*")
		if err != nil {
			return
		}
		defer os.Remove(file.Name())
		_, err = file.WriteString(stamp)
		closeErr := file.Close()
		if err == nil && closeErr == nil {
			_ = os.Rename(file.Name(), path)
		}
	}
}
