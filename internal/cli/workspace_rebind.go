package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type relocationIntent struct {
	Request workspace.RebindRequest      `json:"request"`
	Before  *workspace.ExecutionRegistry `json:"before"`
	After   *workspace.ExecutionRegistry `json:"after"`
}

func runWorkspaceRebind(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime workspace rebind", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "new Main root")
	request := fs.String("request", "", "explicit relocation JSON")
	reason := fs.String("reason", "", "relocation reason")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if *reason == "" {
		return fail(fmt.Errorf("explicit relocation reason is required"))
	}
	if err := workspace.RequireMain(*root); err != nil {
		return fail(err)
	}
	data, err := os.ReadFile(*request)
	if err != nil {
		return fail(err)
	}
	var req workspace.RebindRequest
	if err = json.Unmarshal(data, &req); err != nil {
		return fail(err)
	}
	actual, err := workspace.Canonical(*root)
	if err != nil || actual != req.NewMainRoot {
		return fail(fmt.Errorf("--root must match requested new Main"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, release, err := integration.LockWorkspace(ctx, actual)
	if err != nil {
		return fail(err)
	}
	defer release()
	writer := runtime.NewWriter(filepath.Join(actual, ".claude/loop-state.json"), filepath.Join(actual, ".claude/loop-events.jsonl"), actual, semantic.RuntimeCandidateValidator{})
	snap, err := writer.Snapshot()
	if err != nil {
		return fail(err)
	}
	b, err := workspace.Decode(snap.State)
	if err != nil {
		return fail(err)
	}
	if b == nil {
		return fail(fmt.Errorf("no old binding to relocate"))
	}
	for _, e := range b.Executions {
		leaseCtx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
		lease, err := filelock.Acquire(leaseCtx, filepath.Join(actual, ".claude/workspace-launch", e.RuntimeID, fmt.Sprintf("g%d-%s-e%d.lock", e.BaselineGeneration, e.AssignmentID, e.Generation)))
		stop()
		if err != nil {
			return fail(err)
		}
		defer lease()
	}
	canonical, _ := json.Marshal(req)
	sum := sha256.Sum256(canonical)
	receiptPath := filepath.Join(actual, ".claude/evidence/workspace-relocations", hex.EncodeToString(sum[:])+".json")
	var intent relocationIntent
	if old, err := os.ReadFile(receiptPath); err == nil {
		if err = json.Unmarshal(old, &intent); err != nil {
			return fail(err)
		}
		if !reflect.DeepEqual(intent.Request, req) || intent.Before == nil || intent.After == nil {
			return fail(fmt.Errorf("relocation intent differs from request"))
		}
	} else if !os.IsNotExist(err) {
		return fail(err)
	} else {
		next, err := b.PlanRebind(ctx, snap.State, req)
		if err != nil {
			return fail(err)
		}
		for _, e := range next.Executions {
			if e.DeliveryRef != "" {
				return fail(fmt.Errorf("finish delivery before relocation; immutable candidate retained"))
			}
			if _, found, err := integration.DefaultCheckpointStore().Load(integration.CheckpointPath(actual, e.RuntimeID, e.BaselineGeneration, e.AssignmentID)); err != nil {
				return fail(err)
			} else if found {
				return fail(fmt.Errorf("finish integration/cleanup before relocation; checkpoint coordinates remain immutable"))
			}
		}
		intent = relocationIntent{req, b, next}
		if err = os.MkdirAll(filepath.Dir(receiptPath), 0700); err != nil {
			return fail(err)
		}
		payload, _ := json.MarshalIndent(intent, "", "  ")
		// Atomically publish an immutable intent before changing authority.
		f, err := os.CreateTemp(filepath.Dir(receiptPath), ".relocation-*")
		if err != nil {
			return fail(err)
		}
		defer os.Remove(f.Name())
		_, writeErr := f.Write(payload)
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if writeErr != nil {
			return fail(writeErr)
		}
		if closeErr != nil {
			return fail(closeErr)
		}
		if err = os.Link(f.Name(), receiptPath); err != nil {
			return fail(err)
		}

	}
	if reflect.DeepEqual(b, intent.Before) {
		// Revalidate on retries before changing authority, including exact HEAD.
		if _, err = b.PlanRebind(ctx, snap.State, req); err != nil {
			return fail(err)
		}
		key := fmt.Sprintf("workspace-relocation-r%d", snap.Revision+1)
		snap, err = writer.Update(snap.Revision, runtime.Mutation{EventID: key, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "main", RuntimeID: req.RuntimeID, IdempotencyKey: key, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Message: "explicit Main relocation: " + *reason, Apply: func(state map[string]any) error {
			bound, _ := state["bound_req"].(map[string]any)
			authority, _ := bound["workspace"].(map[string]any)
			if authority["project_root"] != intent.Before.MainRoot || authority["dev_branch"] != intent.Before.Branch {
				return fmt.Errorf("REQ workspace authority changed during relocation")
			}
			authority["project_root"] = intent.After.MainRoot
			state["workspace"] = workspace.Encode(intent.After)
			state["root"] = actual
			return nil
		}})
		if err != nil {
			return fail(err)
		}
		b = intent.After
	} else if !reflect.DeepEqual(b, intent.After) {
		return fail(fmt.Errorf("binding changed since relocation intent; preserve receipt for review"))
	}
	if err = b.Validate(ctx, actual); err != nil {
		return fail(err)
	}
	for _, e := range b.Executions {
		if e.Status == "ready" || e.Status == "preparing" {
			if err = b.RelocatePointer(e, req.OldMainRoot); err != nil {
				return fail(err)
			}
		}
	}
	return encodeJSON(stdout, map[string]any{"binding": b, "relocation_receipt": receiptPath, "revision": snap.Revision})
}
