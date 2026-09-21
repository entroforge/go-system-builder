package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func runWorkspaceCommit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime workspace commit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "control or registered Worker root")
	id := fs.String("assignment", "", "registered assignment")
	agent := fs.String("agent", "", "registered owner")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ctx, release, err := integration.LockWorkspace(ctx, *root)
	if err != nil {
		return fail(err)
	}
	defer release()
	snap, err := runtime.NewStore(filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl")).Snapshot()
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
	e, ok := b.Execution(*id, workspace.RuntimeID(snap.State), workspace.Generation(snap.State))
	if !ok || e.AgentID != *agent || e.Status != "ready" || e.BootstrapSHA256 == "" {
		return fail(fmt.Errorf("commit requires the active bootstrapped Worker owner"))
	}
	path := filepath.Join(e.Path, ".claude/submissions/commit.json")
	actual, err := workspace.Canonical(path)
	if err != nil || actual != path {
		return fail(fmt.Errorf("commit request must be a regular local submission"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	var request workspace.CommitRequest
	if err = json.Unmarshal(data, &request); err != nil {
		return fail(err)
	}
	loaded, err := hookctx.LoadFull(*root, e.AgentID)
	if err != nil {
		return fail(err)
	}
	if loaded.PolicyContext.Agent == nil || loaded.PolicyContext.Agent.State != "working" {
		return fail(fmt.Errorf("commit requires working owner with recorded plan/activation"))
	}
	engine, err := policy.Load(filepath.Join(*root, "docs/control/hook-policy.json"))
	if err != nil {
		return fail(err)
	}
	for _, p := range request.Paths {
		decision, err := engine.Evaluate(policy.Input{Event: "PreToolUse", AgentID: e.AgentID, CWD: e.Path, ToolName: "Write", ToolInput: map[string]any{"file_path": filepath.Join(e.Path, p)}, Runtime: loaded.PolicyContext})
		if err != nil {
			return fail(err)
		}
		if decision.Decision == "deny" {
			return fail(fmt.Errorf("commit path %s rejected: %s", p, decision.Reason))
		}
	}
	receipt, err := b.Commit(ctx, e, request)
	if encodeErr := json.NewEncoder(stdout).Encode(receipt); encodeErr != nil {
		return fail(encodeErr)
	}
	if err != nil {
		return fail(err)
	}
	return 0
}
