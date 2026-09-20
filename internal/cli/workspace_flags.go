package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Parse using Go's real flag grammar before interpreting any coordinates.
// Root routing does not change cwd, argv, environment or another invocation.
func parseWorkspaceFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.Lookup("root") == nil || fs.Name() == "hook" || fs.Name() == "dry-run" {
		return nil
	}
	err := routeWorkspaceFlags(fs)
	if err != nil {
		fmt.Fprintln(fs.Output(), err)
	}
	return err
}

func routeWorkspaceFlags(fs *flag.FlagSet) error {
	requested := fs.Lookup("root").Value.String()
	worker, marked, err := workspace.PointerRoot(requested)
	if err != nil {
		return err
	}
	// Explicit --root Main must not grant Main authority to a Worker process.
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cwdWorker, cwdMarked, err := workspace.PointerRoot(cwd)
	if err != nil {
		return err
	}
	if !marked && !cwdMarked {
		return nil
	}
	if cwdMarked {
		if marked && worker != cwdWorker {
			return fmt.Errorf("CLI root selects another Worker checkout")
		}
		worker = cwdWorker
	}
	if fs.Name() == "init" || fs.Name() == "runtime workspace bind" || fs.Name() == "runtime workspace prepare" {
		return workspace.RequireMain(worker)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	main, err := workspace.ResolveControl(ctx, worker)
	if err != nil {
		return err
	}
	if !marked {
		root, err := workspace.Canonical(requested)
		if err != nil || root != main {
			return fmt.Errorf("Worker --root must identify its own checkout or bound Main")
		}
	}
	var artifacts []string
	switch fs.Name() {
	case "projection", "ready", "validate", "doctor", "health", "explain", "req list", "runtime repair status", "runtime workspace status", "runtime workspace check", "runtime workspace pending", "runtime workspace commit", "runtime workspace deliver", "runtime workspace begin", "runtime workspace report":
	case "runtime agent-begin":
		artifacts = []string{"plan"}
	case "runtime task-complete":
		artifacts = []string{"message"}
	case "runtime agent-event":
		switch fs.Lookup("event").Value.String() {
		case "readback_started", "readback_submitted", "document_conflict_reported", "work_started", "completion_reported", "work_blocked":
		default:
			return fmt.Errorf("Worker cannot issue Main Agent lifecycle approvals")
		}
		artifacts = []string{"message"}
	default:
		return fmt.Errorf("%s requires the bound Main session; Worker root routing does not grant scheduling or integration authority", fs.Name())
	}
	var execution workspace.Execution
	if len(artifacts) > 0 || fs.Name() == "runtime workspace check" || (fs.Name() == "runtime workspace commit" || fs.Name() == "runtime workspace deliver" || fs.Name() == "runtime workspace begin" || fs.Name() == "runtime workspace report") {
		binding, state, err := workspace.Load(main)
		if err != nil {
			return err
		}
		if binding == nil {
			return fmt.Errorf("Worker binding disappeared during command routing")
		}
		matched := false
		for _, e := range binding.Executions {
			ownerFlag := "agent-id"
			if fs.Name() == "runtime workspace check" || (fs.Name() == "runtime workspace commit" || fs.Name() == "runtime workspace deliver" || fs.Name() == "runtime workspace begin" || fs.Name() == "runtime workspace report") {
				ownerFlag = "agent"
				if fs.Lookup("assignment").Value.String() != e.AssignmentID {
					continue
				}
			}
			if e.Path == worker && e.Status == "ready" && e.RuntimeID == workspace.RuntimeID(state) && e.BaselineGeneration == workspace.Generation(state) && fs.Lookup(ownerFlag).Value.String() == e.AgentID {
				matched = true
				execution = e
			}
		}
		if !matched {
			return fmt.Errorf("Worker command agent-id differs from its registered owner")
		}
	}
	for _, name := range []string{"state", "journal"} {
		f := fs.Lookup(name)
		if f == nil {
			continue
		}
		canonical := filepath.Join(main, ".claude/loop-state.json")
		if name == "journal" {
			canonical = filepath.Join(main, ".claude/loop-events.jsonl")
		}
		value := f.Value.String()
		if !filepath.IsAbs(value) {
			value = filepath.Join(main, value)
		}
		resolved, err := workspace.CanonicalProspective(value)
		if err != nil || resolved != canonical {
			return fmt.Errorf("Worker cannot override the authoritative %s path", name)
		}
		if err := fs.Set(name, canonical); err != nil {
			return err
		}
	}
	for _, name := range artifacts {
		f := fs.Lookup(name)
		if f == nil || f.Value.String() == "" {
			continue
		}
		value := f.Value.String()
		// Artifact paths retain the caller's explicitly selected input root.
		if !filepath.IsAbs(value) {
			value = filepath.Join(requested, value)
		}
		value, err = filepath.Abs(value)
		if err != nil {
			return err
		}
		value, err = freezeWorkerSubmission(main, worker, value, execution)
		if err != nil {
			return err
		}
		if err = fs.Set(name, value); err != nil {
			return err
		}
	}
	return fs.Set("root", main)
}
