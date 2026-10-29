package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestNativeMainPendingCancellationPreservesPair(t *testing.T) {
	root := investigationLeadTestRoot(t)
	marker := stopFingerprintPending(t, root)
	prior, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	state, _ := os.ReadFile(statePath)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	in := policy.Input{Context: ctx, Event: "PreToolUse", AgentID: "Main", CWD: root, ToolName: "Read"}
	var out, diag bytes.Buffer
	nativeHookRoutes(root, in, &out, &diag)
	if got, _ := os.ReadFile(marker); !bytes.Equal(prior, got) {
		t.Fatal("cancelled request changed pending operation")
	}
	if got, _ := os.ReadFile(statePath); !bytes.Equal(state, got) {
		t.Fatal("cancelled request changed Runtime")
	}
}

// The public evaluator has older Controller recovery behavior for a foreign
// cwd. Check this new native preflight boundary directly rather than claiming
// that it controls those existing downstream paths.
func TestNativeMainPendingForeignRootDoesNotRecoverAtNativeBoundary(t *testing.T) {
	root := investigationLeadTestRoot(t)
	marker := stopFingerprintPending(t, root)
	prior, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if nativePendingRuntimeMainContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: t.TempDir(), ToolName: "Read"}, &out) || out.Len() != 0 {
		t.Fatal("foreign cwd claimed Main recovery")
	}
	if got, _ := os.ReadFile(marker); !bytes.Equal(prior, got) {
		t.Fatal("foreign cwd recovered original pair")
	}
}
