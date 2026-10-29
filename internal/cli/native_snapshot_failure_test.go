package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestNativeSnapshotSuccessfulReadRestoresFailedObservationReader(t *testing.T) {
	root := t.TempDir()
	// The bounded observation refuses this size while the normal Snapshot can
	// read it. No error injection or replacement state is involved.
	routingState(t, root, fmt.Sprintf(`{"revision":1,"lifecycle":{"state":"planning"},"entities":{"agents":[{"id":"known-worker"}]},"padding":"%s"}`, strings.Repeat("x", 4<<20)))
	in := withHookReader(root, policy.Input{Event: "PreToolUse", AgentID: "known-worker", AgentType: "backend-builder", CWD: root})
	reader, ok := contextForHook(in).Value(hookReaderKey{}).(*hookReader)
	if !ok || reader.Kind != "Helper" || reader.snapshotErr == nil {
		t.Fatalf("observation did not preserve provisional failure: %+v", reader)
	}
	var out, diagnostic bytes.Buffer
	if handled, code := nativeHookRoutes(root, in, &out, &diagnostic); handled || code != 0 {
		t.Fatalf("valid planning state was swallowed: handled=%t code=%d out=%s diag=%s", handled, code, &out, &diagnostic)
	}
	if reader.Kind != "Worker" || reader.Owner != "known-worker" || reader.snapshotErr != nil || reader.snapshot.State == nil {
		t.Fatalf("successful ordinary Snapshot did not restore the reader: %+v", reader)
	}
}

func TestNativeSnapshotCanceledRequestDoesNotReadOrRecover(t *testing.T) {
	root := t.TempDir()
	routingState(t, root, `{"revision":1}`)
	marker := filepath.Join(root, ".claude/loop-state.json.commit-pending.json")
	if err := os.WriteFile(marker, []byte("original pending marker"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, timings := metrics.WithCallTimings(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	in := withHookReader(root, policy.Input{Context: ctx, Event: "SubagentStop", AgentID: "worker", CWD: root})
	reader := contextForHook(in).Value(hookReaderKey{}).(*hookReader)
	if !errors.Is(reader.snapshotErr, context.Canceled) {
		t.Fatalf("canceled observation lost its cause: %+v", reader)
	}
	var out, diagnostic bytes.Buffer
	if handled, code := nativeHookRoutes(root, in, &out, &diagnostic); !handled || code != 0 || out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatalf("canceled request continued: handled=%t code=%d out=%s diag=%s", handled, code, &out, &diagnostic)
	}
	if got := timings.Snapshot()["hook/native_routes/initial_snapshot"].Count; got != 0 {
		t.Fatalf("canceled request read Runtime %d times", got)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "original pending marker" {
		t.Fatalf("canceled request changed pending marker: %s %v", data, err)
	}
}
