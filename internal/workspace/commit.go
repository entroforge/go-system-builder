package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type CommitRequest struct {
	Message string   `json:"message"`
	Paths   []string `json:"paths"`
}
type CommitReceipt struct {
	Execution Execution      `json:"execution"`
	State     string         `json:"state"`
	Request   *CommitRequest `json:"request,omitempty"`
}

func WorkerInvocation(e Execution, verb string) string {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'" }
	return quote(filepath.Join(e.Path, ".claude/bin/loop-harness")) + " runtime workspace " + verb + " --root " + quote(e.Path) + " --assignment " + quote(e.AssignmentID) + " --agent " + quote(e.AgentID)
}

// Commit runs only explicit Git add/commit operations in the registered Worker.
// Repository hooks remain enabled. The caller serializes shared Git mutations;
// no cwd, checkout, stash, reset or automatic add-all is performed.
func (b *ExecutionRegistry) Commit(ctx context.Context, e Execution, request CommitRequest) (CommitReceipt, error) {
	if e.Purpose == DocumentReviewPurpose {
		return CommitReceipt{}, fmt.Errorf("document review executes no product commit; save the issued answer")
	}
	if err := b.Validate(ctx, b.MainRoot); err != nil {
		return CommitReceipt{}, err
	}
	if err := b.ValidateExecution(ctx, e, false); err != nil {
		return CommitReceipt{}, err
	}
	if err := b.ValidateInputs(e); err != nil {
		return CommitReceipt{}, err
	}
	if strings.TrimSpace(request.Message) == "" || len(request.Paths) == 0 {
		return CommitReceipt{}, fmt.Errorf("commit requires a message and explicit paths")
	}
	submissionPaths, err := b.WorkerSubmissionPaths(e)
	if err != nil {
		return CommitReceipt{}, err
	}
	allowed := map[string]bool{}
	for _, p := range request.Paths {
		for _, control := range append(submissionPaths.ControlPaths(), submissionPaths.Answer) {
			if control != "" && (p == control || strings.HasPrefix(control, strings.TrimSuffix(p, "/")+"/")) {
				return CommitReceipt{}, fmt.Errorf("commit requires literal product file paths: %s", p)
			}
		}
		if !filepath.IsLocal(p) || filepath.ToSlash(filepath.Clean(p)) != p || p == "." || (p == ".git" || strings.HasPrefix(p, ".git/")) || strings.HasPrefix(p, ".claude/") || p == ".loop-submissions" || strings.HasPrefix(p, ".loop-submissions/") {
			return CommitReceipt{}, fmt.Errorf("commit requires literal product file paths: %s", p)
		}
		physical, err := CanonicalProspective(filepath.Join(e.Path, filepath.FromSlash(p)))
		if err != nil {
			return CommitReceipt{}, err
		}
		rel, err := filepath.Rel(e.Path, physical)
		if err != nil || !filepath.IsLocal(rel) {
			return CommitReceipt{}, fmt.Errorf("commit path escapes Worker: %s", p)
		}
		matched := false
		for _, scope := range e.WritePaths {
			if PathMatchesScope(p, scope) && PathMatchesScope(filepath.ToSlash(rel), scope) {
				matched = true
			}
		}
		if !matched {
			return CommitReceipt{}, fmt.Errorf("commit path outside assignment: %s", p)
		}
		if info, err := os.Stat(physical); err == nil && info.IsDir() {
			return CommitReceipt{}, fmt.Errorf("enumerate commit files, not directories: %s", p)
		} else if err != nil && !os.IsNotExist(err) {
			return CommitReceipt{}, err
		}
		allowed[p] = true
	}
	request.Paths = nil
	for p := range allowed {
		request.Paths = append(request.Paths, p)
	}
	sort.Strings(request.Paths)
	dir := filepath.Join(b.MainRoot, ".claude/evidence", e.RuntimeID, fmt.Sprintf("g%d", e.BaselineGeneration), "worktree", e.AssignmentID, "commits")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return CommitReceipt{}, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return CommitReceipt{}, err
	}
	maxSequence := 0
	var prior *CommitReceipt
	priorSequence := 0
	for _, entry := range entries {
		var sequence int
		if _, scanErr := fmt.Sscanf(entry.Name(), "operation-%d.json", &sequence); scanErr != nil || sequence < 1 || entry.Name() != fmt.Sprintf("operation-%d.json", sequence) {
			continue // historical and unrelated files are never rewritten
		}
		if sequence > maxSequence {
			maxSequence = sequence
		}
		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		var old CommitReceipt
		if readErr != nil || json.Unmarshal(data, &old) != nil || old.Request == nil {
			continue
		}
		if sequence > priorSequence && old.Execution.RuntimeID == e.RuntimeID && old.Execution.AssignmentID == e.AssignmentID && old.Execution.Generation == e.Generation && old.Execution.AgentID == e.AgentID &&
			old.Request.Message == request.Message && equalCommitPaths(old.Request.Paths, request.Paths) {
			copy := old
			prior = &copy
			priorSequence = sequence
		}
	}
	if prior != nil && prior.State == "preparing" {
		return *prior, fmt.Errorf("previous Git commit outcome is unknown; preserve the worktree and inspect its actual Git status before continuing")
	}
	if prior != nil && prior.State == "committed_pending_review" {
		return *prior, fmt.Errorf("Git commit succeeded but its changed paths could not be confirmed; preserve the commit for review")
	}
	if prior != nil && prior.State == "committed_out_of_scope" {
		return *prior, fmt.Errorf("Git hook committed an undeclared file; preserve the original commit for review")
	}
	staged, err := gitRaw(ctx, e.Path, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return CommitReceipt{}, err
	}
	for _, path := range strings.Split(staged, "\x00") {
		if path != "" && !allowed[path] {
			return CommitReceipt{}, fmt.Errorf("unrelated staged file must be resolved explicitly: %s", path)
		}
	}
	args := append([]string{"status", "--porcelain=v1", "-z", "--"}, request.Paths...)
	status, err := gitRaw(ctx, e.Path, args...)
	if err != nil {
		return CommitReceipt{}, err
	}
	if status == "" && prior != nil && prior.State == "committed" {
		// This reports the previous original receipt only; it does not certify
		// any later change or infer a commit from Git object identity.
		return *prior, nil
	}
	sequence := maxSequence + 1
	path := filepath.Join(dir, fmt.Sprintf("operation-%d.json", sequence))
	cleanExecution := Execution{AssignmentID: e.AssignmentID, RuntimeID: e.RuntimeID, BaselineGeneration: e.BaselineGeneration,
		Generation: e.Generation, AgentID: e.AgentID, Path: e.Path, Branch: e.Branch, TargetBranch: e.TargetBranch, Status: e.Status}
	requestCopy := request
	receipt := CommitReceipt{Execution: cleanExecution, State: "preparing", Request: &requestCopy}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return receipt, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return receipt, err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return receipt, fmt.Errorf("preserve original commit intent: %w", err)
	}
	args = append([]string{"--literal-pathspecs", "add", "--"}, request.Paths...)
	if _, err = Git(ctx, e.Path, args...); err != nil {
		receipt.State = "failed"
		data, _ = json.MarshalIndent(receipt, "", "  ")
		_ = publishBootstrapFile(path, data, 0600)
		return receipt, err
	}
	if _, err = Git(ctx, e.Path, "commit", "-m", request.Message); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ProcessState != nil && exitErr.ProcessState.Exited() && exitErr.ExitCode() > 0 {
			receipt.State = "failed"
			data, _ = json.MarshalIndent(receipt, "", "  ")
			_ = publishBootstrapFile(path, data, 0600)
			return receipt, fmt.Errorf("commit failed; staging and hooks are preserved for retry: %w", err)
		}
		return receipt, fmt.Errorf("commit outcome is unknown after Git failure; preserve staging and inspect the original worktree: %w", err)
	}
	// Once Git returns success, mark that real fact before any further
	// observation: failures here must not cause the commit to run again.
	receipt.State = "committed_pending_review"
	data, err = json.MarshalIndent(receipt, "", "  ")
	if err == nil {
		err = publishBootstrapFile(path, data, 0600)
	}
	if err != nil {
		return receipt, fmt.Errorf("Git returned success but commit state was not saved; inspect the original worktree: %w", err)
	}
	changed, err := gitRaw(ctx, e.Path, "diff-tree", "--no-commit-id", "--name-only", "-r", "-z", "HEAD")
	if err != nil {
		return receipt, fmt.Errorf("commit completed but its actual paths could not be inspected: %w", err)
	}
	for _, changedPath := range strings.Split(changed, "\x00") {
		if changedPath != "" && !allowed[changedPath] {
			receipt.State = "committed_out_of_scope"
			data, _ = json.MarshalIndent(receipt, "", "  ")
			_ = publishBootstrapFile(path, data, 0600)
			return receipt, fmt.Errorf("Git hook committed an undeclared file %s; preserve the commit for review", changedPath)
		}
	}
	receipt.State = "committed"
	data, err = json.MarshalIndent(receipt, "", "  ")
	if err == nil {
		err = publishBootstrapFile(path, data, 0600)
	}
	if err != nil {
		return receipt, fmt.Errorf("commit completed but final state was not saved: %w", err)
	}
	return receipt, nil

}

func equalCommitPaths(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
