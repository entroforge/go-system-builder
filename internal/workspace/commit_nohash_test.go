package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceCommitWithoutHashOrExpectedHead(t *testing.T) {
	b, e, _, ctx := workerFixture(t)
	if err := os.WriteFile(filepath.Join(e.Path, "input.md"), []byte("first actual work"), 0600); err != nil {
		t.Fatal(err)
	}
	request := CommitRequest{Message: "actual work", Paths: []string{"input.md"}}
	first, err := b.Commit(ctx, e, request)
	if err != nil || first.State != "committed" {
		t.Fatalf("first commit: %+v %v", first, err)
	}
	message, err := Git(ctx, e.Path, "log", "-1", "--format=%B")
	if err != nil || message != request.Message {
		t.Fatalf("actual commit message changed: %q %v", message, err)
	}
	replay, err := b.Commit(ctx, e, request)
	if err != nil || replay.State != "committed" {
		t.Fatalf("original replay: %+v %v", replay, err)
	}
	if err := os.WriteFile(filepath.Join(e.Path, "input.md"), []byte("second actual work"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := b.Commit(ctx, e, request)
	if err != nil || second.State != "committed" {
		t.Fatalf("new real work or old expected head: %+v %v", second, err)
	}
	dir := filepath.Join(b.MainRoot, ".claude/evidence", e.RuntimeID, "g1/worktree", e.AssignmentID, "commits")
	for _, name := range []string{"operation-1.json", "operation-2.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "sha256") || strings.Contains(string(data), "expected_head") {
			t.Fatalf("new operation retained machine identity: %s", data)
		}
	}
}

func TestWorkspaceCommitUnknownWindowDoesNotRepeatGit(t *testing.T) {
	b, e, _, ctx := workerFixture(t)
	if err := os.WriteFile(filepath.Join(e.Path, "input.md"), []byte("actual work"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(b.MainRoot, ".claude/evidence", e.RuntimeID, "g1/worktree", e.AssignmentID, "commits")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	receipt := CommitReceipt{Execution: Execution{RuntimeID: e.RuntimeID, AssignmentID: e.AssignmentID, Generation: e.Generation, AgentID: e.AgentID}, State: "preparing", Request: &CommitRequest{Message: "actual work", Paths: []string{"input.md"}}}
	data, _ := json.Marshal(receipt)
	if err := os.WriteFile(filepath.Join(dir, "operation-1.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := Git(ctx, e.Path, "status", "--porcelain=v1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Commit(ctx, e, CommitRequest{Message: "actual work", Paths: []string{"input.md"}})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown outcome was guessed: %v", err)
	}
	after, err := Git(ctx, e.Path, "status", "--porcelain=v1")
	if err != nil || after != before {
		t.Fatalf("unknown retry changed original worktree: %q / %q: %v", before, after, err)
	}
}
