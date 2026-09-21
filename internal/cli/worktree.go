package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Worktree creation consumes the binding, not HEAD, the remote default or a
// copied working directory. Existing assignments are reused without reset.
func runWorktreeCreate(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("runtime worktree-create", flag.ContinueOnError)
	f.SetOutput(stderr)
	root := f.String("root", ".", "authority root")
	id := f.String("assignment-id", "", "registered assignment")
	branch := f.String("branch", "", "new temporary branch (default wt/<assignment>)")
	if e := f.Parse(args); e != nil {
		return 2
	}
	if *id == "" || filepath.Base(*id) != *id || strings.ContainsAny(*id, "/\\\x00") || (*id == ".." || *id == ".") {
		fmt.Fprintln(stderr, "valid --assignment-id is required")
		return 2
	}
	abs, e := filepath.Abs(*root)
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	snap, e := runtime.NewStore(filepath.Join(abs, ".claude/loop-state.json"), filepath.Join(abs, ".claude/loop-events.jsonl")).Snapshot()
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	ref, e := fileview.DevelopmentRef(snap.State)
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	if err := fileview.ValidateAuthority(abs, snap.State); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	loaded, e := hookctx.LoadFull(abs, "")
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	known := false
	var assigned hookctx.AssignmentContext
	for _, a := range loaded.Assignments {
		if a.AssignmentID == *id {
			known = true
			assigned = a
			break
		}
	}
	if !known {
		fmt.Fprintln(stderr, "assignment is not registered; dispatch it first")
		return 1
	}
	if *branch == "" {
		*branch = "wt/" + *id
	}
	target := filepath.Join(abs, ".worktrees", *id)
	record := filepath.Join(abs, ".claude/assignments", *id+".json")
	if e := os.MkdirAll(filepath.Dir(record), 0755); e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	lock, e := os.OpenFile(record+".create.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		fmt.Fprintln(stderr, "worktree creation in progress; inspect the assignment before retry:", e)
		return 1
	}
	lock.Close()
	defer os.Remove(record + ".create.lock")
	prior := map[string]any{}
	if data, e := os.ReadFile(record); e == nil {
		if json.Unmarshal(data, &prior) != nil {
			fmt.Fprintln(stderr, "invalid assignment coordinates; preserve and reconcile")
			return 1
		}
		if path, _ := prior["worktree_path"].(string); path != "" {
			if err := workspace.ValidateCheckout(abs, path); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			branchNow, err := exec.Command("git", "-C", path, "branch", "--show-current").Output()
			if err != nil || strings.TrimSpace(string(branchNow)) != prior["branch"] || prior["target_branch"] != strings.TrimPrefix(ref, "refs/heads/") {
				fmt.Fprintln(stderr, "existing worktree coordinates differ or worktree is missing; preserve and reconcile before reuse")
				return 1
			}
			fmt.Fprintln(stdout, string(data))
			return 0
		}
	}
	if assigned.WorktreePath != "" {
		if err := workspace.ValidateCheckout(abs, assigned.WorktreePath); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		actual, err := exec.Command("git", "-C", assigned.WorktreePath, "branch", "--show-current").Output()
		if err != nil || strings.TrimSpace(string(actual)) != assigned.Branch || assigned.TargetBranch != strings.TrimPrefix(ref, "refs/heads/") || (*branch != "wt/"+*id && *branch != assigned.Branch) {
			fmt.Fprintln(stderr, "registered checkout differs or is missing; preserve and reconcile instead of creating a duplicate")
			return 1
		}
		return encodeJSON(stdout, map[string]any{"assignment_id": *id, "worktree_path": assigned.WorktreePath, "branch": assigned.Branch, "target_branch": assigned.TargetBranch, "reused": true})
	}

	commit, e := fileview.Resolve(abs, "refs/heads/"+strings.TrimPrefix(ref, "refs/heads/"))
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	cmd := exec.Command("git", "-C", abs, "worktree", "add", "-b", *branch, target, commit)
	if out, e := cmd.CombinedOutput(); e != nil {
		fmt.Fprintf(stderr, "create worktree: %v\n%s", e, out)
		return 1
	}
	coordinates := map[string]any{"assignment_id": *id, "worktree_path": target, "branch": *branch, "target_branch": strings.TrimPrefix(ref, "refs/heads/"), "base_commit": commit}
	for key, value := range coordinates {
		prior[key] = value
	}
	data, _ := json.MarshalIndent(prior, "", "  ")
	if e := os.WriteFile(record+".tmp", data, 0600); e != nil {
		fmt.Fprintln(stderr, "worktree created; preserve it and record coordinates:", target, e)
		return 1
	}
	if e := os.Rename(record+".tmp", record); e != nil {
		fmt.Fprintln(stderr, "worktree created; coordinates pending:", target, e)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

// reminderDelivery computes a per-session, fact-deduplicated soft message.
// The returned callback records delivery only after stdout succeeded.
func reminderDelivery(root string, input policy.Input) (string, func()) {
	noop := func() {}
	if input.EffectiveAgentID() != "" {
		return "", noop
	}
	snap, e := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if e != nil {
		return "", noop
	}
	bound, _ := snap.State["bound_req"].(map[string]any)
	if bound == nil {
		return "", noop
	}
	var facts []string
	dev, e := fileview.DevelopmentRef(snap.State)
	if e != nil {
		facts = append(facts, e.Error())
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if out, e := exec.CommandContext(ctx, "git", "-C", root, "branch", "--show-current").Output(); e == nil && strings.TrimSpace(string(out)) != strings.TrimPrefix(dev, "refs/heads/") {
			facts = append(facts, "authority checkout differs from REQ development branch "+dev+"; preserve local changes and restore the declared branch explicitly")
		}
	}
	knownTrees := map[string]bool{}
	if loaded, e := hookctx.LoadFull(root, ""); e == nil {
		for _, a := range loaded.Assignments {
			knownTrees[filepath.Clean(a.WorktreePath)] = true
			if a.WorktreePath == "" {
				continue
			}
			if _, e := os.Stat(a.WorktreePath); e != nil {
				continue
			}
			facts = append(facts, fmt.Sprintf("assignment %s retains %s (state=%s, report=%s, completion=%s); commit worker results, then run runtime task-integrate --assignment-id %s at the authority root; integration includes merge, verification, acknowledgement and cleanup", a.AssignmentID, a.WorktreePath, a.State, a.ReportStatus, a.CompletionRef, a.AssignmentID))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "git", "-C", root, "worktree", "list", "--porcelain", "-z").Output(); err == nil {
		path := ""
		for _, field := range strings.Split(string(out), "\x00") {
			if strings.HasPrefix(field, "worktree ") {
				path = strings.TrimPrefix(field, "worktree ")
			}
			if strings.HasPrefix(field, "HEAD ") && path != "" && filepath.Clean(path) != filepath.Clean(root) {
				if knownTrees[filepath.Clean(path)] {
					facts = append(facts, "temporary checkout "+path+" "+field)
				} else {
					facts = append(facts, "unassociated checkout "+path+" "+field+"; inspect ownership and pending results before dispatching more work; preserve until explicitly received")
				}
			}
		}
	}
	sort.Strings(facts)

	if len(facts) == 0 {
		key := fmt.Sprintf("%x", sha256.Sum256([]byte(input.SessionID)))
		_ = os.Remove(filepath.Join(root, ".claude/worktree-reminders", key+".txt"))
		return "", noop
	}
	message := fmt.Sprintf("WORKTREE REMINDER (advisory) REQ=%v development=%s\n", bound["id"], dev) + strings.Join(facts, "\n")
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(input.SessionID)))
	file := filepath.Join(root, ".claude/worktree-reminders", key+".txt")
	if data, e := os.ReadFile(file); e == nil && string(data) == message {
		return "", noop
	}
	return message, func() {
		if os.MkdirAll(filepath.Dir(file), 0700) == nil {
			if f, err := os.CreateTemp(filepath.Dir(file), ".delivery-"); err == nil {
				name := f.Name()
				defer os.Remove(name)
				_, err = f.WriteString(message)
				closeErr := f.Close()
				if err == nil && closeErr == nil {
					_ = os.Rename(name, file)
				}
			}
		}
	}
}
func runWorktreePostTool(root string, input policy.Input, stdout, stderr io.Writer) int {
	// Reports are observed in the worker context. Never mistake launch/closing
	// text for a completed assignment or mutate integration state here.
	if input.ToolName == "SubagentHandback" {
		raw, _ := input.ToolInput["message"].(string)
		var message map[string]any
		if json.Unmarshal([]byte(raw), &message) != nil {
			return 0
		}
		input.ToolName = "SendMessage"
		input.ToolInput = message
		return runPostToolUseHook(root, input, stdout, stderr)
	}
	if status, _ := input.ToolResponse["status"].(string); status == "async_launched" {
		return 0
	}
	message, delivered := reminderDelivery(root, input)
	if dispatchContext := postToolDispatchContext(root, input); dispatchContext != "" {
		if message != "" {
			message += "\n\n"
		}
		message += dispatchContext
	}
	if message == "" {
		return 0
	}
	data, _, e := hook.RenderWithAdditionalContext(root, "PostToolUse", policy.Decision{Decision: "allow"}, input.Runtime, message)
	if e == nil {
		_, e = stdout.Write(append(data, '\n'))
	}
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	delivered()
	return 0
}

// Explicitly fill or change a legacy binding; Hook never guesses these values.
func runWorkspaceBind(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("req workspace", flag.ContinueOnError)
	f.SetOutput(stderr)
	root := f.String("root", ".", "authority root")
	dev := f.String("dev-branch", "", "development branch")
	up := f.String("release-upstream", "", "final release destination")
	if e := f.Parse(args); e != nil {
		return 2
	}
	binding, e := workspace.Bind(*root, *dev, *up)
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 2
	}
	writer := runtime.NewWriter(filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl"), *root, semantic.RuntimeCandidateValidator{})
	snap, e := writer.Snapshot()
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	next, e := writer.UpdateCurrent(runtime.Mutation{TransitionID: "REQ-WORKSPACE-BIND", Event: "req_workspace_bound", Actor: "user", OccurredAt: time.Now().UTC(), From: snap.State["lifecycle"].(map[string]any), To: snap.State["lifecycle"].(map[string]any), Apply: func(state map[string]any) error {
		bound, _ := state["bound_req"].(map[string]any)
		if bound == nil {
			return fmt.Errorf("bind a REQ first")
		}
		view, e := fileview.New(*root, "refs/heads/"+binding.DevBranch, []fileview.Rule{{Path: ".", Source: "git_tree"}})
		if e != nil {
			return e
		}
		path, _ := bound["path"].(string)
		data, e := view.ReadFile(path)
		if e != nil {
			return e
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != bound["sha256"] {
			return fmt.Errorf("declared branch does not contain the bound REQ bytes; use the amendment path for a changed baseline")
		}
		binding.BoundCommit = view.Commit
		if e := view.Verify(); e != nil {
			return e
		}
		bound["workspace"] = binding.Map()
		return nil
	}})
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	return encodeJSON(stdout, next.State["bound_req"])
}
