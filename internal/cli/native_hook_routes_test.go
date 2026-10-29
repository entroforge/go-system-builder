package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func routingState(t *testing.T, root, state string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), []byte(state), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeHookRoutesShareOneInitialReadAndDoNotLeakIntoNextEvent(t *testing.T) {
	root := t.TempDir()
	const state = `{"revision":1,"lifecycle":{"state":"planning"}}`
	routingState(t, root, state)
	uncached, baseline := metrics.WithCallTimings(context.Background())
	plain := policy.Input{Context: uncached, Event: "PostToolUse", ToolName: "Agent", CWD: root}
	nativeMainReturnHook(root, plain, io.Discard)
	nativeS6Hook(root, plain, io.Discard, io.Discard)
	nativeS9Hook(root, plain, io.Discard, io.Discard)
	nativeReviewHook(root, plain, io.Discard, io.Discard)
	if got := baseline.Snapshot()["hook/native_routes/initial_snapshot"].Count; got != 4 {
		t.Fatalf("unshared adapter Snapshot calls=%d, want 4", got)
	}
	ctx, timings := metrics.WithCallTimings(context.Background())
	in := policy.Input{Context: ctx, Event: "PostToolUse", ToolName: "Agent", CWD: root}
	// All four adapters are unrelated; each reaches its initial read and
	// returns false, without changing the original Runtime bytes.
	for call := int64(1); call <= 2; call++ {
		if handled, code := nativeHookRoutes(root, in, io.Discard, io.Discard); handled || code != 0 {
			t.Fatalf("unrelated event handled=%t code=%d", handled, code)
		}
		if got := timings.Snapshot()["hook/native_routes/initial_snapshot"].Count; int64(got) != call {
			t.Fatalf("actual Snapshot calls=%d, want %d", got, call)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil || string(data) != state {
		t.Fatalf("false routing changed Runtime: %s %v", data, err)
	}
	if in.Context.Value(nativeRoutingKey{}) != nil {
		t.Fatal("routing cache escaped into caller")
	}
}

func TestNativeRoutingSnapshotIsNotCurrentStateOrWriterCache(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	routingState(t, root, `{"revision":1}`)
	routingState(t, other, `{"revision":7}`)
	ctx, timings := metrics.WithCallTimings(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	in := policy.Input{Context: context.WithValue(ctx, nativeRoutingKey{}, &nativeRoutingRead{root: root})}
	first, err := nativeRoutingSnapshot(root, in)
	if err != nil || first.Revision != 1 {
		t.Fatalf("first=%+v %v", first, err)
	}
	// Simulate a commit after routing. Current Store reads must observe it;
	// this cache is explicitly only the original adapter selection view.
	routingState(t, root, `{"revision":2}`)
	current, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).WithContext(ctx).Snapshot()
	if err != nil || current.Revision != 2 {
		t.Fatalf("current Store read became stale: %+v %v", current, err)
	}
	initial, err := nativeRoutingSnapshot(root, in)
	if err != nil || initial.Revision != 1 {
		t.Fatalf("routing view=%+v %v", initial, err)
	}
	unrelated, err := nativeRoutingSnapshot(other, in)
	if err != nil || unrelated.Revision != 7 {
		t.Fatalf("root isolation=%+v %v", unrelated, err)
	}
	if got := timings.Snapshot()["hook/native_routes/initial_snapshot"].Count; got != 2 {
		t.Fatalf("actual initial Snapshot calls=%d, want 2", got)
	}
	cancel()
	if _, err := nativeRoutingSnapshot(root, in); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled routing reused cached success: %v", err)
	}
}

func TestNativeHookRoutesFailedReadIsRetriedByNextEvent(t *testing.T) {
	root := t.TempDir()
	routingState(t, root, `{"revision":1}`)
	marker := filepath.Join(root, ".claude/loop-state.json.commit-pending.json")
	if err := os.WriteFile(marker, []byte("pending"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, timings := metrics.WithCallTimings(context.Background())
	in := policy.Input{Context: ctx, Event: "SubagentStop", AgentID: "unrelated"}
	if handled, _ := nativeHookRoutes(root, in, io.Discard, io.Discard); !handled {
		t.Fatal("failed pending recovery escaped the native Stop boundary")
	}
	if got := timings.Snapshot()["hook/native_routes/initial_snapshot"].Count; got != 1 {
		t.Fatalf("failed actual Snapshot calls=%d, want 1", got)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if _, err := nativeRoutingSnapshot(root, in); err != nil {
		t.Fatalf("next event retained old read failure: %v", err)
	}
}

func TestNativeHookRoutesUnrelatedLifecycleSkipsTaskTranscript(t *testing.T) {
	root := t.TempDir()
	routingState(t, root, `{"revision":1,"lifecycle":{"state":"planning"}}`)
	ctx, timings := metrics.WithCallTimings(context.Background())
	in := policy.Input{Context: ctx, Event: "SubagentStop", AgentID: "unrelated", AgentType: "backend-builder", AgentTranscriptPath: filepath.Join(root, "absent-transcript")}
	if handled, code := nativeHookRoutes(root, in, io.Discard, io.Discard); handled || code != 0 {
		t.Fatalf("unrelated lifecycle handled=%t code=%d", handled, code)
	}
	if spans := timings.Snapshot(); spans["hook/native_routes/task_text"].Count != 0 || spans["hook/native_routes/initial_snapshot"].Count != 1 {
		t.Fatalf("unrelated lifecycle did unnecessary routing work: %+v", spans)
	}
}
