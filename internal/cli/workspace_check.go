package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func runWorkspaceCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime workspace check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "control or registered Worker root")
	id := fs.String("assignment", "", "registered assignment")
	agent := fs.String("agent", "", "registered owner")
	index := fs.Int("check", -1, "zero-based declared executable check index")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	snapshot, err := runtime.NewStore(filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return fail(err)
	}
	b, err := workspace.Decode(snapshot.State)
	if err != nil {
		return fail(err)
	}
	if b == nil {
		return fail(fmt.Errorf("bind Main and prepare the Worker first"))
	}
	e, ok := b.Execution(*id, workspace.RuntimeID(snapshot.State), workspace.Generation(snapshot.State))
	if !ok || e.AgentID != *agent || e.Status != "ready" || e.BootstrapSHA256 == "" {
		return fail(fmt.Errorf("check requires the current bootstrapped assignment and owner"))
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	switch lifecycle["state"] {
	case "building":
	case "bug_resolution":
		if _, err := repair.ResolveWorkspaceAuthority(*root, snapshot.State, e.AssignmentID, e.AgentID); err != nil {
			return fail(err)
		}
	default:
		return fail(fmt.Errorf("Worker check requires building or authorized S9 execution"))
	}
	entities, _ := snapshot.State["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	authorized := false
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row["id"] == e.AgentID && (row["state"] == "working" || row["state"] == "reported") {
			authorized = true
		}
	}
	if !authorized {
		return fail(fmt.Errorf("Worker check requires its owner's active execution or reported delivery"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	receipt, err := b.RunCheck(ctx, e, *index, stdout)
	if receipt != "" {
		fmt.Fprintln(stderr, "Worker check receipt:", receipt)
	}
	if err != nil {
		return fail(err)
	}
	return 0
}
