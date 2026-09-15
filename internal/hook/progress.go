package hook

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func compactGuidance(g policy.Guidance) string {
	s := fmt.Sprintf("LOOP RECOVERY — Stage %s %s. Next: %s. Read %s; if blocked, read %s.", g.Stage, g.LifecycleState, g.Action, g.ProtocolRef, g.ManualRef)
	if len(g.Questions) > 0 {
		s += " Preflight questions: " + strings.Join(g.Questions, " | ")
	}
	if len(g.Integration) > 0 {
		s += " Integration: " + strings.Join(g.Integration, "; ")
	}
	if g.Blocked {
		s += " Blocked: " + g.Blocker
	}
	if g.HumanRequired {
		s += " Human Gateway required; stop automation."
	}
	return s
}

// Summaries are display-only; complete conflicts and safety recovery remain in
// the model context and the existing audit journal. Do not claim work occurred.
func qualityNotice(d policy.Decision, q controller.QualityGateResult) string {
	stage := q.NextCursor
	if d.Guidance != nil {
		stage = d.Guidance.Stage + " " + d.Guidance.LifecycleState
	}
	s := fmt.Sprintf("Loop | %s | %s", stage, q.Status)
	switch {
	case q.Status == controller.StatusBlocked:
		s += " | " + d.RuleID + ": " + d.Reason
		if len(d.Recovery) > 0 {
			s += " | Recovery: " + strings.Join(d.Recovery, "; ")
		}
		return s // Safety reasons are never truncated or deduplicated.
	case d.Decision == "warn":
		s += " | " + d.RuleID + ": " + d.Reason
	case len(q.Conflicts) > 0:
		s += " | Conflicts: " + strings.Join(q.Conflicts, "; ")
	case len(q.Missing) > 0:
		s += " | Missing: " + strings.Join(q.Missing, "; ")
	}
	if d.Guidance != nil {
		s += " | Next: " + d.Guidance.Action
	}
	return compactNotice(s)
}

func compactNotice(s string) string {
	// Bound notices by Unicode characters, not bytes. Full context is separate.
	if i := strings.Index(s, " Read in order:"); i >= 0 {
		s = s[:i]
	}
	const limit = 480
	chars := []rune(s)
	if len(chars) > limit {
		return string(chars[:limit]) + "…"
	}
	return s
}

// PrepareNotice deduplicates only the display field of successful PreToolUse
// events. It never touches model context, permission decisions or the audit.
// Cache loss/corruption/concurrent writers merely produce an extra notice.
// Commit only after stdout succeeded; failed delivery must not poison recovery.
func PrepareNotice(root, session, agent, event string, output []byte) ([]byte, func()) {
	noop := func() {}
	if root == "" || session == "" {
		return output, noop
	}
	var p map[string]any
	if json.Unmarshal(output, &p) != nil {
		return output, noop
	}
	specific, _ := p["hookSpecificOutput"].(map[string]any)
	if specific["permissionDecision"] == "deny" {
		return output, noop
	}
	identity := sha256.Sum256([]byte(session + "\x00" + agent))
	path := filepath.Join(root, ".claude", "hook-notices", fmt.Sprintf("%x", identity))
	if event == "SessionStart" || event == "SubagentStart" {
		return output, func() { _ = os.Remove(path) }
	}
	if event != "PreToolUse" {
		return output, noop
	}
	message, ok := p["systemMessage"].(string)
	if !ok {
		return output, noop
	}
	// Include the complete gate state so a change beyond the display limit is
	// never hidden. Revision churn alone is not progress.
	state := map[string]any{"message": message}
	if context, ok := specific["additionalContext"].(string); ok {
		state["context"] = noticeRevisionFields.ReplaceAllString(context, "")
	}
	if gate, ok := specific["quality_gate"].(map[string]any); ok {
		copy := map[string]any{}
		for k, v := range gate {
			if k != "observed_revision" && k != "fingerprint" {
				copy[k] = v
			}
		}
		state["gate"] = copy
	}
	data, _ := json.Marshal(state)
	sum := sha256.Sum256(data)
	stamp := fmt.Sprintf("%x", sum)
	if old, err := os.ReadFile(path); err == nil && string(old) == stamp {
		delete(p, "systemMessage")
		if encoded, err := json.Marshal(p); err == nil {
			return encoded, noop
		}
	}
	return output, func() {
		if os.MkdirAll(filepath.Dir(path), 0700) != nil {
			return
		}
		f, err := os.CreateTemp(filepath.Dir(path), ".notice-*")
		if err != nil {
			return
		}
		defer os.Remove(f.Name())
		_, err = f.WriteString(stamp)
		closeErr := f.Close()
		if err == nil && closeErr == nil {
			_ = os.Rename(f.Name(), path)
		}
	}
}

// These transport fields change on observation alone; the rest of the full
// context participates so even a conflict beyond the UI preview reappears.
var noticeRevisionFields = regexp.MustCompile(` Observed revision: [0-9]+\.| Fingerprint: [^ ]+\.`)
