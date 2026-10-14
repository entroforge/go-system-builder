package controller_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/filelock"
)

func TestControlCycleParentDeadlineIncludesSnapshotLock(t *testing.T) {
	root := t.TempDir()
	release, err := filelock.Acquire(context.Background(), filepath.Join(root, ".claude/loop-state.json.lock.process"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	out, err := controller.RunControlCycle(ctx, controller.ControlRequest{Root: root, Event: "PreToolUse", ToolName: "Read"})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second || !strings.Contains(out.Error, "deadline exceeded") {
		t.Fatalf("Runtime reset the budget: %#v", out)
	}
	if out.QualityGate.Status != controller.StatusUnknown || out.QualityGate.TransitionCommitted {
		t.Fatal("timeout became successful progress")
	}
	if out.Timing["runtime_lock_wait"].Calls != 1 || out.Timing["runtime_lock_hold"].Calls != 0 {
		t.Fatalf("wrong phases: %#v", out.Timing)
	}
}
