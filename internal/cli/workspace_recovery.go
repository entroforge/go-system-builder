package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func runWorkspaceRecovery(args []string, stdout, stderr io.Writer) int {
	verb := args[0]
	fs := flag.NewFlagSet("runtime workspace "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "bound Main root")
	id := fs.String("assignment", "", "registered assignment")
	owner := fs.String("agent", "", "registered owner")
	generation := fs.Int("execution-generation", 0, "exact old execution generation for replacement")
	request := fs.String("request", "", "reviewed historical Execution JSON for adoption")
	reason := fs.String("reason", "", "explicit recovery reason")
	if err := parseWorkspaceFlags(fs, args[1:]); err != nil {
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if *reason == "" {
		return fail(fmt.Errorf("recovery requires an explicit reason"))
	}
	if err := workspace.RequireMain(*root); err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, release, err := integration.LockWorkspace(ctx, *root)
	if err != nil {
		return fail(err)
	}
	defer release()
	writer := runtime.NewWriter(filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl"), *root, semantic.RuntimeCandidateValidator{})
	snap, err := writer.Snapshot()
	if err != nil {
		return fail(err)
	}
	b, err := workspace.Decode(snap.State)
	if err != nil {
		return fail(err)
	}
	if b == nil {
		return fail(fmt.Errorf("bind Main first"))
	}
	if err = b.Validate(ctx, *root); err != nil {
		return fail(err)
	}
	loaded, err := hookctx.LoadFull(*root, *owner)
	if err != nil {
		return fail(err)
	}
	var a *hookctx.AssignmentContext
	for i := range loaded.Assignments {
		if loaded.Assignments[i].AssignmentID == *id && loaded.Assignments[i].OwnerAgentID == *owner {
			a = &loaded.Assignments[i]
		}
	}
	if a == nil {
		return fail(fmt.Errorf("recovery owner/assignment is no longer registered"))
	}
	if a.CompletionRef != "" || a.CompletionAckRef != "" {
		return fail(fmt.Errorf("reported delivery requires integration/domain recovery, not Worker replacement"))
	}
	lifecycle, _ := snap.State["lifecycle"].(map[string]any)
	if lifecycle["state"] == "bug_resolution" {
		authority, err := repair.ResolveWorkspaceAuthority(*root, snap.State, *id, *owner)
		if err != nil {
			return fail(err)
		}
		a.WritePaths = authority.Assignment.Scope
		a.RequiredChecks = authority.Checks
	} else if lifecycle["state"] != "building" {
		return fail(fmt.Errorf("recovery requires an executing assignment"))
	}
	cpPath := integration.CheckpointPath(*root, workspace.RuntimeID(snap.State), workspace.Generation(snap.State), *id)
	if _, found, err := integration.DefaultCheckpointStore().Load(cpPath); err != nil {
		return fail(err)
	} else if found {
		return fail(fmt.Errorf("preserve existing integration checkpoint; recover it before changing execution identity"))
	}
	persist := func(message string) error {
		key := fmt.Sprintf("workspace-recovery-r%d", snap.Revision+1)
		next, err := writer.Update(snap.Revision, runtime.Mutation{EventID: key, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "main", RuntimeID: workspace.RuntimeID(snap.State), IdempotencyKey: key, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Message: message + ": " + *reason, Apply: func(state map[string]any) error { state["workspace"] = workspace.Encode(b); return nil }})
		if err == nil {
			snap = next
		}
		return err
	}
	var e workspace.Execution
	if verb == "replace" {
		old, ok := b.Execution(*id, workspace.RuntimeID(snap.State), workspace.Generation(snap.State))
		if !ok {
			return fail(fmt.Errorf("no active execution to replace"))
		}
		leaseCtx, done := context.WithTimeout(ctx, 100*time.Millisecond)
		lease, err := filelock.Acquire(leaseCtx, filepath.Join(*root, ".claude/workspace-launch", old.RuntimeID, fmt.Sprintf("g%d-%s-e%d.lock", old.BaselineGeneration, old.AssignmentID, old.Generation)))
		done()
		if err != nil {
			return fail(err)
		}
		defer lease()
		if old.Generation == *generation+1 && old.Status == "preparing" {
			e = old
		} else {
			e, err = b.ReplaceExecution(ctx, snap.State, *id, *owner, *generation)
			if err != nil {
				return fail(err)
			}
			if err = persist("retire old Worker and reserve replacement; preserve old tree and evidence"); err != nil {
				return fail(err)
			}
		}
		if err = b.Materialize(ctx, e); err != nil {
			return fail(err)
		}
	} else {
		data, err := os.ReadFile(*request)
		if err != nil {
			return fail(err)
		}
		if err = json.Unmarshal(data, &e); err != nil {
			return fail(err)
		}
		if e.AssignmentID != *id || e.AgentID != *owner || !reflect.DeepEqual(e.WritePaths, a.WritePaths) || !reflect.DeepEqual(e.Checks, a.RequiredChecks) {
			return fail(fmt.Errorf("historical request differs from registered assignment scope/checks"))
		}
		if existing, ok := b.Execution(*id, workspace.RuntimeID(snap.State), workspace.Generation(snap.State)); ok {
			if existing.Status != "preparing" || existing.Path != e.Path || existing.Branch != e.Branch || existing.BaseCommit != e.BaseCommit || existing.Generation != e.Generation || !reflect.DeepEqual(existing.Inputs, e.Inputs) {
				return fail(fmt.Errorf("adoption retry differs from persisted intent"))
			}
			e = existing
		} else {
			e, err = b.AdoptExecution(ctx, snap.State, e)
			if err != nil {
				return fail(err)
			}
			b.Executions[*id] = e
			if err = persist("reserve explicit historical adoption; historical PASS not promoted"); err != nil {
				return fail(err)
			}
		}
		if err = b.ValidateExecution(ctx, e, false); err != nil {
			return fail(err)
		}
		if err = b.PublishPointer(e); err != nil {
			return fail(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	if err = b.Bootstrap(e, executable); err != nil {
		return fail(err)
	}
	e.BootstrapSHA256, err = workspace.BootstrapDigest(e)
	if err != nil {
		return fail(err)
	}
	e.Status = "ready"
	b.Executions[*id] = e
	if err = persist("recovered Worker ready"); err != nil {
		return fail(err)
	}
	return encodeJSON(stdout, e)
}
