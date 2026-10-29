package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
)

type workspaceReadCancellation struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *workspaceReadCancellation) Err() error {
	c.checks++
	if c.checks == 4 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestPrepareWorkspacePreservesCancellationBeforeCreationAndDuringDigest(t *testing.T) {
	root := t.TempDir()
	workspace := "e2e-workspace/cancelled"
	plan := &Plan{VerificationArtifactWorkspace: &workspace}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if digest, err := prepareVerificationWorkspace(root, plan, ctx); !errors.Is(err, context.Canceled) || digest != "" {
		t.Fatalf("cancelled preparation returned %q %v", digest, err)
	}
	if _, err := os.Stat(filepath.Join(root, workspace)); !os.IsNotExist(err) {
		t.Fatalf("cancelled preparation created a workspace: %v", err)
	}
	parent, cancelWalk := context.WithCancel(context.Background())
	defer cancelWalk()
	interrupted := &workspaceReadCancellation{Context: parent, cancel: cancelWalk}
	if digest, err := prepareVerificationWorkspace(root, plan, interrupted); !errors.Is(err, context.Canceled) || digest != "" {
		t.Fatalf("preparation lost digest cancellation: %q %v", digest, err)
	}
}

func TestWorkspaceDigestContextCancelsDuringWalkAndRechecksLiveFiles(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "e2e-workspace")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	interrupted := &workspaceReadCancellation{Context: parent, cancel: cancel}
	if digest, err := WorkspaceDigestContext(interrupted, root, "e2e-workspace"); !errors.Is(err, context.Canceled) || digest != "" {
		t.Fatalf("workspace walk ignored cancellation: %q %v", digest, err)
	}
	if interrupted.checks < 4 {
		t.Fatal("test did not enter file walk")
	}
	ctx := buildsurface.WithReadCache(context.Background())
	before, err := WorkspaceDigestContext(ctx, root, "e2e-workspace")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "a.txt"), []byte("changed after first verification"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := WorkspaceDigestContext(ctx, root, "e2e-workspace")
	if err != nil || before == after {
		t.Fatalf("request cache waived live workspace recheck: %q %q %v", before, after, err)
	}
}

type cancelAtSealedReceiptConsumer struct {
	context.Context
	cancel  context.CancelFunc
	reached bool
}

func (c *cancelAtSealedReceiptConsumer) Err() error {
	if !c.reached {
		callers := make([]uintptr, 64)
		n := goruntime.Callers(2, callers)
		frames := goruntime.CallersFrames(callers[:n])
		for {
			frame, more := frames.Next()
			if strings.HasSuffix(frame.Function, "/review.IssuedResultReceiptContext") {
				c.reached = true
				c.cancel()
				break
			}
			if !more {
				break
			}
		}
	}
	return c.Context.Err()
}

func TestSealedArtifactVerificationCarriesContextToOriginalReceiptConsumer(t *testing.T) {
	state := map[string]any{"review": map[string]any{
		"plan":        map[string]any{"plan_id": "review-plan-source", "path": ".claude/review/plans/source.json", "sha256": strings.Repeat("a", 64), "revision": 1, "review_round": 1, "status": "clean", "e2e_coverage_state": "cold_start", "verification_artifact_workspace": "e2e-workspace"},
		"assignments": map[string]any{"assignment-e2e-source": map[string]any{"lens": "e2e", "status": "consumed", "agent_id": "reviewer-source", "result_receipt": map[string]any{"packet_id": "packet-source"}}},
	}}
	ptr := PlanPointerFromState(state)
	if ptr == nil {
		t.Fatal("invalid test Plan projection")
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancelAtSealedReceiptConsumer{Context: parent, cancel: cancel}
	err := VerifySealedArtifactDigests(t.TempDir(), state, ptr, nil, ctx)
	if !ctx.reached || !errors.Is(err, context.Canceled) {
		t.Fatalf("sealed E2E consumer lost original request context: reached=%t err=%v", ctx.reached, err)
	}
}
