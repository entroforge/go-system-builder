package workspace

import (
	"path/filepath"
	"strconv"
	"strings"
)

// NativeOwnedCheckInvocation renders the one native S6 WorkerCheck adapter
// command from trusted control-root and owner identity. The selected index is
// the frozen Record's WorkerCheck index; no command or profile is accepted
// from the caller.
func NativeOwnedCheckInvocation(controlRoot, owner string, index int) string {
	if controlRoot == "" || !filepath.IsAbs(controlRoot) || owner == "" || index < 0 {
		return ""
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", `"'"'`) + "'" }
	return quote(filepath.Join(controlRoot, ".claude", "bin", "loop-harness")) +
		" runtime assignment-context check --root " + quote(controlRoot) +
		" --agent-id " + quote(owner) + " --check-index " + strconv.Itoa(index)
}
