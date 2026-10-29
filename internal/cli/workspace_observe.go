package cli

import (
	"context"
	"flag"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"io"
)

func runWorkspaceObserve(args []string, stdout, stderr io.Writer) int {
	if err := workspace.ValidateObservationEnvironment(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fs := flag.NewFlagSet("runtime workspace observe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "Main or registered Worker root")
	id := fs.String("assignment", "", "registered assignment")
	agent := fs.String("agent", "", "registered owner")
	view := fs.String("view", "", "cwd, status, diff, staged, log, check-capabilities, check-reuse or check-running")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if fs.NArg() != 0 {
		return fail(fmt.Errorf("observation accepts no shell arguments"))
	}
	b, state, err := workspace.Load(*root)
	if err != nil {
		return fail(err)
	}
	if b == nil {
		return fail(fmt.Errorf("workspace not bound"))
	}
	e, ok := b.Execution(*id, workspace.RuntimeID(state), workspace.Generation(state))
	if !ok || e.AgentID != *agent {
		return fail(fmt.Errorf("observation owner/assignment mismatch"))
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	ctx := checkexec.WithReuseGuard(context.Background(), checkexec.ReuseGuard{Fresh: lifecycle["state"] == "bug_resolution"})
	if err = b.Observe(ctx, e, *view, stdout); err != nil {
		return fail(err)
	}
	return 0
}
