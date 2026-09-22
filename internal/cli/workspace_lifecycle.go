package cli

import (
	"flag"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"io"
	"path/filepath"
)

// Fixed local filenames keep shell admission exact. The normal lifecycle CLI
// performs owner checks, durable report copying and the original state guards.
func runWorkspaceLifecycle(args []string, stdout, stderr io.Writer) int {
	verb := args[0]
	fs := flag.NewFlagSet("runtime workspace "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "registered Worker or Main root")
	id := fs.String("assignment", "", "registered assignment")
	agent := fs.String("agent", "", "registered owner")
	if err := parseWorkspaceFlags(fs, args[1:]); err != nil {
		return 2
	}
	b, state, err := workspace.Load(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if b == nil {
		fmt.Fprintln(stderr, "bind Main first")
		return 1
	}
	e, ok := b.Execution(*id, workspace.RuntimeID(state), workspace.Generation(state))
	if !ok || e.AgentID != *agent || e.Status != "ready" || e.BootstrapSHA256 == "" {
		fmt.Fprintln(stderr, "lifecycle submission requires current bootstrapped owner")
		return 1
	}
	command, flagName, name := "agent-begin", "--plan", "plan.json"
	if verb == "report" {
		command, flagName, name = "task-complete", "--message", "completion.json"
	}
	return runRuntime([]string{command, "--root", e.Path, "--agent-id", e.AgentID, flagName, filepath.Join(e.Path, ".claude/submissions", name)}, stdout, stderr)
}
