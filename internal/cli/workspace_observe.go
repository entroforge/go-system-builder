package cli

import (
	"context"
	"flag"
	"fmt"
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
	view := fs.String("view", "", "cwd, status, diff, staged or log")
	if err := parseWorkspaceFlags(fs, args); err != nil {
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
	if err = b.Observe(context.Background(), e, *view, stdout); err != nil {
		return fail(err)
	}
	return 0
}
