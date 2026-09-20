package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CommitRequest struct {
	ExpectedHead string   `json:"expected_head"`
	Message      string   `json:"message"`
	Paths        []string `json:"paths"`
}
type CommitReceipt struct {
	RequestSHA256 string    `json:"request_sha256"`
	Execution     Execution `json:"execution"`
	Parent        string    `json:"parent"`
	Commit        string    `json:"commit,omitempty"`
	State         string    `json:"state"`
}

func WorkerInvocation(e Execution, verb string) string {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'" }
	return quote(filepath.Join(e.Path, ".claude/bin/loop-harness")) + " runtime workspace " + verb + " --root " + quote(e.Path) + " --assignment " + quote(e.AssignmentID) + " --agent " + quote(e.AgentID)
}

// Commit runs only explicit Git add/commit operations in the registered Worker.
// Repository hooks remain enabled. The caller serializes shared Git mutations;
// no cwd, checkout, stash, reset or automatic add-all is performed.
func (b *Binding) Commit(ctx context.Context, e Execution, request CommitRequest) (CommitReceipt, error) {
	if err := b.Validate(ctx, b.MainRoot); err != nil {
		return CommitReceipt{}, err
	}
	if err := b.ValidateExecution(ctx, e, false); err != nil {
		return CommitReceipt{}, err
	}
	if err := b.ValidateInputs(e); err != nil {
		return CommitReceipt{}, err
	}
	if request.ExpectedHead == "" || strings.TrimSpace(request.Message) == "" || len(request.Paths) == 0 {
		return CommitReceipt{}, fmt.Errorf("commit requires expected_head, message and explicit paths")
	}
	allowed := map[string]bool{}
	for _, p := range request.Paths {
		if !filepath.IsLocal(p) || filepath.ToSlash(filepath.Clean(p)) != p || p == "." || (p == ".git" || strings.HasPrefix(p, ".git/")) || strings.HasPrefix(p, ".claude/") {
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
	data, _ := json.Marshal(request)
	hash := sha256.Sum256(data)
	key := hex.EncodeToString(hash[:])
	dir := filepath.Join(b.MainRoot, ".claude/evidence", e.RuntimeID, fmt.Sprintf("g%d", e.BaselineGeneration), "worktree", e.AssignmentID, "commits")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return CommitReceipt{}, err
	}
	receipt := CommitReceipt{RequestSHA256: key, Execution: e, Parent: request.ExpectedHead, State: "preparing"}
	persist := func() error {
		data, _ := json.MarshalIndent(receipt, "", "  ")
		return publishBootstrapFile(filepath.Join(dir, key+".json"), data, 0600)
	}
	head, err := Git(ctx, e.Path, "rev-parse", "HEAD")
	if err != nil {
		return receipt, err
	}
	message := request.Message + "\n\nLoop-Commit-Request: " + key
	if head != request.ExpectedHead {
		parent, _ := Git(ctx, e.Path, "rev-parse", "HEAD^")
		body, _ := Git(ctx, e.Path, "log", "-1", "--format=%B")
		if parent != request.ExpectedHead || body != message {
			return receipt, fmt.Errorf("Worker HEAD changed; preserve staging and refresh the explicit commit request")
		}
	} else {
		staged, err := gitRaw(ctx, e.Path, "diff", "--cached", "--name-only", "-z")
		if err != nil {
			return receipt, err
		}
		for _, p := range strings.Split(staged, "\x00") {
			if p != "" && !allowed[p] {
				return receipt, fmt.Errorf("unrelated staged file must be resolved explicitly: %s", p)
			}
		}
		mainHead, err := Git(ctx, b.MainRoot, "rev-parse", "HEAD")
		if err != nil {
			return receipt, err
		}
		if err = persist(); err != nil {
			return receipt, err
		}
		args := append([]string{"--literal-pathspecs", "add", "--"}, request.Paths...)
		if _, err = Git(ctx, e.Path, args...); err != nil {
			return receipt, err
		}
		if _, err = Git(ctx, e.Path, "commit", "-m", message); err != nil {
			return receipt, fmt.Errorf("commit failed; staging and hooks are preserved for retry: %w", err)
		}
		if err = b.Validate(ctx, b.MainRoot); err != nil {
			return receipt, err
		}
		currentMain, err := Git(ctx, b.MainRoot, "rev-parse", "HEAD")
		if err != nil {
			return receipt, err
		}
		if currentMain != mainHead {
			return receipt, fmt.Errorf("Main HEAD changed during Worker commit; preserve both checkouts for review")
		}
		head, err = Git(ctx, e.Path, "rev-parse", "HEAD")
		if err != nil {
			return receipt, err
		}
	}
	changed, err := gitRaw(ctx, e.Path, "diff-tree", "--no-commit-id", "--name-only", "-r", "-z", head)
	if err != nil {
		return receipt, err
	}
	for _, p := range strings.Split(changed, "\x00") {
		if p != "" && !allowed[p] {
			return receipt, fmt.Errorf("Git hook committed an undeclared file %s; preserve commit %s for review", p, head)
		}
	}
	receipt.Commit = head
	receipt.State = "committed"
	return receipt, persist()
}
