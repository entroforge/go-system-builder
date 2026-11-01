package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/submission"
)

func assertOriginalAnswerWriteNotDenied(t *testing.T, root, cwd, owner, answer string) {
	t.Helper()
	in := policy.Input{Event: "PreToolUse", AgentID: owner, CWD: cwd, ToolName: "Write", ToolInput: map[string]any{"file_path": answer}}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"hook", "--root", root, "--event", "PreToolUse"}, bytes.NewReader(raw), &out, &diagnostic); code != 0 {
		t.Fatalf("public Hook: %d %s %s", code, &out, &diagnostic)
	}
	if out.Len() == 0 {
		return
	}
	var response struct {
		Decision string `json:"decision"`
		Hook     struct {
			Permission string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("invalid Hook output %q: %v", &out, err)
	}
	if response.Decision == "deny" || response.Decision == "block" || response.Hook.Permission == "deny" {
		t.Fatalf("original answer write denied: %s; %s", &out, &diagnostic)
	}
}

func TestS8IssuedAnswerPublicHookAllowsOriginalWrite(t *testing.T) {
	f := newS8ListenerFixture(t)
	assertOriginalAnswerWriteNotDenied(t, f.root, f.root, f.packet.Binding.OwnerAgentID, filepath.Join(f.root, f.packet.AnswerPath))
}

func TestS9NativeAnswerPublicHookAllowsOriginalWrite(t *testing.T) {
	root, worker, packet, in, _ := nativeS9WorkerFixture(t)
	plan, result, err := nativeS9AuthorPaths(root, packet)
	if err != nil {
		t.Fatal(err)
	}
	assertOriginalAnswerWriteNotDenied(t, root, root, packet.Binding.OwnerAgentID, filepath.Join(root, packet.AnswerPath))
	// Do not complete the answers during this PreToolUse observation.
	for _, ref := range []string{plan, result} {
		if err := os.Remove(filepath.Join(worker, ref)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	assertOriginalAnswerWriteNotDenied(t, root, worker, in.AgentID, filepath.Join(worker, plan))
	assertOriginalAnswerWriteNotDenied(t, root, worker, in.AgentID, filepath.Join(worker, result))
}

func TestS9IssuedResultPublicHookAllowsOriginalWrite(t *testing.T) {
	root, worker, plan, _, _ := nativeS9WorkerFixture(t)
	ctx := context.Background()
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := receiveNativeS9ReadyPlan(ctx, root, worker, plan, service, &bytes.Buffer{}, nil); err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.SaveRepairPlanReportFrom(ctx, plan.Binding.OwnerAgentID, filepath.Join(root, plan.AnswerPath), root, nil); err != nil || !receipt.Accepted {
		t.Fatalf("original plan receipt: %+v %v", receipt, err)
	}
	if err := recordNativeS9OriginalLocation(ctx, root, plan, worker); err != nil {
		t.Fatal(err)
	}
	packets, err := service.IssueRepairResults(ctx, plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original Result issuance: %+v %v", packets, err)
	}
	assertOriginalAnswerWriteNotDenied(t, root, worker, plan.Binding.OwnerAgentID, filepath.Join(worker, packets[0].AnswerPath))
	_, originalResult, err := nativeS9AuthorPaths(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	assertOriginalAnswerWriteNotDenied(t, root, worker, plan.Binding.OwnerAgentID, filepath.Join(worker, originalResult))
}
